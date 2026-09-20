package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"smartstage/internal/model"
	"smartstage/internal/playback"
)

type presentationSource struct {
	id                         uint64
	cueID, path, kind, audioID string
	hasAudio                   bool
	duration                   float64
}

type visualJob struct {
	ctx                   context.Context
	sequence, stageSerial uint64
	cue                   model.Cue
	outputs               model.Outputs
	audio                 bool
}

type StageEdit struct {
	ExpectedRevision uint64              `json:"expectedRevision"`
	Settings         model.StageSettings `json:"settings"`
}

func imageFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tif", ".tiff", ".heic", ".heif", ".webp", ".avif", ".ico":
		return true
	}
	return false
}

func (s *Service) applySceneLocked(hard bool) error {
	s.sceneRevision++
	audio := s.foreground.audioID
	if audio == "" {
		audio = s.background.audioID
	}
	fade := 0.0
	if s.config.Stage.FadeEnabled && !hard {
		fade = s.config.Stage.FadeSeconds
	}
	scene := playback.Scene{
		Revision: s.sceneRevision, Generation: s.state.Generation,
		ForegroundID: s.foreground.id, ForegroundPath: s.foreground.path,
		ForegroundKind: s.foreground.kind, ForegroundHasAudio: s.foreground.hasAudio,
		ImagePath: s.image.path, BackgroundPath: s.background.path,
		BackgroundKind:  s.background.kind,
		BackgroundAudio: s.config.Stage.BackgroundAudio && s.background.hasAudio,
		AudioID:         audio, DisplayID: s.config.Outputs.DisplayID,
		StageEnabled: s.stageDesired, FadeSeconds: fade, HardStop: hard,
	}
	s.stageEnablePending = s.stageDesired && !s.state.StageEnabled
	if native, ok := s.backend.(playback.SceneBackend); ok {
		return native.ApplyScene(scene)
	}
	// Test/legacy backends retain the original boundary. Native production
	// backends always implement SceneBackend; harness calls remain independent.
	if hard || s.foreground.id == 0 && (s.legacyForegroundID != 0 || s.state.State == "stopping" || s.state.State == "error") {
		if err := s.backend.Stop(s.state.Generation); err != nil {
			return err
		}
	}
	if s.legacyStage != s.stageDesired || hard {
		if err := s.backend.Stage(s.state.Generation, s.config.Outputs.DisplayID, s.stageDesired); err != nil {
			return err
		}
		s.legacyStage = s.stageDesired
	}
	if s.foreground.id != 0 && s.foreground.id != s.legacyForegroundID {
		if err := s.backend.Start(playback.Start{Generation: s.foreground.id, Path: s.foreground.path, AudioID: s.foreground.audioID, DisplayID: s.config.Outputs.DisplayID, Video: s.foreground.kind == "video"}); err != nil {
			return err
		}
	}
	s.legacyForegroundID = s.foreground.id
	return nil
}

func (s *Service) clearImageLocked() {
	s.visualSequence++
	if s.visualCancel != nil {
		s.visualCancel()
		s.visualCancel = nil
	}
	select {
	case <-s.visualLoads:
	default:
	}
	s.image = presentationSource{}
	s.pendingImageID = ""
	s.state.ImageCueID = ""
}

func (s *Service) selectImageLocked(cue model.Cue) {
	s.clearImageLocked()
	ctx, cancel := context.WithCancel(s.ctx)
	s.visualCancel = cancel
	s.pendingImageID = cue.ID
	s.visualLoads <- visualJob{ctx: ctx, sequence: s.visualSequence, stageSerial: s.stageSerial, cue: cue, outputs: s.config.Outputs}
}

func (s *Service) selectBackgroundLocked(id string) {
	s.backgroundSequence++
	if s.backgroundCancel != nil {
		s.backgroundCancel()
		s.backgroundCancel = nil
	}
	select {
	case <-s.backgroundLoads:
	default:
	}
	s.state.BackgroundCueID = id
	s.state.BackgroundError = ""
	if id == "" {
		s.background = presentationSource{}
		if s.sceneRevision != 0 {
			_ = s.applySceneLocked(false)
		}
		return
	}
	for _, cue := range s.config.Cues {
		if cue.ID != id {
			continue
		}
		ctx, cancel := context.WithCancel(s.ctx)
		s.backgroundCancel = cancel
		s.backgroundLoads <- visualJob{ctx: ctx, sequence: s.backgroundSequence, stageSerial: s.stageSerial, cue: cue, outputs: s.config.Outputs, audio: s.config.Stage.BackgroundAudio}
		return
	}
	s.background = presentationSource{}
	s.state.BackgroundCueID = ""
}

func resolveAudio(devices playback.Devices, preference string) (string, error) {
	for _, d := range devices.Audio {
		if d.ID == preference || preference == "default" && d.Default {
			return d.ID, nil
		}
	}
	return "", errors.New("Selected audio output is unavailable; select an output in Admin")
}

