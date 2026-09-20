package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/model"
	"smartstage/internal/playback"
	"smartstage/internal/web"
)

type noRender struct{ events chan playback.Event }

func (n *noRender) Devices(context.Context) (playback.Devices, error) { return playback.Devices{}, nil }
func (n *noRender) Inspect(context.Context, string) (playback.Media, error) {
	return playback.Media{Kind: "audio"}, nil
}
func (n *noRender) Start(playback.Start) error       { return nil }
func (n *noRender) Stop(uint64) error                { return nil }
func (n *noRender) Stage(uint64, string, bool) error { return nil }
func (n *noRender) Events() <-chan playback.Event    { return n.events }
func (n *noRender) Close() error                     { return nil }

type discard struct{}

func (discard) Save(model.Config) error { return nil }

func setupAPI(t *testing.T) (*API, *auth.Manager, *app.Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "PRIVATE_PATH.wav")
	if err := os.WriteFile(path, []byte("unit-test"), 0600); err != nil {
		t.Fatal(err)
	}
	fs, err := files.New([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	cfg := model.DefaultConfig()
	cfg.Cues = []model.Cue{{ID: "cue-a", Label: "<img src=x onerror=alert(1)>", Path: path}}
	s := app.New(&noRender{make(chan playback.Event, 16)}, fs, discard{}, cfg)
	t.Cleanup(s.Close)
	authn := auth.New()
	api := New(s, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	return api, authn, s, path
}
func request(api *API, method, path, body string, session auth.Session, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:43210"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if session.ID != "" {
		r.AddCookie(&http.Cookie{Name: api.cookieName, Value: session.ID})
		r.Header.Set("X-CSRF-Token", session.CSRF)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	return w
}
func TestRoleBoundaryAndPathRedaction(t *testing.T) {
	api, authn, _, path := setupAPI(t)
	admin, _ := authn.LocalAdmin("")
	command, _ := authn.PairCommand(authn.CommandToken(), "two")
	commandAPI := NewCommand(api.app, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	for _, route := range []string{"/api/playlist", "/api/files", "/api/devices"} {
		if w := request(commandAPI, "GET", route, "", command, ""); w.Code != 403 {
			t.Fatalf("command accessed %s: %d", route, w.Code)
		}
		if w := request(api, "GET", route, "", admin, ""); w.Code != 200 {
			t.Fatalf("admin denied %s: %d %s", route, w.Code, w.Body.String())
		}
	}
	w := request(commandAPI, "GET", "/api/state", "", command, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), path) || strings.Contains(w.Body.String(), "PRIVATE_PATH") {
		t.Fatalf("controller path leak: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "<img") {
		t.Fatal("JSON did not escape hostile label")
	}
	var snapshot map[string]any
	if json.Unmarshal(w.Body.Bytes(), &snapshot) != nil {
		t.Fatal("invalid state contract")
	}
}
func TestHostOriginCSRFAndMalformedBodies(t *testing.T) {
	api, authn, _, _ := setupAPI(t)
	session, _ := authn.LocalAdmin("")
	for _, origin := range []string{"https://evil.test", "http://127.0.0.1:1234", "null", ""} {
		w := request(api, "POST", "/api/stop", `{"requestId":"stop-request"}`, session, origin)
		if w.Code != 403 {
			t.Fatalf("mutation accepted origin %q: %d", origin, w.Code)
		}
	}
	bad := session
	bad.CSRF = "wrong"
	if w := request(api, "POST", "/api/stop", `{"requestId":"stop-request"}`, bad, "http://127.0.0.1:8787"); w.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	r := httptest.NewRequest("GET", "http://attacker.test:8787/api/state", nil)
	r.AddCookie(&http.Cookie{Name: api.cookieName, Value: session.ID})
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("host validation bypass")
	}
	for _, body := range []string{`{`, `{"requestId":"valid-request","unexpected":true}`, `{"requestId":"valid-request"} {}`, `null`, fmt.Sprintf(`{"requestId":"%s"}`, strings.Repeat("a", 1024*1024))} {
		if w := request(api, "POST", "/api/stop", body, session, "http://127.0.0.1:8787"); w.Code != 400 {
			t.Fatalf("malformed request %d bytes: %d", len(body), w.Code)
		}
	}
}
func TestPairCookieSecurityLogoutAndNoMediaEndpoint(t *testing.T) {
	api, authn, _, _ := setupAPI(t)
	api = NewCommand(api.app, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	body, _ := json.Marshal(map[string]string{"key": authn.CommandToken()})
	w := request(api, "POST", "/api/pair", string(body), auth.Session{}, "http://127.0.0.1:8787")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Secure {
		t.Fatalf("bad LAN cookie: %+v", cookies)
	}
	s, ok := authn.Get(cookies[0].Value)
	if !ok {
		t.Fatal("cookie does not name a session")
	}
	if w = request(api, "POST", "/api/logout", "{}", s, "http://127.0.0.1:8787"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request(api, "GET", "/api/state", "", s, ""); w.Code != 401 {
		t.Fatal("logged out session accepted")
	}
	for _, route := range []string{"/media/file.mp4", "/testdata/media/tone.mp3", "/assets/../../SMART_STAGE_CODEX_PROMPT.md"} {
		if w := request(api, "GET", route, "", s, ""); w.Code != 404 {
			t.Fatalf("unexpected download endpoint %s: %d", route, w.Code)
		}
	}
	w = request(api, "GET", "/command", "", auth.Session{}, "")
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("security headers missing")
	}
	if strings.Contains(w.Body.String(), "<video") || strings.Contains(w.Body.String(), "<audio") {
		t.Fatal("browser playback elements present")
	}
}
func TestStopBypassesOrdinaryCapacityAndDuplicateRetry(t *testing.T) {
	api, authn, s, _ := setupAPI(t)
	api = NewCommand(api.app, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	session, _ := authn.PairCommand(authn.CommandToken(), "controller")
	for i := 0; i < cap(api.ordinary); i++ {
		api.ordinary <- struct{}{}
	}
	body := `{"requestId":"stop-overload"}`
	w := request(api, "POST", "/api/stop", body, session, "http://127.0.0.1:8787")
	if w.Code != 202 {
		t.Fatalf("STOP blocked by regular operations: %d", w.Code)
	}
	epoch := s.Snapshot(false).StopEpoch
	w = request(api, "POST", "/api/stop", body, session, "http://127.0.0.1:8787")
	if w.Code != 202 || s.Snapshot(false).StopEpoch != epoch || !strings.Contains(w.Body.String(), `"duplicate":true`) {
		t.Fatal("HTTP retry reapplied STOP")
	}
}
func TestSSEAuthoritativeInitialStateAndReconnect(t *testing.T) {
	api, authn, s, _ := setupAPI(t)
	api = NewCommand(api.app, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	session, _ := authn.PairCommand(authn.CommandToken(), "controller")
	// Keep production Host validation active through an actual HTTP stream.
	server := httptest.NewServer(api)
	defer server.Close()
	for i := 0; i < 2; i++ {
		r, _ := http.NewRequest("GET", server.URL+"/api/events", nil)
		r.Host = "127.0.0.1:8787"
		r.AddCookie(&http.Cookie{Name: api.cookieName, Value: session.ID})
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			t.Fatalf("SSE status %d", response.StatusCode)
		}
		data := make([]byte, 4096)
		n, err := response.Body.Read(data)
		response.Body.Close()
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		if !bytes.Contains(data[:n], []byte("event: state")) || !bytes.Contains(data[:n], []byte(s.Snapshot(false).InstanceID)) {
			t.Fatalf("missing snapshot: %s", data[:n])
		}
		_, _ = s.Stop(app.StopRequest{RequestID: fmt.Sprintf("between-connect-%d", i)})
	}
}
