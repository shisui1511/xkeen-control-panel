package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

func TestLogsSetLevel_MihomoUsesSecret(t *testing.T) {
	var gotLevel, gotAuth string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/configs" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		if gotAuth != "Bearer test-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var p map[string]string
		_ = json.Unmarshal(body, &p)
		gotLevel = p["log-level"]
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	api := &API{cfg: &config.Config{MihomoAPIURL: backend.URL, MihomoSecret: "test-secret"}}

	req := httptest.NewRequest(http.MethodPost, "/api/logs/level", strings.NewReader(`{"source":"mihomo","level":"debug"}`))
	rec := httptest.NewRecorder()
	api.LogsSetLevel(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotLevel != "debug" {
		t.Errorf("expected log-level debug, got %q (auth %q)", gotLevel, gotAuth)
	}
}

func TestLogsSetLevel_MihomoRejectsUnknownLevel(t *testing.T) {
	api := &API{cfg: &config.Config{MihomoAPIURL: "http://127.0.0.1:1"}}
	req := httptest.NewRequest(http.MethodPost, "/api/logs/level", strings.NewReader(`{"source":"mihomo","level":"verbose"}`))
	rec := httptest.NewRecorder()
	api.LogsSetLevel(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
