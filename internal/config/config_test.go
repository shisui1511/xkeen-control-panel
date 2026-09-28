package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Port != 8090 {
		t.Errorf("Expected port 8090, got %d", cfg.Port)
	}

	if cfg.XRayConfigDir != "/opt/etc/xray/configs" {
		t.Errorf("Expected XRayConfigDir /opt/etc/xray/configs, got %s", cfg.XRayConfigDir)
	}

	if cfg.Auth.SessionIdleTTLHours != DefaultSessionIdleTTLHours {
		t.Errorf("Expected SessionIdleTTLHours %d, got %d", DefaultSessionIdleTTLHours, cfg.Auth.SessionIdleTTLHours)
	}
	if cfg.Auth.SessionAbsoluteTTLDays != DefaultSessionAbsoluteTTLDays {
		t.Errorf("Expected SessionAbsoluteTTLDays %d, got %d", DefaultSessionAbsoluteTTLDays, cfg.Auth.SessionAbsoluteTTLDays)
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Создаём временную директорию
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Создаём конфиг
	cfg := Default()
	cfg.Port = 9999
	cfg.Auth.PasswordHash = "test-hash"

	// Сохраняем
	err := Save(configPath, cfg)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Проверяем, что файл создан
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Загружаем
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Проверяем значения
	if loadedCfg.Port != 9999 {
		t.Errorf("Expected port 9999, got %d", loadedCfg.Port)
	}

	if loadedCfg.Auth.PasswordHash != "test-hash" {
		t.Errorf("Expected password hash 'test-hash', got %s", loadedCfg.Auth.PasswordHash)
	}
}

func TestSavePasswordHash(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := Default()

	// Сохраняем с новым хешем пароля
	err := cfg.SavePasswordHash(configPath, "new-hash")
	if err != nil {
		t.Fatalf("Failed to save password hash: %v", err)
	}

	// Загружаем и проверяем
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if loadedCfg.Auth.PasswordHash != "new-hash" {
		t.Errorf("Expected password hash 'new-hash', got %s", loadedCfg.Auth.PasswordHash)
	}
}

func TestLoadNonExistent(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error when loading non-existent config")
	}
}

// TestConfigSave_FilePermissions verifies that the config file is saved with
// restricted permissions (0600) so that other users cannot read the password hash.
func TestConfigSave_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := Default()
	cfg.Auth.PasswordHash = "secret-hash"

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	got := info.Mode().Perm()
	want := os.FileMode(0600)
	if got != want {
		t.Errorf("file permissions: got %04o, want %04o", got, want)
	}
}

// TestLoad_MigratesLegacySessionTimeout: legacy session_timeout_hours без
// нового ключа мигрирует в session_idle_ttl_hours, помечает NeedsSave и
// оставляет след в Migrations; после Save() legacy-ключ не пишется обратно.
func TestLoad_MigratesLegacySessionTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	raw := `{"auth":{"password_hash":"h","session_timeout_hours":12,"max_login_attempts":5,"lockout_duration_minutes":5}}`
	if err := os.WriteFile(configPath, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Auth.SessionIdleTTLHours != 12 {
		t.Errorf("expected migrated idle TTL 12, got %d", cfg.Auth.SessionIdleTTLHours)
	}
	if !cfg.NeedsSave {
		t.Error("expected NeedsSave=true after legacy key migration")
	}
	found := false
	for _, m := range cfg.Migrations {
		if strings.Contains(m, "session_timeout_hours") && strings.Contains(m, "session_idle_ttl_hours") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a migration entry mentioning both keys, got %v", cfg.Migrations)
	}

	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "session_timeout_hours") {
		t.Error("saved config must not contain the legacy session_timeout_hours key")
	}
	if strings.Contains(content, "secure_cookie") {
		t.Error("saved config must not contain the removed secure_cookie key")
	}
}

