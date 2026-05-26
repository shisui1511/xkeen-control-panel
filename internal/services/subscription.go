package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// maxSubscriptionBytes caps the download size to 10 MB
const maxSubscriptionBytes = 10 * 1024 * 1024

// invalidIDCharsRe — символы, недопустимые в ID подписки (path injection).
var invalidIDCharsRe = regexp.MustCompile(`[^a-z0-9_-]`)

// subscriptionUserAgent возвращает User-Agent для подписки на основе реальных
// версий установленных ядер:
//   - mihomo-подписки → "mihomo/<версия>" (mihomo нативно качает подписки,
//     провайдеры знают этот UA и отдают clash-meta YAML)
//   - xray-подписки → "v2rayN/<версия xray>" (v2rayN — официальный GUI для
//     Xray-core, оба от 2dust; большинство провайдеров отдают xray-json по этому UA)
func (s *SubscriptionService) subscriptionUserAgent(subType string) string {
	if subType == "mihomo" {
		ver := "1.18.10" // fallback если ядро не найдено
		if s.kernelSvc != nil {
			if k := s.kernelSvc.Get("mihomo"); k != nil && k.CurrentVersion != "" {
				ver = k.CurrentVersion
			}
		}
		return "mihomo/" + ver
	}
	ver := "1.8.24" // fallback если ядро не найдено
	if s.kernelSvc != nil {
		if k := s.kernelSvc.Get("xray"); k != nil && k.CurrentVersion != "" {
			ver = k.CurrentVersion
		}
	}
	return "v2rayN/" + ver
}

// fetchWithUserAgent выполняет GET с правильным User-Agent и HWID-заголовками.
func (s *SubscriptionService) fetchWithUserAgent(subURL string, sub *Subscription) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, subURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.subscriptionUserAgent(sub.Type))

	// HWID Device Limit: per-subscription override or global device HWID.
	hwid := sub.HwidToken
	if hwid == "" {
		hwid = s.hwid
	}
	if hwid != "" {
		req.Header.Set("x-hwid", hwid)
		req.Header.Set("x-device-os", "Linux")
		req.Header.Set("x-device-model", "XKeen Control Panel")
	}
	return s.httpClient.Do(req)
}

// SubscriptionNode представляет метаданные отдельного узла подписки.
type SubscriptionNode struct {
	Tag       string `json:"tag"`                  // Уникальный тег XRay (sub-N-K)
	Name      string `json:"name"`                 // Чистое имя без флагов и мусора
	Country   string `json:"country,omitempty"`    // ISO-код страны (например, RU, DE)
	Flag      string `json:"flag,omitempty"`       // Эмодзи флаг (например, 🇷🇺)
	UseCase   string `json:"use_case,omitempty"`   // Область применения (например, "Youtube, Instagram")
	Speed     string `json:"speed,omitempty"`      // Скорость (например, "1Gb/s")
	IsNew     bool   `json:"is_new,omitempty"`     // Флаг новизны
	Protocol  string `json:"protocol"`             // Протокол (vless, vmess, trojan, shadowsocks)
	Transport string `json:"transport,omitempty"`  // Транспорт (ws, grpc, httpupgrade, xhttp, tcp)
	Security  string `json:"security,omitempty"`   // Безопасность (tls, reality, none)
	Server    string `json:"server,omitempty"`     // Адрес сервера (хост:порт)
	Active    bool   `json:"active,omitempty"`     // Выбран ли узел активным
}

// Subscription represents a proxy subscription
type Subscription struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	TagPrefix  string    `json:"tag_prefix"`
	Interval   int       `json:"interval"` // hours
	LastUpdate time.Time `json:"last_update"`
	Enabled    bool      `json:"enabled"`
	// Type defines the format of the subscription: "xray" (default) or "mihomo"
	Type string `json:"type,omitempty"`

	// Filters (Xray only)
	FilterName      string `json:"filter_name,omitempty"`
	FilterType      string `json:"filter_type,omitempty"`
	FilterTransport string `json:"filter_transport,omitempty"`

	// RoutingMode управляет автоматическим созданием routing-правила (XRay only).
	// "" / "manual" — только запись outbounds, пользователь настраивает routing сам.
	// "auto"         — дополнительно записывать 05_routing.{id}.json с правилом
	//                  geosite:geolocation-!cn → balancer → все прокси подписки.
	RoutingMode string `json:"routing_mode,omitempty"`

	ProxyCount int    `json:"proxy_count"`
	LastError  string `json:"last_error,omitempty"`

	// DetectedFormat — формат, в котором были получены данные при последнем refresh.
	// Значения: "xray-json", "sing-box", "clash-meta", "base64", "share-links".
	DetectedFormat string `json:"detected_format,omitempty"`
	// ProviderType — эвристический тип провайдера по заголовкам ответа.
	// Значения: "remnawave", "marzban", "3x-ui", "custom".
	ProviderType string `json:"provider_type,omitempty"`

	Upload    int64 `json:"upload,omitempty"`
	Download  int64 `json:"download,omitempty"`
	Total     int64 `json:"total,omitempty"`
	RuleCount int   `json:"rule_count,omitempty"`

	// Метаданные из response headers (Remnawave/Marzban протокол).
	ProfileTitle       string `json:"profile_title,omitempty"`        // имя из header profile-title (base64)
	ProfileUpdateHours int    `json:"profile_update_hours,omitempty"` // из header profile-update-interval
	SupportURL         string `json:"support_url,omitempty"`          // из header support-url
	ProfileWebPageURL  string `json:"profile_web_page_url,omitempty"` // из header profile-web-page-url
	Expire             int64  `json:"expire,omitempty"`               // unix ts окончания подписки
	UseProviderInterval bool   `json:"use_provider_interval,omitempty"` // использовать ли интервал провайдера

	// Mihomo state tracking — для in-place правки config.yaml.
	// ProxyNames — имена прокси, принадлежащих этой подписке;
	// при refresh старые блоки удаляются по этим именам и заменяются новыми.
	// ManagedYAML — последний снимок YAML блоков (для diff/drift detection).
	// LastHash — хэш контента, чтобы не дёргать restart если ничего не изменилось.
	// LastChanged — true если последний refresh принёс изменения (для UI badge).
	// MihomoGroups — имена proxy-groups в config.yaml, в которых нужно
	// автоматически держать ссылки на прокси этой подписки.
	ProxyNames   []string `json:"proxy_names,omitempty"`
	ManagedYAML  string   `json:"managed_yaml,omitempty"`
	LastCount    int      `json:"last_count,omitempty"`
	LastSkipped  int      `json:"last_skipped,omitempty"`
	LastHash     string   `json:"last_hash,omitempty"`
	LastChanged  bool     `json:"last_changed,omitempty"`
	MihomoGroups []string `json:"mihomo_groups,omitempty"`

	Nodes        []SubscriptionNode `json:"nodes,omitempty"`
	Announcement string             `json:"announcement,omitempty"`

	// HwidToken — device HWID, отправляется как x-hwid header при запросе подписки.
	// Необходим для провайдеров с device-lock (Remnawave HWID Device Limit).
	// Пользователь задаёт вручную или копирует из Happ.
	HwidToken string `json:"hwid_token,omitempty"`
	// HwidLocked — провайдер вернул X-Hwid-Not-Supported: true при последнем refresh.
	// Означает что без HwidToken будут приходить заглушки вместо реальных нод.
	HwidLocked bool `json:"hwid_locked,omitempty"`
}

// Clone возвращает глубокую копию Subscription.
func (s *Subscription) Clone() Subscription {
	if s == nil {
		return Subscription{}
	}
	res := *s
	if s.ProxyNames != nil {
		res.ProxyNames = make([]string, len(s.ProxyNames))
		copy(res.ProxyNames, s.ProxyNames)
	}
	if s.MihomoGroups != nil {
		res.MihomoGroups = make([]string, len(s.MihomoGroups))
		copy(res.MihomoGroups, s.MihomoGroups)
	}
	if s.Nodes != nil {
		res.Nodes = make([]SubscriptionNode, len(s.Nodes))
		copy(res.Nodes, s.Nodes)
	}
	return res
}


