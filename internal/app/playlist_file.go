package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"smartstage/internal/identity"
	"smartstage/internal/model"
)

const MaxPlaylistFileBytes = 4 * 1024 * 1024

const playlistFileFormat = "smartstage-playlist"
const playlistFileVersion = 1

// PlaylistFile is a portable show description, not a copy of the application
// configuration. Media stays at its original host path; output devices, cached
// validation, credentials and live playback state never belong in this file.
type PlaylistFile struct {
	Format  string              `json:"format"`
	Version int                 `json:"version"`
	Cues    []PlaylistFileCue   `json:"cues"`
	Stage   model.StageSettings `json:"stage"`
}

type PlaylistFileCue struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Path       string `json:"path"`
	Color      string `json:"color,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
	Background bool   `json:"background,omitempty"`
}

type PlaylistLoad struct {
	ExpectedRevision uint64       `json:"expectedRevision"`
	Playlist         PlaylistFile `json:"playlist"`
}

// DecodePlaylist accepts one bounded, versioned JSON document. It intentionally
// rejects unknown fields so a state.json or an unrelated JSON file cannot be
// mistaken for a playlist. Filesystem checks happen in LoadPlaylist on the host.
func DecodePlaylist(reader io.Reader) (PlaylistFile, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxPlaylistFileBytes+1))
	if err != nil {
		return PlaylistFile{}, problem("invalid_playlist", "Could not read the playlist file")
	}
	if len(data) > MaxPlaylistFileBytes {
		return PlaylistFile{}, problem("playlist_too_large", "Playlist files must be no larger than 4 MiB")
	}
	var playlist PlaylistFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&playlist); err != nil {
		return PlaylistFile{}, problem("invalid_playlist", "Choose a valid Smart Stage playlist file")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return PlaylistFile{}, problem("invalid_playlist", "The playlist file contains trailing data")
	}
	if err := validatePlaylistFile(playlist); err != nil {
		return PlaylistFile{}, err
	}
	return playlist, nil
}

func validatePlaylistFile(playlist PlaylistFile) error {
	if playlist.Format != playlistFileFormat || playlist.Version != playlistFileVersion {
		return problem("unsupported_playlist", "This is not a supported Smart Stage playlist file")
	}
	if playlist.Cues == nil {
		return problem("invalid_playlist", "The playlist file must include its cues")
	}
	if len(playlist.Cues) > model.MaxCues {
		return problem("too_many_cues", "A show may contain at most 500 cues")
	}
	if !playlist.Stage.Valid() {
		return problem("invalid_stage", "Fade duration must be between 0.1 and 30 seconds")
	}
	ids := make(map[string]bool, len(playlist.Cues))
	for _, cue := range playlist.Cues {
		if cue.ID == "" || len(cue.ID) > 128 || strings.ContainsAny(cue.ID, "\x00\r\n") || ids[cue.ID] {
			return problem("invalid_cue_id", "Playlist cue IDs must be nonempty and unique")
		}
		ids[cue.ID] = true
		if err := ValidateLabel(cue.Label); err != nil {
			return err
		}
		if !filepath.IsAbs(cue.Path) || len(cue.Path) > 32768 || strings.ContainsAny(cue.Path, "\x00") || strings.Contains(cue.Path, "://") {
			return problem("invalid_path", "Playlist media must use absolute host file paths")
		}
		if !model.ValidCueColor(cue.Color) {
			return problem("invalid_color", "Cue colors must be empty for the default or use #RRGGBB")
		}
	}
	if playlist.Stage.BackgroundCueID != "" && !ids[playlist.Stage.BackgroundCueID] {
		return problem("invalid_background", "The stage background must refer to a cue in the playlist file")
	}
	return nil
}

// ExportPlaylist snapshots the saved show without interrupting playback. An
// update/shutdown rejects new file operations, just like other Admin edits.
func (s *Service) ExportPlaylist() (PlaylistFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.state.UpdatePending {
		return PlaylistFile{}, problem("updating", "Wait for the application update before saving a playlist file")
	}
	file := PlaylistFile{Format: playlistFileFormat, Version: playlistFileVersion, Stage: s.config.Stage, Cues: make([]PlaylistFileCue, 0, len(s.config.Cues))}
	for _, cue := range s.config.Cues {
		file.Cues = append(file.Cues, PlaylistFileCue{ID: cue.ID, Label: cue.Label, Path: cue.Path, Color: cue.Color, Hidden: cue.Hidden, Background: cue.Background})
	}
	return file, nil
}

func (s *Service) canLoadPlaylistLocked(expectedRevision uint64) error {
	if s.closed || s.state.UpdatePending {
		return problem("updating", "Wait for the application update before loading a playlist")
	}
	if expectedRevision != s.config.PlaylistRevision || expectedRevision == ^uint64(0) {
		return problem("revision_conflict", "The playlist changed; reload it before loading a playlist file")
	}
	if s.state.State != "stopped" && s.state.State != "error" || s.foreground.id != 0 || s.state.StageEnabled || s.stageDesired || s.stageEnablePending || s.pendingImageID != "" {
		return problem("must_stop", "Stop playback and turn Stage off before loading a playlist")
	}
	return nil
}

// LoadPlaylist replaces cues and stage/sound settings in one durable
// transaction. New identities isolate late validation/native callbacks and
// queued controller requests from the newly loaded show. Media must still exist
// inside the host's configured roots; one invalid source rejects the whole load.
func (s *Service) LoadPlaylist(load PlaylistLoad) (model.Config, error) {
	if err := validatePlaylistFile(load.Playlist); err != nil {
		return model.Config{}, err
	}
	s.editMu.Lock()
	defer s.editMu.Unlock()
	s.mu.Lock()
	if err := s.canLoadPlaylistLocked(load.ExpectedRevision); err != nil {
		s.mu.Unlock()
		return model.Config{}, err
	}
	next := s.config.Clone()
	s.mu.Unlock()

	next.Cues = make([]model.Cue, 0, len(load.Playlist.Cues))
	ids := make(map[string]string, len(load.Playlist.Cues))
	for _, cue := range load.Playlist.Cues {
		resolved, _, err := s.files.File(cue.Path)
		if err != nil {
			return model.Config{}, problem("invalid_path", fmt.Sprintf("Cannot load cue %q: %s. The current playlist has not changed", cue.Label, err))
		}
		id := identity.New()
		ids[cue.ID] = id
		next.Cues = append(next.Cues, model.Cue{ID: id, Label: cue.Label, Path: resolved, Color: cue.Color, Hidden: cue.Hidden, Background: cue.Background, Cache: model.Validation{Status: "unchecked"}})
	}
	next.Stage = load.Playlist.Stage
	next.Stage.BackgroundCueID = ids[load.Playlist.Stage.BackgroundCueID]
	next.PlaylistRevision++

	s.mu.Lock()
	// PLAY or Stage-on may have won while filesystem checks were running. Once
	// configBusy is set their existing guards hold until persistence completes.
	if err := s.canLoadPlaylistLocked(load.ExpectedRevision); err != nil {
		s.mu.Unlock()
		return model.Config{}, err
	}
	s.configBusy = true
	s.mu.Unlock()
	err := s.store.Save(next)
	s.mu.Lock()
	s.configBusy = false
	if err != nil {
		s.mu.Unlock()
		return model.Config{}, problem("save_failed", err.Error())
	}
	s.config = next
	s.invalidateLocked()
	s.state.StopEpoch++
	s.clearActiveLocked()
	s.clearImageLocked()
	s.foreground = presentationSource{}
	s.background = presentationSource{}
	s.state.State = "stopped"
	s.state.LastError = ""
	s.stageSerial++
	if !s.closed {
		s.selectBackgroundLocked(next.Stage.BackgroundCueID)
		s.changedLocked()
	}
	result := s.config.Clone()
	s.mu.Unlock()
	s.queueValidation()
	return result, nil
}
