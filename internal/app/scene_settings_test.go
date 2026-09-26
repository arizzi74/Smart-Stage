package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"smartstage/internal/files"
	"smartstage/internal/model"
	"smartstage/internal/playback"
)

type stageSettingsNative struct {
	sceneNative
	inspectMu sync.Mutex
	calls     int
	failure   error
	entered   chan struct{}
	release   chan struct{}
}

func (f *stageSettingsNative) Inspect(ctx context.Context, path string) (playback.Media, error) {
	f.inspectMu.Lock()
	f.calls++
	failure, entered, release := f.failure, f.entered, f.release
	f.inspectMu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return playback.Media{}, ctx.Err()
		}
	}
	if failure != nil {
		return playback.Media{}, failure
	}
	return f.sceneNative.Inspect(ctx, path)
}

func (f *stageSettingsNative) inspectionCount() int {
	f.inspectMu.Lock()
	defer f.inspectMu.Unlock()
	return f.calls
}

func stageSettingsSetup(t *testing.T, extension string) (*Service, *stageSettingsNative, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "background"+extension)
	if err := os.WriteFile(path, []byte("original fixture media"), 0600); err != nil {
		t.Fatal(err)
	}
	browser, err := files.New([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	config := model.DefaultConfig()
	config.Stage.BackgroundCueID = "background"
	config.Cues = []model.Cue{{ID: "background", Label: "Background", Path: path}}
	f := &stageSettingsNative{sceneNative: sceneNative{fakeNative: fakeNative{events: make(chan playback.Event, 128)}}}
	s := New(f, browser, &memoryStore{}, config)
	t.Cleanup(s.Close)
	// The real coordinator inspection records fresh filesystem metadata, even
	// when the inspected audio-only fixture is rejected as a stage background.
	eventually(t, func() bool { return s.Playlist().Cues[0].Cache.Status == "ready" })
	return s, f, path
}

func TestConfigureStageReusesUnchangedReadyBackground(t *testing.T) {
	for _, extension := range []string{".png", ".mp4"} {
		t.Run(extension, func(t *testing.T) {
			s, f, _ := stageSettingsSetup(t, extension)
			before := s.Playlist()
			calls := f.inspectionCount()
			f.inspectMu.Lock()
			f.failure = errors.New("unexpected redundant native decode")
			f.inspectMu.Unlock()
			settings := before.Stage
			settings.FadeEnabled, settings.FadeSeconds, settings.ToggleAudio = true, 2.5, true
			updated, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: settings})
			if err != nil {
				t.Fatal(err)
			}
			if updated.Stage != settings || updated.PlaylistRevision != before.PlaylistRevision+1 {
				t.Fatalf("stage edits were not saved: %+v", updated)
			}
			if f.inspectionCount() != calls {
				t.Fatal("saving fades re-decoded an unchanged ready background")
			}
		})
	}
}

func TestConfigureStageRevalidatesChangedOrUncheckedBackground(t *testing.T) {
	for _, change := range []string{"size", "mtime", "unchecked", "checking"} {
		t.Run(change, func(t *testing.T) {
			s, f, path := stageSettingsSetup(t, ".mp4")
			before := s.Playlist()
			switch change {
			case "size":
				if err := os.WriteFile(path, []byte("replacement fixture with different size"), 0600); err != nil {
					t.Fatal(err)
				}
			case "mtime":
				modified := time.Unix(0, before.Cues[0].Cache.Modified).Add(3 * time.Second)
				if err := os.Chtimes(path, modified, modified); err != nil {
					t.Fatal(err)
				}
			case "unchecked", "checking":
				s.mu.Lock()
				s.config.Cues[0].Cache.Status = change
				s.mu.Unlock()
			}
			calls := f.inspectionCount()
			f.inspectMu.Lock()
			f.failure = errors.New("native fixture decoder failed")
			f.inspectMu.Unlock()
			settings := before.Stage
			settings.FadeEnabled = true
			_, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: settings})
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != "invalid_background" || !strings.Contains(problem.Message, "native fixture decoder failed") {
				t.Fatalf("failed reinspection lost its actionable error: %v", err)
			}
			if f.inspectionCount() != calls+1 {
				t.Fatal("changed or unchecked background did not undergo inspection")
			}
			if after := s.Playlist(); after.Stage != before.Stage || after.PlaylistRevision != before.PlaylistRevision {
				t.Fatalf("failed background inspection changed settings: %+v", after)
			}
		})
	}
}

