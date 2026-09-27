package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func TestSceneSelectedVideoPausesAndResumesWithBackgroundIntact(t *testing.T) {
	for _, background := range []string{"", "backdrop", "background-video"} {
		t.Run("background-"+background, func(t *testing.T) {
			s, f, _, paths := sceneSetup(t, model.StageSettings{BackgroundCueID: background, BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 2.5})
			if background != "" {
				eventually(t, func() bool { return f.latest().BackgroundPath == paths[background] })
			}
			video := scenePlaying(t, s, f, "video")
			before := s.Snapshot(true)
			r := PlayRequest{RequestID: "video-second-press", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "video"}
			ack, err := s.Play(r)
			if err != nil {
				t.Fatal(err)
			}
			paused := f.latest()
			if !paused.ForegroundPaused || paused.ForegroundID != video.ForegroundID || paused.BackgroundPath != paths[background] || !paused.StageEnabled || paused.HardStop || paused.FadeSeconds != 2.5 || s.Snapshot(true).StopEpoch != before.StopEpoch {
				t.Fatalf("pause changed presentation: %+v", paused)
			}
			if retry, err := s.Play(r); err != nil || !retry.Duplicate || retry.TransportRevision != ack.TransportRevision || f.latest().Revision != paused.Revision {
				t.Fatalf("pause retry changed intent: %+v %v", retry, err)
			}
			s.nativeEvent(playback.Event{Kind: "paused", Generation: paused.ForegroundID, TransportRevision: paused.TransportRevision, Position: 8, Duration: 30})
			s.nativeEvent(playback.Event{Kind: "ended", Generation: video.ForegroundID, TransportRevision: video.TransportRevision})
			if state := s.Snapshot(true); state.State != "paused" || state.ActiveCueID != "video" || state.Elapsed != 8 {
				t.Fatalf("late end displaced pause: %+v", state)
			}
			r.RequestID = "video-third-press"
			if _, err := s.Play(r); err != nil {
				t.Fatal(err)
			}
			resumed := f.latest()
			if resumed.ForegroundPaused || resumed.ForegroundID != video.ForegroundID || resumed.SeekRevision != 0 {
				t.Fatalf("resume restarted video: %+v", resumed)
			}
			s.nativeEvent(playback.Event{Kind: "playing", Generation: resumed.ForegroundID, TransportRevision: resumed.TransportRevision, Position: 8, Duration: 30})
			if state := s.Snapshot(true); state.State != "playing" || state.Elapsed != 8 {
				t.Fatalf("resume lost position: %+v", state)
			}
			if _, err := s.Stop(StopRequest{"video-explicit-stop"}); err != nil {
				t.Fatal(err)
			}
			if scene := f.latest(); scene.ForegroundID != 0 || scene.BackgroundPath != paths[background] || !scene.StageEnabled {
				t.Fatalf("STOP lost background: %+v", scene)
			}
		})
	}
}

func TestSceneSelectedImageToggleKeepsIndependentMusic(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{BackgroundCueID: "backdrop", FadeEnabled: true, FadeSeconds: 1.3})
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["backdrop"] })
	music := scenePlaying(t, s, f, "music")
	if _, err := play(s, "image", "image-first-press"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	image := f.latest()
	sceneStageCompletion(t, s, f, image)
	s.nativeEvent(playback.Event{Kind: "progress", Generation: music.ForegroundID, SceneRevision: image.Revision, TransportRevision: image.TransportRevision, StageEnabled: true, Position: 8})
	before := s.Snapshot(true)
	r := PlayRequest{RequestID: "image-second-press", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "image"}
	ack, err := s.Play(r)
	if err != nil {
		t.Fatal(err)
	}
	toggled := f.latest()
	state := s.Snapshot(true)
	if toggled.ImagePath != "" || toggled.ForegroundID != music.ForegroundID || toggled.ForegroundPath != paths["music"] || toggled.BackgroundPath != paths["backdrop"] || !toggled.StageEnabled || toggled.FadeSeconds != 1.3 || state.ImageCueID != "" || state.ActiveCueID != "music" || state.State != "playing" || state.Elapsed != 8 || state.Generation != before.Generation || state.StopEpoch != before.StopEpoch+1 {
		t.Fatalf("image toggle interrupted music or lost the backdrop: scene=%+v state=%+v", toggled, state)
	}
	if retry, err := s.Play(r); err != nil || !retry.Duplicate || retry.StopEpoch != ack.StopEpoch || f.latest().Revision != toggled.Revision {
		t.Fatalf("duplicate image toggle changed playback: ack=%+v error=%v", retry, err)
	}
	r.RequestID = "old-image-press"
	if _, err := s.Play(r); err == nil {
		t.Fatal("a delayed image press reselected the removed image")
	}
	// Progress queued before the image disappeared still belongs to this music.
	s.nativeEvent(playback.Event{Kind: "progress", Generation: music.ForegroundID, SceneRevision: image.Revision, TransportRevision: image.TransportRevision, StageEnabled: true, Position: 9})
	if state := s.Snapshot(true); state.Elapsed != 9 || state.ActiveCueID != "music" || state.ImageCueID != "" {
		t.Fatalf("image removal invalidated independent music: %+v", state)
	}
}

func TestSceneSelectedImageToggleDoesNotRevealCoveredVideo(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{FadeSeconds: 1})
	scenePlaying(t, s, f, "video")
	if _, err := play(s, "image", "cover-video-with-image"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	if _, err := play(s, "image", "remove-covering-image"); err != nil {
		t.Fatal(err)
	}
	if scene := f.latest(); scene.ImagePath != "" || scene.ForegroundID != 0 || scene.BackgroundPath != "" || !scene.StageEnabled || scene.HardStop || scene.FadeSeconds != 0 {
		t.Fatalf("image toggle revealed the covered video instead of black: %+v", scene)
	}
}

