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
	JSONSuccess(w, SettingsResponse{
		Port:    a.cfg.Port,
		HTTPS:   a.cfg.HTTPS,
		DevMode: a.cfg.DevMode,
	})
}

func (a *API) SettingsHTTPS(w http.ResponseWriter, r *http.Request) {
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

	a.cfg.HTTPS.Enabled = req.Enabled

	if a.cfg.ConfigPath != "" {
		if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}

	JSONSuccess(w, map[string]interface{}{
		"https":            a.cfg.HTTPS,
		"restart_required": true,
	})
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

	a.cfg.DevMode = req.Enabled

	if a.cfg.ConfigPath != "" {
		if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
			JSONError(w, http.StatusInternalServerError, "failed to save config: "+err.Error())
			return
		}
	}

	JSONSuccess(w, map[string]bool{"dev_mode": a.cfg.DevMode})
}
