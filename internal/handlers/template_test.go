package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestTemplateList_Handler(t *testing.T) {
	// 1. Method Not Allowed (POST)
	api := &API{}
	reqPost := httptest.NewRequest(http.MethodPost, "/api/templates/list", nil)
	recPost := httptest.NewRecorder()
	api.TemplateList(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	// 2. Service Unavailable when templateSvc is nil
	reqGet := httptest.NewRequest(http.MethodGet, "/api/templates/list", nil)
	recGetNil := httptest.NewRecorder()
	api.TemplateList(recGetNil, reqGet)
	if recGetNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when templateSvc is nil, got %d", recGetNil.Code)
	}

	// 3. Success when templateSvc is initialized
	testFS := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{"version":"1.0.0","templates":[{"name":"Default Config","description":"Basic template","type":"xray","filename":"default.json"}]}`),
		},
		"xray/default.json": &fstest.MapFile{
			Data: []byte(`{"mode":"rule"}`),
		},
	}
	api.templateSvc = services.NewTemplateService(testFS, t.TempDir())

	recGetSuccess := httptest.NewRecorder()
	api.TemplateList(recGetSuccess, reqGet)
	if recGetSuccess.Code != http.StatusOK {
		t.Errorf("expected 200 for TemplateList, got %d", recGetSuccess.Code)
	}

	var list []services.Template
	if err := json.NewDecoder(recGetSuccess.Body).Decode(&list); err != nil {
		t.Fatalf("failed to decode templates list: %v", err)
	}
	if len(list) != 1 || list[0].Filename != "default.json" {
		t.Errorf("unexpected templates list: %+v", list)
	}
}
