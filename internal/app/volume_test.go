package app

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func volumeRequest(s *Service, id string, value float64) PlayRequest {
	state := s.Snapshot(true)
	return PlayRequest{RequestID: id, InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, Action: "volume", Volume: &value}
}

func TestLiveVolumesKeepPlaybackTransportAndIndependentBackground(t *testing.T) {
	s, native, _, _ := sceneSetup(t, model.StageSettings{BackgroundCueID: "background-video", BackgroundAudio: true, AudioFadeEnabled: true, AudioFadeSeconds: 2, VisualFadeSeconds: 1})
	eventually(t, func() bool { return native.latest().BackgroundPath != "" })
	before := scenePlaying(t, s, native, "music")
	if before.MasterVolume != .75 || before.ForegroundVolume != .5 || before.BackgroundVolume != .5 {
		t.Fatalf("default levels: %+v", before)
	}
	value := .9
	updated, err := s.EditCueVolume(CueVolumeEdit{ExpectedRevision: s.Playlist().PlaylistRevision, CueID: "music", Volume: &value})
	if err != nil || updated.Cues[0].PlaybackVolume() != value {
		t.Fatalf("track edit: %+v %v", updated, err)
	}
	next := native.latest()
	if next.ForegroundID != before.ForegroundID || next.TransportRevision != before.TransportRevision || next.ForegroundVolume != .9 || next.BackgroundVolume != .5 || next.AudioFadeSeconds != 2 {
		t.Fatalf("volume changed transport/background/fade: %+v", next)
	}
	request := volumeRequest(s, "master-mute-0001", 0)
	ack, err := s.Play(request)
	if err != nil || !ack.Accepted {
		t.Fatal(ack, err)
	}
	next = native.latest()
	if next.MasterVolume != 0 || next.ForegroundID != before.ForegroundID || next.TransportRevision != before.TransportRevision || s.Snapshot(true).State != "playing" {
		t.Fatalf("master mute stopped/restarted playback: %+v", next)
	}
	if again, err := s.Play(request); err != nil || !again.Duplicate || native.latest() != next {
		t.Fatalf("duplicate master write reapplied: %+v %v", again, err)
	}
	if _, err := s.Play(volumeRequest(s, "master-raise-0001", 1)); err != nil {
		t.Fatal(err)
	}
	if native.latest().MasterVolume != 1 || s.Playlist().PlaylistRevision != updated.PlaylistRevision {
		t.Fatal("master write changed playlist revision")
	}
	native.events <- playback.Event{Kind: "progress", Generation: before.ForegroundID, TransportRevision: before.TransportRevision, Position: 1.5, Duration: 30}
	eventually(t, func() bool { return s.Snapshot(true).Elapsed == 1.5 })
	state := s.Snapshot(true)
	if _, err := s.Play(PlayRequest{RequestID: "volume-pause-0001", InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: state.ActiveCueID}); err != nil {
		t.Fatal(err)
	}
	paused := native.latest()
	if _, err := s.Play(volumeRequest(s, "master-paused-001", .2)); err != nil {
		t.Fatal(err)
	}
	if next := native.latest(); !next.ForegroundPaused || next.TransportRevision != paused.TransportRevision || next.ForegroundID != paused.ForegroundID || s.Snapshot(true).Elapsed != 1.5 {
		t.Fatalf("volume resumed or sought paused source: %+v", next)
	}
}

func TestInvalidVolumeAndSaveFailureLeaveConfirmedLevel(t *testing.T) {
	s, native, storage, _ := sceneSetup(t, model.DefaultConfig().Stage)
	scenePlaying(t, s, native, "music")
	for _, value := range []float64{-.01, 1.01, math.NaN(), math.Inf(1)} {
		if _, err := s.Play(volumeRequest(s, "invalid-master-001", value)); err == nil {
			t.Fatalf("invalid master %v accepted", value)
		}
		if _, err := s.EditCueVolume(CueVolumeEdit{ExpectedRevision: s.Playlist().PlaylistRevision, CueID: "music", Volume: &value}); err == nil {
			t.Fatalf("invalid track %v accepted", value)
		}
	}
	level := .2
	before := native.latest()
	storage.fail = true
	if _, err := s.EditCueVolume(CueVolumeEdit{ExpectedRevision: s.Playlist().PlaylistRevision, CueID: "music", Volume: &level}); err == nil || native.latest() != before || s.Playlist().Cues[0].PlaybackVolume() != .5 {
		t.Fatalf("failed persistence changed live/saved level: %v", err)
	}
	storage.fail = false
	if _, err := s.EditCueVolume(CueVolumeEdit{ExpectedRevision: s.Playlist().PlaylistRevision, CueID: "image", Volume: &level}); err == nil {
		t.Fatal("image volume accepted")
	}
	request := volumeRequest(s, "stale-volume-001", .25)
	if _, err := s.Stop(StopRequest{RequestID: "stop-before-volume"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Play(request); err == nil || s.Snapshot(true).MasterVolume != .75 {
		t.Fatal("queued pre-STOP volume accepted", err)
	}
}

func TestTrackVolumePlaylistRoundTripAndLegacyDefault(t *testing.T) {
	s, _, _, _ := sceneSetup(t, model.DefaultConfig().Stage)
	level := 0.0
	if _, err := s.EditCueVolume(CueVolumeEdit{ExpectedRevision: s.Playlist().PlaylistRevision, CueID: "music", Volume: &level}); err != nil {
		t.Fatal(err)
	}
	file := playlistDocument(t, s)
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlaylist(bytes.NewReader(data))
	if err != nil || decoded.Cues[0].Volume == nil || *decoded.Cues[0].Volume != 0 {
		t.Fatalf("mute did not round-trip: %+v %v", decoded, err)
	}
	if _, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: decoded}); err != nil {
		t.Fatal(err)
	}
	config := s.Playlist()
	if config.Cues[0].PlaybackVolume() != 0 {
		t.Fatal("loading dropped mute")
	}
	*decoded.Cues[0].Volume = .8
	if s.Playlist().Cues[0].PlaybackVolume() != 0 {
		t.Fatal("loaded playlist levels alias the caller's document")
	}
	clone := config.Clone()
	*clone.Cues[0].Volume = 1
	if s.Playlist().Cues[0].PlaybackVolume() != 0 {
		t.Fatal("config clone volume aliases live state")
	}
	for i := range decoded.Cues {
		decoded.Cues[i].Volume = nil
	}
	if _, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: decoded}); err != nil || s.Playlist().Cues[0].PlaybackVolume() != .5 {
		t.Fatal("legacy playlist did not receive centered default", err)
	}
}
