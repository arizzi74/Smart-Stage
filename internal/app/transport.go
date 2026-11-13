package app

import (
	"math"
	"smartstage/internal/playback"
)

// transportLocked addresses the cue's own source, including retained video and
// audio layers. Generation and transport revision make delayed gestures inert.
func (s *Service) transportLocked(r PlayRequest) error {
	if r.Action != "pause" && r.Action != "resume" && r.Action != "seek" {
		return problem("invalid_action", "Choose play, pause, resume or seek")
	}
	source := s.sourceForCueLocked(r.CueID)
	if source == nil || r.Generation != source.id {
		return problem("stale_generation", "The active cue changed; refresh state before changing playback")
	}
	if r.TransportRevision != source.transportRevision {
		return problem("stale_transport", "Playback changed; refresh state before changing playback")
	}
	if _, ok := s.backend.(playback.SceneBackend); !ok {
		return problem("transport_unsupported", "This playback backend does not support pause or seek")
	}
	if r.Action != "seek" {
		return s.pauseSourceLocked(source, r.Action == "pause")
	}
	if s.isPendingSourceLocked(source) || source.kind != "audio" && source.kind != "video" || source.state != "playing" && source.state != "paused" {
		return problem("seek_unavailable", "Wait for an active audio or video cue before seeking")
	}
	duration := source.duration
	if source.cueID == s.state.ActiveCueID {
		duration = s.state.Duration
	}
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return problem("seek_unavailable", "Seeking requires a known finite duration")
	}
	if r.Position == nil || math.IsNaN(*r.Position) || math.IsInf(*r.Position, 0) || *r.Position < 0 || *r.Position > duration {
		return problem("invalid_position", "Seek position must be between zero and the cue duration")
	}
	s.transportSequence++
	source.transportRevision = s.transportSequence
	source.seekRevision++
	source.seekSeconds = *r.Position
	source.position = *r.Position
	source.seekPending = true
	s.state.ActiveCueID = source.cueID
	s.syncSelectedLocked()
	if err := s.applySceneLocked(false); err != nil {
		s.failLocked(err.Error())
		return err
	}
	s.changedLocked()
	return nil
}

func (s *Service) pauseLocked(paused bool) error {
	source := s.sourceForCueLocked(s.state.ActiveCueID)
	if source == nil {
		return problem("transport_unavailable", "Select an audio or video cue before changing playback")
	}
	return s.pauseSourceLocked(source, paused)
}

func (s *Service) pauseSourceLocked(source *presentationSource, paused bool) error {
	if _, ok := s.backend.(playback.SceneBackend); !ok {
		return problem("transport_unsupported", "This playback backend does not support pause or seek")
	}
	if source.paused == paused {
		return nil
	}
	source.paused = paused
	s.transportSequence++
	source.transportRevision = s.transportSequence
	s.state.ActiveCueID = source.cueID
	s.syncSelectedLocked()
	// Pending intent never changes another source while its inspection runs.
	if !s.isPendingSourceLocked(source) {
		if err := s.applySceneLocked(false); err != nil {
			s.failLocked(err.Error())
			return err
		}
	}
	s.changedLocked()
	return nil
}
