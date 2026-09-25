package handlers

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
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

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	var keepToken string
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		keepToken = cookie.Value
	}

	authSvc := a.srv.GetAuthService()
	if err := authSvc.ChangePassword(ip, keepToken, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			// 403, а не 401: сессия действительна, неверен только введённый
			// пароль — клиент не должен разлогинивать пользователя
			a.errorResponse(w, a.t(r, "auth.wrong_password"), http.StatusForbidden)
			return
		}
		if errors.Is(err, auth.ErrTooManyAttempts) {
			a.errorResponse(w, a.t(r, "auth.rate_limited"), http.StatusTooManyRequests)
			return
		}
		a.errorResponse(w, a.t(r, "error.internal"), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
