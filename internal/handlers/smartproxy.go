package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) smartProxy() *services.SmartProxyService {
	return a.smartProxySvc
}

func (a *API) SmartProxyList(w http.ResponseWriter, r *http.Request) {
	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.smartProxySvc.List())
}

func (a *API) SmartProxyGet(w http.ResponseWriter, r *http.Request) {
	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	p := a.smartProxySvc.Get(id)
	if p == nil {
		a.errorResponse(w, "Profile not found", http.StatusNotFound)
		return
	}

	a.jsonResponse(w, p)
}

func (a *API) SmartProxyAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}

	var p services.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if p.Name == "" || p.GroupName == "" || p.ProxyName == "" {
		a.errorResponse(w, "Name, group_name and proxy_name are required", http.StatusBadRequest)
		return
	}

	if err := a.smartProxySvc.Add(&p); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, p)
}

func (a *API) SmartProxyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	var p services.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.smartProxySvc.Update(id, &p); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	a.jsonResponse(w, p)
}

func (a *API) SmartProxyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	if err := a.smartProxySvc.Delete(id); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) SmartProxySetEnabled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		a.errorResponse(w, "ID required", http.StatusBadRequest)
		return
	}

	enabled := r.URL.Query().Get("enabled") == "true"

	if err := a.smartProxySvc.SetEnabled(id, enabled); err != nil {
		a.errorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) SmartProxyStatus(w http.ResponseWriter, r *http.Request) {
	if a.smartProxySvc == nil {
		a.errorResponse(w, "Smart Proxy service unavailable", http.StatusServiceUnavailable)
		return
	}

	a.jsonResponse(w, a.smartProxySvc.CurrentStatus())
}