// TestLoad_DefaultSessionTTL: конфиг без auth-ключей вообще получает
// дефолтные idle/absolute TTL.
func TestLoad_DefaultSessionTTL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Auth.SessionIdleTTLHours != DefaultSessionIdleTTLHours {
		t.Errorf("expected default idle TTL %d, got %d", DefaultSessionIdleTTLHours, cfg.Auth.SessionIdleTTLHours)
	}
	if cfg.Auth.SessionAbsoluteTTLDays != DefaultSessionAbsoluteTTLDays {
		t.Errorf("expected default absolute TTL %d, got %d", DefaultSessionAbsoluteTTLDays, cfg.Auth.SessionAbsoluteTTLDays)
	}
}

// TestLoad_ClampsOutOfRangeSessionTTL: значения TTL вне допустимого
// диапазона заменяются дефолтами и помечают NeedsSave.
func TestLoad_ClampsOutOfRangeSessionTTL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	raw := `{"auth":{"session_idle_ttl_hours":0,"session_absolute_ttl_days":400}}`
	if err := os.WriteFile(configPath, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Auth.SessionIdleTTLHours != DefaultSessionIdleTTLHours {
		t.Errorf("expected idle TTL clamped to default %d, got %d", DefaultSessionIdleTTLHours, cfg.Auth.SessionIdleTTLHours)
	}
	if cfg.Auth.SessionAbsoluteTTLDays != DefaultSessionAbsoluteTTLDays {
		t.Errorf("expected absolute TTL clamped to default %d, got %d", DefaultSessionAbsoluteTTLDays, cfg.Auth.SessionAbsoluteTTLDays)
	}
	if !cfg.NeedsSave {
		t.Error("expected NeedsSave=true after clamping out-of-range TTL values")
	}
}

// TestSave_DropsLegacyAuthKeys: Save() никогда не пишет удалённые ключи
// session_timeout_hours/secure_cookie — они просто не существуют в структуре.
func TestSave_DropsLegacyAuthKeys(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := Default()
	if err := Save(configPath, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, "session_timeout_hours") {
		t.Error("saved config must not contain session_timeout_hours")
	}
	if strings.Contains(content, "secure_cookie") {
		t.Error("saved config must not contain secure_cookie")
	}
	if !strings.Contains(content, "session_idle_ttl_hours") {
		t.Error("saved config must contain session_idle_ttl_hours")
	}
}

// TestLoad_ForcesHTTPSEnabled реализует D-12: https.enabled из файла больше
// не может отключить HTTPS — Load() всегда переводит его в true, а
// "enabled": false в файле мигрирует с записью в Migrations/NeedsSave.
func TestLoad_ForcesHTTPSEnabled(t *testing.T) {
	t.Run("enabled=false in file migrates to true", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")

		raw := `{"https":{"enabled":false,"cert_path":"/opt/etc/xcp/my.pem","key_path":"/opt/etc/xcp/my.key"}}`
		if err := os.WriteFile(configPath, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if !cfg.HTTPS.Enabled {
			t.Error("expected HTTPS.Enabled=true after forced migration")
		}
		if !cfg.NeedsSave {
			t.Error("expected NeedsSave=true after https.enabled=false migration")
		}
		found := false
		for _, m := range cfg.Migrations {
			if strings.Contains(m, "https.enabled") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a migration entry mentioning https.enabled, got %v", cfg.Migrations)
		}
		// cert_path/key_path сохраняются без изменений.
		if cfg.HTTPS.CertPath != "/opt/etc/xcp/my.pem" {
			t.Errorf("expected cert_path preserved, got %q", cfg.HTTPS.CertPath)
		}
		if cfg.HTTPS.KeyPath != "/opt/etc/xcp/my.key" {
			t.Errorf("expected key_path preserved, got %q", cfg.HTTPS.KeyPath)
		}

		if err := Save(configPath, cfg); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"enabled": true`) {
			t.Errorf("expected saved config to contain enabled: true, got: %s", data)
		}
	})

	t.Run("no https section in file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.json")

		if err := os.WriteFile(configPath, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		if !cfg.HTTPS.Enabled {
			t.Error("expected HTTPS.Enabled=true by default")
		}
		for _, m := range cfg.Migrations {
			if strings.Contains(m, "https.enabled") {
				t.Errorf("expected no https.enabled migration entry when key is absent, got %v", cfg.Migrations)
			}
		}
	})
}