// Outbound represents a parsed proxy outbound
type Outbound struct {
	Tag            string                 `json:"tag"`
	Protocol       string                 `json:"protocol"`
	Settings       map[string]interface{} `json:"settings"`
	StreamSettings map[string]interface{} `json:"streamSettings,omitempty"`
}

// SkipReason описывает причину пропуска конкретной строки/прокси при парсинге.
type SkipReason struct {
	Line    int    `json:"line"`
	Reason  string `json:"reason"`
	Snippet string `json:"snippet"`
}

// ParseReport представляет отчет о результатах парсинга подписки.
type ParseReport struct {
	ParsedCount  int          `json:"parsed_count"`
	SkippedCount int          `json:"skipped_count"`
	Skipped      []SkipReason `json:"skipped"`
	Timestamp    time.Time    `json:"timestamp"`
}


// backoff constants for failed auto-refreshes
const (
	backoffBase = 5 * time.Minute
	backoffMax  = 4 * time.Hour
)

// retryState tracks exponential backoff per subscription.
type retryState struct {
	failCount int
	nextRetry time.Time
}

// SubscriptionService manages subscriptions
type SubscriptionService struct {
	dataDir         string
	configDir       string // Xray config dir for fragment files
	mihomoConfigDir string // Mihomo config dir for proxy-provider files
	subscriptions   []Subscription
	mu              sync.RWMutex
	mihomoMu        sync.Mutex // Mutex для синхронизации записи config.yaml Mihomo
	ongoing         sync.Map   // Map of ID -> struct{}{} to track active refreshes
	retries         sync.Map   // ID -> *retryState for exponential backoff
	httpClient      *http.Client
	consoleSvc      *ConsoleService
	kernelSvc       *KernelService // для получения реальных версий ядер
	hwid            string         // постоянный UUID устройства, передаётся как x-hwid
}

// SetConsoleService подключает ConsoleService для триггера xkeen -restart
// после изменения Mihomo config.yaml.
func (s *SubscriptionService) SetConsoleService(svc *ConsoleService) {
	s.consoleSvc = svc
}

// SetKernelService подключает KernelService для определения реальных версий
// ядер xray и mihomo, используемых в User-Agent при запросах подписок.
func (s *SubscriptionService) SetKernelService(svc *KernelService) {
	s.kernelSvc = svc
}

// loadOrGenerateHWID читает UUID устройства из файла xcp_hwid.txt или
// генерирует новый UUID v4 и сохраняет его для следующих запусков.
// Используется как x-hwid header для провайдеров с HWID Device Limit.
func loadOrGenerateHWID(dataDir string) string {
	dir := filepath.Join(dataDir, "data")
	path := filepath.Join(dir, "xcp_hwid.txt")
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); len(id) == 36 {
			return id
		}
	}
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return ""
	}
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant bits
	id := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(path, []byte(id), 0600)
	return id
}

func NewSubscriptionService(dataDir, configDir, mihomoConfigDir string) *SubscriptionService {
	svc := &SubscriptionService{
		dataDir:         dataDir,
		configDir:       configDir,
		mihomoConfigDir: mihomoConfigDir,
		httpClient:      utils.SafeHTTPClient(30 * time.Second),
		hwid:            loadOrGenerateHWID(dataDir),
	}
	svc.load()
	return svc
}

func (s *SubscriptionService) subPath(filename string) string {
	dir := filepath.Join(s.dataDir, "subscriptions")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, filename)
}

func (s *SubscriptionService) SetHTTPClient(client *http.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.httpClient = client
}

func (s *SubscriptionService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.subPath("subscriptions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &s.subscriptions)

	needSave := false
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == "" {
			s.subscriptions[i].ID = fmt.Sprintf("sub_%d_%d", time.Now().Unix(), i)
			needSave = true
		}
	}
	if needSave {
		indentData, err := json.MarshalIndent(s.subscriptions, "", "  ")
		if err == nil {
			utils.AtomicWriteFile(path, indentData, 0600)
		}
	}
}

func (s *SubscriptionService) save() error {
	// Note: mu must be held by caller or we use a separate locked version
	path := s.subPath("subscriptions.json")
	data, err := json.MarshalIndent(s.subscriptions, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(path, data, 0600)
}

func (s *SubscriptionService) List() []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]Subscription, len(s.subscriptions))
	for i := range s.subscriptions {
		res[i] = s.subscriptions[i].Clone()
		res[i].ProxyCount = s.getProxyCount(&res[i])
	}
	return res
}

func (s *SubscriptionService) Get(id string) *Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			cloned := s.subscriptions[i].Clone()
			cloned.ProxyCount = s.getProxyCount(&cloned)
			return &cloned
		}
	}
	return nil
}

