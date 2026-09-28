package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// sessionSettingsResponse — тело GET/POST /api/settings/session: текущие
// idle/absolute TTL сессии и границы допустимого диапазона (D-06), чтобы
// фронтенду не нужно было хардкодить константы из internal/config.
type sessionSettingsResponse struct {
	IdleTTLHours    int `json:"idle_ttl_hours"`
	AbsoluteTTLDays int `json:"absolute_ttl_days"`
	IdleTTLMin      int `json:"idle_ttl_min"`
	IdleTTLMax      int `json:"idle_ttl_max"`
	AbsoluteTTLMin  int `json:"absolute_ttl_min"`
	AbsoluteTTLMax  int `json:"absolute_ttl_max"`
}

func (a *API) sessionSettingsSnapshot() sessionSettingsResponse {
	a.cfg.RLock()
	idleTTLHours := a.cfg.Auth.SessionIdleTTLHours
	absoluteTTLDays := a.cfg.Auth.SessionAbsoluteTTLDays
	a.cfg.RUnlock()
	return sessionSettingsResponse{
		IdleTTLHours:    idleTTLHours,
		AbsoluteTTLDays: absoluteTTLDays,
		IdleTTLMin:      config.MinSessionIdleTTLHours,
		IdleTTLMax:      config.MaxSessionIdleTTLHours,
		AbsoluteTTLMin:  config.MinSessionAbsoluteTTLDays,
		AbsoluteTTLMax:  config.MaxSessionAbsoluteTTLDays,
	}
}

// SessionSettings handles GET/POST /api/settings/session (protected, D-06):
// GET reads the current idle/absolute session TTL plus the allowed range;
// POST validates and applies a new value — persisted to config.json and
// applied immediately to already-live sessions via AuthService.SetTTL.
func (a *API) SessionSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		JSONSuccess(w, a.sessionSettingsSnapshot())
		return
	case http.MethodPost:
		a.sessionSettingsUpdate(w, r)
		return
	default:
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
	}
}

func (a *API) sessionSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdleTTLHours    int `json:"idle_ttl_hours"`
		AbsoluteTTLDays int `json:"absolute_ttl_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, a.t(r, "error.invalid_request"), http.StatusBadRequest)
		return
	}

	if !config.ValidSessionTTL(req.IdleTTLHours, req.AbsoluteTTLDays) {
		JSONErrorCode(w, http.StatusBadRequest, "session_ttl_out_of_range", a.t(r, "settings.session_ttl_out_of_range"))
		return
	}

	// Lock/Unlock bracket only the field writes (134-REVIEW CR-01):
	// a.cfg is the same *config.Config the SIGHUP password-reload goroutine
	// and other handler goroutines mutate concurrently, and config.Save
	// below takes its own RLock for the marshal — holding Lock across the
	// Save call would deadlock against that RLock.
	a.cfg.Lock()
	a.cfg.Auth.SessionIdleTTLHours = req.IdleTTLHours
	a.cfg.Auth.SessionAbsoluteTTLDays = req.AbsoluteTTLDays
	a.cfg.Unlock()

	if a.cfg.ConfigPath != "" {
		if err := config.Save(a.cfg.ConfigPath, a.cfg); err != nil {
			a.errorResponse(w, a.t(r, "error.internal"), http.StatusInternalServerError)
			return
		}
	}

	a.srv.GetAuthService().SetTTL(
		time.Duration(req.IdleTTLHours)*time.Hour,
		time.Duration(req.AbsoluteTTLDays)*24*time.Hour,
	)

	JSONSuccess(w, a.sessionSettingsSnapshot())
}
