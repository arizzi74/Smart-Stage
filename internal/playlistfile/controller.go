// Package playlistfile handles paths selected by the native playlist dialogs.
// HTTP callers can request a dialog but cannot supply a read or write path.
package playlistfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"smartstage/internal/app"
	"smartstage/internal/model"
	"smartstage/internal/platform"
)

type service interface {
	ExportPlaylist() (app.PlaylistFile, error)
	LoadPlaylist(app.PlaylistLoad) (model.Config, error)
	Snapshot(bool) app.State
}

type Status struct {
	ID        uint64 `json:"id"`
	Operation string `json:"operation"`
	Phase     string `json:"phase"`
	Filename  string `json:"filename,omitempty"`
	Message   string `json:"message,omitempty"`
	Code      string `json:"code,omitempty"`
}

type Controller struct {
	mu        sync.Mutex
	workers   sync.WaitGroup
	app       service
	choose    func(uint64, bool) bool
	available func() bool
	closed    bool
	status    Status
	revision  uint64
	snapshot  []byte
}

func New(app service, choose func(uint64, bool) bool, available func() bool) *Controller {
	return &Controller{app: app, choose: choose, available: available, status: Status{Phase: "idle"}}
}

func (c *Controller) Available() bool {
	return c.choose != nil && c.available != nil && c.available()
}

func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Controller) Start(operation string, revision uint64) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Status{}, &app.Error{Code: "unavailable", Message: "Application is shutting down"}
	}
	if operation != "save" && operation != "load" {
		return Status{}, &app.Error{Code: "invalid_operation", Message: "Choose save or load"}
	}
	if !c.Available() {
		return Status{}, &app.Error{Code: "unavailable", Message: "Native playlist file selection is unavailable"}
	}
	if c.status.Phase == "choosing" || c.status.Phase == "saving" || c.status.Phase == "loading" {
		return Status{}, &app.Error{Code: "busy", Message: "Finish the open playlist file dialog first"}
	}
	state := c.app.Snapshot(true)
	if state.UpdatePending {
		return Status{}, &app.Error{Code: "updating", Message: "Wait for the application update before editing the show"}
	}
	if revision == 0 || state.PlaylistRevision != revision {
		return Status{}, &app.Error{Code: "revision_conflict", Message: "The playlist changed in another tab; reload it before editing"}
	}
	if operation == "load" && ((state.State != "stopped" && state.State != "error") || state.StageEnabled) {
		return Status{}, &app.Error{Code: "must_stop", Message: "Stop playback and disable stage output before loading a playlist"}
	}
	var data []byte
	if operation == "save" {
		document, err := c.app.ExportPlaylist()
		if err != nil {
			return Status{}, err
		}
		data, err = json.MarshalIndent(document, "", "  ")
		if err != nil {
			return Status{}, err
		}
		data = append(data, '\n')
		if len(data) > app.MaxPlaylistFileBytes {
			return Status{}, &app.Error{Code: "invalid_playlist", Message: "Playlist files must not exceed 4 MiB"}
		}
		if c.app.Snapshot(true).PlaylistRevision != revision {
			return Status{}, &app.Error{Code: "revision_conflict", Message: "The playlist changed in another tab; reload it before editing"}
		}
	}
	next := Status{ID: c.status.ID + 1, Operation: operation, Phase: "choosing"}
	if !c.choose(next.ID, operation == "save") {
		return Status{}, &app.Error{Code: "busy", Message: "Native file selection is not ready; try again"}
	}
	c.status, c.revision, c.snapshot = next, revision, data
	return next, nil
}

// Complete accepts only the matching native result once. Disk I/O runs outside
// the host event loop so Escape, STOP and Quit remain responsive.
func (c *Controller) Complete(result platform.DesktopPlaylistResult) {
	c.mu.Lock()
	if c.closed || c.status.Phase != "choosing" || result.ID != c.status.ID {
		c.mu.Unlock()
		return
	}
	if result.Cancelled || result.Error != "" {
		c.status.Phase = "cancelled"
		if result.Error != "" {
			c.status.Phase, c.status.Message, c.status.Code = "error", result.Error, "playlist_file_failed"
		}
		c.snapshot = nil
		c.mu.Unlock()
		return
	}
	operation, revision, data := c.status.Operation, c.revision, c.snapshot
	c.snapshot = nil
	c.status.Filename = filepath.Base(result.Path)
	c.status.Phase = "loading"
	if operation == "save" {
		c.status.Phase = "saving"
	}
	c.workers.Add(1)
	c.mu.Unlock()
	go func() {
		defer c.workers.Done()
		var err error
		if operation == "save" {
			err = saveFile(result.Path, data)
		} else {
			var document app.PlaylistFile
			document, err = readFile(result.Path)
			if err == nil {
				_, err = c.app.LoadPlaylist(app.PlaylistLoad{ExpectedRevision: revision, Playlist: document})
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.status.Phase = "complete"
		if err != nil {
			c.status.Phase, c.status.Message, c.status.Code = "error", err.Error(), "playlist_file_failed"
			var problem *app.Error
			if errors.As(err, &problem) {
				c.status.Code = problem.Code
			}
		}
	}()
}

func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	c.snapshot = nil
	c.mu.Unlock()
	c.workers.Wait()
}

func validPath(path string) error {
	if !filepath.IsAbs(path) || len(path) > 32768 || strings.ContainsRune(path, 0) || !strings.HasSuffix(strings.ToLower(path), ".json") {
		return errors.New("Choose a .smartstage.json playlist file")
	}
	return nil
}

func readFile(path string) (app.PlaylistFile, error) {
	if err := validPath(path); err != nil {
		return app.PlaylistFile{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return app.PlaylistFile{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > app.MaxPlaylistFileBytes {
		return app.PlaylistFile{}, errors.New("Choose a regular playlist file no larger than 4 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return app.PlaylistFile{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return app.PlaylistFile{}, errors.New("The selected playlist file changed; choose it again")
	}
	return app.DecodePlaylist(f)
}

func saveFile(path string, data []byte) error {
	if err := validPath(path); err != nil {
		return err
	}
	if len(data) > app.MaxPlaylistFileBytes {
		return errors.New("Playlist files must not exceed 4 MiB")
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("Choose a regular playlist file as the save destination")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".smartstage-playlist-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceFile(temp, path)
}
