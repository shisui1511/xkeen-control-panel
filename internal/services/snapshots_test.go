package services

import (
	"archive/tar"
	"compress/gzip"
	"io"
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
	datFile := filepath.Join(configDir1, "geoip.dat")

	if err := os.WriteFile(file1, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte("key: value"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(datFile, []byte("large geoip database content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create snapshots and tmp subdirs inside tmpDataDir to test exclusions
	snapshotsSubdir := filepath.Join(tmpDataDir, "snapshots")
	tmpSubdir := filepath.Join(tmpDataDir, "tmp")
	if err := os.MkdirAll(snapshotsSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmpSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	dummyBackupFile := filepath.Join(snapshotsSubdir, "dummy.tar.gz")
	if err := os.WriteFile(dummyBackupFile, []byte("dummy archive"), 0644); err != nil {
		t.Fatal(err)
	}
	dummyTmpFile := filepath.Join(tmpSubdir, "dummy_temp.txt")
	if err := os.WriteFile(dummyTmpFile, []byte("dummy temp"), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewSnapshotService(tmpDataDir, []string{configDir1, configDir2, tmpDataDir})

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

	// Verify exclusions and filtering:
	// Let's read the created archive and verify that snapshots/, tmp/, and geoip.dat are NOT inside
	archFile, err := os.Open(svc.archivePath(meta.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer archFile.Close()
	gr, err := gzip.NewReader(archFile)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := hdr.Name
		if strings.Contains(name, "snapshots") {
			t.Errorf("snapshots directory/file %s should be excluded but was found in archive", name)
		}
		if strings.Contains(name, "tmp") {
			t.Errorf("tmp directory/file %s should be excluded but was found in archive", name)
		}
		if strings.HasSuffix(strings.ToLower(name), ".dat") {
			t.Errorf("DAT file %s should be filtered out but was found in archive", name)
		}
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
	gw2 := gzip.NewWriter(f)
	tw2 := tar.NewWriter(gw2)

	// Write an entry with directory traversal path
	traversalPath := filepath.Join("config1", "..", "..", "escaped.txt")
	hdr := &tar.Header{
		Name: strings.ReplaceAll(traversalPath, "\\", "/"),
		Mode: 0600,
		Size: int64(len("evil")),
	}
	if err := tw2.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw2.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	tw2.Close()
	gw2.Close()
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

	// Trigger restore - should FAIL (return error) due to Zip Slip detection
	if err := svc.Restore("escaped-id"); err == nil {
		t.Fatal("restore should have failed due to Zip Slip path")
	}

	// Verify the escaped file was NOT created outside target directories
	escapedFile := filepath.Join(tmpDataDir, "escaped.txt")
	if _, err := os.Stat(escapedFile); !os.IsNotExist(err) {
		t.Errorf("Zip Slip vulnerability detected: file created at %s", escapedFile)
	}

	// 5. Test SaveUploaded
	uploadContent := "test upload data"
	uploadReader := strings.NewReader(uploadContent)
	uploadMeta, err := svc.SaveUploaded(uploadReader, "backup.tar.gz")
	if err != nil {
		t.Fatalf("SaveUploaded failed: %v", err)
	}
	if !strings.HasPrefix(uploadMeta.Label, "Загружен:") {
		t.Errorf("expected label prefix 'Загружен:', got %s", uploadMeta.Label)
	}
	// Verify uploaded file content
	uploadedData, err := os.ReadFile(svc.archivePath(uploadMeta.ID))
	if err != nil || string(uploadedData) != uploadContent {
		t.Errorf("uploaded data mismatch: %v, content=%q", err, string(uploadedData))
	}

	// Test SaveUploaded file size limit (15MB)
	// We pass a large reader
	largeReader := io.LimitReader(dummyZeroReader{}, 16*1024*1024) // 16MB
	_, err = svc.SaveUploaded(largeReader, "too_large.tar.gz")
	if err == nil {
		t.Fatal("expected SaveUploaded to fail for files > 15MB")
	}

	// 6. Delete Snapshot
	if err := svc.Delete(meta.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, err := os.Stat(svc.archivePath(meta.ID)); !os.IsNotExist(err) {
		t.Error("archive was not deleted")
	}
}

type dummyZeroReader struct{}

func (dummyZeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func TestSnapshotService_EdgeCases(t *testing.T) {
	tmpDataDir := t.TempDir()
	configDir := filepath.Join(tmpDataDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewSnapshotService(tmpDataDir, []string{configDir})

	// 1. Test symlink skipping during Create
	regFile := filepath.Join(configDir, "regular.txt")
	if err := os.WriteFile(regFile, []byte("regular content"), 0644); err != nil {
		t.Fatal(err)
	}
	symLink := filepath.Join(configDir, "link.txt")
	hasSymlink := true
	if err := os.Symlink("regular.txt", symLink); err != nil {
		hasSymlink = false
	}

	meta, err := svc.Create("Edge Case Test")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify the archive doesn't contain the symlink
	archFile, err := os.Open(svc.archivePath(meta.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer archFile.Close()
	gr, err := gzip.NewReader(archFile)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	foundSymlink := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(hdr.Name, "link.txt") {
			foundSymlink = true
		}
	}
	if hasSymlink && foundSymlink {
		t.Error("symlink was included in snapshot archive, but it should have been skipped")
	}

	// 2. Test restore file size limit (> 10MB)
	largeTarGz := filepath.Join(tmpDataDir, "large.tar.gz")
	f, err := os.Create(largeTarGz)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     "config/large_file.txt",
		Mode:     0644,
		Size:     11 * 1024 * 1024,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	f.Close()

	largeMeta := SnapshotMeta{
		ID:        "large-id",
		Label:     "Too Large",
		CreatedAt: meta.CreatedAt + 2,
	}
	if err := svc.saveMeta("large-id", largeMeta); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(largeTarGz, svc.archivePath("large-id")); err != nil {
		t.Fatal(err)
	}

	err = svc.Restore("large-id")
	if err == nil {
		t.Error("expected restore to fail due to file exceeding 10 MB limit")
	} else if !strings.Contains(err.Error(), "exceeds maximum allowed size of 10 MB") {
		t.Errorf("expected size limit error, got: %v", err)
	}
}
