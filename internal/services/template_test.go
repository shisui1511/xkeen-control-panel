package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/assets"
)

// testCatalogJSON содержит минимальный catalog.json для in-memory тестов.
const testCatalogJSON = `{"version":"1.0.0","templates":[{"name":"Test","description":"d","type":"xray","filename":"test.json"}]}`

// testMapFS возвращает fstest.MapFS с catalog.json и xray/test.json.
func testMapFS() fstest.MapFS {
	return fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(testCatalogJSON),
		},
		"xray/test.json": &fstest.MapFile{
			Data: []byte(`{"test": true}`),
		},
	}
}

func TestTemplateService_List(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)
	svc := NewTemplateService(fsys, tempDir, "", assetsSvc)

	list := svc.List()
	if len(list) == 0 {
		t.Fatal("expected at least one template")
	}
	if list[0].Name == "" || list[0].Type == "" {
		t.Errorf("invalid template — Name or Type empty: %+v", list[0])
	}
}

func TestTemplateService_FetchByName(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)
	svc := NewTemplateService(fsys, tempDir, "", assetsSvc)

	// Несуществующее имя должно возвращать ошибку
	_, err := svc.FetchByName("Non-existent Template Name")
	if err == nil {
		t.Error("expected error for non-existent template, got nil")
	}

	// Существующий шаблон должен возвращать содержимое
	content, err := svc.FetchByName("Test")
	if err != nil {
		t.Fatalf("failed to fetch existing template: %v", err)
	}
	if content == "" {
		t.Error("expected non-empty template content")
	}
}

func TestTemplateService_NoURLTemplates(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)
	svc := NewTemplateService(fsys, tempDir, "", assetsSvc)

	list := svc.List()
	for _, tmpl := range list {
		// Template struct не содержит поля URL (D-07, TMPL-02) —
		// embedded шаблоны никогда не хранят сетевые адреса.
		// Проверяем что поле Content не содержит хардкоженных URL шаблонов.
		if strings.HasPrefix(tmpl.Content, "http") {
			t.Errorf("template %q has unexpected http content prefix — embedded templates must not have network URLs (TMPL-02)", tmpl.Name)
		}
	}
}

func TestTemplateService_PathTraversal(t *testing.T) {
	// catalog.json с filename, содержащим path traversal
	maliciousFS := fstest.MapFS{
		"catalog.json": &fstest.MapFile{
			Data: []byte(`{"templates":[{"name":"Evil","description":"d","type":"xray","filename":"../secret"}]}`),
		},
		// Файл вне templates/ — не должен быть доступен
		"secret": &fstest.MapFile{
			Data: []byte("SECRET_CONTENT"),
		},
	}
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)
	svc := NewTemplateService(maliciousFS, tempDir, "", assetsSvc)

	content, err := svc.FetchByName("Evil")
	// filepath.Base("../secret") == "secret"; xray/secret не существует в FS → обязана быть ошибка.
	if err == nil {
		t.Errorf("expected error for path traversal filename, got content: %q", content)
	}
}

func TestTemplateService_FetchByNameFallback(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)
	svc := NewTemplateService(fsys, tempDir, "", assetsSvc)

	// Level 3: embedded FS (Test template content)
	content, err := svc.FetchByName("Test")
	if err != nil {
		t.Fatalf("expected to fetch embedded template: %v", err)
	}
	if content != `{"test": true}` {
		t.Errorf("expected embedded content `{\"test\": true}`, got: %q", content)
	}

	// Level 2: disk cache templates.json
	cachedData := cachedTemplates{
		FetchedAt: time.Now(),
		Version:   "2.0.0",
		Templates: []Template{
			{
				Name:        "Test",
				Description: "Disk Cache",
				Type:        "xray",
				Filename:    "test.json",
				Content:     `{"test": "disk"}`,
			},
		},
	}
	data, err := json.Marshal(cachedData)
	if err != nil {
		t.Fatalf("failed to marshal cachedTemplates: %v", err)
	}
	err = os.WriteFile(filepath.Join(tempDir, "templates.json"), data, 0600)
	if err != nil {
		t.Fatalf("failed to write disk cache templates.json: %v", err)
	}

	// Clear memory s.templates to force loading from disk/memory fallback
	svc.mu.Lock()
	svc.templates = []Template{
		{
			Name:        "Test",
			Description: "d",
			Type:        "xray",
			Filename:    "test.json",
			Content:     "", // Empty content forces search to next level (disk cache)
		},
	}
	svc.mu.Unlock()

	content, err = svc.FetchByName("Test")
	if err != nil {
		t.Fatalf("expected to fetch from disk cache: %v", err)
	}
	if content != `{"test": "disk"}` {
		t.Errorf("expected disk content `{\"test\": \"disk\"}`, got: %q", content)
	}

	// Level 1: Memory cache
	svc.mu.Lock()
	svc.templates = []Template{
		{
			Name:        "Test",
			Description: "d",
			Type:        "xray",
			Filename:    "test.json",
			Content:     `{"test": "memory"}`,
		},
	}
	svc.mu.Unlock()

	content, err = svc.FetchByName("Test")
	if err != nil {
		t.Fatalf("expected to fetch from memory: %v", err)
	}
	if content != `{"test": "memory"}` {
		t.Errorf("expected memory content `{\"test\": \"memory\"}`, got: %q", content)
	}
}

