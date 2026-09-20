package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	"smartstage/internal/web"
)

type commandStageBackend struct {
	noRender
	mu         sync.Mutex
	enabled    bool
	stageCalls int
	stopError  error
	stageError error
}

func (*commandStageBackend) Devices(context.Context) (playback.Devices, error) {
	return playback.Devices{
		Audio:    []playback.AudioDevice{{ID: "speaker", Name: "Speaker", Default: true}},
		Displays: []playback.Display{{ID: "screen", Name: "Stage screen"}},
	}, nil
}
func (b *commandStageBackend) Stop(generation uint64) error {
	b.mu.Lock()
	enabled := b.enabled
	stopError := b.stopError
	b.mu.Unlock()
	if stopError != nil {
		return stopError
	}
	b.events <- playback.Event{Kind: "stopped", Generation: generation, StageEnabled: enabled}
	return nil
}
func (b *commandStageBackend) Stage(generation uint64, _ string, enabled bool) error {
	b.mu.Lock()
	if b.stageError != nil {
		err := b.stageError
		b.mu.Unlock()
		return err
	}
	b.enabled = enabled
	b.stageCalls++
	b.mu.Unlock()
	b.events <- playback.Event{Kind: "stopped", Generation: generation, StageEnabled: enabled}
	return nil
}
func (b *commandStageBackend) Start(start playback.Start) error {
	b.mu.Lock()
	enabled := b.enabled
	b.mu.Unlock()
	b.events <- playback.Event{Kind: "playing", Generation: start.Generation, StageEnabled: enabled}
	return nil
}

func commandStageSetup(t *testing.T) (*API, *auth.Manager, *app.Service, *commandStageBackend) {
	t.Helper()
	root := t.TempDir()
	media := filepath.Join(root, "tone.wav")
	if err := os.WriteFile(media, []byte("test-only media"), 0600); err != nil {
		t.Fatal(err)
	}
	browser, err := files.New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	config := model.DefaultConfig()
	config.Outputs = model.Outputs{AudioID: "default", DisplayID: "screen", AllowPrimary: true}
	config.Cues = []model.Cue{{ID: "one", Label: "Opening", Path: media}}
	backend := &commandStageBackend{noRender: noRender{events: make(chan playback.Event, 32)}}
	service := app.New(backend, browser, discard{}, config)
	t.Cleanup(service.Close)
	authentication := auth.New()
	return NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787), authentication, service, backend
}

func waitCommandStageState(t *testing.T, service *app.Service, state string, enabled bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current := service.Snapshot(false)
		if current.State == state && current.StageEnabled == enabled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stage state did not become %s, enabled=%v: %+v", state, enabled, service.Snapshot(false))
}

func TestRemoteStageOnAndOffPreservePlayback(t *testing.T) {
	command, authentication, service, _ := commandStageSetup(t)
	session, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	origin := "http://127.0.0.1:8787"
	if response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, session, origin); response.Code != 202 {
		t.Fatalf("remote stage on: %d %s", response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "stopped", true)
	current := service.Snapshot(false)
	play, _ := json.Marshal(app.PlayRequest{RequestID: "remote-play", InstanceID: current.InstanceID, StopEpoch: current.StopEpoch, CueID: "one"})
	if response := request(command, "POST", "/api/play", string(play), session, origin); response.Code != 202 {
		t.Fatalf("remote PLAY: %d %s", response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "playing", true)
	if response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, session, origin); response.Code != 202 {
		t.Fatalf("remote could not enable stage during playback: %d %s", response.Code, response.Body.String())
	}
	before := service.Snapshot(false)
	if response := request(command, "POST", "/api/stage-output", `{"enabled":false}`, session, origin); response.Code != 202 {
		t.Fatalf("remote stage off: %d %s", response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "playing", false)
	after := service.Snapshot(false)
	if after.ActiveCueID != before.ActiveCueID || after.StopEpoch != before.StopEpoch || after.Generation != before.Generation {
		t.Fatal("remote stage off interrupted or invalidated playback")
	}
}

func TestRemoteStageRetainsSessionOriginCSRFBorders(t *testing.T) {
	command, authentication, service, backend := commandStageSetup(t)
	session, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	origin := "http://127.0.0.1:8787"
	if response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, auth.Session{}, origin); response.Code != 401 {
		t.Fatal("unauthenticated remote stage request:", response.Code)
	}
	for _, badOrigin := range []string{"", "http://attacker.example"} {
		if response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, session, badOrigin); response.Code != 403 {
			t.Fatal("remote stage accepted invalid Origin:", response.Code)
		}
	}
	badSession := session
	badSession.CSRF = "incorrect"
	if response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, badSession, origin); response.Code != 403 {
		t.Fatal("remote stage accepted invalid CSRF:", response.Code)
	}
	for _, route := range []string{"/api/playlist", "/api/files?showHidden=true", "/api/devices", "/api/update", "/api/stage-output"} {
		if response := request(command, "GET", route, "", session, origin); response.Code != 403 {
			t.Fatalf("remote GET gained access to %s: %d", route, response.Code)
		}
	}
	for _, route := range []string{"/api/playlist", "/api/outputs", "/api/stage-settings"} {
		if response := request(command, "PUT", route, "{}", session, origin); response.Code != 403 {
			t.Fatalf("remote mutation gained access to %s: %d", route, response.Code)
		}
	}
	if response := request(command, "POST", "/api/stage-output", `{"enabled":true,"displayId":"other"}`, session, origin); response.Code != 400 {
		t.Fatal("remote stage selected an arbitrary display:", response.Code)
	}
	backend.mu.Lock()
	calls := backend.stageCalls
	backend.mu.Unlock()
	if calls != 0 || service.Snapshot(false).StageEnabled {
		t.Fatal("rejected request changed stage output")
	}
}

func TestRemoteStageResponseRedactsNativeErrors(t *testing.T) {
	command, authentication, service, backend := commandStageSetup(t)
	session, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	const privateError = "Could not stop /private/operator/show/secret.wav"
	backend.mu.Lock()
	backend.stageError = errors.New(privateError)
	backend.mu.Unlock()
	response := request(command, "POST", "/api/stage-output", `{"enabled":true}`, session, "http://127.0.0.1:8787")
	if response.Code != 400 || strings.Contains(response.Body.String(), privateError) || strings.Contains(response.Body.String(), "secret.wav") {
		t.Fatalf("remote stage response leaked a native error: %d %s", response.Code, response.Body.String())
	}
	if service.Snapshot(true).LastError != privateError {
		t.Fatal("Admin lost the diagnostic error")
	}
}
