package handlers

import (
	"net/http"
	"os"
	"strings"
	"time"
)

// CapabilitiesResponse describes which backend features are available.
type CapabilitiesResponse struct {
	Kernels      map[string]KernelCapability `json:"kernels"`
	Mihomo       MihomoCapability            `json:"mihomo"`
	XRay         XRayCapability              `json:"xray"`
	ActiveKernel string                      `json:"active_kernel"`
	XKeenDNS     bool                        `json:"xkeen_dns"`
	GlobalHwid   string                      `json:"global_hwid,omitempty"`
}

// XRayCapability describes XRay confdir setup status.
type XRayCapability struct {
	// ConfDir — путь к директории конфигов XRay (из cfg.XRayConfigDir).
	ConfDir string `json:"conf_dir"`
	// ConfDirExists — true если директория существует на диске.
	// Если false — fragment-файлы подписок не будут подхвачены XRay.
	ConfDirExists bool `json:"conf_dir_exists"`
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
	APIURL           string `json:"api_url,omitempty"`
	DiscoveredSecret string `json:"discovered_secret,omitempty"`
}

func (a *API) Capabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
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
	resp.Mihomo.APIURL = a.cfg.MihomoAPIURL
	resp.Mihomo.DiscoveredSecret = discoveredSecret

	// Detect which kernel is currently active
	var activeKernel string
	if a.kernelSvc != nil {
		for _, info := range a.kernelSvc.List() {
			if info.ProcessStatus == "running" {
				activeKernel = info.Name
				break
			}
		}
	}
	if activeKernel == "" && a.xkeenSvc != nil {
		// Fallback to checking from xkeen -status raw output
		if status, err := a.xkeenSvc.Status(); err == nil {
			lower := strings.ToLower(status)
			if strings.Contains(lower, "xray") {
				activeKernel = "xray"
			} else if strings.Contains(lower, "mihomo") {
				activeKernel = "mihomo"
			}
		}
	}
	if activeKernel == "" {
		activeKernel = "none"
	}
	resp.ActiveKernel = activeKernel

	// XRay confdir capability
	resp.XRay.ConfDir = a.cfg.XRayConfigDir
	if _, err := os.Stat(a.cfg.XRayConfigDir); err == nil {
		resp.XRay.ConfDirExists = true
	}

	if a.xkeenSvc != nil {
		resp.XKeenDNS = a.xkeenSvc.IsDNSProxyingEnabled()
	}

	if a.subscriptionSvc != nil {
		resp.GlobalHwid = a.subscriptionSvc.GetHWID()
	}

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
