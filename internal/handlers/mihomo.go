package handlers

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

func (a *API) MihomoStatus(w http.ResponseWriter, r *http.Request) {
	out, err := a.mihomoSvc.Status()
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}

func (a *API) MihomoProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(a.cfg.MihomoAPIURL)
	if err != nil {
		a.errorResponse(w, a.t(r, "mihomo.api_error"), http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// T033: 30s timeouts for dial and response header
	proxy.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, a.t(r, "mihomo.not_running")+": "+err.Error(), http.StatusBadGateway)
	}

	// T033: forward MihomoSecret as Authorization header if configured
	secret := a.cfg.MihomoSecret
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.Host = target.Host
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}

	// Strip /api/mihomo/proxy prefix and forward the rest
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/mihomo/proxy")
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}

	proxy.ServeHTTP(w, r)
}
