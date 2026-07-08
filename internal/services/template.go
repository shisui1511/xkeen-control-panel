package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/assets"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// templatesRepoBase — базовый URL онлайн-репозитория шаблонов по умолчанию.
const templatesRepoBase = "https://raw.githubusercontent.com/shisui1511/xkeen-control-panel-templates/main"

// Template описывает один конфигурационный шаблон.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "xray" или "mihomo"
	Filename    string `json:"filename,omitempty"`
	Content     string `json:"content,omitempty"`
}

// cachedTemplates — формат DataDir-кэша templates.json (D-03).
type cachedTemplates struct {
	FetchedAt time.Time  `json:"fetched_at"`
	Version   string     `json:"version"`
	Templates []Template `json:"templates"`
}

// TemplateStatus представляет текущий статус обновлений шаблонов.
type TemplateStatus struct {
	TemplatesRepoURL string    `json:"templates_repo_url"`
	CurrentVersion   string    `json:"current_version"`
	LastUpdated      time.Time `json:"last_updated"`
	LastCheck        time.Time `json:"last_check"`
	HasUpdate        bool      `json:"has_update"`
	Incompatible     bool      `json:"incompatible"`
	WarningMessage   string    `json:"warning_message"`
}

// TemplateService предоставляет доступ к конфигурационным шаблонам
// из embedded FS (офлайн) или DataDir-кэша (после онлайн-обновления).
type TemplateService struct {
	embeddedFS       fs.FS
	dataDir          string
	templatesRepoURL string
	templates        []Template
	currentVersion   string
	lastUpdated      time.Time
	lastCheck        time.Time
	hasUpdate        bool
	incompatible     bool
	warningMessage   string
	assetsSvc        *assets.AssetsService
	mu               sync.RWMutex
	httpClient       *http.Client
}

// NewTemplateService создаёт TemplateService с embedded FS, DataDir для кэша, URL репозитория и AssetsService.
// При запуске читает каталог: сначала из DataDir-кэша, затем из embedded FS.
func NewTemplateService(templatesFS fs.FS, dataDir string, repoURL string, assetsSvc *assets.AssetsService) *TemplateService {
	if repoURL == "" {
		repoURL = templatesRepoBase
	}
	svc := &TemplateService{
		embeddedFS:       templatesFS,
		dataDir:          dataDir,
		templatesRepoURL: repoURL,
		assetsSvc:        assetsSvc,
		httpClient:       utils.SafeHTTPClient(10 * time.Second),
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
	var templates []Template
	var version string
	var fetchedAt time.Time

	// Попробовать DataDir-кэш сначала (D-03)
	if cached, err := os.ReadFile(s.storePath()); err == nil {
		var c cachedTemplates
		if json.Unmarshal(cached, &c) == nil && len(c.Templates) > 0 {
			templates = c.Templates
			version = c.Version
			fetchedAt = c.FetchedAt
		}
	}

	if templates == nil {
		// Fallback: embedded catalog.json
		data, err := fs.ReadFile(s.embeddedFS, "catalog.json")
		if err != nil {
			return
		}
		var catalog struct {
			Version   string     `json:"version"`
			Templates []Template `json:"templates"`
		}
		if json.Unmarshal(data, &catalog) == nil {
			templates = catalog.Templates
			version = catalog.Version
		}
	}

	s.mu.Lock()
	s.templates = templates
	s.currentVersion = version
	s.lastUpdated = fetchedAt
	s.mu.Unlock()
}

// List возвращает список доступных шаблонов.
func (s *TemplateService) List() []Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates
}

