package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

type playlistFileStore struct {
	mu      sync.Mutex
	saved   []model.Config
	fail    bool
	entered chan struct{}
	release chan struct{}
}

func (p *playlistFileStore) Save(config model.Config) error {
	if p.entered != nil {
		close(p.entered)
		<-p.release
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return errors.New("test disk is full")
	}
	p.saved = append(p.saved, config.Clone())
	return nil
}

func playlistDocument(t *testing.T, s *Service) PlaylistFile {
	t.Helper()
	file, err := s.ExportPlaylist()
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestPlaylistFileRoundTripKeepsShowAndLocalOutputs(t *testing.T) {
	s, _, _, _ := sceneSetup(t, model.StageSettings{FadeSeconds: 1})
	file := playlistDocument(t, s)
	file.Cues[0].Label = "Music for act two"
	file.Cues[0].Color = "#aB1234"
	file.Cues[1].Hidden = true
	file.Cues[4].Background = true
	file.Stage = model.StageSettings{BackgroundCueID: file.Cues[4].ID, BackgroundAudio: true, FadeEnabled: true, FadeSeconds: 2.5, ToggleAudio: true}
	// Order is part of the file, even when the same source appears repeatedly.
	file.Cues[0], file.Cues[1] = file.Cues[1], file.Cues[0]
	file.Cues = append(file.Cues, PlaylistFileCue{ID: "repeat-music", Label: "Music again", Path: file.Cues[1].Path})
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"cache", "outputs", "playlistRevision", "instanceId", "token", "validation", "duration", "generation"} {
		if bytes.Contains(data, []byte(`"`+forbidden+`"`)) {
			t.Fatalf("playlist exported application-only field %s: %s", forbidden, data)
		}
	}
	decoded, err := DecodePlaylist(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, file) {
		t.Fatalf("round trip changed the file: got %+v, want %+v", decoded, file)
	}
	before, oldState := s.Playlist(), s.Snapshot(true)
	persistence := &playlistFileStore{}
	s.store = persistence
	loaded, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: before.PlaylistRevision, Playlist: decoded})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Outputs != before.Outputs || loaded.Schema != before.Schema || loaded.PlaylistRevision != before.PlaylistRevision+1 {
		t.Fatalf("local settings or monotonic revision lost: %+v", loaded)
	}
	if len(loaded.Cues) != len(decoded.Cues) {
		t.Fatalf("loaded %d cues, expected %d", len(loaded.Cues), len(decoded.Cues))
	}
	ids := map[string]bool{}
	for i, cue := range loaded.Cues {
		want := decoded.Cues[i]
		for _, old := range decoded.Cues {
			if cue.ID == old.ID {
				t.Fatal("load reused a file's cue ID")
			}
		}
		if cue.ID == "" || ids[cue.ID] || cue.Label != want.Label || cue.Path != want.Path || cue.Color != want.Color || cue.Hidden != want.Hidden || cue.Background != want.Background || cue.Cache != (model.Validation{Status: "unchecked"}) {
			t.Fatalf("cue %d lost presentation metadata or trusted cached validation: %+v", i, cue)
		}
		ids[cue.ID] = true
	}
	wantStage := decoded.Stage
	wantStage.BackgroundCueID = loaded.Cues[4].ID
	if loaded.Stage != wantStage {
		t.Fatalf("stage selection/settings not remapped: got %+v want %+v", loaded.Stage, wantStage)
	}
	persistence.mu.Lock()
	if len(persistence.saved) != 1 || !reflect.DeepEqual(persistence.saved[0], loaded) {
		t.Fatal("playlist was not saved in a single complete transaction")
	}
	persistence.mu.Unlock()
	state := s.Snapshot(true)
	if state.StopEpoch <= oldState.StopEpoch || state.Generation <= oldState.Generation || state.StageEnabled || state.ActiveCueID != "" {
		t.Fatalf("loaded playlist retained live show state: %+v", state)
	}
	if _, err := s.Play(PlayRequest{RequestID: "before-playlist-load", InstanceID: oldState.InstanceID, StopEpoch: oldState.StopEpoch, CueID: loaded.Cues[0].ID}); err == nil {
		t.Fatal("a controller request from before load crossed the playlist boundary")
	}
	eventually(t, func() bool {
		for _, cue := range s.Playlist().Cues {
			if cue.Cache.Status != "ready" {
				return false
			}
		}
		return true
	})
}

