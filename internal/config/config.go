package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Config represents the main application configuration structure.
type Config struct {
	Port            int         `json:"port"`
	XRayConfigDir   string      `json:"xray_config_dir"`
	XKeenBinary     string      `json:"xkeen_binary"`
	MihomoConfigDir string      `json:"mihomo_config_dir"`
	MihomoBinary    string      `json:"mihomo_binary"`
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
	UpdateChannel    string      `json:"update_channel"` // stable, beta, dev
	TemplatesRepoURL string      `json:"templates_repo_url"`
	DevMode          bool        `json:"dev_mode"`
	ConfigPath       string      `json:"-"`
}

// AuthConfig represents the configuration settings for authentication and session management.
type AuthConfig struct {
	PasswordHash     string `json:"password_hash"`
	SessionTimeout   int    `json:"session_timeout_hours"`
	MaxLoginAttempts int    `json:"max_login_attempts"`
	LockoutDuration  int    `json:"lockout_duration_minutes"`
	SecureCookie     bool   `json:"secure_cookie"`
}

// HTTPSConfig represents the settings for enabling/configuring HTTPS on the control panel.
type HTTPSConfig struct {
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
		Port:            8090,
		XRayConfigDir:   "/opt/etc/xray/configs",
		XKeenBinary:     findXKeen(),
		MihomoConfigDir: "/opt/etc/mihomo",
		MihomoBinary:    "/opt/sbin/mihomo",
		MihomoAPIURL:    "http://localhost:9090",
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
			PasswordHash:     "",
			SessionTimeout:   24,
			MaxLoginAttempts: 5,
			LockoutDuration:  5,
			SecureCookie:     true,
		},
		HTTPS: HTTPSConfig{
			Enabled:  true,
			CertPath: "",
			KeyPath:  "",
		},
		UpdateChannel:    "stable",
		TemplatesRepoURL: "https://raw.githubusercontent.com/shisui1511/xkeen-control-panel-templates/main",
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
	return cfg, nil
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
