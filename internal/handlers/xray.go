package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi"
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

func (a *API) reloadXrayIfRunning() {
	if a.consoleSvc != nil && a.kernelSvc != nil {
		if k := a.kernelSvc.Get("xray"); k != nil && k.ProcessStatus == "running" {
			_, _ = a.consoleSvc.Execute("-restart")
		}
	}
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

	info := services.FindXrayAPIFragment(a.cfg.XRayConfigDir)
	if !info.FileExists && !info.HasAnyJSON {
		a.errorResponse(w, "Xray configuration file not found or not readable", http.StatusServiceUnavailable)
		return
	}
	configPath := info.Path
	if a.pathVal != nil {
		cleanPath, err := a.pathVal.Validate(configPath)
		if err != nil {
			a.errorResponse(w, fmt.Sprintf("invalid config path: %v", err), http.StatusBadRequest)
			return
		}
		configPath = cleanPath
	}

	var originalContent string
	if info.FileExists {
		data, err := os.ReadFile(configPath)
		if err != nil {
			a.errorResponse(w, "Xray configuration file not found or not readable", http.StatusServiceUnavailable)
			return
		}
		originalContent = string(data)
	}

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
		if ok, out := services.ValidateXrayConfigDir(a.cfg.XRayConfigDir); !ok {
			// Rollback
			if info.FileExists {
				_ = utils.AtomicWriteFile(configPath, []byte(originalContent), 0600)
			} else {
				_ = os.Remove(configPath)
			}
			a.errorResponse(w, fmt.Sprintf("Xray config validation failed, rolled back: %s", out), http.StatusServiceUnavailable)
			return
		}

		a.reloadXrayIfRunning()
		JSONSuccess(w, map[string]interface{}{"enabled": true})
		return
	}

	// Disable monitoring
	if !info.APIPresent {
		// Already disabled or no api block found
		JSONSuccess(w, map[string]interface{}{"enabled": false})
		return
	}

	newContent, err := services.DeprovisionXrayAPIBlock(originalContent)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// If it was modular 00_api.json and no other inbounds remain, remove the file entirely
	shouldRemoveModular := false
	if info.IsModular && filepath.Base(configPath) == "00_api.json" {
		var checkRoot map[string]interface{}
		if err := json.Unmarshal([]byte(newContent), &checkRoot); err == nil {
			inbs, _ := checkRoot["inbounds"].([]interface{})
			if len(inbs) == 0 {
				shouldRemoveModular = true
			}
		}
	}

	if shouldRemoveModular {
		_ = os.Remove(configPath)
	} else {
		if err := utils.AtomicWriteFile(configPath, []byte(newContent), 0600); err != nil {
			a.errorResponse(w, fmt.Sprintf("failed to write config: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Dry-run verification on disable path
	if ok, out := services.ValidateXrayConfigDir(a.cfg.XRayConfigDir); !ok {
		// Rollback
		_ = utils.AtomicWriteFile(configPath, []byte(originalContent), 0600)
		a.errorResponse(w, fmt.Sprintf("Xray config validation failed, rolled back: %s", out), http.StatusServiceUnavailable)
		return
	}

	a.reloadXrayIfRunning()
	JSONSuccess(w, map[string]interface{}{"enabled": false})
}

// XrayTestRouteRequest defines parameters for testing an Xray route.
type XrayTestRouteRequest struct {
	Domain     string            `json:"domain"`
	IP         string            `json:"ip"`
	Port       int               `json:"port"`
	Network    string            `json:"network"`
	Protocol   string            `json:"protocol"`
	InboundTag string            `json:"inbound_tag"`
	Attributes map[string]string `json:"attributes"`
}

// XrayTestRoute handles POST /api/xray/test-route to test which routing rule matches a destination.
func (a *API) XrayTestRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req XrayTestRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	domain := strings.TrimSpace(req.Domain)
	ipStr := strings.TrimSpace(req.IP)

	if domain == "" && ipStr == "" {
		a.errorResponse(w, "domain or ip is required", http.StatusBadRequest)
		return
	}

	if domain != "" {
		if len(domain) > 253 {
			a.errorResponse(w, "domain exceeds maximum length of 253 characters", http.StatusBadRequest)
			return
		}
		for _, c := range domain {
			if unicode.IsControl(c) || unicode.IsSpace(c) {
				a.errorResponse(w, "domain contains invalid or control characters", http.StatusBadRequest)
				return
			}
		}
	}

	if ipStr != "" {
		if parsed := net.ParseIP(ipStr); parsed == nil {
			a.errorResponse(w, "invalid IP address format", http.StatusBadRequest)
			return
		}
	}

	if req.Port < 0 || req.Port > 65535 {
		a.errorResponse(w, "port must be between 1 and 65535", http.StatusBadRequest)
		return
	}

	targetPort := req.Port
	if targetPort == 0 {
		targetPort = 443
	}

	if len(req.Attributes) > 50 {
		a.errorResponse(w, "too many attributes", http.StatusBadRequest)
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
		a.errorResponse(w, "Xray core is unavailable", http.StatusServiceUnavailable)
		return
	}
	defer release()

	result, err := client.TestRoute(ctx, xrayapi.RouteTestInput{
		Domain:     domain,
		IP:         ipStr,
		Port:       uint32(targetPort),
		Network:    req.Network,
		Protocol:   req.Protocol,
		InboundTag: req.InboundTag,
		Attributes: req.Attributes,
	})
	if err != nil {
		if errors.Is(err, xrayapi.ErrCoreUnavailable) {
			a.errorResponse(w, "Xray core is unavailable", http.StatusServiceUnavailable)
			return
		}
		if errors.Is(err, xrayapi.ErrInvalidArgument) {
			a.errorResponse(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.errorResponse(w, fmt.Sprintf("Failed to test route: %v", err), http.StatusServiceUnavailable)
		return
	}

	JSONSuccess(w, result)
}

// XrayRestartLogger handles POST /api/xray/restart-logger to request Xray core
// to reopen its log files for safe external log rotation (e.g. logrotate).
func (a *API) XrayRestartLogger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.xrayGRPCSvc == nil {
		a.errorResponse(w, "Xray gRPC service is not configured", http.StatusServiceUnavailable)
		return
	}

	a.restartLoggerMutex.Lock()
	now := time.Now()
	if !a.lastRestartLogger.IsZero() && now.Sub(a.lastRestartLogger) < 5*time.Second {
		remSec := int(math.Ceil(5 - now.Sub(a.lastRestartLogger).Seconds()))
		a.restartLoggerMutex.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(remSec))
		a.errorResponse(w, fmt.Sprintf("Too many requests: retry after %d seconds", remSec), http.StatusTooManyRequests)
		return
	}
	a.lastRestartLogger = now
	a.restartLoggerMutex.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	client, release, err := a.xrayGRPCSvc.Acquire(ctx)
	if err != nil {
		a.errorResponse(w, "Xray core is unavailable", http.StatusServiceUnavailable)
		return
	}
	defer release()

	if err := client.RestartLogger(ctx); err != nil {
		if errors.Is(err, xrayapi.ErrCoreUnavailable) {
			a.errorResponse(w, "Xray core is unavailable", http.StatusServiceUnavailable)
			return
		}
		a.errorResponse(w, fmt.Sprintf("Failed to restart logger: %v", err), http.StatusServiceUnavailable)
		return
	}

	JSONSuccess(w, map[string]interface{}{
		"success": true,
		"message": "Log files reopened successfully for rotation",
	})
}

// XrayTLSPingRequest defines payload for testing TLS handshake.
type XrayTLSPingRequest struct {
	Dest       string   `json:"dest"`
	ServerName string   `json:"server_name"`
	ALPN       []string `json:"alpn"`
}

// XrayTLSPing handles POST /api/xray/tls-ping to perform a native TLS handshake ping.
func (a *API) XrayTLSPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req XrayTLSPingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "invalid request body", http.StatusBadRequest)
		return
	}

	panelPort := 8090
	if a.cfg != nil && a.cfg.Port > 0 {
		panelPort = a.cfg.Port
	}

	dialAddr, defaultSNI, err := services.ValidateTLSTarget(req.Dest, panelPort)
	if err != nil {
		a.errorResponse(w, fmt.Sprintf("invalid destination: %v", err), http.StatusBadRequest)
		return
	}

	serverName := strings.TrimSpace(req.ServerName)
	if len(serverName) > 253 {
		a.errorResponse(w, "server_name exceeds maximum length of 253 characters", http.StatusBadRequest)
		return
	}
	if serverName == "" {
		serverName = defaultSNI
	}

	result, err := services.TLSPing(dialAddr, serverName, req.ALPN)
	if err != nil {
		a.errorResponse(w, fmt.Sprintf("tls ping failed: %v", err), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, result)
}


