package app

import (
	"context"
	"sync"
	"testing"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

func TestUpdateReservationProtectsShowAndReleasesAfterFailure(t *testing.T) {
	s, backend, _ := setup(t, false)
	before := s.Snapshot(true)
	unreserve, err := s.ReserveUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot(false).UpdatePending {
		t.Fatal("controllers do not observe the update reservation")
	}
	if _, err = play(s, "a", "during-update"); err == nil {
		t.Fatal("PLAY accepted during update")
	}
	show := s.Playlist()
	if _, err = s.EditPlaylist(PlaylistEdit{ExpectedRevision: show.PlaylistRevision}); err == nil {
		t.Fatal("show edit accepted during update")
	}
	if err = s.ConfigureOutputs(context.Background(), show.Outputs); err == nil {
		t.Fatal("output edit accepted during update")
	}
	if err = s.Stage(context.Background(), true); err == nil {
		t.Fatal("stage enable accepted during update")
	}
	if _, err = s.ReserveUpdate(); err == nil {
		t.Fatal("two updates reserved the application")
	}
	stop, err := s.Stop(StopRequest{RequestID: "emergency-stop"})
	if err != nil {
		t.Fatal("update blocked STOP:", err)
	}
	s.nativeEvent(playback.Event{Kind: "stopped", Generation: stop.Generation})
	unreserve()
	unreserve() // cancellation can race with shutdown; it must be idempotent.
	if s.Snapshot(true).UpdatePending || s.Playlist().PlaylistRevision != show.PlaylistRevision {
		t.Fatal("failed update changed the saved show or kept controls reserved")
	}
	if _, err = s.Play(PlayRequest{RequestID: "old-before-update", InstanceID: before.InstanceID, StopEpoch: before.StopEpoch, CueID: "a"}); err == nil {
		t.Fatal("an old queued PLAY survived the update boundary")
	}
	if _, err = play(s, "a", "after-failed-update"); err != nil {
		t.Fatal("fresh PLAY unavailable after failed update:", err)
	}
	eventually(t, func() bool { return backend.count() == 1 })
}

// Real native Stage calls return after queueing work. A preceding Stop callback
// can arrive before the Stage callback even though both share a generation.
type delayedUpdateStageNative struct{ *fakeNative }

func (*delayedUpdateStageNative) Stage(uint64, string, bool) error { return nil }

func TestUpdateRejectsPendingStageEnableAfterPreliminaryStop(t *testing.T) {
	for _, completion := range []string{"enabled then disabled", "superseded by STOP"} {
		t.Run(completion, func(t *testing.T) {
			backend := &delayedUpdateStageNative{&fakeNative{events: make(chan playback.Event, 16)}}
			config := model.DefaultConfig()
			config.Outputs.DisplayID = "screen"
			config.Outputs.AllowPrimary = true
			s := New(backend, nil, &memoryStore{}, config)
			t.Cleanup(s.Close)
			if err := s.Stage(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			generation := s.Snapshot(true).Generation
			// This is the first event emitted by Stage's preliminary native Stop,
			// while the actual stage-enable command is still queued on the OS loop.
			s.nativeEvent(playback.Event{Kind: "stopped", Generation: generation, StageEnabled: false})
			if state := s.Snapshot(true); state.State != "stopped" || state.StageEnabled {
				t.Fatalf("test did not reach the intermediate native state: %+v", state)
			}
			if release, err := s.ReserveUpdate(); err == nil {
				release()
				t.Fatal("update reserved before the queued stage-enable completed")
			}
			if completion == "enabled then disabled" {
				s.nativeEvent(playback.Event{Kind: "stopped", Generation: generation, StageEnabled: true})
				if release, err := s.ReserveUpdate(); err == nil {
					release()
					t.Fatal("update reserved an enabled stage")
				}
				if err := s.Stage(context.Background(), false); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.Stop(StopRequest{RequestID: "cancel-pending-stage"}); err != nil {
					t.Fatal(err)
				}
				// A late completion from the cancelled generation cannot re-enable
				// the stage or retain the update reservation's transition guard.
				s.nativeEvent(playback.Event{Kind: "stopped", Generation: generation, StageEnabled: true})
			}
			s.nativeEvent(playback.Event{Kind: "stopped", Generation: s.Snapshot(true).Generation, StageEnabled: false})
			release, err := s.ReserveUpdate()
			if err != nil {
				t.Fatal("completed/cancelled stage transition left updates blocked:", err)
			}
			release()
		})
	}
}

func TestUpdateNeverInterruptsConcurrentPlay(t *testing.T) {
	for i := 0; i < 30; i++ {
		s, _, _ := setup(t, false)
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		var playErr, updateErr error
		var release func()
		go func() { defer wait.Done(); <-start; _, playErr = play(s, "a", "concurrent-play") }()
		go func() { defer wait.Done(); <-start; release, updateErr = s.ReserveUpdate() }()
		close(start)
		wait.Wait()
		if (playErr == nil) == (updateErr == nil) {
			t.Fatalf("exactly one operation must succeed: PLAY %v, update %v", playErr, updateErr)
		}
		if release != nil {
			release()
		}
		s.Close()
	}
}

func TestUpdateWaitsForSavedEditAndRejectsEnabledStage(t *testing.T) {
	s, _, persistence := setup(t, false)
	s.mu.Lock()
	s.state.StageEnabled = true
	s.mu.Unlock()
	if _, err := s.ReserveUpdate(); err == nil {
		t.Fatal("update accepted with enabled black stage")
	}
	s.mu.Lock()
	s.state.StageEnabled = false
	s.mu.Unlock()
	persistence.entered, persistence.release = make(chan struct{}), make(chan struct{})
	editDone := make(chan error, 1)
	go func() {
		_, err := s.EditPlaylist(PlaylistEdit{ExpectedRevision: s.Playlist().PlaylistRevision, Cues: []CueEdit{{ID: "a", Label: "Saved before update", Path: s.Playlist().Cues[0].Path}}})
		editDone <- err
	}()
	<-persistence.entered
	updateDone := make(chan error, 1)
	go func() { _, err := s.ReserveUpdate(); updateDone <- err }()
	if s.Snapshot(true).UpdatePending {
		t.Fatal("update reserved while persistence was incomplete")
	}
	close(persistence.release)
	if err := <-editDone; err != nil {
		t.Fatal(err)
	}
	if err := <-updateDone; err != nil {
		t.Fatal(err)
	}
	if s.Playlist().Cues[0].Label != "Saved before update" {
		t.Fatal("update did not retain the completed save")
	}
}
