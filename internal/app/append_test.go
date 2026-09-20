package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"smartstage/internal/files"
	"smartstage/internal/model"
	"smartstage/internal/playback"
	"smartstage/internal/store"
)

func appendService(t *testing.T) (*Service, *fakeNative, *store.Store, string, string) {
	t.Helper()
	base := t.TempDir()
	mediaRoot := filepath.Join(base, "media")
	if err := os.Mkdir(mediaRoot, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(mediaRoot, "original.wav")
	if err := os.WriteFile(original, []byte("existing media remains here"), 0600); err != nil {
		t.Fatal(err)
	}
	browser, err := files.New([]string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(base, "config")
	storage, config, err := store.Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := browser.File(original)
	if err != nil {
		t.Fatal(err)
	}
	config.Cues = []model.Cue{{ID: "existing", Label: "Keep this label", Path: canonical, Color: "#A012ef"}}
	if err := storage.Save(config); err != nil {
		t.Fatal(err)
	}
	backend := &fakeNative{events: make(chan playback.Event, 128)}
	service := New(backend, browser, storage, config)
	t.Cleanup(func() { service.Close(); _ = storage.Close() })
	return service, backend, storage, mediaRoot, configDir
}

func TestAppendHostFilesKeepsOriginalFilesAndShowAcrossRestart(t *testing.T) {
	service, backend, storage, root, configDir := appendService(t)
	before := service.Playlist()
	stateBefore := service.Snapshot(false)
	paths := []string{filepath.Join(root, "Café opening.wav"), filepath.Join(root, "Finale.mp4")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("original bytes: "+filepath.Base(path)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	appended, err := service.AppendHostFiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(appended.Cues) != 3 || appended.PlaylistRevision != before.PlaylistRevision+1 {
		t.Fatalf("batch was not appended as one revision: %+v", appended)
	}
	kept := appended.Cues[0]
	if kept.ID != before.Cues[0].ID || kept.Label != before.Cues[0].Label || kept.Path != before.Cues[0].Path || kept.Color != before.Cues[0].Color {
		t.Fatal("native append changed an existing cue")
	}
	for i, path := range paths {
		canonical, _ := filepath.EvalSymlinks(path)
		cue := appended.Cues[i+1]
		if cue.Path != canonical || cue.Color != "" || cue.ID == "" || cue.ID == kept.ID {
			t.Fatalf("import did not reference the original file with a fresh identity: %+v", cue)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original bytes: "+filepath.Base(path) {
			t.Fatal("native append modified or moved an original file")
		}
	}
	if appended.Cues[1].ID == appended.Cues[2].ID || appended.Cues[1].Label != "Café opening" || appended.Cues[2].Label != "Finale" {
		t.Fatal("new cue identities or default labels are incorrect")
	}
	stateAfter := service.Snapshot(false)
	if backend.count() != 0 || stateAfter.Generation != stateBefore.Generation || stateAfter.StopEpoch != stateBefore.StopEpoch || stateAfter.State != "stopped" {
		t.Fatal("native append started or interrupted playback")
	}
	service.Close()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, restored, err := store.Open(configDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if len(restored.Cues) != 3 || restored.Cues[0].Color != before.Cues[0].Color || restored.Cues[1].ID != appended.Cues[1].ID || restored.Cues[1].Path != appended.Cues[1].Path {
		t.Fatalf("native import did not survive restart: %+v", restored)
	}
}

func TestAppendHostFilesRejectsWholeInvalidBatchWithoutSaving(t *testing.T) {
	service, _, _, root, configDir := appendService(t)
	valid := filepath.Join(root, "valid.wav")
	if err := os.WriteFile(valid, []byte("leave me here"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err := os.WriteFile(outside, []byte("outside root"), 0600); err != nil {
		t.Fatal(err)
	}
	batches := [][]string{
		nil,
		{"relative.wav"},
		{valid, filepath.Join(root, "missing.wav")},
		{valid, outside},
		{valid, root},
	}
	link := filepath.Join(root, "escaped-link.wav")
	if err := os.Symlink(outside, link); err == nil {
		batches = append(batches, []string{valid, link})
	}
	for _, paths := range batches {
		before := service.Playlist()
		savedBefore, err := os.ReadFile(filepath.Join(configDir, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.AppendHostFiles(paths); err == nil {
			t.Fatalf("invalid batch accepted: %v", paths)
		}
		after := service.Playlist()
		savedAfter, err := os.ReadFile(filepath.Join(configDir, "state.json"))
		if err != nil || string(savedAfter) != string(savedBefore) || after.PlaylistRevision != before.PlaylistRevision || len(after.Cues) != len(before.Cues) || after.Cues[0].Color != before.Cues[0].Color {
			t.Fatalf("rejected batch partially changed the show: %v", paths)
		}
	}
}

func TestAppendHostFilesPreservesActivePlaybackAndRejectsUpdateReservation(t *testing.T) {
	service, backend, _ := setup(t, false)
	path := service.Playlist().Cues[0].Path
	release, err := service.ReserveUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AppendHostFiles([]string{path}); err == nil {
		t.Fatal("native import bypassed update reservation")
	}
	release()
	if _, err := play(service, "a", "play-before-append"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return backend.count() == 1 })
	before := service.Snapshot(false)
	backend.mu.Lock()
	stopsBefore := len(backend.stops)
	backend.mu.Unlock()
	if _, err := service.AppendHostFiles([]string{path}); err != nil {
		t.Fatal(err)
	}
	after := service.Snapshot(false)
	backend.mu.Lock()
	stopsAfter := len(backend.stops)
	backend.mu.Unlock()
	if after.Generation != before.Generation || after.StopEpoch != before.StopEpoch || after.ActiveCueID != before.ActiveCueID || after.State != before.State || backend.count() != 1 || stopsAfter != stopsBefore {
		t.Fatal("native append interrupted or restarted the active cue")
	}
}

func TestAppendHostFilesEnforcesFinalCueLimit(t *testing.T) {
	service, _, _ := setup(t, false)
	path := service.Playlist().Cues[0].Path
	service.mu.Lock()
	service.config.Cues = nil
	for i := 0; i < model.MaxCues; i++ {
		service.config.Cues = append(service.config.Cues, model.Cue{ID: fmt.Sprint(i), Label: "Existing", Path: path})
	}
	service.mu.Unlock()
	before := service.Playlist()
	_, err := service.AppendHostFiles([]string{path})
	var problem *Error
	if !errors.As(err, &problem) || problem.Code != "too_many_cues" {
		t.Fatalf("overfull native import returned %v", err)
	}
	if after := service.Playlist(); len(after.Cues) != model.MaxCues || after.PlaylistRevision != before.PlaylistRevision {
		t.Fatal("overfull native import modified the show")
	}
}

type appendSerialStore struct {
	mu      sync.Mutex
	saves   int
	entered chan struct{}
	release chan struct{}
}

func (p *appendSerialStore) Save(model.Config) error {
	p.mu.Lock()
	p.saves++
	first := p.saves == 1
	p.mu.Unlock()
	if first {
		close(p.entered)
		<-p.release
	}
	return nil
}

func TestAppendHostFilesWaitsForConcurrentRenameAndColorSave(t *testing.T) {
	service, _, _ := setup(t, false)
	persistence := &appendSerialStore{entered: make(chan struct{}), release: make(chan struct{})}
	service.store = persistence
	before := service.Playlist()
	edits := colorEdits(before)
	color := "#445566"
	edits[0].Label, edits[0].Color = "Completed rename", &color
	editDone := make(chan error, 1)
	go func() {
		_, err := service.EditPlaylist(PlaylistEdit{ExpectedRevision: before.PlaylistRevision, Cues: edits})
		editDone <- err
	}()
	<-persistence.entered
	appendDone := make(chan error, 1)
	go func() {
		_, err := service.AppendHostFiles([]string{before.Cues[0].Path})
		appendDone <- err
	}()
	select {
	case err := <-appendDone:
		close(persistence.release)
		t.Fatalf("append finished before the in-flight save: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(persistence.release)
	if err := <-editDone; err != nil {
		t.Fatal(err)
	}
	if err := <-appendDone; err != nil {
		t.Fatal("append used a stale playlist revision:", err)
	}
	after := service.Playlist()
	if len(after.Cues) != len(before.Cues)+1 || after.PlaylistRevision != before.PlaylistRevision+2 || after.Cues[0].ID != before.Cues[0].ID || after.Cues[0].Label != "Completed rename" || after.Cues[0].Color != color {
		t.Fatalf("native append lost a concurrent saved edit: %+v", after)
	}
}
