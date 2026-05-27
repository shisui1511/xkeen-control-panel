package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupMihomoConfig_CreatesBackup(t *testing.T) {
	dataDir := t.TempDir()
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	content := "port: 7890\nproxies: []\n"
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := backupMihomoConfig(dataDir, configPath); err != nil {
		t.Fatalf("backupMihomoConfig: %v", err)
	}

	backupDir := filepath.Join(dataDir, "backup", "mihomo")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Name(), "config.yaml.") {
		t.Errorf("backup filename should start with config.yaml., got %q", entries[0].Name())
	}

	// Verify содержимое идентично.
	backupContent, err := os.ReadFile(filepath.Join(backupDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if string(backupContent) != content {
		t.Errorf("backup content mismatch:\nwant: %q\ngot:  %q", content, backupContent)
	}
}

func TestBackupMihomoConfig_NoConfig(t *testing.T) {
	dataDir := t.TempDir()
	// Файл не существует.
	if err := backupMihomoConfig(dataDir, "/nonexistent/config.yaml"); err != nil {
		t.Errorf("missing config should not be an error: %v", err)
	}
	// Backup dir не создаётся (нечего бэкапить).
	if _, err := os.Stat(filepath.Join(dataDir, "backup", "mihomo")); !os.IsNotExist(err) {
		t.Errorf("backup dir should not exist when no config: err=%v", err)
	}
}

func TestPruneMihomoConfigBackups_KeepsLastN(t *testing.T) {
	backupDir := t.TempDir()

	// Создаём 8 бэкапов с разными timestamps.
	for i := 1; i <= 8; i++ {
		name := fmt.Sprintf("config.yaml.%d", 1000000000000+int64(i))
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Дополнительный файл, который не должен трогаться.
	if err := os.WriteFile(filepath.Join(backupDir, "README.md"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	pruneMihomoConfigBackups(backupDir, 5)

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}

	// Должно остаться 5 backups + 1 README.
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.yaml.") {
			count++
		}
	}
	if count != 5 {
		t.Errorf("expected 5 backups remaining, got %d", count)
	}

	// README не должен быть удалён.
	if _, err := os.Stat(filepath.Join(backupDir, "README.md")); err != nil {
		t.Error("README.md should not be deleted")
	}

	// Самые старые (1..3) должны быть удалены, 4..8 — оставлены.
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("config.yaml.%d", 1000000000000+int64(i))
		if _, err := os.Stat(filepath.Join(backupDir, name)); !os.IsNotExist(err) {
			t.Errorf("old backup %s should be deleted", name)
		}
	}
	for i := 4; i <= 8; i++ {
		name := fmt.Sprintf("config.yaml.%d", 1000000000000+int64(i))
		if _, err := os.Stat(filepath.Join(backupDir, name)); err != nil {
			t.Errorf("recent backup %s should be kept: %v", name, err)
		}
	}
}
