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

	authSvcTemp := auth.NewAuthService("", false, 5, 0, nil)
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
