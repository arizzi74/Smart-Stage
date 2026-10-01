package app

import "smartstage/internal/model"

type CueVolumeEdit struct {
	ExpectedRevision uint64   `json:"expectedRevision"`
	CueID            string   `json:"cueId"`
	Volume           *float64 `json:"volume"`
}

// EditCueVolume shares the existing saved-show transaction. Changing a level
// never replaces the source or advances its playback/transport generation.
func (s *Service) EditCueVolume(edit CueVolumeEdit) (model.Config, error) {
	if edit.Volume == nil || !model.ValidVolume(*edit.Volume) {
		return model.Config{}, problem("invalid_volume", "Track volume must be between 0 and 100 percent")
	}
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	config := s.config.Clone()
	s.mu.Unlock()
	if edit.ExpectedRevision != config.PlaylistRevision {
		return model.Config{}, problem("revision_conflict", "The playlist changed in another tab; reload it before editing")
	}
	cues := make([]CueEdit, 0, len(config.Cues))
	found := false
	for _, cue := range config.Cues {
		volume := cue.Volume
		if cue.ID == edit.CueID {
			if cue.Cache.Media.Kind == "image" || cue.Cache.Media.Kind == "" && imageFile(cue.Path) {
				return model.Config{}, problem("invalid_volume", "Images do not have a playback volume")
			}
			found = true
			volume = edit.Volume
		}
		cues = append(cues, CueEdit{ID: cue.ID, Label: cue.Label, Path: cue.Path, Color: &cue.Color, Hidden: &cue.Hidden, Background: &cue.Background, Volume: volume})
	}
	if !found {
		return model.Config{}, problem("cue_not_found", "Cue does not exist")
	}
	// Levels do not change sources or cached native inspection. Dragging a fader
	// must not repeatedly schedule validation of the entire show.
	return s.editPlaylistTransactionLocked(PlaylistEdit{ExpectedRevision: edit.ExpectedRevision, Cues: cues}, false)
}
