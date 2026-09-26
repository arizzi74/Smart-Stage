package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"smartstage/internal/files"
	"smartstage/internal/model"
	"smartstage/internal/playback"
)

// sceneNative records coordinator intent only. Tests explicitly deliver native
// completions; they do not establish image rendering, mixing, or audible fades.
type sceneNative struct {
	fakeNative
	sceneMu sync.Mutex
	scenes  []playback.Scene
	gate    *sceneInspectGate
}

type sceneInspectGate struct {
	path                        string
	entered, release, completed chan struct{}
	once                        sync.Once
}

func (f *sceneNative) ApplyScene(scene playback.Scene) error {
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	f.scenes = append(f.scenes, scene)
	return nil
}

func (f *sceneNative) Inspect(_ context.Context, path string) (playback.Media, error) {
	f.sceneMu.Lock()
	gate := f.gate
	f.sceneMu.Unlock()
	if gate != nil && gate.path == path {
		gate.once.Do(func() {
			close(gate.entered)
			// Deliberately return a successful late decoder result after STOP,
			// even though the production inspector should honor cancellation.
			<-gate.release
			close(gate.completed)
		})
	}
	switch filepath.Ext(path) {
	case ".wav":
		return playback.Media{Kind: "audio", HasAudio: true, Duration: 30}, nil
	case ".mp4":
		return playback.Media{Kind: "video", HasVideo: true, HasAudio: true, Duration: 12}, nil
	case ".png":
		return playback.Media{Kind: "image"}, nil
	default:
		return playback.Media{}, errors.New("test fixture has an unsupported extension")
	}
}

func (f *sceneNative) latest() playback.Scene {
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	if len(f.scenes) == 0 {
		return playback.Scene{}
	}
	return f.scenes[len(f.scenes)-1]
}

func sceneSetup(t *testing.T, settings model.StageSettings) (*Service, *sceneNative, *memoryStore, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	// macOS TempDir commonly uses /var, whose real path is /private/var.
	// Compare and gate the canonical paths supplied to native inspection.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	config := model.DefaultConfig()
	config.Outputs = model.Outputs{AudioID: "default", DisplayID: "screen", AllowPrimary: true}
	config.Stage = settings
	for _, item := range []struct{ id, name string }{{"music", "music.wav"}, {"next", "next.wav"}, {"image", "image.png"}, {"backdrop", "backdrop.png"}, {"background-video", "background.mp4"}, {"video", "foreground.mp4"}} {
		path := filepath.Join(dir, item.name)
		if err := os.WriteFile(path, []byte("test-only source, interpreted by sceneNative"), 0600); err != nil {
			t.Fatal(err)
		}
		paths[item.id] = path
		config.Cues = append(config.Cues, model.Cue{ID: item.id, Label: item.id, Path: path, Background: item.id == "background-video"})
	}
	browser, err := files.New([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	f := &sceneNative{fakeNative: fakeNative{events: make(chan playback.Event, 128)}}
	store := &memoryStore{}
	s := New(f, browser, store, config)
	t.Cleanup(s.Close)
	return s, f, store, paths
}

func scenePlaying(t *testing.T, s *Service, f *sceneNative, cue string) playback.Scene {
	t.Helper()
	before := f.latest()
	if _, err := play(s, cue, "play-"+cue+"-"+strconv.FormatInt(time.Now().UnixNano(), 10)); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		next := f.latest()
		return next.ForegroundID != 0 && next.ForegroundID != before.ForegroundID
	})
	scene := f.latest()
	f.events <- playback.Event{Kind: "playing", Generation: scene.ForegroundID, SceneRevision: scene.Revision, Duration: 30, StageEnabled: scene.StageEnabled}
	eventually(t, func() bool { return s.Snapshot(true).State == "playing" })
	return scene
}

func sceneStageCompletion(t *testing.T, s *Service, f *sceneNative, scene playback.Scene) {
	t.Helper()
	f.events <- playback.Event{Kind: "stage", Generation: scene.Generation, SceneRevision: scene.Revision, StageEnabled: scene.StageEnabled}
	eventually(t, func() bool { return s.Snapshot(true).StageEnabled == scene.StageEnabled })
}

