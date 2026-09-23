package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func newXKeenSettingsAPI(t *testing.T) (*API, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "xkeen")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	api := &API{}
	api.SetXKeenSettingsService(services.NewXKeenSettingsService(dir, filepath.Join(root, "xcp"), []string{dir}))
	return api, dir
}

func TestXKeenSettingsHandlers(t *testing.T) {
	api, dir := newXKeenSettingsAPI(t)

	rec := httptest.NewRecorder()
	api.XKeenSettingsList(rec, httptest.NewRequest(http.MethodGet, "/api/xkeen/settings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kind":"xkeen_json"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.XKeenSettingsValidate(rec, httptest.NewRequest(http.MethodPost, "/api/xkeen/settings/validate",
		strings.NewReader(`{"kind":"port_proxying","content":"80\n70000\n"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"invalid_port"`) {
		t.Fatalf("validate: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.XKeenSettingsSave(rec, httptest.NewRequest(http.MethodPost, "/api/xkeen/settings/save",
		strings.NewReader(`{"kind":"ip_exclude","content":"not-an-ip\n"}`)))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("save invalid: expected 422, got %d", rec.Code)
	}
	var resp struct {
		Data services.XKeenSettingsFile `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Data.Issues) != 1 || resp.Data.Issues[0].Line != 1 {
		t.Fatalf("save invalid: expected issues in data, got %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.XKeenSettingsSave(rec, httptest.NewRequest(http.MethodPost, "/api/xkeen/settings/save",
		strings.NewReader(`{"kind":"ip_exclude","content":"1.1.1.1\n"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "ip_exclude.lst")); string(data) != "1.1.1.1\n" {
		t.Fatalf("unexpected file content %q", data)
	}

	rec = httptest.NewRecorder()
	api.XKeenSettingsSave(rec, httptest.NewRequest(http.MethodPost, "/api/xkeen/settings/save",
		strings.NewReader(`{"kind":"../../passwd","content":""}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown kind: expected 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.XKeenSettingsSave(rec, httptest.NewRequest(http.MethodGet, "/api/xkeen/settings/save", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET save: expected 405, got %d", rec.Code)
	}
}
