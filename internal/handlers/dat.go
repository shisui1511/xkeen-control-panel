package handlers

import (
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func (a *API) DATList(w http.ResponseWriter, r *http.Request) {
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}
	a.jsonResponse(w, a.datSvc.List())
}

func (a *API) DATUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	datType := r.URL.Query().Get("type")
	if datType == "" {
		a.errorResponse(w, "type parameter required (geoip, geosite, mmdb)", http.StatusBadRequest)
		return
	}

	size, err := a.datSvc.Update(datType)
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.jsonResponse(w, map[string]interface{}{
		"success": true,
		"size":    size,
	})
}

func (a *API) DATUpdateAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	if a.datSvc == nil {
		a.errorResponse(w, "DAT Manager service unavailable", http.StatusServiceUnavailable)
		return
	}

	results, _ := a.datSvc.UpdateAll()
	a.jsonResponse(w, map[string]interface{}{
		"results": results,
	})
}

func (a *API) DATInfo(w http.ResponseWriter, r *http.Request) {
	info, err := services.GetLatestReleaseInfo()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.jsonResponse(w, info)
}
