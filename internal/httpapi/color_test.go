package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"smartstage/internal/app"
	"smartstage/internal/web"
)

func TestAdminCueColorsAreVisibleToCommandWithoutSourcePaths(t *testing.T) {
	adminAPI, authentication, service, path := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	commandSession, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	color := "#1a2B3c"
	config := service.Playlist()
	body, _ := json.Marshal(app.PlaylistEdit{ExpectedRevision: config.PlaylistRevision, Cues: []app.CueEdit{{ID: config.Cues[0].ID, Label: config.Cues[0].Label, Path: path, Color: &color}}})
	if response := request(adminAPI, "PUT", "/api/playlist", string(body), admin, "http://127.0.0.1:8787"); response.Code != 200 {
		t.Fatalf("Admin color edit: %d %s", response.Code, response.Body.String())
	}
	response := request(command, "GET", "/api/state", "", commandSession, "")
	var payload struct {
		State app.State `json:"state"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.State.Cues) != 1 || payload.State.Cues[0].Color != color {
		t.Fatalf("Command color state: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), path) {
		t.Fatal("cue color exposed the private source path")
	}
	if response := request(command, "PUT", "/api/playlist", string(body), commandSession, "http://127.0.0.1:8787"); response.Code != 403 {
		t.Fatal("Command session gained color editing authority:", response.Code)
	}
}
