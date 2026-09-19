package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"smartstage/internal/identity"
	"smartstage/internal/model"
)

type CueEdit struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}
type PlaylistEdit struct {
	ExpectedRevision uint64    `json:"expectedRevision"`
	Cues             []CueEdit `json:"cues"`
}

func (s *Service) EditPlaylist(edit PlaylistEdit) (model.Config, error) {
	if len(edit.Cues) > model.MaxCues {
		return model.Config{}, problem("too_many_cues", "A show may contain at most 500 cues")
	}
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	base := s.config.Clone()
	s.mu.Unlock()
	if edit.ExpectedRevision != base.PlaylistRevision {
		return model.Config{}, problem("revision_conflict", "The playlist changed in another tab; reload it before editing")
	}
	old := map[string]model.Cue{}
	for _, c := range base.Cues {
		old[c.ID] = c
	}
	seen := map[string]bool{}
	next := base.Clone()
	next.Cues = []model.Cue{}
	for _, item := range edit.Cues {
		id := item.ID
		if id == "" {
			id = identity.New()
		} else if _, ok := old[id]; !ok {
			return model.Config{}, problem("invalid_cue_id", "New cues must omit id; existing IDs must belong to this playlist")
		}
		if seen[id] {
			return model.Config{}, problem("duplicate_cue_id", "Cue IDs must be unique; add the same file as a new cue to repeat it")
		}
		seen[id] = true
		path := item.Path
		previous, exists := old[id]
		cache := previous.Cache
		if !exists || path != previous.Path {
			resolved, _, err := s.files.File(path)
			if err != nil {
				return model.Config{}, problem("invalid_path", err.Error())
			}
			path = resolved
			cache = model.Validation{Status: "unchecked"}
		}
		label := item.Label
		if label == "" && !exists {
			label = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if err := ValidateLabel(label); err != nil {
			return model.Config{}, err
		}
		next.Cues = append(next.Cues, model.Cue{ID: id, Label: label, Path: path, Cache: cache})
	}
	removing := map[string]bool{}
	for id, c := range old {
		found := false
		for _, n := range next.Cues {
			if n.ID == id && n.Path == c.Path {
				found = true
				break
			}
		}
		if !found {
			removing[id] = true
		}
	}
	s.mu.Lock()
	if s.config.PlaylistRevision != edit.ExpectedRevision {
		s.mu.Unlock()
		return model.Config{}, problem("revision_conflict", "Playlist changed; reload before editing")
	}
	if removing[s.state.ActiveCueID] {
		s.mu.Unlock()
		return model.Config{}, problem("active_cue", "Stop the active/loading cue before removing or replacing its source")
	}
	s.removing = removing
	next.PlaylistRevision++
	s.mu.Unlock()
	err := s.store.Save(next)
	s.mu.Lock()
	s.removing = map[string]bool{}
	if err != nil {
		s.mu.Unlock()
		return model.Config{}, problem("save_failed", err.Error())
	}
	// Merge newer derived validation results that arrived during disk I/O.
	for i := range next.Cues {
		for _, current := range s.config.Cues {
			if current.ID == next.Cues[i].ID && current.Path == next.Cues[i].Path {
				next.Cues[i].Cache = current.Cache
			}
		}
	}
	s.config = next
	s.changedLocked()
	result := s.config.Clone()
	s.mu.Unlock()
	s.queueValidation()
	return result, nil
}

func (s *Service) ConfigureOutputs(ctx context.Context, out model.Outputs) error {
	if len(out.AudioID) > 4096 || len(out.DisplayID) > 4096 {
		return problem("invalid_output", "Output identity is too long")
	}
	if out.AudioID == "" {
		out.AudioID = "default"
	}
	devices, err := s.backend.Devices(ctx)
	if err != nil {
		return err
	}
	if out.AudioID != "default" {
		available := false
		for _, d := range devices.Audio {
			if d.ID == out.AudioID {
				available = true
				break
			}
		}
		if !available {
			return problem("output_unavailable", "Select an available audio output")
		}
	}
	if out.DisplayID != "" {
		if err := checkDisplay(devices, out); err != nil {
			return err
		}
	}
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	if s.state.State != "stopped" && s.state.State != "error" {
		s.mu.Unlock()
		return problem("must_stop", "STOP playback before changing outputs")
	}
	s.configBusy = true
	next := s.config.Clone()
	next.Outputs = out
	s.mu.Unlock()
	err = s.store.Save(next)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configBusy = false
	if err != nil {
		return problem("save_failed", err.Error())
	}
	s.config.Outputs = out
	s.state.OutputFault = false
	s.stopLocked()
	// Moving output configuration disarms presentation until explicitly enabled
	// or a new intentional video cue starts with these choices.
	if err = s.backend.Stage(s.state.Generation, out.DisplayID, false); err != nil {
		s.failLocked(err.Error())
		return err
	}
	return nil
}

func (s *Service) Stage(ctx context.Context, enabled bool) error {
	if !enabled {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.stopLocked()
		return s.backend.Stage(s.state.Generation, s.config.Outputs.DisplayID, false)
	}
	s.mu.Lock()
	out := s.config.Outputs
	gen := s.state.Generation
	s.mu.Unlock()
	devices, err := s.backend.Devices(ctx)
	if err != nil {
		return err
	}
	if err = checkDisplay(devices, out); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Generation != gen || (s.state.State != "stopped" && s.state.State != "error") || s.configBusy {
		return problem("must_stop", "Enable stage output while stopped")
	}
	if s.state.OutputFault {
		return problem("output_unavailable", "Re-select and save available outputs before enabling the stage")
	}
	s.stopLocked()
	return s.backend.Stage(s.state.Generation, out.DisplayID, true)
}

func (s *Service) queueValidation() {
	select {
	case s.validation <- struct{}{}:
	default:
	}
}
func (s *Service) Validate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.ValidationJob.Running || len(s.validation) > 0 {
		return problem("busy", "Validation is already running")
	}
	s.queueValidation()
	return nil
}
func (s *Service) validationLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.validation:
			s.mu.Lock()
			cues := s.config.Clone().Cues
			s.state.ValidationJob = ValidationJob{Running: true, Total: len(cues)}
			s.changedLocked()
			s.mu.Unlock()
			for _, cue := range cues {
				if s.ctx.Err() != nil {
					return
				}
				s.mu.Lock()
				for i := range s.config.Cues {
					if s.config.Cues[i].ID == cue.ID && s.config.Cues[i].Path == cue.Path {
						s.config.Cues[i].Cache.Status = "checking"
					}
				}
				s.changedLocked()
				s.mu.Unlock()
				ctx, cancel := context.WithTimeout(s.ctx, 35*time.Second)
				result := s.inspect(ctx, cue.Path)
				cancel()
				if errors.Is(s.ctx.Err(), context.Canceled) {
					return
				}
				s.mu.Lock()
				for i := range s.config.Cues {
					if s.config.Cues[i].ID == cue.ID && s.config.Cues[i].Path == cue.Path {
						s.config.Cues[i].Cache = result
					}
				}
				s.state.ValidationJob.Completed++
				s.changedLocked()
				s.mu.Unlock()
			}
			s.mu.Lock()
			s.state.ValidationJob.Running = false
			s.changedLocked()
			s.mu.Unlock()
		}
	}
}
