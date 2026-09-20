// Package gateway provides a narrowly scoped reverse tunnel for Smart Stage
// remote controls. It never proxies arbitrary URLs or the local Admin API.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	commandCookie  = "smartstage_command_session"
	maxBody        = 1 << 20
	maxFrame       = 2 << 20
	chunkSize      = 32 << 10
	requestTimeout = 30 * time.Second
	writeTimeout   = 10 * time.Second
)

// Config is stored in a mode-0600 file on the public server. Token authorizes
// desktop registration; it is separate from each desktop's phone pairing token.
type Config struct {
	Listen    string `json:"listen"`
	PublicURL string `json:"publicURL"`
	Token     string `json:"token"`
}

// ValidateURL accepts the HTTPS location configured by the gateway installer.
func ValidateURL(raw string) (*url.URL, error) {
	if len(raw) > 512 {
		return nil, errors.New("gateway URL is too long")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || (u.Path != "/smartstage" && u.Path != "/smartstage/") {
		return nil, errors.New("gateway URL must be https://hostname/smartstage")
	}
	u.Path = "/smartstage"
	u.Host = strings.ToLower(u.Host)
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, errors.New("gateway URL port must be between 1 and 65535")
		}
		if value == 443 {
			host := u.Hostname()
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			u.Host = host
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("gateway URL has an empty port")
	}
	return u, nil
}

// NewToken returns a cryptographically random 256-bit registration secret.
func NewToken() (string, error) { return randomHex(32) }

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validToken(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

func tokenEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func allowed(method, path string) bool {
	switch path {
	case "/command", "/assets/app.js", "/assets/style.css", "/assets/wake-lock.js", "/favicon.ico", "/licenses.txt":
		return method == http.MethodGet || method == http.MethodHead
	case "/api/state", "/api/events":
		return method == http.MethodGet
	case "/api/pair", "/api/play", "/api/stop", "/api/emergency-stop", "/api/stage-output", "/api/logout":
		return method == http.MethodPost
	}
	return false
}

func isStop(path string) bool { return path == "/api/stop" || path == "/api/emergency-stop" }

func requestHeaders(h http.Header) http.Header {
	out := make(http.Header)
	for _, k := range []string{"Accept", "Content-Type", "Origin", "X-CSRF-Token", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Last-Event-Id"} {
		if values := h.Values(k); len(values) != 0 {
			out[http.CanonicalHeaderKey(k)] = append([]string(nil), values...)
		}
	}
	// An existing virtual host can also serve unrelated applications whose
	// cookies use Path=/. Never disclose those website sessions to a desktop.
	for _, cookie := range (&http.Request{Header: h}).Cookies() {
		if cookie.Name == commandCookie {
			out.Add("Cookie", cookie.String())
		}
	}
	return out
}

func responseHeaders(h http.Header, prefix string) http.Header {
	out := make(http.Header)
	for _, k := range []string{"Content-Type", "Cache-Control", "Content-Security-Policy", "Referrer-Policy", "X-Frame-Options", "X-Content-Type-Options", "Retry-After", "X-Accel-Buffering"} {
		if values := h.Values(k); len(values) != 0 {
			out[http.CanonicalHeaderKey(k)] = append([]string(nil), values...)
		}
	}
	// A registered desktop cannot set cookies for another endpoint or the
	// existing website that shares this virtual host.
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.Name != commandCookie {
			continue
		}
		if c.Path != strings.TrimSuffix(prefix, "/") && c.Path != prefix {
			continue
		}
		c.Domain = ""
		c.Secure = true
		c.HttpOnly = true
		c.SameSite = http.SameSiteStrictMode
		out.Add("Set-Cookie", c.String())
	}
	return out
}

func validOrigin(h http.Header, origin, method, path string) bool {
	values := h.Values("Origin")
	if len(values) > 1 || len(values) == 1 && values[0] != origin {
		return false
	}
	if h.Get("Sec-Fetch-Site") != "cross-site" {
		return true
	}
	// Opening the QR link from Admin (or another application) is a cross-site
	// navigation. It may load the public pairing page, never authenticated API
	// data or a mutation. Existing website cookies are filtered separately.
	return (method == http.MethodGet || method == http.MethodHead) && path == "/command" && h.Get("Sec-Fetch-Mode") == "navigate" && h.Get("Sec-Fetch-Dest") == "document"
}

type message struct {
	Type   string      `json:"type"`
	ID     uint64      `json:"id,omitempty"`
	Prefix string      `json:"prefix,omitempty"`
	Method string      `json:"method,omitempty"`
	Path   string      `json:"path,omitempty"`
	Header http.Header `json:"header,omitempty"`
	Body   []byte      `json:"body,omitempty"`
	Status int         `json:"status,omitempty"`
}

func writeMessage(ctx context.Context, c *websocket.Conn, m message) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, m)
}

func heartbeat(ctx context.Context, c *websocket.Conn, cancel context.CancelFunc) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pingCtx, done := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pingCtx)
			done()
			if err != nil {
				cancel()
				_ = c.CloseNow()
				return
			}
		}
	}
}

// Capacity reserves a separate lane for STOP even while ordinary requests or
// long-lived event streams are at their limits. Both ends enforce these limits.
type capacity struct{ ordinary, streams, stops chan struct{} }

func newCapacity() capacity {
	return capacity{make(chan struct{}, 32), make(chan struct{}, 32), make(chan struct{}, 4)}
}

func (c capacity) acquire(path string) (func(), bool) {
	slots := c.ordinary
	if path == "/api/events" {
		slots = c.streams
	} else if isStop(path) {
		slots = c.stops
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
}
