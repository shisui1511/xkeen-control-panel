package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) SubscriptionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	subs := a.subscriptionSvc.List()
	a.jsonResponse(w, subs)
}

func (a *API) SubscriptionAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var sub services.Subscription
	if err := json.Unmarshal(body, &sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var presence struct {
		EnableXray   *bool `json:"enable_xray"`
		EnableMihomo *bool `json:"enable_mihomo"`
	}
	if err := json.Unmarshal(body, &presence); err == nil {
		if presence.EnableXray == nil && presence.EnableMihomo == nil {
			sub.EnableXray = true
			sub.EnableMihomo = true
		}
	}

	if sub.URL == "" {
		a.errorResponse(w, "URL is required", http.StatusBadRequest)
		return
	}

	if sub.Interval == 0 {
		sub.Interval = 24 // default 24 hours
	}

	if err := a.subscriptionSvc.Add(&sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, sub)
}

func (a *API) SubscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	var sub services.Subscription
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.Update(id, &sub); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, a.subscriptionSvc.Get(id))
}

func (a *API) SubscriptionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.Delete(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	JSONSuccess(w, nil)
}

func (a *API) SubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.Refresh(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sub := a.subscriptionSvc.Get(id)
	a.jsonResponse(w, sub)
}

func (a *API) SubscriptionRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	subs := a.subscriptionSvc.List()
	type result struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status bool   `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(subs))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, sub := range subs {
		if !sub.Enabled {
			continue
		}
		wg.Add(1)
		go func(s services.Subscription) {
			defer wg.Done()
			err := a.subscriptionSvc.Refresh(s.ID)
			r := result{ID: s.ID, Name: s.Name, Status: err == nil}
			if err != nil {
				r.Error = err.Error()
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(sub)
	}
	wg.Wait()

	a.jsonResponse(w, results)
}

func (a *API) SubscriptionRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	body, headers, err := a.subscriptionSvc.GetRaw(id)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, map[string]interface{}{
		"body":    body,
		"headers": headers,
	})
}

func (a *API) SubscriptionParseReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	report, err := a.subscriptionSvc.GetParseReport(id)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, report)
}

func (a *API) SubscriptionNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	sub := a.subscriptionSvc.Get(id)
	if sub == nil {
		a.errorResponse(w, "subscription not found", http.StatusNotFound)
		return
	}

	nodes := sub.Nodes
	if nodes == nil {
		nodes = []services.SubscriptionNode{}
	}

	a.jsonResponse(w, nodes)
}

func (a *API) SubscriptionHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	if a.subscriptionSvc.Get(id) == nil {
		a.errorResponse(w, "subscription not found", http.StatusNotFound)
		return
	}

	if a.subscriptionHealthSvc == nil {
		a.jsonResponse(w, map[string]interface{}{})
		return
	}

	// ?force=true — немедленная перепроверка
	if r.URL.Query().Get("force") == "true" {
		nodeTag := r.URL.Query().Get("node_tag")
		if nodeTag != "" {
			a.subscriptionHealthSvc.ForceCheckNode(id, nodeTag)
		} else {
			a.subscriptionHealthSvc.ForceCheck(id)
		}
	}

	health := a.subscriptionHealthSvc.GetHealth(id)
	a.jsonResponse(w, health)
}

func (a *API) SubscriptionSetActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID is required", http.StatusBadRequest)
		return
	}

	var body struct {
		NodeTag string `json:"node_tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeTag == "" {
		a.errorResponse(w, "node_tag is required", http.StatusBadRequest)
		return
	}

	if err := a.subscriptionSvc.SetActiveNode(id, body.NodeTag); err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "cannot set active node in auto routing mode (balancer is managing selection)" {
			status = http.StatusConflict
		}
		a.errorResponse(w, err.Error(), status)
		return
	}

	JSONSuccess(w, map[string]string{"active_node": body.NodeTag})
}

// adhocSubscriptionID возвращает стабильный ID для подписки, не
// зарегистрированной в панели — используется как fallback для имени файла
// кэша провайдера, чтобы повторные запросы того же url попадали в один файл.
func adhocSubscriptionID(urlStr string) string {
	sum := sha256.Sum256([]byte(urlStr))
	return "adhoc-" + hex.EncodeToString(sum[:])[:16]
}

// MihomoProviderAdapter — loopback-only endpoint /api/provider.yaml. Проксирует
// запрос подписки к upstream-провайдеру (с HWID/device заголовками и
// ClashMeta User-Agent), конвертирует ответ в Mihomo proxy-provider payload
// (только секция proxies:) и отдаёт его Mihomo. При сетевой ошибке отдаёт
// последний закэшированный на диске payload.
func (a *API) MihomoProviderAdapter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Проверка RemoteAddr (isLoopback)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host != "127.0.0.1" && host != "::1" {
		a.errorResponse(w, "Access forbidden", http.StatusForbidden)
		return
	}

	// 2. Получение query-параметра url
	urlStr := r.URL.Query().Get("url")
	if urlStr == "" {
		a.errorResponse(w, "url parameter is required", http.StatusBadRequest)
		return
	}

	// 3. Поиск подписки по URL; если не найдена — создаём временный объект
	// (панель всё равно проксирует upstream, HWID/device заголовки берутся
	// из глобального состояния сервиса).
	var sub *services.Subscription
	subs := a.subscriptionSvc.List()
	for i := range subs {
		if subs[i].URL == urlStr {
			sub = &subs[i]
			break
		}
	}
	if sub != nil && !sub.Enabled {
		a.errorResponse(w, "subscription is disabled", http.StatusForbidden)
		return
	}
	if sub == nil {
		sub = &services.Subscription{URL: urlStr, ID: adhocSubscriptionID(urlStr)}
	}

	// 4. Upstream fetch + конвертация + Happ fallback + кэш на диск (graceful
	// fallback на кэш при сетевой ошибке).
	payload, err := a.subscriptionSvc.ProviderFetchWithFallback(r.Context(), urlStr, sub)
	if err != nil {
		log.Printf("[Subscriptions] provider fetch failed for sub=%s: %v", sub.ID, err)
		http.Error(w, "Failed to fetch provider", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// MihomoProviderRedirect редиректит устаревший путь /mihomo/provider.yaml на
// новый /api/provider.yaml, сохраняя все query-параметры.
func (a *API) MihomoProviderRedirect(w http.ResponseWriter, r *http.Request) {
	target := "/api/provider.yaml"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusFound)
}
