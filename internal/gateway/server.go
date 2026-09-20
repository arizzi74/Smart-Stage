package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Server routes public remote-control requests to authenticated desktop links.
// Run it on loopback behind the installer's HTTPS reverse proxy.
type Server struct {
	cfg         Config
	public      *url.URL
	mu          sync.Mutex
	endpoints   map[string]*endpoint
	connections int
	closed      bool
	ctx         context.Context
	cancel      context.CancelFunc
}

type endpoint struct {
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	prefix   string
	mu       sync.Mutex
	next     uint64
	pending  map[uint64]*pendingRequest
	capacity capacity
}

type pendingRequest struct {
	replies chan message
	failed  chan struct{}
}

func NewServer(cfg Config) (*Server, error) {
	u, err := ValidateURL(cfg.PublicURL)
	if err != nil {
		return nil, err
	}
	if !validToken(cfg.Token) {
		return nil, errors.New("gateway token must contain 64 hexadecimal characters")
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8790"
	}
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("gateway listener must be a loopback IP address and port")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{cfg: cfg, public: u, endpoints: make(map[string]*endpoint), ctx: ctx, cancel: cancel}, nil
}

// Close terminates all tunnels and immediately invalidates their public URLs.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	for _, e := range s.endpoints {
		e.cancel()
		_ = e.conn.CloseNow()
	}
	s.mu.Unlock()
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodGet && r.URL.Path == s.public.Path+"/health" && r.URL.RawPath == "" && r.URL.RawQuery == "" {
		peer, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(peer); ip != nil && ip.IsLoopback() {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "ok\n")
			return
		}
	}
	if !s.secureRequest(r) {
		http.Error(w, "HTTPS gateway origin required", http.StatusForbidden)
		return
	}
	if r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == s.public.Path+"/api/connect" {
		s.connect(w, r)
		return
	}
	base := s.public.Path + "/e/"
	if !strings.HasPrefix(r.URL.Path, base) {
		http.NotFound(w, r)
		return
	}
	id, rest, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, base), "/")
	path := "/" + rest
	if !ok || len(id) != 32 || !allowed(r.Method, path) {
		http.NotFound(w, r)
		return
	}
	if !validOrigin(r.Header, "https://"+s.public.Host, r.Method, path) {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	e := s.endpoints[id]
	s.mu.Unlock()
	if e == nil {
		http.Error(w, "Smart Stage is disconnected. Reconnect using the URL in Admin.", http.StatusGone)
		return
	}
	e.serve(w, r, path)
}

func (s *Server) secureRequest(r *http.Request) bool {
	if !strings.EqualFold(r.Host, s.public.Host) {
		return false
	}
	if r.TLS != nil {
		return true
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(peer)
	return err == nil && ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || len(r.Header.Values("Origin")) != 0 || len(r.Header.Values("Authorization")) != 1 || !tokenEqual(r.Header.Get("Authorization"), "Bearer "+s.cfg.Token) {
		http.Error(w, "Registration denied", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	if s.closed || s.connections >= 16 {
		s.mu.Unlock()
		http.Error(w, "Gateway capacity reached", http.StatusServiceUnavailable)
		return
	}
	s.connections++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.connections--; s.mu.Unlock() }()
	id, err := randomHex(16)
	if err != nil {
		http.Error(w, "Registration unavailable", http.StatusServiceUnavailable)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxFrame)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	e := &endpoint{conn: conn, ctx: ctx, cancel: cancel, prefix: s.public.Path + "/e/" + id + "/", pending: make(map[uint64]*pendingRequest), capacity: newCapacity()}
	if err := writeMessage(ctx, conn, message{Type: "hello", Prefix: e.prefix}); err != nil {
		return
	}
	readyCtx, done := context.WithTimeout(ctx, 10*time.Second)
	var ready message
	err = wsjson.Read(readyCtx, conn, &ready)
	done()
	if err != nil || ready.Type != "ready" {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.endpoints[id] = e
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.endpoints, id); s.mu.Unlock() }()
	if err := writeMessage(ctx, conn, message{Type: "registered"}); err != nil {
		return
	}
	go heartbeat(ctx, conn, cancel)
	for {
		var m message
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			return
		}
		if m.ID == 0 || (m.Type != "response" && m.Type != "data" && m.Type != "done") || len(m.Body) > chunkSize {
			_ = conn.Close(websocket.StatusPolicyViolation, "Invalid relay message")
			return
		}
		e.mu.Lock()
		p := e.pending[m.ID]
		if p != nil {
			select {
			case p.replies <- m:
			default:
				// Never let one slow browser stall every remote and its STOP.
				delete(e.pending, m.ID)
				close(p.failed)
			}
		}
		e.mu.Unlock()
	}
}

func (e *endpoint) serve(w http.ResponseWriter, r *http.Request, path string) {
	release, ok := e.capacity.acquire(path)
	if !ok {
		http.Error(w, "Too many remote requests", http.StatusTooManyRequests)
		return
	}
	defer release()
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Request body too large or incomplete", http.StatusRequestEntityTooLarge)
		return
	}
	if r.Method != http.MethodPost && len(body) != 0 {
		http.Error(w, "Unexpected request body", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	if path != "/api/events" {
		cancel()
		ctx, cancel = context.WithTimeout(r.Context(), requestTimeout)
	}
	defer cancel()
	p := &pendingRequest{replies: make(chan message, 8), failed: make(chan struct{})}
	e.mu.Lock()
	e.next++
	id := e.next
	e.pending[id] = p
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.pending, id)
		e.mu.Unlock()
		// This also cancels completed requests safely; IDs are never reused.
		_ = writeMessage(e.ctx, e.conn, message{Type: "cancel", ID: id})
	}()
	if ctx.Err() != nil {
		return
	}
	if err := writeMessage(e.ctx, e.conn, message{Type: "request", ID: id, Method: r.Method, Path: path, Header: requestHeaders(r.Header), Body: body}); err != nil {
		http.Error(w, "Smart Stage is disconnected", http.StatusBadGateway)
		return
	}
	started := false
	controller := http.NewResponseController(w)
	defer controller.SetWriteDeadline(time.Time{})
	for {
		select {
		case <-ctx.Done():
			if !started {
				http.Error(w, "Smart Stage request timed out", http.StatusGatewayTimeout)
			}
			return
		case <-e.ctx.Done():
			if !started {
				http.Error(w, "Smart Stage is disconnected", http.StatusBadGateway)
			}
			return
		case <-p.failed:
			if !started {
				http.Error(w, "Remote response exceeded buffering limit", http.StatusBadGateway)
			}
			return
		case m := <-p.replies:
			switch m.Type {
			case "response":
				if started || m.Status < 200 || m.Status > 599 {
					return
				}
				for k, values := range responseHeaders(m.Header, e.prefix) {
					w.Header()[k] = values
				}
				_ = controller.SetWriteDeadline(time.Now().Add(writeTimeout))
				w.WriteHeader(m.Status)
				started = true
				if path == "/api/events" {
					_ = http.NewResponseController(w).Flush()
				}
			case "data":
				if !started {
					return
				}
				_ = controller.SetWriteDeadline(time.Now().Add(writeTimeout))
				if _, err := w.Write(m.Body); err != nil {
					return
				}
				if path == "/api/events" {
					if err := http.NewResponseController(w).Flush(); err != nil {
						return
					}
				}
			case "done":
				if !started {
					http.Error(w, "Invalid response from Smart Stage", http.StatusBadGateway)
				}
				return
			}
		}
	}
}
