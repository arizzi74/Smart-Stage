package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"smartstage/internal/auth"
	"smartstage/internal/gateway"
	"smartstage/internal/remote"
	"smartstage/internal/web"
)

func TestPublicGatewayActualCommandAPI(t *testing.T) {
	admin, authentication, service, privatePath := setupAPI(t)
	var relay *gateway.Server
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { relay.ServeHTTP(w, r) }))
	defer tlsServer.Close()
	registrationToken, _ := gateway.NewToken()
	var err error
	relay, err = gateway.NewServer(gateway.Config{Listen: "127.0.0.1:8790", PublicURL: tlsServer.URL + "/smartstage", Token: registrationToken})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	dir := t.TempDir()
	var disables, restarts atomic.Int32
	manager, err := remote.New(context.Background(), remote.Settings{Mode: "gateway"}, remote.Options{ConfigDir: dir, HTTPClient: tlsServer.Client(), DisableLAN: func() error { disables.Add(1); return nil },
		Handler: func(public, prefix string, a *auth.Manager) http.Handler {
			handler, e := NewGatewayCommand(service, a, web.Handler(), public, prefix)
			if e != nil {
				t.Error(e)
			}
			return handler
		},
		Changed: func(mode, link, token string) {
			var links []RemoteLink
			if link != "" {
				links = []RemoteLink{{Label: "Public", URL: link}}
			}
			admin.SetRemoteControl(links, token, mode)
		},
		ReserveRestart: func() (func(), func(), error) { return func() { restarts.Add(1) }, func() {}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	admin.SetGateway(manager)
	adminSession, _ := authentication.LocalAdmin("")
	if manager.Status().Status != "unconfigured" || disables.Load() != 1 {
		t.Fatal("fresh installation did not disable LAN")
	}
	body, _ := json.Marshal(remote.Edit{Mode: "gateway", URL: tlsServer.URL + "/smartstage/", Token: registrationToken})
	w := request(admin, "PUT", "/api/gateway", string(body), adminSession, "http://127.0.0.1:8787")
	if w.Code != 200 || strings.Contains(w.Body.String(), registrationToken) {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for manager.Status().Status != "connected" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	status := manager.Status()
	if status.Status != "connected" {
		t.Fatalf("not connected: %+v", status)
	}
	link, _ := url.Parse(status.RemoteURL)
	phoneToken := strings.TrimPrefix(link.Fragment, "token=")
	if len(phoneToken) != 64 || phoneToken == registrationToken || phoneToken == authentication.CommandToken() {
		t.Fatal("public control must have an independent high-entropy token")
	}
	prefix := strings.TrimSuffix(link.Path, "command")
	call := func(method, path, body, csrf string, cookie *http.Cookie) (*http.Response, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, tlsServer.URL+path, strings.NewReader(body))
		if method == "POST" {
			r.Header.Set("Origin", tlsServer.URL)
			r.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		response, e := tlsServer.Client().Do(r)
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
		return response, data
	}
	response, data := call("GET", prefix+"command", "", "", nil)
	if response.StatusCode != 200 || !strings.Contains(string(data), "./assets/app.js") {
		t.Fatalf("page: %d", response.StatusCode)
	}
	for _, asset := range []string{"app.js", "style.css", "wake-lock.js"} {
		response, data = call("GET", prefix+"assets/"+asset, "", "", nil)
		if response.StatusCode != 200 {
			t.Fatalf("asset %s %d", asset, response.StatusCode)
		}
		if asset == "app.js" && !strings.Contains(string(data), "window.smartStageI18n") {
			t.Fatal("translations must be bundled through the existing gateway asset allowlist")
		}
	}
	response, _ = call("POST", prefix+"api/pair", `{"key":"`+registrationToken+`"}`, "", nil)
	if response.StatusCode != 401 {
		t.Fatalf("registration token paired browser: %d", response.StatusCode)
	}
	response, data = call("POST", prefix+"api/pair", `{"key":"`+phoneToken+`"}`, "", nil)
	if response.StatusCode != 200 {
		t.Fatalf("pair %d %s", response.StatusCode, data)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Path != prefix || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie %+v", cookie)
	}
	var paired struct {
		CSRF string `json:"csrfToken"`
	}
	json.Unmarshal(data, &paired)
	if paired.CSRF == "" {
		t.Fatal("missing CSRF")
	}
	response, data = call("GET", prefix+"api/state", "", "", cookie)
	if response.StatusCode != 200 || strings.Contains(string(data), privatePath) || strings.Contains(string(data), registrationToken) {
		t.Fatalf("remote state %d %s", response.StatusCode, data)
	}
	for _, blocked := range []string{"admin", "api/files", "api/playlist", "api/gateway", "api/local-session", "api/quit", "api/update", "api/remote-control", "../admin"} {
		response, _ = call("GET", prefix+blocked, "", "", cookie)
		if response.StatusCode == 200 {
			t.Fatal("public access to " + blocked)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", tlsServer.URL+prefix+"api/events", nil)
	r.AddCookie(cookie)
	events, err := tlsServer.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(events.Body)
	found := false
	for i := 0; i < 5; i++ {
		line, e := reader.ReadString('\n')
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(line, "data: ") {
			found = true
			if strings.Contains(line, privatePath) {
				t.Fatal("SSE leaked host path")
			}
			break
		}
	}
	if !found {
		t.Fatal("SSE did not flush state")
	}
	events.Body.Close()
	response, data = call("POST", prefix+"api/logout", `{}`, paired.CSRF, cookie)
	if response.StatusCode != 200 || response.Cookies()[0].Path != prefix {
		t.Fatalf("logout %d %s", response.StatusCode, data)
	}
	response, _ = call("GET", prefix+"api/state", "", "", cookie)
	if response.StatusCode != 401 {
		t.Fatal("logout retained session")
	}
	w = request(admin, "GET", "/api/gateway", "", adminSession, "")
	if strings.Contains(w.Body.String(), registrationToken) {
		t.Fatal("registration secret echoed")
	}
	w = request(admin, "GET", "/api/remote-control/qr?index=0", "", adminSession, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("QR %d", w.Code)
	}
	oldURL := status.RemoteURL
	if _, err := manager.Reconnect(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for (manager.Status().Status != "connected" || manager.Status().RemoteURL == oldURL) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if manager.Status().RemoteURL == oldURL || manager.Status().RemoteURL == "" {
		t.Fatal("reconnect did not rotate endpoint and pairing")
	}
	response, _ = call("GET", prefix+"command", "", "", nil)
	if response.StatusCode != 410 {
		t.Fatalf("old endpoint remains live %d", response.StatusCode)
	}
	saved, err := remote.Load(dir)
	if err != nil || saved.Token != registrationToken {
		t.Fatalf("saved settings %v", err)
	}
	info, _ := os.Stat(filepath.Join(dir, "gateway.json"))
	if info.Mode().Perm()&0077 != 0 && os.PathSeparator != '\\' {
		t.Fatal("credentials readable by other users")
	}
	w = request(admin, "PUT", "/api/gateway", `{"mode":"lan"}`, adminSession, "http://127.0.0.1:8787")
	if w.Code != 200 || restarts.Load() != 1 || !manager.Status().Restart || manager.Status().RemoteURL != "" {
		t.Fatalf("LAN transition: %d %s", w.Code, w.Body)
	}
}

func TestGatewaySettingsRequireLocalAdminOriginAndCSRF(t *testing.T) {
	admin, a, s, _ := setupAPI(t)
	command := NewCommand(s, a, web.Handler(), []string{"127.0.0.1"}, 8787)
	remoteSession, _ := a.PairCommand(a.CommandToken(), "peer")
	for _, p := range []string{"/api/gateway", "/api/gateway/reconnect"} {
		if w := request(command, "POST", p, `{}`, remoteSession, "http://127.0.0.1:8787"); w.Code != 403 {
			t.Fatalf("remote authorized %d", w.Code)
		}
	}
	adminSession, _ := a.LocalAdmin("")
	for _, origin := range []string{"", "https://evil.example"} {
		if w := request(admin, "PUT", "/api/gateway", `{"mode":"lan"}`, adminSession, origin); w.Code != 403 {
			t.Fatalf("untrusted mutation %d", w.Code)
		}
	}
}
