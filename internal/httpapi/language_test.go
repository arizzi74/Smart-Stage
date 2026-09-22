package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"smartstage/internal/auth"
	"smartstage/internal/locale"
	"smartstage/internal/web"
)

func TestLanguagePreferencesAdminOnlyAndIndependentOfShow(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	m, err := locale.New(t.TempDir(), "it", nil)
	if err != nil {
		t.Fatal(err)
	}
	api.SetLanguage(m)
	admin, _ := authentication.LocalAdmin("")
	origin := "http://127.0.0.1:8787"
	before := service.Snapshot(true)
	for _, route := range []string{"/api/state", "/api/local-session", "/api/admin-presence"} {
		method, body := "GET", ""
		if route != "/api/state" {
			method, body = "POST", "{}"
		}
		w := request(api, method, route, body, admin, origin)
		var response struct {
			Language locale.Snapshot `json:"language"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Language != m.Snapshot() {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
	}
	w := request(api, "PUT", "/api/language", `{"mode":"en"}`, admin, origin)
	if w.Code != 200 || m.Snapshot().Effective != "en" {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(before, service.Snapshot(true)) {
		t.Fatal("changing language mutated show state")
	}
	w = request(api, "GET", "/api/language", "", admin, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"en"`) {
		t.Fatal(w.Body.String())
	}
	remote := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	remote.SetLanguage(m) // must ignore accidental host wiring to a remote listener.
	session, _ := authentication.PairCommand(authentication.CommandToken(), "phone")
	for _, method := range []string{"GET", "PUT"} {
		w = request(remote, method, "/api/language", `{"mode":"it"}`, session, origin)
		if w.Code != 403 {
			t.Fatalf("remote %s accepted: %d", method, w.Code)
		}
	}
	w = request(remote, "GET", "/api/state", "", session, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), `"language"`) {
		t.Fatalf("remote language preference leak: %s", w.Body.String())
	}
}

func TestLanguageAuthenticationAndInputValidation(t *testing.T) {
	api, authentication, _, _ := setupAPI(t)
	m, _ := locale.New(t.TempDir(), "it", nil)
	api.SetLanguage(m)
	admin, _ := authentication.LocalAdmin("")
	origin := "http://127.0.0.1:8787"
	for _, method := range []string{"GET", "PUT"} {
		if w := request(api, method, "/api/language", `{"mode":"en"}`, auth.Session{}, origin); w.Code != 401 {
			t.Fatalf("unauthenticated: %d", w.Code)
		}
	}
	for _, badOrigin := range []string{"", "https://evil.example", "http://127.0.0.1:8788"} {
		if w := request(api, "PUT", "/api/language", `{"mode":"en"}`, admin, badOrigin); w.Code != 403 {
			t.Fatalf("origin %q: %d", badOrigin, w.Code)
		}
	}
	bad := admin
	bad.CSRF = "wrong"
	if w := request(api, "PUT", "/api/language", `{"mode":"en"}`, bad, origin); w.Code != 403 {
		t.Fatalf("csrf: %d", w.Code)
	}
	for _, body := range []string{`null`, `{}`, `{"mode":"fr"}`, `{"mode":"en","extra":true}`, `{"mode":"en"} {}`} {
		if w := request(api, "PUT", "/api/language", body, admin, origin); w.Code != 400 {
			t.Fatalf("body %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	if m.Snapshot().Effective != "it" {
		t.Fatal("invalid request changed preference")
	}
}
