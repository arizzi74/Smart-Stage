package httpapi

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	qr "github.com/piglig/go-qr"

	"smartstage/internal/auth"
	"smartstage/internal/web"
)

// Exercise the handlers through real HTTP sockets and real Set-Cookie headers.
func startAPI(t *testing.T, api *API) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(api)
	_, port, _ := net.SplitHostPort(s.Listener.Addr().String())
	api.port = port
	t.Cleanup(s.Close)
	return s
}

func httpCall(t *testing.T, server *httptest.Server, method, path, body, origin string, cookies ...*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	response, err := server.Client().Do(r)
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

func TestSeparateHTTPListenersSessionsAndPrivatePairingQR(t *testing.T) {
	admin, authn, service, privatePath := setupAPI(t)
	command := NewCommand(service, authn, web.Handler(), []string{"127.0.0.1"}, 8788)
	localServer := startAPI(t, admin)
	remoteServer := startAPI(t, command)
	link := "http://192.168.3.7:8788/command#token=" + authn.CommandToken()
	admin.SetRemoteLinks([]RemoteLink{{Label: "Wi-Fi", URL: link}})
	for _, server := range []*httptest.Server{localServer, remoteServer} {
		response, notice := httpCall(t, server, "GET", "/licenses.txt", "", "")
		if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/plain; charset=utf-8" || !bytes.Contains(notice, []byte("Copyright (c) 2023 piglig")) || !bytes.Contains(notice, []byte("THE SOFTWARE IS PROVIDED")) || bytes.Contains(notice, []byte(authn.CommandToken())) {
			t.Fatalf("embedded license unavailable or discloses pairing data: %d %s", response.StatusCode, notice)
		}
		head, body := httpCall(t, server, "HEAD", "/licenses.txt", "", "")
		if head.StatusCode != 200 || len(body) != 0 || head.ContentLength != int64(len(notice)) {
			t.Fatal("license HEAD did not describe the embedded document")
		}
	}

	for _, origin := range []string{"", "null", remoteServer.URL, "http://evil.example"} {
		response, _ := httpCall(t, localServer, "POST", "/api/local-session", "{}", origin)
		if response.StatusCode != 403 {
			t.Fatalf("local session accepted origin %q: %d", origin, response.StatusCode)
		}
	}
	response, data := httpCall(t, localServer, "POST", "/api/local-session", "{}", localServer.URL)
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"role":"admin"`)) {
		t.Fatalf("local session: %d %s", response.StatusCode, data)
	}
	adminCookie := response.Cookies()[0]
	if adminCookie.Name != AdminCookie || !adminCookie.HttpOnly || adminCookie.SameSite != http.SameSiteStrictMode || adminCookie.Domain != "" {
		t.Fatalf("Admin cookie scope = %+v", adminCookie)
	}
	response, _ = httpCall(t, localServer, "POST", "/api/local-session", "{}", localServer.URL, adminCookie)
	if response.StatusCode != 200 || response.Cookies()[0].Value != adminCookie.Value {
		t.Fatal("Admin reload did not reuse session")
	}
	response, _ = httpCall(t, localServer, "POST", "/api/pair", `{"key":"`+authn.CommandToken()+`"}`, localServer.URL)
	if response.StatusCode != 404 {
		t.Fatal("local Admin listener exposed remote pairing")
	}
	response, _ = httpCall(t, remoteServer, "POST", "/api/local-session", "{}", remoteServer.URL)
	if response.StatusCode != 404 {
		t.Fatal("Command listener exposed local auto-login")
	}
	response, _ = httpCall(t, remoteServer, "GET", "/admin", "", "")
	if response.StatusCode != 404 {
		t.Fatal("Command listener served Admin page")
	}

	// Cookie names do not share authority across ports, and deliberately copying
	// the stronger session under the other cookie name must also be rejected.
	for _, cookie := range []*http.Cookie{adminCookie, {Name: CommandCookie, Value: adminCookie.Value}} {
		response, data = httpCall(t, remoteServer, "GET", "/api/state", "", "", cookie)
		if response.StatusCode != 401 || bytes.Contains(data, []byte(privatePath)) {
			t.Fatal("Admin session leaked authority to Command")
		}
	}
	oldAdminKey, _ := authn.Keys()
	response, _ = httpCall(t, remoteServer, "POST", "/api/pair", `{"key":"`+oldAdminKey+`"}`, remoteServer.URL)
	if response.StatusCode != 401 {
		t.Fatal("Command listener accepted an Admin credential")
	}
	response, data = httpCall(t, remoteServer, "POST", "/api/pair", `{"key":"`+authn.CommandToken()+`"}`, remoteServer.URL)
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"role":"command"`)) {
		t.Fatalf("Command pair: %d %s", response.StatusCode, data)
	}
	commandCookie := response.Cookies()[0]
	if commandCookie.Name != CommandCookie || commandCookie.Name == adminCookie.Name {
		t.Fatal("listeners share cookies")
	}
	response, _ = httpCall(t, localServer, "GET", "/api/state", "", "", &http.Cookie{Name: AdminCookie, Value: commandCookie.Value})
	if response.StatusCode != 401 {
		t.Fatal("Command session gained Admin authority")
	}
	for _, route := range []string{"/api/remote-control", "/api/remote-control/qr?index=0", "/api/files", "/api/playlist", "/api/devices"} {
		response, data = httpCall(t, remoteServer, "GET", route, "", "", commandCookie, adminCookie)
		if response.StatusCode != 403 || bytes.Contains(data, []byte(authn.CommandToken())) || bytes.Contains(data, []byte(privatePath)) {
			t.Fatalf("Command disclosed Admin-only route %s: %d %s", route, response.StatusCode, data)
		}
	}
	response, data = httpCall(t, remoteServer, "GET", "/api/state", "", "", commandCookie)
	if response.StatusCode != 200 || bytes.Contains(data, []byte(privatePath)) || bytes.Contains(data, []byte(authn.CommandToken())) || bytes.Contains(data, []byte(link)) {
		t.Fatalf("Command state discloses local information: %s", data)
	}
	response, _ = httpCall(t, localServer, "GET", "/api/remote-control", "", "")
	if response.StatusCode != 401 {
		t.Fatal("pairing code disclosed without Admin session")
	}
	response, data = httpCall(t, localServer, "GET", "/api/remote-control", "", "", adminCookie)
	var payload struct {
		Token string       `json:"token"`
		Links []RemoteLink `json:"links"`
	}
	if response.StatusCode != 200 || json.Unmarshal(data, &payload) != nil || payload.Token != authn.CommandToken() || len(payload.Links) != 1 || payload.Links[0].URL != link {
		t.Fatalf("local remote-control contract: %d %s", response.StatusCode, data)
	}
	response, data = httpCall(t, localServer, "GET", payload.Links[0].QRURL, "", "", adminCookie)
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/png" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("QR headers missing")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := qr.Decode(img)
	if err != nil || decoded != link {
		t.Fatalf("camera would open %q (%v), want %q", decoded, err, link)
	}
	for _, query := range []string{"", "index=-1", "index=1", "index=0&index=0", "index=0&extra=1", "index=no", "index=%zz"} {
		response, _ = httpCall(t, localServer, "GET", "/api/remote-control/qr?"+query, "", "", adminCookie)
		if response.StatusCode == 200 {
			t.Fatalf("invalid QR index accepted: %q", query)
		}
	}
	admin.SetRemoteLinks(nil)
	response, data = httpCall(t, localServer, "GET", "/api/remote-control", "", "", adminCookie)
	if response.StatusCode != 200 || !bytes.Contains(data, []byte(`"links":[]`)) {
		t.Fatal("removed network links were retained")
	}
}

