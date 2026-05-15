package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsoleService_GetCommands(t *testing.T) {
	svc := NewConsoleService("/bin/true")
	commands := svc.GetCommands()

	if len(commands) == 0 {
		t.Fatal("expected commands, got empty list")
	}

	first := commands[0]
	if first.Name != "service" {
		t.Fatalf("expected first category name 'service', got %s", first.Name)
	}
	if len(first.Commands) == 0 {
		t.Fatal("expected at least one command in first category")
	}
}

func TestConsoleService_Execute(t *testing.T) {
	tmpDir := t.TempDir()
	dummyPath := filepath.Join(tmpDir, "xkeen_dummy")
	err := os.WriteFile(dummyPath, []byte("#!/bin/sh\necho xkeen dummy\n"), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock xkeen: %v", err)
	}

	svc := NewConsoleService(dummyPath)

	result, err := svc.Execute("-status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "xkeen dummy") {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestConsoleService_Execute_Failure(t *testing.T) {
	tmpDir := t.TempDir()
	failPath := filepath.Join(tmpDir, "xkeen_fail")
	err := os.WriteFile(failPath, []byte("#!/bin/sh\nexit 1\n"), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock xkeen: %v", err)
	}

	svc := NewConsoleService(failPath)
	result, err := svc.Execute("-status")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Fatal("expected command to fail")
	}
	if result.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestConsoleService_Execute_NonExistent(t *testing.T) {
	svc := NewConsoleService("/nonexistent/xkeen")
	result, err := svc.Execute("-status")
	if err != nil {
		t.Fatal("Execute should not return error for external command failures")
	}
	if result.Success {
		t.Fatal("expected failure for non-existent binary")
	}
}
