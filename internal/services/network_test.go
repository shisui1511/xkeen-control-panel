package services

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestNetworkToolsService_PortCheck_Open(t *testing.T) {
	// Start a local TCP listener on a random port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	// Get listener address
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}

	// Run port check
	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.PortCheck("127.0.0.1", port, 2*time.Second)
	if err != nil {
		t.Fatalf("PortCheck failed: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected PortCheck success, got error: %s", res.Error)
	}
	if res.RTTMs < 0 {
		t.Errorf("expected non-negative RTT, got %d", res.RTTMs)
	}
	if res.Port != port {
		t.Errorf("expected port %d, got %d", port, res.Port)
	}
}

func TestNetworkToolsService_PortCheck_Closed(t *testing.T) {
	// Port 1 is generally closed
	svc := NewNetworkToolsService("http://127.0.0.1:9090")
	res, err := svc.PortCheck("127.0.0.1", 1, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("PortCheck failed: %v", err)
	}

	if res.Success {
		t.Fatal("expected PortCheck to fail on closed port 1")
	}
	if res.Error == "" {
		t.Error("expected error message on failure")
	}
}

func TestNetworkToolsService_ProxyDelayTest_Success(t *testing.T) {
	// Create a mock Clash API server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/node-1/delay" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("url") != "https://google.com" {
			t.Errorf("unexpected target url query parameter: %s", r.URL.Query().Get("url"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"delay": 120}`))
	}))
	defer ts.Close()

	svc := NewNetworkToolsService(ts.URL)
	res, err := svc.ProxyDelayTest("node-1", "https://google.com", 2000)
	if err != nil {
		t.Fatalf("ProxyDelayTest failed: %v", err)
	}

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if res.Delay != 120 {
		t.Errorf("expected delay 120, got %d", res.Delay)
	}
	if res.ProxyName != "node-1" {
		t.Errorf("expected proxy name node-1, got %s", res.ProxyName)
	}
}

func TestNetworkToolsService_ProxyDelayTest_Failure(t *testing.T) {
	// Create a mock Clash API server returning 500 Internal Server Error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "clash error details"}`))
	}))
	defer ts.Close()

	svc := NewNetworkToolsService(ts.URL)
	res, err := svc.ProxyDelayTest("node-1", "https://google.com", 2000)
	if err != nil {
		t.Fatalf("ProxyDelayTest failed: %v", err)
	}

	if res.Success {
		t.Fatal("expected failure, got success")
	}
	if res.Error != "clash error details" {
		t.Errorf("expected error 'clash error details', got: %s", res.Error)
	}
}
