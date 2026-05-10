package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/user/xkeen-control-panel/internal/services"
)

func (a *API) NetworkPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Host  string `json:"host"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Host == "" {
		a.errorResponse(w, "Host required", http.StatusBadRequest)
		return
	}

	if a.networkSvc == nil {
		a.networkSvc = services.NewNetworkToolsService()
	}

	result, err := a.networkSvc.Ping(req.Host, req.Count)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkTraceroute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Host    string `json:"host"`
		MaxHops int    `json:"max_hops"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Host == "" {
		a.errorResponse(w, "Host required", http.StatusBadRequest)
		return
	}

	if a.networkSvc == nil {
		a.networkSvc = services.NewNetworkToolsService()
	}

	result, err := a.networkSvc.Traceroute(req.Host, req.MaxHops)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkDNS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Host       string `json:"host"`
		RecordType string `json:"record_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Host == "" {
		a.errorResponse(w, "Host required", http.StatusBadRequest)
		return
	}

	if a.networkSvc == nil {
		a.networkSvc = services.NewNetworkToolsService()
	}

	result, err := a.networkSvc.DNSLookup(req.Host, req.RecordType)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkHTTPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL     string `json:"url"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		a.errorResponse(w, "URL required", http.StatusBadRequest)
		return
	}

	if a.networkSvc == nil {
		a.networkSvc = services.NewNetworkToolsService()
	}

	result, err := a.networkSvc.HTTPTest(req.URL, req.Timeout)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkIP(w http.ResponseWriter, r *http.Request) {
	if a.networkSvc == nil {
		a.networkSvc = services.NewNetworkToolsService()
	}

	result, err := a.networkSvc.GetPublicIP()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}
