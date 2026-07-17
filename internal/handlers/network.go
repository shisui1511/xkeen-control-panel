package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var hostRegex = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9.]*[a-zA-Z0-9]$`)

// validateURL rejects URLs that could be used for SSRF attacks:
// - non-HTTP(S) schemes (file://, ftp://, etc.)
// - loopback addresses (127.x.x.x, ::1, localhost)
// - link-local addresses (169.254.x.x, fe80::)
// - private and reserved ranges (RFC-1918, CGNAT, IPv6 ULA)
func validateURL(rawURL string) error {
	return utils.ValidateURL(context.Background(), rawURL, false)
}

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

	// Basic host validation to prevent command injection
	if !hostRegex.MatchString(req.Host) {
		a.errorResponse(w, "Invalid host format", http.StatusBadRequest)
		return
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

	if !hostRegex.MatchString(req.Host) {
		a.errorResponse(w, "Invalid host format", http.StatusBadRequest)
		return
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

	if !hostRegex.MatchString(req.Host) {
		a.errorResponse(w, "Invalid host format", http.StatusBadRequest)
		return
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

	if err := validateURL(req.URL); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := a.networkSvc.HTTPTest(req.URL, req.Timeout)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	result, err := a.networkSvc.GetPublicIP()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkProxyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ProxyName string `json:"proxy_name"`
		URL       string `json:"url"`
		Timeout   int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.ProxyName == "" {
		a.errorResponse(w, "Proxy name required", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		a.errorResponse(w, "URL required", http.StatusBadRequest)
		return
	}

	if err := validateURL(req.URL); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Timeout <= 0 {
		req.Timeout = 5000
	}

	result, err := a.networkSvc.ProxyDelayTest(req.ProxyName, req.URL, req.Timeout)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}

func (a *API) NetworkPortCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Host    string `json:"host"`
		Port    int    `json:"port"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Host == "" {
		a.errorResponse(w, "Host required", http.StatusBadRequest)
		return
	}

	if !hostRegex.MatchString(req.Host) {
		a.errorResponse(w, "Invalid host format", http.StatusBadRequest)
		return
	}

	if req.Port < 1 || req.Port > 65535 {
		a.errorResponse(w, "Invalid port range (must be 1-65535)", http.StatusBadRequest)
		return
	}

	if req.Timeout <= 0 {
		req.Timeout = 5000
	}

	result, err := a.networkSvc.PortCheck(req.Host, req.Port, time.Duration(req.Timeout)*time.Millisecond)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, result)
}
