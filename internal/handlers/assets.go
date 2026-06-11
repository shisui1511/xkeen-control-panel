package handlers

import (
	"net/http"
)

func (a *API) AssetsDefinition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.assetsSvc == nil {
		a.errorResponse(w, "Assets service unavailable", http.StatusServiceUnavailable)
		return
	}
	data, err := a.assetsSvc.GetDefinition()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
