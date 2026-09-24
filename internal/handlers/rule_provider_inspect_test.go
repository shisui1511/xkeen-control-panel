package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestRuleProviderInspectHandlers(t *testing.T) {
	home := t.TempDir()
	cfg := "rule-providers:\n  local: { type: inline, behavior: domain, payload: [\"+.a.test\", \"+.b.test\"] }\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	api := &API{}
	api.SetRouteTracerService(services.NewRouteTracerService(nil, nil, home))

	rec := httptest.NewRecorder()
	api.RuleProvidersInfo(rec, httptest.NewRequest(http.MethodGet, "/api/rule-providers/info", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"local"`) {
		t.Fatalf("info: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.RuleProviderContent(rec, httptest.NewRequest(http.MethodGet, "/api/rule-providers/content?name=local&q=b.", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"entries":["+.b.test"]`) {
		t.Fatalf("content: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.RuleProviderContent(rec, httptest.NewRequest(http.MethodGet, "/api/rule-providers/content?name=nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown provider: expected 404, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.RuleProviderContent(rec, httptest.NewRequest(http.MethodGet, "/api/rule-providers/content", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name: expected 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.RuleProviderCheckURL(rec, httptest.NewRequest(http.MethodGet, "/api/rule-providers/check-url?name=local", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET check-url: expected 405, got %d", rec.Code)
	}
}
