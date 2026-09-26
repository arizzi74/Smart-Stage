package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/platform"
	"smartstage/internal/playlistfile"
	"smartstage/internal/web"
)

func TestPlaylistFileHTTPRoundTripAndStrictImport(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	origin := "http://127.0.0.1:8787"
	exported := request(api, "GET", "/api/playlist/export", "", admin, "")
	if exported.Code != 200 || exported.Header().Get("Content-Disposition") != `attachment; filename="Playlist.smartstage.json"` {
		t.Fatal(exported.Code, exported.Body.String())
	}
	var file app.PlaylistFile
	if err := json.Unmarshal(exported.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{`"outputs"`, `"cache"`, `"token"`, `"playlistRevision"`} {
		if strings.Contains(exported.Body.String(), secret) {
			t.Fatal("Export included private or derived configuration:", secret)
		}
	}
	file.Cues[0].Label = "Loaded show"
	file.Cues[0].Color = "#abcdef"
	body, _ := json.Marshal(app.PlaylistLoad{ExpectedRevision: 1, Playlist: file})
	// A valid bounded document larger than the normal 1 MiB API limit is
	// accepted on the import route. The file's own 4 MiB limit still applies.
	large := strings.TrimSuffix(string(body), "}") + strings.Repeat(" ", 1100000) + "}"
	w := request(api, "POST", "/api/playlist/import", large, admin, origin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	loaded := service.Playlist()
	if loaded.PlaylistRevision != 2 || loaded.Cues[0].ID == file.Cues[0].ID || loaded.Cues[0].Color != "#abcdef" || loaded.Cues[0].Label != "Loaded show" {
		t.Fatal("Playlist was not restored with fresh identities:", loaded)
	}
	// Reusing an older page must not replace the newly loaded show.
	if w := request(api, "POST", "/api/playlist/import", string(body), admin, origin); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	good, _ := json.Marshal(file)
	for _, invalid := range []string{
		"null", "{}", `{"expectedRevision":2,"playlist":null}`,
		`{"expectedRevision":2,"playlist":{"format":"other"}}`,
		`{"expectedRevision":2,"playlist":` + strings.TrimSuffix(string(good), "}") + `,"outputs":{"audioId":"other"}}}`,
		`{"expectedRevision":2,"playlist":` + string(good) + `,"path":"/tmp/forged.json"}`,
		string(body) + " {}",
		`{"expectedRevision":2,"playlist":` + strings.Repeat(" ", app.MaxPlaylistFileBytes+1025) + string(good) + `}`,
	} {
		if w := request(api, "POST", "/api/playlist/import", invalid, admin, origin); w.Code != 400 {
			t.Fatalf("Invalid import returned %d: %s", w.Code, w.Body.String())
		}
	}
	after := service.Playlist()
	if after.PlaylistRevision != loaded.PlaylistRevision || after.Cues[0].ID != loaded.Cues[0].ID {
		t.Fatal("Rejected import replaced the active playlist")
	}
}

func TestPlaylistFilesRemainLocalAdminOnly(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	var dialogs int
	controller := playlistfile.New(service, func(uint64, bool) bool { dialogs++; return true }, func() bool { return true })
	t.Cleanup(controller.Close)
	api.SetPlaylistFiles(controller)
	command.SetPlaylistFiles(controller)
	origin := "http://127.0.0.1:8787"
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/playlist/export", ""},
		{"GET", "/api/playlist/file", ""},
		{"POST", "/api/playlist/file", `{"operation":"save","expectedRevision":1}`},
		{"POST", "/api/playlist/import", `{}`},
	} {
		if w := request(api, route.method, route.path, route.body, auth.Session{}, origin); w.Code != 401 {
			t.Fatal("Anonymous playlist file access:", w.Code)
		}
		if w := request(command, route.method, route.path, route.body, remote, origin); w.Code != 403 {
			t.Fatal("Remote playlist file access:", w.Code)
		}
		if w := request(api, route.method, route.path, route.body, admin, "https://foreign.example"); w.Code != 403 {
			t.Fatal("Cross-origin playlist file access:", w.Code)
		}
		if route.method == "POST" {
			bad := admin
			bad.CSRF = "incorrect"
			if w := request(api, route.method, route.path, route.body, bad, origin); w.Code != 403 {
				t.Fatal("Missing CSRF protection:", w.Code)
			}
		}
	}
	for _, body := range []string{"null", `{"operation":"save","expectedRevision":1,"path":"/tmp/forged.json"}`} {
		if w := request(api, "POST", "/api/playlist/file", body, admin, origin); w.Code != 400 {
			t.Fatal("Native chooser accepted a path or null:", w.Code)
		}
	}
	if dialogs != 0 {
		t.Fatal("Rejected requests opened a native dialog")
	}
	w := request(api, "POST", "/api/admin-presence", `{}`, admin, origin)
	if !strings.Contains(w.Body.String(), `"playlistFiles":true`) {
		t.Fatal("Native playlist capability was not advertised")
	}
}

func TestNativePlaylistFileSnapshotCancellationAndRevisionConflict(t *testing.T) {
	api, authentication, service, media := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	var nativeID uint64
	controller := playlistfile.New(service, func(id uint64, save bool) bool { nativeID = id; return true }, func() bool { return true })
	t.Cleanup(controller.Close)
	api.SetPlaylistFiles(controller)
	origin := "http://127.0.0.1:8787"
	start := func(operation string, revision uint64) playlistfile.Status {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"operation": operation, "expectedRevision": revision})
		w := request(api, "POST", "/api/playlist/file", string(body), admin, origin)
		var status playlistfile.Status
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &status) != nil || status.ID != nativeID {
			t.Fatal(w.Code, w.Body.String())
		}
		return status
	}
	wait := func(phase string) playlistfile.Status {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			status := controller.Status()
			if status.Phase == phase {
				return status
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("Expected %s, got %#v", phase, controller.Status())
		return playlistfile.Status{}
	}
	first := start("save", 1)
	path := filepath.Join(t.TempDir(), "Evening.smartstage.json")
	controller.Complete(platform.DesktopPlaylistResult{ID: first.ID + 10, Path: path})
	if controller.Status().Phase != "choosing" {
		t.Fatal("Unrelated native result was accepted")
	}
	// Edits while the panel is open do not change the captured save snapshot.
	_, err := service.EditPlaylist(app.PlaylistEdit{ExpectedRevision: 1, Cues: []app.CueEdit{{ID: "cue-a", Label: "Newer edit", Path: media}}})
	if err != nil {
		t.Fatal(err)
	}
	controller.Complete(platform.DesktopPlaylistResult{ID: first.ID, Path: path})
	wait("complete")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved app.PlaylistFile
	if json.Unmarshal(data, &saved) != nil || saved.Cues[0].Label == "Newer edit" {
		t.Fatal("Save did not preserve its captured snapshot")
	}
	second := start("load", 2)
	controller.Complete(platform.DesktopPlaylistResult{ID: second.ID, Cancelled: true})
	wait("cancelled")
	if service.Playlist().PlaylistRevision != 2 {
		t.Fatal("Cancel changed the playlist")
	}
	third := start("load", 2)
	_, err = service.EditPlaylist(app.PlaylistEdit{ExpectedRevision: 2, Cues: []app.CueEdit{{ID: "cue-a", Label: "Concurrent edit", Path: media}}})
	if err != nil {
		t.Fatal(err)
	}
	controller.Complete(platform.DesktopPlaylistResult{ID: third.ID, Path: path})
	if status := wait("error"); status.Code != "revision_conflict" {
		t.Fatal(status)
	}
	last := start("load", 3)
	controller.Complete(platform.DesktopPlaylistResult{ID: last.ID, Path: path})
	wait("complete")
	loaded, err := service.ExportPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	loaded.Cues[0].ID = saved.Cues[0].ID
	if !reflect.DeepEqual(loaded, saved) || service.Playlist().PlaylistRevision != 4 {
		t.Fatal("Native load did not restore the saved playlist")
	}
	// Duplicate results from an already completed panel never import again.
	controller.Complete(platform.DesktopPlaylistResult{ID: last.ID, Path: path})
	if service.Playlist().PlaylistRevision != 4 {
		t.Fatal("Duplicate native result replaced the playlist twice")
	}
}
