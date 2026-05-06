package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func (a *API) MihomoStatus(w http.ResponseWriter, r *http.Request) {
	out, err := a.mihomoSvc.Status()
	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}

func (a *API) MihomoControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := r.URL.Query().Get("action")

	var out string
	var err error

	switch action {
	case "start":
		out, err = a.mihomoSvc.Start()
	case "stop":
		out, err = a.mihomoSvc.Stop()
	case "restart":
		out, err = a.mihomoSvc.Restart()
	default:
		a.errorResponse(w, "Invalid action", http.StatusBadRequest)
		return
	}

	if err != nil {
		a.errorResponse(w, out, http.StatusInternalServerError)
		return
	}
	w.Write([]byte(out))
}

func (a *API) MihomoProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(a.cfg.MihomoAPIURL)
	if err != nil {
		a.errorResponse(w, "Invalid Mihomo API URL", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Mihomo API unavailable: "+err.Error(), http.StatusBadGateway)
	}

	// Strip /api/mihomo/proxy prefix and forward the rest
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/mihomo/proxy")
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}

	proxy.ServeHTTP(w, r)
}
