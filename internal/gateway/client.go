package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type ClientOptions struct {
	URL   string
	Token string
	// Handler builds an isolated command-only handler after registration. Its
	// cookie and asset paths use prefix, which always has a trailing slash.
	Handler      func(prefix string) http.Handler
	OnConnect    func(endpointURL string)
	OnDisconnect func(error)
	// HTTPClient may supply a custom TLS trust store. Redirects are always
	// disabled so the registration token cannot be redirected elsewhere.
	HTTPClient *http.Client
}

// ServeConnection makes one outbound WSS connection and blocks until it ends.
// Callers may retry with bounded backoff; a retry always creates a new endpoint
// and handler. No commands are retained or replayed across connections.
func ServeConnection(ctx context.Context, opts ClientOptions) (result error) {
	if opts.OnDisconnect != nil {
		defer func() { opts.OnDisconnect(result) }()
	}
	u, err := ValidateURL(opts.URL)
	if err != nil {
		return err
	}
	if !validToken(opts.Token) {
		return errors.New("invalid gateway registration token")
	}
	if opts.Handler == nil {
		return errors.New("gateway command handler is required")
	}
	client := http.Client{}
	if opts.HTTPClient != nil {
		client = *opts.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dialURL := *u
	dialURL.Scheme = "wss"
	dialURL.Path += "/api/connect"
	dialCtx, done := context.WithTimeout(ctx, 10*time.Second)
	conn, response, err := websocket.Dial(dialCtx, dialURL.String(), &websocket.DialOptions{HTTPClient: &client, HTTPHeader: http.Header{"Authorization": {"Bearer " + opts.Token}}, CompressionMode: websocket.CompressionDisabled})
	done()
	if err != nil {
		// Do not return websocket/http error text: it can contain deployment
		// URLs or credentials supplied by an untrusted upstream proxy.
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			return errors.New("gateway rejected the registration token")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("could not establish a secure gateway connection")
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxFrame)
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	helloCtx, done := context.WithTimeout(connCtx, 10*time.Second)
	var hello message
	err = wsjson.Read(helloCtx, conn, &hello)
	done()
	if err != nil || hello.Type != "hello" || !validPrefix(u.Path, hello.Prefix) {
		return errors.New("invalid gateway registration response")
	}
	handler := opts.Handler(hello.Prefix)
	if handler == nil {
		return errors.New("gateway command handler is unavailable")
	}
	if err := writeMessage(connCtx, conn, message{Type: "ready"}); err != nil {
		return errors.New("gateway disconnected during registration")
	}
	registeredCtx, done := context.WithTimeout(connCtx, 10*time.Second)
	var registered message
	err = wsjson.Read(registeredCtx, conn, &registered)
	done()
	if err != nil || registered.Type != "registered" {
		return errors.New("gateway disconnected during registration")
	}
	if opts.OnConnect != nil {
		opts.OnConnect("https://" + u.Host + hello.Prefix + "command")
	}
	go heartbeat(connCtx, conn, cancel)
	var mu sync.Mutex
	pending := make(map[uint64]context.CancelFunc)
	capacity := newCapacity()
	defer func() {
		cancel()
		mu.Lock()
		for _, stop := range pending {
			stop()
		}
		mu.Unlock()
	}()
	for {
		var m message
		if err := wsjson.Read(connCtx, conn, &m); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("gateway connection ended")
		}
		if m.Type == "cancel" && m.ID != 0 {
			mu.Lock()
			stop := pending[m.ID]
			mu.Unlock()
			if stop != nil {
				stop()
			}
			continue
		}
		if m.Type != "request" || m.ID == 0 {
			return errors.New("invalid gateway relay message")
		}
		if !allowed(m.Method, m.Path) || len(m.Body) > maxBody || m.Method != http.MethodPost && len(m.Body) != 0 || !validOrigin(m.Header, "https://"+u.Host, m.Method, m.Path) {
			if err := rejectRequest(connCtx, conn, m.ID, http.StatusForbidden); err != nil {
				return errors.New("gateway connection ended")
			}
			continue
		}
		release, ok := capacity.acquire(m.Path)
		if !ok {
			if err := rejectRequest(connCtx, conn, m.ID, http.StatusTooManyRequests); err != nil {
				return errors.New("gateway connection ended")
			}
			continue
		}
		reqCtx, stop := context.WithCancel(connCtx)
		if m.Path != "/api/events" {
			stop()
			reqCtx, stop = context.WithTimeout(connCtx, requestTimeout)
		}
		mu.Lock()
		if pending[m.ID] != nil {
			mu.Unlock()
			release()
			stop()
			return errors.New("duplicate gateway request")
		}
		pending[m.ID] = stop
		mu.Unlock()
		go func(m message, reqCtx context.Context, stop context.CancelFunc, release func()) {
			defer release()
			defer stop()
			defer func() { mu.Lock(); delete(pending, m.ID); mu.Unlock() }()
			r := &http.Request{Method: m.Method, URL: &url.URL{Scheme: "https", Host: u.Host, Path: m.Path}, Header: requestHeaders(m.Header), Body: io.NopCloser(bytes.NewReader(m.Body)), ContentLength: int64(len(m.Body)), Host: u.Host, RemoteAddr: "192.0.2.1:0", RequestURI: m.Path, TLS: &tls.ConnectionState{HandshakeComplete: true}, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1}
			r = r.WithContext(reqCtx)
			w := &tunnelWriter{ctx: reqCtx, connCtx: connCtx, conn: conn, id: m.ID, header: make(http.Header), prefix: hello.Prefix, head: m.Method == http.MethodHead}
			defer func() {
				panicked := recover() != nil
				if reqCtx.Err() != nil {
					return
				}
				if panicked && !w.started {
					w.WriteHeader(http.StatusInternalServerError)
				}
				if !w.started {
					w.WriteHeader(http.StatusOK)
				}
				_ = writeMessage(connCtx, conn, message{Type: "done", ID: m.ID})
			}()
			handler.ServeHTTP(w, r)
		}(m, reqCtx, stop, release)
	}
}

