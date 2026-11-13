package app

import (
	"context"
	"errors"
	"smartstage/internal/model"
	"smartstage/internal/playback"
)

type pendingAudio struct {
	source presentationSource
	cancel context.CancelFunc
}

// Sources have independent identities and transport revisions. The command
// epoch advances separately so revisiting an older layer never makes native
// scene ordering run backwards.
func (s *Service) sourceForCueLocked(cueID string) *presentationSource {
	if cueID == "" {
		return nil
	}
	if s.pending.cueID == cueID {
		return &s.pending
	}
	if pending := s.mixPending[cueID]; pending != nil {
		return &pending.source
	}
	if s.foreground.cueID == cueID {
		return &s.foreground
	}
	for i := range s.audioLayers {
		if s.audioLayers[i].cueID == cueID {
			return &s.audioLayers[i]
		}
	}
	return nil
}

func (s *Service) sourceForIDLocked(id uint64) *presentationSource {
	if id == 0 {
		return nil
	}
	if source := s.pendingForIDLocked(id); source != nil {
		return source
	}
	if s.foreground.id == id {
		return &s.foreground
	}
	for i := range s.audioLayers {
		if s.audioLayers[i].id == id {
			return &s.audioLayers[i]
		}
	}
	return nil
}

func (s *Service) cuePlaybackViewLocked(view *CueView) {
	source := s.sourceForCueLocked(view.ID)
	if source == nil {
		return
	}
	view.Selected = true
	if source.kind != "" {
		view.Kind = source.kind
	}
	view.State = source.state
	view.Paused = source.paused
	view.Generation = source.id
	view.TransportRevision = source.transportRevision
	view.Elapsed = source.position
	view.SeekPending = source.seekPending
	if source.duration > 0 {
		view.Duration = source.duration
	}
}

// syncSelectedLocked keeps the existing single transport panel useful while
// every running cue also exposes its own transport in Cues. Native callbacks
// for another layer do not steal that panel's selection.
func (s *Service) syncSelectedLocked() {
	source := s.sourceForCueLocked(s.state.ActiveCueID)
	if source == nil {
		switch {
		case s.newestPendingLocked() != nil:
			source = s.newestPendingLocked()
		case len(s.audioLayers) != 0:
			source = &s.audioLayers[len(s.audioLayers)-1]
		case s.foreground.id != 0:
			source = &s.foreground
		}
	}
	if source == nil {
		s.clearActiveLocked()
		s.state.Generation = s.generationSequence
		s.state.State = "stopped"
		return
	}
	s.state.ActiveCueID = source.cueID
	s.state.Generation = source.id
	s.state.TransportRevision = source.transportRevision
	s.state.State = source.state
	s.state.Paused = source.paused
	s.state.Elapsed = source.position
	s.state.Duration = source.duration
	s.state.SeekPending = source.seekPending
	s.state.ResolvedAudioID = source.audioID
}

func (s *Service) commitSourceLocked(source presentationSource, cue model.Cue) {
	if source.kind != "audio" || !cue.KeepVideo && !cue.MixAudio {
		s.foreground = source
		s.audioLayers = nil
		s.backgroundMuted = false
		s.clearImageLocked()
		return
	}
	// A mixed cue preserves the exact existing sources. KeepVideo alone replaces
	// music while retaining only the existing foreground video's visual timeline.
	if !cue.MixAudio {
		s.audioLayers = nil
		if s.foreground.kind == "video" {
			s.foreground.muted = true
		} else {
			s.foreground = presentationSource{}
		}
		if s.background.kind == "video" {
			s.backgroundMuted = true
		}
	}
	s.audioLayers = append(s.audioLayers, source)
}

func (s *Service) sceneAudioLayersLocked() []playback.AudioLayer {
	if len(s.audioLayers) == 0 {
		return nil
	}
	layers := make([]playback.AudioLayer, 0, len(s.audioLayers))
	for _, source := range s.audioLayers {
		layers = append(layers, playback.AudioLayer{ID: source.id, Path: source.path, AudioID: source.audioID, Paused: source.paused, TransportRevision: source.transportRevision, SeekRevision: source.seekRevision, SeekSeconds: source.seekSeconds, Volume: s.trackVolumeLocked(source.cueID)})
	}
	return layers
}

func (s *Service) backgroundAudioEligibleLocked() bool {
	if !s.config.Stage.BackgroundAudio || !s.background.hasAudio || s.backgroundMuted {
		return false
	}
	if s.foreground.id != 0 && s.foreground.hasAudio && !s.foreground.mixAudio {
		return false
	}
	for _, source := range s.audioLayers {
		if !source.mixAudio {
			return false
		}
	}
	return true
}

func (s *Service) removeSourceLocked(id uint64) {
	s.removePendingLocked(id)
	if s.foreground.id == id {
		s.foreground = presentationSource{}
	}
	for i := range s.audioLayers {
		if s.audioLayers[i].id == id {
			s.audioLayers = append(s.audioLayers[:i], s.audioLayers[i+1:]...)
			break
		}
	}
}

