package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestVersionHandler(t *testing.T) {
	srv, err := server.New(&server.Config{Port: 8090}, "v0.15.0", fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}

	api := &API{
		xkeenSvc: services.NewXKeenService("/bin/true", t.TempDir()),
		srv:      srv,
	}

	// 1. Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	recPost := httptest.NewRecorder()
	api.Version(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	// 2. GET returns version info
	reqGet := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	recGet := httptest.NewRecorder()
	api.Version(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", recGet.Code)
	}
}

// TestVersionHandler_FromCache: версия берётся из кэша, `xkeen -v` на запросе
// не запускается; без версии в кэше отдаётся "unknown".
func TestVersionHandler_FromCache(t *testing.T) {
	srv, err := server.New(&server.Config{Port: 8090}, "v0.15.0", fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("ok")},
	})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	invoked := filepath.Join(dir, "invoked.log")
	stub := filepath.Join(dir, "xkeen-stub")
	script := "#!/bin/sh\necho invoked >> " + invoked + "\necho 'Версия XKeen 9.9'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, cached, want string
	}{
		{"version cached", "2.0 Beta", "2.0 Beta"},
		{"version empty", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := services.NewXKeenStatusCacheFunc(
				func(context.Context) (string, error) { return "XKeen is running", nil },
				func(context.Context) string { return tc.cached },
				nil, time.Hour,
			)
			cache.Start()
			defer cache.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cache.RefreshNow(ctx)

			api := &API{xkeenSvc: services.NewXKeenService(stub, t.TempDir()), srv: srv}
			api.SetXKeenStatusCache(cache)

			rec := httptest.NewRecorder()
			api.Version(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["version"] != tc.want || body["panel_version"] != "v0.15.0" {
				t.Errorf("body = %v, want version=%q panel_version=v0.15.0", body, tc.want)
			}
			if _, err := os.Stat(invoked); err == nil {
				t.Error("xkeen был запущен на пути запроса /api/version")
			}
		})
	}
}
