package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDATManagerService_List(t *testing.T) {
	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	os.WriteFile(filepath.Join(tmpXray, "geoip.dat"), []byte("dummy xray dat"), 0644)
	os.WriteFile(filepath.Join(tmpMihomo, "country.mmdb"), []byte("dummy mmdb"), 0644)
	os.WriteFile(filepath.Join(tmpMihomo, "config.dat"), []byte("dummy mihomo dat"), 0644)

	svc := NewDATManagerService(tmpXray, tmpMihomo)
	files := svc.List()

	foundGeoIP := false
	foundMMDB := false
	foundMihomoDat := false

	for _, f := range files {
		if f.Name == "geoip.dat" && f.Type == "xray" {
			foundGeoIP = true
		}
		if f.Name == "country.mmdb" && f.Type == "mihomo" {
			foundMMDB = true
		}
		if f.Name == "config.dat" && f.Type == "mihomo" {
			foundMihomoDat = true
		}
	}

	if !foundGeoIP {
		t.Error("expected geoip.dat in xray dir")
	}
	if !foundMMDB {
		t.Error("expected country.mmdb in mihomo dir")
	}
	if !foundMihomoDat {
		t.Error("expected config.dat in mihomo dir")
	}
}

func TestDATManagerService_List_NoFiles(t *testing.T) {
	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()
	svc := NewDATManagerService(tmpXray, tmpMihomo)
	files := svc.List()

	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func makeVarint(val uint64) []byte {
	var buf []byte
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			buf = append(buf, b|0x80)
		} else {
			buf = append(buf, b)
			break
		}
	}
	return buf
}

func makeLD(fieldNum int, data []byte) []byte {
	tag := (fieldNum << 3) | 2
	var buf []byte
	buf = append(buf, makeVarint(uint64(tag))...)
	buf = append(buf, makeVarint(uint64(len(data)))...)
	buf = append(buf, data...)
	return buf
}

func makeVarintField(fieldNum int, val uint64) []byte {
	tag := fieldNum << 3
	var buf []byte
	buf = append(buf, makeVarint(uint64(tag))...)
	buf = append(buf, makeVarint(val)...)
	return buf
}

func TestSearchTag_GeoSite(t *testing.T) {
	dom1 := makeLD(2, []byte("google.com"))
	dom2 := makeLD(2, []byte("youtube.com"))

	entry1 := append(makeLD(1, []byte("google")), makeLD(2, dom1)...)
	entry1 = append(entry1, makeLD(2, dom2)...)

	outer := makeLD(1, entry1)

	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	os.WriteFile(filepath.Join(tmpXray, "geosite.dat"), outer, 0644)

	svc := NewDATManagerService(tmpXray, tmpMihomo)

	res, err := svc.SearchTag("geosite.dat", "google", "", 0, 10)
	if err != nil {
		t.Fatalf("SearchTag failed: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("expected 2 entries, got %d", res.Total)
	}
	if res.Entries[0] != "google.com" || res.Entries[1] != "youtube.com" {
		t.Errorf("unexpected entries: %v", res.Entries)
	}

	res, err = svc.SearchTag("geosite.dat", "google", "youtube", 0, 10)
	if err != nil {
		t.Fatalf("SearchTag failed: %v", err)
	}
	if res.Total != 1 {
		t.Errorf("expected 1 entry, got %d", res.Total)
	}
	if res.Entries[0] != "youtube.com" {
		t.Errorf("expected youtube.com, got %s", res.Entries[0])
	}
}

func TestSearchTag_GeoIP(t *testing.T) {
	cidr1 := append(makeLD(1, []byte{8, 8, 8, 8}), makeVarintField(2, 32)...)
	cidr2 := append(makeLD(1, []byte{1, 1, 1, 1}), makeVarintField(2, 24)...)

	entry1 := append(makeLD(1, []byte("google")), makeLD(2, cidr1)...)
	entry1 = append(entry1, makeLD(2, cidr2)...)

	outer := makeLD(1, entry1)

	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	os.WriteFile(filepath.Join(tmpXray, "geoip.dat"), outer, 0644)

	svc := NewDATManagerService(tmpXray, tmpMihomo)

	res, err := svc.SearchTag("geoip.dat", "google", "", 0, 10)
	if err != nil {
		t.Fatalf("SearchTag failed: %v", err)
	}
	if res.Total != 2 {
		t.Errorf("expected 2 entries, got %d", res.Total)
	}
	if res.Entries[0] != "8.8.8.8/32" || res.Entries[1] != "1.1.1.1/24" {
		t.Errorf("unexpected entries: %v", res.Entries)
	}
}

func TestSearchTag_MalformedProtobuf(t *testing.T) {
	// Create a malformed protobuf payload with a wiretype 2 tag, but a huge length
	// tag: field-1 wiretype 2 -> (1<<3)|2 = 10 (0x0A)
	data := []byte{0x0A}
	// Append a huge length varint: 0xFFFFFFFFFFFFFFFF
	for i := 0; i < 9; i++ {
		data = append(data, 0xFF)
	}
	data = append(data, 0x01)

	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	os.WriteFile(filepath.Join(tmpXray, "geosite.dat"), data, 0644)

	svc := NewDATManagerService(tmpXray, tmpMihomo)

	// This must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SearchTag panicked on malformed input: %v", r)
		}
	}()

	_, err := svc.SearchTag("geosite.dat", "any", "", 0, 10)
	if err != nil {
		t.Logf("SearchTag returned expected error/nil: %v", err)
	}
}