func TestSceneHiddenSelectedImagePressShowsItAgain(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{FadeSeconds: 1})
	music := scenePlaying(t, s, f, "music")
	if _, err := play(s, "image", "show-before-stage-off"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	sceneStageCompletion(t, s, f, f.latest())
	if err := s.Stage(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot(true)
	// Re-select even before the native stage-off acknowledgement arrives.
	// Desired visibility, not the older completion, decides whether to toggle.
	if _, err := play(s, "image", "reselect-hidden-image"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().StageEnabled })
	if scene, state := f.latest(), s.Snapshot(true); scene.ImagePath != paths["image"] || scene.ForegroundID != music.ForegroundID || state.ImageCueID != "image" || state.StopEpoch != before.StopEpoch || state.ActiveCueID != "music" {
		t.Fatalf("pressing a hidden image did not restore it with music intact: scene=%+v state=%+v", scene, state)
	}
}

func TestSceneVideoPausePreservesItsImageOverlay(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{BackgroundCueID: "backdrop", FadeSeconds: 1})
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["backdrop"] })
	scenePlaying(t, s, f, "video")
	if _, err := play(s, "image", "image-over-selected-video"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	if _, err := play(s, "video", "toggle-covered-video"); err != nil {
		t.Fatal(err)
	}
	if scene := f.latest(); scene.ForegroundID == 0 || !scene.ForegroundPaused || scene.ImagePath != paths["image"] || scene.BackgroundPath != paths["backdrop"] || !scene.StageEnabled {
		t.Fatalf("video pause disturbed its image overlay: %+v", scene)
	}
}

func TestSceneSecondPressCancelsPendingVisualAndLatePreparation(t *testing.T) {
	for _, cue := range []string{"image"} {
		t.Run(cue, func(t *testing.T) {
			s, f, _, paths := sceneSetup(t, model.StageSettings{FadeEnabled: true, FadeSeconds: 1})
			if err := s.Stage(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			sceneStageCompletion(t, s, f, f.latest())
			music := scenePlaying(t, s, f, "music")
			gate := &sceneInspectGate{path: paths[cue], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
			f.sceneMu.Lock()
			f.gate = gate
			f.sceneMu.Unlock()
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
			before := s.Snapshot(true)
			r := PlayRequest{RequestID: "pending-first-press", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: cue}
			accepted, err := s.Play(r)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-gate.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("cue inspection did not start")
			}
			if retry, err := s.Play(r); err != nil || !retry.Duplicate || retry.Generation != accepted.Generation {
				t.Fatalf("duplicate pending press was not idempotent: ack=%+v error=%v", retry, err)
			}
			if s.Snapshot(true).StopEpoch != before.StopEpoch {
				t.Fatal("duplicate pending press stopped its cue")
			}
			r.RequestID = "pending-second-press"
			if _, err := s.Play(r); err != nil {
				t.Fatal(err)
			}
			expectedForeground := uint64(0)
			if cue == "image" {
				expectedForeground = music.ForegroundID
			}
			if scene := f.latest(); scene.ForegroundID != expectedForeground || scene.ImagePath != "" || !scene.StageEnabled {
				t.Fatalf("second pending press did not clear the visual: %+v", scene)
			}
			if state := s.Snapshot(true); cue == "image" && (state.Generation != before.Generation || state.ActiveCueID != "music" || state.State != "playing") {
				t.Fatalf("pending image cancellation interrupted independent music: %+v", state)
			}
			release.Do(func() { close(gate.release) })
			<-gate.completed
			// Drain the same worker with another intentional cue, so the full
			// scene history can prove the cancelled work never reached native.
			if cue == "image" {
				if _, err := play(s, "backdrop", "next-intentional-image"); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { return f.latest().ImagePath == paths["backdrop"] })
			} else {
				scenePlaying(t, s, f, "music")
			}
			f.sceneMu.Lock()
			defer f.sceneMu.Unlock()
			for _, scene := range f.scenes {
				if scene.ForegroundPath == paths[cue] || scene.ImagePath == paths[cue] {
					t.Fatalf("cancelled visual preparation was applied late: %+v", scene)
				}
			}
		})
	}
}

func TestSceneBackgroundButtonRemainsSelectedOnSecondPress(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{BackgroundCueID: "background-video", FadeSeconds: 1})
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
	before := s.Snapshot(true)
	if _, err := play(s, "background-video", "reselect-background"); err != nil {
		t.Fatal(err)
	}
	if state := s.Snapshot(true); state.BackgroundCueID != "background-video" || state.StopEpoch != before.StopEpoch || s.Playlist().Stage.BackgroundCueID != "background-video" {
		t.Fatalf("second background press disabled or changed the saved background: %+v", state)
	}
}

func TestSceneMusicPausesRegardlessOfLegacyToggleSetting(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			s, f, _, paths := sceneSetup(t, model.StageSettings{ToggleAudio: enabled, FadeSeconds: 1})
			music := scenePlaying(t, s, f, "music")
			if _, err := play(s, "music", "second-music-press"); err != nil {
				t.Fatal(err)
			}
			if scene := f.latest(); !scene.ForegroundPaused || scene.ForegroundID != music.ForegroundID || scene.ForegroundPath != paths["music"] || s.Snapshot(true).ActiveCueID != "music" {
				t.Fatalf("music did not pause in place: %+v", scene)
			}
		})
	}
}
