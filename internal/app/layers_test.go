package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func setAudioFlags(s *Service, cueID string, keep, mix bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == cueID {
			s.config.Cues[i].KeepVideo, s.config.Cues[i].MixAudio = keep, mix
		}
	}
}

func cueView(t *testing.T, s *Service, cueID string) CueView {
	t.Helper()
	for _, cue := range s.Snapshot(true).Cues {
		if cue.ID == cueID {
			return cue
		}
	}
	t.Fatalf("cue %q missing", cueID)
	return CueView{}
}

func layerRequest(t *testing.T, s *Service, cueID, action, id string) PlayRequest {
	t.Helper()
	state := s.Snapshot(true)
	cue := cueView(t, s, cueID)
	return PlayRequest{RequestID: id, InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: cueID, Generation: cue.Generation, TransportRevision: cue.TransportRevision, Action: action}
}

func startLayer(t *testing.T, s *Service, f *sceneNative, cueID string) playback.AudioLayer {
	t.Helper()
	if _, err := play(s, cueID, "layer-"+cueID+fmt.Sprint(time.Now().UnixNano())); err != nil {
		t.Fatal(err)
	}
	var layer playback.AudioLayer
	eventually(t, func() bool {
		cue := cueView(t, s, cueID)
		for _, candidate := range f.latest().AudioLayers {
			if candidate.ID == cue.Generation {
				layer = candidate
				return true
			}
		}
		return false
	})
	scene := f.latest()
	s.nativeEvent(playback.Event{Kind: "playing", Generation: layer.ID, TransportRevision: layer.TransportRevision, SceneRevision: scene.Revision, Duration: 30, StageEnabled: scene.StageEnabled})
	if cue := cueView(t, s, cueID); cue.State != "playing" || !cue.Selected {
		t.Fatalf("layer not selected: %+v", cue)
	}
	return layer
}