func validPrefix(base, prefix string) bool {
	start := base + "/e/"
	if !strings.HasPrefix(prefix, start) || !strings.HasSuffix(prefix, "/") {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(prefix, start), "/")
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && len(id) == 32
}

func rejectRequest(ctx context.Context, conn *websocket.Conn, id uint64, status int) error {
	if err := writeMessage(ctx, conn, message{Type: "response", ID: id, Status: status}); err != nil {
		return err
	}
	return writeMessage(ctx, conn, message{Type: "done", ID: id})
}

type tunnelWriter struct {
	ctx           context.Context
	connCtx       context.Context
	conn          *websocket.Conn
	id            uint64
	header        http.Header
	prefix        string
	started, head bool
	err           error
	deadline      time.Time
}

func (w *tunnelWriter) Header() http.Header { return w.header }
func (w *tunnelWriter) WriteHeader(status int) {
	if w.started {
		return
	}
	if status < 200 || status > 599 {
		status = http.StatusInternalServerError
	}
	w.started = true
	if w.ctx.Err() != nil {
		w.err = w.ctx.Err()
		return
	}
	w.err = w.send(message{Type: "response", ID: w.id, Status: status, Header: responseHeaders(w.header, w.prefix)})
}
func (w *tunnelWriter) Write(b []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	if w.err != nil {
		return 0, w.err
	}
	if w.head {
		return len(b), nil
	}
	n := 0
	for len(b) != 0 {
		if w.ctx.Err() != nil {
			return n, w.ctx.Err()
		}
		size := min(len(b), chunkSize)
		if err := w.send(message{Type: "data", ID: w.id, Body: b[:size]}); err != nil {
			w.err = err
			return n, err
		}
		n += size
		b = b[size:]
	}
	return n, nil
}
func (w *tunnelWriter) Flush() {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
}

func (w *tunnelWriter) SetWriteDeadline(deadline time.Time) error { w.deadline = deadline; return nil }

func (w *tunnelWriter) send(m message) error {
	ctx := w.connCtx
	if !w.deadline.IsZero() {
		var done context.CancelFunc
		ctx, done = context.WithDeadline(ctx, w.deadline)
		defer done()
	}
	return writeMessage(ctx, w.conn, m)
}
