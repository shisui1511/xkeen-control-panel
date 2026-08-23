package handlers

import (
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// XrayRealityKeygen handles GET /api/xray/reality/keygen to generate Reality keypairs.
func (a *API) XrayRealityKeygen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	kp, err := services.GenerateRealityKeypair()
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, kp)
}
