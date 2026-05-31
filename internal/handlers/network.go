package handlers

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	hostRegex = regexp.MustCompile(`^[a-zA-Z0-9][-a-zA-Z0-9.]*[a-zA-Z0-9]$`)

	privateRanges = []string{
		"10.0.0.0/8",      // RFC 1918
		"172.16.0.0/12",   // RFC 1918
		"192.168.0.0/16",  // RFC 1918
		"100.64.0.0/10",   // Carrier-Grade NAT (RFC 6598)
		"fc00::/7",        // IPv6 Unique Local Address (RFC 4193)
	}
	privateNets []*net.IPNet
)

func init() {
	for _, cidr := range privateRanges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			privateNets = append(privateNets, ipNet)
		}
	}
}

// validateURL rejects URLs that could be used for SSRF attacks:
// - non-HTTP(S) schemes (file://, ftp://, etc.)
// - loopback addresses (127.x.x.x, ::1, localhost)
// - link-local addresses (169.254.x.x, fe80::)
// - private and reserved ranges (RFC-1918, CGNAT, IPv6 ULA)
func validateURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	hostname := u.Hostname()
	ips, err := net.LookupHost(hostname)
	if err != nil {
		// Allow DNS failure — the real request will fail; don't block on lookup error.
		return nil
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("SSRF: target resolves to restricted address")
		}
		// Private and local ranges (IPv4 private, CGNAT, IPv6 ULA)
		for _, network := range privateNets {
			if network.Contains(ip) {
				return fmt.Errorf("SSRF: target resolves to private or local address")
			}
		}
	}
	return nil
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
