package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// UserRulesList returns all custom user rules.
func (a *API) UserRulesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	if a.userRulesSvc == nil {
		JSONSuccess(w, []services.UserRule{})
		return
	}

	rules := a.userRulesSvc.List()
	JSONSuccess(w, rules)
}

// UserRulesSave persists the list of custom user rules and atomically injects them into the active kernel.
func (a *API) UserRulesSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Rules []services.UserRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if a.userRulesSvc == nil {
		a.errorResponse(w, "User rules service not available", http.StatusInternalServerError)
		return
	}

	if err := a.userRulesSvc.Save(req.Rules); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	applied := false
	reloaded := false
	activeKernel := a.getActiveKernelName()

	// Mihomo injection & reload
	if activeKernel == "mihomo" || activeKernel == "both" || (activeKernel == "none" && a.cfg != nil && a.cfg.MihomoConfigDir != "") {
		if a.cfg != nil && a.cfg.MihomoConfigDir != "" {
			configPath := filepath.Join(a.cfg.MihomoConfigDir, "config.yaml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				configPath = filepath.Join(a.cfg.MihomoConfigDir, "config.yml")
			}
			if _, err := os.Stat(configPath); err == nil {
				if err := a.userRulesSvc.InjectMihomoRules(configPath, "PROXY"); err == nil {
					applied = true
					if a.mihomoSvc != nil {
						if err := a.mihomoSvc.ReloadConfig(configPath); err == nil {
							reloaded = true
						}
					}
				}
			}
		}
	}

	// Xray injection
	if activeKernel == "xray" || activeKernel == "both" {
		if a.cfg != nil && a.cfg.XRayConfigDir != "" {
			routingPath := filepath.Join(a.cfg.XRayConfigDir, "05_routing.json")
			if _, err := os.Stat(routingPath); err == nil {
				if err := a.userRulesSvc.InjectXrayRules(routingPath, "proxy"); err == nil {
					applied = true
				}
			}
		}
	}

	JSONSuccess(w, map[string]interface{}{
		"applied":  applied,
		"reloaded": reloaded,
		"count":    len(req.Rules),
	})
}
