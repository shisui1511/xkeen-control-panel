package handlers

import (
	"encoding/json"
	"net/http"
)

// MihomoMigrateSocket handles previewing and applying migration of Mihomo configuration to Unix Domain Socket.
func (a *API) MihomoMigrateSocket(w http.ResponseWriter, r *http.Request) {
	if a.mihomoSvc == nil {
		a.errorResponse(w, a.t(r, "mihomo.not_available"), http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		preview, err := a.mihomoSvc.GetMigrationPreview()
		if err != nil {
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}
		JSONSuccess(w, preview)

	case http.MethodPost:
		var req struct {
			Action string `json:"action"` // "preview" | "apply"
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&req)
		}

		if req.Action == "preview" || r.URL.Query().Get("preview") == "true" {
			preview, err := a.mihomoSvc.GetMigrationPreview()
			if err != nil {
				a.errorResponse(w, err.Error(), http.StatusInternalServerError)
				return
			}
			JSONSuccess(w, preview)
			return
		}

		// Apply migration
		result, err := a.mihomoSvc.MigrateToSocket(a.xkeenSvc)
		if err != nil {
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}

		a.ClearCapabilitiesCache()

		if !result.Success {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   result.Error,
				"data":    result,
			})
			return
		}

		JSONSuccess(w, result)

	default:
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
	}
}
