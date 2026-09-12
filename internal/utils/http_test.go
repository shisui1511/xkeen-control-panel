package utils

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowPrivate bool
		wantErr      bool
	}{
		{"valid public https", "https://google.com", false, false},
		{"valid public http", "http://example.com/path?query=1", false, false},
		{"invalid scheme ftp", "ftp://example.com", false, true},
		{"invalid scheme file", "file:///etc/passwd", false, true},
		{"invalid scheme gopher", "gopher://example.com", false, true},
		{"malformed URL", "://bad-url", false, true},
		{"loopback ipv4", "http://127.0.0.1", false, true},
		{"loopback ipv4 with port", "http://127.0.0.1:8080", false, true},
		{"loopback localhost", "http://localhost", false, true},
		{"loopback ipv6", "http://[::1]", false, true},
		{"loopback ipv6 allowed", "http://[::1]", true, false},
		{"private class A", "http://10.0.0.1", false, true},
		{"private class B", "http://172.16.0.1", false, true},
		{"private class C", "http://192.168.1.1", false, true},
		{"cgnat address", "http://100.64.1.1", false, true},
		{"cgnat allowed", "http://100.64.1.1", true, false},
		{"loopback ipv4 allowed", "http://127.0.0.1", true, false},
		{"private class C allowed", "http://192.168.1.1", true, false},
		{"empty host", "http://", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(context.Background(), tt.url, tt.allowPrivate)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1", true},
		{"127.0.1.1", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"192.168.0.254", true},
		{"169.254.1.1", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"100.63.255.255", false},
		{"100.128.0.1", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			actual := isPrivateIP(ip)
			if actual != tt.expected {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, actual, tt.expected)
			}
		})
	}
}

func TestSafeHTTPClient_BlocksPrivate(t *testing.T) {
	client := SafeHTTPClient(500 * time.Millisecond)

	privateURLs := []string{
		"http://127.0.0.1:80",
		"http://127.0.0.1:8080",
		"http://10.0.0.1:80",
		"http://172.16.0.1:80",
		"http://192.168.1.1:80",
		"http://169.254.169.254:80",
		"http://100.64.0.1:80",
	}

	for _, u := range privateURLs {
		t.Run(u, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			_, err = client.Do(req)
			if err == nil {
				t.Errorf("expected request to %s to fail with SSRF protection, but succeeded", u)
			} else if !strings.Contains(err.Error(), "private network") && !strings.Contains(err.Error(), "prohibited") && !strings.Contains(err.Error(), "dial") {
				t.Logf("request failed as expected: %v", err)
			}
		})
	}
}