func (s *Service) sourceEventLocked(e playback.Event) {
	source := s.sourceForIDLocked(e.Generation)
	if source == nil {
		return
	}
	if _, native := s.backend.(playback.SceneBackend); native && e.TransportRevision != source.transportRevision {
		return
	}
	// A decoder callback for a source still being inspected cannot start it.
	if s.isPendingSourceLocked(source) {
		return
	}
	if e.SceneRevision == s.sceneRevision || e.SceneRevision == 0 {
		s.state.StageEnabled = e.StageEnabled
		if e.StageEnabled {
			s.stageEnablePending = false
		}
	}
	switch e.Kind {
	case "playing", "paused":
		if (e.Kind == "paused") != source.paused {
			return
		}
		source.state = e.Kind
		source.seekPending = false
		if finitePosition(e.Position) {
			source.position = e.Position
		}
		if e.Duration > 0 && finitePosition(e.Duration) {
			source.duration = e.Duration
		}
	case "progress":
		if source.state != "playing" && source.state != "paused" {
			return
		}
		if finitePosition(e.Position) {
			source.position = e.Position
		}
		if e.Duration > 0 && finitePosition(e.Duration) {
			source.duration = e.Duration
		}
	case "ended":
		s.removeSourceLocked(e.Generation)
		s.syncSelectedLocked()
		if err := s.applySceneLocked(false); err != nil {
			s.failLocked(err.Error())
			return
		}
	case "error", "device-lost":
		if e.Kind == "device-lost" {
			s.state.OutputFault = true
			s.failLocked(e.Message)
			return
		}
		if source == &s.foreground && len(s.audioLayers) == 0 && len(s.mixPending) == 0 {
			s.failLocked(e.Message)
			return
		}
		// Keep the legacy single-foreground failure policy, while an independent
		// active/pending mixed source survives failure of another source's decoder.
		s.removeSourceLocked(e.Generation)
		s.state.LastError = e.Message
		s.syncSelectedLocked()
		empty := s.foreground.id == 0 && len(s.audioLayers) == 0 && s.newestPendingLocked() == nil
		if err := s.applySceneLocked(false); err != nil {
			s.failLocked(err.Error())
			return
		}
		if empty {
			s.state.State = "error"
		}
		s.changedLocked()
		return
	default:
		return
	}
	s.syncSelectedLocked()
	s.changedLocked()
}

func (s *Service) pendingForIDLocked(id uint64) *presentationSource {
	if id == 0 {
		return nil
	}
	if s.pending.id == id {
		return &s.pending
	}
	for _, pending := range s.mixPending {
		if pending.source.id == id {
			return &pending.source
		}
	}
	return nil
}

func (s *Service) isPendingSourceLocked(source *presentationSource) bool {
	return source != nil && s.pendingForIDLocked(source.id) == source
}

func (s *Service) newestPendingLocked() *presentationSource {
	var source *presentationSource
	if s.pending.id != 0 {
		source = &s.pending
	}
	for _, pending := range s.mixPending {
		if source == nil || pending.source.id > source.id {
			source = &pending.source
		}
	}
	return source
}

func (s *Service) removePendingLocked(id uint64) {
	if id == 0 {
		return
	}
	if s.pending.id == id {
		s.pending = presentationSource{}
		if s.loadCancel != nil {
			s.loadCancel()
			s.loadCancel = nil
		}
	}
	for cueID, pending := range s.mixPending {
		if pending.source.id == id {
			pending.cancel()
			delete(s.mixPending, cueID)
			break
		}
	}
}

func (s *Service) cancelPreparationsLocked() {
	s.removePendingLocked(s.pending.id)
	for cueID, pending := range s.mixPending {
		pending.cancel()
		delete(s.mixPending, cueID)
	}
	select {
	case <-s.loads:
	default:
	}
	for {
		select {
		case <-s.mixLoads:
		default:
			return
		}
	}
}

func (s *Service) cancelOlderPreparationsLocked(id uint64) {
	if s.pending.id != 0 && s.pending.id < id {
		s.removePendingLocked(s.pending.id)
	}
	for _, pending := range s.mixPending {
		if pending.source.id < id {
			s.removePendingLocked(pending.source.id)
		}
	}
}

func (s *Service) watchPreparation(ctx context.Context, id uint64) {
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.pendingForIDLocked(id) == nil {
		return
	}
	s.removePendingLocked(id)
	if s.foreground.id == 0 && len(s.audioLayers) == 0 && s.newestPendingLocked() == nil {
		s.failLocked("Cue preparation timed out")
	} else {
		s.state.LastError = "Cue preparation timed out"
		s.syncSelectedLocked()
		s.changedLocked()
	}
}

// Expired requests can leave queued work while an inspector is still returning.
// Compact that bounded mailbox before enqueueing rather than ever blocking STOP
// on a full channel while holding the coordinator mutex.
func (s *Service) enqueueMixLocked(job loadJob) bool {
	if len(s.mixLoads) == cap(s.mixLoads) {
		queued := make([]loadJob, 0, len(s.mixLoads))
	drain:
		for {
			select {
			case candidate := <-s.mixLoads:
				if s.pendingForIDLocked(candidate.generation) != nil && candidate.ctx.Err() == nil {
					queued = append(queued, candidate)
				}
			default:
				break drain
			}
		}
		for _, candidate := range queued {
			s.mixLoads <- candidate
		}
	}
	// Each pending cue is unique and the playlist admits at most MaxCues entries,
	// so the live mailbox always has space after cancelled entries are removed.
	select {
	case s.mixLoads <- job:
		return true
	default:
		return false
	}
}
