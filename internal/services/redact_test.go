package services

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	input := "\x1b[36mINFO\x1b[0m \x1b[32m[DNS]\x1b[0m query google.com"
	expected := "INFO [DNS] query google.com"
	got := StripANSI(input)
	if got != expected {
		t.Errorf("StripANSI() = %q, want %q", got, expected)
	}
}

func TestStripCarriageReturns(t *testing.T) {
	input := "Downloading 50%\rDownloading 100%\r\nComplete"
	expected := "Downloading 50%Downloading 100%\nComplete"
	got := StripCarriageReturns(input)
	if got != expected {
		t.Errorf("StripCarriageReturns() = %q, want %q", got, expected)
	}
}

func TestNormalizeTimestamps(t *testing.T) {
	input := "1970-01-01T00:00:05Z kernel starting"
	got := NormalizeTimestamps(input)
	if !strings.Contains(got, "[NTP:1970]") {
		t.Errorf("NormalizeTimestamps() failed to replace 1970 date: got %q", got)
	}
}

func TestRedactSensitiveText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		omits    []string
	}{
		{
			name:     "URL tokens",
			input:    "Fetching https://sub.provider.com/api?token=secret123456&key=mykey7890",
			contains: []string{"token=*REDACTED*", "key=*REDACTED*"},
			omits:    []string{"secret123456", "mykey7890"},
		},
		{
			name:     "Bearer authorization",
			input:    "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.t-ID1UR",
			contains: []string{"Bearer *REDACTED*"},
			omits:    []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:     "Reality keys and passwords",
			input:    `{"privateKey": "uG8192348123984712=", "password": "supersecretpassword", "shortId": "abcd1234"}`,
			contains: []string{`"privateKey": "*REDACTED*"`, `"password": "*REDACTED*"`, `"shortId": "*REDACTED*"`},
			omits:    []string{"uG8192348123984712=", "supersecretpassword", "abcd1234"},
		},
		{
			name:     "Private LAN IP addresses",
			input:    "TCP connection from 192.168.1.105:54321 to 1.1.1.1:443 via 172.16.0.1",
			contains: []string{"192.168.***.***", "172.16.***.***", "1.1.1.1:443"},
			omits:    []string{"192.168.1.105", "172.16.0.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSensitiveText(tt.input)
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("RedactSensitiveText() result %q does not contain %q", got, c)
				}
			}
			for _, o := range tt.omits {
				if strings.Contains(got, o) {
					t.Errorf("RedactSensitiveText() result %q should NOT contain %q", got, o)
				}
			}
		})
	}
}

func TestRedactionReader(t *testing.T) {
	input := "Line 1: token=xyz123\nLine 2: 192.168.1.50\nLine 3: clean\n"
	reader := NewRedactionReader(strings.NewReader(input))
	var buf bytes.Buffer
	_, err := io.Copy(&buf, reader)
	if err != nil {
		t.Fatalf("io.Copy failed: %v", err)
	}

	res := buf.String()
	if strings.Contains(res, "xyz123") {
		t.Errorf("RedactionReader leaked token: %q", res)
	}
	if strings.Contains(res, "192.168.1.50") {
		t.Errorf("RedactionReader leaked IP: %q", res)
	}
	if !strings.Contains(res, "token=*REDACTED*") || !strings.Contains(res, "192.168.***.***") {
		t.Errorf("RedactionReader missing redaction markers: %q", res)
	}
}
