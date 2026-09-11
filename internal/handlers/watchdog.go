package handlers

import (
	"errors"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// WatchdogStatusResponse is the JSON schema representing the watchdog state in API responses.
type WatchdogStatusResponse struct {
	State               string `json:"state"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	DisarmAttempts      int    `json:"disarm_attempts"`
	LastDisarmError     string `json:"last_disarm_error"`
	InterceptionActive  bool   `json:"interception_active"`
	InterceptionFamily  string `json:"interception_family"`
	NextAttemptAt       int64  `json:"next_attempt_at"`
	DegradedAt          int64  `json:"degraded_at"`
}

// newWatchdogStatusResponse converts an internal WatchdogSnapshot to the external WatchdogStatusResponse schema.
func newWatchdogStatusResponse(s services.WatchdogSnapshot) WatchdogStatusResponse {
	var nextAttemptAt int64
	if !s.NextAttemptAt.IsZero() {
		nextAttemptAt = s.NextAttemptAt.Unix()
	}
	var degradedAt int64
	if !s.DegradedAt.IsZero() {
		degradedAt = s.DegradedAt.Unix()
	}

	return WatchdogStatusResponse{
		State:               s.State,
		ConsecutiveFailures: s.ConsecutiveFailures,
		DisarmAttempts:      s.DisarmAttempts,
		LastDisarmError:     s.LastDisarmError,
		InterceptionActive:  s.InterceptionActive,
		InterceptionFamily:  s.InterceptionFamily,
		NextAttemptAt:       nextAttemptAt,
		DegradedAt:          degradedAt,
	}
}

// WatchdogStatus returns the current snapshot of watchdog state as JSON.
func (a *API) WatchdogStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.watchdogSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "watchdog service not available")
		return
	}
	JSONSuccess(w, newWatchdogStatusResponse(a.watchdogSvc.Snapshot()))
}

// WatchdogReset triggers a manual reset of watchdog counters, latches, and cooldowns.
func (a *API) WatchdogReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.watchdogSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "watchdog service not available")
		return
	}

	snapshot, err := a.watchdogSvc.TryReset()
	if err != nil {
		if errors.Is(err, services.ErrWatchdogResetInFlight) || errors.Is(err, services.ErrWatchdogResetCooldown) {
			JSONError(w, http.StatusConflict, err.Error())
			return
		}
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSONSuccess(w, newWatchdogStatusResponse(snapshot))
}
