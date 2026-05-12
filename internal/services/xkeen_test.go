package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXKeenService_New(t *testing.T) {
	svc := NewXKeenService("/opt/bin/xkeen")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.BinaryPath != "/opt/bin/xkeen" {
		t.Fatalf("expected BinaryPath '/opt/bin/xkeen', got %s", svc.BinaryPath)
	}
}

func TestXKeenService_Status(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Active\"\n"), 0755)

	svc := NewXKeenService(dummy)
	out, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if out != "Active" {
		t.Fatalf("expected Active, got %s", out)
	}
}

func TestXKeenService_Start(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Started\"\n"), 0755)

	svc := NewXKeenService(dummy)
	out, err := svc.Start()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Started") {
		t.Fatalf("expected Started, got %s", out)
	}
}

func TestXKeenService_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Stopped\"\n"), 0755)

	svc := NewXKeenService(dummy)
	out, err := svc.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Stopped") {
		t.Fatalf("expected Stopped, got %s", out)
	}
}

func TestXKeenService_Restart(t *testing.T) {
	tmpDir := t.TempDir()
	dummy := filepath.Join(tmpDir, "xkeen")
	os.WriteFile(dummy, []byte("#!/bin/sh\necho \"Restarted\"\n"), 0755)

	svc := NewXKeenService(dummy)
	out, err := svc.Restart()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Restarted") {
		t.Fatalf("expected Restarted, got %s", out)
	}
}