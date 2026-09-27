package app

import (
	"math"

	"smartstage/internal/playback"
)

// transportLocked shares /api/play with cue buttons so deployed command relays
// can forward transport actions without adding a new route to their allowlist.
func (s *Service) transportLocked(r PlayRequest) error {
	if r.Action != "pause" && r.Action != "resume" && r.Action != "seek" {
		return problem("invalid_action", "Choose play, pause, resume or seek")
	}
	if r.Generation != s.state.Generation || r.CueID == "" || r.CueID != s.state.ActiveCueID {
		return problem("stale_generation", "The active cue changed; refresh state before changing playback")
	}
	if r.TransportRevision != s.state.TransportRevision {
		return problem("stale_transport", "Playback changed; refresh state before changing playback")
	}
	if _, ok := s.backend.(playback.SceneBackend); !ok {
		return problem("transport_unsupported", "This playback backend does not support pause or seek")
	}
	if r.Action != "seek" {
		return s.pauseLocked(r.Action == "pause")
	}
	if s.foreground.id == 0 || s.foreground.cueID != s.state.ActiveCueID || s.foreground.kind != "audio" && s.foreground.kind != "video" || s.state.State != "playing" && s.state.State != "paused" {
		return problem("seek_unavailable", "Wait for an active audio or video cue before seeking")
	}
	duration := s.state.Duration
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return problem("seek_unavailable", "Seeking requires a known finite duration")
	}
	if r.Position == nil || math.IsNaN(*r.Position) || math.IsInf(*r.Position, 0) || *r.Position < 0 || *r.Position > duration {
		return problem("invalid_position", "Seek position must be between zero and the cue duration")
	}
	s.state.TransportRevision++
	s.foreground.transportRevision = s.state.TransportRevision
	s.foreground.seekRevision++
	s.foreground.seekSeconds = *r.Position
	s.foreground.position = *r.Position
	s.foreground.seekPending = true
	s.state.Elapsed = *r.Position
	s.state.SeekPending = true
	if err := s.applySceneLocked(false); err != nil {
		s.failLocked(err.Error())
		return err
	}
	s.changedLocked()
	return nil
}

func (s *Service) pauseLocked(paused bool) error {
	if _, ok := s.backend.(playback.SceneBackend); !ok {
		return problem("transport_unsupported", "This playback backend does not support pause or seek")
	}
	if s.state.ActiveCueID == "" || s.state.State != "loading" && s.state.State != "playing" && s.state.State != "paused" {
		return problem("transport_unavailable", "Select an audio or video cue before changing playback")
	}
	if s.state.Paused == paused {
		return nil
	}
	s.state.Paused = paused
	s.state.TransportRevision++
	// A replacement may still be inspected while its predecessor is audible.
	// Only apply transport to the source belonging to this selected generation.
	if s.foreground.id == s.state.Generation || s.state.State != "loading" && s.foreground.cueID == s.state.ActiveCueID {
		s.foreground.paused = paused
		s.foreground.transportRevision = s.state.TransportRevision
		if err := s.applySceneLocked(false); err != nil {
			s.failLocked(err.Error())
			return err
		}
	}
	s.changedLocked()
	return nil
}