func TestAdminRejectsActualNetworkPeerEvenWithLoopbackHeaders(t *testing.T) {
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var address string
	for _, addr := range addresses {
		ip, _, err := net.ParseCIDR(addr.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			address = ip.String()
			break
		}
	}
	if address == "" {
		t.Skip("no non-loopback IPv4 interface available for actual-peer check")
	}
	api, _, _, _ := setupAPI(t)
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(api)
	server.Listener = listener
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	api.port = port
	api.SetHosts([]string{address, "127.0.0.1"}) // Admin must ignore a widened host list.
	server.Start()
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, path := range []string{"/admin", "/api/local-session", "/api/remote-control"} {
		r, _ := http.NewRequest("POST", "http://"+net.JoinHostPort(address, port)+path, strings.NewReader("{}"))
		r.Host = net.JoinHostPort("127.0.0.1", port)
		r.Header.Set("Origin", "http://"+r.Host)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		r.Header.Set("X-Real-IP", "127.0.0.1")
		r.Header.Set("Forwarded", "for=127.0.0.1;host="+r.Host)
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("network peer reached %s despite spoofed loopback headers: %d", path, response.StatusCode)
		}
	}
}

func TestCommandNavigationAndAPIGuessLimits(t *testing.T) {
	api, authn, service, _ := setupAPI(t)
	api = NewCommand(service, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	for _, site := range []string{"same-site", "cross-site"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:8787/command", nil)
		r.Header.Set("Sec-Fetch-Site", site)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("Admin-to-Command navigation denied: %d", w.Code)
		}
		r.URL.Path = "/api/pair"
		r.Method = "POST"
		w = httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("API inherited public navigation exception")
		}
	}
	for i := 0; i < auth.AttemptsPerIP; i++ {
		w := request(api, "POST", "/api/pair", `{"key":"wrong"}`, auth.Session{}, "http://127.0.0.1:8787")
		if w.Code != 401 {
			t.Fatalf("guess %d: %d", i, w.Code)
		}
	}
	w := request(api, "POST", "/api/pair", `{"key":"`+authn.CommandToken()+`"}`, auth.Session{}, "http://127.0.0.1:8787")
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("pair guesses were not throttled")
	}
	// The exhausted pairing budget must not interfere with an existing STOP.
	session, err := authn.PairCommand(authn.CommandToken(), "other-phone")
	if err != nil {
		t.Fatal(err)
	}
	w = request(api, "POST", "/api/stop", `{"requestId":"stop-after-pair-throttle"}`, session, "http://127.0.0.1:8787")
	if w.Code != 202 {
		t.Fatalf("pairing throttle blocked STOP: %d %s", w.Code, w.Body.String())
	}

	// No alternate loopback spelling or local DNS name broadens Admin's Host.
	admin := NewAdmin(service, authn, web.Handler(), []string{"localhost", "192.0.2.1"}, 8787)
	for _, host := range []string{"localhost", "127.0.0.2", "192.0.2.1"} {
		r := httptest.NewRequest("GET", (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(8787)), Path: "/admin"}).String(), nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		admin.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("Admin accepted alternate Host %s", host)
		}
	}
}
