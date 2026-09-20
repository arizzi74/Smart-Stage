package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"smartstage/internal/auth"
	"smartstage/internal/web"
)

func TestLifecycleRequiresLocalAdminOriginAndCSRF(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	var quits atomic.Int32
	var chooses atomic.Int32
	api.SetQuit(func() { quits.Add(1) })
	api.SetChooseFiles(func() bool { chooses.Add(1); return true }, func() bool { return true })
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	command.SetQuit(func() { quits.Add(1) })
	command.SetChooseFiles(func() bool { chooses.Add(1); return true }, func() bool { return true })
	badCSRF := admin
	badCSRF.CSRF = "wrong"
	for _, path := range []string{"/api/quit", "/api/admin-presence", "/api/choose-files"} {
		for _, tc := range []struct {
			name    string
			api     *API
			session auth.Session
			origin  string
			want    int
		}{
			{"unauthenticated", api, auth.Session{}, "http://127.0.0.1:8787", 401},
			{"remote", command, remote, "http://127.0.0.1:8787", 403},
			{"missing origin", api, admin, "", 403},
			{"foreign origin", api, admin, "http://evil.example", 403},
			{"bad csrf", api, badCSRF, "http://127.0.0.1:8787", 403},
		} {
			if w := request(tc.api, "POST", path, "{}", tc.session, tc.origin); w.Code != tc.want {
				t.Fatalf("%s %s: got %d, want %d", path, tc.name, w.Code, tc.want)
			}
		}
		for _, body := range []string{"null", "[]", "{", "{\"unexpected\":true}", "{} {}"} {
			if w := request(api, "POST", path, body, admin, "http://127.0.0.1:8787"); w.Code != 400 {
				t.Fatalf("%s accepted %q: %d", path, body, w.Code)
			}
		}
		if w := request(api, "GET", path, "", admin, ""); w.Code != 405 {
			t.Fatalf("%s accepted GET: %d", path, w.Code)
		}
		if w := request(api, "POST", path+"?extra=1", "{}", admin, "http://127.0.0.1:8787"); w.Code != 400 {
			t.Fatalf("%s accepted a query: %d", path, w.Code)
		}
		r := httptest.NewRequest("POST", "http://127.0.0.1:8787"+path, strings.NewReader("{}"))
		r.RemoteAddr = "192.168.1.5:54321"
		r.AddCookie(&http.Cookie{Name: AdminCookie, Value: admin.ID})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://127.0.0.1:8787")
		r.Header.Set("X-CSRF-Token", admin.CSRF)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s accepted a nonloopback peer: %d", path, w.Code)
		}
	}
	if quits.Load() != 0 || chooses.Load() != 0 || api.HasAdminPresence() || command.HasAdminPresence() {
		t.Fatal("Rejected requests changed host lifecycle state")
	}
}

func TestNativeChooserCapabilityAndUnavailableHost(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	var available, accepted atomic.Bool
	var choices atomic.Int32
	api.SetChooseFiles(func() bool { choices.Add(1); return accepted.Load() }, available.Load)
	for _, enabled := range []bool{false, true} {
		available.Store(enabled)
		for _, route := range []struct{ method, path, body string }{
			{"POST", "/api/local-session", "{}"}, {"GET", "/api/state", ""},
		} {
			w := request(api, route.method, route.path, route.body, admin, "http://127.0.0.1:8787")
			var reply struct{ Capabilities map[string]bool }
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Capabilities == nil || reply.Capabilities["chooseFiles"] != enabled {
				t.Fatalf("Wrong native chooser capability: %s", w.Body.String())
			}
		}
		w := request(api, "POST", "/api/choose-files", "{}", admin, "http://127.0.0.1:8787")
		if !enabled && (w.Code != 501 || choices.Load() != 0) {
			t.Fatal("Unavailable native host received a chooser request")
		}
		if enabled && (w.Code != 503 || choices.Load() != 1) {
			t.Fatal("Native queue rejection was not reported")
		}
	}
	accepted.Store(true)
	w := request(api, "POST", "/api/choose-files", "{}", admin, "http://127.0.0.1:8787")
	if w.Code != 202 || !strings.Contains(w.Body.String(), "\"choosing\":true") || choices.Load() != 2 {
		t.Fatal("Native chooser request was not accepted")
	}
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	command.SetChooseFiles(func() bool { return true }, func() bool { return true })
	w = request(command, "GET", "/api/state", "", remote, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "capabilities") {
		t.Fatal("Command received Admin native capabilities")
	}
}

