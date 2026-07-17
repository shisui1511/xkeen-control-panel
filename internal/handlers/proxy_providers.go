package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// providerNameRe — допустимый формат имени Mihomo-провайдера: латинские
// буквы (регистр сохраняется, например «TEST_PROVIDER»), цифры, дефис и
// подчёркивание (встречается в ключах рукописных config.yaml, импортируемых
// панелью). Панель сама генерирует имена по инвариванту GetMihomoProviderName
// (subscription_converter.go): [A-Za-z0-9-]. Всё остальное — мусорный ввод,
// который не должен уходить в исходящий запрос к Clash API.
var providerNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

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
		// name != trimmed гарантирует, что суффикс "/refresh" действительно
		// был отрезан: путь /api/proxy-providers/refresh (без имени) не должен
		// трактоваться как обновление провайдера с именем "refresh".
		if name != "" && name != trimmed {
			a.ProxyProviderRefresh(w, r, name)
			return
		}
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/proxy-providers/") && strings.HasSuffix(r.URL.Path, "/nodes") {
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/proxy-providers/")
		name := strings.TrimSuffix(trimmed, "/nodes")
		if name != "" && name != trimmed {
			a.ProxyProviderNodes(w, r, name)
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

	if !providerNameRe.MatchString(name) {
		a.errorResponse(w, a.t(r, "error.bad_request"), http.StatusBadRequest)
		return
	}

	err := a.subscriptionSvc.TriggerMihomoProviderReload(name)
	if err != nil {
		// Полный текст ошибки — только в лог сервера; клиенту отдаются
		// локализованные сообщения без внутренних деталей (URL контроллера и т.п.).
		log.Printf("[ProxyProviders] Error reloading provider %s: %v", name, err)
		var statusErr *services.MihomoAPIStatusError
		switch {
		case errors.Is(err, services.ErrMihomoAPINotConfigured):
			a.errorResponse(w, a.t(r, "mihomo.api_not_configured"), http.StatusServiceUnavailable)
		case errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound:
			// Clash API не знает такого провайдера.
			a.errorResponse(w, a.t(r, "error.not_found"), http.StatusNotFound)
		case errors.As(err, &statusErr):
			a.errorResponse(w, a.t(r, "mihomo.api_error"), http.StatusBadGateway)
		default:
			// Сетевые ошибки: Mihomo не запущен / API недоступен.
			a.errorResponse(w, a.t(r, "mihomo.not_running"), http.StatusBadGateway)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type MihomoNodeInfo struct {
	Tag     string `json:"tag"`
	Name    string `json:"name"`
	Alive   bool   `json:"alive"`
	Tested  bool   `json:"tested"`
	DelayMs int    `json:"delay_ms"`
}

type clashProxyEntry struct {
	Name    string `json:"name"`
	Alive   bool   `json:"alive"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

func (a *API) ProxyProviderNodes(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if !providerNameRe.MatchString(name) {
		a.errorResponse(w, a.t(r, "error.bad_request"), http.StatusBadRequest)
		return
	}

	secret := a.ResolveMihomoSecret()
	targetURL := fmt.Sprintf("%s/providers/proxies/%s", a.cfg.MihomoAPIURL, url.PathEscape(name))

	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		log.Printf("[ProxyProviders] Error creating request for nodes: %v", err)
		a.errorResponse(w, a.t(r, "mihomo.not_running"), http.StatusBadGateway)
		return
	}

	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ProxyProviders] Error fetching nodes from Clash API: %v", err)
		a.errorResponse(w, a.t(r, "mihomo.not_running"), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		a.errorResponse(w, a.t(r, "error.not_found"), http.StatusNotFound)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ProxyProviders] Clash API nodes request returned status %d", resp.StatusCode)
		a.errorResponse(w, a.t(r, "mihomo.api_error"), http.StatusBadGateway)
		return
	}

	var clashResp struct {
		Proxies []clashProxyEntry `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&clashResp); err != nil {
		log.Printf("[ProxyProviders] Error decoding Clash API nodes response: %v", err)
		a.errorResponse(w, a.t(r, "mihomo.api_error"), http.StatusBadGateway)
		return
	}

	nodes := make([]MihomoNodeInfo, len(clashResp.Proxies))
	for i, p := range clashResp.Proxies {
		tested := len(p.History) > 0
		var delay int
		if tested {
			delay = p.History[len(p.History)-1].Delay
		}
		alive := false
		if tested {
			alive = p.Alive && delay > 0
		}

		nodes[i] = MihomoNodeInfo{
			Tag:     p.Name,
			Name:    p.Name,
			Alive:   alive,
			Tested:  tested,
			DelayMs: delay,
		}
	}

	a.jsonResponse(w, nodes)
}
