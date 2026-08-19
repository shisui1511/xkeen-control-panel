package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFilePreservesSymlink(t *testing.T) {
	// На роутере config.yaml Mihomo — симлинк на активный профиль. Запись
	// не должна подменять симлинк обычным файлом.
	dir := t.TempDir()
	target := filepath.Join(dir, "profiles", "default.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicWriteFile(link, []byte("new\n"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "new\n" {
		t.Errorf("expected target to receive the write, got %q", got)
	}
}

func TestAtomicWriteFileBrokenSymlink(t *testing.T) {
	// Битый симлинк: запись идёт по исходному пути и заменяет его.
	dir := t.TempDir()
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join(dir, "missing.yaml"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicWriteFile(link, []byte("data\n"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	got, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "data\n" {
		t.Errorf("expected %q, got %q", "data\n", got)
	}
}
