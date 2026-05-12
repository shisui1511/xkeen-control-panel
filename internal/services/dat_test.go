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
