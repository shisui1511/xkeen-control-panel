package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// RouteTest handles POST /api/rules/test to simulate routing evaluation for a domain or IP.
func (a *API) RouteTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req services.RouteTraceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cleanTarget := strings.TrimSpace(req.Target)
	if cleanTarget == "" {
		a.errorResponse(w, "Target cannot be empty", http.StatusBadRequest)
		return
	}
	if len(cleanTarget) > 255 {
		a.errorResponse(w, "Target exceeds maximum length of 255 characters", http.StatusBadRequest)
		return
	}

	if a.routeTracerSvc == nil {
		a.errorResponse(w, "Route tracer service not available", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result, err := a.routeTracerSvc.TraceRoute(ctx, cleanTarget, req.Port)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	JSONSuccess(w, result)
}
