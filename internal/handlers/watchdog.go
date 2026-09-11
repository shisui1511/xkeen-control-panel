package handlers

import (
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
