package services

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Template описывает один конфигурационный шаблон.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "xray" или "mihomo"
	Filename    string `json:"filename,omitempty"`
	Content     string `json:"content"`
}

// TemplateService предоставляет доступ к конфигурационным шаблонам
// исключительно из встроенной embedded FS.
type TemplateService struct {
	embeddedFS fs.FS
	dataDir    string
	templates  []Template
	mu         sync.RWMutex
}

// NewTemplateService создаёт автономный TemplateService с embedded FS и DataDir.
// При инициализации выполняет разовую очистку устаревшего кэша и предзагружает
// все встроенные шаблоны в память.
func NewTemplateService(templatesFS fs.FS, dataDir string) *TemplateService {
	svc := &TemplateService{
		embeddedFS: templatesFS,
		dataDir:    dataDir,
		templates:  []Template{},
	}
	svc.cleanupLegacyCache()
	svc.loadCatalog()
	return svc
}

// cleanupLegacyCache удаляет устаревший дисковый кэш templates.json в dataDir, если он существует (D-07).
func (s *TemplateService) cleanupLegacyCache() {
	if s.dataDir == "" {
		return
	}
	legacyPath := filepath.Join(s.dataDir, "templates.json")
	if _, err := os.Stat(legacyPath); err == nil {
		_ = os.Remove(legacyPath)
	}
}

// loadCatalog загружает каталог шаблонов из embedded catalog.json
// и предзагружает содержимое каждого шаблона в поле Content (D-01, D-03).
func (s *TemplateService) loadCatalog() {
	data, err := fs.ReadFile(s.embeddedFS, "catalog.json")
	if err != nil {
		return
	}
	var catalog struct {
		Version   string     `json:"version"`
		Templates []Template `json:"templates"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return
	}

	allowedTypes := map[string]bool{"xray": true, "mihomo": true}
	templates := make([]Template, 0, len(catalog.Templates))
	for _, tmpl := range catalog.Templates {
		if !allowedTypes[tmpl.Type] || tmpl.Filename == "" {
			continue
		}
		// Санитизация filename через filepath.Base блокирует path traversal (T-118-02)
		safeName := filepath.Base(tmpl.Filename)
		if safeName == "." || safeName == "/" || safeName == ".." {
			continue
		}
		content, err := fs.ReadFile(s.embeddedFS, tmpl.Type+"/"+safeName)
		if err != nil {
			continue
		}
		tmpl.Content = string(content)
		tmpl.Filename = safeName
		templates = append(templates, tmpl)
	}

	s.mu.Lock()
	s.templates = templates
	s.mu.Unlock()
}

// List возвращает список доступных шаблонов с предзагруженным контентом.
func (s *TemplateService) List() []Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.templates == nil {
		return []Template{}
	}
	return s.templates
}
