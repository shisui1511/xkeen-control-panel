package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// newSessionSettingsTestAPI строит *API с временным config.json (D-06) —
// SessionSettings должен сохранять POST в реальный файл, а не только менять
// значение в памяти.
func newSessionSettingsTestAPI(t *testing.T) *API {
	t.Helper()

	api, authSvc := newAuthHandlerTestAPI(t, "initialpass123")
	t.Cleanup(authSvc.Stop)

	cfg := config.Default()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	api.cfg = cfg

	return api
}

// TestSessionSettings_GetPost проверяет D-06: GET отдаёт текущие значения и
// границы диапазона; POST в границах диапазона сохраняет config.json и сразу
// применяет TTL к AuthService; POST вне диапазона (0, 721, 366) — 400
// session_ttl_out_of_range без изменения конфига; PUT — 405.
func TestSessionSettings_GetPost(t *testing.T) {
	api := newSessionSettingsTestAPI(t)

	// GET: дефолты 24/30, границы 1..720 / 1..365.
	reqGet := httptest.NewRequest(http.MethodGet, "/api/settings/session", nil)
	recGet := httptest.NewRecorder()
	api.SessionSettings(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recGet.Code, recGet.Body.String())
	}
	var getResp struct {
		Data sessionSettingsResponse `json:"data"`
	}
	if err := json.NewDecoder(recGet.Body).Decode(&getResp); err != nil {
		t.Fatal(err)
	}
	if getResp.Data.IdleTTLHours != 24 || getResp.Data.AbsoluteTTLDays != 30 {
		t.Errorf("expected defaults 24/30, got %d/%d", getResp.Data.IdleTTLHours, getResp.Data.AbsoluteTTLDays)
	}
	if getResp.Data.IdleTTLMin != 1 || getResp.Data.IdleTTLMax != 720 {
		t.Errorf("expected idle range 1..720, got %d..%d", getResp.Data.IdleTTLMin, getResp.Data.IdleTTLMax)
	}
	if getResp.Data.AbsoluteTTLMin != 1 || getResp.Data.AbsoluteTTLMax != 365 {
		t.Errorf("expected absolute range 1..365, got %d..%d", getResp.Data.AbsoluteTTLMin, getResp.Data.AbsoluteTTLMax)
	}

	// PUT is not allowed.
	reqPut := httptest.NewRequest(http.MethodPut, "/api/settings/session", nil)
	recPut := httptest.NewRecorder()
	api.SessionSettings(recPut, reqPut)
	if recPut.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for PUT, got %d", recPut.Code)
	}

	// POST within range persists to config.json and applies to AuthService.TTL().
	body, _ := json.Marshal(map[string]int{"idle_ttl_hours": 12, "absolute_ttl_days": 7})
	reqPost := httptest.NewRequest(http.MethodPost, "/api/settings/session", bytes.NewReader(body))
	recPost := httptest.NewRecorder()
	api.SessionSettings(recPost, reqPost)
	if recPost.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recPost.Code, recPost.Body.String())
	}

	data, err := os.ReadFile(api.cfg.ConfigPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var savedCfg config.Config
	if err := json.Unmarshal(data, &savedCfg); err != nil {
		t.Fatal(err)
	}
	if savedCfg.Auth.SessionIdleTTLHours != 12 || savedCfg.Auth.SessionAbsoluteTTLDays != 7 {
		t.Errorf("expected config.json to contain 12/7, got %d/%d", savedCfg.Auth.SessionIdleTTLHours, savedCfg.Auth.SessionAbsoluteTTLDays)
	}

	idle, absolute := api.srv.GetAuthService().TTL()
	if idle != 12*time.Hour {
		t.Errorf("expected AuthService idle TTL 12h, got %v", idle)
	}
	if absolute != 7*24*time.Hour {
		t.Errorf("expected AuthService absolute TTL 7d, got %v", absolute)
	}

	// Out-of-range values are rejected and must not change the persisted config.
	for _, tc := range []struct {
		name string
		idle int
		abs  int
	}{
		{"idle=0", 0, 7},
		{"idle=721", 721, 7},
		{"absolute=366", 12, 366},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodyBad, _ := json.Marshal(map[string]int{"idle_ttl_hours": tc.idle, "absolute_ttl_days": tc.abs})
			reqBad := httptest.NewRequest(http.MethodPost, "/api/settings/session", bytes.NewReader(bodyBad))
			recBad := httptest.NewRecorder()
			api.SessionSettings(recBad, reqBad)
			if recBad.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", recBad.Code, recBad.Body.String())
			}
			var errResp struct {
				Code string `json:"code"`
			}
			if err := json.NewDecoder(recBad.Body).Decode(&errResp); err != nil {
				t.Fatal(err)
			}
			if errResp.Code != "session_ttl_out_of_range" {
				t.Errorf("expected code session_ttl_out_of_range, got %q", errResp.Code)
			}

			dataAfter, err := os.ReadFile(api.cfg.ConfigPath)
			if err != nil {
				t.Fatalf("read config.json: %v", err)
			}
			var cfgAfter config.Config
			if err := json.Unmarshal(dataAfter, &cfgAfter); err != nil {
				t.Fatal(err)
			}
			if cfgAfter.Auth.SessionIdleTTLHours != 12 || cfgAfter.Auth.SessionAbsoluteTTLDays != 7 {
				t.Errorf("expected config.json to remain 12/7 after rejected update, got %d/%d", cfgAfter.Auth.SessionIdleTTLHours, cfgAfter.Auth.SessionAbsoluteTTLDays)
			}
		})
	}
}
