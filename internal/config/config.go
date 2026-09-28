package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// DefaultPanelPort и DefaultLoopbackPort — дефолтные HTTP/HTTPS-порты панели.
// Именованные константы вместо литералов там, где панель ссылается на свой
// собственный дефолтный адрес (например, в mihomo proxy-provider блоке
// подписки, subscription.go), чтобы рассинхронизация с Default() ниже была
// невозможна при смене дефолтного порта (IN-02 из код-ревью фазы 133).
const (
	DefaultPanelPort    = 8090
	DefaultLoopbackPort = 8091
)

// Диапазоны и дефолты TTL сессии (D-05, D-06). Диапазоны — решение
// планировщика (Claude's Discretion, 134-RESEARCH.md A5): idle 1ч..30д
// (720ч), absolute 1..365 дней.
const (
	DefaultSessionIdleTTLHours    = 24
	DefaultSessionAbsoluteTTLDays = 30
	MinSessionIdleTTLHours        = 1
	MaxSessionIdleTTLHours        = 720
	MinSessionAbsoluteTTLDays     = 1
	MaxSessionAbsoluteTTLDays     = 365
)

// Config represents the main application configuration structure.
type Config struct {
	Port            int         `json:"port"`
	LoopbackPort    int         `json:"loopback_port"`
	XRayConfigDir   string      `json:"xray_config_dir"`
	XRayAPIPort     int         `json:"xray_api_port"`
	XKeenBinary     string      `json:"xkeen_binary"`
	MihomoConfigDir string      `json:"mihomo_config_dir"`
	MihomoBinary    string      `json:"mihomo_binary"`
	XrayBinary      string      `json:"xray_binary"`
	MihomoAPIURL    string      `json:"mihomo_api_url"`
	AllowedRoots    []string    `json:"allowed_roots"`
	LogLevel        string      `json:"log_level"`
	LogPath         string      `json:"log_path"`
	XCPLogPath      string      `json:"xcp_log_path"`
	LogSources      []string    `json:"log_sources"`
	DataDir         string      `json:"data_dir"`
	Auth            AuthConfig  `json:"auth"`
	HTTPS           HTTPSConfig `json:"https"`
	MihomoSecret    string      `json:"mihomo_secret"`
	UpdateChannel   string      `json:"update_channel"` // stable, beta, dev
	// Фоновая проверка обновлений (уведомления) и автоустановка в окно времени
	UpdateAutoCheck     bool   `json:"update_auto_check"`
	UpdateAutoInstall   bool   `json:"update_auto_install"`
	UpdateInstallWindow string `json:"update_install_window"` // "HH:MM-HH:MM", местное время роутера
	DevMode             bool   `json:"dev_mode"`
	ConfigPath          string `json:"-"`

	// NeedsSave и Migrations — служебные поля, не сериализуются. Load()
	// выставляет их, когда конфиг на диске содержит устаревшие ключи
	// (session_timeout_hours, secure_cookie) или значения вне допустимого
	// диапазона; main.go пишет Migrations в xcp.log и пересохраняет конфиг
	// при NeedsSave (лог-файл на момент самого Load() ещё не настроен).
	NeedsSave  bool     `json:"-"`
	Migrations []string `json:"-"`
}

// AuthConfig represents the configuration settings for authentication and session management.
type AuthConfig struct {
	PasswordHash           string `json:"password_hash"`
	SessionIdleTTLHours    int    `json:"session_idle_ttl_hours"`
	SessionAbsoluteTTLDays int    `json:"session_absolute_ttl_days"`
	MaxLoginAttempts       int    `json:"max_login_attempts"`
	LockoutDuration        int    `json:"lockout_duration_minutes"`
}

