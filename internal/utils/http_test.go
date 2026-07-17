package utils

import (
	"context"
	"testing"
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
		{"loopback ipv4", "http://127.0.0.1", false, true},
		{"loopback ipv4 with port", "http://127.0.0.1:8080", false, true},
		{"loopback localhost", "http://localhost", false, true},
		{"private class A", "http://10.0.0.1", false, true},
		{"private class B", "http://172.16.0.1", false, true},
		{"private class C", "http://192.168.1.1", false, true},
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
