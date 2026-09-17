package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/assets"
)

func TestConsoleHandlers(t *testing.T) {
	// 1. Nil console service -> 503
	apiNil := &API{}

	reqGetNil := httptest.NewRequest(http.MethodGet, "/api/console/commands", nil)
	rrGetNil := httptest.NewRecorder()
	apiNil.ConsoleListCommands(rrGetNil, reqGetNil)
	if rrGetNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil consoleSvc list, got %d", rrGetNil.Code)
	}

	reqPostNil := httptest.NewRequest(http.MethodPost, "/api/console/execute", bytes.NewBufferString(`{"command":"-status"}`))
	rrPostNil := httptest.NewRecorder()
	apiNil.ConsoleExecute(rrPostNil, reqPostNil)
	if rrPostNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil consoleSvc exec, got %d", rrPostNil.Code)
	}

	// 2. Initialized console service
	api := &API{
		consoleSvc: services.NewConsoleService("/bin/true"),
	}

	// ConsoleListCommands: 405 on POST, 200 on GET
	reqPostCmds := httptest.NewRequest(http.MethodPost, "/api/console/commands", nil)
	rrPostCmds := httptest.NewRecorder()
	api.ConsoleListCommands(rrPostCmds, reqPostCmds)
	if rrPostCmds.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST list, got %d", rrPostCmds.Code)
	}

	reqGetCmds := httptest.NewRequest(http.MethodGet, "/api/console/commands", nil)
	rrGetCmds := httptest.NewRecorder()
	api.ConsoleListCommands(rrGetCmds, reqGetCmds)
	if rrGetCmds.Code != http.StatusOK {
		t.Errorf("expected 200 for GET list, got %d", rrGetCmds.Code)
	}

	// ConsoleExecute: 405 on GET, 400 on bad JSON, 200 on valid command
	reqGetExec := httptest.NewRequest(http.MethodGet, "/api/console/execute", nil)
	rrGetExec := httptest.NewRecorder()
	api.ConsoleExecute(rrGetExec, reqGetExec)
	if rrGetExec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET exec, got %d", rrGetExec.Code)
	}

	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/console/execute", bytes.NewBufferString("{invalid"))
	rrBadJSON := httptest.NewRecorder()
	api.ConsoleExecute(rrBadJSON, reqBadJSON)
	if rrBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", rrBadJSON.Code)
	}

	reqExec := httptest.NewRequest(http.MethodPost, "/api/console/execute", bytes.NewBufferString(`{"command":"-status"}`))
	rrExec := httptest.NewRecorder()
	api.ConsoleExecute(rrExec, reqExec)
	if rrExec.Code != http.StatusOK {
		t.Errorf("expected 200 for valid exec, got %d", rrExec.Code)
	}
}

func TestAssetsHandlers(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Nil assets service -> 503
	apiNil := &API{}
	reqGetNil := httptest.NewRequest(http.MethodGet, "/api/assets/definition", nil)
	rrGetNil := httptest.NewRecorder()
	apiNil.AssetsDefinition(rrGetNil, reqGetNil)
	if rrGetNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil assetsSvc, got %d", rrGetNil.Code)
	}

	// 2. Initialized assets service
	api := &API{
		assetsSvc: assets.NewService(tmpDir),
	}

	// Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/assets/definition", nil)
	rrPost := httptest.NewRecorder()
	api.AssetsDefinition(rrPost, reqPost)
	if rrPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", rrPost.Code)
	}

	// GET returns definition (200 OK)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/assets/definition", nil)
	rrGet := httptest.NewRecorder()
	api.AssetsDefinition(rrGet, reqGet)
	if rrGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", rrGet.Code)
	}
}
