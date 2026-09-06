package handlers

import (
	"encoding/json"
	"net/http"

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

// UserRulesSave persists the list of custom user rules.
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

	if a.userRulesSvc != nil {
		if err := a.userRulesSvc.Save(req.Rules); err != nil {
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	JSONSuccess(w, nil)
}
