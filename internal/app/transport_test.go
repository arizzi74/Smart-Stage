package app

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func transportRequest(s *Service, action, id string, position float64) PlayRequest {
	state := s.Snapshot(true)
	r := PlayRequest{RequestID: id, InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: state.ActiveCueID, Generation: state.Generation, TransportRevision: state.TransportRevision, Action: action}
	if action == "seek" {
		r.Position = &position
	}
	return r
}

func transportEvent(s *Service, scene playback.Scene, kind string, position float64) {
	s.nativeEvent(playback.Event{Kind: kind, Generation: scene.ForegroundID, SceneRevision: scene.Revision, TransportRevision: scene.TransportRevision, Position: position, Duration: 30, StageEnabled: scene.StageEnabled})
}

func TestSeekValidatesIdentityBoundsAndIdempotencyBeforeMutation(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	r := transportRequest(s, "seek", "valid-seek-request", 12)
	for _, test := range []struct {
		name   string
		change func(*PlayRequest)
	}{
		{"instance", func(r *PlayRequest) { r.InstanceID = "old" }},
		{"epoch", func(r *PlayRequest) { r.StopEpoch-- }},
		{"generation", func(r *PlayRequest) { r.Generation-- }},
		{"transport", func(r *PlayRequest) { r.TransportRevision-- }},
		{"cue", func(r *PlayRequest) { r.CueID = "next" }},
		{"missing", func(r *PlayRequest) { r.Position = nil }},
		{"negative", func(r *PlayRequest) { v := -1.0; r.Position = &v }},
		{"beyond", func(r *PlayRequest) { v := 31.0; r.Position = &v }},
		{"nan", func(r *PlayRequest) { v := math.NaN(); r.Position = &v }},
		{"infinite", func(r *PlayRequest) { v := math.Inf(1); r.Position = &v }},
		{"action", func(r *PlayRequest) { r.Action = "rewind" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := r
			test.change(&bad)
			before := f.latest()
			if _, err := s.Play(bad); err == nil {
				t.Fatal("invalid request accepted")
			}
			if f.latest() != before {
				t.Fatal("invalid request mutated native scene")
			}
		})
	}
	ack, err := s.Play(r)
	if err != nil {
		t.Fatal(err)
	}
	seek := f.latest()
	if seek.SeekSeconds != 12 || seek.SeekRevision != 1 || seek.TransportRevision != r.TransportRevision+1 || !s.Snapshot(true).SeekPending {
		t.Fatalf("seek not queued: %+v", seek)
	}
	if again, err := s.Play(r); err != nil || !again.Duplicate || again.TransportRevision != ack.TransportRevision || f.latest() != seek {
		t.Fatalf("retry reapplied seek: %+v %v", again, err)
	}
	conflict := r
	v := 15.0
	conflict.Position = &v
	if _, err := s.Play(conflict); err == nil {
		t.Fatal("request id reused with new seek")
	}
	r.RequestID = "delayed-seek-request"
	if _, err := s.Play(r); err == nil {
		t.Fatal("stale transport accepted")
	}
	transportEvent(s, seek, "playing", 12)
	if state := s.Snapshot(true); state.SeekPending || state.Elapsed != 12 || state.State != "playing" {
		t.Fatalf("seek not acknowledged: %+v", state)
	}
}

