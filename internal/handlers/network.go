package handlers

import (
	"net/http"
)

// NetworkIP returns the current public WAN IP of the router.
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
