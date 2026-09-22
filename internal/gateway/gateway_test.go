package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type fixture struct {
	server *Server
	tls    *httptest.Server
	token  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{token: strings.Repeat("ab", 32)}
	f.tls = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.server.ServeHTTP(w, r) }))
	var err error
	f.server, err = NewServer(Config{PublicURL: f.tls.URL + "/smartstage", Token: f.token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.server.Close(); f.tls.Close() })
	return f
}

func (f *fixture) host(t *testing.T, factory func(string) http.Handler) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	connected := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- ServeConnection(ctx, ClientOptions{URL: f.tls.URL + "/smartstage", Token: f.token, Handler: factory, HTTPClient: f.tls.Client(), OnConnect: func(endpoint string) { connected <- endpoint }})
	}()
	t.Cleanup(cancel)
	select {
	case endpoint := <-connected:
		return strings.TrimSuffix(endpoint, "/command"), cancel, done
	case err := <-done:
		t.Fatalf("host registration: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("host registration timed out")
	}
	return "", cancel, done
}

func (f *fixture) request(t *testing.T, method, target string, body io.Reader, headers http.Header) *http.Response {
	t.Helper()
	r, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header = headers.Clone()
	resp, err := f.tls.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func bodyText(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	return string(b)
}

func TestRegistrationAuthAndOrigin(t *testing.T) {
	f := newFixture(t)
	for _, test := range []struct{ name, token, origin string }{{"missing", "", ""}, {"wrong", strings.Repeat("cd", 32), ""}, {"browser origin", f.token, f.tls.URL}} {
		t.Run(test.name, func(t *testing.T) {
			h := make(http.Header)
			if test.token != "" {
				h.Set("Authorization", "Bearer "+test.token)
			}
			if test.origin != "" {
				h.Set("Origin", test.origin)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, resp, err := websocket.Dial(ctx, strings.Replace(f.tls.URL, "https:", "wss:", 1)+"/smartstage/api/connect", &websocket.DialOptions{HTTPClient: f.tls.Client(), HTTPHeader: h})
			if conn != nil {
				conn.CloseNow()
			}
			if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected denied registration; response=%v error=%v", resp, err)
			}
		})
	}
	var disconnected atomic.Bool
	err := ServeConnection(context.Background(), ClientOptions{URL: f.tls.URL + "/smartstage", Token: strings.Repeat("cd", 32), HTTPClient: f.tls.Client(), Handler: func(string) http.Handler { return http.NotFoundHandler() }, OnDisconnect: func(error) { disconnected.Store(true) }})
	if err == nil || !disconnected.Load() {
		t.Fatal("registration failure must notify caller")
	}
}

func TestIsolatedEndpointsAndNarrowForwarding(t *testing.T) {
	f := newFixture(t)
	var calls atomic.Int32
	newHost := func(name string) string {
		endpoint, _, _ := f.host(t, func(prefix string) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Host != strings.TrimPrefix(f.tls.URL, "https://") || r.TLS == nil || r.Header.Get("Authorization") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Admin-Secret") != "" {
					t.Error("untrusted headers or wrong public origin reached host")
				}
				if r.URL.Path == "/api/stop" && r.Header.Get("X-CSRF-Token") != "csrf-test" {
					http.Error(w, "missing CSRF", 403)
					return
				}
				if r.URL.Path == "/api/state" && r.Header.Get("Cookie") != "smartstage_command_session=session" {
					http.Error(w, "pairing required", 401)
					return
				}
				http.SetCookie(w, &http.Cookie{Name: "smartstage_command_session", Value: name, Path: strings.TrimSuffix(prefix, "/"), HttpOnly: true})
				http.SetCookie(w, &http.Cookie{Name: "website", Value: "forbidden", Path: "/"})
				w.Header().Set("Location", "https://elsewhere.invalid")
				io.WriteString(w, name+":"+r.URL.Path)
			})
		})
		return endpoint
	}
	one, two := newHost("one"), newHost("two")
	if one == two {
		t.Fatal("endpoint IDs reused")
	}
	headers := http.Header{"Cookie": {"website=private-site-session; smartstage_command_session=session"}, "Authorization": {"Bearer private"}, "X-Forwarded-For": {"127.0.0.1"}, "X-Admin-Secret": {"private"}}
	for _, test := range []struct{ endpoint, name string }{{one, "one"}, {two, "two"}} {
		r := f.request(t, "GET", test.endpoint+"/api/state", nil, headers)
		if r.StatusCode != 200 || bodyText(t, r) != test.name+":/api/state" {
			t.Fatal("request crossed endpoint")
		}
		cookies := r.Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/smartstage/e/"+strings.TrimPrefix(test.endpoint, f.tls.URL+"/smartstage/e/") {
			t.Fatalf("unsafe endpoint cookies: %v", cookies)
		}
		if r.Header.Get("Location") != "" {
			t.Fatal("unapproved response header forwarded")
		}
	}
	r := f.request(t, "GET", one+"/api/state", nil, nil)
	if r.StatusCode != 401 {
		t.Fatal("phone authentication handler was bypassed")
	}
	before := calls.Load()
	for _, path := range []string{"/admin", "/api/config", "/api/files", "/api/quit", "/api/gateway", "/assets/../admin", "/%61dmin", "/api/state?url=http://127.0.0.1/admin", "//command"} {
		r = f.request(t, "GET", one+path, nil, nil)
		if r.StatusCode != 404 {
			t.Fatalf("path %s not denied: %d", path, r.StatusCode)
		}
	}
	if calls.Load() != before {
		t.Fatal("private route reached host")
	}
	csrfHeaders := make(http.Header)
	csrfHeaders.Set("Origin", f.tls.URL)
	csrfHeaders.Set("X-CSRF-Token", "csrf-test")
	r = f.request(t, "POST", one+"/api/pair", strings.NewReader(`{}`), csrfHeaders)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("pairing failed: %d", r.StatusCode)
	}
	csrfHeaders.Set("Cookie", r.Cookies()[0].String())
	r = f.request(t, "POST", one+"/api/stop", strings.NewReader(`{}`), csrfHeaders)
	if r.StatusCode != 200 {
		t.Fatalf("CSRF header was lost across tunnel: %d", r.StatusCode)
	}
	navigation := http.Header{"Sec-Fetch-Site": {"cross-site"}, "Sec-Fetch-Mode": {"navigate"}, "Sec-Fetch-Dest": {"document"}}
	r = f.request(t, "GET", one+"/command", nil, navigation)
	if r.StatusCode != 200 {
		t.Fatalf("Admin link navigation was blocked: %d", r.StatusCode)
	}
	r = f.request(t, "GET", one+"/api/state", nil, navigation)
	if r.StatusCode != 403 {
		t.Fatalf("cross-site navigation reached state: %d", r.StatusCode)
	}
	for _, headers := range []http.Header{{"Origin": {"https://evil.invalid"}}, {"Sec-Fetch-Site": {"cross-site"}}, {"Origin": {"null"}}} {
		r = f.request(t, "POST", one+"/api/stop", strings.NewReader(`{}`), headers)
		if r.StatusCode != 403 {
			t.Fatal("cross-origin request accepted")
		}
	}
}