// HTTPSConfig represents the settings for enabling/configuring HTTPS on the control panel.
type HTTPSConfig struct {
	// Enabled принудительно true после Load() (D-12, 134-06): панель
	// работает только по HTTPS, флаг из файла больше не отключает TLS.
	// Поле оставлено в конфиге и структуре: старый бинарник при откате,
	// прочитав "enabled": true из уже смигрированного файла, тоже
	// останется на HTTPS.
	Enabled  bool   `json:"enabled"`
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

func findXKeen() string {
	paths := []string{
		"/opt/sbin/xkeen",
		"/opt/bin/xkeen",
		"/usr/local/bin/xkeen",
		"/usr/bin/xkeen",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Try which
	if path, err := exec.LookPath("xkeen"); err == nil {
		return path
	}
	return "/opt/sbin/xkeen" // fallback
}

// Default returns the default configuration for the application.
func Default() *Config {
	return &Config{
		Port:            DefaultPanelPort,
		LoopbackPort:    DefaultLoopbackPort,
		XRayConfigDir:   "/opt/etc/xray/configs",
		XRayAPIPort:     10085,
		XKeenBinary:     findXKeen(),
		MihomoConfigDir: "/opt/etc/mihomo",
		MihomoBinary:    "/opt/sbin/mihomo",
		XrayBinary:      "/opt/sbin/xray",
		MihomoAPIURL:    "http://127.0.0.1:9090",
		DataDir:         "/opt/etc/xcp",
		LogLevel:        "info",
		LogPath:         "/opt/var/log/xkeen.log",
		XCPLogPath:      "/opt/var/log/xcp.log",
		LogSources:      []string{"/opt/var/log/xkeen.log", "/opt/var/log/xcp.log"},
		AllowedRoots: []string{
			"/opt/etc/xray",
			"/opt/etc/xkeen",
			"/opt/etc/mihomo",
			"/opt/etc/xcp",
			"/opt/var/log",
			"/opt/sbin",
			"/opt/bin",
		},
		Auth: AuthConfig{
			PasswordHash:           "",
			SessionIdleTTLHours:    DefaultSessionIdleTTLHours,
			SessionAbsoluteTTLDays: DefaultSessionAbsoluteTTLDays,
			MaxLoginAttempts:       5,
			LockoutDuration:        5,
		},
		HTTPS: HTTPSConfig{
			Enabled:  true,
			CertPath: "",
			KeyPath:  "",
		},
		UpdateChannel:       "stable",
		UpdateAutoCheck:     true,
		UpdateAutoInstall:   false,
		UpdateInstallWindow: "03:00-05:00",
	}
}

// Load reads and parses the configuration file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.ConfigPath = path

	if cfg.XRayAPIPort == 0 {
		cfg.XRayAPIPort = 10085
	}

	if cfg.XCPLogPath == "" {
		cfg.XCPLogPath = "/opt/var/log/xcp.log"
	}

	if len(cfg.LogSources) == 0 {
		sources := []string{}
		if cfg.LogPath != "" {
			sources = append(sources, cfg.LogPath)
		} else {
			sources = append(sources, "/opt/var/log/xkeen.log")
		}
		sources = append(sources, cfg.XCPLogPath)
		cfg.LogSources = sources
	} else {
		found := false
		for _, s := range cfg.LogSources {
			if s == cfg.XCPLogPath {
				found = true
				break
			}
		}
		if !found {
			cfg.LogSources = append(cfg.LogSources, cfg.XCPLogPath)
		}
	}

	migrateSessionTTL(cfg, data)
	migrateHTTPSEnabled(cfg, data)

	return cfg, nil
}

// validIdleHours/validAbsoluteDays — диапазоны допустимых значений TTL
// сессии (см. константы выше).
func validIdleHours(h int) bool {
	return h >= MinSessionIdleTTLHours && h <= MaxSessionIdleTTLHours
}

func validAbsoluteDays(d int) bool {
	return d >= MinSessionAbsoluteTTLDays && d <= MaxSessionAbsoluteTTLDays
}

// ValidSessionTTL проверяет, что оба TTL сессии (idle-часы, absolute-дни)
// находятся в допустимых диапазонах.
func ValidSessionTTL(idleHours, absDays int) bool {
	return validIdleHours(idleHours) && validAbsoluteDays(absDays)
}

// authTTLProbe читает сырой JSON конфига отдельно от основного Unmarshal —
// нужно различить «ключа нет в файле» (nil-указатель) от «ключ есть и равен
// нулю», что обычный Unmarshal в cfg такого различия не даёт (отсутствующее
// поле и нулевое значение неразличимы после заполнения из Default()).
type authTTLProbe struct {
	Auth struct {
		IdleHours            *int `json:"session_idle_ttl_hours"`
		AbsoluteDays         *int `json:"session_absolute_ttl_days"`
		LegacySessionTimeout *int `json:"session_timeout_hours"`
	} `json:"auth"`
}

// migrateSessionTTL реализует D-06: legacy session_timeout_hours переносится
// в session_idle_ttl_hours и удаляется из файла вместе с secure_cookie;
// значения TTL вне допустимого диапазона заменяются дефолтами. Не логирует
// сама (лог-файл на этом этапе main() ещё не настроен) — только копит
// cfg.Migrations и выставляет cfg.NeedsSave, чтобы main.go записал строки в
// xcp.log и пересохранил конфиг уже после настройки логирования.
func migrateSessionTTL(cfg *Config, data []byte) {
	var probe authTTLProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		return
	}

	if probe.Auth.IdleHours == nil {
		if probe.Auth.LegacySessionTimeout != nil && validIdleHours(*probe.Auth.LegacySessionTimeout) {
			cfg.Auth.SessionIdleTTLHours = *probe.Auth.LegacySessionTimeout
			cfg.NeedsSave = true
			cfg.Migrations = append(cfg.Migrations, fmt.Sprintf(
				"auth.session_timeout_hours=%d migrated to auth.session_idle_ttl_hours",
				*probe.Auth.LegacySessionTimeout))
		}
		// Иначе новый ключ отсутствует, а legacy нет или вне диапазона —
		// в cfg.Auth.SessionIdleTTLHours уже лежит дефолт из Default().
	}

	if probe.Auth.LegacySessionTimeout != nil {
		cfg.NeedsSave = true // legacy-ключ должен исчезнуть из файла при Save
	}
	// secure_cookie не пробится отдельно: поле убрано из AuthConfig целиком,
	// поэтому Save() уже не пишет его — переписывать файл специально ради
	// этого ключа не нужно (в отличие от session_timeout_hours, чьё СТАРОЕ
	// значение нужно было прочитать перед тем, как оно перестанет
	// парситься).

	if !validIdleHours(cfg.Auth.SessionIdleTTLHours) {
		cfg.Migrations = append(cfg.Migrations, fmt.Sprintf(
			"auth.session_idle_ttl_hours=%d out of range, reset to default %d",
			cfg.Auth.SessionIdleTTLHours, DefaultSessionIdleTTLHours))
		cfg.Auth.SessionIdleTTLHours = DefaultSessionIdleTTLHours
		cfg.NeedsSave = true
	}
	if !validAbsoluteDays(cfg.Auth.SessionAbsoluteTTLDays) {
		cfg.Migrations = append(cfg.Migrations, fmt.Sprintf(
			"auth.session_absolute_ttl_days=%d out of range, reset to default %d",
			cfg.Auth.SessionAbsoluteTTLDays, DefaultSessionAbsoluteTTLDays))
		cfg.Auth.SessionAbsoluteTTLDays = DefaultSessionAbsoluteTTLDays
		cfg.NeedsSave = true
	}
}