func TestSceneImageAndStageChangesKeepForegroundIdentity(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{FadeSeconds: 1})
	music := scenePlaying(t, s, f, "music")
	if _, err := play(s, "image", "show-image-over-music"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	image := f.latest()
	sceneStageCompletion(t, s, f, image)
	if state := s.Snapshot(true); image.ForegroundID != music.ForegroundID || state.ActiveCueID != "music" || state.ImageCueID != "image" || state.State != "playing" {
		t.Fatalf("image replaced the running music: scene=%+v state=%+v", image, state)
	}
	if err := s.Stage(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	off := f.latest()
	sceneStageCompletion(t, s, f, off)
	if off.StageEnabled || off.ForegroundID != music.ForegroundID || off.ForegroundPath != paths["music"] || off.ImagePath != paths["image"] {
		t.Fatalf("stage off changed the foreground or discarded the image: %+v", off)
	}
	// An older native stage-open completion must not reopen an explicitly
	// closed stage, even while foreground music keeps its older identity.
	s.nativeEvent(playback.Event{Kind: "stage", Generation: image.Generation, SceneRevision: image.Revision, StageEnabled: true})
	if s.Snapshot(true).StageEnabled {
		t.Fatal("stale stage completion reopened the display")
	}
	s.nativeEvent(playback.Event{Kind: "progress", Generation: music.ForegroundID, SceneRevision: image.Revision, StageEnabled: true, Position: 2})
	if s.Snapshot(true).StageEnabled {
		t.Fatal("late progress from continuing music reopened the independently closed stage")
	}
	if err := s.Stage(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	on := f.latest()
	sceneStageCompletion(t, s, f, on)
	if state := s.Snapshot(true); on.ForegroundID != music.ForegroundID || state.ActiveCueID != "music" || state.State != "playing" || state.ImageCueID != "image" {
		t.Fatalf("reopening stage restarted or deselected music: scene=%+v state=%+v", on, state)
	}
	if f.count() != 0 {
		t.Fatal("scene-capable backend also received a legacy Start")
	}
}

func TestSceneBackgroundSwitchStopToggleAndEmergency(t *testing.T) {
	settings := model.StageSettings{BackgroundCueID: "backdrop", BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 1.6, ToggleAudio: true}
	s, f, _, paths := sceneSetup(t, settings)
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["backdrop"] })
	if err := s.Stage(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	sceneStageCompletion(t, s, f, f.latest())
	music := scenePlaying(t, s, f, "music")
	if _, err := play(s, "background-video", "switch-runtime-background"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().BackgroundPath == paths["background-video"] })
	background := f.latest()
	if background.ForegroundID != music.ForegroundID || !background.BackgroundAudio || s.Snapshot(true).ActiveCueID != "music" {
		t.Fatalf("background switch replaced music: %+v", background)
	}
	if s.Playlist().Stage.BackgroundCueID != "backdrop" || s.Snapshot(true).BackgroundCueID != "background-video" {
		t.Fatal("runtime background button overwrote the saved default")
	}
	if _, err := play(s, "image", "image-before-stop"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	if _, err := s.Stop(StopRequest{RequestID: "return-to-background"}); err != nil {
		t.Fatal(err)
	}
	stopped := f.latest()
	if stopped.ForegroundID != 0 || stopped.ImagePath != "" || stopped.BackgroundPath != paths["background-video"] || !stopped.StageEnabled || stopped.HardStop || stopped.FadeSeconds != settings.FadeSeconds {
		t.Fatalf("STOP did not request the configured background transition: %+v", stopped)
	}
	f.events <- playback.Event{Kind: "stopped", Generation: stopped.Generation, SceneRevision: stopped.Revision, StageEnabled: true}
	eventually(t, func() bool { return s.Snapshot(true).State == "stopped" })
	music = scenePlaying(t, s, f, "music")
	if _, err := play(s, "image", "image-before-music-toggle"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.latest().ImagePath == paths["image"] })
	if _, err := play(s, "music", "toggle-selected-music"); err != nil {
		t.Fatal(err)
	}
	toggled := f.latest()
	if state := s.Snapshot(true); toggled.ForegroundID != 0 || toggled.ImagePath != paths["image"] || state.ImageCueID != "image" || state.ActiveCueID != "" {
		t.Fatalf("selected music toggle failed to retain image: scene=%+v state=%+v", toggled, state)
	}
	if _, err := s.EmergencyStop(StopRequest{RequestID: "emergency-during-transition"}); err != nil {
		t.Fatal(err)
	}
	emergency := f.latest()
	if !emergency.HardStop || emergency.FadeSeconds != 0 || emergency.StageEnabled || emergency.ForegroundID != 0 || emergency.ImagePath != "" {
		t.Fatalf("emergency stop was not immediate complete silence and stage off: %+v", emergency)
	}
	before := s.Snapshot(true)
	s.nativeEvent(playback.Event{Kind: "playing", Generation: music.ForegroundID, SceneRevision: music.Revision, StageEnabled: true})
	if state := s.Snapshot(true); state.ActiveCueID != "" || state.StageEnabled || state.Generation != before.Generation {
		t.Fatalf("late foreground completion undid emergency stop: %+v", state)
	}
}