func TestPausedSeekRetainsSelectionImageStageAndPosition(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{BackgroundCueID: "background-video", BackgroundAudio: true, AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
	original := scenePlaying(t, s, f, "music")
	if _, err := play(s, "image", "image-before-paused-seek"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	if _, err := s.Play(transportRequest(s, "pause", "explicit-pause-request", 0)); err != nil {
		t.Fatal(err)
	}
	paused := f.latest()
	transportEvent(s, paused, "paused", 7)
	for _, position := range []float64{0, 17, 30} {
		if _, err := s.Play(transportRequest(s, "seek", fmt.Sprintf("paused-seek-%v", position), position)); err != nil {
			t.Fatal(err)
		}
		seek := f.latest()
		if !seek.ForegroundPaused || seek.ForegroundID != original.ForegroundID || seek.ImagePath != paths["image"] || seek.BackgroundPath != paths["background-video"] || !seek.StageEnabled {
			t.Fatalf("seek disrupted scene: %+v", seek)
		}
		transportEvent(s, original, "progress", 3)
		if state := s.Snapshot(true); state.Elapsed != position || !state.SeekPending {
			t.Fatalf("stale progress overwrote requested seek: %+v", state)
		}
		transportEvent(s, seek, "paused", position)
		if state := s.Snapshot(true); state.State != "paused" || !state.Paused || state.SeekPending || state.Elapsed != position || state.ActiveCueID != "music" {
			t.Fatalf("paused seek changed transport: %+v", state)
		}
	}
	before := f.latest()
	if err := s.Stage(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if scene := f.latest(); scene.SeekRevision != before.SeekRevision || scene.TransportRevision != before.TransportRevision || !scene.ForegroundPaused {
		t.Fatalf("stage edit reapplied transport: %+v", scene)
	}
	if _, err := s.Play(transportRequest(s, "resume", "resume-after-seek", 0)); err != nil {
		t.Fatal(err)
	}
	resumed := f.latest()
	if resumed.ForegroundPaused || resumed.SeekRevision != before.SeekRevision || resumed.ForegroundID != original.ForegroundID {
		t.Fatalf("resume restarted source: %+v", resumed)
	}
}

func TestLoadingPauseIntentDoesNotPausePreviousSourceAndStopCancelsIt(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
			old := scenePlaying(t, s, f, "music")
			gate := &sceneInspectGate{path: paths["video"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
			f.sceneMu.Lock()
			f.gate = gate
			f.sceneMu.Unlock()
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
			if _, err := play(s, "video", "loading-first-press"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gate.entered:
			case <-time.After(time.Second):
				t.Fatal("inspection not started")
			}
			if _, err := play(s, "video", "loading-second-press"); err != nil {
				t.Fatal(err)
			}
			if state := s.Snapshot(true); !state.Paused || state.State != "loading" || state.ActiveCueID != "video" || f.latest() != old {
				t.Fatalf("loading pause changed old source: %+v", state)
			}
			if stop {
				if _, err := s.Stop(StopRequest{"stop-paused-loading"}); err != nil {
					t.Fatal(err)
				}
			}
			release.Do(func() { close(gate.release) })
			<-gate.completed
			if !stop {
				eventually(t, func() bool { return f.latest().ForegroundPath == paths["video"] })
				if !f.latest().ForegroundPaused {
					t.Fatal("paused loading cue started playing")
				}
				transportEvent(s, f.latest(), "paused", 0)
				if s.Snapshot(true).State != "paused" {
					t.Fatal("paused load was not acknowledged")
				}
			} else {
				// A subsequent job drains the same load worker, proving STOP won
				// even though inspection deliberately returned after cancellation.
				scenePlaying(t, s, f, "next")
				f.sceneMu.Lock()
				defer f.sceneMu.Unlock()
				for _, scene := range f.scenes {
					if scene.ForegroundPath == paths["video"] {
						t.Fatal("STOP lost to late paused load")
					}
				}
			}
		})
	}
}

func TestConcurrentSeeksAcceptOneRevisionAndStopInvalidatesPendingWork(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	r := transportRequest(s, "seek", "concurrent-seek-base", 8)
	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			candidate := r
			candidate.RequestID = fmt.Sprintf("concurrent-seek-%d", i)
			_, err := s.Play(candidate)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d seeks from one revision", accepted)
	}
	seek := f.latest()
	late := transportRequest(s, "resume", "late-resume-request", 0)
	if _, err := s.EmergencyStop(StopRequest{"stop-pending-seek"}); err != nil {
		t.Fatal(err)
	}
	stopped := f.latest()
	transportEvent(s, seek, "playing", 8)
	transportEvent(s, seek, "paused", 8)
	transportEvent(s, seek, "progress", 9)
	if _, err := s.Play(late); err == nil {
		t.Fatal("pre-STOP transport rearmed playback")
	}
	if f.latest() != stopped || !stopped.HardStop || stopped.ForegroundID != 0 || s.Snapshot(true).ActiveCueID != "" || s.Snapshot(true).SeekPending {
		t.Fatal("late seek undid emergency STOP")
	}
	scenePlaying(t, s, f, "music")
	late = transportRequest(s, "seek", "seek-before-replace", 5)
	scenePlaying(t, s, f, "next")
	late.StopEpoch = s.Snapshot(true).StopEpoch
	if _, err := s.Play(late); err == nil {
		t.Fatal("old foreground seek affected replacement")
	}
}

func TestNewerSeekSupersedesUnacknowledgedSeek(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	if _, err := s.Play(transportRequest(s, "seek", "first-pending-seek", 8)); err != nil {
		t.Fatal(err)
	}
	first := f.latest()
	if _, err := s.Play(transportRequest(s, "seek", "latest-pending-seek", 21)); err != nil {
		t.Fatal(err)
	}
	latest := f.latest()
	if latest.SeekRevision != first.SeekRevision+1 || latest.ForegroundID != first.ForegroundID {
		t.Fatal("newer seek did not retain source and supersede old target")
	}
	transportEvent(s, first, "playing", 8)
	transportEvent(s, first, "progress", 9)
	transportEvent(s, first, "ended", 30)
	if state := s.Snapshot(true); !state.SeekPending || state.Elapsed != 21 || state.ActiveCueID != "music" {
		t.Fatalf("stale seek callback displaced latest request: %+v", state)
	}
	transportEvent(s, latest, "playing", 21)
	if state := s.Snapshot(true); state.SeekPending || state.Elapsed != 21 {
		t.Fatalf("latest seek was not acknowledged: %+v", state)
	}
}

func TestPausedCueBlocksUpdatesOutputsImportAndSourceRemoval(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	if _, err := play(s, "music", "pause-before-guards"); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "paused", 4)
	if _, err := s.ReserveUpdate(); err == nil {
		t.Fatal("update allowed while paused")
	}
	if err := s.ConfigureOutputs(context.Background(), s.Playlist().Outputs); err == nil {
		t.Fatal("output replacement allowed while paused")
	}
	config := s.Playlist()
	edit := PlaylistEdit{ExpectedRevision: config.PlaylistRevision}
	for _, cue := range config.Cues {
		if cue.ID != "music" {
			edit.Cues = append(edit.Cues, CueEdit{ID: cue.ID, Label: cue.Label, Path: cue.Path})
		}
	}
	if _, err := s.EditPlaylist(edit); err == nil {
		t.Fatal("paused source removed")
	}
	for _, duration := range []float64{0, math.Inf(1), math.NaN()} {
		s.mu.Lock()
		s.state.Duration = duration
		s.mu.Unlock()
		if _, err := s.Play(transportRequest(s, "seek", "seek-unknown-duration", 0)); err == nil {
			t.Fatal("seek accepted without finite duration")
		}
	}
}