// httpsProbe читает сырой JSON конфига отдельно от основного Unmarshal, тем
// же приёмом, что и authTTLProbe: указатель различает «ключа нет в файле» от
// «ключ есть и равен false» — обычный Unmarshal в cfg этого не различает,
// потому что Default() уже заполнил поле значением true.
type httpsProbe struct {
	HTTPS struct {
		Enabled *bool `json:"enabled"`
	} `json:"https"`
}

// migrateHTTPSEnabled реализует D-12: панель работает только по HTTPS,
// флаг https.enabled из файла больше не может её отключить.
// "enabled": false в файле мигрирует в true с записью в cfg.Migrations и
// cfg.NeedsSave (лог-файл на этом этапе main() ещё не настроен — как и
// migrateSessionTTL, эта функция не логирует сама). После неё
// cfg.HTTPS.Enabled всегда true, независимо от того, что было в файле или в
// probe.
func migrateHTTPSEnabled(cfg *Config, data []byte) {
	var probe httpsProbe
	if err := json.Unmarshal(data, &probe); err == nil {
		if probe.HTTPS.Enabled != nil && !*probe.HTTPS.Enabled {
			cfg.Migrations = append(cfg.Migrations,
				"https.enabled=false is ignored: panel is HTTPS-only, migrated to true")
			cfg.NeedsSave = true
		}
	}
	cfg.HTTPS.Enabled = true
}

// Save writes the given configuration to the specified path atomically.
func Save(path string, cfg *Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0755)
	return utils.AtomicWriteFile(path, data, 0600)
}

// SavePasswordHash updates the password hash in the configuration and saves it to the specified path.
func (c *Config) SavePasswordHash(path string, hash string) error {
	c.Auth.PasswordHash = hash
	return Save(path, c)
}