func TestSSEStreamsCancellationAndDisconnect(t *testing.T) {
	f := newFixture(t)
	canceled := make(chan struct{}, 2)
	endpoint, stop, done := f.host(t, func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/events" {
				w.Header().Set("Content-Type", "text/event-stream")
				if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Error(err)
				}
				io.WriteString(w, "event: state\ndata: ready\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				canceled <- struct{}{}
				return
			}
			io.WriteString(w, "alive")
		})
	})
	r := f.request(t, "GET", endpoint+"/api/events", nil, nil)
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("SSE headers lost")
	}
	line, err := bufio.NewReader(r.Body).ReadString('\n')
	if err != nil || line != "event: state\n" {
		t.Fatalf("SSE not flushed: %q %v", line, err)
	}
	r.Body.Close()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("browser close did not cancel host SSE")
	}
	r = f.request(t, "GET", endpoint+"/api/state", nil, nil)
	if bodyText(t, r) != "alive" {
		t.Fatal("browser cancellation closed shared tunnel")
	}
	r = f.request(t, "GET", endpoint+"/api/events", nil, nil)
	stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("host did not disconnect")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("host disconnect did not cancel request")
	}
	r.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r = f.request(t, "GET", endpoint+"/api/state", nil, nil)
		r.Body.Close()
		if r.StatusCode == http.StatusGone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stale endpoint remains: %d", r.StatusCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
	newEndpoint, _, _ := f.host(t, func(string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "new") })
	})
	if newEndpoint == endpoint {
		t.Fatal("reconnect reused stale control URL")
	}
}

