package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"smartstage/internal/files"
	"smartstage/internal/model"
	"smartstage/internal/playback"
)

// This test-only implementation is compiled exclusively into the Go test binary.
// Passing this suite is not evidence of native playback or hardware routing.
type fakeNative struct {
	mu             sync.Mutex
	events         chan playback.Event
	starts         []playback.Start
	stops          []uint64
	inspectEntered chan struct{}
	inspectRelease chan struct{}
	inspectError   error
}

func (f *fakeNative) Devices(context.Context) (playback.Devices, error) {
	return playback.Devices{Audio: []playback.AudioDevice{{ID: "speaker", Name: "Speaker", Default: true}}, Displays: []playback.Display{{ID: "screen", Name: "Screen"}}}, nil
}
func (f *fakeNative) Inspect(ctx context.Context, _ string) (playback.Media, error) {
	if f.inspectEntered != nil {
		select {
		case f.inspectEntered <- struct{}{}:
		default:
		}
	}
	if f.inspectRelease != nil {
		select {
		case <-f.inspectRelease:
		case <-ctx.Done():
			return playback.Media{}, ctx.Err()
		}
	}
	return playback.Media{Kind: "audio", HasAudio: true, Duration: 3}, f.inspectError
}
func (f *fakeNative) Start(r playback.Start) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, r)
	return nil
}
func (f *fakeNative) Stop(g uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, g)
	return nil
}
func (f *fakeNative) Stage(g uint64, _ string, enabled bool) error {
	f.events <- playback.Event{Generation: g, Kind: "stopped", StageEnabled: enabled}
	return nil
}
func (f *fakeNative) Events() <-chan playback.Event { return f.events }
func (f *fakeNative) Close() error                  { return nil }
func (f *fakeNative) count() int                    { f.mu.Lock(); defer f.mu.Unlock(); return len(f.starts) }

type memoryStore struct {
	fail    bool
	entered chan struct{}
	release chan struct{}
}

