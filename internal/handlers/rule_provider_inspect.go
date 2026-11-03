package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// RuleProvidersInfo handles GET /api/rule-providers/info: rule-providers from
// the Mihomo config with the state of their local files.
func (a *API) RuleProvidersInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.routeTracerSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "route tracer service not available")
		return
	}
	list, err := a.routeTracerSvc.ListRuleProviders()
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, list)
}

// RuleProviderContent handles GET /api/rule-providers/content?name=&q=&offset=&limit=.
func (a *API) RuleProviderContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.routeTracerSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "route tracer service not available")
		return
	}
	q := r.URL.Query()
	name := q.Get("name")
	if name == "" || len(name) > 256 {
		JSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	search := q.Get("q")
	if len(search) > 256 {
		search = search[:256]
	}

	// First read of a large MRS set converts it (seconds on a router CPU).
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	page, err := a.routeTracerSvc.RuleProviderContent(ctx, name, search, offset, limit)
	switch {
	case errors.Is(err, services.ErrRuleProviderNotFound):
		JSONError(w, http.StatusNotFound, err.Error())
	case err != nil:
		// Typically the provider file is not downloaded yet.
		JSONError(w, http.StatusConflict, err.Error())
	default:
		JSONSuccess(w, page)
	}
}

// RuleProviderCheckURL handles POST /api/rule-providers/check-url?name=.
func (a *API) RuleProviderCheckURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.routeTracerSvc == nil {
		JSONError(w, http.StatusServiceUnavailable, "route tracer service not available")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" || len(name) > 256 {
		JSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	res, err := a.routeTracerSvc.CheckRuleProviderURL(r.Context(), name, a.providerProxyProbe())
	if errors.Is(err, services.ErrRuleProviderNotFound) {
		JSONError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, res)
}

// providerProxyProbe собирает пробу адреса через узел или группу работающего ядра
// (GET /proxies/{имя}/delay). nil — API ядра недоступно: нет сервиса, не описан
// контроллер или он не отвечает; проверка тогда идёт только напрямую. Проба
// идёт под delayGuard: пачка проверок не должна перегружать ядро на слабом CPU.
func (a *API) providerProxyProbe() services.ProxyProbe {
	if a.mihomoSvc == nil || !a.mihomoSvc.HasController() {
		return nil
	}
	// ProxyDelay ходит с секретом из профиля, тем же пользуется и проверка.
	var secret string
	if info, err := a.mihomoSvc.ParseControllerConfig(); err == nil {
		secret = info.Secret
	}
	if reachable, authenticated := a.mihomoSvc.ProbeAPI(secret); !reachable || !authenticated {
		return nil
	}
	return func(ctx context.Context, proxy, rawURL string) (int, error) {
		if a.delayGuard != nil {
			release, err := a.delayGuard.Acquire(ctx)
			if err != nil {
				return 0, err
			}
			defer release()
		}
		pctx, cancel := context.WithTimeout(ctx, services.RuleProviderProbeTimeout+3*time.Second)
		defer cancel()
		return a.mihomoSvc.ProxyDelay(pctx, proxy, rawURL, services.RuleProviderProbeTimeout)
	}
}
