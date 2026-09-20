package app

import "sync"

// ReserveUpdate serializes with saved-show edits and playback decisions. Once it
// succeeds, even a simultaneous remote PLAY cannot start a cue before restart.
// The returned cancellation releases the reservation if staging fails.
func (s *Service) ReserveUpdate() (func(), error) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.state.UpdatePending {
		return nil, problem("updating", "An update or shutdown is already in progress")
	}
	if s.state.State != "stopped" || s.state.StageEnabled || s.stageEnablePending {
		return nil, problem("must_stop", "Stop playback and disable stage output before updating Smart Stage")
	}
	s.invalidateLocked()
	s.state.StopEpoch++
	s.state.UpdatePending = true
	s.changedLocked()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.state.UpdatePending = false
			s.changedLocked()
		})
	}, nil
}
