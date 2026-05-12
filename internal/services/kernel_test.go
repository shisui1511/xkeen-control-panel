package services

import (
	"os"
	"path/filepath"
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