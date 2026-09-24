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

func TestValidatePathAllowed(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "allowed")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	allowedRoots := []string{subDir}

	tests := []struct {
		name         string
		path         string
		allowedRoots []string
		wantErr      bool
	}{
		{
			name:         "Direct file in allowed root",
			path:         filepath.Join(subDir, "test.txt"),
			allowedRoots: allowedRoots,
			wantErr:      false,
		},
		{
			name:         "Subdirectory file in allowed root",
			path:         filepath.Join(subDir, "nested", "test.txt"),
			allowedRoots: allowedRoots,
			wantErr:      false,
		},
		{
			name:         "Path traversal attempt using parent reference",
			path:         filepath.Join(subDir, "..", "outside.txt"),
			allowedRoots: allowedRoots,
			wantErr:      true,
		},
		{
			name:         "Completely outside path",
			path:         filepath.Join(tempDir, "other.txt"),
			allowedRoots: allowedRoots,
			wantErr:      true,
		},
		{
			name:         "Empty allowed roots",
			path:         filepath.Join(subDir, "test.txt"),
			allowedRoots: []string{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validatePathAllowed(tt.path, tt.allowedRoots)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePathAllowed() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got == "" {
				t.Errorf("validatePathAllowed() returned empty path for valid input")
			}
		})
	}
}

func TestAtomicWriteFileSafe(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "safe_dir")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatal(err)
	}
	allowedRoots := []string{allowedDir}

	// 1. Safe write inside allowed roots
	safeFile := filepath.Join(allowedDir, "file.txt")
	err := AtomicWriteFileSafe(safeFile, []byte("safe content"), 0644, allowedRoots)
	if err != nil {
		t.Fatalf("AtomicWriteFileSafe inside allowed root failed: %v", err)
	}
	content, err := os.ReadFile(safeFile)
	if err != nil || string(content) != "safe content" {
		t.Fatalf("read content mismatch: got %q, err %v", string(content), err)
	}

	// 2. Unsafe write outside allowed roots
	unsafeFile := filepath.Join(tempDir, "outside.txt")
	err = AtomicWriteFileSafe(unsafeFile, []byte("unsafe content"), 0644, allowedRoots)
	if err == nil {
		t.Fatal("expected AtomicWriteFileSafe outside allowed root to fail, but succeeded")
	}
	if _, err := os.Stat(unsafeFile); !os.IsNotExist(err) {
		t.Fatalf("unsafe file should not have been created: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.txt")
	dst := filepath.Join(tempDir, "nested", "dst.txt")

	if err := os.WriteFile(src, []byte("source data to copy"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Copy valid file
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}
	content, err := os.ReadFile(dst)
	if err != nil || string(content) != "source data to copy" {
		t.Fatalf("read destination mismatch: got %q, err %v", string(content), err)
	}

	// 2. Copy non-existent source
	nonExistentSrc := filepath.Join(tempDir, "missing.txt")
	err = CopyFile(nonExistentSrc, filepath.Join(tempDir, "missing_dst.txt"))
	if err == nil {
		t.Fatal("expected CopyFile with missing source to fail, but succeeded")
	}
}

func TestAtomicReplaceFile_KeepsSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "profiles", "default.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := AtomicReplaceFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("config.yaml symlink was replaced by a regular file")
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Fatalf("profile content = %q", data)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode widened to %v", fi.Mode().Perm())
	}

	fresh := filepath.Join(dir, "fresh.json")
	if err := AtomicReplaceFile(fresh, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode = %v", fi.Mode().Perm())
	}
}