func TestResolveUpdateURL_MihomoConfigGeox(t *testing.T) {
	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	configContent := `
geox-url:
  geosite: "https://example.com/zkeen.dat"
  geoip: "https://example.com/zkeenip.dat"
`
	if err := os.WriteFile(filepath.Join(tmpMihomo, "config.yaml"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewDATManagerService(tmpXray, tmpMihomo)

	// GeoSite.dat in Mihomo should use custom geox-url.geosite
	urlSite, err := svc.resolveUpdateURL("GeoSite.dat", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urlSite != "https://example.com/zkeen.dat" {
		t.Errorf("expected custom geosite URL, got %s", urlSite)
	}

	// GeoIP.dat in Mihomo should use custom geox-url.geoip
	urlIP, err := svc.resolveUpdateURL("GeoIP.dat", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urlIP != "https://example.com/zkeenip.dat" {
		t.Errorf("expected custom geoip URL, got %s", urlIP)
	}

	// Xray geosite.dat should use standard URL
	urlXray, err := svc.resolveUpdateURL("geosite.dat", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urlXray != "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat" {
		t.Errorf("expected v2fly URL for Xray geosite, got %s", urlXray)
	}
}

func TestResolveUpdateURL_MihomoDefaultMetaCubeX(t *testing.T) {
	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	// No config.yaml exists
	svc := NewDATManagerService(tmpXray, tmpMihomo)

	urlSite, err := svc.resolveUpdateURL("geosite.dat", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urlSite != "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat" {
		t.Errorf("expected MetaCubeX geosite URL, got %s", urlSite)
	}

	urlIP, err := svc.resolveUpdateURL("geoip.dat", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if urlIP != "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat" {
		t.Errorf("expected MetaCubeX geoip URL, got %s", urlIP)
	}
}

func TestDATManagerService_BrokenSymlink(t *testing.T) {
	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	// Create broken symlink
	brokenLink := filepath.Join(tmpXray, "broken.dat")
	_ = os.Symlink(filepath.Join(tmpXray, "nonexistent.dat"), brokenLink)

	svc := NewDATManagerService(tmpXray, tmpMihomo)
	files := svc.List()

	var found *DATFile
	for i := range files {
		if files[i].Name == "broken.dat" {
			found = &files[i]
			break
		}
	}

	if found == nil {
		t.Fatal("broken.dat not found in List()")
	}
	if found.Exists {
		t.Errorf("expected Exists: false for broken symlink, got true")
	}
	if !found.IsSymlink {
		t.Errorf("expected IsSymlink: true for broken symlink, got false")
	}
}

func TestDATManagerService_ValidationRollback(t *testing.T) {
	tmpXray := t.TempDir()
	tmpMihomo := t.TempDir()

	// Create initial file
	target := filepath.Join(tmpMihomo, "GeoSite.dat")
	initialData := []byte("initial valid data")
	if err := os.WriteFile(target, initialData, 0644); err != nil {
		t.Fatal(err)
	}
	// Also config.yaml
	if err := os.WriteFile(filepath.Join(tmpMihomo, "config.yaml"), []byte("mode: rule"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create mock failing mihomo binary
	mockBin := filepath.Join(tmpMihomo, "mock-mihomo")
	mockScript := "#!/bin/sh\necho 'mock validation error'\nexit 1\n"
	if err := os.WriteFile(mockBin, []byte(mockScript), 0755); err != nil {
		t.Fatal(err)
	}

	svc := NewDATManagerService(tmpXray, tmpMihomo)
	svc.SetBinaries(mockBin, "", "")

	// Call validateKernelConfig directly
	err := svc.validateKernelConfig(tmpMihomo, "GeoSite.dat")
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "mock validation error") {
		t.Errorf("expected mock validation error, got %v", err)
	}
}

func TestDATManagerService_SymlinkBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "actual.dat")
	linkPath := filepath.Join(dir, "link.dat")

	if err := os.WriteFile(targetPath, []byte("target data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatal(err)
	}

	// Backup the symlink
	if err := backupFile(linkPath); err != nil {
		t.Fatalf("backupFile failed: %v", err)
	}

	// Verify .bak.link exists and contains target path
	linkBak := linkPath + ".bak.link"
	content, err := os.ReadFile(linkBak)
	if err != nil {
		t.Fatalf("reading link backup failed: %v", err)
	}
	if string(content) != targetPath {
		t.Errorf("expected link target %s, got %s", targetPath, string(content))
	}

	// Simulate replacement with new file (which breaks symlink)
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte("replaced data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Call restoreFile
	if err := restoreFile(linkPath); err != nil {
		t.Fatalf("restoreFile failed: %v", err)
	}

	// Verify linkPath was restored as symlink to targetPath
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat restored link failed: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected restored file to be symlink, got regular file")
	}
	readTarget, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink failed: %v", err)
	}
	if readTarget != targetPath {
		t.Errorf("expected target %s, got %s", targetPath, readTarget)
	}
	if _, err := os.Stat(linkBak); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after restore", linkBak)
	}
}

// TestDATManagerService_BackupFile_CleansSiblingBackupOnTypeSwitch covers
// the regression from 120-REVIEW.md CR-03: a geo-file that alternates
// between symlink and regular file across successive Update() runs must
// not leave a stale backup of the opposite type behind, because
// rollbackFile/restoreFile unconditionally check .bak.link first and would
// otherwise restore the older, now-wrong backup type.
func TestDATManagerService_BackupFile_CleansSiblingBackupOnTypeSwitch(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "actual.dat")
	path := filepath.Join(dir, "geo.dat")

	if err := os.WriteFile(targetPath, []byte("target data"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("symlink -> regular file removes stale .bak", func(t *testing.T) {
		// First backup: path is a symlink -> creates .bak.link.
		if err := os.Symlink(targetPath, path); err != nil {
			t.Fatal(err)
		}
		if err := backupFile(path); err != nil {
			t.Fatalf("backupFile (symlink) failed: %v", err)
		}
		if _, err := os.Stat(path + ".bak.link"); err != nil {
			t.Fatalf(".bak.link missing after symlink backup: %v", err)
		}

		// Simulate an external xkeen -ug rewriting the geo-file as a plain
		// file, then leave a stale .bak from some earlier backup cycle to
		// mimic the orphaned-backup scenario described in CR-03.
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("plain data v1"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".bak", []byte("stale .bak"), 0644); err != nil {
			t.Fatal(err)
		}

		// Second backup: path is now a regular file -> must remove the
		// sibling .bak.link left over from the symlink era.
		if err := backupFile(path); err != nil {
			t.Fatalf("backupFile (regular) failed: %v", err)
		}
		if _, err := os.Stat(path + ".bak.link"); !os.IsNotExist(err) {
			t.Errorf("expected stale .bak.link to be removed after regular-file backup")
		}
		if _, err := os.Stat(path + ".bak"); err != nil {
			t.Errorf(".bak missing after regular-file backup: %v", err)
		}

		cleanBackupFile(path)
		_ = os.Remove(path)
	})

	t.Run("regular file -> symlink removes stale .bak.link", func(t *testing.T) {
		// First backup: path is a regular file -> creates .bak.
		if err := os.WriteFile(path, []byte("plain data"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := backupFile(path); err != nil {
			t.Fatalf("backupFile (regular) failed: %v", err)
		}
		if _, err := os.Stat(path + ".bak"); err != nil {
			t.Fatalf(".bak missing after regular-file backup: %v", err)
		}

		// Simulate the geo-file becoming a symlink, with a stale .bak.link
		// left over from an even earlier backup cycle.
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetPath, path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".bak.link", []byte("stale link"), 0644); err != nil {
			t.Fatal(err)
		}

		// Second backup: path is now a symlink -> must remove the sibling
		// .bak left over from the regular-file era.
		if err := backupFile(path); err != nil {
			t.Fatalf("backupFile (symlink) failed: %v", err)
		}
		if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
			t.Errorf("expected stale .bak to be removed after symlink backup")
		}
		if _, err := os.Stat(path + ".bak.link"); err != nil {
			t.Errorf(".bak.link missing after symlink backup: %v", err)
		}

		cleanBackupFile(path)
	})
}

func TestDATManagerService_CleanBackupFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.dat")
	if err := os.WriteFile(filePath, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath+".bak", []byte("bak data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath+".bak.link", []byte("link data"), 0644); err != nil {
		t.Fatal(err)
	}

	cleanBackupFile(filePath)

	if _, err := os.Stat(filePath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("expected .bak to be removed")
	}
	if _, err := os.Stat(filePath + ".bak.link"); !os.IsNotExist(err) {
		t.Errorf("expected .bak.link to be removed")
	}
}
