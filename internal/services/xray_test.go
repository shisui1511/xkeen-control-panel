package services_test

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestTLSPing(t *testing.T) {
	// 1. Success against local test TLS server
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{
		NextProtos: []string{"h2", "http/1.1"},
	}
	server.StartTLS()
	defer server.Close()

	addr := server.Listener.Addr().String()

	res, err := services.TLSPing(addr, "example.com", []string{"h2", "http/1.1"})
	if err != nil {
		t.Fatalf("TLSPing returned unexpected Go error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK true, got false with error: %s", res.Error)
	}
	if res.TLSVersion == "" {
		t.Errorf("expected non-empty TLSVersion")
	}
	if res.CipherSuite == "" {
		t.Errorf("expected non-empty CipherSuite")
	}
	if res.ChainLength < 1 {
		t.Errorf("expected ChainLength >= 1, got %d", res.ChainLength)
	}
	if res.HandshakeMs < 0 {
		t.Errorf("expected HandshakeMs >= 0, got %d", res.HandshakeMs)
	}

	// 2. Unreachable destination returns OK=false, non-empty error, but no Go error
	// Listen and immediately close to get an unused port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	closedAddr := lis.Addr().String()
	_ = lis.Close()

	resDown, err := services.TLSPing(closedAddr, "", nil)
	if err != nil {
		t.Fatalf("expected err == nil for unreachable host, got: %v", err)
	}
	if resDown.OK {
		t.Errorf("expected OK=false for closed address")
	}
	if resDown.Error == "" {
		t.Errorf("expected non-empty Error string for unreachable host")
	}
}

func TestTLSPingTargetValidation(t *testing.T) {
	panelPort := 8090

	testCases := []struct {
		name        string
		dest        string
		panelPort   int
		shouldFail  bool
		errContains string
	}{
		// Valid cases
		{"valid domain with port", "cloudflare.com:443", panelPort, false, ""},
		{"valid domain without port", "example.com", panelPort, false, ""},
		{"valid public IPv4", "1.1.1.1:443", panelPort, false, ""},
		{"valid public IPv4 no port", "8.8.8.8", panelPort, false, ""},

		// Loopback
		{"loopback ipv4", "127.0.0.1:443", panelPort, true, "loopback"},
		{"loopback ipv4 no port", "127.0.0.5", panelPort, true, "loopback"},
		{"loopback localhost", "localhost:443", panelPort, true, "loopback"},
		{"loopback ipv6", "[::1]:443", panelPort, true, "loopback"},

		// Private IPv4 & IPv6
		{"private 10.x", "10.0.0.1:443", panelPort, true, "private"},
		{"private 172.16.x", "172.16.0.1:443", panelPort, true, "private"},
		{"private 192.168.x", "192.168.1.1:443", panelPort, true, "private"},
		{"private ipv6 ula", "[fc00::1]:443", panelPort, true, "private"},

		// Link-local & cloud metadata
		{"link-local metadata", "169.254.169.254:443", panelPort, true, "link-local"},
		{"link-local general", "169.254.1.1:443", panelPort, true, "link-local"},
		{"link-local ipv6", "[fe80::1]:443", panelPort, true, "link-local"},

		// CGNAT
		{"cgnat", "100.64.0.1:443", panelPort, true, "carrier-grade NAT"},

		// Panel port matching
		{"panel port match", "example.com:8090", panelPort, true, "matches control panel port"},

		// Control chars & whitespace
		{"control char newline", "example.com\n:443", panelPort, true, "invalid or control"},
		{"control char tab", "example\t.com:443", panelPort, true, "invalid or control"},
		{"whitespace in dest", "example .com:443", panelPort, true, "invalid or control"},

		// Empty & invalid format
		{"empty dest", "", panelPort, true, "empty"},
		{"empty host with port", ":443", panelPort, true, "empty"},
		{"port out of range low", "example.com:0", panelPort, true, "port must be between"},
		{"port out of range high", "example.com:70000", panelPort, true, "port must be between"},
		{"host too long", strings.Repeat("a", 255) + ".com:443", panelPort, true, "maximum length"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := services.ValidateTLSTarget(tc.dest, tc.panelPort)
			if tc.shouldFail {
				if err == nil {
					t.Errorf("expected validation failure for %q, got nil", tc.dest)
				} else if tc.errContains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.errContains)) {
					t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected validation success for %q, got error: %v", tc.dest, err)
				}
			}
		})
	}
}