func TestKeepVideoReplacesSoundtrackAndMuteSurvivesPauseEndAndMix(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{BackgroundCueID: "background-video", BackgroundAudio: true, AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	transportEvent(s, video, "progress", 5)
	setAudioFlags(s, "music", true, false)
	layer := startLayer(t, s, f, "music")
	scene := f.latest()
	if scene.ForegroundID != video.ForegroundID || scene.TransportRevision != video.TransportRevision || !scene.ForegroundMuted || scene.BackgroundAudio || scene.ForegroundPaused {
		t.Fatalf("replacement restarted video or left soundtrack eligible: %+v", scene)
	}
	if cue := cueView(t, s, "video"); !cue.Selected || cue.Elapsed != 5 || cue.State != "playing" {
		t.Fatalf("retained video missing: %+v", cue)
	}
	if _, err := s.Play(layerRequest(t, s, "music", "pause", "pause-replacement-layer")); err != nil {
		t.Fatal(err)
	}
	paused := f.latest().AudioLayers[0]
	s.nativeEvent(playback.Event{Kind: "paused", Generation: paused.ID, TransportRevision: paused.TransportRevision, Position: 6})
	if scene := f.latest(); !scene.ForegroundMuted || !scene.AudioLayers[0].Paused || scene.ForegroundPaused {
		t.Fatalf("pausing layer changed video: %+v", scene)
	}
	// A delayed terminal callback from before pause must not remove the layer.
	s.nativeEvent(playback.Event{Kind: "ended", Generation: layer.ID, TransportRevision: layer.TransportRevision})
	if len(f.latest().AudioLayers) != 1 {
		t.Fatal("stale terminal callback removed replacement")
	}
	s.nativeEvent(playback.Event{Kind: "ended", Generation: paused.ID, TransportRevision: paused.TransportRevision})
	if scene := f.latest(); len(scene.AudioLayers) != 0 || scene.ForegroundID != video.ForegroundID || !scene.ForegroundMuted {
		t.Fatalf("ending layer restored soundtrack: %+v", scene)
	}
	setAudioFlags(s, "next", false, true)
	startLayer(t, s, f, "next")
	if !f.latest().ForegroundMuted {
		t.Fatal("Mix audio revived a previously muted soundtrack")
	}
}

func TestMixAudioKeepsVideoAndLayersWithIndependentTransportAndVolumes(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeEnabled: true, AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	setAudioFlags(s, "music", false, true) // Mix alone implies preserving video.
	setAudioFlags(s, "next", true, true)   // Mix takes precedence over KeepVideo.
	first := startLayer(t, s, f, "music")
	second := startLayer(t, s, f, "next")
	scene := f.latest()
	if scene.ForegroundID != video.ForegroundID || scene.ForegroundMuted || len(scene.AudioLayers) != 2 {
		t.Fatalf("mix replaced existing presentation: %+v", scene)
	}
	// Per-cue transport continues to address an older layer after another starts.
	stale := layerRequest(t, s, "music", "seek", "stale-first-layer-seek")
	if _, err := play(s, "music", "toggle-first-layer-pause"); err != nil {
		t.Fatal(err)
	}
	scene = f.latest()
	if !scene.AudioLayers[0].Paused || scene.AudioLayers[1].Paused || scene.AudioLayers[1].TransportRevision != second.TransportRevision || scene.ForegroundPaused || scene.TransportRevision != video.TransportRevision {
		t.Fatalf("pause leaked to another source: %+v", scene)
	}
	if state := s.Snapshot(true); state.Generation != first.ID || state.TransportRevision != scene.AudioLayers[0].TransportRevision || state.ActiveCueID != "music" {
		t.Fatalf("selected panel identity mismatched layer: %+v", state)
	}
	if scene.Generation < second.ID {
		t.Fatal("selecting an older layer moved command epoch backwards")
	}
	position := 9.0
	stale.Position = &position
	if _, err := s.Play(stale); err == nil {
		t.Fatal("stale per-layer seek accepted")
	}
	seek := layerRequest(t, s, "music", "seek", "seek-paused-first-layer")
	seek.Position = &position
	// A paused completion acknowledges the layer before it becomes seekable.
	paused := scene.AudioLayers[0]
	s.nativeEvent(playback.Event{Kind: "paused", Generation: paused.ID, TransportRevision: paused.TransportRevision, Position: 4, Duration: 30})
	if _, err := s.Play(seek); err != nil {
		t.Fatal(err)
	}
	scene = f.latest()
	if scene.AudioLayers[0].SeekSeconds != 9 || scene.AudioLayers[0].SeekRevision != 1 || scene.AudioLayers[1].SeekRevision != 0 || scene.SeekRevision != 0 {
		t.Fatalf("seek affected another timeline: %+v", scene)
	}
	level := .9
	if _, err := s.EditCueVolume(CueVolumeEdit{ExpectedRevision: s.Playlist().PlaylistRevision, CueID: "music", Volume: &level}); err != nil {
		t.Fatal(err)
	}
	scene = f.latest()
	if scene.AudioLayers[0].Volume != level || scene.AudioLayers[1].Volume != model.DefaultTrackVolume || scene.AudioLayers[0].ID != first.ID || scene.AudioLayers[0].SeekRevision != 1 {
		t.Fatalf("fader restarted a layer: %+v", scene)
	}
	beforeStage := scene
	if err := s.Stage(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	scene = f.latest()
	if scene.StageEnabled || scene.ForegroundID != video.ForegroundID || !reflect.DeepEqual(scene.AudioLayers, beforeStage.AudioLayers) {
		t.Fatalf("Stage off changed source transports: %+v", scene)
	}
}

func TestMixPreservesBackgroundSoundAndKeepVideoReplacesExistingMusic(t *testing.T) {
	for _, mix := range []bool{false, true} {
		t.Run(fmt.Sprint(mix), func(t *testing.T) {
			s, f, _, paths := sceneSetup(t, model.StageSettings{BackgroundCueID: "background-video", BackgroundAudio: true, AudioFadeSeconds: 1, VisualFadeSeconds: 1})
			eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
			if err := s.Stage(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			setAudioFlags(s, "music", !mix, mix)
			first := startLayer(t, s, f, "music")
			if scene := f.latest(); scene.BackgroundAudio != mix || scene.BackgroundPath != paths["background-video"] {
				t.Fatalf("wrong background soundtrack policy: %+v", scene)
			}
			setAudioFlags(s, "next", true, false)
			replacement := startLayer(t, s, f, "next")
			if scene := f.latest(); scene.ForegroundID != 0 || len(scene.AudioLayers) != 1 || scene.AudioLayers[0].ID != replacement.ID || cueView(t, s, "music").Selected {
				t.Fatalf("KeepVideo retained old music without a foreground video: %+v", scene)
			}
			s.nativeEvent(playback.Event{Kind: "playing", Generation: first.ID, TransportRevision: first.TransportRevision})
			if cueView(t, s, "music").Selected {
				t.Fatal("removed layer revived via late callback")
			}
		})
	}
}

func TestLayerSlowPreparationDoesNotCutSourcesAndStopWinsLateCompletion(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	setAudioFlags(s, "music", false, true)
	startLayer(t, s, f, "music")
	setAudioFlags(s, "next", true, false)
	gate := &sceneInspectGate{path: paths["next"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	before := f.latest()
	if _, err := play(s, "next", "slow-replacement-layer"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("inspection did not start")
	}
	if !reflect.DeepEqual(f.latest(), before) || !cueView(t, s, "video").Selected || !cueView(t, s, "music").Selected || !cueView(t, s, "next").Selected {
		t.Fatal("slow inspection cut a source or lost selection")
	}
	if _, err := play(s, "next", "pause-pending-replacement"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.latest(), before) || !cueView(t, s, "next").Paused {
		t.Fatal("pending pause affected a running source")
	}
	if _, err := s.Stop(StopRequest{RequestID: "stop-all-pending-layers"}); err != nil {
		t.Fatal(err)
	}
	stop := f.latest()
	if stop.ForegroundID != 0 || len(stop.AudioLayers) != 0 {
		t.Fatal("STOP retained sources")
	}
	release.Do(func() { close(gate.release) })
	<-gate.completed
	s.nativeEvent(playback.Event{Kind: "playing", Generation: video.ForegroundID, TransportRevision: video.TransportRevision})
	// The next job drains the worker and proves the cancelled successful decode
	// cannot append its old layer after STOP.
	setAudioFlags(s, "music", false, false)
	scenePlaying(t, s, f, "music")
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	for _, scene := range f.scenes {
		for _, layer := range scene.AudioLayers {
			if layer.Path == paths["next"] {
				t.Fatal("late inspection rearmed stopped layer")
			}
		}
	}
}

func TestConcurrentLayerTogglesAreUniqueAndStopRejectsStaleTransport(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	setAudioFlags(s, "music", false, true)
	startLayer(t, s, f, "music")
	stale := layerRequest(t, s, "music", "pause", "stale-layer-after-stop")
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := play(s, "music", fmt.Sprintf("concurrent-layer-toggle-%d", i))
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if scene := f.latest(); len(scene.AudioLayers) != 1 || scene.AudioLayers[0].Paused {
		t.Fatalf("toggles duplicated or unbalanced layer: %+v", scene)
	}
	if _, err := s.Stop(StopRequest{RequestID: "concurrent-layers-global-stop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Play(stale); err == nil {
		t.Fatal("STOP accepted stale layer transport")
	}
	for _, cue := range s.Snapshot(true).Cues {
		if cue.Selected {
			t.Fatalf("STOP retained selected cue: %+v", cue)
		}
	}
}

func TestAudioFlagsRoundTripEditExportDecodeLoadAndActiveProtection(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	config := s.Playlist()
	edit := PlaylistEdit{ExpectedRevision: config.PlaylistRevision}
	yes, no := true, false
	for _, cue := range config.Cues {
		item := CueEdit{ID: cue.ID, Path: cue.Path, Label: cue.Label}
		if cue.ID == "music" {
			item.KeepVideo, item.MixAudio = &yes, &yes
		}
		edit.Cues = append(edit.Cues, item)
	}
	if _, err := s.EditPlaylist(edit); err != nil {
		t.Fatal(err)
	}
	exported, err := s.ExportPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlaylist(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Cues[0].KeepVideo || !decoded.Cues[0].MixAudio {
		t.Fatal("saved playlist lost audio flags")
	}
	loaded, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: decoded})
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Cues[0].KeepVideo || !loaded.Cues[0].MixAudio {
		t.Fatal("load lost audio flags")
	}
	// Removing/replacing any active layer is rejected, even when another cue is
	// selected in the single legacy transport panel.
	firstID, nextID := loaded.Cues[0].ID, loaded.Cues[1].ID
	setAudioFlags(s, nextID, false, true)
	startLayer(t, s, f, firstID)
	startLayer(t, s, f, nextID)
	config = s.Playlist()
	edit = PlaylistEdit{ExpectedRevision: config.PlaylistRevision}
	for _, cue := range config.Cues {
		if cue.ID != firstID {
			edit.Cues = append(edit.Cues, CueEdit{ID: cue.ID, Path: cue.Path, Label: cue.Label})
		}
	}
	if _, err := s.EditPlaylist(edit); err == nil {
		t.Fatal("removal of retained active layer accepted")
	}
	// Explicit false survives an edit that otherwise omits optional settings.
	config = s.Playlist()
	edit = PlaylistEdit{ExpectedRevision: config.PlaylistRevision}
	for _, cue := range config.Cues {
		item := CueEdit{ID: cue.ID, Path: cue.Path, Label: cue.Label}
		if cue.ID == firstID {
			item.KeepVideo, item.MixAudio = &no, &no
		}
		edit.Cues = append(edit.Cues, item)
	}
	result, err := s.EditPlaylist(edit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cues[0].KeepVideo || result.Cues[0].MixAudio {
		t.Fatal("explicit false did not clear audio flags")
	}
}

func TestFailedLayerInspectionAndDecoderPreserveOtherSources(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	setAudioFlags(s, "music", false, true)
	first := startLayer(t, s, f, "music")
	setAudioFlags(s, "next", false, true)
	gate := &sceneInspectGate{path: paths["next"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), err: fmt.Errorf("test decoder rejected source")}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	before := f.latest()
	if _, err := play(s, "next", "failed-mixed-inspection"); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	close(gate.release)
	<-gate.completed
	eventually(t, func() bool { return s.Snapshot(true).LastError != "" })
	if !reflect.DeepEqual(before, f.latest()) || cueView(t, s, "next").Selected || !cueView(t, s, "music").Selected || !cueView(t, s, "video").Selected {
		t.Fatal("failed inspection interrupted existing sources")
	}
	f.sceneMu.Lock()
	f.gate = nil
	f.sceneMu.Unlock()
	// Revalidation permits an intentional retry after the failed inspection.
	s.mu.Lock()
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == "next" {
			s.config.Cues[i].Cache.Status = "unchecked"
		}
	}
	s.mu.Unlock()
	second := startLayer(t, s, f, "next")
	s.nativeEvent(playback.Event{Kind: "error", Generation: second.ID, TransportRevision: second.TransportRevision, SceneRevision: f.latest().Revision, Message: "layer decoder failed"})
	after := f.latest()
	if after.ForegroundID != video.ForegroundID || len(after.AudioLayers) != 1 || after.AudioLayers[0].ID != first.ID || cueView(t, s, "next").Selected || s.Snapshot(true).LastError != "layer decoder failed" {
		t.Fatalf("one decoder failure interrupted independent sources: %+v", after)
	}
	// Global errors use the scene's command epoch and zero transport revision,
	// distinct from a layer's exact transport even when their IDs coincide.
	s.nativeEvent(playback.Event{Kind: "error", Generation: after.Generation, SceneRevision: after.Revision, Message: "global scene failure"})
	if scene := f.latest(); !scene.HardStop || scene.ForegroundID != 0 || len(scene.AudioLayers) != 0 {
		t.Fatalf("global failure did not clear sources: %+v", scene)
	}
}

func TestUnflaggedAudioAndAnyVideoReplaceAllLayers(t *testing.T) {
	for _, replacement := range []string{"music", "video"} {
		t.Run(replacement, func(t *testing.T) {
			s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
			setAudioFlags(s, "next", false, true)
			old := startLayer(t, s, f, "next")
			if replacement == "video" {
				setAudioFlags(s, "video", true, true)
			}
			latest := scenePlaying(t, s, f, replacement)
			if len(latest.AudioLayers) != 0 || latest.ForegroundMuted || cueView(t, s, "next").Selected {
				t.Fatalf("replacement retained prior layers: %+v", latest)
			}
			before := f.latest()
			s.nativeEvent(playback.Event{Kind: "error", Generation: old.ID, TransportRevision: old.TransportRevision, SceneRevision: before.Revision, Message: "late disposed layer failure"})
			if !reflect.DeepEqual(before, f.latest()) {
				t.Fatal("disposed layer error cleared replacement")
			}
		})
	}
}

func TestLiveLayerKindRemainsAvailableAfterRevalidationChangesCache(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	setAudioFlags(s, "music", false, true)
	startLayer(t, s, f, "music")
	s.mu.Lock()
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == "music" {
			s.config.Cues[i].Cache.Media = playback.Media{}
			s.config.Cues[i].Cache.Status = "error"
		}
	}
	s.mu.Unlock()
	if cue := cueView(t, s, "music"); cue.Kind != "audio" || !cue.Selected {
		t.Fatalf("cached validation hid running source kind: %+v", cue)
	}
	if _, err := play(s, "music", "pause-invalid-but-running-layer"); err != nil {
		t.Fatal(err)
	}
	if !f.latest().AudioLayers[0].Paused {
		t.Fatal("running layer could not be paused after revalidation")
	}
}

func TestRapidMixedCuesRemainIndependentlyPendingAndAcceptOutOfOrderNativeCompletions(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	setAudioFlags(s, "music", false, true)
	setAudioFlags(s, "next", false, true)
	gate := &sceneInspectGate{path: paths["music"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "music", "rapid-first-mixed-cue"); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	if _, err := play(s, "next", "rapid-second-mixed-cue"); err != nil {
		t.Fatal(err)
	}
	first, second := cueView(t, s, "music"), cueView(t, s, "next")
	if !first.Selected || !second.Selected || first.State != "loading" || second.State != "loading" || first.Generation == second.Generation {
		t.Fatalf("independent pending additions lost: %+v %+v", first, second)
	}
	if _, err := play(s, "music", "pause-first-pending-mixed-cue"); err != nil {
		t.Fatal(err)
	}
	if !cueView(t, s, "music").Paused || cueView(t, s, "next").Paused {
		t.Fatal("pending pause leaked to another addition")
	}
	release.Do(func() { close(gate.release) })
	<-gate.completed
	eventually(t, func() bool { return len(f.latest().AudioLayers) == 2 })
	scene := f.latest()
	if scene.ForegroundID != video.ForegroundID || scene.ForegroundMuted {
		t.Fatalf("rapid mix replaced video: %+v", scene)
	}
	// Actual native players can become ready in either order even though host
	// inspection is serialized. Each completion updates only its own selection.
	secondLayer, firstLayer := scene.AudioLayers[1], scene.AudioLayers[0]
	s.nativeEvent(playback.Event{Kind: "playing", Generation: secondLayer.ID, TransportRevision: secondLayer.TransportRevision, SceneRevision: scene.Revision, StageEnabled: scene.StageEnabled})
	s.nativeEvent(playback.Event{Kind: "paused", Generation: firstLayer.ID, TransportRevision: firstLayer.TransportRevision, SceneRevision: scene.Revision, StageEnabled: scene.StageEnabled})
	if cueView(t, s, "music").State != "paused" || cueView(t, s, "next").State != "playing" || !cueView(t, s, "video").Selected {
		t.Fatal("out-of-order completion changed another source")
	}
	if _, err := s.Stop(StopRequest{RequestID: "stop-rapid-mixed-cues"}); err != nil {
		t.Fatal(err)
	}
	s.nativeEvent(playback.Event{Kind: "playing", Generation: firstLayer.ID, TransportRevision: firstLayer.TransportRevision})
	s.nativeEvent(playback.Event{Kind: "playing", Generation: secondLayer.ID, TransportRevision: secondLayer.TransportRevision})
	if scene := f.latest(); scene.ForegroundID != 0 || len(scene.AudioLayers) != 0 {
		t.Fatal("late native readiness rearmed a stopped layer")
	}
}

func TestStopAndNormalReplacementCancelEveryPendingMixedCue(t *testing.T) {
	for _, stop := range []bool{true, false} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
			setAudioFlags(s, "music", false, true)
			setAudioFlags(s, "next", false, true)
			gate := &sceneInspectGate{path: paths["music"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
			f.sceneMu.Lock()
			f.gate = gate
			f.sceneMu.Unlock()
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
			if _, err := play(s, "music", "pending-mix-before-clear-one"); err != nil {
				t.Fatal(err)
			}
			<-gate.entered
			if _, err := play(s, "next", "pending-mix-before-clear-two"); err != nil {
				t.Fatal(err)
			}
			first, second := cueView(t, s, "music"), cueView(t, s, "next")
			if stop {
				if _, err := s.Stop(StopRequest{RequestID: "stop-all-pending-mix"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := play(s, "video", "normal-replaces-pending-mix"); err != nil {
					t.Fatal(err)
				}
			}
			if cueView(t, s, "music").Selected || cueView(t, s, "next").Selected {
				t.Fatal("clear retained pending additions")
			}
			// Even readiness callbacks racing cancellation cannot become active state.
			s.nativeEvent(playback.Event{Kind: "playing", Generation: second.Generation, TransportRevision: second.TransportRevision})
			s.nativeEvent(playback.Event{Kind: "playing", Generation: first.Generation, TransportRevision: first.TransportRevision})
			release.Do(func() { close(gate.release) })
			<-gate.completed
			if stop {
				if _, err := play(s, "video", "new-video-after-pending-stop"); err != nil {
					t.Fatal(err)
				}
			}
			eventually(t, func() bool { return f.latest().ForegroundPath == paths["video"] })
			f.sceneMu.Lock()
			defer f.sceneMu.Unlock()
			for _, scene := range f.scenes {
				if len(scene.AudioLayers) != 0 {
					t.Fatalf("cancelled mix prepared after clear: %+v", scene)
				}
			}
		})
	}
}

func TestPreparationTimeoutDiscardsOnlyItsOwnPendingMix(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	video := scenePlaying(t, s, f, "video")
	setAudioFlags(s, "music", false, true)
	gate := &sceneInspectGate{path: paths["music"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "music", "timeout-one-mixed-addition"); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	id := cueView(t, s, "music").Generation
	// Drive the same deadline path without waiting 35 wall-clock seconds. The
	// deliberately blocked inspector models a decoder that returns after expiry.
	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	s.watchPreparation(deadline, id)
	if cueView(t, s, "music").Selected || f.latest().ForegroundID != video.ForegroundID || s.Snapshot(true).LastError != "Cue preparation timed out" {
		t.Fatal("deadline cut video or retained pending cue")
	}
	release.Do(func() { close(gate.release) })
	<-gate.completed
	if _, err := play(s, "next", "drain-worker-after-timeout"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ForegroundPath == paths["next"] })
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	for _, scene := range f.scenes {
		if len(scene.AudioLayers) != 0 {
			t.Fatal("late timed-out inspection created layer")
		}
	}
}

func TestExpiredPreparationMailboxCannotBlockStop(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	setAudioFlags(s, "music", false, true)
	setAudioFlags(s, "next", false, true)
	gate := &sceneInspectGate{path: paths["music"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "music", "block-inspector-mailbox"); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// Deadlines can remove pending identities while cancelled work awaits the
	// single inspector. Saturate those leftovers to test command responsiveness.
	for i := 0; i < cap(s.mixLoads); i++ {
		s.mixLoads <- loadJob{ctx: expired, generation: uint64(1000 + i)}
	}
	done := make(chan error, 1)
	go func() {
		_, err := play(s, "next", "enqueue-after-expired-mailbox")
		if err == nil {
			_, err = s.Stop(StopRequest{RequestID: "stop-full-expired-mailbox"})
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("full preparation mailbox blocked STOP")
	}
	release.Do(func() { close(gate.release) })
	<-gate.completed
}

func TestActiveAndPendingLayersGuardSavedOperationsAndEscapeClearsThem(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	setAudioFlags(s, "music", false, true)
	setAudioFlags(s, "next", false, true)
	startLayer(t, s, f, "music")
	exported, err := s.ExportPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	gate := &sceneInspectGate{path: paths["next"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "next", "pending-layer-saved-operation-guards"); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	if _, err := s.ReserveUpdate(); err == nil {
		t.Fatal("update allowed with layers")
	}
	if _, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: exported}); err == nil {
		t.Fatal("playlist replacement allowed with layers")
	}
	if err := s.ConfigureOutputs(context.Background(), s.Playlist().Outputs); err == nil {
		t.Fatal("output change allowed with layers")
	}
	stale := layerRequest(t, s, "music", "pause", "stale-layer-after-escape")
	s.nativeEvent(playback.Event{Kind: "escape", Generation: stale.Generation})
	if scene := f.latest(); scene.ForegroundID != 0 || len(scene.AudioLayers) != 0 || !scene.HardStop || scene.StageEnabled {
		t.Fatalf("escape retained layers: %+v", scene)
	}
	if _, err := s.Play(stale); err == nil {
		t.Fatal("escape accepted stale layer transport")
	}
	s.nativeEvent(playback.Event{Kind: "stopped", Generation: f.latest().Generation, SceneRevision: f.latest().Revision})
	release.Do(func() { close(gate.release) })
	<-gate.completed
	// Once stopped, the existing update/load/output operations can proceed and
	// queued decoder success cannot reinstall a source into the newly loaded show.
	undo, err := s.ReserveUpdate()
	if err != nil {
		t.Fatal(err)
	}
	undo()
	loaded, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: exported})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureOutputs(context.Background(), loaded.Outputs); err != nil {
		t.Fatal(err)
	}
	if scene := f.latest(); scene.ForegroundID != 0 || len(scene.AudioLayers) != 0 {
		t.Fatal("saved operation revived old layers")
	}
}

func TestFailedMixedInspectionDoesNotCancelAnotherPendingAddition(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	setAudioFlags(s, "music", false, true)
	setAudioFlags(s, "next", false, true)
	gate := &sceneInspectGate{path: paths["music"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), err: fmt.Errorf("first decoder failed")}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "music", "failed-first-pending-mix"); err != nil {
		t.Fatal(err)
	}
	<-gate.entered
	if _, err := play(s, "next", "retained-second-pending-mix"); err != nil {
		t.Fatal(err)
	}
	if len(f.latest().AudioLayers) != 0 {
		t.Fatal("a second host inspection bypassed the occupied worker")
	}
	release.Do(func() { close(gate.release) })
	<-gate.completed
	eventually(t, func() bool { return len(f.latest().AudioLayers) == 1 })
	if scene := f.latest(); scene.AudioLayers[0].Path != paths["next"] || cueView(t, s, "music").Selected || !cueView(t, s, "next").Selected {
		t.Fatalf("one failed inspection cancelled another addition: %+v", scene)
	}
}

func TestMixRetainsPrimaryMusicAndItsPauseDoesNotPauseAddedAudio(t *testing.T) {
	s, f, _, _ := sceneSetup(t, model.StageSettings{AudioFadeSeconds: 1, VisualFadeSeconds: 1})
	music := scenePlaying(t, s, f, "music")
	setAudioFlags(s, "next", false, true)
	added := startLayer(t, s, f, "next")
	if scene := f.latest(); scene.ForegroundID != music.ForegroundID || scene.ForegroundMuted || len(scene.AudioLayers) != 1 {
		t.Fatalf("mix replaced existing music: %+v", scene)
	}
	if _, err := play(s, "music", "pause-primary-music-with-mix"); err != nil {
		t.Fatal(err)
	}
	scene := f.latest()
	if !scene.ForegroundPaused || scene.AudioLayers[0].Paused || scene.AudioLayers[0].TransportRevision != added.TransportRevision {
		t.Fatal("primary pause affected added audio")
	}
	s.nativeEvent(playback.Event{Kind: "ended", Generation: added.ID, TransportRevision: added.TransportRevision})
	if scene := f.latest(); scene.ForegroundID != music.ForegroundID || !scene.ForegroundPaused || len(scene.AudioLayers) != 0 || !cueView(t, s, "music").Selected {
		t.Fatal("added audio ending discarded paused primary music")
	}
}
