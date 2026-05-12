package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDATManagerService_List(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "geoip.dat"), []byte("dummy dat content"), 0644)

	svc := NewDATManagerService(tmp)
	files := svc.List()

	geoip, ok := files["geoip"]
	if !ok {
		t.Fatal("expected geoip file entry")
	}
	if !geoip.Exists {
		t.Fatal("expected geoip to exist")
	}
	if geoip.Size == 0 {
		t.Fatal("expected geoip size > 0")
	}

	geosite, ok := files["geosite"]
	if !ok {
		t.Fatal("expected geosite file entry")
	}
	if geosite.Exists {
		t.Fatal("expected geosite to not exist")
	}
}

func TestDATManagerService_List_NoFiles(t *testing.T) {
	tmp := t.TempDir()
	svc := NewDATManagerService(tmp)
	files := svc.List()

	for _, f := range files {
		if f.Exists {
			t.Fatal("expected no files to exist")
		}
	}
}