func (s *SubscriptionService) getProxyCount(sub *Subscription) int {
	if sub.Type == "mihomo" {
		// Для Mihomo используем кэшированный счётчик из последнего refresh.
		return sub.LastCount
	}
	path := s.getFragmentPath(sub)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var outbounds []Outbound
	if err := json.Unmarshal(data, &outbounds); err != nil {
		var wrapper struct {
			Outbounds []Outbound `json:"outbounds"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 == nil {
			outbounds = wrapper.Outbounds
		} else {
			return 0
		}
	}
	return len(outbounds)
}

func (s *SubscriptionService) Add(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.ID == "" {
		sub.ID = fmt.Sprintf("sub_%d", time.Now().Unix())
	} else {
		// Санитизируем ID — только [a-z0-9_-] допустимы в имени файла.
		sub.ID = strings.ToLower(sub.ID)
		sub.ID = invalidIDCharsRe.ReplaceAllString(sub.ID, "_")
	}
	s.subscriptions = append(s.subscriptions, *sub)
	return s.save()
}

func (s *SubscriptionService) Update(id string, sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			// Partial update: preserve ID and all runtime-fetched data.
			// Only overwrite user-editable config fields from the form.
			existing := &s.subscriptions[i]
			existing.Name = sub.Name
			existing.URL = sub.URL
			existing.TagPrefix = sub.TagPrefix
			existing.Interval = sub.Interval
			existing.Enabled = sub.Enabled
			existing.FilterName = sub.FilterName
			existing.FilterType = sub.FilterType
			// FilterTransport — обновляем только если явно указан (форма может не отправлять поле).
			if sub.FilterTransport != "" {
				existing.FilterTransport = sub.FilterTransport
			}
			existing.UseProviderInterval = sub.UseProviderInterval
			// Type, RoutingMode, MihomoGroups — обновляем только если явно указаны.
			if sub.Type != "" {
				existing.Type = sub.Type
			}
			if sub.RoutingMode != "" {
				existing.RoutingMode = sub.RoutingMode
			}
			if sub.MihomoGroups != nil {
				existing.MihomoGroups = sub.MihomoGroups
			}
			return s.save()
		}
	}
	return fmt.Errorf("subscription not found")
}

func (s *SubscriptionService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Find subscription
	var sub *Subscription
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			sub = &s.subscriptions[i]
			break
		}
	}
	if sub == nil {
		return fmt.Errorf("subscription not found")
	}

	// Remove from list
	newList := make([]Subscription, 0, len(s.subscriptions)-1)
	for _, s := range s.subscriptions {
		if s.ID != id {
			newList = append(newList, s)
		}
	}
	s.subscriptions = newList

	// Delete managed fragment files (XRay only).
	// Mihomo: прокси будут удалены из config.yaml при следующем refresh
	// или останутся — пользователь может почистить вручную.
	if sub.Type != "mihomo" {
		os.Remove(s.getFragmentPath(sub))
		os.Remove(s.getRoutingFragmentPath(sub)) // noop если файла нет
	}

	// Delete diagnostic files
	os.Remove(s.subPath("sub_" + id + "_raw.txt"))
	os.Remove(s.subPath("sub_" + id + "_headers.json"))
	os.Remove(s.subPath("sub_" + id + "_parse_report.json"))

	return s.save()
}

func (s *SubscriptionService) Refresh(id string) error {
	// Prevent concurrent refreshes for the same ID
	if _, loaded := s.ongoing.LoadOrStore(id, struct{}{}); loaded {
		return fmt.Errorf("refresh already in progress for this subscription")
	}
	defer s.ongoing.Delete(id)

	subCopy, ok := func() (Subscription, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		sub := s.GetLocked(id)
		if sub == nil {
			return Subscription{}, false
		}
		return sub.Clone(), true
	}()
	if !ok {
		return fmt.Errorf("subscription not found")
	}

	var refreshErr error
	if subCopy.Type == "mihomo" {
		refreshErr = s.refreshMihomo(&subCopy)
	} else {
		refreshErr = s.refreshXray(&subCopy)
	}

	// Persist last_error so frontend can show error state
	s.mu.Lock()
	defer s.mu.Unlock()
	if live := s.GetLocked(id); live != nil {
		if refreshErr != nil {
			live.LastError = refreshErr.Error()
		} else {
			live.LastError = ""
			live.LastUpdate = subCopy.LastUpdate
			live.Upload = subCopy.Upload
			live.Download = subCopy.Download
			live.Total = subCopy.Total
			live.Expire = subCopy.Expire
			live.RuleCount = subCopy.RuleCount
			// Метаданные из headers подписки.
			live.ProfileTitle = subCopy.ProfileTitle
			live.ProfileUpdateHours = subCopy.ProfileUpdateHours
			live.SupportURL = subCopy.SupportURL
			live.ProfileWebPageURL = subCopy.ProfileWebPageURL
			// Tracking полей (parse stats + mihomo in-place).
			live.LastCount = subCopy.LastCount
			live.LastSkipped = subCopy.LastSkipped
			live.DetectedFormat = subCopy.DetectedFormat
			live.ProviderType = subCopy.ProviderType
			live.Nodes = subCopy.Nodes
			live.Announcement = subCopy.Announcement
			if subCopy.Type == "mihomo" {
				live.ProxyNames = subCopy.ProxyNames
				live.LastHash = subCopy.LastHash
				live.LastChanged = subCopy.LastChanged
				live.DetectedFormat = "clash-meta"
			}
		}
		_ = s.save()
	}

	return refreshErr
}

func (s *SubscriptionService) refreshXray(sub *Subscription) error {
	// Download subscription (outside of lock to avoid blocking other operations)
	outbounds, skipReasons, body, headers, err := s.downloadAndParse(sub.URL, sub)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-get sub in case it was modified
	live := s.GetLocked(sub.ID)
	if live == nil {
		return fmt.Errorf("subscription not found")
	}

	// Apply filters
	outbounds = s.applyFilters(outbounds, live)

	// Generate fragment file
	fragmentPath := s.getFragmentPath(live)
	nodes, err := s.writeFragment(fragmentPath, outbounds, live)
	if err != nil {
		return err
	}

	sub.Nodes = nodes
	sub.Announcement = parseAnnouncement(body, headers)

	// В режиме "auto" — создать routing-фрагмент с balancer и правилом для !CN.
	if live.RoutingMode == "auto" {
		tags := make([]string, 0, len(outbounds))
		for _, ob := range outbounds {
			tags = append(tags, ob.Tag)
		}
		routingPath := s.getRoutingFragmentPath(live)
		if err := s.writeRoutingFragment(routingPath, live, tags); err != nil {
			log.Printf("routing fragment write error for %s: %v", live.ID, err)
		}
	} else {
		// Если режим изменился с auto → manual, удаляем старый routing-фрагмент.
		os.Remove(s.getRoutingFragmentPath(live))
	}

	sub.LastUpdate = time.Now()

	// Логирование UA-ответа
	log.Printf("[Subscriptions] Refresh Xray ID: %s, Format: %s, Size: %d bytes, Proxies: %d, Skipped: %d",
		sub.ID, sub.DetectedFormat, len(body), sub.LastCount, sub.LastSkipped)

	// Сохранение файлов отладки
	report := &ParseReport{
		ParsedCount:  sub.LastCount,
		SkippedCount: sub.LastSkipped,
		Skipped:      skipReasons,
		Timestamp:    sub.LastUpdate,
	}
	s.saveDebugFiles(sub.ID, body, headers, report)

	return nil
}

func (s *SubscriptionService) refreshMihomo(sub *Subscription) error {
	body, headers, err := s.downloadRaw(sub.URL, sub)
	if err != nil {
		return err
	}

	if !looksLikeClashYAML(string(body)) {
		return fmt.Errorf("данная подписка не имеет формата Clash/Mihomo YAML и не поддерживается ядром Mihomo. Пожалуйста, переключите тип подписки на XRay в её настройках")
	}

	s.mihomoMu.Lock()
	defer s.mihomoMu.Unlock()

	// Извлечь блоки прокси из YAML подписки.
	newBlocks, newNames := ParseMihomoSubscriptionBlocks(string(body))
	if len(newBlocks) == 0 {
		return fmt.Errorf("no proxy blocks found in subscription YAML")
	}

	configDir := s.mihomoConfigDir
	if configDir == "" {
		configDir = "/opt/etc/mihomo"
	}
	configPath := filepath.Join(configDir, "config.yaml")

	// Бэкап перед правкой.
	if err := backupMihomoConfig(s.dataDir, configPath); err != nil {
		return fmt.Errorf("backup mihomo config: %w", err)
	}

	// Читаем текущий config.yaml (если нет — создаём минимальный).
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read mihomo config: %w", err)
		}
		rawConfig = []byte("# Mihomo config — managed by xkeen-control-panel\n")
	}

	// In-place замена прокси этой подписки.
	newConfig := ReplaceMihomoProxies(string(rawConfig), sub.ProxyNames, newBlocks)

	// Обновить ссылки в proxy-groups.
	for _, group := range sub.MihomoGroups {
		newConfig = UpdateMihomoGroupProxies(newConfig, group, newNames, sub.ProxyNames)
	}

	// Сравниваем хэши — restart только при реальных изменениях.
	h := sha256.Sum256([]byte(newConfig))
	newHash := fmt.Sprintf("%x", h[:])
	oldHash := sub.LastHash

	sub.ProxyNames = newNames
	sub.LastCount = len(newNames)
	sub.RuleCount = countMihomoRules(string(body))
	sub.LastHash = newHash
	sub.LastUpdate = time.Now()

	// Генерация списка нод для Mihomo подписки
	nodes := make([]SubscriptionNode, 0, len(newBlocks))
	for _, block := range newBlocks {
		node := ParseClashProxyNode(block)
		if node.Tag == "" {
			continue
		}
		nodes = append(nodes, node)
	}
	sub.Nodes = nodes
	sub.Announcement = parseAnnouncement(body, headers)

	// Логирование UA-ответа
	log.Printf("[Subscriptions] Refresh Mihomo ID: %s, Format: clash-meta, Size: %d bytes, Proxies: %d, Skipped: 0",
		sub.ID, len(body), sub.LastCount)

	// Сохранение файлов отладки
	report := &ParseReport{
		ParsedCount:  sub.LastCount,
		SkippedCount: 0,
		Skipped:      nil,
		Timestamp:    sub.LastUpdate,
	}
	s.saveDebugFiles(sub.ID, body, headers, report)

	if newHash == oldHash {
		sub.LastChanged = false
		return nil
	}

	if err := utils.AtomicWriteFile(configPath, []byte(newConfig), 0600); err != nil {
		return fmt.Errorf("write mihomo config: %w", err)
	}
	sub.LastChanged = true

	// Триггер рестарта через ConsoleService.
	if s.consoleSvc != nil {
		if _, err := s.consoleSvc.Execute("-restart"); err != nil {
			log.Printf("subscription %s: xkeen -restart after mihomo config update: %v", sub.ID, err)
		}
	}

	return nil
}

func (s *SubscriptionService) downloadRaw(subURL string, sub *Subscription) ([]byte, http.Header, error) {
	parsed, err := url.Parse(subURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, nil, fmt.Errorf("only http and https URLs are allowed for subscriptions")
	}
	resp, err := s.fetchWithUserAgent(subURL, sub)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	applySubscriptionHeaders(resp.Header, sub)

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes))
	if err != nil {
		return nil, nil, err
	}
	return body, resp.Header, nil
}

func (s *SubscriptionService) GetLocked(id string) *Subscription {
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			return &s.subscriptions[i]
		}
	}
	return nil
}

func (s *SubscriptionService) downloadAndParse(subURL string, sub *Subscription) ([]Outbound, []SkipReason, []byte, http.Header, error) {
	// C-6: validate URL scheme
	parsed, err := url.Parse(subURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, nil, nil, nil, fmt.Errorf("only http and https URLs are allowed for subscriptions")
	}

	resp, err := s.fetchWithUserAgent(subURL, sub)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer resp.Body.Close()

	applySubscriptionHeaders(resp.Header, sub)

	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// C-4: cap download size to 10 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	outs, skipReasons, err := parseSubscriptionBody(body, resp.Header.Get("Content-Type"), sub)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return outs, skipReasons, body, resp.Header, nil
}

// parseSubscriptionBody detects the format of a subscription response and parses it
// into outbounds. It tries formats in priority order: sing-box JSON, xray JSON,
// clash YAML, then base64/share-links.
func parseSubscriptionBody(body []byte, contentTypeHeader string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	contentType := strings.ToLower(contentTypeHeader)
	content := strings.TrimSpace(string(body))

	// 1) Sing-box JSON
	if (contentType == "" || strings.Contains(contentType, "json")) && looksLikeSingBoxJSON(body) {
		if outs, err := parseSingBoxJSON(body); err == nil && len(outs) > 0 {
			sub.DetectedFormat = "sing-box"
			sub.LastCount = len(outs)
			sub.LastSkipped = 0
			return outs, nil, nil
		}
	}

	// 2) Xray full-config array (each element is a complete xray config with "remarks" as node name)
	if outs := parseXrayConfigArray(body); len(outs) > 0 {
		sub.DetectedFormat = "xray-json"
		sub.LastCount = len(outs)
		sub.LastSkipped = 0
		return outs, nil, nil
	}

	// 3) Xray JSON array of outbounds (with non-empty protocol)
	var jsonOutbounds []Outbound
	if err := json.Unmarshal(body, &jsonOutbounds); err == nil {
		// filter to outbounds that actually have a protocol (avoids false positive on config arrays)
		var valid []Outbound
		for _, o := range jsonOutbounds {
			if o.Protocol != "" {
				valid = append(valid, o)
			}
		}
		if len(valid) > 0 {
			sub.DetectedFormat = "xray-json"
			sub.LastCount = len(valid)
			sub.LastSkipped = 0
			return valid, nil, nil
		}
	}

	// 4) Xray JSON object
	var jsonConfig struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &jsonConfig); err == nil && len(jsonConfig.Outbounds) > 0 {
		sub.DetectedFormat = "xray-json"
		sub.LastCount = len(jsonConfig.Outbounds)
		sub.LastSkipped = 0
		return jsonConfig.Outbounds, nil, nil
	}

	// 5) Clash/Mihomo YAML Check
	if looksLikeClashYAML(content) {
		return nil, nil, fmt.Errorf("данная подписка имеет формат Clash/Mihomo YAML и не поддерживается ядром XRay. Пожалуйста, переключите тип подписки на Mihomo в её настройках")
	}

	// 6) Base64 or plain share-links
	return parseShareLinks(content, sub)
}

func looksLikeClashYAML(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return false
	}
	for _, line := range strings.SplitN(trimmed, "\n", 50) {
		l := strings.TrimSpace(line)
		if l == "proxies:" || strings.HasPrefix(l, "proxies:") {
			return true
		}
	}
	return false
}

// parseXrayConfigArray parses a subscription where the response is a JSON array
// of complete Xray configs (each element has dns/routing/outbounds/remarks).
// Each element represents one server; "remarks" is used as the node tag.
// Returns nil if the body does not match this format.
func parseXrayConfigArray(body []byte) []Outbound {
	var configs []struct {
		Remarks   string     `json:"remarks"`
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(body, &configs); err != nil || len(configs) == 0 {
		return nil
	}
	// Only match if first element looks like a full config (has outbounds with protocol)
	if len(configs[0].Outbounds) == 0 {
		return nil
	}
	hasProto := false
	for _, o := range configs[0].Outbounds {
		if o.Protocol != "" {
			hasProto = true
			break
		}
	}
	if !hasProto {
		return nil
	}

	proxyProtocols := map[string]bool{
		"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
		"socks": true, "http": true, "wireguard": true,
	}

	var result []Outbound
	for _, cfg := range configs {
		// Find the primary proxy outbound (first one with a proxy protocol)
		for _, ob := range cfg.Outbounds {
			if !proxyProtocols[ob.Protocol] {
				continue
			}
			out := ob
			// Use "remarks" as the tag for this server
			if cfg.Remarks != "" {
				out.Tag = cfg.Remarks
			}
			result = append(result, out)
			break
		}
	}
	return result
}

// parseShareLinks parses a subscription body that is either a base64-encoded or
// plain newline-separated list of proxy share links.
func parseShareLinks(content string, sub *Subscription) ([]Outbound, []SkipReason, error) {
	wasBase64 := false
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(content)
	}
	if err == nil {
		content = string(decoded)
		wasBase64 = true
	}

	lines := strings.Split(content, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 5000 {
		return nil, nil, fmt.Errorf("subscription too large: %d entries (max 5000)", nonEmpty)
	}

	var outbounds []Outbound
	var skipReasons []SkipReason
	skipped := 0
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if outbound := parseShareLink(line); outbound != nil {
			outbounds = append(outbounds, *outbound)
		} else {
			skipped++
			snippet := line
			if len(snippet) > 60 {
				snippet = snippet[:57] + "..."
			}
			skipReasons = append(skipReasons, SkipReason{
				Line:    idx + 1,
				Reason:  skipReasonForScheme(line),
				Snippet: snippet,
			})
		}
	}

	sub.LastCount = len(outbounds)
	sub.LastSkipped = skipped
	if wasBase64 {
		sub.DetectedFormat = "base64"
	} else {
		sub.DetectedFormat = "share-links"
	}
	return outbounds, skipReasons, nil
}

// skipReasonForScheme returns a human-readable skip reason based on the URL scheme prefix.
func skipReasonForScheme(line string) string {
	switch {
	case strings.HasPrefix(line, "vmess://"):
		return "ошибка декодирования или невалидный JSON в vmess://"
	case strings.HasPrefix(line, "vless://"):
		return "невалидный URL или порт в vless://"
	case strings.HasPrefix(line, "trojan://"):
		return "невалидный URL или порт в trojan://"
	case strings.HasPrefix(line, "ss://"):
		return "невалидный URL или порт в ss://"
	case strings.HasPrefix(line, "hy2://"):
		return "невалидный URL или порт в hy2://"
	case strings.HasPrefix(line, "tuic://"):
		return "невалидный URL или порт в tuic://"
	case strings.HasPrefix(line, "socks://"), strings.HasPrefix(line, "socks5://"):
		return "невалидный URL или порт в socks://"
	case strings.HasPrefix(line, "http-proxy://"):
		return "невалидный URL или порт в http-proxy://"
	default:
		return "неподдерживаемый протокол или невалидный URL"
	}
}

func (s *SubscriptionService) saveDebugFiles(id string, body []byte, headers http.Header, report *ParseReport) {
	rawPath := s.subPath("sub_" + id + "_raw.txt")
	_ = utils.AtomicWriteFile(rawPath, body, 0600)

	hdrMap := make(map[string][]string)
	for k, v := range headers {
		hdrMap[k] = v
	}
	hdrData, err := json.MarshalIndent(hdrMap, "", "  ")
	if err == nil {
		hdrPath := s.subPath("sub_" + id + "_headers.json")
		_ = utils.AtomicWriteFile(hdrPath, hdrData, 0600)
	}

	if report != nil {
		repData, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			repPath := s.subPath("sub_" + id + "_parse_report.json")
			_ = utils.AtomicWriteFile(repPath, repData, 0600)
		}
	}
}

// GetRaw возвращает сырое тело ответа и заголовки последней загрузки подписки.
func (s *SubscriptionService) GetRaw(id string) (string, map[string][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exists := false
	for _, sub := range s.subscriptions {
		if sub.ID == id {
			exists = true
			break
		}
	}
	if !exists {
		return "", nil, fmt.Errorf("subscription not found")
	}

	rawPath := s.subPath("sub_" + id + "_raw.txt")
	bodyBytes, err := os.ReadFile(rawPath)
	if err != nil {
		return "", nil, fmt.Errorf("raw response not found: %w", err)
	}

	hdrPath := s.subPath("sub_" + id + "_headers.json")
	hdrBytes, err := os.ReadFile(hdrPath)
	if err != nil {
		return string(bodyBytes), nil, nil
	}

	var headers map[string][]string
	if err := json.Unmarshal(hdrBytes, &headers); err != nil {
		return string(bodyBytes), nil, nil
	}

	return string(bodyBytes), headers, nil
}

// GetParseReport возвращает отчет о результатах парсинга последней загрузки подписки.
func (s *SubscriptionService) GetParseReport(id string) (*ParseReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exists := false
	for _, sub := range s.subscriptions {
		if sub.ID == id {
			exists = true
			break
		}
	}
	if !exists {
		return nil, fmt.Errorf("subscription not found")
	}

	repPath := s.subPath("sub_" + id + "_parse_report.json")
	repBytes, err := os.ReadFile(repPath)
	if err != nil {
		return nil, fmt.Errorf("parse report not found: %w", err)
	}

	var report ParseReport
	if err := json.Unmarshal(repBytes, &report); err != nil {
		return nil, err
	}

	return &report, nil
}

// SetActiveNode перемещает ноду с указанным тегом на первую позицию в
// 04_outbounds.{id}.json. XRay читает outbounds по порядку и использует первый
// в качестве активного. Доступно только при routing_mode = "manual".
func (s *SubscriptionService) SetActiveNode(subscriptionID, nodeTag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub := s.GetLocked(subscriptionID)
	if sub == nil {
		return fmt.Errorf("subscription not found")
	}
	if sub.Type == "mihomo" {
		return fmt.Errorf("active node selection is not supported for Mihomo subscriptions")
	}
	if sub.RoutingMode == "auto" {
		return fmt.Errorf("cannot set active node in auto routing mode (balancer is managing selection)")
	}

	fragmentPath := s.getFragmentPath(sub)
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		return fmt.Errorf("outbounds file not found: %w", err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return fmt.Errorf("parse outbounds: %w", err)
	}

	// Находим ноду по тегу
	idx := -1
	for i, ob := range wrapper.Outbounds {
		if ob.Tag == nodeTag {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("node %q not found in subscription outbounds", nodeTag)
	}

	// Перемещаем на первую позицию
	if idx > 0 {
		selected := wrapper.Outbounds[idx]
		newOutbounds := make([]Outbound, 0, len(wrapper.Outbounds))
		newOutbounds = append(newOutbounds, selected)
		newOutbounds = append(newOutbounds, wrapper.Outbounds[:idx]...)
		newOutbounds = append(newOutbounds, wrapper.Outbounds[idx+1:]...)
		wrapper.Outbounds = newOutbounds
	}

	// Обновляем Active-флаг в Nodes
	for i := range sub.Nodes {
		sub.Nodes[i].Active = sub.Nodes[i].Tag == nodeTag
	}

	newData, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}
	if err := utils.AtomicWriteFile(fragmentPath, newData, 0600); err != nil {
		return err
	}
	_ = s.save()

	// Триггер рестарта через ConsoleService.
	if s.consoleSvc != nil {
		if _, err := s.consoleSvc.Execute("-restart"); err != nil {
			log.Printf("subscription %s: xkeen -restart after active node switch: %v", sub.ID, err)
		}
	}

	return nil
}

func (s *SubscriptionService) applyFilters(outbounds []Outbound, sub *Subscription) []Outbound {
	if sub.FilterName == "" && sub.FilterType == "" && sub.FilterTransport == "" {
		return outbounds
	}

	// Компилируем regex для FilterName (с case-insensitive флагом).
	// Если pattern невалидный — трактуем как пустой (не фильтруем по имени).
	var nameRe *regexp.Regexp
	if sub.FilterName != "" {
		if r, err := regexp.Compile("(?i)" + sub.FilterName); err == nil {
			nameRe = r
		}
	}

	var filtered []Outbound
	for _, ob := range outbounds {
		if nameRe != nil && !nameRe.MatchString(ob.Tag) {
			continue
		}
		if sub.FilterType != "" && !strings.EqualFold(ob.Protocol, sub.FilterType) {
			continue
		}
		if sub.FilterTransport != "" {
			transport := ""
			if ob.StreamSettings != nil {
				if net, ok := ob.StreamSettings["network"].(string); ok {
					transport = net
				}
			}
			if !strings.EqualFold(transport, sub.FilterTransport) {
				continue
			}
		}
		filtered = append(filtered, ob)
	}

	return filtered
}

func (s *SubscriptionService) getFragmentPath(sub *Subscription) string {
	return filepath.Join(s.configDir, fmt.Sprintf("04_outbounds.%s.json", sub.ID))
}

func (s *SubscriptionService) getRoutingFragmentPath(sub *Subscription) string {
	return filepath.Join(s.configDir, fmt.Sprintf("05_routing.%s.json", sub.ID))
}

// writeRoutingFragment записывает XRay confdir-фрагмент с balancer и routing-правилом.
//
// Структура:
//
//	{
//	  "routing": {
//	    "balancers": [{"tag": "{id}-balancer", "selector": ["{prefix}-"]}],
//	    "rules": [{"type":"field","domain":["geosite:geolocation-!cn"],"balancerTag":"{id}-balancer"}]
//	  }
//	}
//
// Если у подписки нет TagPrefix — маршрутизируем напрямую к первому прокси.
func (s *SubscriptionService) writeRoutingFragment(path string, sub *Subscription, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	type Rule struct {
		Type        string   `json:"type"`
		Domain      []string `json:"domain"`
		OutboundTag string   `json:"outboundTag,omitempty"`
		BalancerTag string   `json:"balancerTag,omitempty"`
	}
	type Balancer struct {
		Tag      string   `json:"tag"`
		Selector []string `json:"selector"`
	}
	type Routing struct {
		Balancers []Balancer `json:"balancers,omitempty"`
		Rules     []Rule     `json:"rules"`
	}
	type Fragment struct {
		Routing Routing `json:"routing"`
	}

	var frag Fragment
	domains := []string{"geosite:geolocation-!cn", "geoip:!cn"}

	if sub.TagPrefix != "" {
		// Balancer выбирает прокси по префиксу тега — работает для любого числа прокси.
		balancerTag := sub.ID + "-balancer"
		frag = Fragment{
			Routing: Routing{
				Balancers: []Balancer{{
					Tag:      balancerTag,
					Selector: []string{sub.TagPrefix + "-"},
				}},
				Rules: []Rule{{
					Type:        "field",
					Domain:      domains,
					BalancerTag: balancerTag,
				}},
			},
		}
	} else {
		// Без префикса — напрямую к первому тегу.
		frag = Fragment{
			Routing: Routing{
				Rules: []Rule{{
					Type:        "field",
					Domain:      domains,
					OutboundTag: tags[0],
				}},
			},
		}
	}

	data, err := json.MarshalIndent(frag, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return utils.AtomicWriteFile(path, data, 0600)
}

func (s *SubscriptionService) writeFragment(path string, outbounds []Outbound, sub *Subscription) ([]SubscriptionNode, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	nodes := make([]SubscriptionNode, 0, len(outbounds))
	seen := make(map[string]int)
	for i := range outbounds {
		origTag := outbounds[i].Tag

		// Add tag prefix and deduplicate tags
		if sub.TagPrefix != "" {
			outbounds[i].Tag = fmt.Sprintf("%s-%s", sub.TagPrefix, outbounds[i].Tag)
		}
		tag := outbounds[i].Tag
		if count, exists := seen[tag]; exists {
			outbounds[i].Tag = fmt.Sprintf("%s-%d", tag, count)
			seen[tag]++
		} else {
			seen[tag] = 1
		}

		// Парсим оригинальный тег (remark) для метаданных
		node := parseRemark(origTag)
		node.Tag = outbounds[i].Tag
		node.Protocol = outbounds[i].Protocol
		node.Server = extractServer(&outbounds[i])

		// Извлекаем transport и security
		node.Transport = "tcp"
		node.Security = "none"
		if outbounds[i].StreamSettings != nil {
			if net, ok := outbounds[i].StreamSettings["network"].(string); ok && net != "" {
				node.Transport = net
			}
			if sec, ok := outbounds[i].StreamSettings["security"].(string); ok && sec != "" {
				node.Security = sec
			}
		}

		nodes = append(nodes, node)
	}

	wrapper := struct {
		Outbounds []Outbound `json:"outbounds"`
	}{
		Outbounds: outbounds,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := utils.AtomicWriteFile(path, data, 0600); err != nil {
		return nil, err
	}

	return nodes, nil
}

// parseShareLink parses various share link formats
func parseShareLink(link string) *Outbound {
	// vmess://
	if strings.HasPrefix(link, "vmess://") {
		return parseVMessLink(link)
	}

	// vless://
	if strings.HasPrefix(link, "vless://") {
		return parseVLESSLink(link)
	}

	// trojan://
	if strings.HasPrefix(link, "trojan://") {
		return parseTrojanLink(link)
	}

	// ss:// (Shadowsocks)
	if strings.HasPrefix(link, "ss://") {
		return parseSSLink(link)
	}

	// hy2:// (Hysteria2)
	if strings.HasPrefix(link, "hy2://") {
		return parseHysteria2Link(link)
	}

	// tuic:// (TUIC)
	if strings.HasPrefix(link, "tuic://") {
		return parseTUICLink(link)
	}

	// socks:// or socks5://
	if strings.HasPrefix(link, "socks://") || strings.HasPrefix(link, "socks5://") {
		return parseSOCKSLink(link)
	}

	// http:// proxy (must come after http-based subscription URL check is done)
	if strings.HasPrefix(link, "http-proxy://") {
		return parseHTTPProxyLink(link)
	}

	return nil
}

func parseVMessLink(link string) *Outbound {
	// vmess://base64(json) — some clients use URL-safe base64 without padding
	b64 := strings.TrimPrefix(link, "vmess://")
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Try URL-safe base64 with padding
		padded := b64
		if rem := len(padded) % 4; rem != 0 {
			padded += strings.Repeat("=", 4-rem)
		}
		var err2 error
		data, err2 = base64.URLEncoding.DecodeString(padded)
		if err2 != nil {
			// Try raw URL-safe base64 (no padding required)
			data, err2 = base64.RawURLEncoding.DecodeString(b64)
			if err2 != nil {
				return nil
			}
		}
	}

	var vmess struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
		Aid  string `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		Sni  string `json:"sni"`
	}

	if err := json.Unmarshal(data, &vmess); err != nil {
		return nil
	}

	portInt, err := strconv.Atoi(vmess.Port)
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}
	aidInt, _ := strconv.Atoi(vmess.Aid) // Aid=0 if empty/invalid — valid default

	// Build StreamSettings from VMess JSON fields
	streamSettings := map[string]interface{}{}
	if vmess.Net != "" {
		streamSettings["network"] = vmess.Net
	}
	switch vmess.Net {
	case "ws":
		wsSettings := map[string]interface{}{}
		if vmess.Path != "" {
			wsSettings["path"] = vmess.Path
		}
		if vmess.Host != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": vmess.Host}
		}
		if len(wsSettings) > 0 {
			streamSettings["wsSettings"] = wsSettings
		}
	case "grpc":
		if vmess.Path != "" {
			streamSettings["grpcSettings"] = map[string]interface{}{"serviceName": vmess.Path}
		}
	}
	if vmess.TLS == "tls" {
		tlsSettings := map[string]interface{}{}
		sni := vmess.Sni
		if sni == "" {
			sni = vmess.Host
		}
		if sni != "" {
			tlsSettings["serverName"] = sni
		}
		if len(tlsSettings) > 0 {
			streamSettings["tlsSettings"] = tlsSettings
		}
	}

	ob := &Outbound{
		Tag:      vmess.PS,
		Protocol: "vmess",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": vmess.Add,
					"port":    portInt,
					"users": []map[string]interface{}{
						{
							"id":       vmess.ID,
							"alterId":  aidInt,
							"security": "auto",
						},
					},
				},
			},
		},
	}
	if len(streamSettings) > 0 {
		ob.StreamSettings = streamSettings
	}
	return ob
}

