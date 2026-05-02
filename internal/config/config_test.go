package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Port != 8089 {
		t.Errorf("Expected port 8089, got %d", cfg.Port)
	}

	if cfg.XRayConfigDir != "/opt/etc/xray/configs" {
		t.Errorf("Expected XRayConfigDir /opt/etc/xray/configs, got %s", cfg.XRayConfigDir)
	}

	if cfg.Auth.SessionTimeout != 24 {
		t.Errorf("Expected SessionTimeout 24, got %d", cfg.Auth.SessionTimeout)
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
