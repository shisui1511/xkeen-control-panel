package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) MihomoStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	out, err := a.mihomoSvc.Status()
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}

const mihomoProxyPrefix = "/api/mihomo/proxy"

// kernelRestarting сообщает, что идёт перезапуск ядра, начатый панелью: окно
// после XKeenService.Restart и SwitchKernel (InRestart). XKeen выполняет
// -restart в фоне, и старый процесс ядра гаснет уже после ответа панели, когда
// гейт опросов фронтенда ещё открыт (capabilities видели старый процесс): пока
// новый процесс не открыл API, запросы к нему не проходят.
func (a *API) kernelRestarting() bool {
	return a.xkeenSvc != nil && a.xkeenSvc.InRestart()
}

// mihomoProxyContext проверяет сокет до открытия потока и отменяет его при смене профиля.
func (a *API) mihomoProxyContext(w http.ResponseWriter, r *http.Request) (services.ControllerInfo, context.Context, context.CancelFunc) {
	if a.mihomoSvc == nil {
		a.errorResponse(w, a.t(r, "mihomo.not_running"), http.StatusServiceUnavailable)
		return services.ControllerInfo{}, nil, nil
	}
	ctx, cancel := a.mihomoSvc.ControllerContext(r.Context())
	info, err := a.mihomoSvc.UnixController()
	if err == nil {
		return info, ctx, cancel
	}
	cancel()
	switch {
	case errors.Is(err, services.ErrMihomoNoSocket):
		JSONErrorCode(w, http.StatusConflict, "no_socket", a.t(r, "mihomo.not_running"))
	case a.kernelRestarting():
		JSONErrorCode(w, http.StatusConflict, "kernel_restarting", a.t(r, "kernel.restarting"))
	default:
		a.errorResponse(w, a.t(r, "mihomo.not_running"), http.StatusBadGateway)
	}
	return services.ControllerInfo{}, nil, nil
}

func (a *API) MihomoProxy(w http.ResponseWriter, r *http.Request) {
	// Whitelist allowed HTTP methods
	switch r.Method {
	case http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch:
		// allowed
	default:
		a.errorResponse(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	info, ctx, cancel := a.mihomoProxyContext(w, r)
	if cancel == nil {
		return
	}
	defer cancel()
	r = r.WithContext(ctx)

	if a.delayGuard != nil && (strings.Contains(r.URL.Path, "/delay") || strings.Contains(r.URL.Path, "/healthcheck")) {
		release, err := a.delayGuard.Acquire(r.Context())
		if err != nil {
			if errors.Is(err, ErrQueueFull) || errors.Is(err, ErrWaitTimeout) {
				w.Header().Set("Retry-After", "2")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "Too many requests to Mihomo core",
					"retry_after": 2,
					"code":        "busy",
				})
				return
			}
			if errors.Is(err, context.Canceled) {
				return
			}
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer release()
	}

	target := &url.URL{Scheme: "http", Host: "localhost"}
	secret := info.Secret
	if secret == "" {
		secret = a.cfg.MihomoSecret
	}

	proxy := &httputil.ReverseProxy{
		Transport: a.mihomoSvc.GetHTTPTransport(),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Ядро перезапускает сама панель: API недоступен штатно, это не сбой
			// шлюза. Ответ сразу, без ожидания API: опросы страниц повторятся по
			// своему такту, а гейт опросов закроется по capabilities.
			if a.kernelRestarting() {
				JSONErrorCode(w, http.StatusConflict, "kernel_restarting", a.t(r, "kernel.restarting"))
				return
			}
			a.errorResponse(w, a.t(r, "mihomo.not_running")+": "+err.Error(), http.StatusBadGateway)
		},
		Rewrite: func(pr *httputil.ProxyRequest) {
			// X-Forwarded-* is intentionally not propagated: Rewrite mode
			// strips inbound X-Forwarded-* and we deliberately do not call
			// pr.SetXForwarded() — the target is the local loopback Clash
			// API on the Unix socket, which ignores client-forwarding headers.
			pr.SetURL(target)
			pr.Out.Host = target.Host
			if secret != "" {
				pr.Out.Header.Set("Authorization", "Bearer "+secret)
			}
		},
	}

	// Strip /api/mihomo/proxy prefix and forward the rest. RawPath обрезается
	// вместе с Path: иначе он перестаёт соответствовать Path, EscapedPath()
	// его отбрасывает, и %2F в имени прокси («HK/SG Auto») становится
	// разделителем пути.
	r.URL.Path = strings.TrimPrefix(r.URL.Path, mihomoProxyPrefix)
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}
	if r.URL.RawPath != "" {
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, mihomoProxyPrefix)
		if r.URL.RawPath == "" {
			r.URL.RawPath = "/"
		}
	}

	proxy.ServeHTTP(w, r)
}

// MihomoDNSQuery handles interactive DNS resolution via /api/mihomo/dns/query.
func (a *API) MihomoDNSQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if strings.TrimSpace(name) == "" {
		a.errorResponse(w, "name query parameter is required", http.StatusBadRequest)
		return
	}
	qtype := r.URL.Query().Get("type")
	if qtype == "" {
		qtype = "A"
	}

	if a.mihomoSvc == nil {
		a.errorResponse(w, "mihomo service not available", http.StatusServiceUnavailable)
		return
	}

	res, err := a.mihomoSvc.DNSQuery(r.Context(), name, qtype)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(res)
}

// MihomoFlushFakeIP handles Fake-IP cache flush via /api/mihomo/cache/fakeip/flush.
func (a *API) MihomoFlushFakeIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.mihomoSvc == nil {
		a.errorResponse(w, "mihomo service not available", http.StatusServiceUnavailable)
		return
	}

	if err := a.mihomoSvc.FlushFakeIPCache(r.Context()); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadGateway)
		return
	}

	JSONSuccess(w, nil)
}
