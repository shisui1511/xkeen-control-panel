package services

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotService(t *testing.T) {
	tmpDataDir := t.TempDir()
	configDir1 := filepath.Join(tmpDataDir, "config1")
	configDir2 := filepath.Join(tmpDataDir, "config2")

	if err := os.MkdirAll(configDir1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir2, 0755); err != nil {
		t.Fatal(err)
	}

	file1 := filepath.Join(configDir1, "test1.json")
	file2 := filepath.Join(configDir2, "test2.yaml")

	if err := os.WriteFile(file1, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("key: value"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewSnapshotService(tmpDataDir, []string{configDir1, configDir2})

	// 1. Create Snapshot
	meta, err := svc.Create("Test Backup")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if meta.Label != "Test Backup" || meta.ID == "" {
		t.Errorf("unexpected metadata: %+v", meta)
	}

	// Verify metadata file and archive file exist
	if _, err := os.Stat(svc.metaPath(meta.ID)); err != nil {
		t.Errorf("metadata file does not exist: %v", err)
	}
	if _, err := os.Stat(svc.archivePath(meta.ID)); err != nil {
		t.Errorf("archive file does not exist: %v", err)
	}

	// 2. List Snapshots
	list, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != meta.ID {
		t.Errorf("unexpected list: %+v", list)
	}

	// 3. Restore Snapshot
	// Let's modify/delete original files first
	if err := os.Remove(file1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := svc.Restore(meta.ID); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	// Check files are restored correctly
	data1, err := os.ReadFile(file1)
	if err != nil || string(data1) != `{"ok":true}` {
		t.Errorf("file1 not restored correctly: %v, content=%q", err, string(data1))
	}
	data2, err := os.ReadFile(file2)
	if err != nil || string(data2) != "key: value" {
		t.Errorf("file2 not restored correctly: %v, content=%q", err, string(data2))
	}

	// 4. Zip Slip protection test
	// Create a malicious archive with ".." path in a temporary file
	maliciousTarGz := filepath.Join(tmpDataDir, "malicious.tar.gz")
	f, err := os.Create(maliciousTarGz)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Write an entry with directory traversal path
	traversalPath := filepath.Join("config1", "..", "..", "escaped.txt")
	// tar header uses forward slashes
	hdr := &tar.Header{
		Name: strings.ReplaceAll(traversalPath, "\\", "/"),
		Mode: 0600,
		Size: int64(len("evil")),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	f.Close()

	// Rename it to match the service naming pattern
	escapedMeta := SnapshotMeta{
		ID:        "escaped-id",
		Label:     "Escaped",
		CreatedAt: meta.CreatedAt + 1,
	}
	if err := svc.saveMeta("escaped-id", escapedMeta); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(maliciousTarGz, svc.archivePath("escaped-id")); err != nil {
		t.Fatal(err)
	}

	// Trigger restore
	if err := svc.Restore("escaped-id"); err != nil {
		t.Fatalf("restore returned error instead of skipping: %v", err)
	}

	// Verify the escaped file was NOT created outside target directories
	escapedFile := filepath.Join(tmpDataDir, "escaped.txt")
	if _, err := os.Stat(escapedFile); !os.IsNotExist(err) {
		t.Errorf("Zip Slip vulnerability detected: file created at %s", escapedFile)
	}

	// 5. Delete Snapshot
	if err := svc.Delete(meta.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(svc.archivePath(meta.ID)); !os.IsNotExist(err) {
		t.Error("archive was not deleted")
	}
}
