package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/auth"
	"smartstage/internal/files"
	"smartstage/internal/model"
	"smartstage/internal/playback"
	"smartstage/internal/store"
	"smartstage/internal/web"
)

// This API fixture supplies synthetic native acknowledgements. Persistence and
// authentication use the production implementations; playback is not exercised.
type sceneRouteBackend struct {
	noRender
	mu     sync.Mutex
	scenes []playback.Scene
}

func (*sceneRouteBackend) Devices(context.Context) (playback.Devices, error) {
	return playback.Devices{Audio: []playback.AudioDevice{{ID: "speaker", Default: true}}, Displays: []playback.Display{{ID: "screen"}}}, nil
}
func (*sceneRouteBackend) Inspect(_ context.Context, path string) (playback.Media, error) {
	switch filepath.Ext(path) {
	case ".png":
		return playback.Media{Kind: "image"}, nil
	case ".mp4":
		return playback.Media{Kind: "video", HasVideo: true, HasAudio: true, Duration: 30}, nil
	default:
		return playback.Media{Kind: "audio", HasAudio: true, Duration: 30}, nil
	}
}
func (b *sceneRouteBackend) ApplyScene(scene playback.Scene) error {
	b.mu.Lock()
	b.scenes = append(b.scenes, scene)
	b.mu.Unlock()
	b.events <- playback.Event{Kind: "stage", Generation: scene.Generation, SceneRevision: scene.Revision, StageEnabled: scene.StageEnabled}
	kind, generation := "stopped", scene.Generation
	if scene.ForegroundID != 0 {
		kind, generation = "playing", scene.ForegroundID
	}
	b.events <- playback.Event{Kind: kind, Generation: generation, SceneRevision: scene.Revision, StageEnabled: scene.StageEnabled, Duration: 30}
	return nil
}
func (b *sceneRouteBackend) latest() playback.Scene {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.scenes) == 0 {
		return playback.Scene{}
	}
	return b.scenes[len(b.scenes)-1]
}

