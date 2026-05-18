package services

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestKernelService_New(t *testing.T) {
	svc := NewKernelService()
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestKernelService_List(t *testing.T) {
	svc := NewKernelService()
	kernels := svc.List()
	if len(kernels) == 0 {
		t.Fatal("expected at least one kernel")
	}
}

func TestKernelService_Get(t *testing.T) {
	svc := NewKernelService()
	kernel := svc.Get("xray")
	if kernel == nil {
		t.Fatal("expected xray kernel to exist")
	}
	if kernel.Name != "xray" {
		t.Fatalf("expected kernel name 'xray', got %s", kernel.Name)
	}
}

func TestKernelService_Get_Unknown(t *testing.T) {
	svc := NewKernelService()
	kernel := svc.Get("unknown")
	if kernel != nil {
		t.Fatal("expected nil for unknown kernel")
	}
}

func TestKernelService_SetChannel(t *testing.T) {
	svc := NewKernelService()

	ok := svc.SetChannel("xray", "preview")
	if !ok {
		t.Fatal("expected SetChannel to succeed")
	}

	k := svc.Get("xray")
	if k.Channel != "preview" {
		t.Fatalf("expected channel 'preview', got %s", k.Channel)
	}

	ok = svc.SetChannel("unknown", "preview")
	if ok {
		t.Fatal("expected SetChannel to fail for unknown kernel")
	}
}

func TestKernelService_DetectVersion_Xray(t *testing.T) {
	tmpDir := t.TempDir()
	xrayPath := filepath.Join(tmpDir, "xray")
	os.WriteFile(xrayPath, []byte("#!/bin/sh\necho \"Xray 1.8.24 (Xray, Penetrates Everything.)\"\n"), 0755)

	svc := NewKernelService()
	svc.kernels["xray"].BinaryPath = xrayPath

	v := svc.detectVersion(svc.kernels["xray"])
	if v != "1.8.24" {
		t.Fatalf("expected version 1.8.24, got %s", v)
	}
}

func TestKernelService_DetectVersion_Mihomo(t *testing.T) {
	tmpDir := t.TempDir()
	mihomoPath := filepath.Join(tmpDir, "mihomo")
	os.WriteFile(mihomoPath, []byte("#!/bin/sh\necho \"Mihomo Version v1.18.0\"\n"), 0755)

	svc := NewKernelService()
	svc.kernels["mihomo"].BinaryPath = mihomoPath

	v := svc.detectVersion(svc.kernels["mihomo"])
	if v != "1.18.0" {
		t.Fatalf("expected version 1.18.0, got %s", v)
	}
}

func TestKernelService_DetectVersion_NotInstalled(t *testing.T) {
	svc := NewKernelService()
	svc.kernels["xray"].BinaryPath = "/tmp/does-not-exist"

	v := svc.detectVersion(svc.kernels["xray"])
	if v != "not installed" {
		t.Fatalf("expected version 'not installed', got %s", v)
	}
}

// TestValidateKernelPath: path traversal is rejected; valid paths are accepted.
func TestValidateKernelPath(t *testing.T) {
	cases := []struct {
		path    string
		wantErr bool
	}{
		{"/opt/bin/xray", false},
		{"/opt/bin/.backup/kernel.bak.123", false},
		{"/opt/etc/mihomo/config.yaml", false},
		{"/opt/bin/../etc/passwd", true}, // traversal
		{"/home/user/evil", true},        // outside allowed roots
		{"relative/path", true},          // not absolute
		{"", true},                       // empty
	}

	for _, tc := range cases {
		err := validateKernelPath(tc.path)
		if tc.wantErr && err == nil {
			t.Errorf("path %q: expected error, got nil", tc.path)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("path %q: unexpected error: %v", tc.path, err)
		}
	}
}

// TestSetChannel_InvalidValue: invalid channel name returns false.
func TestSetChannel_InvalidValue(t *testing.T) {
	svc := NewKernelService()
	ok := svc.SetChannel("xray", "nightly")
	if ok {
		t.Error("expected SetChannel to return false for invalid channel 'nightly'")
	}
	ok = svc.SetChannel("xray", "")
	if ok {
		t.Error("expected SetChannel to return false for empty channel")
	}
	ok = svc.SetChannel("xray", "stable")
	if !ok {
		t.Error("expected SetChannel to return true for 'stable'")
	}
}

// TestConcurrentInstall409: calling Install twice on the same kernel while the first is in progress
// returns an error containing "install already in progress".
func TestConcurrentInstall409(t *testing.T) {
	svc := NewKernelService()

	// Manually acquire the install lock for "xray" to simulate an in-progress install.
	mu := &sync.Mutex{}
	actual, _ := svc.installLocks.LoadOrStore("xray", mu)
	installMu := actual.(*sync.Mutex)
	installMu.Lock() // hold the lock — simulates an ongoing install
	defer installMu.Unlock()

	// Now calling Install should fail immediately with "install already in progress".
	err := svc.Install("xray")
	if err == nil {
		t.Fatal("expected error from Install when lock is held, got nil")
	}
	if !strings.Contains(err.Error(), "install already in progress") {
		t.Errorf("expected 'install already in progress' error, got: %v", err)
	}
}

// TestDecompressionLimit: zip with a 51 MB entry is rejected.
func TestDecompressionLimit(t *testing.T) {
	// Create a zip in memory with a single large file
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.Create("xray")
	if err != nil {
		t.Fatal(err)
	}

	// Write 51 MB of zeros
	chunk := make([]byte, 64*1024)
	total := 0
	limit := 51 * 1024 * 1024
	for total < limit {
		n := limit - total
		if n > len(chunk) {
			n = len(chunk)
		}
		written, err := fw.Write(chunk[:n])
		if err != nil {
			t.Fatal(err)
		}
		total += written
	}
	w.Close()

	// Write zip to a temp file
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "xray.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewKernelService()
	outPath, err := svc.extractZip(zipPath, "xray")
	// The function should succeed (LimitReader silently stops at limit) but we verify
	// the output file is not larger than maxKernelExtractBytes
	if err != nil {
		// If extraction returned error, that's acceptable too
		return
	}
	defer os.Remove(outPath)

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat extracted file: %v", err)
	}
	if info.Size() > maxKernelExtractBytes {
		t.Errorf("extracted file size %d exceeds limit %d", info.Size(), maxKernelExtractBytes)
	}
}
