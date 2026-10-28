package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/web"
)

const cloudflareTestOrigin = "https://show.trycloudflare.com"

type cloudflareFixture struct {
	api         *API
	server      *httptest.Server
	auth        *auth.Manager
	service     *app.Service
	privatePath string
	cancel      context.CancelFunc
}

func newCloudflareFixture(t *testing.T, wrappers ...func(http.Handler) http.Handler) *cloudflareFixture {
	t.Helper()
	_, _, service, path := setupAPI(t)
	authentication := auth.NewPublicCommand()
	api, err := NewCloudflareCommand(service, authentication, web.Handler(), cloudflareTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var handler http.Handler = api
	for _, wrap := range wrappers {
		handler = wrap(handler)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	server.Start()
	f := &cloudflareFixture{api: api, server: server, auth: authentication, service: service, privatePath: path, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		_ = api.Close()
		server.Close()
	})
	return f
}

func (f *cloudflareFixture) call(t *testing.T, method, path, body, csrf string, cookie *http.Cookie) (*http.Response, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Host = strings.TrimPrefix(cloudflareTestOrigin, "https://")
	if method != http.MethodGet {
		r.Header.Set("Origin", cloudflareTestOrigin)
		r.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, data
}

func (f *cloudflareFixture) pair(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	response, data := f.call(t, http.MethodPost, "/api/pair", `{"key":"`+f.auth.CommandToken()+`"}`, "", nil)
	var reply struct {
		CSRF          string `json:"csrfToken"`
		LiveTransport string `json:"liveTransport"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(data, &reply) != nil || reply.CSRF == "" || reply.LiveTransport != "websocket" {
		t.Fatalf("pair: %d %s", response.StatusCode, data)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatalf("public cookie: %+v", cookies)
	}
	return cookies[0], reply.CSRF
}

func (f *cloudflareFixture) dial(t *testing.T, cookie *http.Cookie, origin, path string, customize ...func(*websocket.DialOptions)) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	if cookie != nil {
		header.Set("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	opts := &websocket.DialOptions{
		Host: strings.TrimPrefix(cloudflareTestOrigin, "https://"), HTTPClient: f.server.Client(), HTTPHeader: header,
	}
	for _, edit := range customize {
		edit(opts)
	}
	conn, response, err := websocket.Dial(ctx, f.server.URL+path, opts)
	if conn != nil {
		t.Cleanup(func() { _ = conn.CloseNow() })
	}
	return conn, response, err
}

func authenticateCloudflare(t *testing.T, conn *websocket.Conn, csrf string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "authenticate", "csrfToken": csrf}); err != nil {
		t.Fatal(err)
	}
}

func readCloudflareState(t *testing.T, conn *websocket.Conn) app.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var message struct {
		Type  string    `json:"type"`
		State app.State `json:"state"`
	}
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatal(err)
	}
	if message.Type != "state" || message.State.InstanceID == "" {
		t.Fatalf("invalid state: %+v", message)
	}
	return message.State
}

func TestCloudflareCommandPairingStatusAndRouteIsolation(t *testing.T) {
	f := newCloudflareFixture(t)
	cookie, csrf := f.pair(t)
	response, data := f.call(t, http.MethodGet, "/api/state", "", "", cookie)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(data), `"liveTransport":"websocket"`) || strings.Contains(string(data), f.privatePath) {
		t.Fatalf("state: %d %s", response.StatusCode, data)
	}
	if !strings.Contains(response.Header.Get("Content-Security-Policy"), "wss://show.trycloudflare.com") {
		t.Fatal("public WebSocket origin missing from CSP")
	}
	conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
	if err != nil {
		t.Fatal(err)
	}
	authenticateCloudflare(t, conn, csrf)
	initial := readCloudflareState(t, conn)
	if initial.InstanceID != f.service.Snapshot(false).InstanceID {
		t.Fatal("initial status is not the current service instance")
	}
	encoded, _ := json.Marshal(initial)
	if strings.Contains(string(encoded), f.privatePath) || strings.Contains(string(encoded), "PRIVATE_PATH") || strings.Contains(string(encoded), f.auth.CommandToken()) {
		t.Fatalf("live command status leaked private data: %s", encoded)
	}
	response, data = f.call(t, http.MethodPost, "/api/stop", `{"requestId":"cloudflare-live-stop"}`, csrf, cookie)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("stop: %d %s", response.StatusCode, data)
	}
	updated := readCloudflareState(t, conn)
	if updated.StopEpoch != initial.StopEpoch+1 || updated.Revision <= initial.Revision {
		t.Fatalf("live update not authoritative: before=%+v after=%+v", initial, updated)
	}
	for _, path := range []string{"/admin", "/api/events", "/api/local-session", "/api/files", "/api/playlist", "/api/gateway", "/api/quit", "/api/update", "/api/remote-control"} {
		response, _ := f.call(t, http.MethodGet, path, "", "", cookie)
		if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusSwitchingProtocols {
			t.Fatalf("public handler exposed %s", path)
		}
	}
	// Commands still require their ordinary HTTP CSRF protection.
	response, _ = f.call(t, http.MethodPost, "/api/stop", `{"requestId":"cloudflare-missing-csrf"}`, "", cookie)
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("Cloudflare command bypassed CSRF")
	}
}

func TestCloudflareWebSocketOriginSessionAndCSRF(t *testing.T) {
	f := newCloudflareFixture(t)
	cookie, csrf := f.pair(t)
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
		origin string
		path   string
		status int
	}{
		{"anonymous", nil, cloudflareTestOrigin, "/api/ws", http.StatusUnauthorized},
		{"forged session", &http.Cookie{Name: CommandCookie, Value: "forged"}, cloudflareTestOrigin, "/api/ws", http.StatusUnauthorized},
		{"missing origin", cookie, "", "/api/ws", http.StatusForbidden},
		{"cross origin", cookie, "https://evil.example", "/api/ws", http.StatusForbidden},
		{"insecure origin", cookie, "http://show.trycloudflare.com", "/api/ws", http.StatusForbidden},
		{"null origin", cookie, "null", "/api/ws", http.StatusForbidden},
		{"query token", cookie, cloudflareTestOrigin, "/api/ws?csrfToken=" + csrf, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn, response, err := f.dial(t, test.cookie, test.origin, test.path)
			if err == nil || conn != nil || response == nil || response.StatusCode != test.status {
				t.Fatalf("upgrade: response=%v err=%v", response, err)
			}
		})
	}
	for _, payload := range []string{
		`{"type":"authenticate","csrfToken":"wrong"}`,
		`{"type":"authenticate"}`,
		`{"type":"authenticate","csrfToken":"` + csrf + `","unexpected":true}`,
		`{"type":"authenticate","csrfToken":"` + csrf + `"} {}`,
		`null`,
	} {
		conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
			cancel()
			t.Fatal(err)
		}
		_, _, err = conn.Read(ctx)
		cancel()
		if websocket.CloseStatus(err) != liveCSRFDenied {
			t.Fatalf("invalid first frame disclosed status or wrong close: %s: %v", payload, err)
		}
	}
}

func TestCloudflareWebSocketLogoutAndShutdown(t *testing.T) {
	for _, closeAPI := range []bool{false, true} {
		t.Run(fmt.Sprintf("close-api=%v", closeAPI), func(t *testing.T) {
			f := newCloudflareFixture(t)
			cookie, csrf := f.pair(t)
			conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
			if err != nil {
				t.Fatal(err)
			}
			authenticateCloudflare(t, conn, csrf)
			readCloudflareState(t, conn)
			if closeAPI {
				if err := f.api.Close(); err != nil {
					t.Fatal(err)
				}
				// Close is idempotent, and a retired tunnel cannot authenticate
				// new requests with its previous browser sessions.
				_ = f.api.Close()
				response, _ := f.call(t, http.MethodGet, "/api/state", "", "", cookie)
				if response.StatusCode != http.StatusServiceUnavailable {
					t.Fatal("closed API retained session authority")
				}
			} else {
				response, _ := f.call(t, http.MethodPost, "/api/logout", `{}`, csrf, cookie)
				if response.StatusCode != http.StatusOK || !response.Cookies()[0].Secure {
					t.Fatal("logout did not clear the secure cookie")
				}
				// As with SSE, a revoked session is checked before each update
				// and heartbeat, without waiting for the session lifetime.
				_, _ = f.service.Stop(app.StopRequest{RequestID: "cloudflare-revoked-session"})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _, err = conn.Read(ctx)
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("socket survived session/shutdown: %v", err)
			}
			if !closeAPI && websocket.CloseStatus(err) != liveSessionExpired {
				t.Fatalf("revocation did not request pairing: %v", err)
			}
		})
	}
	t.Run("server context", func(t *testing.T) {
		f := newCloudflareFixture(t)
		cookie, csrf := f.pair(t)
		conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
		if err != nil {
			t.Fatal(err)
		}
		authenticateCloudflare(t, conn, csrf)
		readCloudflareState(t, conn)
		f.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, _, err := conn.Read(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("BaseContext cancellation retained hijacked socket: %v", err)
		}
	})
}

func TestCloudflareWebSocketBoundsAndStopCapacity(t *testing.T) {
	f := newCloudflareFixture(t)
	cookie, csrf := f.pair(t)
	conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", liveReadLimit+1))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("oversized authentication frame: %v", err)
	}
	// Hold authenticated status sockets at the cap without reading/sending an
	// initial frame. Their short handshake deadlines also apply before CSRF.
	for i := 0; i < maxLiveSockets; i++ {
		if _, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws"); err != nil {
			t.Fatalf("socket %d: %v", i, err)
		}
	}
	if _, response, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws"); err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unbounded sockets: response=%v err=%v", response, err)
	}
	for i := 0; i < cap(f.api.ordinary); i++ {
		f.api.ordinary <- struct{}{}
	}
	response, body := f.call(t, http.MethodPost, "/api/stop", `{"requestId":"cloudflare-stop-at-capacity"}`, csrf, cookie)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("full live/ordinary lanes blocked STOP: %d %s", response.StatusCode, body)
	}
}

func TestCloudflareWebSocketHeartbeat(t *testing.T) {
	t.Parallel()
	f := newCloudflareFixture(t)
	cookie, csrf := f.pair(t)
	conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
	if err != nil {
		t.Fatal(err)
	}
	authenticateCloudflare(t, conn, csrf)
	readCloudflareState(t, conn)
	ctx, cancel := context.WithTimeout(context.Background(), liveHeartbeat+2*liveTimeout)
	defer cancel()
	var message struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &message); err != nil || message.Type != "heartbeat" {
		t.Fatalf("no browser-visible heartbeat after protocol pong: %+v %v", message, err)
	}
}

func TestCloudflareWebSocketDeadlines(t *testing.T) {
	t.Parallel()
	for _, authenticate := range []bool{false, true} {
		t.Run(fmt.Sprintf("authenticate=%v", authenticate), func(t *testing.T) {
			t.Parallel()
			f := newCloudflareFixture(t)
			cookie, csrf := f.pair(t)
			conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws", func(options *websocket.DialOptions) {
				options.OnPingReceived = func(context.Context, []byte) bool { return false }
			})
			if err != nil {
				t.Fatal(err)
			}
			if authenticate {
				authenticateCloudflare(t, conn, csrf)
				readCloudflareState(t, conn)
			}
			ctx, cancel := context.WithTimeout(context.Background(), liveHeartbeat+2*liveTimeout)
			defer cancel()
			if _, _, err := conn.Read(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("unresponsive peer survived authentication/pong timeout: %v", err)
			}
		})
	}
}

type gatedUpgradeWriter struct {
	http.ResponseWriter
	entered chan<- struct{}
	release <-chan struct{}
}

func (w *gatedUpgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	close(w.entered)
	<-w.release
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func TestCloudflareCloseDuringWebSocketUpgrade(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f := newCloudflareFixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&gatedUpgradeWriter{ResponseWriter: w, entered: entered, release: release}, r)
		})
	})
	cookie, _ := f.pair(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, _, err := f.dial(t, cookie, cloudflareTestOrigin, "/api/ws")
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, _, err := conn.Read(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("socket registered after Close remained open: %v", err)
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("upgrade did not reach Hijack")
	}
	_ = f.api.Close()
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close raced with socket registration and leaked a connection")
	}
	f.api.websockets.mu.Lock()
	defer f.api.websockets.mu.Unlock()
	if len(f.api.websockets.connections) != 0 {
		t.Fatal("closed API retained a hijacked socket")
	}
}

func TestCloudflareTrustAndConstructor(t *testing.T) {
	_, _, service, _ := setupAPI(t)
	for _, publicURL := range []string{"http://show.trycloudflare.com", "https://user:secret@show.trycloudflare.com", "https://show.trycloudflare.com/command", "https://show.trycloudflare.com?token=secret", "https://show.trycloudflare.com/#token=secret", "https://show.trycloudflare.com/#", "https://show.trycloudflare.com:", "https://show.trycloudflare.com:0", "https://show.trycloudflare.com:65536", "https://bad_host.trycloudflare.com", "https://show.trycloudflare.com//"} {
		if api, err := NewCloudflareCommand(service, auth.NewPublicCommand(), web.Handler(), publicURL); err == nil {
			_ = api.Close()
			t.Fatalf("accepted invalid public URL %q", publicURL)
		}
	}
	canonical, err := NewCloudflareCommand(service, auth.NewPublicCommand(), web.Handler(), "https://SHOW.trycloudflare.com:0443/")
	if err != nil {
		t.Fatal(err)
	}
	defer canonical.Close()
	if canonical.publicOrigin != cloudflareTestOrigin {
		t.Fatal("public origin did not match browser URL normalization")
	}
	api, err := NewCloudflareCommand(service, auth.NewPublicCommand(), web.Handler(), cloudflareTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	for _, test := range []struct {
		peer   string
		host   string
		origin string
	}{
		{"192.0.2.1:1234", "show.trycloudflare.com", cloudflareTestOrigin},
		{"127.0.0.1:1234", "127.0.0.1", cloudflareTestOrigin},
		{"127.0.0.1:1234", "show.trycloudflare.com.evil.example", cloudflareTestOrigin},
		{"127.0.0.1:1234", "show.trycloudflare.com", "http://show.trycloudflare.com"},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/command", nil)
		r.RemoteAddr = test.peer
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "show.trycloudflare.com")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("proxy headers bypassed trust: %+v: %d", test, w.Code)
		}
	}
	// Gateway/LAN/Admin remain on their original SSE protocol.
	lan := NewCommand(service, auth.New(), web.Handler(), []string{"127.0.0.1"}, 8787)
	session, _ := lan.auth.PairCommand(lan.auth.CommandToken(), "test")
	if reply := request(lan, http.MethodGet, "/api/state", "", session, ""); !strings.Contains(reply.Body.String(), `"liveTransport":"sse"`) {
		t.Fatal("LAN transport changed")
	}
	if reply := request(lan, http.MethodGet, "/api/ws", "", session, ""); reply.Code != http.StatusNotFound {
		t.Fatal("LAN unexpectedly exposed WebSockets")
	}
}
