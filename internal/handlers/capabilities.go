package handlers

import (
	"net/http"
	"strings"
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

// MihomoCapability describes live connectivity and status of the Mihomo API.
type MihomoCapability struct {
	ProcessRunning   bool   `json:"process_running"`
	APIReachable     bool   `json:"api_reachable"`
	APIAuthenticated bool   `json:"api_authenticated"`
	Reachable        bool   `json:"reachable"` // backward compatibility
	DiscoveredSecret string `json:"discovered_secret,omitempty"`
}

func (a *API) Capabilities(w http.ResponseWriter, r *http.Request) {
	a.capsCacheMutex.Lock()
	if a.capsCache != nil && time.Since(a.capsCacheTime) < 3*time.Second {
		cached := a.capsCache
		a.capsCacheMutex.Unlock()
		JSONSuccess(w, cached)
		return
	}
	a.capsCacheMutex.Unlock()

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

	var running bool
	if a.mihomoSvc != nil {
		if status, err := a.mihomoSvc.Status(); err == nil {
			running = strings.Contains(status, "running")
		}
	}

	var discoveredSecret string
	secret := a.cfg.MihomoSecret
	if secret == "" && a.mihomoSvc != nil {
		if _, parsedSecret, err := a.mihomoSvc.ParseConfig(); err == nil && parsedSecret != "" {
			secret = parsedSecret
			discoveredSecret = maskSecret(parsedSecret)
		}
	} else if secret != "" {
		discoveredSecret = maskSecret(secret)
	}

	reachable, authenticated := probeMihomoAPI(a.cfg.MihomoAPIURL, secret)

	resp.Mihomo.ProcessRunning = running
	resp.Mihomo.APIReachable = reachable
	resp.Mihomo.APIAuthenticated = authenticated
	resp.Mihomo.Reachable = reachable
	resp.Mihomo.DiscoveredSecret = discoveredSecret

	a.capsCacheMutex.Lock()
	a.capsCache = resp
	a.capsCacheTime = time.Now()
	a.capsCacheMutex.Unlock()

	JSONSuccess(w, resp)
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

// probeMihomoAPI attempts GET <mihomoURL>/version with a 3-second timeout and proper secret token.
func probeMihomoAPI(mihomoURL string, secret string) (reachable bool, authenticated bool) {
	req, err := http.NewRequest(http.MethodGet, mihomoURL+"/version", nil)
	if err != nil {
		return false, false
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, true
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return true, false
	}
	return true, false
}