func TestImageKindCorrectedDuringInspectionKeepsPausedMusicControllable(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	music := scenePlaying(t, s, f, "music")
	if _, err := play(s, "music", "pause-before-corrected-image"); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "paused", 7)
	// A stale cached kind routes through foreground inspection before the
	// decoder identifies the source as an independent image.
	s.mu.Lock()
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == "image" {
			s.config.Cues[i].Cache.Media.Kind = "audio"
		}
	}
	s.mu.Unlock()
	if _, err := play(s, "image", "inspect-corrected-image"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	if state := s.Snapshot(true); state.State != "paused" || !state.Paused || state.ActiveCueID != "music" || state.Elapsed != 7 || f.latest().ForegroundID != music.ForegroundID {
		t.Fatalf("corrected image disturbed paused music: %+v", state)
	}
	if _, err := s.Play(transportRequest(s, "seek", "seek-after-corrected-image", 9)); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "paused", 9)
	if _, err := play(s, "music", "resume-after-corrected-image"); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "playing", 9)
	if state := s.Snapshot(true); state.State != "playing" || state.Paused || state.Elapsed != 9 || f.latest().ForegroundID != music.ForegroundID {
		t.Fatalf("corrected image broke music transport: %+v", state)
	}
}

func TestResumeAtDurationCanEndBeforePlayingAcknowledgement(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	scenePlaying(t, s, f, "music")
	if _, err := play(s, "music", "pause-before-endpoint"); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "paused", 7)
	if _, err := s.Play(transportRequest(s, "seek", "paused-seek-endpoint", 30)); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "paused", 30)
	if _, err := play(s, "music", "resume-at-endpoint"); err != nil {
		t.Fatal(err)
	}
	transportEvent(s, f.latest(), "ended", 30)
	if state := s.Snapshot(true); state.State != "stopped" || state.ActiveCueID != "" || state.Paused || state.SeekPending || f.latest().ForegroundID != 0 {
		t.Fatalf("direct end after resume left stale selection: %+v", state)
	}
}

func TestPauseRacingNaturalEndAcceptsCurrentTerminalAcknowledgement(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	original := scenePlaying(t, s, f, "music")
	if _, err := play(s, "music", "pause-racing-natural-end"); err != nil {
		t.Fatal(err)
	}
	paused := f.latest()
	transportEvent(s, original, "ended", 30)
	if s.Snapshot(true).ActiveCueID != "music" {
		t.Fatal("old terminal callback displaced a newer pause")
	}
	transportEvent(s, paused, "ended", 30)
	if state := s.Snapshot(true); state.State != "stopped" || state.ActiveCueID != "" {
		t.Fatalf("released native source left a stale selection: %+v", state)
	}
}
