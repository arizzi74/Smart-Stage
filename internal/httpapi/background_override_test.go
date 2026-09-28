package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"smartstage/internal/app"
	"smartstage/internal/web"
)

func TestBackgroundOverrideExistingPlayRouteAndStateForAdminAndRemote(t *testing.T) {
	for _, saved := range []string{"", "image", "video"} {
		t.Run("default="+saved, func(t *testing.T) {
			api, authn, service, backend, _, dir := sceneRouteSetup(t)
			admin, _ := authn.LocalAdmin("")
			remote, _ := authn.PairCommand(authn.CommandToken(), "background-override-controller")
			command := NewCommand(service, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
			origin := "http://127.0.0.1:8787"
			settings := service.Playlist().Stage
			settings.BackgroundCueID = saved
			if _, err := service.ConfigureStage(context.Background(), app.StageEdit{ExpectedRevision: service.Playlist().PlaylistRevision, Settings: settings}); err != nil {
				t.Fatal(err)
			}
			beforeFile, err := os.ReadFile(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			stateFromRemote := func(override, effective string) app.State {
				t.Helper()
				response := request(command, "GET", "/api/state", "", remote, "")
				var payload struct {
					State app.State `json:"state"`
				}
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
					t.Fatalf("remote state: %d %s", response.Code, response.Body.String())
				}
				state := payload.State
				if !strings.Contains(response.Body.String(), `"backgroundOverrideCueId":`) || strings.Contains(response.Body.String(), dir) || state.BackgroundOverrideCueID != override || state.BackgroundCueID != effective || state.Stage.BackgroundCueID != saved {
					t.Fatalf("remote background state missing, incorrect or leaked paths: %s", response.Body.String())
				}
				return state
			}
			state := stateFromRemote("", saved)
			play := app.PlayRequest{RequestID: "background-http-music", InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: "music"}
			body, _ := json.Marshal(play)
			if response := request(api, "POST", "/api/play", string(body), admin, origin); response.Code != 202 {
				t.Fatal(response.Code, response.Body.String())
			}
			waitCommandStageState(t, service, "playing", false)
			for _, action := range []string{"pause", "seek"} {
				state = service.Snapshot(true)
				transport := app.PlayRequest{RequestID: "background-http-" + action, InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: "music", Generation: state.Generation, TransportRevision: state.TransportRevision, Action: action}
				if action == "seek" {
					position := 8.0
					transport.Position = &position
				}
				body, _ = json.Marshal(transport)
				if response := request(api, "POST", "/api/play", string(body), admin, origin); response.Code != 202 {
					t.Fatal(response.Code, response.Body.String())
				}
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					current := service.Snapshot(true)
					if current.State == "paused" && !current.SeekPending {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			before := service.Snapshot(true)
			if before.State != "paused" || before.Elapsed != 8 || before.SeekPending {
				t.Fatalf("paused native fixture not ready: %+v", before)
			}
			selectOverride := app.PlayRequest{RequestID: "background-http-select", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "video"}
			selectionBody, _ := json.Marshal(selectOverride)
			for i, selected := range []bool{true, false} {
				body = selectionBody
				if !selected {
					clear := selectOverride
					clear.RequestID = "background-http-clear"
					body, _ = json.Marshal(clear)
				}
				endpoint, session := command, remote
				if !selected {
					endpoint, session = api, admin
				}
				if response := request(endpoint, "POST", "/api/play", string(body), session, origin); response.Code != 202 {
					t.Fatalf("background toggle %d: %d %s", i, response.Code, response.Body.String())
				}
				var duplicate app.Ack
				response := request(endpoint, "POST", "/api/play", string(body), session, origin)
				if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &duplicate) != nil || !duplicate.Duplicate {
					t.Fatalf("toggle retry was not idempotent: %d %s", response.Code, response.Body.String())
				}
				override, effective := "", saved
				if selected {
					override, effective = "video", "video"
				}
				after := stateFromRemote(override, effective)
				if after.ActiveCueID != before.ActiveCueID || after.Generation != before.Generation || after.TransportRevision != before.TransportRevision || after.StopEpoch != before.StopEpoch || after.Elapsed != 8 || !after.Paused || after.StageEnabled {
					t.Fatalf("background API changed paused foreground or enabled Stage: %+v", after)
				}
			}
			// A late delivery of the original select cannot resurrect a cleared override.
			if response := request(command, "POST", "/api/play", string(selectionBody), remote, origin); response.Code != 202 {
				t.Fatal(response.Code, response.Body.String())
			}
			stateFromRemote("", saved)
			if afterFile, err := os.ReadFile(filepath.Join(dir, "state.json")); err != nil || !bytes.Equal(beforeFile, afterFile) {
				t.Fatal("background session buttons changed persisted show bytes")
			}
			if strings.Contains(string(beforeFile), "backgroundOverrideCueId") {
				t.Fatal("session state was persisted")
			}
			if scene := backend.latest(); scene.ForegroundID != before.Generation || !scene.ForegroundPaused {
				t.Fatalf("background request replaced native foreground: %+v", scene)
			}
			if _, err := service.Stop(app.StopRequest{RequestID: "invalidate-background-http"}); err != nil {
				t.Fatal(err)
			}
			selectOverride.RequestID = fmt.Sprintf("stale-background-http-%s", saved)
			body, _ = json.Marshal(selectOverride)
			if response := request(command, "POST", "/api/play", string(body), remote, origin); response.Code != 409 {
				t.Fatalf("STOP failed to invalidate delayed background selection: %d %s", response.Code, response.Body.String())
			}
			stateFromRemote("", saved)
		})
	}
}
