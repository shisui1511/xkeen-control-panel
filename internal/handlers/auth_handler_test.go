package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
)

func newAuthHandlerTestAPI(t *testing.T, initialPassword string) (*API, *auth.AuthService) {
	t.Helper()

	authSvcTemp := auth.NewAuthService(auth.Options{MaxLoginAttempts: 5})
	hash, err := authSvcTemp.HashPassword(initialPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	mapFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("SPA")},
	}
	srvCfg := &server.Config{
		Port:         0,
		PasswordHash: hash,
	}

	srv, err := server.New(srvCfg, "v1.0.0", mapFS)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	cfg := &config.Config{
		AllowedRoots: []string{t.TempDir()},
	}

	api := &API{
		cfg: cfg,
		srv: srv,
	}

	return api, srv.GetAuthService()
}

func TestChangePassword_Handler(t *testing.T) {
	api, authSvc := newAuthHandlerTestAPI(t, "initialpass123")
	defer authSvc.Stop()

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/auth/change-password", nil)
	recGet := httptest.NewRecorder()
	api.ChangePassword(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", bytes.NewReader([]byte("{invalid-json")))
	recBadJSON := httptest.NewRecorder()
	api.ChangePassword(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. New password too short (< 8 chars)
	bodyShort, _ := json.Marshal(map[string]string{
		"current_password": "initialpass123",
		"new_password":     "short",
	})
	reqShort := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", bytes.NewReader(bodyShort))
	recShort := httptest.NewRecorder()
	api.ChangePassword(recShort, reqShort)
	if recShort.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", recShort.Code)
	}

	// 4. Wrong current password -> 403: сессия действительна, клиент не разлогинивается
	bodyWrong, _ := json.Marshal(map[string]string{
		"current_password": "wrongpassword",
		"new_password":     "brandnewpass123",
	})
	reqWrong := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", bytes.NewReader(bodyWrong))
	recWrong := httptest.NewRecorder()
	api.ChangePassword(recWrong, reqWrong)
	if recWrong.Code != http.StatusForbidden {
		t.Errorf("expected 403 for wrong current password, got %d", recWrong.Code)
	}

	// 5. Success -> 200 OK
	bodySuccess, _ := json.Marshal(map[string]string{
		"current_password": "initialpass123",
		"new_password":     "brandnewpass123",
	})
	reqSuccess := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", bytes.NewReader(bodySuccess))
	recSuccess := httptest.NewRecorder()
	api.ChangePassword(recSuccess, reqSuccess)
	if recSuccess.Code != http.StatusOK {
		t.Errorf("expected 200 for successful password change, got %d", recSuccess.Code)
	}

	// Verify new password works and old fails
	if err := authSvc.VerifyPassword("brandnewpass123"); err != nil {
		t.Errorf("new password did not verify: %v", err)
	}
	if err := authSvc.VerifyPassword("initialpass123"); err == nil {
		t.Error("old password still works after change")
	}
}

// TestChangePassword_Handler_SetsNewCookieAndCSRF: успешная смена пароля
// перевыпускает текущую сессию (SESS-02, D-09) — ответ содержит csrf_token,
// соответствующий новому токену из Set-Cookie, а старый токен того же
// браузера после этого больше не принимается.
func TestChangePassword_Handler_SetsNewCookieAndCSRF(t *testing.T) {
	api, authSvc := newAuthHandlerTestAPI(t, "initialpass123")
	defer authSvc.Stop()

	current, err := authSvc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]string{
		"current_password": "initialpass123",
		"new_password":     "brandnewpass456",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: current.Token})
	rec := httptest.NewRecorder()
	api.ChangePassword(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Status    string `json:"status"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.CSRFToken == "" {
		t.Fatal("expected a non-empty csrf_token in the response")
	}

	var newCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			newCookie = c
		}
	}
	if newCookie == nil {
		t.Fatal("expected a new session cookie in the response")
	}
	if newCookie.Value == current.Token {
		t.Error("expected a newly issued token, not the old one")
	}
	if resp.CSRFToken != auth.DeriveCSRFToken(newCookie.Value) {
		t.Error("csrf_token in the response must match the new session's derived CSRF")
	}

	if _, err := authSvc.ValidateSession(current.Token); err == nil {
		t.Error("the old token of the current session must be rejected after password change")
	}
	if _, err := authSvc.ValidateSession(newCookie.Value); err != nil {
		t.Errorf("the newly issued session must be valid: %v", err)
	}
}

// TestAuthSessionsHandlers exercises AuthSessions / AuthSessionTerminate /
// AuthSessionsTerminateOthers end to end: list (with method guard), reject
// terminating the current session, terminate an other session, repeated
// terminate of the same id is idempotent (404), and terminate-others with
// only the current session left returns 0.
func TestAuthSessionsHandlers(t *testing.T) {
	api, authSvc := newAuthHandlerTestAPI(t, "initialpass123")
	defer authSvc.Stop()

	login := func(t *testing.T) *http.Cookie {
		t.Helper()
		issued, err := authSvc.CreateSession()
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.SessionCookieName, Value: issued.Token}
	}

	currentCookie := login(t)
	login(t) // second session — identified below via ListSessions, not by cookie

	// GET-only endpoint rejects POST.
	reqBadMethod := httptest.NewRequest(http.MethodPost, "/api/auth/sessions", nil)
	recBadMethod := httptest.NewRecorder()
	api.AuthSessions(recBadMethod, reqBadMethod)
	if recBadMethod.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", recBadMethod.Code)
	}

	reqList := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	reqList.AddCookie(currentCookie)
	recList := httptest.NewRecorder()
	api.AuthSessions(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recList.Code, recList.Body.String())
	}
	var listResp struct {
		Success bool               `json:"success"`
		Data    []auth.SessionInfo `json:"data"`
	}
	if err := json.NewDecoder(recList.Body).Decode(&listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Data) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(listResp.Data))
	}

	var currentID, otherID string
	for _, s := range listResp.Data {
		if s.Current {
			currentID = s.ID
		} else {
			otherID = s.ID
		}
	}
	if currentID == "" || otherID == "" {
		t.Fatalf("expected one current and one other session, got %+v", listResp.Data)
	}

	// Empty id -> 400.
	reqEmptyID := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/terminate", bytes.NewReader([]byte(`{}`)))
	reqEmptyID.AddCookie(currentCookie)
	recEmptyID := httptest.NewRecorder()
	api.AuthSessionTerminate(recEmptyID, reqEmptyID)
	if recEmptyID.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty id, got %d", recEmptyID.Code)
	}

	// Terminating the current session -> 400 session_is_current.
	bodyCurrent, _ := json.Marshal(map[string]string{"id": currentID})
	reqCurrent := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/terminate", bytes.NewReader(bodyCurrent))
	reqCurrent.AddCookie(currentCookie)
	recCurrent := httptest.NewRecorder()
	api.AuthSessionTerminate(recCurrent, reqCurrent)
	if recCurrent.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for current session, got %d", recCurrent.Code)
	}
	var currentErrResp struct {
		Code string `json:"code"`
	}
	json.NewDecoder(recCurrent.Body).Decode(&currentErrResp)
	if currentErrResp.Code != "session_is_current" {
		t.Errorf("expected code session_is_current, got %q", currentErrResp.Code)
	}

	// Terminating the other session -> 200.
	bodyOther, _ := json.Marshal(map[string]string{"id": otherID})
	reqOther := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/terminate", bytes.NewReader(bodyOther))
	reqOther.AddCookie(currentCookie)
	recOther := httptest.NewRecorder()
	api.AuthSessionTerminate(recOther, reqOther)
	if recOther.Code != http.StatusOK {
		t.Errorf("expected 200 terminating other session, got %d: %s", recOther.Code, recOther.Body.String())
	}

	// Repeated terminate of the same id -> 404 (idempotent).
	reqRepeat := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/terminate", bytes.NewReader(bodyOther))
	reqRepeat.AddCookie(currentCookie)
	recRepeat := httptest.NewRecorder()
	api.AuthSessionTerminate(recRepeat, reqRepeat)
	if recRepeat.Code != http.StatusNotFound {
		t.Errorf("expected 404 for repeated terminate, got %d", recRepeat.Code)
	}
	var repeatResp struct {
		Code string `json:"code"`
	}
	json.NewDecoder(recRepeat.Body).Decode(&repeatResp)
	if repeatResp.Code != "session_not_found" {
		t.Errorf("expected code session_not_found, got %q", repeatResp.Code)
	}

	// terminate-others with only the current session left -> 0.
	reqOthers := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/terminate-others", nil)
	reqOthers.AddCookie(currentCookie)
	recOthers := httptest.NewRecorder()
	api.AuthSessionsTerminateOthers(recOthers, reqOthers)
	if recOthers.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recOthers.Code)
	}
	var othersResp struct {
		Data struct {
			Terminated int `json:"terminated"`
		} `json:"data"`
	}
	json.NewDecoder(recOthers.Body).Decode(&othersResp)
	if othersResp.Data.Terminated != 0 {
		t.Errorf("expected 0 terminated (only current left), got %d", othersResp.Data.Terminated)
	}
}
