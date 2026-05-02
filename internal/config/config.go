package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Port            int        `json:"port"`
	XRayConfigDir   string     `json:"xray_config_dir"`
	XKeenBinary     string     `json:"xkeen_binary"`
	MihomoConfigDir string     `json:"mihomo_config_dir"`
	MihomoBinary    string     `json:"mihomo_binary"`
	AllowedRoots    []string   `json:"allowed_roots"`
	LogLevel        string     `json:"log_level"`
	DataDir         string     `json:"data_dir"`
	Auth            AuthConfig `json:"auth"`
}

type AuthConfig struct {
	PasswordHash     string `json:"password_hash"`
	SessionTimeout   int    `json:"session_timeout_hours"`
	MaxLoginAttempts int    `json:"max_login_attempts"`
	LockoutDuration  int    `json:"lockout_duration_minutes"`
}

func Default() *Config {
	return &Config{
		Port:            8089,
		XRayConfigDir:   "/opt/etc/xray/configs",
		XKeenBinary:     "xkeen",
		MihomoConfigDir: "/opt/etc/mihomo",
		MihomoBinary:    "mihomo",
		DataDir:         "/opt/etc/xkeen-control-panel",
		LogLevel:        "info",
		AllowedRoots: []string{
			"/opt/etc/xray",
			"/opt/etc/xkeen",
			"/opt/etc/mihomo",
			"/opt/var/log",
		},
		Auth: AuthConfig{
			PasswordHash:     "",
			SessionTimeout:   24,
			MaxLoginAttempts: 5,
			LockoutDuration:  5,
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, data, 0644)
}

func (c *Config) SavePasswordHash(path string, hash string) error {
	c.Auth.PasswordHash = hash
	return Save(path, c)
}