// FetchByName возвращает содержимое шаблона по имени.
// Читает контент из памяти, дискового кэша или embedded FS (3-уровневый fallback).
func (s *TemplateService) FetchByName(name string) (string, error) {
	s.mu.RLock()
	var filename, templateType string
	var memContent string
	for _, t := range s.templates {
		if t.Name == name {
			filename = t.Filename
			templateType = t.Type
			memContent = t.Content
			break
		}
	}
	s.mu.RUnlock()

	// 1. Уровень 1: Поиск в памяти
	if memContent != "" {
		return memContent, nil
	}

	if filename == "" {
		return "", fmt.Errorf("template not found: %s", name)
	}

	// Санитизация filename через filepath.Base — блокирует path traversal (T-15-05).
	safeName := filepath.Base(filename)

	// Валидация type по allowlist — дополнительная защита от path traversal (CR-01).
	if templateType != "xray" && templateType != "mihomo" {
		return "", fmt.Errorf("invalid template type: %s", templateType)
	}

	// 2. Уровень 2: Поиск в дисковом кэше templates.json
	if cached, err := os.ReadFile(s.storePath()); err == nil {
		var c cachedTemplates
		if json.Unmarshal(cached, &c) == nil {
			for _, t := range c.Templates {
				if t.Name == name && t.Content != "" {
					return t.Content, nil
				}
			}
		}
	}

	// 3. Уровень 3: Читать из embedded FS по пути type/filename.
	content, err := fs.ReadFile(s.embeddedFS, templateType+"/"+safeName)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %w", err)
	}
	return string(content), nil
}