func TestConfigureStageAcceptsSuccessfullyRevalidatedChangedBackground(t *testing.T) {
	s, f, path := stageSettingsSetup(t, ".mp4")
	before := s.Playlist()
	if err := os.WriteFile(path, []byte("a different valid fixture video"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := f.inspectionCount()
	settings := before.Stage
	settings.FadeEnabled, settings.FadeSeconds = true, 1.5
	updated, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if f.inspectionCount() != calls+1 || updated.Stage != settings || updated.PlaylistRevision != before.PlaylistRevision+1 {
		t.Fatalf("changed valid background did not revalidate and save: calls=%d config=%+v", f.inspectionCount(), updated)
	}
}

func TestConfigureStageRejectsMissingOutsideOrNonregularCachedBackground(t *testing.T) {
	for _, change := range []string{"missing", "outside root", "directory"} {
		t.Run(change, func(t *testing.T) {
			s, f, path := stageSettingsSetup(t, ".png")
			before := s.Playlist()
			if change == "outside root" {
				path = filepath.Join(t.TempDir(), "outside.png")
				if err := os.WriteFile(path, []byte("original fixture media"), 0600); err != nil {
					t.Fatal(err)
				}
				s.mu.Lock()
				s.config.Cues[0].Path = path
				s.mu.Unlock()
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if change == "directory" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			calls := f.inspectionCount()
			_, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: before.Stage})
			var problem *Error
			if !errors.As(err, &problem) || problem.Code != "invalid_background" {
				t.Fatalf("unsafe cached background was accepted: %v", err)
			}
			if f.inspectionCount() != calls {
				t.Fatal("native inspector received an unavailable host file")
			}
			if after := s.Playlist(); after.PlaylistRevision != before.PlaylistRevision || after.Stage != before.Stage {
				t.Fatalf("rejected source changed settings: %+v", after)
			}
		})
	}
}

func TestConfigureStageRejectsReadyAudioBackground(t *testing.T) {
	s, f, _ := stageSettingsSetup(t, ".wav")
	before := s.Playlist()
	calls := f.inspectionCount()
	_, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: before.Stage})
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != "invalid_background" {
		t.Fatalf("ready audio-only cue was accepted as a background: %v", err)
	}
	if f.inspectionCount() != calls || s.Playlist().PlaylistRevision != before.PlaylistRevision {
		t.Fatal("invalid audio background was re-decoded or persisted")
	}
}

func TestConfigureStageReinspectionDoesNotBlockStop(t *testing.T) {
	s, f, path := stageSettingsSetup(t, ".mp4")
	before := s.Playlist()
	if err := os.WriteFile(path, []byte("changed fixture media awaiting blocked inspection"), 0600); err != nil {
		t.Fatal(err)
	}
	f.inspectMu.Lock()
	f.entered, f.release = make(chan struct{}, 1), make(chan struct{})
	f.failure = errors.New("blocked inspection failed")
	entered, release := f.entered, f.release
	f.inspectMu.Unlock()
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	saved := make(chan error, 1)
	go func() {
		_, err := s.ConfigureStage(context.Background(), StageEdit{ExpectedRevision: before.PlaylistRevision, Settings: before.Stage})
		saved <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("changed background did not reach inspection")
	}
	stopped := make(chan error, 1)
	go func() {
		_, err := s.Stop(StopRequest{RequestID: "stop-during-stage-settings-inspection"})
		stopped <- err
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("STOP waited for background inspection")
	}
	once.Do(func() { close(release) })
	if err := <-saved; err == nil {
		t.Fatal("failed background inspection was accepted")
	}
}
