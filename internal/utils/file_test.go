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
	// Битый симлинк: запись идёт в целевой файл, создавая его, а симлинк сохраняется.
	dir := t.TempDir()
	target := filepath.Join(dir, "missing.yaml")
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicWriteFile(link, []byte("data\n"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	// Симлинк должен остаться симлинком
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}

	// Данные должны быть записаны в целевой файл
	gotTarget, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(gotTarget) != "data\n" {
		t.Errorf("expected target to contain %q, got %q", "data\n", gotTarget)
	}

	// Чтение через симлинк также должно возвращать записанные данные
	gotLink, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	if string(gotLink) != "data\n" {
		t.Errorf("expected link to read %q, got %q", "data\n", gotLink)
	}
}

func TestAtomicWriteFile_SymlinkRelative(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(subdir, "default.yaml")
	if err := os.WriteFile(targetFile, []byte("initial\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Относительный симлинк config.yaml -> profiles/default.yaml
	linkFile := filepath.Join(dir, "config.yaml")
	relTarget := filepath.Join("profiles", "default.yaml")
	if err := os.Symlink(relTarget, linkFile); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicWriteFile(linkFile, []byte("updated relative\n"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	info, err := os.Lstat(linkFile)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("relative symlink was replaced by a regular file")
	}

	got, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "updated relative\n" {
		t.Errorf("expected %q, got %q", "updated relative\n", got)
	}
}

func TestAtomicWriteFile_SymlinkAbsolute(t *testing.T) {
	dir := t.TempDir()
	targetFile := filepath.Join(dir, "actual.txt")
	linkFile := filepath.Join(dir, "symlink.txt")

	if err := os.WriteFile(targetFile, []byte("orig"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetFile, linkFile); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := AtomicWriteFile(linkFile, []byte("new-data"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	info, err := os.Lstat(linkFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("absolute symlink was replaced by a regular file")
	}

	got, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-data" {
		t.Errorf("expected %q, got %q", "new-data", got)
	}
}

func TestAtomicWriteFile_RegularFile(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("old content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AtomicWriteFile(regularFile, []byte("new content"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(regularFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Errorf("expected %q, got %q", "new content", got)
	}
}
