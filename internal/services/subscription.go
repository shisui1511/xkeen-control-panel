package services

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// maxSubscriptionBytes caps the download size to 10 MB
const maxSubscriptionBytes = 10 * 1024 * 1024

// invalidIDCharsRe — символы, недопустимые в ID подписки (path injection).
var invalidIDCharsRe = regexp.MustCompile(`[^a-z0-9_-]`)

var (
	nonAlphanumericDashRe = regexp.MustCompile(`[^a-zA-Z0-9-]`)
	multiDashRe           = regexp.MustCompile(`-+`)
	allowedXrayProtocols  = map[string]bool{
		"vless":       true,
		"vmess":       true,
		"trojan":      true,
		"shadowsocks": true,
		"socks":       true,
		"http":        true,
	}
)

// SubscriptionNode представляет метаданные отдельного узла подписки.
type SubscriptionNode struct {
	Tag          string `json:"tag"`                 // Уникальный тег XRay (sub-N-K)
	Name         string `json:"name"`                // Чистое имя без флагов и мусора
	Country      string `json:"country,omitempty"`   // ISO-код страны (например, RU, DE)
	Flag         string `json:"flag,omitempty"`      // Эмодзи флаг (например, 🇷🇺)
	UseCase      string `json:"use_case,omitempty"`  // Область применения (например, "Youtube, Instagram")
	Speed        string `json:"speed,omitempty"`     // Скорость (например, "1Gb/s")
	IsNew        bool   `json:"is_new,omitempty"`    // Флаг новизны
	Protocol     string `json:"protocol"`            // Протокол (vless, vmess, trojan, shadowsocks)
	Transport    string `json:"transport,omitempty"` // Транспорт (ws, grpc, httpupgrade, xhttp, tcp)
	Security     string `json:"security,omitempty"`  // Безопасность (tls, reality, none)
	Server       string `json:"server,omitempty"`    // Адрес сервера (хост:порт)
	Active       bool   `json:"active,omitempty"`    // Выбран ли узел активным
	UUID         string `json:"uuid,omitempty"`
	Password     string `json:"password,omitempty"`
	Flow         string `json:"flow,omitempty"`
	PublicKey    string `json:"public_key,omitempty"`
	ShortID      string `json:"short_id,omitempty"`
	ServerName   string `json:"servername,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	WSPath       string `json:"ws_path,omitempty"`
	Cipher       string `json:"cipher,omitempty"`
	SNI          string `json:"sni,omitempty"`
	Congestion   string `json:"congestion,omitempty"`
	AlterID      int    `json:"alter_id,omitempty"`
	Insecure     bool   `json:"insecure,omitempty"`
	ObfsType     string `json:"obfs_type,omitempty"`
	ObfsPassword string `json:"obfs_password,omitempty"`
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

	EnableXray   bool `json:"enable_xray"`
	EnableMihomo bool `json:"enable_mihomo"`

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
	ProfileTitle        string `json:"profile_title,omitempty"`         // имя из header profile-title (base64)
	ProfileUpdateHours  int    `json:"profile_update_hours,omitempty"`  // из header profile-update-interval
	SupportURL          string `json:"support_url,omitempty"`           // из header support-url
	ProfileWebPageURL   string `json:"profile_web_page_url,omitempty"`  // из header profile-web-page-url
	Expire              int64  `json:"expire,omitempty"`                // unix ts окончания подписки
	UseProviderInterval bool   `json:"use_provider_interval,omitempty"` // использовать ли интервал провайдера

	// Mihomo state tracking — для in-place правки config.yaml.
	ProxyNames     []string `json:"proxy_names,omitempty"`
	ManagedYAML    string   `json:"managed_yaml,omitempty"`
	LastCount      int      `json:"last_count,omitempty"`
	LastSkipped    int      `json:"last_skipped,omitempty"`
	LastHash       string   `json:"last_hash,omitempty"`
	LastHashMihomo string   `json:"last_hash_mihomo,omitempty"`
	LastChanged    bool     `json:"last_changed,omitempty"`
	MihomoGroups   []string `json:"mihomo_groups,omitempty"`

	Nodes        []SubscriptionNode `json:"nodes,omitempty"`
	Announcement string             `json:"announcement,omitempty"`

	// HwidToken — device HWID, отправляется как x-hwid header при запросе подписки.
	HwidToken string `json:"hwid_token,omitempty"`
	// HwidLocked — провайдер вернул X-Hwid-Not-Supported: true при последнем refresh.
	HwidLocked bool `json:"hwid_locked,omitempty"`

	// MihomoIntegrated — интегрирована ли подписка в config.yaml Mihomo
	MihomoIntegrated bool `json:"mihomo_integrated"`
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
	deviceInfo      *DeviceInfo    // модель/ОС роутера для x-device-* заголовков (см. task 60-01-05)
	mihomoAPIURL    string
	mihomoSecret    string
	lastCleanup     time.Time
	panelPort       int
	panelHTTPS      bool
}

func NewSubscriptionService(dataDir, configDir, mihomoConfigDir string) *SubscriptionService {
	svc := &SubscriptionService{
		dataDir:         dataDir,
		configDir:       configDir,
		mihomoConfigDir: mihomoConfigDir,
		httpClient:      utils.SafeHTTPClient(30 * time.Second),
		hwid:            loadOrGenerateHWID(dataDir),
		deviceInfo:      NewDeviceInfo(),
	}
	svc.load()
	return svc
}

func (s *SubscriptionService) SetPanelAddress(port int, https bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.panelPort = port
	s.panelHTTPS = https
}

func (s *SubscriptionService) generateMihomoProxyProviderBlock(sub *Subscription) string {
	s.mu.RLock()
	port := s.panelPort
	https := s.panelHTTPS
	s.mu.RUnlock()

	if port == 0 {
		port = 8090
	}

	scheme := "http"
	if https {
		scheme = "https"
	}

	providerName := GetMihomoProviderName(sub.ProfileTitle, sub.Name, sub.URL, sub.ID)
	escapedURL := url.QueryEscape(sub.URL)
	loopbackURL := fmt.Sprintf("%s://127.0.0.1:%d/api/provider.yaml?url=%s", scheme, port, escapedURL)

	intervalSec := sub.Interval * 3600
	if intervalSec <= 0 {
		intervalSec = 24 * 3600 // дефолт 24 часа
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("  %s:\n", providerName))
	sb.WriteString("    type: http\n")
	sb.WriteString(fmt.Sprintf("    url: %q\n", loopbackURL))
	sb.WriteString(fmt.Sprintf("    interval: %d\n", intervalSec))
	sb.WriteString(fmt.Sprintf("    path: ./proxy_providers/%s.yaml\n", providerName))
	if https {
		sb.WriteString("    skip-cert-verify: true\n")
	}
	sb.WriteString("    health-check:\n")
	sb.WriteString("      enable: true\n")
	sb.WriteString("      url: http://www.gstatic.com/generate_204\n")
	sb.WriteString("      interval: 300")

	return sb.String()
}

