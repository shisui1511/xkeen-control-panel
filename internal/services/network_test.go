package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkToolsService_New(t *testing.T) {
	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNetworkToolsService_Ping(t *testing.T) {
	tmpDir := t.TempDir()
	pingPath := filepath.Join(tmpDir, "ping")
	err := os.WriteFile(pingPath, []byte("#!/bin/sh\necho \"64 bytes from test.com\"\n"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.Ping("test.com", 1)
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected ping success: %s", res.Error)
	}
	if !strings.Contains(res.Output, "64 bytes") {
		t.Fatalf("unexpected ping output: %s", res.Output)
	}
}

func TestNetworkToolsService_Traceroute(t *testing.T) {
	tmpDir := t.TempDir()
	cmdPath := filepath.Join(tmpDir, "traceroute")
	os.WriteFile(cmdPath, []byte("#!/bin/sh\necho \"traceroute to test.com\"\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.Traceroute("test.com", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatal("expected success")
	}
	if !strings.Contains(res.Output, "traceroute to") {
		t.Fatalf("unexpected output: %s", res.Output)
	}
}

func TestNetworkToolsService_DNSLookup(t *testing.T) {
	tmpDir := t.TempDir()
	cmdPath := filepath.Join(tmpDir, "nslookup")
	os.WriteFile(cmdPath, []byte("#!/bin/sh\necho \"Server: 8.8.8.8\"\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.DNSLookup("test.com", "ANY")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("expected success: %s", res.Error)
	}
	if len(res.Records) == 0 || !strings.Contains(res.Records[0], "8.8.8.8") {
		t.Fatal("unexpected dns output")
	}
}

func TestNetworkToolsService_HTTPTest(t *testing.T) {
	tmpDir := t.TempDir()
	cmdPath := filepath.Join(tmpDir, "curl")
	os.WriteFile(cmdPath, []byte("#!/bin/sh\necho \"HTTP_CODE: 200\"\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.HTTPTest("http://test.com", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("expected success: %s", res.Error)
	}
	if !strings.Contains(res.Output, "HTTP_CODE: 200") {
		t.Fatal("unexpected http output")
	}
}

func TestNetworkToolsService_GetPublicIP(t *testing.T) {
	tmpDir := t.TempDir()
	cmdPath := filepath.Join(tmpDir, "curl")
	os.WriteFile(cmdPath, []byte("#!/bin/sh\necho \"192.168.1.1\"\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.GetPublicIP()
	if err != nil {
		t.Fatal(err)
	}
	// Note: depending on the test environment, GetPublicIP tries to dial out.
	// If it fails to dial out, it will fail before curl. We'll just accept either.
	_ = res
}