// FetchOnlineUpdates скачивает актуальный каталог из онлайн-репозитория (D-04),
// заполняет содержимое шаблонов и сохраняет кэш в DataDir (D-03).
// При сетевой ошибке embedded-шаблоны не затираются.
func (s *TemplateService) FetchOnlineUpdates() (int, error) {
	repoURL := s.templatesRepoURL
	if repoURL == "" {
		repoURL = templatesRepoBase
	}

	// Скачать catalog.json из онлайн-репозитория
	catalogURL := repoURL + "/catalog.json"
	catalogData, err := s.fetchURL(catalogURL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch online catalog: %w", err)
	}

	var catalog struct {
		Version   string     `json:"version"`
		Templates []Template `json:"templates"`
	}
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		return 0, fmt.Errorf("failed to parse online catalog: %w", err)
	}

	// Скачать assets-definition.json
	assetsURL := repoURL + "/assets-definition.json"
	if assetsData, err := s.fetchURL(assetsURL); err == nil {
		if s.assetsSvc != nil {
			if updateErr := s.assetsSvc.UpdateDefinition(assetsData); updateErr != nil {
				s.mu.Lock()
				s.incompatible = true
				s.warningMessage = updateErr.Error()
				s.mu.Unlock()
				return 0, fmt.Errorf("failed to update assets definition: %w", updateErr)
			}
			s.mu.Lock()
			s.incompatible = false
			s.warningMessage = ""
			s.mu.Unlock()
		}
	} else {
		log.Printf("WARNING: failed to fetch assets definition from %s: %v", assetsURL, err)
	}

	// allowedTypes — разрешённые значения type из каталога (CR-01).
	allowedTypes := map[string]bool{"xray": true, "mihomo": true}

	// Скачать содержимое каждого шаблона
	updated := make([]Template, 0, len(catalog.Templates))
	for _, tmpl := range catalog.Templates {
		if tmpl.Filename == "" || tmpl.Type == "" {
			continue
		}
		// Валидация type по allowlist (CR-01)
		if !allowedTypes[tmpl.Type] {
			continue
		}
		// Санитизация filename (T-15-05)
		safeName := filepath.Base(tmpl.Filename)
		fileURL := repoURL + "/" + tmpl.Type + "/" + safeName

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
	now := time.Now()
	cache := cachedTemplates{
		FetchedAt: now,
		Version:   catalog.Version,
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
	s.currentVersion = catalog.Version
	s.lastUpdated = now
	s.hasUpdate = false
	s.mu.Unlock()

	return len(updated), nil
}

// CheckForUpdates проверяет наличие обновлений на удаленном репозитории.
func (s *TemplateService) CheckForUpdates() (bool, error) {
	repoURL := s.templatesRepoURL
	if repoURL == "" {
		repoURL = templatesRepoBase
	}

	// 1. Проверяем версию каталога
	catalogURL := repoURL + "/catalog.json"
	catalogData, err := s.fetchURL(catalogURL)
	if err != nil {
		return false, fmt.Errorf("failed to fetch remote catalog: %w", err)
	}

	var remoteCatalog struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(catalogData, &remoteCatalog); err != nil {
		return false, fmt.Errorf("failed to parse remote catalog: %w", err)
	}

	// 2. Проверяем версию ассетов (assets-definition.json)
	assetsURL := repoURL + "/assets-definition.json"
	var remoteAssetsVersion string
	var compatibilityErr error
	var fetchedAssets bool
	if assetsData, err := s.fetchURL(assetsURL); err == nil {
		fetchedAssets = true
		var remoteAssets struct {
			SchemaVersion string `json:"schema_version"`
		}
		if json.Unmarshal(assetsData, &remoteAssets) == nil {
			remoteAssetsVersion = remoteAssets.SchemaVersion
		}
		if s.assetsSvc != nil {
			compatibilityErr = s.assetsSvc.CheckCompatibility(assetsData)
		}
	}

	s.mu.Lock()
	localCatalogVer := s.currentVersion
	s.mu.Unlock()

	var localAssetsVer string
	if s.assetsSvc != nil {
		if localAssetsData, err := s.assetsSvc.GetDefinition(); err == nil {
			var localAssets struct {
				SchemaVersion string `json:"schema_version"`
			}
			if json.Unmarshal(localAssetsData, &localAssets) == nil {
				localAssetsVer = localAssets.SchemaVersion
			}
		}
	}

	hasUpdate := false
	if remoteCatalog.Version != "" && remoteCatalog.Version != localCatalogVer {
		hasUpdate = true
	}
	if remoteAssetsVersion != "" && remoteAssetsVersion != localAssetsVer {
		hasUpdate = true
	}

	s.mu.Lock()
	if fetchedAssets {
		if compatibilityErr != nil {
			s.incompatible = true
			s.warningMessage = compatibilityErr.Error()
		} else {
			s.incompatible = false
			s.warningMessage = ""
		}
	}
	s.hasUpdate = hasUpdate
	s.lastCheck = time.Now()
	s.mu.Unlock()

	return hasUpdate, nil
}

// GetStatus возвращает метаданные о статусе обновлений.
func (s *TemplateService) GetStatus() TemplateStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	repoURL := s.templatesRepoURL
	if repoURL == "" {
		repoURL = templatesRepoBase
	}

	return TemplateStatus{
		TemplatesRepoURL: repoURL,
		CurrentVersion:   s.currentVersion,
		LastUpdated:      s.lastUpdated,
		LastCheck:        s.lastCheck,
		HasUpdate:        s.hasUpdate,
		Incompatible:     s.incompatible,
		WarningMessage:   s.warningMessage,
	}
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

// StartBackgroundChecker запускает фоновую проверку обновлений шаблонов.
// Первая проверка запускается с задержкой в 2 минуты, последующие — каждые 24 часа.
func (s *TemplateService) StartBackgroundChecker(ctx context.Context) {
	initialDelay := time.NewTimer(2 * time.Minute)
	defer initialDelay.Stop()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Template background update checker stopped.")
			return
		case <-initialDelay.C:
			log.Println("Running initial templates update check...")
			if _, err := s.CheckForUpdates(); err != nil {
				log.Printf("Background templates update check failed: %v", err)
			}
		case <-ticker.C:
			log.Println("Running periodic templates update check...")
			if _, err := s.CheckForUpdates(); err != nil {
				log.Printf("Background templates update check failed: %v", err)
			}
		}
	}
}
