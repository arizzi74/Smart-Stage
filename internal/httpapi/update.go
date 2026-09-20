package httpapi

import (
	"errors"
	"net/http"

	"smartstage/internal/app"
	"smartstage/internal/update"
)

// UpdateController is configured only on the localhost Admin listener. Update
// routes use the same session, Origin and CSRF checks as saved-show mutations.
type UpdateController interface {
	Status() update.Status
	Check() error
	Install() error
}

func (a *API) SetUpdater(updater UpdateController) {
	if a.role != "admin" {
		return
	}
	a.mu.Lock()
	a.updater = updater
	a.mu.Unlock()
}

func (a *API) updateRequest(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	updater := a.updater
	a.mu.RUnlock()
	if updater == nil {
		fail(w, http.StatusServiceUnavailable, "updates_unavailable", "Updates are unavailable in this host")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, updater.Status())
		return
	}
	var body *struct{}
	if !decode(w, r, &body) {
		return
	}
	if body == nil {
		fail(w, http.StatusBadRequest, "invalid_json", "Send an empty JSON object")
		return
	}
	var err error
	if r.URL.Path == "/api/update/check" {
		err = updater.Check()
	} else {
		err = updater.Install()
	}
	if err != nil {
		var appError *app.Error
		if errors.As(err, &appError) {
			respondError(w, err)
		} else {
			fail(w, http.StatusConflict, "update_unavailable", err.Error())
		}
		return
	}
	writeJSON(w, http.StatusAccepted, updater.Status())
}
