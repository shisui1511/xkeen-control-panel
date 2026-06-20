package services

import (
	"os"
	"path/filepath"
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
	tag := (fieldNum << 3) | 0
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

