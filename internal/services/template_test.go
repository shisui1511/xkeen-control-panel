package services

import (
	"strings"
	"testing"
	"testing/fstest"
)

// testCatalogJSON содержит минимальный catalog.json для in-memory тестов.
const testCatalogJSON = `{"templates":[{"name":"Test","description":"d","type":"xray","filename":"test.json"}]}`

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
	svc := NewTemplateService(fsys, t.TempDir())

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
	svc := NewTemplateService(fsys, t.TempDir())

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
	svc := NewTemplateService(fsys, t.TempDir())

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
	svc := NewTemplateService(maliciousFS, t.TempDir())

	content, err := svc.FetchByName("Evil")
	// Ожидаем либо ошибку, либо пустой контент — но не "SECRET_CONTENT"
	if err == nil && content == "SECRET_CONTENT" {
		t.Error("path traversal vulnerability: FetchByName returned content outside templates directory")
	}
}
