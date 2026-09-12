package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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
	svc := NewTemplateService(fsys, tempDir)

	list := svc.List()
	if len(list) == 0 {
		t.Fatal("expected at least one template")
	}
	if list[0].Name == "" || list[0].Type == "" {
		t.Errorf("invalid template — Name or Type empty: %+v", list[0])
	}
	if list[0].Content != `{"test": true}` {
		t.Errorf("expected Content `{\"test\": true}`, got: %q", list[0].Content)
	}
}

func TestTemplateService_NoURLTemplates(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	svc := NewTemplateService(fsys, tempDir)

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
	svc := NewTemplateService(maliciousFS, tempDir)

	list := svc.List()
	for _, tmpl := range list {
		if tmpl.Name == "Evil" {
			if tmpl.Content == "SECRET_CONTENT" {
				t.Fatalf("path traversal vulnerability detected: secret file was read into content")
			}
			if tmpl.Content != "" {
				t.Errorf("expected empty content for traversal path, got: %q", tmpl.Content)
			}
		}
	}
}

func TestTemplateService_LegacyCacheCleanup(t *testing.T) {
	fsys := testMapFS()
	tempDir := t.TempDir()
	legacyFile := filepath.Join(tempDir, "templates.json")

	if err := os.WriteFile(legacyFile, []byte(`{"legacy": true}`), 0600); err != nil {
		t.Fatalf("failed to create dummy legacy cache file: %v", err)
	}

	if _, err := os.Stat(legacyFile); err != nil {
		t.Fatalf("expected legacy file to exist before service init: %v", err)
	}

	_ = NewTemplateService(fsys, tempDir)

	if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
		t.Errorf("expected legacy cache file to be deleted by cleanupLegacyCache(), got err: %v", err)
	}
}
