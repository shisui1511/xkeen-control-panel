package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestUserRulesHandlers(t *testing.T) {
	// 1. UserRulesList when userRulesSvc is nil -> returns empty array
	api := &API{}
	reqGet := httptest.NewRequest(http.MethodGet, "/api/user-rules", nil)
	recGetNil := httptest.NewRecorder()
	api.UserRulesList(recGetNil, reqGet)
	if recGetNil.Code != http.StatusOK {
		t.Errorf("expected 200 when userRulesSvc is nil, got %d", recGetNil.Code)
	}

	// 2. UserRulesList Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/user-rules", nil)
	recPost := httptest.NewRecorder()
	api.UserRulesList(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST to UserRulesList, got %d", recPost.Code)
	}

	// 3. UserRulesSave Method Not Allowed (GET)
	recSaveGet := httptest.NewRecorder()
	api.UserRulesSave(recSaveGet, reqGet)
	if recSaveGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET to UserRulesSave, got %d", recSaveGet.Code)
	}

	// 4. UserRulesSave with bad JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/user-rules/save", bytes.NewReader([]byte("{invalid")))
	recBadJSON := httptest.NewRecorder()
	api.UserRulesSave(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 5. UserRulesSave and List with valid rules
	tmpDir := t.TempDir()
	api.userRulesSvc = services.NewUserRulesService(tmpDir)

	rulesPayload := map[string]interface{}{
		"rules": []services.UserRule{
			{
				ID:      "rule-1",
				Type:    "domain",
				Value:   "example.com",
				Target:  "direct",
				Enabled: true,
			},
			{
				ID:      "rule-2",
				Type:    "ip_cidr",
				Value:   "1.1.1.1/32",
				Target:  "proxy",
				Enabled: true,
			},
		},
	}
	body, _ := json.Marshal(rulesPayload)
	reqSaveGood := httptest.NewRequest(http.MethodPost, "/api/user-rules/save", bytes.NewReader(body))
	recSaveGood := httptest.NewRecorder()
	api.UserRulesSave(recSaveGood, reqSaveGood)
	if recSaveGood.Code != http.StatusOK {
		t.Errorf("expected 200 for good UserRulesSave, got %d", recSaveGood.Code)
	}

	var saveResp struct {
		Success bool `json:"success"`
		Data    struct {
			Applied  bool `json:"applied"`
			Reloaded bool `json:"reloaded"`
			Count    int  `json:"count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(recSaveGood.Body).Decode(&saveResp); err != nil {
		t.Fatalf("failed to decode save response: %v", err)
	}
	if !saveResp.Success || saveResp.Data.Count != 2 {
		t.Errorf("unexpected save response: %+v", saveResp)
	}

	// Verify UserRulesList returns the saved rules
	recGetList := httptest.NewRecorder()
	api.UserRulesList(recGetList, reqGet)
	if recGetList.Code != http.StatusOK {
		t.Errorf("expected 200 for UserRulesList, got %d", recGetList.Code)
	}

	var resp struct {
		Success bool                `json:"success"`
		Data    []services.UserRule `json:"data"`
	}
	if err := json.NewDecoder(recGetList.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode list: %v", err)
	}
	if !resp.Success || len(resp.Data) != 2 {
		t.Errorf("unexpected list response: %+v", resp)
	}
}

func TestUserRulesSave_RuntimeInjectionAndReload(t *testing.T) {
	reloaded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.RequestURI() == "/configs?force=true" {
			reloaded = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	hostPort := strings.TrimPrefix(server.URL, "http://")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfgContent := "external-controller: " + hostPort + "\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(configPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	api := &API{
		cfg: &config.Config{
			MihomoConfigDir: tmpDir,
		},
		userRulesSvc: services.NewUserRulesService(tmpDir),
		mihomoSvc:    services.NewMihomoService("", "", tmpDir),
	}

	rulesPayload := map[string]interface{}{
		"rules": []services.UserRule{
			{
				ID:      "rule-1",
				Type:    "domain",
				Value:   "injected.org",
				Target:  "proxy",
				Group:   "SpecialProxy",
				Enabled: true,
			},
		},
	}
	body, _ := json.Marshal(rulesPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/rules/custom", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	api.UserRulesSave(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var saveResp struct {
		Success bool `json:"success"`
		Data    struct {
			Applied  bool `json:"applied"`
			Reloaded bool `json:"reloaded"`
			Count    int  `json:"count"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&saveResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !saveResp.Data.Applied {
		t.Errorf("expected applied: true")
	}
	if !saveResp.Data.Reloaded {
		t.Errorf("expected reloaded: true")
	}
	if !reloaded {
		t.Errorf("expected server to receive PUT /configs?force=true")
	}

	// Verify config.yaml contains markers and injected rule
	contentBytes, _ := os.ReadFile(configPath)
	content := string(contentBytes)
	if !strings.Contains(content, services.UserRulesBeginMarker) || !strings.Contains(content, "DOMAIN,injected.org,SpecialProxy") {
		t.Errorf("config.yaml was not properly injected:\n%s", content)
	}
}