func TestDecodePlaylistRejectsUnsupportedMalformedAndOversizeFiles(t *testing.T) {
	s, _, _ := setup(t, false)
	valid := playlistDocument(t, s)
	encode := func(file PlaylistFile) string { data, _ := json.Marshal(file); return string(data) }
	data := encode(valid)
	cases := map[string]string{
		"empty": "", "null": "null", "unrelated": `{}`, "malformed": `{`,
		"trailing document": data + `{}`, "trailing garbage": data + `oops`,
		"unknown field": strings.Replace(data, `"format":`, `"token":"not-a-playlist-field","format":`, 1),
		"cache field":   strings.Replace(data, `"label":`, `"cache":{"status":"ready"},"label":`, 1),
		"too large":     strings.Repeat(" ", MaxPlaylistFileBytes+1),
	}
	for name, change := range map[string]func(*PlaylistFile){
		"wrong format":       func(f *PlaylistFile) { f.Format = "another-application" },
		"future version":     func(f *PlaylistFile) { f.Version++ },
		"missing cues":       func(f *PlaylistFile) { f.Cues = nil },
		"duplicate ids":      func(f *PlaylistFile) { f.Cues[1].ID = f.Cues[0].ID },
		"empty id":           func(f *PlaylistFile) { f.Cues[0].ID = "" },
		"empty label":        func(f *PlaylistFile) { f.Cues[0].Label = " " },
		"newline label":      func(f *PlaylistFile) { f.Cues[0].Label = "cue\nline" },
		"invalid color":      func(f *PlaylistFile) { f.Cues[0].Color = "red; position:fixed" },
		"relative path":      func(f *PlaylistFile) { f.Cues[0].Path = "song.wav" },
		"URL path":           func(f *PlaylistFile) { f.Cues[0].Path = "https://example.invalid/song.wav" },
		"NUL path":           func(f *PlaylistFile) { f.Cues[0].Path += "\x00" },
		"foreign background": func(f *PlaylistFile) { f.Stage.BackgroundCueID = "not-in-cues" },
		"bad fade":           func(f *PlaylistFile) { f.Stage.FadeSeconds = 31 },
		"too many cues":      func(f *PlaylistFile) { f.Cues = make([]PlaylistFileCue, model.MaxCues+1) },
	} {
		file := valid
		file.Cues = append([]PlaylistFileCue{}, valid.Cues...)
		change(&file)
		cases[name] = encode(file)
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePlaylist(strings.NewReader(input)); err == nil {
				t.Fatal("invalid playlist file was accepted")
			}
		})
	}
	if _, err := DecodePlaylist(strings.NewReader(data + "\n \t")); err != nil {
		t.Fatal("trailing whitespace rejected:", err)
	}
}

func TestLoadPlaylistRejectsInvalidMediaWithoutChangingCurrentShow(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "outside roots", "invalid metadata", "stale revision", "save failure"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _ := setup(t, false)
			before, state := s.Playlist(), s.Snapshot(true)
			file := playlistDocument(t, s)
			file.Cues[0].Label = "Would replace the show"
			persistence := &playlistFileStore{}
			s.store = persistence
			load := PlaylistLoad{ExpectedRevision: before.PlaylistRevision, Playlist: file}
			switch kind {
			case "missing":
				load.Playlist.Cues[1].Path = filepath.Join(filepath.Dir(file.Cues[0].Path), "deleted.wav")
			case "directory":
				load.Playlist.Cues[1].Path = filepath.Dir(file.Cues[0].Path)
			case "outside roots":
				outside := filepath.Join(t.TempDir(), "outside.wav")
				if err := os.WriteFile(outside, []byte("outside configured media roots"), 0600); err != nil {
					t.Fatal(err)
				}
				load.Playlist.Cues[1].Path = outside
			case "invalid metadata":
				load.Playlist.Cues[1].Color = "invalid"
			case "stale revision":
				load.ExpectedRevision++
			case "save failure":
				persistence.fail = true
			}
			if _, err := s.LoadPlaylist(load); err == nil {
				t.Fatal("invalid import succeeded")
			}
			if !reflect.DeepEqual(s.Playlist(), before) || !reflect.DeepEqual(s.Snapshot(true), state) {
				t.Fatal("rejected load changed the current playlist or playback state")
			}
			persistence.mu.Lock()
			defer persistence.mu.Unlock()
			if len(persistence.saved) != 0 {
				t.Fatal("invalid input reached durable persistence")
			}
		})
	}
}

