package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfigService(t *testing.T) {
	tmp := t.TempDir()
	svc := NewConfigService(tmp)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestConfigService_List(t *testing.T) {
	tmp := t.TempDir()
	files := []string{"config1.json", "config2.json"}
	for _, f := range files {
		os.WriteFile(filepath.Join(tmp, f), []byte("{}"), 0644)
	}

	svc := NewConfigService(tmp)
	got, err := svc.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 files, got %d", len(got))
	}
}

func TestConfigService_ReadWrite(t *testing.T) {
	tmp := t.TempDir()
	svc := NewConfigService(tmp)
	testFile := filepath.Join(tmp, "test.json")
	data := []byte(`{"log": {"level": "debug"}}`)

	err := svc.Save(testFile, data)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	read, err := svc.Read(testFile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(read) != string(data) {
		t.Fatalf("data mismatch: got %s, want %s", read, data)
	}
}

func TestConfigService_PathTraversal(t *testing.T) {
	tmp := t.TempDir()
	svc := NewConfigService(tmp)

	_, err := svc.Read("../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestConfigService_Backup(t *testing.T) {
	tmp := t.TempDir()
	svc := NewConfigService(tmp)
	testFile := filepath.Join(tmp, "config.json")

	os.WriteFile(testFile, []byte(`{"v1":true}`), 0644)
	backups, err := svc.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected 0 backups initially, got %d", len(backups))
	}

	err = svc.Save(testFile, []byte(`{"v2":true}`))
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	backups, err = svc.ListBackups(testFile)
	if err != nil {
		t.Fatalf("ListBackups after save failed: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup after save, got %d", len(backups))
	}
}