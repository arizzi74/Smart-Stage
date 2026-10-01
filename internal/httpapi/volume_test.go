package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"smartstage/internal/app"
	"smartstage/internal/web"
)

func TestVolumeRolesValidationAndState(t *testing.T) {
	adminAPI, authentication, service, path := setupAPI(t)
	admin, _ := authentication.LocalAdmin("")
	remote, _ := authentication.PairCommand(authentication.CommandToken(), "remote")
	command := NewCommand(service, authentication, web.Handler(), []string{"127.0.0.1"}, 8787)
	config := service.Playlist()
	body := `{"expectedRevision":1,"cueId":"` + config.Cues[0].ID + `","volume":0}`
	if response := request(command, "PUT", "/api/playlist/volume", body, remote, "http://127.0.0.1:8787"); response.Code != 403 {
		t.Fatal("remote gained saved-track editing rights", response.Code)
	}
	if response := request(adminAPI, "PUT", "/api/playlist/volume", body, admin, "http://127.0.0.1:8787"); response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, value := range []string{"null", "-0.1", "1.1", `"50"`} {
		body := `{"expectedRevision":2,"cueId":"` + config.Cues[0].ID + `","volume":` + value + `}`
		if response := request(adminAPI, "PUT", "/api/playlist/volume", body, admin, "http://127.0.0.1:8787"); response.Code != 400 {
			t.Fatalf("invalid volume %s: %d", value, response.Code)
		}
	}
	state := service.Snapshot(false)
	level := .1
	master, _ := json.Marshal(app.PlayRequest{RequestID: "remote-master-001", InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, Action: "volume", Volume: &level})
	if response := request(command, "POST", "/api/play", string(master), remote, "http://127.0.0.1:8787"); response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	response := request(command, "GET", "/api/state", "", remote, "")
	var payload struct {
		State app.State `json:"state"`
	}
	if json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.State.MasterVolume != .1 || payload.State.Cues[0].Volume != 0 || strings.Contains(response.Body.String(), path) {
		t.Fatal("wrong/redacted volume state", response.Body.String())
	}
	if response := request(command, "POST", "/api/play", string(master), remote, "https://evil.example"); response.Code != 403 {
		t.Fatal("cross-origin volume mutation accepted", response.Code)
	}
}
