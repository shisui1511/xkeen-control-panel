package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

// XrayGRPCMonitoringRequest represents the payload for enabling/disabling gRPC monitoring.
type XrayGRPCMonitoringRequest struct {
	Enabled bool `json:"enabled"`
}

// XrayGRPCMonitoring handles POST /api/xray/grpc/monitoring to enable or disable
// the gRPC api block in Xray's config.json.
func (a *API) XrayGRPCMonitoring(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req XrayGRPCMonitoringRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if a.cfg == nil || a.cfg.XRayConfigDir == "" {
		a.errorResponse(w, "Xray config directory is not configured", http.StatusServiceUnavailable)
		return
	}

	configPath := filepath.Join(a.cfg.XRayConfigDir, "config.json")
	if a.pathVal != nil {
		cleanPath, err := a.pathVal.Validate(configPath)
		if err != nil {
			a.errorResponse(w, fmt.Sprintf("invalid config path: %v", err), http.StatusBadRequest)
			return
		}
		configPath = cleanPath
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		a.errorResponse(w, "Xray configuration file not found or not readable", http.StatusServiceUnavailable)
		return
	}

	originalContent := string(data)

	if req.Enabled {
		port := a.cfg.XRayAPIPort
		if port == 0 {
			port = 10085
		}

		newContent, err := services.ProvisionXrayAPIBlock(originalContent, port)
		if err != nil {
			if strings.Contains(err.Error(), "already in use") {
				a.errorResponse(w, err.Error(), http.StatusConflict)
				return
			}
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := utils.AtomicWriteFile(configPath, []byte(newContent), 0600); err != nil {
			a.errorResponse(w, fmt.Sprintf("failed to write config: %v", err), http.StatusInternalServerError)
			return
		}

		// Dry-run verification if xray binary exists
		xrayBin := "xray"
		if a.cfg.XKeenBinary != "" {
			binDir := filepath.Dir(a.cfg.XKeenBinary)
			candidate := filepath.Join(binDir, "xray")
			if _, err := os.Stat(candidate); err == nil {
				xrayBin = candidate
			}
		}
		if p, err := exec.LookPath(xrayBin); err == nil {
			cmd := exec.Command(p, "-test", "-config", configPath)
			if out, err := cmd.CombinedOutput(); err != nil {
				// Rollback
				_ = utils.AtomicWriteFile(configPath, []byte(originalContent), 0600)
				a.errorResponse(w, fmt.Sprintf("Xray config validation failed, rolled back: %s", string(out)), http.StatusServiceUnavailable)
				return
			}
		}

		JSONSuccess(w, map[string]interface{}{"enabled": true})
		return
	}

	// Disable monitoring
	newContent, err := services.DeprovisionXrayAPIBlock(originalContent)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := utils.AtomicWriteFile(configPath, []byte(newContent), 0600); err != nil {
		a.errorResponse(w, fmt.Sprintf("failed to write config: %v", err), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, map[string]interface{}{"enabled": false})
}

