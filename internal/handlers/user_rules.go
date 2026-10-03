package handlers

import (
	"encoding/json"
	"log"
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
		a.errorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	applied := false
	reloaded := false
	var warningMsg string
	st := a.activeKernelState()
	if st.Conflict {
		// Запущены оба ядра: правила пишутся в оба конфига, но Mihomo панель не
		// перезагружает (при конфликте она сама ядрами не управляет).
		warningMsg = a.t(r, "kernel.conflict")
	}

	// Mihomo injection & reload
	if st.Conflict || st.Kernel == "mihomo" || (st.Kernel == "none" && a.cfg != nil && a.cfg.MihomoConfigDir != "") {
		if a.cfg != nil && a.cfg.MihomoConfigDir != "" {
			configPath := filepath.Join(a.cfg.MihomoConfigDir, "config.yaml")
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				configPath = filepath.Join(a.cfg.MihomoConfigDir, "config.yml")
			}
			if _, err := os.Stat(configPath); err == nil {
				if err := a.userRulesSvc.InjectMihomoRules(configPath, "PROXY"); err != nil {
					log.Printf("[UserRules] Failed to inject Mihomo rules into %s: %v", configPath, err)
					warningMsg = "Failed to inject rules into Mihomo configuration"
				} else {
					applied = true
					if !st.Conflict && a.mihomoSvc != nil {
						if err := a.mihomoSvc.ReloadConfig(configPath); err != nil {
							log.Printf("[UserRules] Failed to reload Mihomo config %s: %v", configPath, err)
							warningMsg = "Failed to reload Mihomo configuration"
						} else {
							reloaded = true
						}
					}
				}
			}
		}
	}

	// Xray injection
	if st.Conflict || st.Kernel == "xray" {
		if a.cfg != nil && a.cfg.XRayConfigDir != "" {
			routingPath := filepath.Join(a.cfg.XRayConfigDir, "05_routing.json")
			if _, err := os.Stat(routingPath); err == nil {
				if err := a.userRulesSvc.InjectXrayRules(routingPath, "proxy"); err != nil {
					log.Printf("[UserRules] Failed to inject Xray rules into %s: %v", routingPath, err)
					if warningMsg == "" {
						warningMsg = "Failed to inject rules into Xray configuration"
					}
				} else {
					applied = true
				}
			}
		}
	}

	res := map[string]interface{}{
		"applied":  applied,
		"reloaded": reloaded,
		"count":    len(req.Rules),
	}
	if warningMsg != "" {
		res["warning"] = warningMsg
	}

	JSONSuccess(w, res)
}
