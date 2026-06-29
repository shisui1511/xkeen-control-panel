package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// ChangePassword handles POST /api/auth/change-password (protected).
// Body: {"current_password": "...", "new_password": "..."}
func (a *API) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.errorResponse(w, a.t(r, "error.invalid_request"), http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < 8 {
		a.errorResponse(w, a.t(r, "auth.password_too_short"), http.StatusBadRequest)
		return
	}

	authSvc := a.srv.GetAuthService()
	if err := authSvc.ChangePassword(req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			a.errorResponse(w, a.t(r, "auth.wrong_password"), http.StatusUnauthorized)
			return
		}
		a.errorResponse(w, a.t(r, "error.internal"), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
