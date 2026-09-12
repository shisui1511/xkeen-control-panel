package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
