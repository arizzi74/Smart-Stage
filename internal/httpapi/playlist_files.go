package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"smartstage/internal/app"
	"smartstage/internal/playlistfile"
)

type PlaylistFiles interface {
	Available() bool
	Status() playlistfile.Status
	Start(string, uint64) (playlistfile.Status, error)
}

func (a *API) SetPlaylistFiles(controller PlaylistFiles) {
	if a.role != "admin" {
		return
	}
	a.mu.Lock()
	a.playlistFiles = controller
	a.mu.Unlock()
}

func (a *API) playlistFileRequest(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/playlist/export":
		document, err := a.app.ExportPlaylist()
		if err != nil {
			respondError(w, err)
			return
		}
		data, err := json.MarshalIndent(document, "", "  ")
		if err != nil || len(data)+1 > app.MaxPlaylistFileBytes {
			fail(w, 400, "invalid_playlist", "Playlist files must not exceed 4 MiB")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="Playlist.smartstage.json"`)
		_, _ = w.Write(append(data, '\n'))
	case "/api/playlist/import":
		var body *struct {
			ExpectedRevision uint64          `json:"expectedRevision"`
			Playlist         json.RawMessage `json:"playlist"`
		}
		if !decodeLimit(w, r, &body, app.MaxPlaylistFileBytes+1024) {
			return
		}
		if body == nil {
			fail(w, 400, "invalid_json", "Send a playlist file and expected revision")
			return
		}
		document, err := app.DecodePlaylist(bytes.NewReader(body.Playlist))
		if err != nil {
			respondError(w, err)
			return
		}
		config, err := a.app.LoadPlaylist(app.PlaylistLoad{ExpectedRevision: body.ExpectedRevision, Playlist: document})
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, config)
	case "/api/playlist/file":
		a.mu.RLock()
		controller := a.playlistFiles
		a.mu.RUnlock()
		if controller == nil || !controller.Available() {
			fail(w, 501, "playlist_files_unavailable", "Native playlist file selection is unavailable")
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, 200, controller.Status())
			return
		}
		var body *struct {
			Operation        string `json:"operation"`
			ExpectedRevision uint64 `json:"expectedRevision"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body == nil {
			fail(w, 400, "invalid_json", "Choose save or load and an expected revision")
			return
		}
		status, err := controller.Start(body.Operation, body.ExpectedRevision)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 202, status)
	}
}
