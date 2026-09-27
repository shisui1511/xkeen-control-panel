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

// AuthSessions handles GET /api/auth/sessions (protected) — returns the list
// of the account's active sessions (opaque id, browser, os, ip, times),
// current session first. Never exposes tokens or their hashes (T-134-09).
func (a *API) AuthSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var currentToken string
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		currentToken = cookie.Value
	}

	JSONSuccess(w, a.srv.GetAuthService().ListSessions(currentToken))
}

// AuthSessionTerminate handles POST /api/auth/sessions/terminate (protected).
// Body: {"id": "..."}. Terminating the current session is rejected — the
// client must use Logout for that.
func (a *API) AuthSessionTerminate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		a.errorResponse(w, a.t(r, "error.invalid_request"), http.StatusBadRequest)
		return
	}

	var currentToken string
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		currentToken = cookie.Value
	}

	authSvc := a.srv.GetAuthService()
	if err := authSvc.TerminateSession(req.ID, currentToken); err != nil {
		if errors.Is(err, auth.ErrSessionIsCurrent) {
			JSONErrorCode(w, http.StatusBadRequest, "session_is_current", a.t(r, "auth.session_is_current"))
			return
		}
		if errors.Is(err, auth.ErrSessionNotFound) {
			JSONErrorCode(w, http.StatusNotFound, "session_not_found", a.t(r, "auth.session_not_found"))
			return
		}
		a.errorResponse(w, a.t(r, "error.internal"), http.StatusInternalServerError)
		return
	}

	JSONSuccess(w, map[string]int{"terminated": 1})
}

// AuthSessionsTerminateOthers handles POST /api/auth/sessions/terminate-others
// (protected) — terminates every session except the current one and returns
// how many were terminated (0 is a valid, idempotent result).
func (a *API) AuthSessionsTerminateOthers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	var currentToken string
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		currentToken = cookie.Value
	}

	n := a.srv.GetAuthService().TerminateOtherSessions(currentToken)
	JSONSuccess(w, map[string]int{"terminated": n})
}
