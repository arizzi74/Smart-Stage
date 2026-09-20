package app

import (
	"path/filepath"

	"smartstage/internal/model"
)

// AppendHostFiles adds original host files delivered by the native file picker
// or desktop file-open action. Files stay in place, and playback is not interrupted.
// A whole batch uses the same validation and atomic save as a browser edit.
func (s *Service) AppendHostFiles(paths []string) (model.Config, error) {
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	if s.closed || s.state.UpdatePending {
		s.mu.Unlock()
		return model.Config{}, problem("updating", "Wait for the application update before editing the show")
	}
	base := s.config.Clone()
	s.mu.Unlock()
	if len(paths) == 0 {
		return model.Config{}, problem("invalid_path", "Choose one or more media files to add")
	}
	if len(paths) > model.MaxCues-len(base.Cues) {
		return model.Config{}, problem("too_many_cues", "A show may contain at most 500 cues")
	}
	edit := PlaylistEdit{ExpectedRevision: base.PlaylistRevision, Cues: make([]CueEdit, 0, len(base.Cues)+len(paths))}
	for _, cue := range base.Cues {
		// Omitted color preserves the existing choice in editPlaylistLocked.
		edit.Cues = append(edit.Cues, CueEdit{ID: cue.ID, Label: cue.Label, Path: cue.Path})
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return model.Config{}, problem("invalid_path", "Native file imports require an absolute host path")
		}
		edit.Cues = append(edit.Cues, CueEdit{Path: path})
	}
	return s.editPlaylistLocked(edit)
}
