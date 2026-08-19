package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type MihomoGroupsResponse struct {
	Groups []string `json:"groups"`
}

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

func (a *API) MihomoGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	reqPath := r.URL.Query().Get("path")
	var targetPath string

	if reqPath != "" {
		cleanPath, err := a.pathVal.Validate(reqPath)
		if err != nil {
			a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
			return
		}
		targetPath = cleanPath
	} else {
		configDir := a.cfg.MihomoConfigDir
		if configDir == "" {
			configDir = "/opt/etc/mihomo"
		}
		targetPath = filepath.Join(configDir, "config.yaml")
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			altPath := filepath.Join(configDir, "config.yml")
			if _, err := os.Stat(altPath); err == nil {
				targetPath = altPath
			}
		}
	}

	cleanTarget, err := a.pathVal.Validate(targetPath)
	if err != nil {
		a.errorResponse(w, a.t(r, "config.path_not_allowed"), http.StatusForbidden)
		return
	}

	data, err := os.ReadFile(cleanTarget)
	if err != nil {
		if os.IsNotExist(err) {
			a.jsonResponse(w, MihomoGroupsResponse{Groups: []string{}})
			return
		}
		log.Printf("[MihomoGroups] failed to read config %s: %v", utils.SanitizeLogInput(cleanTarget), err)
		a.jsonResponse(w, MihomoGroupsResponse{Groups: []string{}})
		return
	}

	groups, err := services.ExtractProxyGroupNames(string(data))
	if err != nil {
		log.Printf("[MihomoGroups] failed to extract groups from %s: %v", utils.SanitizeLogInput(cleanTarget), err)
		a.jsonResponse(w, MihomoGroupsResponse{Groups: []string{}})
		return
	}
	if groups == nil {
		groups = []string{}
	}

	a.jsonResponse(w, MihomoGroupsResponse{Groups: groups})
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

	var target *url.URL
	var secret string
	var transport *http.Transport

	if a.mihomoSvc != nil {
		info, err := a.mihomoSvc.ParseControllerConfig()
		if err == nil && info.Type == "unix" && info.Target != "" {
			target, _ = url.Parse("http://localhost")
			transport = a.mihomoSvc.GetHTTPTransport()
			secret = info.Secret
		} else if err == nil && info.Type == "tcp" && info.Target != "" {
			tStr := info.Target
			if !strings.HasPrefix(tStr, "http://") && !strings.HasPrefix(tStr, "https://") {
				tStr = "http://" + tStr
			}
			target, _ = url.Parse(tStr)
			transport = a.mihomoSvc.GetHTTPTransport()
			secret = info.Secret
		}
	}

	if target == nil {
		var err error
		target, err = url.Parse(a.cfg.MihomoAPIURL)
		if err != nil {
			a.errorResponse(w, a.t(r, "mihomo.api_error"), http.StatusInternalServerError)
			return
		}
		transport = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: 30 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
		}
	}

	if secret == "" {
		secret = a.cfg.MihomoSecret
		if secret == "" && a.mihomoSvc != nil {
			if _, parsedSecret, err := a.mihomoSvc.ParseConfig(); err == nil && parsedSecret != "" {
				secret = parsedSecret
			}
		}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		a.errorResponse(w, a.t(r, "mihomo.not_running")+": "+err.Error(), http.StatusBadGateway)
	}

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
