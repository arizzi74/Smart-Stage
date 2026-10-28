package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"smartstage/internal/app"
	"smartstage/internal/auth"
)

const maxLiveSockets = 32
const liveReadLimit = 1024
const liveHeartbeat = 10 * time.Second
const liveTimeout = 3 * time.Second
const liveAuthenticationTimeout = 5 * time.Second

const liveSessionExpired websocket.StatusCode = 4401
const liveCSRFDenied websocket.StatusCode = 4403

// NewCloudflareCommand creates a command-only handler for a dedicated loopback
// listener behind cloudflared. The public HTTPS Host and Origin are checked
// without trusting forwarding headers. Its separate authentication manager
// must belong to this tunnel. Close must run before HTTP server shutdown,
// because net/http does not close hijacked WebSocket connections.
func NewCloudflareCommand(service *app.Service, authentication *auth.Manager, assets http.Handler, publicURL string) (*API, error) {
	u, err := url.Parse(publicURL)
	if err != nil || len(publicURL) > 512 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(publicURL, "#") || u.RawPath != "" || (u.Path != "" && u.Path != "/") || !validPublicHostname(u.Hostname()) || strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("public command URL must be an HTTPS origin")
	}
	port := 443
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, errors.New("invalid public command port")
		}
	}
	a := NewCommand(service, authentication, assets, []string{u.Hostname()}, port)
	publicHost := strings.ToLower(u.Hostname())
	if port != 443 {
		publicHost = net.JoinHostPort(publicHost, strconv.Itoa(port))
	}
	a.publicOrigin = "https://" + publicHost
	ctx, cancel := context.WithCancel(context.Background())
	a.websockets = &liveSockets{ctx: ctx, cancel: cancel, connections: make(map[net.Conn]struct{}), slots: make(chan struct{}, maxLiveSockets)}
	return a, nil
}

func validPublicHostname(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}

type liveSockets struct {
	mu          sync.Mutex
	isClosed    bool
	connections map[net.Conn]struct{}
	slots       chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
}

func (s *liveSockets) closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isClosed
}

func (s *liveSockets) add(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isClosed {
		return false
	}
	s.connections[c] = struct{}{}
	return true
}

func (s *liveSockets) remove(c net.Conn) {
	s.mu.Lock()
	delete(s.connections, c)
	s.mu.Unlock()
}

// Close revokes this Cloudflare handler and closes all of its live sockets,
// including sockets being registered concurrently. Other API modes have no
// hijacked connections and need no additional shutdown work.
func (a *API) Close() error {
	s := a.websockets
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.isClosed = true
	connections := make([]net.Conn, 0, len(s.connections))
	for c := range s.connections {
		connections = append(connections, c)
	}
	s.mu.Unlock()
	s.cancel()
	for _, c := range connections {
		_ = c.Close()
	}
	return nil
}

type liveUpgradeWriter struct {
	http.ResponseWriter
	sockets *liveSockets
	conn    net.Conn
}

func (w *liveUpgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	// Track the raw connection at the instant net/http releases ownership.
	// Closing it also interrupts a WebSocket close handshake already in flight.
	if !w.sockets.add(conn) {
		_ = conn.Close()
		return nil, nil, net.ErrClosed
	}
	w.conn = conn
	return conn, buffer, nil
}

func (a *API) liveStatus(w http.ResponseWriter, r *http.Request, session auth.Session) {
	// Browsers send Origin on WebSocket upgrades. Requiring it also excludes
	// cookie-authenticated clients that skip the same-origin handshake.
	if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") == "" {
		fail(w, http.StatusForbidden, "origin_required", "Open the paired Smart Stage page")
		return
	}
	s := a.websockets
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		fail(w, http.StatusServiceUnavailable, "overloaded", "Too many live status connections")
		return
	}
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	// A host BaseContext cancellation must also terminate a hijacked socket.
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer stopRequest()
	upgrade := &liveUpgradeWriter{ResponseWriter: w, sockets: s}
	defer func() {
		if upgrade.conn != nil {
			s.remove(upgrade.conn)
			_ = upgrade.conn.Close()
		}
	}()
	conn, err := websocket.Accept(upgrade, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(liveReadLimit)
	// A browser cannot set the HTTP CSRF header on an upgrade. Prove its CSRF
	// token in the first bounded frame before exposing any status information.
	authCtx, done := context.WithTimeout(ctx, liveAuthenticationTimeout)
	messageType, data, err := conn.Read(authCtx)
	done()
	if err != nil {
		return
	}
	var hello struct {
		Type      string `json:"type"`
		CSRFToken string `json:"csrfToken"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if messageType != websocket.MessageText || decoder.Decode(&hello) != nil || decoder.Decode(new(any)) != io.EOF || hello.Type != "authenticate" || !auth.CheckCSRF(session, hello.CSRFToken) {
		_ = conn.Close(liveCSRFDenied, "A valid session CSRF token is required")
		return
	}
	if current, ok := a.auth.Get(session.ID); !ok || current.Role != "command" {
		_ = conn.Close(liveSessionExpired, "Pair this browser session again")
		return
	}
	updates, unsubscribe, err := a.app.Subscribe()
	if err != nil {
		_ = conn.Close(websocket.StatusTryAgainLater, "Live status is busy; reconnect")
		return
	}
	defer unsubscribe()
	// Status is server-to-client only. CloseRead handles protocol ping/pong and
	// rejects any further application frames instead of accepting commands.
	readCtx := conn.CloseRead(ctx)
	heartbeat := time.NewTicker(liveHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-readCtx.Done():
			return
		case _, ok := <-updates:
			if !ok {
				return
			}
			if current, ok := a.auth.Get(session.ID); !ok || current.Role != "command" {
				_ = conn.Close(liveSessionExpired, "Pair this browser session again")
				return
			}
			if err := writeLiveMessage(ctx, conn, map[string]any{"type": "state", "state": a.app.Snapshot(false)}); err != nil {
				return
			}
		case <-heartbeat.C:
			if current, ok := a.auth.Get(session.ID); !ok || current.Role != "command" {
				_ = conn.Close(liveSessionExpired, "Pair this browser session again")
				return
			}
			pingCtx, done := context.WithTimeout(ctx, liveTimeout)
			err := conn.Ping(pingCtx)
			done()
			if err != nil {
				return
			}
			if err := writeLiveMessage(ctx, conn, map[string]string{"type": "heartbeat"}); err != nil {
				return
			}
		}
	}
}

func writeLiveMessage(ctx context.Context, conn *websocket.Conn, value any) error {
	ctx, done := context.WithTimeout(ctx, liveTimeout)
	defer done()
	return wsjson.Write(ctx, conn, value)
}