func TestReservedStopAndRequestLimits(t *testing.T) {
	f := newFixture(t)
	entered := make(chan struct{}, 32)
	endpoint, _, _ := f.host(t, func(prefix string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/pair" {
				http.SetCookie(w, &http.Cookie{Name: commandCookie, Value: "paired-stop", Path: prefix, MaxAge: 3600})
			}
			if r.URL.Path == "/api/state" {
				entered <- struct{}{}
				<-r.Context().Done()
				return
			}
			w.WriteHeader(202)
		})
	})
	paired := f.request(t, "POST", endpoint+"/api/pair", strings.NewReader(`{}`), nil)
	if paired.StatusCode != http.StatusAccepted || len(paired.Cookies()) != 1 {
		t.Fatal("pairing failed")
	}
	pairedHeaders := http.Header{"Cookie": {paired.Cookies()[0].String()}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			r, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/state", nil)
			resp, err := f.tls.Client().Do(r)
			if err == nil {
				resp.Body.Close()
			}
		})
	}
	defer wg.Wait()
	defer cancel()
	for range 32 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("request slots did not fill")
		}
	}
	r := f.request(t, "GET", endpoint+"/command", nil, nil)
	if r.StatusCode != 429 {
		t.Fatalf("ordinary capacity not enforced: %d", r.StatusCode)
	}
	r = f.request(t, "POST", endpoint+"/api/stop", strings.NewReader(`{"requestId":"stop"}`), pairedHeaders)
	if r.StatusCode != 202 {
		t.Fatalf("STOP starved: %d", r.StatusCode)
	}
	r = f.request(t, "POST", endpoint+"/api/emergency-stop", strings.NewReader(strings.Repeat("x", maxBody+1)), pairedHeaders)
	if r.StatusCode != 413 {
		t.Fatalf("body limit not enforced: %d", r.StatusCode)
	}
}

func TestClientIndependentlyRejectsPrivateRoutes(t *testing.T) {
	var calls atomic.Int32
	result := make(chan message, 1)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		writeMessage(ctx, conn, message{Type: "hello", Prefix: "/smartstage/e/" + strings.Repeat("a", 32) + "/"})
		var ready message
		if wsjson.Read(ctx, conn, &ready) != nil {
			return
		}
		writeMessage(ctx, conn, message{Type: "registered"})
		writeMessage(ctx, conn, message{Type: "request", ID: 1, Method: "POST", Path: "/api/quit"})
		var response message
		if wsjson.Read(ctx, conn, &response) == nil {
			result <- response
		}
	}))
	defer upstream.Close()
	err := ServeConnection(context.Background(), ClientOptions{URL: upstream.URL + "/smartstage", Token: strings.Repeat("ab", 32), HTTPClient: upstream.Client(), Handler: func(string) http.Handler {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	}})
	if err == nil {
		t.Fatal("closed upstream not reported")
	}
	select {
	case m := <-result:
		if m.Status != 403 {
			t.Fatalf("private route response: %+v", m)
		}
	default:
		t.Fatal("missing denial")
	}
	if calls.Load() != 0 {
		t.Fatal("malicious gateway reached private handler")
	}
}

func TestConfigurationAndProxyTrust(t *testing.T) {
	for _, u := range []string{"http://example.com/smartstage", "https://u:p@example.com/smartstage", "https://example.com/", "https://example.com/smartstage?x=1", "https://example.com/smartstage#secret", "https://example.com/%73martstage", "https://example.com:99999/smartstage", "https://example.com:0/smartstage", "https://example.com:/smartstage"} {
		if _, err := ValidateURL(u); err == nil {
			t.Errorf("accepted unsafe URL %s", u)
		}
	}
	u, err := ValidateURL("https://example.com/smartstage/")
	if err != nil || u.Path != "/smartstage" {
		t.Fatal("normalization failed")
	}
	u, err = ValidateURL("https://EXAMPLE.com:443/smartstage")
	if err != nil || u.Host != "example.com" {
		t.Fatal("public origin normalization failed")
	}
	if _, err := NewServer(Config{Listen: "0.0.0.0:8790", PublicURL: u.String(), Token: strings.Repeat("ab", 32)}); err == nil {
		t.Fatal("public plaintext listener accepted")
	}
	s, err := NewServer(Config{PublicURL: u.String(), Token: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		peer, host, proto string
		tls, allowed      bool
	}{{"127.0.0.1:2", "example.com", "https", false, true}, {"192.0.2.1:2", "example.com", "https", false, false}, {"127.0.0.1:2", "evil.invalid", "https", false, false}, {"127.0.0.1:2", "example.com", "", false, false}, {"192.0.2.1:2", "example.com", "", true, true}} {
		r := httptest.NewRequest("GET", "https://"+tc.host+"/smartstage/e/id/command", nil)
		r.RemoteAddr = tc.peer
		r.TLS = nil
		if tc.tls {
			r.TLS = &tls.ConnectionState{}
		}
		r.Header.Set("X-Forwarded-Proto", tc.proto)
		if s.secureRequest(r) != tc.allowed {
			t.Errorf("wrong proxy trust: %+v", tc)
		}
	}
}
