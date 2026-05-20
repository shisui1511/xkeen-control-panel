package handlers

import (
	"net/http"
	"time"
)

// CapabilitiesResponse describes which backend features are available.
type CapabilitiesResponse struct {
	Kernels map[string]KernelCapability `json:"kernels"`
	Mihomo  MihomoCapability            `json:"mihomo"`
}

// KernelCapability holds install status for a single kernel.
type KernelCapability struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Channel   string `json:"channel,omitempty"`
}

// MihomoCapability describes live connectivity to the Mihomo API.
type MihomoCapability struct {
	Reachable bool `json:"reachable"`
}

// Capabilities handles GET /api/capabilities.
// This is a protected endpoint: the frontend uses it to conditionally render
// UI elements based on what the backend can actually do.
func (a *API) Capabilities(w http.ResponseWriter, r *http.Request) {
	resp := CapabilitiesResponse{
		Kernels: make(map[string]KernelCapability),
	}

	// Collect kernel statuses (if kernelSvc is wired)
	if a.kernelSvc != nil {
		for _, info := range a.kernelSvc.List() {
			installed := info.ProcessStatus != "not_installed"
			resp.Kernels[info.Name] = KernelCapability{
				Installed: installed,
				Version:   info.CurrentVersion,
				Channel:   info.Channel,
			}
		}
	}

	// Probe Mihomo API with a short timeout
	resp.Mihomo.Reachable = probeMihomoReachable(a.cfg.MihomoAPIURL)

	JSONSuccess(w, resp)
}

// probeMihomoReachable attempts GET <mihomoURL>/version with a 3-second timeout.
func probeMihomoReachable(mihomoURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(mihomoURL + "/version")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