func (s *Service) visualLoop(jobs <-chan visualJob, background bool) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-jobs:
			s.prepareVisual(job, background)
		}
	}
}

func (s *Service) prepareVisual(job visualJob, background bool) {
	ctx, cancel := context.WithTimeout(job.ctx, 35*time.Second)
	defer cancel()
	path, _, err := s.files.File(job.cue.Path)
	var cache model.Validation
	var source presentationSource
	if err == nil {
		cache = s.inspect(ctx, path)
		if cache.Status != "ready" {
			err = errors.New(cache.Reason)
		}
	}
	if err == nil && cache.Media.Kind != "image" && (!background || cache.Media.Kind != "video") {
		err = errors.New("Choose an image or video for a stage background, or an image for an image cue")
	}
	if err == nil {
		source = presentationSource{cueID: job.cue.ID, path: path, kind: cache.Media.Kind, hasAudio: cache.Media.HasAudio, duration: cache.Media.Duration}
		if !background || job.audio && cache.Media.HasAudio {
			var devices playback.Devices
			devices, err = s.backend.Devices(ctx)
			if err == nil && !background {
				err = checkDisplay(devices, job.outputs)
			}
			if err == nil && background && job.audio && cache.Media.HasAudio {
				source.audioID, err = resolveAudio(devices, job.outputs.AudioID)
			}
		}
	}
	if job.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.visualSequence
	if background {
		current = s.backgroundSequence
	}
	if s.closed || current != job.sequence {
		return
	}
	for i := range s.config.Cues {
		if s.config.Cues[i].ID == job.cue.ID && s.config.Cues[i].Path == job.cue.Path && cache.Status != "" {
			s.config.Cues[i].Cache = cache
		}
	}
	if err != nil {
		if background {
			s.background = presentationSource{}
			s.state.BackgroundError = err.Error()
		} else {
			s.pendingImageID = ""
			s.state.LastError = err.Error()
		}
	} else if background {
		s.background = source
		s.state.BackgroundError = ""
	} else {
		s.image = source
		s.pendingImageID = ""
		s.state.ImageCueID = job.cue.ID
		if job.stageSerial == s.stageSerial {
			s.stageDesired = true
		}
	}
	if e := s.applySceneLocked(false); e != nil {
		s.failLocked(e.Error())
		return
	}
	s.changedLocked()
}

func (s *Service) ConfigureStage(ctx context.Context, edit StageEdit) (model.Config, error) {
	if !edit.Settings.Valid() {
		return model.Config{}, problem("invalid_stage", "Fade duration must be between 0.1 and 30 seconds")
	}
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	if s.closed || s.state.UpdatePending {
		s.mu.Unlock()
		return model.Config{}, problem("updating", "Wait for the application update before changing stage settings")
	}
	next := s.config.Clone()
	s.mu.Unlock()
	if edit.ExpectedRevision != next.PlaylistRevision {
		return model.Config{}, problem("revision_conflict", "The show changed; reload it before saving stage settings")
	}
	if edit.Settings.BackgroundCueID != "" {
		found := false
		for _, cue := range next.Cues {
			if cue.ID != edit.Settings.BackgroundCueID {
				continue
			}
			found = true
			validation := s.InspectFile(ctx, cue.Path)
			if validation.Status != "ready" || validation.Media.Kind != "image" && validation.Media.Kind != "video" {
				return model.Config{}, problem("invalid_background", "Select a readable image or video as the stage background")
			}
		}
		if !found {
			return model.Config{}, problem("cue_not_found", "The background cue no longer exists")
		}
	}
	next.Stage = edit.Settings
	next.PlaylistRevision++
	if err := s.store.Save(next); err != nil {
		return model.Config{}, problem("save_failed", err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keep validation that completed while the persisted settings were saved.
	next.Cues = s.config.Clone().Cues
	old := s.config.Stage
	s.config = next
	if s.closed {
		return s.config.Clone(), nil
	}
	if old.BackgroundCueID != next.Stage.BackgroundCueID {
		s.selectBackgroundLocked(next.Stage.BackgroundCueID)
	} else if old.BackgroundAudio != next.Stage.BackgroundAudio {
		s.selectBackgroundLocked(s.state.BackgroundCueID)
	}
	if err := s.applySceneLocked(false); err != nil {
		s.failLocked(err.Error())
		return model.Config{}, err
	}
	s.changedLocked()
	return s.config.Clone(), nil
}

func (s *Service) EmergencyStop(r StopRequest) (Ack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := fingerprint(struct {
		Kind    string
		Request StopRequest
	}{"emergency-stop", r})
	if ack, ok, err := s.duplicateLocked(r.RequestID, hash); ok || err != nil {
		return ack, err
	}
	s.stageDesired = false
	s.stageSerial++
	s.state.StageEnabled = false
	s.stopForegroundLocked(true, true)
	return s.rememberLocked(r.RequestID, hash), nil
}
