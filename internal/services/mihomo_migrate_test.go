package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMihomoService_GetMigrationPreview(t *testing.T) {
	t.Run("insecure TCP 0.0.0.0 config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configYAML := `
port: 7890
external-controller: 0.0.0.0:9090
secret: "test-secret"
`
		if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
			t.Fatal(err)
		}

		svc := NewMihomoService("", "", tmpDir)
		preview, err := svc.GetMigrationPreview()
		if err != nil {
			t.Fatalf("GetMigrationPreview failed: %v", err)
		}
		if preview.AlreadyMigrated {
			t.Errorf("AlreadyMigrated = true, want false")
		}
		if !preview.IsInsecure {
			t.Errorf("IsInsecure = false, want true")
		}
		if preview.CurrentType != "tcp" {
			t.Errorf("CurrentType = %q, want 'tcp'", preview.CurrentType)
		}
		if preview.CurrentController != "0.0.0.0:9090" {
			t.Errorf("CurrentController = %q, want '0.0.0.0:9090'", preview.CurrentController)
		}
		if preview.TargetSocket != DefaultMihomoSocketPath {
			t.Errorf("TargetSocket = %q, want %q", preview.TargetSocket, DefaultMihomoSocketPath)
		}
		if !strings.Contains(preview.DiffOld, "external-controller: 0.0.0.0:9090") {
			t.Errorf("DiffOld does not contain expected line: %q", preview.DiffOld)
		}
		if !strings.Contains(preview.DiffNew, "external-controller-unix: /opt/var/run/mihomo.sock") {
			t.Errorf("DiffNew does not contain expected line: %q", preview.DiffNew)
		}
	})

	t.Run("already migrated unix socket config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configYAML := `
port: 7890
external-controller-unix: /opt/var/run/mihomo.sock
secret: "test-secret"
`
		if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
			t.Fatal(err)
		}

		svc := NewMihomoService("", "", tmpDir)
		preview, err := svc.GetMigrationPreview()
		if err != nil {
			t.Fatalf("GetMigrationPreview failed: %v", err)
		}
		if !preview.AlreadyMigrated {
			t.Errorf("AlreadyMigrated = false, want true")
		}
		if preview.IsInsecure {
			t.Errorf("IsInsecure = true, want false")
		}
		if preview.CurrentType != "unix" {
			t.Errorf("CurrentType = %q, want 'unix'", preview.CurrentType)
		}
	})
}

func TestMihomoService_MigrateToSocket_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configYAML := `port: 7890
external-controller: 0.0.0.0:9090
secret: "test-secret"
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewMihomoService("", "", tmpDir)
	res, err := svc.MigrateToSocket(nil)
	if err != nil {
		t.Fatalf("MigrateToSocket failed: %v", err)
	}
	if !res.Success {
		t.Fatalf("MigrateToSocket returned success=false: %s", res.Error)
	}
	if res.RolledBack {
		t.Errorf("RolledBack = true, want false")
	}
	if res.SocketPath != DefaultMihomoSocketPath {
		t.Errorf("SocketPath = %q, want %q", res.SocketPath, DefaultMihomoSocketPath)
	}

	// Verify backup file exists
	if res.BackupPath == "" {
		t.Errorf("BackupPath is empty")
	}
	fullBackup := filepath.Join(tmpDir, res.BackupPath)
	if _, err := os.Stat(fullBackup); err != nil {
		t.Errorf("Backup file %s does not exist", fullBackup)
	}

	// Verify config.yaml was updated
	newContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	strContent := string(newContent)
	if !strings.Contains(strContent, "external-controller-unix: /opt/var/run/mihomo.sock") {
		t.Errorf("config.yaml does not contain unix socket directive: %s", strContent)
	}
	if strings.Contains(strContent, "external-controller: 0.0.0.0:9090") {
		t.Errorf("config.yaml still contains old tcp controller directive: %s", strContent)
	}
}

func TestMihomoService_MigrateToSocket_RollbackOnValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	origContent := `port: 7890
external-controller: 0.0.0.0:9090
secret: "test-secret"
`
	if err := os.WriteFile(configPath, []byte(origContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create dummy failing binary
	dummyBin := filepath.Join(tmpDir, "mihomo-mock")
	if err := os.WriteFile(dummyBin, []byte("#!/bin/sh\necho 'syntax error on line 42' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewMihomoService(dummyBin, "", tmpDir)
	res, err := svc.MigrateToSocket(nil)
	if err != nil {
		t.Fatalf("MigrateToSocket unexpected error: %v", err)
	}
	if res.Success {
		t.Fatalf("MigrateToSocket expected failure, got success")
	}
	if !res.RolledBack {
		t.Errorf("RolledBack = false, want true")
	}
	if !strings.Contains(res.Error, "syntax error") {
		t.Errorf("Error does not contain failure reason: %s", res.Error)
	}

	// Verify config.yaml was rolled back to original
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != origContent {
		t.Errorf("config.yaml was not restored properly. Got:\n%s\nWant:\n%s", string(restored), origContent)
	}
}
