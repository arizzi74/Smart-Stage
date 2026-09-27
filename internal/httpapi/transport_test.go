package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"smartstage/internal/app"
	"smartstage/internal/web"
)

func TestTransportUsesExistingPlayRouteForAdminAndRemote(t *testing.T) {
	api, authn, service, backend, _, dir := sceneRouteSetup(t)
	admin, _ := authn.LocalAdmin("")
	remote, _ := authn.PairCommand(authn.CommandToken(), "transport-controller")
	command := NewCommand(service, authn, web.Handler(), []string{"127.0.0.1"}, 8787)
	origin := "http://127.0.0.1:8787"
	state := service.Snapshot(true)
	body, _ := json.Marshal(app.PlayRequest{RequestID: "transport-http-play", InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: "music"})
	if reply := request(api, "POST", "/api/play", string(body), admin, origin); reply.Code != 202 {
		t.Fatal(reply.Code, reply.Body.String())
	}
	waitCommandStageState(t, service, "playing", false)
	for i, action := range []string{"pause", "seek", "resume"} {
		state = service.Snapshot(true)
		position := 14.0
		r := app.PlayRequest{RequestID: fmt.Sprintf("transport-http-%d", i), InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: state.ActiveCueID, Generation: state.Generation, TransportRevision: state.TransportRevision, Action: action}
		if action == "seek" {
			r.Position = &position
		}
		body, _ = json.Marshal(r)
		reply := request(command, "POST", "/api/play", string(body), remote, origin)
		if reply.Code != 202 {
			t.Fatalf("remote %s: %d %s", action, reply.Code, reply.Body.String())
		}
		want := "paused"
		if action == "resume" {
			want = "playing"
		}
		waitCommandStageState(t, service, want, false)
		if action == "seek" && (!backend.latest().ForegroundPaused || backend.latest().SeekSeconds != position) {
			t.Fatal("remote seek resumed or lost position")
		}
		var accepted, duplicate app.Ack
		if err := json.Unmarshal(reply.Body.Bytes(), &accepted); err != nil {
			t.Fatal(err)
		}
		retry := request(command, "POST", "/api/play", string(body), remote, origin)
		if retry.Code != 202 || json.Unmarshal(retry.Body.Bytes(), &duplicate) != nil || !duplicate.Duplicate || duplicate.TransportRevision != accepted.TransportRevision {
			t.Fatalf("retry changed %s: %s", action, retry.Body.String())
		}
		r.RequestID = fmt.Sprintf("stale-transport-http-%d", i)
		body, _ = json.Marshal(r)
		if response := request(command, "POST", "/api/play", string(body), remote, origin); response.Code != 409 {
			t.Fatalf("stale %s: %d %s", action, response.Code, response.Body.String())
		}
	}
	reply := request(command, "GET", "/api/state", "", remote, "")
	if strings.Contains(reply.Body.String(), dir) || !strings.Contains(reply.Body.String(), `"transportRevision"`) || !strings.Contains(reply.Body.String(), `"seekPending"`) || !strings.Contains(reply.Body.String(), `"paused"`) {
		t.Fatalf("remote transport state malformed or leaked paths: %s", reply.Body.String())
	}
}

func TestSeekRejectsMalformedMissingAndOutOfRangePositions(t *testing.T) {
	api, authn, service, _, _, _ := sceneRouteSetup(t)
	admin, _ := authn.LocalAdmin("")
	origin := "http://127.0.0.1:8787"
	state := service.Snapshot(true)
	body, _ := json.Marshal(app.PlayRequest{RequestID: "http-seek-validation-play", InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: "music"})
	if response := request(api, "POST", "/api/play", string(body), admin, origin); response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	waitCommandStageState(t, service, "playing", false)
	state = service.Snapshot(true)
	base := fmt.Sprintf(`{"requestId":"http-invalid-seek","instanceId":%q,"stopEpoch":%d,"cueId":"music","generation":%d,"transportRevision":%d,"action":"seek"`, state.InstanceID, state.StopEpoch, state.Generation, state.TransportRevision)
	for _, suffix := range []string{`}`, `,"position":null}`, `,"position":-1}`, `,"position":31}`, `,"position":"10"}`, `,"position":1e1000}`, `,"position":NaN}`, `,"position":4,"unknown":true}`} {
		response := request(api, "POST", "/api/play", base+suffix, admin, origin)
		if response.Code != 400 {
			t.Fatalf("invalid position accepted (%s): %d %s", suffix, response.Code, response.Body.String())
		}
	}
	if after := service.Snapshot(true); after.TransportRevision != state.TransportRevision || after.SeekPending || after.Elapsed != state.Elapsed {
		t.Fatalf("invalid seek changed state: %+v", after)
	}
}