func parseVLESSLink(link string) *Outbound {
	// vless://id@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	id := ""
	if u.User != nil {
		id = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	// Build user entry
	user := map[string]interface{}{
		"id":         id,
		"encryption": "none",
	}
	// flow parameter
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}

	// Build StreamSettings from query params; unknown keys are silently ignored
	streamSettings := map[string]interface{}{}
	network := q.Get("type")
	if network != "" {
		streamSettings["network"] = network
	}
	security := q.Get("security")
	if security != "" {
		streamSettings["security"] = security
	}

	switch security {
	case "reality":
		realitySettings := map[string]interface{}{}
		if pbk := q.Get("pbk"); pbk != "" {
			realitySettings["publicKey"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			realitySettings["shortId"] = sid
		}
		if sni := q.Get("sni"); sni != "" {
			realitySettings["serverName"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			realitySettings["fingerprint"] = fp
		}
		if len(realitySettings) > 0 {
			streamSettings["realitySettings"] = realitySettings
		}
	case "tls":
		tlsSettings := map[string]interface{}{}
		if sni := q.Get("sni"); sni != "" {
			tlsSettings["serverName"] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			tlsSettings["fingerprint"] = fp
		}
		if alpnStr := q.Get("alpn"); alpnStr != "" {
			tlsSettings["alpn"] = strings.Split(alpnStr, ",")
		}
		if len(tlsSettings) > 0 {
			streamSettings["tlsSettings"] = tlsSettings
		}
	}

	// WebSocket settings (network=ws)
	if network == "ws" {
		wsSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			wsSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": host}
		}
		if len(wsSettings) > 0 {
			streamSettings["wsSettings"] = wsSettings
		}
	}

	// HTTPUpgrade settings (network=httpupgrade)
	if network == "httpupgrade" {
		huSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			huSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			huSettings["host"] = host
		}
		if len(huSettings) > 0 {
			streamSettings["httpupgradeSettings"] = huSettings
		}
	}

	// XHTTP / SplitHTTP settings (network=xhttp)
	if network == "xhttp" {
		xhttpSettings := map[string]interface{}{}
		if path := q.Get("path"); path != "" {
			xhttpSettings["path"] = path
		}
		if host := q.Get("host"); host != "" {
			xhttpSettings["host"] = host
		}
		if mode := q.Get("mode"); mode != "" {
			xhttpSettings["mode"] = mode
		}
		if len(xhttpSettings) > 0 {
			streamSettings["xhttpSettings"] = xhttpSettings
		}
	}

	ob := &Outbound{
		Tag:      tag,
		Protocol: "vless",
		Settings: map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": u.Hostname(),
					"port":    portInt,
					"users":   []map[string]interface{}{user},
				},
			},
		},
	}
	if len(streamSettings) > 0 {
		ob.StreamSettings = streamSettings
	}
	return ob
}

