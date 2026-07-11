package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

type ProxyProviderResponse struct {
	services.Subscription
	MihomoProvider *MihomoProviderInfo `json:"mihomo_provider"`
}

type MihomoProviderInfo struct {
	Name        string `json:"name"`
	VehicleType string `json:"vehicle_type"`
	UpdatedAt   string `json:"updated_at"`
	NodeCount   int    `json:"node_count"`
}

type ClashProvider struct {
	Name        string        `json:"name"`
	VehicleType string        `json:"vehicleType"`
	UpdatedAt   string        `json:"updatedAt"`
	Proxies     []interface{} `json:"proxies"`
}

type ClashProvidersResponse struct {
	Providers map[string]ClashProvider `json:"providers"`
}

func (a *API) ProxyProvidersRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && (r.URL.Path == "/api/proxy-providers" || r.URL.Path == "/api/proxy-providers/") {
		a.ProxyProvidersList(w, r)
		return
	}
	if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/proxy-providers/") && strings.HasSuffix(r.URL.Path, "/refresh") {
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/proxy-providers/")
		name := strings.TrimSuffix(trimmed, "/refresh")
		if name != "" {
			a.ProxyProviderRefresh(w, r, name)
			return
		}
	}
	a.errorResponse(w, "Not Found", http.StatusNotFound)
}

func (a *API) ProxyProvidersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	subs := a.subscriptionSvc.List()

	// 1. Check if Mihomo is running
	running := false
	if a.mihomoSvc != nil {
		status, err := a.mihomoSvc.Status()
		if err == nil && strings.HasPrefix(status, "running") {
			running = true
		}
	}

	clashProviders := make(map[string]ClashProvider)
	if running {
		// 2. Fetch providers from Clash API
		secret := a.ResolveMihomoSecret()

		req, err := http.NewRequest(http.MethodGet, a.cfg.MihomoAPIURL+"/providers/proxies", nil)
		if err != nil {
			log.Printf("[ProxyProviders] Error creating request: %v", err)
		} else {
			if secret != "" {
				req.Header.Set("Authorization", "Bearer "+secret)
			}
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[ProxyProviders] Warning: failed to fetch proxy providers from Clash API: %v", err)
			} else {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var clashResp ClashProvidersResponse
					if err := json.NewDecoder(resp.Body).Decode(&clashResp); err != nil {
						log.Printf("[ProxyProviders] Error decoding Clash API response: %v", err)
					} else {
						clashProviders = clashResp.Providers
					}
				} else {
					log.Printf("[ProxyProviders] Warning: Clash API returned status %d", resp.StatusCode)
				}
			}
		}
	} else {
		log.Printf("[ProxyProviders] Debug: Clash API unreachable (Mihomo is not running)")
	}

	// 3. Enrich subscriptions with Clash API state
	respList := make([]ProxyProviderResponse, len(subs))
	for i, sub := range subs {
		respList[i] = ProxyProviderResponse{
			Subscription: sub,
		}
		providerName := sub.GetProviderName()
		if provider, ok := clashProviders[providerName]; ok {
			respList[i].MihomoProvider = &MihomoProviderInfo{
				Name:        provider.Name,
				VehicleType: provider.VehicleType,
				UpdatedAt:   provider.UpdatedAt,
				NodeCount:   len(provider.Proxies),
			}
		}
	}

	a.jsonResponse(w, respList)
}

func (a *API) ProxyProviderRefresh(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPut {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	err := a.subscriptionSvc.TriggerMihomoProviderReload(name)
	if err != nil {
		log.Printf("[ProxyProviders] Error reloading provider %s: %v", name, err)
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