func sceneRouteSetup(t *testing.T) (*API, *auth.Manager, *app.Service, *sceneRouteBackend, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	storage, config, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	config.Outputs = model.Outputs{AudioID: "default", DisplayID: "screen", AllowPrimary: true}
	for _, item := range []struct{ id, file string }{{"music", "private-song.wav"}, {"image", "private-image.png"}, {"video", "private-video.mp4"}} {
		path := filepath.Join(dir, item.file)
		if err := os.WriteFile(path, []byte("synthetic native fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		config.Cues = append(config.Cues, model.Cue{ID: item.id, Label: item.id, Path: path, Hidden: item.id == "image", Background: item.id == "video"})
	}
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	browser, err := files.New([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	backend := &sceneRouteBackend{noRender: noRender{events: make(chan playback.Event, 128)}}
	service := app.New(backend, browser, storage, config)
	t.Cleanup(service.Close)
	authentication := auth.New()
	return New(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787), authentication, service, backend, storage, dir
}

func TestStageSettingsRequireLocalAdminAndRejectUntrustedInput(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "stage-settings-remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	badCSRF := admin
	badCSRF.CSRF = "incorrect"
	body := `{"expectedRevision":1,"settings":{"backgroundCueId":"","backgroundAudio":true,"fadeEnabled":true,"fadeSeconds":1.5,"toggleAudio":true}}`
	for _, test := range []struct {
		name    string
		api     *API
		session auth.Session
		origin  string
		status  int
	}{
		{"unauthenticated", api, auth.Session{}, "http://127.0.0.1:8787", 401},
		{"remote", command, remote, "http://127.0.0.1:8787", 403},
		{"remote credential on admin", api, remote, "http://127.0.0.1:8787", 401},
		{"missing origin", api, admin, "", 403},
		{"foreign origin", api, admin, "http://attacker.example", 403},
		{"bad csrf", api, badCSRF, "http://127.0.0.1:8787", 403},
	} {
		if reply := request(test.api, "PUT", "/api/stage-settings", body, test.session, test.origin); reply.Code != test.status {
			t.Fatalf("%s: got %d, want %d: %s", test.name, reply.Code, test.status, reply.Body.String())
		}
	}
	for _, invalid := range []string{`null`, `[]`, `{}`, body + ` {}`, strings.Replace(body, `"fadeSeconds":1.5`, `"fadeSeconds":0`, 1), strings.Replace(body, `"fadeSeconds":1.5`, `"fadeSeconds":31`, 1), strings.Replace(body, `"toggleAudio":true`, `"toggleAudio":true,"path":"/private/injected.mp4"`, 1)} {
		if reply := request(api, "PUT", "/api/stage-settings", invalid, admin, "http://127.0.0.1:8787"); reply.Code != 400 {
			t.Fatalf("invalid settings %s: %d %s", invalid, reply.Code, reply.Body.String())
		}
	}
	r := httptest.NewRequest("PUT", "http://127.0.0.1:8787/api/stage-settings", strings.NewReader(body))
	r.RemoteAddr = "192.168.0.12:54321"
	r.AddCookie(&http.Cookie{Name: AdminCookie, Value: admin.ID})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://127.0.0.1:8787")
	r.Header.Set("X-CSRF-Token", admin.CSRF)
	w := httptest.NewRecorder()
	api.ServeHTTP(w, r)
	if w.Code != 403 || service.Playlist().PlaylistRevision != 1 || service.Playlist().Stage.FadeEnabled {
		t.Fatal("untrusted settings request changed the show")
	}
}

func TestStageSettingsHTTPRevisionPersistenceAndRemoteRedaction(t *testing.T) {
	api, authentication, service, _, storage, dir := sceneRouteSetup(t)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "settings-observer")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	settings := model.StageSettings{BackgroundCueID: "video", BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 2.5, ToggleAudio: true}
	body, _ := json.Marshal(app.StageEdit{ExpectedRevision: 1, Settings: settings})
	response := request(api, "PUT", "/api/stage-settings", string(body), admin, "http://127.0.0.1:8787")
	var saved model.Config
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &saved) != nil || saved.PlaylistRevision != 2 || saved.Stage != settings {
		t.Fatalf("stage settings were not saved: %d %s", response.Code, response.Body.String())
	}
	if response := request(api, "PUT", "/api/stage-settings", string(body), admin, "http://127.0.0.1:8787"); response.Code != 409 {
		t.Fatalf("stale settings overwrote a newer show: %d %s", response.Code, response.Body.String())
	}
	bad := settings
	bad.BackgroundCueID = "music"
	body, _ = json.Marshal(app.StageEdit{ExpectedRevision: saved.PlaylistRevision, Settings: bad})
	if response := request(api, "PUT", "/api/stage-settings", string(body), admin, "http://127.0.0.1:8787"); response.Code != 400 {
		t.Fatalf("audio-only background was accepted: %d %s", response.Code, response.Body.String())
	}
	remoteState := request(command, "GET", "/api/state", "", remote, "")
	var reply struct{ State app.State }
	if remoteState.Code != 200 || json.Unmarshal(remoteState.Body.Bytes(), &reply) != nil || reply.State.Stage != settings || !reply.State.Cues[1].Hidden || !reply.State.Cues[2].Background {
		t.Fatalf("remote lost presentation settings or cue flags: %d %s", remoteState.Code, remoteState.Body.String())
	}
	if strings.Contains(remoteState.Body.String(), dir) || strings.Contains(remoteState.Body.String(), "private-video.mp4") {
		t.Fatal("remote stage settings exposed host source paths")
	}
	service.Close()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, restored, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if restored.Stage != settings || restored.PlaylistRevision != saved.PlaylistRevision || !restored.Cues[1].Hidden || !restored.Cues[2].Background {
		t.Fatalf("HTTP settings or cue flags did not survive storage reopen: %+v", restored)
	}
}

func TestEmergencyStopSecurityPriorityAndIdempotency(t *testing.T) {
	api, authentication, service, backend, _, _ := sceneRouteSetup(t)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "emergency-controller")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	origin := "http://127.0.0.1:8787"
	for _, target := range []struct {
		api     *API
		session auth.Session
	}{{api, admin}, {command, remote}} {
		before := service.Snapshot(true)
		bad := target.session
		bad.CSRF = "wrong"
		for _, test := range []struct {
			session auth.Session
			origin  string
			status  int
		}{{auth.Session{}, origin, 401}, {target.session, "", 403}, {target.session, "http://attacker.example", 403}, {bad, origin, 403}} {
			if response := request(target.api, "POST", "/api/emergency-stop", `{"requestId":"emergency-invalid"}`, test.session, test.origin); response.Code != test.status {
				t.Fatalf("emergency authentication guard: %d %s", response.Code, response.Body.String())
			}
		}
		for _, invalid := range []string{`null`, `{}`, `{"requestId":"x"}`, `{"requestId":"emergency-unknown","enabled":true}`} {
			if response := request(target.api, "POST", "/api/emergency-stop", invalid, target.session, origin); response.Code != 400 {
				t.Fatalf("invalid emergency body accepted: %d %s", response.Code, response.Body.String())
			}
		}
		if service.Snapshot(true).StopEpoch != before.StopEpoch {
			t.Fatal("rejected emergency request interrupted playback")
		}
	}
	if response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, remote, origin); response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "stopped", true)
	state := service.Snapshot(false)
	body, _ := json.Marshal(app.PlayRequest{RequestID: "music-before-emergency", CueID: "music", InstanceID: state.InstanceID, StopEpoch: state.StopEpoch})
	if response := request(command, "POST", "/api/play", string(body), remote, origin); response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "playing", true)
	for i := 0; i < cap(command.ordinary); i++ {
		command.ordinary <- struct{}{}
	}
	if response := request(command, "POST", "/api/play", string(body), remote, origin); response.Code != 503 {
		t.Fatal("ordinary slots were not saturated")
	}
	response := request(command, "POST", "/api/emergency-stop", `{"requestId":"remote-priority-emergency"}`, remote, origin)
	var first, repeated app.Ack
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &first) != nil {
		t.Fatalf("emergency blocked behind normal operations: %d %s", response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "stopped", false)
	if scene := backend.latest(); !scene.HardStop || scene.ForegroundID != 0 || scene.StageEnabled || scene.FadeSeconds != 0 {
		t.Fatalf("HTTP emergency did not request immediate silence and stage off: %+v", scene)
	}
	response = request(command, "POST", "/api/emergency-stop", `{"requestId":"remote-priority-emergency"}`, remote, origin)
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &repeated) != nil || !repeated.Duplicate || repeated.StopEpoch != first.StopEpoch {
		t.Fatalf("emergency retry repeated the stop: %d %s", response.Code, response.Body.String())
	}
	// A different command cannot reuse an emergency request identity.
	if response := request(command, "POST", "/api/stop", `{"requestId":"remote-priority-emergency"}`, remote, origin); response.Code != 409 {
		t.Fatal("emergency identity was reused for an ordinary stop")
	}
	if response := request(api, "POST", "/api/emergency-stop", `{"requestId":"admin-emergency-stop"}`, admin, origin); response.Code != 202 {
		t.Fatal("Admin cannot send emergency stop")
	}
}

func TestStageSettingsWaitForUpdateButEmergencyRemainsAvailable(t *testing.T) {
	api, authentication, service, _ := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	release, err := service.ReserveUpdate()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	body, _ := json.Marshal(app.StageEdit{ExpectedRevision: 1, Settings: model.StageSettings{FadeSeconds: 1}})
	if response := request(api, "PUT", "/api/stage-settings", string(body), admin, "http://127.0.0.1:8787"); response.Code != 409 {
		t.Fatalf("stage settings changed during update reservation: %d %s", response.Code, response.Body.String())
	}
	started := time.Now()
	if response := request(api, "POST", "/api/emergency-stop", `{"requestId":"emergency-during-update"}`, admin, "http://127.0.0.1:8787"); response.Code != 202 {
		t.Fatalf("emergency unavailable during update: %d %s", response.Code, response.Body.String())
	}
	if time.Since(started) > time.Second || service.Playlist().PlaylistRevision != 1 {
		t.Fatal("emergency waited on update work or changed persisted show settings")
	}
}