func (m *memoryStore) Save(model.Config) error {
	if m.entered != nil {
		close(m.entered)
		<-m.release
	}
	if m.fail {
		return errors.New("disk full")
	}
	return nil
}
func setup(t *testing.T, blocked bool) (*Service, *fakeNative, *memoryStore) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tone.wav")
	if err := os.WriteFile(path, []byte("test-only source"), 0600); err != nil {
		t.Fatal(err)
	}
	browser, err := files.New([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeNative{events: make(chan playback.Event, 128)}
	if blocked {
		f.inspectEntered = make(chan struct{}, 1)
		f.inspectRelease = make(chan struct{})
	}
	p := &memoryStore{}
	config := model.DefaultConfig()
	config.Cues = []model.Cue{{ID: "a", Label: "A", Path: path}, {ID: "b", Label: "B", Path: path}}
	s := New(f, browser, p, config)
	t.Cleanup(s.Close)
	return s, f, p
}
func play(s *Service, id, request string) (Ack, error) {
	state := s.Snapshot(false)
	return s.Play(PlayRequest{RequestID: request, InstanceID: state.InstanceID, StopEpoch: state.StopEpoch, CueID: id})
}
func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func TestStopCancelsLoadingAndRejectsLateCallbacksAndOldEpoch(t *testing.T) {
	s, f, _ := setup(t, true)
	old := s.Snapshot(false)
	a, err := play(s, "a", "request-a")
	if err != nil {
		t.Fatal(err)
	}
	<-f.inspectEntered
	if _, err := play(s, "b", "request-b"); err != nil {
		t.Fatal(err)
	}
	stop, err := s.Stop(StopRequest{"stop-request"})
	if err != nil {
		t.Fatal(err)
	}
	close(f.inspectRelease)
	s.nativeEvent(playback.Event{Generation: a.Generation, Kind: "playing"})
	s.nativeEvent(playback.Event{Generation: a.Generation, Kind: "ended"})
	s.nativeEvent(playback.Event{Generation: stop.Generation, Kind: "stopped", StageEnabled: true})
	if state := s.Snapshot(false); state.State != "stopped" || state.ActiveCueID != "" || !state.StageEnabled {
		t.Fatalf("stale callback revived playback: %+v", state)
	}
	_, err = s.Play(PlayRequest{RequestID: "delayed-old", InstanceID: old.InstanceID, StopEpoch: old.StopEpoch, CueID: "a"})
	if err == nil {
		t.Fatal("old epoch accepted")
	}
	time.Sleep(10 * time.Millisecond)
	if f.count() != 0 {
		t.Fatal("cancelled native work started")
	}
}
func TestRequestRetryAndDifferentContent(t *testing.T) {
	s, f, _ := setup(t, false)
	before := s.Snapshot(false)
	r := PlayRequest{RequestID: "intentional-one", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "a"}
	a, err := s.Play(r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Play(r)
	if err != nil || !again.Duplicate || again.Generation != a.Generation {
		t.Fatalf("retry: %+v %v", again, err)
	}
	r.CueID = "b"
	if _, err := s.Play(r); err == nil {
		t.Fatal("conflicting reuse accepted")
	}
	eventually(t, func() bool { return f.count() == 1 })
	if _, err := play(s, "a", "intentional-two"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.count() == 2 })
	stop, _ := s.Stop(StopRequest{"stop-delivery"})
	retry, _ := s.Stop(StopRequest{"stop-delivery"})
	if stop.StopEpoch != retry.StopEpoch || !retry.Duplicate {
		t.Fatal("duplicate STOP incremented epoch")
	}
}
func TestPlaylistRevisionActiveProtectionAndSaveFailure(t *testing.T) {
	s, f, store := setup(t, false)
	if _, err := play(s, "a", "start-active"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.count() == 1 })
	base := s.Playlist()
	if _, err := s.EditPlaylist(PlaylistEdit{base.PlaylistRevision, []CueEdit{{ID: "b", Label: "B", Path: base.Cues[1].Path}}}); err == nil {
		t.Fatal("active cue removed")
	}
	edit := PlaylistEdit{base.PlaylistRevision, []CueEdit{{ID: "b", Label: "<script>alert(1)</script>", Path: base.Cues[1].Path}, {ID: "a", Label: "Renamed active", Path: base.Cues[0].Path}}}
	updated, err := s.EditPlaylist(edit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Cues[1].ID != "a" || s.Snapshot(false).ActivePosition != 2 {
		t.Fatal("reordering changed identity or position")
	}
	if _, err := s.EditPlaylist(edit); err == nil {
		t.Fatal("stale edit overwrote playlist")
	}
	store.fail = true
	edit.ExpectedRevision = updated.PlaylistRevision
	edit.Cues[0].Label = "Unsaved"
	if _, err := s.EditPlaylist(edit); err == nil {
		t.Fatal("save failure hidden")
	}
	if s.Playlist().Cues[0].Label == "Unsaved" {
		t.Fatal("unsaved edit committed")
	}
}
func TestStopDoesNotWaitForPersistenceOrSubscribers(t *testing.T) {
	s, _, store := setup(t, false)
	store.entered = make(chan struct{})
	store.release = make(chan struct{})
	ch, unsubscribe, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	_ = ch
	base := s.Playlist()
	done := make(chan error, 1)
	go func() {
		_, err := s.EditPlaylist(PlaylistEdit{base.PlaylistRevision, []CueEdit{{ID: "a", Label: "A", Path: base.Cues[0].Path}}})
		done <- err
	}()
	<-store.entered
	stopped := make(chan struct{})
	go func() { _, _ = s.Stop(StopRequest{"priority-stop"}); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("STOP trapped behind persistence or subscriber")
	}
	close(store.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestNativeFailureProcessInstanceAndCommandRedaction(t *testing.T) {
	s, f, _ := setup(t, false)
	state := s.Snapshot(false)
	if _, err := s.Play(PlayRequest{RequestID: "bad-instance", InstanceID: "old-process", StopEpoch: state.StopEpoch, CueID: "a"}); err == nil {
		t.Fatal("old process instance accepted")
	}
	a, _ := play(s, "a", "load-failure")
	eventually(t, func() bool { return f.count() == 1 })
	s.nativeEvent(playback.Event{Generation: a.Generation, Kind: "error", Message: "failed opening /private/show/secret.mp4", StageEnabled: true})
	if s.Snapshot(false).LastError == s.Snapshot(true).LastError || s.Snapshot(false).ActiveCueID != "" || s.Snapshot(false).State != "error" {
		t.Fatal("native error hidden or private path leaked")
	}
}
func TestTwoControllersLatestAcceptedCueWins(t *testing.T) {
	s, _, _ := setup(t, true)
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); _, _ = play(s, id, "controller-"+id) }(id)
	}
	wg.Wait()
	last := s.Snapshot(false)
	s.nativeEvent(playback.Event{Generation: last.Generation - 1, Kind: "playing"})
	if s.Snapshot(false).State != "loading" {
		t.Fatal("replaced controller callback applied")
	}
	_, _ = s.Stop(StopRequest{"controller-stop"})
	s.nativeEvent(playback.Event{Generation: last.Generation, Kind: "playing"})
	if s.Snapshot(false).ActiveCueID != "" {
		t.Fatal("controller revived after STOP")
	}
}
