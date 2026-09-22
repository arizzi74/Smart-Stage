package httpapi

import (
	"errors"
	"net/http"

	"smartstage/internal/locale"
)

func (a *API) SetLanguage(manager *locale.Manager) {
	if a.role != "admin" {
		return
	}
	a.mu.Lock()
	a.language = manager
	a.mu.Unlock()
}

func (a *API) languageRequest(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	manager := a.language
	a.mu.RUnlock()
	if manager == nil {
		fail(w, http.StatusServiceUnavailable, "language_unavailable", "Language settings are unavailable")
		return
	}
	if r.Method == http.MethodPut {
		var body *struct {
			Mode string `json:"mode"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body == nil {
			fail(w, http.StatusBadRequest, "invalid_language", "Select system, en or it")
			return
		}
		if err := manager.Set(body.Mode); err != nil {
			if errors.Is(err, locale.ErrMode) {
				fail(w, http.StatusBadRequest, "invalid_language", "Select system, en or it")
			} else {
				fail(w, http.StatusInternalServerError, "language_save_failed", "Could not save the language preference")
			}
			return
		}
	}
	writeJSON(w, http.StatusOK, manager.Snapshot())
}
