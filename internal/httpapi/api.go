package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/model"
	"smartstage/internal/qrcode"
)

const AdminCookie = "smartstage_admin_session"
const CommandCookie = "smartstage_command_session"

type RemoteLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	QRURL string `json:"qrURL"`
}

type API struct {
	app            *app.Service
	auth           *auth.Manager
	assets         http.Handler
	mu             sync.RWMutex
	hosts          map[string]bool
	port           string
	ordinary       chan struct{}
	role           string
	cookieName     string
	remoteLinks    []RemoteLink
	updater        UpdateController
	quit           func()
	quitOnce       sync.Once
	adminSeen      time.Time
	adminEvents    int
	chooseFiles    func() bool
	canChooseFiles func() bool
}

// New defaults to the loopback-only Admin handler. A network controller must use
// NewCommand; no handler combines Admin and Command privileges.
func New(service *app.Service, authentication *auth.Manager, assets http.Handler, hosts []string, port int) *API {
	return NewAdmin(service, authentication, assets, hosts, port)
}

func NewAdmin(service *app.Service, authentication *auth.Manager, assets http.Handler, hosts []string, port int) *API {
	return newAPI(service, authentication, assets, []string{"127.0.0.1"}, port, "admin", AdminCookie)
}

func NewCommand(service *app.Service, authentication *auth.Manager, assets http.Handler, hosts []string, port int) *API {
	return newAPI(service, authentication, assets, hosts, port, "command", CommandCookie)
}

func newAPI(service *app.Service, authentication *auth.Manager, assets http.Handler, hosts []string, port int, role, cookie string) *API {
	a := &API{app: service, auth: authentication, assets: assets, port: strconv.Itoa(port), ordinary: make(chan struct{}, 16), role: role, cookieName: cookie}
	a.SetHosts(hosts)
	return a
}
func (a *API) SetHosts(hosts []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.role == "admin" {
		hosts = []string{"127.0.0.1"}
	}
	a.hosts = map[string]bool{}
	for _, h := range hosts {
		a.hosts[strings.ToLower(h)] = true
	}
}

// SetRemoteLinks copies host-discovered links; they are never included in
// Command state or events. Limit the local discovery list and QR payload size.
func (a *API) SetRemoteLinks(links []RemoteLink) {
	if a.role != "admin" {
		return
	}
	next := make([]RemoteLink, 0, min(len(links), 64))
	for _, link := range links {
		if len(link.URL) > 1024 || len(next) == 64 {
			continue
		}
		link.QRURL = "/api/remote-control/qr?index=" + strconv.Itoa(len(next))
		next = append(next, link)
	}
	a.mu.Lock()
	a.remoteLinks = next
	a.mu.Unlock()
}