func parseTrojanLink(link string) *Outbound {
	// trojan://password@host:port?params#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	// In trojan:// URIs, the password is the entire userinfo (before @), not a "password" field
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	// Build StreamSettings from query params; unknown keys are silently ignored
	security := q.Get("security")
	if security == "" {
		security = "tls" // default for trojan
	}
	streamSettings := map[string]interface{}{
		"security": security,
	}
	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if fp := q.Get("fp"); fp != "" {
		tlsSettings["fingerprint"] = fp
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		tlsSettings["alpn"] = strings.Split(alpnStr, ",")
	}
	if len(tlsSettings) > 0 {
		streamSettings["tlsSettings"] = tlsSettings
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "trojan",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     portInt,
					"password": password,
				},
			},
		},
		StreamSettings: streamSettings,
	}
}

func parseHysteria2Link(link string) *Outbound {
	// hy2://password@host:port?sni=...&obfs=...&obfs-password=...&insecure=...#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	password := ""
	if u.User != nil {
		password = u.User.Username()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if insecure := q.Get("insecure"); insecure == "1" || insecure == "true" {
		tlsSettings["allowInsecure"] = true
	}

	streamSettings := map[string]interface{}{
		"network":     "tcp",
		"security":    "tls",
		"tlsSettings": tlsSettings,
	}

	settings := map[string]interface{}{
		"servers": []map[string]interface{}{
			{
				"address":  u.Hostname(),
				"port":     portInt,
				"password": password,
			},
		},
	}

	// obfs settings placed in settings; unknown params silently ignored
	if obfs := q.Get("obfs"); obfs != "" {
		obfsMap := map[string]interface{}{"type": obfs}
		if obfsPass := q.Get("obfs-password"); obfsPass != "" {
			obfsMap["password"] = obfsPass
		}
		settings["hysteria2Settings"] = map[string]interface{}{
			"obfs": obfsMap,
		}
	}

	return &Outbound{
		Tag:            tag,
		Protocol:       "hysteria2",
		Settings:       settings,
		StreamSettings: streamSettings,
	}
}

func parseTUICLink(link string) *Outbound {
	// tuic://uuid:password@host:port?sni=...&congestion_control=...&alpn=...#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	uuid := ""
	password := ""
	if u.User != nil {
		uuid = u.User.Username()
		password, _ = u.User.Password()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	q := u.Query()

	tlsSettings := map[string]interface{}{}
	if sni := q.Get("sni"); sni != "" {
		tlsSettings["serverName"] = sni
	}
	if alpnStr := q.Get("alpn"); alpnStr != "" {
		tlsSettings["alpn"] = strings.Split(alpnStr, ",")
	}

	server := map[string]interface{}{
		"address":  u.Hostname(),
		"port":     portInt,
		"uuid":     uuid,
		"password": password,
	}
	// unknown params silently ignored
	if cc := q.Get("congestion_control"); cc != "" {
		server["congestionControl"] = cc
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "tuic",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
		StreamSettings: map[string]interface{}{
			"network":     "udp",
			"security":    "tls",
			"tlsSettings": tlsSettings,
		},
	}
}

func parseSSLink(link string) *Outbound {
	// ss://method:password@host:port#tag
	// or ss://base64(method:password)@host:port#tag
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	// Try to decode base64 user info
	userInfo := u.User.String()
	decoded, err := base64.StdEncoding.DecodeString(userInfo)
	if err == nil {
		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) == 2 {
			u.User = url.UserPassword(parts[0], parts[1])
		}
	}

	method := ""
	password := ""
	if u.User != nil {
		method = u.User.Username()
		password, _ = u.User.Password()
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "shadowsocks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  u.Hostname(),
					"port":     portInt,
					"method":   method,
					"password": password,
				},
			},
		},
	}
}

