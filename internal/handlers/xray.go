package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// XrayRealityKeygen handles GET /api/xray/reality/keygen to generate Reality keypairs.
func (a *API) XrayRealityKeygen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	kp, err := services.GenerateRealityKeypair()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, kp)
}

// XrayStats handles GET /api/xray/stats to fetch outbound traffic stats from Xray via gRPC.
func (a *API) XrayStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.xrayGRPCSvc == nil {
		a.errorResponse(w, "Xray gRPC service is not configured", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	client, release, err := a.xrayGRPCSvc.Acquire(ctx)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer release()

	stats, err := client.OutboundTraffic(ctx)
	if err != nil {
		a.errorResponse(w, fmt.Sprintf("Failed to query Xray stats: %v", err), http.StatusServiceUnavailable)
		return
	}

	JSONSuccess(w, stats)
}