func (a *API) trusted(r *http.Request) bool {
	if a.role == "admin" {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(peer).IsLoopback() {
			return false
		}
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
		port = "80"
		if r.TLS != nil {
			port = "443"
		}
	}
	a.mu.RLock()
	allowed := a.hosts[strings.ToLower(host)] && port == a.port
	a.mu.RUnlock()
	if !allowed || strings.ContainsAny(host, "/@\\") {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.User != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
	}
	publicCommandPage := a.role == "command" && (r.Method == http.MethodGet || r.Method == http.MethodHead) && (r.URL.Path == "/command" || r.URL.Path == "/")
	if !publicCommandPage && (r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Sec-Fetch-Site") == "same-site") {
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": app.Error{Code: code, Message: message}})
}
func respondError(w http.ResponseWriter, err error) {
	var e *app.Error
	if !errors.As(err, &e) {
		fail(w, 500, "internal_error", "The operation failed; check host logs and Admin Status")
		return
	}
	status := 400
	switch e.Code {
	case "revision_conflict", "request_conflict", "stale_epoch", "stale_instance", "active_cue", "must_stop", "updating":
		status = 409
	case "cue_not_found":
		status = 404
	case "busy", "overloaded", "unavailable":
		status = 503
	case "save_failed":
		status = 500
	}
	fail(w, status, e.Code, e.Message)
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(w, 415, "content_type", "Use application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		fail(w, 400, "invalid_json", "Malformed, excessive or unknown JSON fields")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "invalid_json", "Exactly one JSON object is required")
		return false
	}
	return true
}
func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if !a.trusted(r) {
		fail(w, 403, "origin_denied", "Host or Origin is not an allowed local address")
		return
	}
	if r.URL.Path == "/licenses.txt" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fail(w, 405, "method", "Use GET or HEAD")
			return
		}
		text := qrcode.Licenses()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(text)))
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, text)
		}
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if a.role == "command" && r.URL.Path == "/" {
			http.Redirect(w, r, "/command", http.StatusSeeOther)
			return
		}
		if a.role == "command" && r.URL.Path == "/admin" || a.role == "admin" && r.URL.Path == "/command" {
			http.NotFound(w, r)
			return
		}
		a.assets.ServeHTTP(w, r)
		return
	}
	if r.URL.RawQuery != "" && r.URL.Path != "/api/files" && r.URL.Path != "/api/remote-control/qr" {
		fail(w, 400, "invalid_request", "Query parameters are not accepted on this route")
		return
	}
	if r.URL.Path == "/api/local-session" {
		if a.role != "admin" {
			fail(w, 404, "unknown_route", "Unknown route")
			return
		}
		if r.Method != http.MethodPost {
			fail(w, 405, "method", "Use POST")
			return
		}
		if r.Header.Get("Origin") == "" {
			fail(w, 403, "origin_required", "Open the local Smart Stage Admin page")
			return
		}
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		existing := ""
		if previous, err := r.Cookie(a.cookieName); err == nil {
			existing = previous.Value
		}
		s, err := a.auth.LocalAdmin(existing)
		if err != nil {
			fail(w, 429, "session_failed", err.Error())
			return
		}
		a.setSession(w, r, s)
		return
	}
	if r.URL.Path == "/api/pair" {
		if a.role != "command" {
			fail(w, 404, "unknown_route", "Unknown route")
			return
		}
		if r.Method != http.MethodPost {
			fail(w, 405, "method", "Use POST")
			return
		}
		// Pairing uses an explicit same-origin header even before a session exists.
		if r.Header.Get("Origin") == "" {
			fail(w, 403, "origin_required", "Pair from the Smart Stage page")
			return
		}
		var body struct {
			Key string `json:"key"`
		}
		if !decode(w, r, &body) {
			return
		}
		if len(body.Key) > 256 {
			fail(w, 400, "invalid_key", "Pairing key is too long")
			return
		}
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		s, err := a.auth.PairCommand(body.Key, remote)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, auth.ErrTooManyAttempts) {
				status = http.StatusTooManyRequests
				w.Header().Set("Retry-After", "60")
			}
			fail(w, status, "pair_failed", err.Error())
			return
		}
		if previous, err := r.Cookie(a.cookieName); err == nil {
			if old, ok := a.auth.Get(previous.Value); ok && old.Role == a.role {
				a.auth.Logout(previous.Value)
			}
		}
		a.setSession(w, r, s)
		return
	}
	cookie, err := r.Cookie(a.cookieName)
	if err != nil {
		fail(w, 401, "unpaired", "Pair this browser session first")
		return
	}
	session, ok := a.auth.Get(cookie.Value)
	if !ok || session.Role != a.role {
		fail(w, 401, "unpaired", "Session expired or was logged out; pair again")
		return
	}
	if r.Method != http.MethodGet && (!auth.CheckCSRF(session, r.Header.Get("X-CSRF-Token")) || r.Header.Get("Origin") == "") {
		fail(w, 403, "csrf_denied", "A valid session CSRF token and same Origin are required")
		return
	}
	admin := session.Role == "admin"
	path := r.URL.Path
	remoteStage := r.Method == http.MethodPost && path == "/api/stage-output"
	if path != "/api/state" && path != "/api/events" && path != "/api/play" && path != "/api/stop" && path != "/api/logout" && !remoteStage && !admin {
		fail(w, 403, "admin_required", "Admin pairing is required")
		return
	}
	// STOP and Quit bypass bounded ordinary-operation slots.
	if path != "/api/stop" && path != "/api/quit" && path != "/api/events" && path != "/api/state" {
		select {
		case a.ordinary <- struct{}{}:
			defer func() { <-a.ordinary }()
		default:
			fail(w, 503, "overloaded", "Server is busy; retry after the current operation")
			return
		}
	}
	key := r.Method + " " + path
	switch key {
	case "POST /api/quit", "POST /api/admin-presence", "POST /api/choose-files":
		a.lifecycleRequest(w, r)
	case "GET /api/update", "POST /api/update/check", "POST /api/update/install":
		a.updateRequest(w, r)
	case "GET /api/remote-control":
		a.mu.RLock()
		links := append([]RemoteLink{}, a.remoteLinks...)
		a.mu.RUnlock()
		writeJSON(w, 200, map[string]any{"token": a.auth.CommandToken(), "links": links})
	case "GET /api/remote-control/qr":
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["index"]) != 1 {
			fail(w, 400, "invalid_index", "Choose one remote control link")
			return
		}
		index, err := strconv.Atoi(query.Get("index"))
		a.mu.RLock()
		link := ""
		if err == nil && index >= 0 && index < len(a.remoteLinks) {
			link = a.remoteLinks[index].URL
		}
		a.mu.RUnlock()
		if link == "" {
			fail(w, 404, "link_not_found", "Remote control link is no longer available")
			return
		}
		png, err := qrcode.PNG(link)
		if err != nil {
			fail(w, 500, "qr_failed", "Could not create the remote control QR code")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	case "GET /api/state":
		reply := map[string]any{"role": session.Role, "csrfToken": session.CSRF, "state": a.app.Snapshot(admin)}
		a.addCapabilities(reply)
		writeJSON(w, 200, reply)
	case "GET /api/events":
		a.events(w, r, session)
	case "POST /api/logout":
		a.auth.Logout(session.ID)
		http.SetCookie(w, &http.Cookie{Name: a.cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: -1})
		writeJSON(w, 200, map[string]bool{"loggedOut": true})
	case "POST /api/play":
		var body app.PlayRequest
		if !decode(w, r, &body) {
			return
		}
		ack, err := a.app.Play(body)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 202, ack)
	case "POST /api/stop":
		var body app.StopRequest
		if !decode(w, r, &body) {
			return
		}
		ack, err := a.app.Stop(body)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 202, ack)
	case "GET /api/playlist":
		writeJSON(w, 200, a.app.Playlist())
	case "PUT /api/playlist":
		var body app.PlaylistEdit
		if !decode(w, r, &body) {
			return
		}
		config, err := a.app.EditPlaylist(body)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, config)
	case "GET /api/files":
		if len(r.URL.RawQuery) > 100000 {
			fail(w, 400, "invalid_path", "Invalid file query")
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			fail(w, 400, "invalid_path", "Invalid file query")
			return
		}
		for key, values := range query {
			if (key != "path" && key != "showHidden") || len(values) != 1 {
				fail(w, 400, "invalid_path", "Use one path and an optional showHidden=true or false")
				return
			}
		}
		showHidden := false
		if values, ok := query["showHidden"]; ok {
			if values[0] != "true" && values[0] != "false" {
				fail(w, 400, "invalid_path", "showHidden must be true or false")
				return
			}
			showHidden = values[0] == "true"
		}
		listing, err := a.app.Browser().BrowseWithHidden(query.Get("path"), showHidden)
		if err != nil {
			fail(w, 400, "browse_failed", err.Error())
			return
		}
		writeJSON(w, 200, listing)
	case "GET /api/devices":
		devices, err := a.app.Devices(r.Context())
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, devices)
	case "POST /api/inspect":
		var body struct {
			Path string `json:"path"`
		}
		if !decode(w, r, &body) {
			return
		}
		writeJSON(w, 200, a.app.InspectFile(r.Context(), body.Path))
	case "PUT /api/outputs":
		var body model.Outputs
		if !decode(w, r, &body) {
			return
		}
		if err := a.app.ConfigureOutputs(r.Context(), body); err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, a.app.Snapshot(true))
	case "POST /api/stage-output":
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := a.app.Stage(r.Context(), body.Enabled); err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 202, a.app.Snapshot(admin))
	case "POST /api/validate":
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		if err := a.app.Validate(); err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 202, map[string]bool{"accepted": true})
	default:
		fail(w, 405, "unknown_route", "Unknown route or unsupported HTTP method")
	}
}
func (a *API) setSession(w http.ResponseWriter, r *http.Request, s auth.Session) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName, Value: s.ID, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: int(auth.Lifetime.Seconds())})
	reply := map[string]any{"role": s.Role, "csrfToken": s.CSRF, "expires": s.Expires}
	a.addCapabilities(reply)
	writeJSON(w, 200, reply)
}

func (a *API) events(w http.ResponseWriter, r *http.Request, session auth.Session) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "stream_unavailable", "Streaming is unavailable")
		return
	}
	updates, unsubscribe, err := a.app.Subscribe()
	if err != nil {
		respondError(w, err)
		return
	}
	defer unsubscribe()
	if session.Role == "admin" {
		a.mu.Lock()
		a.adminEvents++
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			a.adminEvents--
			a.mu.Unlock()
		}()
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-updates:
			if !ok {
				return
			}
			if _, ok := a.auth.Get(session.ID); !ok {
				return
			}
			state := a.app.Snapshot(session.Role == "admin")
			data, _ := json.Marshal(state)
			_ = controller.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if _, err := fmt.Fprintf(w, "id: %s:%d\nevent: state\ndata: %s\n\n", state.InstanceID, state.Revision, data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, ok := a.auth.Get(session.ID); !ok {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(3 * time.Second))
			if _, err := fmt.Fprint(w, "event: heartbeat\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