func TestQuitFlushesAcknowledgementBeforeOneShutdownAndBypassesCapacity(t *testing.T) {
	api, authentication, _, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	for i := 0; i < cap(api.ordinary); i++ {
		api.ordinary <- struct{}{}
	}
	var quits atomic.Int32
	w := httptest.NewRecorder()
	api.SetQuit(func() {
		if !w.Flushed || w.Code != 202 || !strings.Contains(w.Body.String(), "\"quitting\":true") {
			t.Error("Shutdown was requested before the complete acknowledgement was flushed")
		}
		quits.Add(1)
	})
	r := httptest.NewRequest("POST", "http://127.0.0.1:8787/api/quit", strings.NewReader("{}"))
	r.RemoteAddr = "127.0.0.1:54321"
	r.AddCookie(&http.Cookie{Name: AdminCookie, Value: admin.ID})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://127.0.0.1:8787")
	r.Header.Set("X-CSRF-Token", admin.CSRF)
	api.ServeHTTP(w, r)
	if quits.Load() != 1 {
		t.Fatal("Quit did not dispatch shutdown")
	}
	var retries sync.WaitGroup
	for range 8 {
		retries.Go(func() {
			if w := request(api, "POST", "/api/quit", "{}", admin, "http://127.0.0.1:8787"); w.Code != 202 {
				t.Errorf("Repeated Quit returned %d", w.Code)
			}
		})
	}
	retries.Wait()
	if quits.Load() != 1 {
		t.Fatal("Repeated Quit dispatched shutdown more than once")
	}
}

func TestQuitWithoutHostCallbackIsUnavailable(t *testing.T) {
	api, authentication, _, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	if w := request(api, "POST", "/api/quit", "{}", admin, "http://127.0.0.1:8787"); w.Code != 503 {
		t.Fatalf("Unexpected missing-host response: %d", w.Code)
	}
}

func TestAdminPresenceNeedsExplicitPageHeartbeatAndExpires(t *testing.T) {
	api, authentication, _, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/local-session", "{}"}, {"GET", "/api/state", ""}, {"GET", "/admin", ""},
	} {
		if w := request(api, route.method, route.path, route.body, admin, "http://127.0.0.1:8787"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if api.HasAdminPresence() {
		t.Fatal("Session/state probes were counted as a live Admin page")
	}
	w := request(api, "POST", "/api/admin-presence", "{}", admin, "http://127.0.0.1:8787")
	var reply map[string]bool
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || !reply["present"] || !api.HasAdminPresence() {
		t.Fatal("Admin page heartbeat was not recorded")
	}
	api.mu.Lock()
	api.adminSeen = time.Now().Add(-11 * time.Second)
	api.mu.Unlock()
	if api.HasAdminPresence() {
		t.Fatal("Stale Admin heartbeat was counted as live")
	}
}

func TestOnlyActiveAdminSSECountsAsPresence(t *testing.T) {
	for _, role := range []string{"admin", "command"} {
		t.Run(role, func(t *testing.T) {
			api, authentication, service, _ := setupAPI(t)
			session, _ := authentication.LocalAdmin("")
			if role == "command" {
				api = NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
				session, _ = authentication.PairCommand(authentication.CommandToken(), "remote")
			}
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(finished)
				api.ServeHTTP(w, r)
			}))
			defer server.Close()
			r, _ := http.NewRequest("GET", server.URL+"/api/events", nil)
			r.Host = "127.0.0.1:8787"
			r.AddCookie(&http.Cookie{Name: api.cookieName, Value: session.ID})
			response, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 200 || api.HasAdminPresence() != (role == "admin") {
				response.Body.Close()
				t.Fatal("Active SSE had incorrect Admin presence")
			}
			response.Body.Close()
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("SSE did not end after disconnect")
			}
			if api.HasAdminPresence() {
				t.Fatal("Disconnected SSE still counted as Admin presence")
			}
		})
	}
}
