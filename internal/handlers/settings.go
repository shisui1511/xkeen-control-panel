package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

type SettingsResponse struct {
	Port    int                `json:"port"`
	HTTPS   config.HTTPSConfig `json:"https"`
	DevMode bool               `json:"dev_mode"`
}

func (a *API) SettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	a.cfg.RLock()
	resp := SettingsResponse{
		Port:    a.cfg.Port,
		HTTPS:   a.cfg.HTTPS,
		DevMode: a.cfg.DevMode,
	}
	a.cfg.RUnlock()
	JSONSuccess(w, resp)
}

func (a *API) SettingsDevMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Lock/Unlock bracket only the field write (134-REVIEW CR-01); config.Save
	// below takes its own RLock for the marshal, so it must run unlocked here.
	a.cfg.Lock()
	a.cfg.DevMode = req.Enabled
	a.cfg.Unlock()

	if a.cfg.ConfigPath != "" {
		if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}

	a.cfg.RLock()
	devMode := a.cfg.DevMode
	a.cfg.RUnlock()
	JSONSuccess(w, map[string]bool{"dev_mode": devMode})
}