func parseSOCKSLink(link string) *Outbound {
	// socks:// or socks5://user:pass@host:port#tag
	// Normalise socks5:// to socks:// so url.Parse works uniformly
	link = strings.Replace(link, "socks5://", "socks://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	server := map[string]interface{}{
		"address": u.Hostname(),
		"port":    portInt,
	}
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		if user != "" {
			server["users"] = []map[string]interface{}{
				{"user": user, "pass": pass},
			}
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "socks",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
	}
}

// parseHTTPProxyLink parses http-proxy://user:pass@host:port#tag share links.
// Uses the "http-proxy://" scheme to avoid conflicts with http:// subscription URLs.
func parseHTTPProxyLink(link string) *Outbound {
	// Normalise http-proxy:// → http:// so url.Parse can handle it
	link = strings.Replace(link, "http-proxy://", "http://", 1)
	u, err := url.Parse(link)
	if err != nil {
		return nil
	}

	tag := u.Fragment
	if tag == "" {
		tag = u.Hostname()
	}

	portInt, err := strconv.Atoi(u.Port())
	if err != nil || portInt < 1 || portInt > 65535 {
		return nil
	}

	server := map[string]interface{}{
		"address": u.Hostname(),
		"port":    portInt,
	}
	if u.User != nil {
		user := u.User.Username()
		pass, _ := u.User.Password()
		if user != "" {
			server["users"] = []map[string]interface{}{
				{"user": user, "pass": pass},
			}
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "http",
		Settings: map[string]interface{}{
			"servers": []map[string]interface{}{server},
		},
	}
}

// ParseLinksResult holds the result for a single link parse attempt.
type ParseLinksResult struct {
	Link     string    `json:"link"`
	Outbound *Outbound `json:"outbound,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// ParseLinks parses a slice of share links and returns results for each.
// Unsupported or invalid links are reported as errors, not fatal failures.
func (s *SubscriptionService) ParseLinks(links []string) []ParseLinksResult {
	results := make([]ParseLinksResult, 0, len(links))
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		ob := parseShareLink(link)
		if ob == nil {
			results = append(results, ParseLinksResult{
				Link:  link,
				Error: "unsupported or invalid share link format",
			})
		} else {
			results = append(results, ParseLinksResult{
				Link:     link,
				Outbound: ob,
			})
		}
	}
	return results
}

// isRefreshDue returns true if a subscription needs to be refreshed.
// Respects the exponential backoff state for previously-failed refreshes.
func (s *SubscriptionService) isRefreshDue(sub *Subscription, now time.Time) bool {
	interval := sub.Interval
	if sub.UseProviderInterval && sub.ProfileUpdateHours > 0 {
		interval = sub.ProfileUpdateHours
	}
	if !sub.Enabled || interval <= 0 {
		return false
	}
	// Check backoff: if a previous attempt failed, wait until nextRetry
	if val, ok := s.retries.Load(sub.ID); ok {
		rs := val.(*retryState)
		if now.Before(rs.nextRetry) {
			return false
		}
	}
	return now.Sub(sub.LastUpdate) >= time.Duration(interval)*time.Hour
}

// recordFailure increments the failure counter and schedules the next retry
// with exponential backoff capped at backoffMax.
func (s *SubscriptionService) recordFailure(id string) {
	rs := &retryState{failCount: 1}
	if val, ok := s.retries.Load(id); ok {
		rs = val.(*retryState)
		rs.failCount++
	}
	delay := backoffBase * (1 << uint(rs.failCount-1))
	if delay > backoffMax {
		delay = backoffMax
	}
	rs.nextRetry = time.Now().Add(delay)
	s.retries.Store(id, rs)
}

// clearFailure resets the backoff state on a successful refresh.
func (s *SubscriptionService) clearFailure(id string) {
	s.retries.Delete(id)
}

// checkAndRefreshDue scans all subscriptions and launches a goroutine for
// each one that is due at the given point in time. Failed refreshes are
// subject to exponential backoff.
func (s *SubscriptionService) checkAndRefreshDue(now time.Time) {
	subs := s.List()
	for _, sub := range subs {
		if s.isRefreshDue(&sub, now) {
			go func(id string) {
				if err := s.Refresh(id); err != nil {
					// "already in progress" is not a real failure — skip backoff
					if !strings.Contains(err.Error(), "already in progress") {
						s.recordFailure(id)
						fc := 0
						if val, ok := s.retries.Load(id); ok {
							fc = val.(*retryState).failCount
						}
						log.Printf("subscription %s: auto-refresh failed (attempt %d): %v", id, fc, err)
					}
				} else {
					s.clearFailure(id)
				}
			}(sub.ID)
		}
	}
}

// RunScheduler starts a background loop that refreshes overdue subscriptions
// every checkInterval. It exits cleanly when ctx is cancelled.
func (s *SubscriptionService) RunScheduler(ctx context.Context, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.checkAndRefreshDue(t)
		}
	}
}

// parseSubscriptionUserinfo parses values from Subscription-Userinfo header:
// e.g., upload=123; download=456; total=789; expire=0
func parseSubscriptionUserinfo(header string) (upload, download, total, expire int64) {
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		vStr := strings.TrimSpace(kv[1])
		val, err := strconv.ParseInt(vStr, 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "upload":
			upload = val
		case "download":
			download = val
		case "total":
			total = val
		case "expire":
			expire = val
		}
	}
	return
}

// applySubscriptionHeaders читает все стандартные headers подписки
// (Remnawave/Marzban/X-UI протокол) и записывает в Subscription.
//
// Распознаваемые headers:
//   - Subscription-Userinfo: upload=N; download=N; total=N; expire=TS
//   - profile-title: base64(name) — имя подписки в клиенте
//   - profile-update-interval: hours — частота auto-refresh (диктует провайдер)
//   - support-url: URL контакта поддержки провайдера
//   - profile-web-page-url: URL человеко-читаемой web-страницы подписки
func applySubscriptionHeaders(h http.Header, sub *Subscription) {
	if userInfo := h.Get("Subscription-Userinfo"); userInfo != "" {
		sub.Upload, sub.Download, sub.Total, sub.Expire = parseSubscriptionUserinfo(userInfo)
	} else {
		sub.Upload, sub.Download, sub.Total, sub.Expire = 0, 0, 0, 0
	}

	// profile-title — base64-encoded имя профиля.
	// Провайдеры присылают его как `profile-title: base64:My VPN`
	// или просто `profile-title: <base64>`.
	if title := h.Get("profile-title"); title != "" {
		title = strings.TrimPrefix(title, "base64:")
		if decoded, err := base64.StdEncoding.DecodeString(title); err == nil {
			sub.ProfileTitle = strings.TrimSpace(string(decoded))
		} else if decoded, err := base64.URLEncoding.DecodeString(title); err == nil {
			sub.ProfileTitle = strings.TrimSpace(string(decoded))
		} else {
			// Plain text fallback (некоторые провайдеры не кодируют).
			sub.ProfileTitle = strings.TrimSpace(title)
		}
	}

	if updInt := h.Get("profile-update-interval"); updInt != "" {
		if hours, err := strconv.Atoi(strings.TrimSpace(updInt)); err == nil && hours > 0 {
			sub.ProfileUpdateHours = hours
		}
	}

	sub.SupportURL = strings.TrimSpace(h.Get("support-url"))
	sub.ProfileWebPageURL = strings.TrimSpace(h.Get("profile-web-page-url"))

	// Remnawave HWID Device Limit: если провайдер вернул этот header,
	// значит HWID не принят и будут приходить заглушки вместо реальных нод.
	if strings.EqualFold(strings.TrimSpace(h.Get("x-hwid-not-supported")), "true") {
		sub.HwidLocked = true
	} else {
		sub.HwidLocked = false
	}

	// Эвристическое определение типа провайдера по headers и URL.
	sub.ProviderType = detectProviderType(h, sub.ProfileWebPageURL, sub.SupportURL)
}

// detectProviderType определяет тип провайдера по заголовкам и URL.
// Порядок: специфичные маркеры → generic fallback.
func detectProviderType(h http.Header, webPageURL, supportURL string) string {
	// Remnawave: отдаёт x-remnawave заголовок или uptime-kuma-style URL
	for _, key := range []string{"x-remnawave-version", "x-remnawave", "remnawave-version"} {
		if h.Get(key) != "" {
			return "remnawave"
		}
	}
	if containsAny(webPageURL+supportURL, "remnawave") {
		return "remnawave"
	}

	// Marzban: x-marzban или URL с marzban
	for _, key := range []string{"x-marzban-version", "x-marzban"} {
		if h.Get(key) != "" {
			return "marzban"
		}
	}
	if containsAny(webPageURL+supportURL, "marzban") {
		return "marzban"
	}

	// 3X-UI / X-UI
	if containsAny(webPageURL+supportURL, "3x-ui", "x-ui", "3xui", "xui") {
		return "3x-ui"
	}
	if h.Get("x-xui") != "" || h.Get("x-3xui") != "" {
		return "3x-ui"
	}

	// Custom — у провайдера есть кастомные данные, но тип неизвестен
	return "custom"
}

func containsAny(s string, subs ...string) bool {
	s = strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// countMihomoRules counts the number of rules in a Mihomo config.
// It finds the "rules:" section and counts lines that start with "-" or "  -" inside it.
func countMihomoRules(content string) int {
	lines := strings.Split(content, "\n")
	inRulesSection := false
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if inRulesSection {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, "-") && strings.Contains(line, ":") {
				if !strings.HasPrefix(trimmed, "rules:") {
					inRulesSection = false
				}
			}
		}

		if strings.HasPrefix(trimmed, "rules:") {
			inRulesSection = true
			continue
		}

		if inRulesSection {
			if strings.HasPrefix(trimmed, "-") {
				count++
			}
		}
	}
	return count
}

func extractServer(ob *Outbound) string {
	if ob.Settings == nil {
		return ""
	}
	// Для vmess / vless
	if vnext, ok := ob.Settings["vnext"].([]interface{}); ok && len(vnext) > 0 {
		if serverMap, ok := vnext[0].(map[string]interface{}); ok {
			address, _ := serverMap["address"].(string)
			var port float64
			if p, ok := serverMap["port"].(float64); ok {
				port = p
			} else if p, ok := serverMap["port"].(int); ok {
				port = float64(p)
			}
			if address != "" && port > 0 {
				return fmt.Sprintf("%s:%d", address, int(port))
			}
		}
	}
	// Для trojan / shadowsocks / hysteria2 / socks / http
	if servers, ok := ob.Settings["servers"].([]interface{}); ok && len(servers) > 0 {
		if serverMap, ok := servers[0].(map[string]interface{}); ok {
			address, _ := serverMap["address"].(string)
			var port float64
			if p, ok := serverMap["port"].(float64); ok {
				port = p
			} else if p, ok := serverMap["port"].(int); ok {
				port = float64(p)
			}
			if address != "" && port > 0 {
				return fmt.Sprintf("%s:%d", address, int(port))
			}
		}
	}
	return ""
}

func parseAnnouncement(body []byte, headers http.Header) string {
	content := string(body)
	content = strings.TrimSpace(content)

	// Декодируем base64 если тело закодировано
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(content)
	}
	if err == nil {
		content = string(decoded)
	}

	lines := strings.Split(content, "\n")
	var announceLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			ann := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			announceLines = append(announceLines, ann)
		} else {
			break
		}
	}

	if len(announceLines) > 0 {
		return strings.Join(announceLines, "\n")
	}

	// Fallback на заголовки
	// Remnawave/VK-proxy использует "Announce: base64:..." — проверяем первым.
	if ann := headers.Get("Announce"); ann != "" {
		ann = strings.TrimPrefix(ann, "base64:")
		if dec, err := base64.StdEncoding.DecodeString(ann); err == nil {
			return strings.TrimSpace(string(dec))
		}
		if dec, err := base64.URLEncoding.DecodeString(ann); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(ann)
	}
	if ann := headers.Get("subscription-announce"); ann != "" {
		if dec, err := base64.StdEncoding.DecodeString(ann); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(ann)
	}
	if desc := headers.Get("profile-description"); desc != "" {
		if dec, err := base64.StdEncoding.DecodeString(desc); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(desc)
	}
	if st := headers.Get("support-text"); st != "" {
		if dec, err := base64.StdEncoding.DecodeString(st); err == nil {
			return strings.TrimSpace(string(dec))
		}
		return strings.TrimSpace(st)
	}

	return ""
}
