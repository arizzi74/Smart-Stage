package httpapi

import (
	"testing"

	"smartstage/internal/auth"
	"smartstage/internal/update"
	"smartstage/internal/web"
)

type recordingUpdater struct{ checks, installs int }

func (u *recordingUpdater) Status() update.Status {
	return update.Status{CurrentVersion: "v0.1.0-preview.9", Phase: "idle"}
}
func (u *recordingUpdater) Check() error   { u.checks++; return nil }
func (u *recordingUpdater) Install() error { u.installs++; return nil }

func TestUpdatesRequireLocalAdminAndCSRF(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	updater := &recordingUpdater{}
	api.SetUpdater(updater)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	command.SetUpdater(updater) // even accidental setup cannot grant remote access.
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/update", ""}, {"POST", "/api/update/check", "{}"}, {"POST", "/api/update/install", "{}"},
	} {
		if w := request(api, route.method, route.path, route.body, auth.Session{}, "http://127.0.0.1:8787"); w.Code != 401 {
			t.Fatal("unauthenticated updater access:", w.Code)
		}
		if w := request(command, route.method, route.path, route.body, remote, "http://127.0.0.1:8787"); w.Code != 403 {
			t.Fatal("remote updater access:", w.Code)
		}
	}
	for _, path := range []string{"/api/update/check", "/api/update/install"} {
		for _, origin := range []string{"", "http://attacker.example"} {
			if w := request(api, "POST", path, "{}", admin, origin); w.Code != 403 {
				t.Fatal("update accepted invalid Origin:", w.Code)
			}
		}
		badCSRF := admin
		badCSRF.CSRF = "wrong"
		if w := request(api, "POST", path, "{}", badCSRF, "http://127.0.0.1:8787"); w.Code != 403 {
			t.Fatal("update accepted invalid CSRF:", w.Code)
		}
		for _, body := range []string{`null`, `{"url":"https://attacker.example/binary"}`, `{"version":"v0.0.1"}`, `{"path":"/tmp/program"}`} {
			if w := request(api, "POST", path, body, admin, "http://127.0.0.1:8787"); w.Code != 400 {
				t.Fatal("update accepted browser-controlled installation data:", w.Code)
			}
		}
	}
	if updater.checks != 0 || updater.installs != 0 {
		t.Fatal("rejected requests triggered updater actions")
	}
	if w := request(api, "POST", "/api/update/check", "{}", admin, "http://127.0.0.1:8787"); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(api, "POST", "/api/update/install", "{}", admin, "http://127.0.0.1:8787"); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if updater.checks != 1 || updater.installs != 1 {
		t.Fatal("authorized update action was not dispatched exactly once")
	}
}
