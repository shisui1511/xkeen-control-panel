package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

type SettingsResponse struct {
	Port  int                `json:"port"`
	HTTPS config.HTTPSConfig `json:"https"`
}

func (a *API) SettingsGet(w http.ResponseWriter, r *http.Request) {
	JSONSuccess(w, SettingsResponse{
		Port:  a.cfg.Port,
		HTTPS: a.cfg.HTTPS,
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