func TestLoadPlaylistRejectsPlaybackStageAndUpdateTransitions(t *testing.T) {
	for _, mode := range []string{"loading", "playing", "stopping", "stage enabled", "stage desired", "stage pending", "image pending", "updating", "closed"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := setup(t, false)
			file := playlistDocument(t, s)
			before := s.Playlist()
			s.mu.Lock()
			switch mode {
			case "loading", "playing", "stopping":
				s.state.State = mode
			case "stage enabled":
				s.state.StageEnabled = true
			case "stage desired":
				s.stageDesired = true
			case "stage pending":
				s.stageEnablePending = true
			case "image pending":
				s.pendingImageID = "a"
			case "updating":
				s.state.UpdatePending = true
			case "closed":
				s.closed = true
			}
			s.mu.Unlock()
			_, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: before.PlaylistRevision, Playlist: file})
			if mode == "closed" { // allow the fixture's normal cleanup to cancel workers
				s.mu.Lock()
				s.closed = false
				s.mu.Unlock()
			}
			if err == nil || !reflect.DeepEqual(s.Playlist(), before) {
				t.Fatal("load changed a running or reserved show")
			}
		})
	}
}

func TestLoadingPlaylistBlocksPlayAndStageButNeverSTOP(t *testing.T) {
	s, _, _, _ := sceneSetup(t, model.StageSettings{FadeSeconds: 1})
	file := playlistDocument(t, s)
	persistence := &playlistFileStore{entered: make(chan struct{}), release: make(chan struct{})}
	s.store = persistence
	done := make(chan error, 1)
	go func() {
		_, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: file})
		done <- err
	}()
	<-persistence.entered
	var release sync.Once
	unblock := func() { release.Do(func() { close(persistence.release) }) }
	defer unblock()
	for _, cue := range []string{"music", "image", "background-video"} {
		if _, err := play(s, cue, "during-playlist-load-"+cue); err == nil {
			t.Fatalf("%s cue was accepted while replacing the playlist", cue)
		}
	}
	if err := s.Stage(context.Background(), true); err == nil {
		t.Fatal("Stage enabled while replacing the playlist")
	}
	stopDone := make(chan error, 1)
	go func() { _, err := s.Stop(StopRequest{RequestID: "stop-during-playlist-load"}); stopDone <- err }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("playlist disk I/O blocked STOP")
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.Snapshot(true).StageEnabled || s.Snapshot(true).State != "stopped" {
		t.Fatal("loading a playlist enabled playback")
	}
}

func TestPlaylistExportWhilePlayingAndEmptyImport(t *testing.T) {
	s, f, _ := setup(t, false)
	if _, err := play(s, "a", "export-while-playing"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.count() == 1 })
	s.nativeEvent(playback.Event{Kind: "playing", Generation: s.Snapshot(true).Generation})
	state := s.Snapshot(true)
	file := playlistDocument(t, s)
	if s.Snapshot(true).Generation != state.Generation || s.Snapshot(true).State != "playing" {
		t.Fatal("saving a playlist interrupted playback")
	}
	stop, err := s.Stop(StopRequest{RequestID: "stop-before-empty-import"})
	if err != nil {
		t.Fatal(err)
	}
	s.nativeEvent(playback.Event{Kind: "stopped", Generation: stop.Generation})
	file.Cues = []PlaylistFileCue{}
	loaded, err := s.LoadPlaylist(PlaylistLoad{ExpectedRevision: s.Playlist().PlaylistRevision, Playlist: file})
	if err != nil || len(loaded.Cues) != 0 {
		t.Fatalf("empty playlist could not be restored: %+v, %v", loaded, err)
	}
	s.mu.Lock()
	s.state.UpdatePending = true
	s.mu.Unlock()
	if _, err := s.ExportPlaylist(); err == nil {
		t.Fatal("export started during update reservation")
	}
}