func TestSceneStopCancelsLateReplacementInspection(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{FadeEnabled: true, FadeSeconds: 1})
	music := scenePlaying(t, s, f, "music")
	gate := &sceneInspectGate{path: paths["next"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "next", "slow-replacement"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement inspection did not start")
	}
	if f.latest().ForegroundID != music.ForegroundID {
		t.Fatal("old music was discarded before the replacement was prepared")
	}
	stop, err := s.Stop(StopRequest{RequestID: "stop-during-inspection"})
	if err != nil {
		t.Fatal(err)
	}
	if f.latest().ForegroundID != 0 {
		t.Fatal("STOP did not clear the desired foreground while inspection was blocked")
	}
	release.Do(func() { close(gate.release) })
	<-gate.completed
	// The next intentional foreground cue passes through the same serial
	// loader, proving the cancelled inspection returned before checking the
	// complete scene history. Only this new request may start foreground audio.
	resumed := scenePlaying(t, s, f, "music")
	s.nativeEvent(playback.Event{Kind: "playing", Generation: stop.Generation - 1, StageEnabled: true})
	if state := s.Snapshot(true); f.latest().ForegroundID != resumed.ForegroundID || state.ActiveCueID != "music" {
		t.Fatalf("cancelled replacement changed the later intentional cue: scene=%+v state=%+v", f.latest(), state)
	}
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	for _, scene := range f.scenes {
		if scene.ForegroundPath == paths["next"] {
			t.Fatalf("a late successful inspection submitted the cancelled replacement: %+v", scene)
		}
	}
}

func TestSceneSettingsAndCueFlagsSurviveUnrelatedEdits(t *testing.T) {
	settings := model.StageSettings{FadeSeconds: 1}
	s, _, store, _ := sceneSetup(t, settings)
	base := s.Playlist()
	edits := colorEdits(base)
	yes := true
	edits[0].Hidden = &yes
	edits[2].Background = &yes
	flagged, err := s.EditPlaylist(PlaylistEdit{ExpectedRevision: base.PlaylistRevision, Cues: edits})
	if err != nil {
		t.Fatal(err)
	}
	settings = model.StageSettings{BackgroundCueID: "image", BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 2.5, ToggleAudio: true}
	configured, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: flagged.PlaylistRevision, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	edits = colorEdits(configured) // Omits hidden/background intentionally.
	edits[0].Label = "Renamed hidden music"
	edits[0], edits[2] = edits[2], edits[0]
	renamed, err := s.EditPlaylist(PlaylistEdit{ExpectedRevision: configured.PlaylistRevision, Cues: edits})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Stage != settings || !renamed.Cues[0].Background || !renamed.Cues[2].Hidden {
		t.Fatalf("unrelated reorder/rename reset stage settings or cue flags: %+v", renamed)
	}
	if _, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: configured.PlaylistRevision, Settings: model.StageSettings{FadeSeconds: 1}}); err == nil {
		t.Fatal("stale settings revision overwrote a newer show")
	}
	store.fail = true
	if _, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: renamed.PlaylistRevision, Settings: model.StageSettings{FadeSeconds: 1}}); err == nil {
		t.Fatal("stage settings save failure was hidden")
	}
	if after := s.Playlist(); after.PlaylistRevision != renamed.PlaylistRevision || after.Stage != settings {
		t.Fatalf("failed settings write mutated the saved show: %+v", after)
	}
}

func TestSceneDeviceLossCancelsReplacementWithNewerGeneration(t *testing.T) {
	s, f, _, paths := sceneSetup(t, model.StageSettings{FadeSeconds: 1})
	music := scenePlaying(t, s, f, "music")
	gate := &sceneInspectGate{path: paths["next"], entered: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{})}
	f.sceneMu.Lock()
	f.gate = gate
	f.sceneMu.Unlock()
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	if _, err := play(s, "next", "replacement-before-device-loss"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement inspection did not start")
	}
	if s.Snapshot(true).Generation == music.ForegroundID {
		t.Fatal("test did not create a newer pending foreground generation")
	}
	s.nativeEvent(playback.Event{Kind: "device-lost", Generation: music.ForegroundID, SceneRevision: music.Revision, Message: "Test audio output disconnected"})
	if state := s.Snapshot(true); !state.OutputFault || state.State != "error" || state.ActiveCueID != "" || state.StageEnabled || !f.latest().HardStop {
		t.Fatalf("device loss was ignored during replacement inspection: scene=%+v state=%+v", f.latest(), state)
	}
	if _, err := play(s, "music", "blocked-until-output-reselected"); err == nil {
		t.Fatal("playback resumed before output reselection")
	}
	if err := s.ConfigureOutputs(context.Background(), s.Playlist().Outputs); err != nil {
		t.Fatal(err)
	}
	reset := f.latest()
	f.events <- playback.Event{Kind: "stopped", Generation: reset.Generation, SceneRevision: reset.Revision}
	eventually(t, func() bool { return s.Snapshot(true).State == "stopped" })
	release.Do(func() { close(gate.release) })
	<-gate.completed
	// A new deliberate request drains the same source-loader mailbox after the
	// cancelled inspection. The old replacement must never reach ApplyScene.
	scenePlaying(t, s, f, "music")
	f.sceneMu.Lock()
	defer f.sceneMu.Unlock()
	for _, scene := range f.scenes {
		if scene.ForegroundPath == paths["next"] {
			t.Fatalf("disconnected pending replacement reached native playback: %+v", scene)
		}
	}
}