func TestTemplateService_BackgroundChecker(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)
	svc := NewTemplateService(fsys, tempDir, "", assetsSvc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		svc.StartBackgroundChecker(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Success, exited cleanly
	case <-time.After(1 * time.Second):
		t.Fatal("StartBackgroundChecker did not exit cleanly when context was cancelled")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTemplateService_FetchOnlineUpdates_NetworkFallback(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)

	// 1. Start an HTTP test server that returns a 500 error / network failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	svc := NewTemplateService(fsys, tempDir, "http://example.com", assetsSvc)
	svc.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			targetURL, _ := url.Parse(server.URL)
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	// FetchOnlineUpdates should fail with a network error
	_, err := svc.FetchOnlineUpdates()
	if err == nil {
		t.Fatal("expected error from FetchOnlineUpdates when network fails, got nil")
	}

	// Verify that the local template still works via FetchByName
	content, err := svc.FetchByName("Test")
	if err != nil {
		t.Fatalf("FetchByName should still work using fallback, got error: %v", err)
	}
	if content != `{"test": true}` {
		t.Errorf("expected embedded content `{\"test\": true}`, got: %q", content)
	}
}

func TestTemplateService_UpdateIncompatibility(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	assetsSvc := assets.NewService(tempDir)

	// 1. Start an HTTP test server that serves:
	// - catalog.json (valid)
	// - assets-definition.json (incompatible schema version e.g. 2.0.0)
	// - xray/test.json (valid)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "catalog.json") {
			w.Write([]byte(`{"version":"2.0.0","templates":[{"name":"Test","description":"d","type":"xray","filename":"test.json"}]}`))
		} else if strings.HasSuffix(r.URL.Path, "assets-definition.json") {
			w.Write([]byte(`{"schema_version":"2.0.0"}`))
		} else if strings.HasSuffix(r.URL.Path, "test.json") {
			w.Write([]byte(`{"test": "online"}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc := NewTemplateService(fsys, tempDir, "http://example.com", assetsSvc)
	svc.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			targetURL, _ := url.Parse(server.URL)
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	// CheckForUpdates should set Incompatible: true since remote schema is 2.0.0 (local default is 1.0.0)
	_, err := svc.CheckForUpdates()
	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}

	status := svc.GetStatus()
	if !status.Incompatible {
		t.Error("expected status.Incompatible to be true")
	}
	if !strings.Contains(status.WarningMessage, "incompatible schema version") {
		t.Errorf("expected warning message to contain 'incompatible schema version', got %q", status.WarningMessage)
	}

	// FetchOnlineUpdates should fail with incompatibility error
	_, err = svc.FetchOnlineUpdates()
	if err == nil {
		t.Fatal("expected FetchOnlineUpdates to fail, got nil")
	}
	if !strings.Contains(err.Error(), "incompatible schema version") {
		t.Errorf("expected error to mention 'incompatible schema version', got: %v", err)
	}

	// Double-check GetStatus still returns Incompatible
	status = svc.GetStatus()
	if !status.Incompatible {
		t.Error("expected status.Incompatible to remain true after failed FetchOnlineUpdates")
	}
}
