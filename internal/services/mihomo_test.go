package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMihomoService_New(t *testing.T) {
	svc := NewMihomoService("/opt/bin/mihomo", "/opt/etc/mihomo")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.BinaryPath != "/opt/bin/mihomo" {
		t.Fatalf("expected BinaryPath '/opt/bin/mihomo', got %s", svc.BinaryPath)
	}
	if svc.ConfigDir != "/opt/etc/mihomo" {
		t.Fatalf("expected ConfigDir '/opt/etc/mihomo', got %s", svc.ConfigDir)
	}
}

func TestMihomoService_Status_Stopped(t *testing.T) {
	svc := NewMihomoService("/nonexistent/binary", "/nonexistent/dir")

	// Create dummy pidof that returns empty string
	tmpDir := t.TempDir()
	pidofPath := filepath.Join(tmpDir, "pidof")
	os.WriteFile(pidofPath, []byte("#!/bin/sh\nexit 1\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status != "stopped" {
		t.Fatalf("expected 'stopped', got %s", status)
	}
}

func TestMihomoService_Status_Running(t *testing.T) {
	svc := NewMihomoService("/opt/bin/mihomo", "/opt/etc/mihomo")

	// Create dummy pidof that returns a pid
	tmpDir := t.TempDir()
	pidofPath := filepath.Join(tmpDir, "pidof")
	os.WriteFile(pidofPath, []byte("#!/bin/sh\necho \"12345\"\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !strings.Contains(status, "running (pid: 12345)") {
		t.Fatalf("expected 'running', got %s", status)
	}
}

func TestMihomoService_Start(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "mihomo")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Started\"\n"), 0755)

	svc := NewMihomoService(dummy, "/opt/etc/mihomo")
	out, err := svc.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Started") {
		t.Fatalf("expected Started, got %s", out)
	}
}

func TestMihomoService_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "mihomo")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Stopped\"\n"), 0755)

	svc := NewMihomoService(dummy, "/opt/etc/mihomo")
	out, err := svc.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Stopped") {
		t.Fatalf("expected Stopped, got %s", out)
	}
}

func TestMihomoService_Restart(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "mihomo")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Restarted\"\n"), 0755)

	svc := NewMihomoService(dummy, "/opt/etc/mihomo")
	out, err := svc.Restart()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Restarted") {
		t.Fatalf("expected Restarted, got %s", out)
	}
}
