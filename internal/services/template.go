package services

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// templatesRepoBase — базовый URL онлайн-репозитория шаблонов (D-02).
const templatesRepoBase = "https://raw.githubusercontent.com/shisui1511/xkeen-templates/main"

// Template описывает один конфигурационный шаблон.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`     // "xray" или "mihomo"
	Filename    string `json:"filename,omitempty"`
	Content     string `json:"content,omitempty"`
}

// cachedTemplates — формат DataDir-кэша templates.json (D-03).
type cachedTemplates struct {
	FetchedAt time.Time  `json:"fetched_at"`
	Templates []Template `json:"templates"`
}

// TemplateService предоставляет доступ к конфигурационным шаблонам
// из embedded FS (офлайн) или DataDir-кэша (после онлайн-обновления).
type TemplateService struct {
	embeddedFS fs.FS
	dataDir    string
	templates  []Template
	mu         sync.RWMutex
	httpClient *http.Client
}

// NewTemplateService создаёт TemplateService с embedded FS и DataDir для кэша.
// При запуске читает каталог: сначала из DataDir-кэша, затем из embedded FS.
func NewTemplateService(templatesFS fs.FS, dataDir string) *TemplateService {
	svc := &TemplateService{
		embeddedFS: templatesFS,
		dataDir:    dataDir,
		httpClient: utils.SafeHTTPClient(10 * time.Second),
	}
	svc.loadCatalog()
	return svc
}

// storePath возвращает путь к DataDir-кэшу каталога шаблонов.
func (s *TemplateService) storePath() string {
	return filepath.Join(s.dataDir, "templates.json")
}

// loadCatalog загружает каталог шаблонов. Приоритет: DataDir-кэш → embedded catalog.json.
func (s *TemplateService) loadCatalog() {
	// Попробовать DataDir-кэш сначала (D-03)
	if cached, err := os.ReadFile(s.storePath()); err == nil {
		var c cachedTemplates
		if json.Unmarshal(cached, &c) == nil && len(c.Templates) > 0 {
			s.templates = c.Templates
			return
		}
	}
	// Fallback: embedded catalog.json
	data, err := fs.ReadFile(s.embeddedFS, "catalog.json")
	if err != nil {
		return
	}
	var catalog struct {
		Templates []Template `json:"templates"`
	}
	if json.Unmarshal(data, &catalog) == nil {
		s.templates = catalog.Templates
	}
}

// List возвращает список доступных шаблонов.
func (s *TemplateService) List() []Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates
}

// FetchByName возвращает содержимое шаблона по имени.
// Читает файл из embedded FS по типу/filename, sanitizing filename через filepath.Base (T-15-05).
func (s *TemplateService) FetchByName(name string) (string, error) {
	s.mu.RLock()
	var filename, templateType string
	for _, t := range s.templates {
		if t.Name == name {
			filename = t.Filename
			templateType = t.Type
			break
		}
	}
	s.mu.RUnlock()

	if filename == "" {
		return "", fmt.Errorf("template not found: %s", name)
	}

	// Санитизация filename через filepath.Base — блокирует path traversal (T-15-05).
	safeName := filepath.Base(filename)

	// Читать из embedded FS по пути type/filename.
	content, err := fs.ReadFile(s.embeddedFS, templateType+"/"+safeName)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %w", err)
	}
	return string(content), nil
}

// FetchOnlineUpdates скачивает актуальный каталог из онлайн-репозитория (D-04),
// заполняет содержимое шаблонов и сохраняет кэш в DataDir (D-03).
// При сетевой ошибке embedded-шаблоны не затираются.
// Перенос SSRF-защиты из старого FetchByName (T-15-03, T-15-04).
func (s *TemplateService) FetchOnlineUpdates() (int, error) {
	// Скачать catalog.json из онлайн-репозитория
	catalogURL := templatesRepoBase + "/catalog.json"

	catalogData, err := s.fetchURL(catalogURL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch online catalog: %w", err)
	}

	var catalog struct {
		Templates []Template `json:"templates"`
	}
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		return 0, fmt.Errorf("failed to parse online catalog: %w", err)
	}

	// Скачать содержимое каждого шаблона
	updated := make([]Template, 0, len(catalog.Templates))
	for _, tmpl := range catalog.Templates {
		if tmpl.Filename == "" || tmpl.Type == "" {
			continue
		}
		// Санитизация filename (T-15-05)
		safeName := filepath.Base(tmpl.Filename)
		fileURL := templatesRepoBase + "/" + tmpl.Type + "/" + safeName

		content, err := s.fetchURL(fileURL)
		if err != nil {
			// При ошибке одного шаблона продолжаем остальные
			continue
		}
		tmpl.Content = string(content)
		tmpl.Filename = safeName
		updated = append(updated, tmpl)
	}

	if len(updated) == 0 {
		return 0, fmt.Errorf("no templates fetched from online repository")
	}

	// Сохранить кэш в DataDir (D-03)
	cache := cachedTemplates{
		FetchedAt: time.Now(),
		Templates: updated,
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal template cache: %w", err)
	}
	if err := utils.AtomicWriteFile(s.storePath(), data, 0600); err != nil {
		return 0, fmt.Errorf("failed to save template cache: %w", err)
	}

	// Обновить s.templates под Lock
	s.mu.Lock()
	s.templates = updated
	s.mu.Unlock()

	return len(updated), nil
}

// fetchURL выполняет GET-запрос с SSRF-защитой (T-15-03) и ограничением размера (T-15-04).
func (s *TemplateService) fetchURL(rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil, fmt.Errorf("access to localhost is prohibited")
	}

	// Дополнительная проверка IP для CodeQL SSRF-анализа
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve host: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return nil, fmt.Errorf("access to private network is prohibited")
		}
	}

	resp, err := s.httpClient.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	// Лимит размера 1MB (T-15-04)
	return io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
}
