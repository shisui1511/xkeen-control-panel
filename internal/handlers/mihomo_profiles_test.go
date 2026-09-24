package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestMihomoProfileHandlers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "mihomo")
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "default.yaml"), []byte("mode: rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}

	api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
	svc := services.NewMihomoProfileService(dir, filepath.Join(root, "xcp"))
	api.SetMihomoProfileService(svc)
	// No core in tests: skip validation and restarts.
	svc.Validate = func(string) error { return nil }
	svc.CoreActive = func() bool { return false }

	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if path == "/api/mihomo/profiles" {
			api.MihomoProfiles(rec, req)
		} else {
			api.MihomoProfileAction(rec, req)
		}
		return rec
	}

	if rec := call(http.MethodGet, "/api/mihomo/profiles", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":"default"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/create", `{"name":"work"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"work"`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/create", `{"name":"../etc"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad name: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/delete", `{"name":"default"}`); rec.Code != http.StatusConflict {
		t.Fatalf("delete active: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/activate", `{"name":"work"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":"work"`) {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/activate", `{"name":"nope"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("activate missing: %d", rec.Code)
	}
	if rec := call(http.MethodGet, "/api/mihomo/profiles/create", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET action: %d", rec.Code)
	}
}
