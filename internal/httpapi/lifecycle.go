package httpapi

import (
	"net/http"
	"time"
)

// SetQuit registers the host's nonblocking graceful-shutdown request. Only the
// loopback Admin listener can invoke it, after session, Origin and CSRF checks.
func (a *API) SetQuit(quit func()) {
	if a.role != "admin" {
		return
	}
	a.mu.Lock()
	a.quit = quit
	a.mu.Unlock()
}

// SetChooseFiles registers a nonblocking native chooser request and its current
// availability. The browser never supplies a path to this operation.
func (a *API) SetChooseFiles(choose, available func() bool) {
	if a.role != "admin" {
		return
	}
	a.mu.Lock()
	a.chooseFiles, a.canChooseFiles = choose, available
	a.mu.Unlock()
}

func (a *API) addCapabilities(reply map[string]any) {
	if a.role != "admin" {
		return
	}
	a.mu.RLock()
	choose, available := a.chooseFiles, a.canChooseFiles
	language := a.language
	a.mu.RUnlock()
	reply["capabilities"] = map[string]bool{"chooseFiles": choose != nil && available != nil && available()}
	if language != nil {
		reply["language"] = language.Snapshot()
	}
}

// HasAdminPresence reports a live authenticated Admin page on this process.
// Ordinary state/session probes do not count. Existing versions without the
// page heartbeat can still prove their presence through an active Admin SSE.
func (a *API) HasAdminPresence() bool {
	if a.role != "admin" {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.adminEvents > 0 || (!a.adminSeen.IsZero() && time.Since(a.adminSeen) < 10*time.Second)
}

func (a *API) lifecycleRequest(w http.ResponseWriter, r *http.Request) {
	var body *struct{}
	if !decode(w, r, &body) {
		return
	}
	if body == nil {
		fail(w, http.StatusBadRequest, "invalid_json", "Send an empty JSON object")
		return
	}
	if r.URL.Path == "/api/admin-presence" {
		a.mu.Lock()
		a.adminSeen = time.Now()
		a.mu.Unlock()
		reply := map[string]any{"present": true}
		a.addCapabilities(reply)
		writeJSON(w, http.StatusOK, reply)
		return
	}
	if r.URL.Path == "/api/choose-files" {
		a.mu.RLock()
		choose, available := a.chooseFiles, a.canChooseFiles
		a.mu.RUnlock()
		if choose == nil || available == nil || !available() {
			fail(w, http.StatusNotImplemented, "choose_files_unavailable", "Native file selection is unavailable in this host")
			return
		}
		if !choose() {
			fail(w, http.StatusServiceUnavailable, "choose_files_unavailable", "Native file selection is not ready; try again")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"choosing": true})
		return
	}
	a.mu.RLock()
	quit := a.quit
	a.mu.RUnlock()
	if quit == nil {
		fail(w, http.StatusServiceUnavailable, "quit_unavailable", "This host cannot be quit from Admin")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"quitting": true})
	// Deliver the acknowledgement before cancellation closes SSE and initiates
	// HTTP shutdown. The callback only requests shutdown; it must not wait for
	// this handler (or for native/UI cleanup) to finish.
	_ = http.NewResponseController(w).Flush()
	a.quitOnce.Do(quit)
}
