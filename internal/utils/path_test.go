package utils

import (
	"path/filepath"
	"testing"
)

func TestPathValidatorValidPaths(t *testing.T) {
	validator := NewPathValidator([]string{
		"/opt/etc/xray",
		"/opt/etc/mihomo",
		"/opt/var/log",
	})

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "exact root match",
			path: "/opt/etc/xray",
			want: "/opt/etc/xray",
		},
		{
			name: "subdirectory",
			path: "/opt/etc/xray/configs/config.json",
			want: "/opt/etc/xray/configs/config.json",
		},
		{
			name: "path with dots",
			path: "/opt/etc/xray/./configs/config.json",
			want: "/opt/etc/xray/configs/config.json",
		},
		{
			name: "mihomo path",
			path: "/opt/etc/mihomo/config.yaml",
			want: "/opt/etc/mihomo/config.yaml",
		},
		{
			name: "log path",
			path: "/opt/var/log/xkeen.log",
			want: "/opt/var/log/xkeen.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validator.Validate(tt.path)
			if err != nil {
				t.Errorf("Validate() error = %v, want nil", err)
				return
			}
			if got != tt.want {
				t.Errorf("Validate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathValidatorInvalidPaths(t *testing.T) {
	validator := NewPathValidator([]string{
		"/opt/etc/xray",
		"/opt/etc/mihomo",
	})

	tests := []struct {
		name string
		path string
	}{
		{
			name: "path traversal up",
			path: "/opt/etc/xray/../../../etc/passwd",
		},
		{
			name: "completely different path",
			path: "/etc/passwd",
		},
		{
			name: "path traversal with dots",
			path: "/opt/etc/xray/configs/../../../../../../etc/passwd",
		},
		{
			name: "not allowed root",
			path: "/opt/etc/other/file.txt",
		},
		{
			name: "similar but not exact prefix",
			path: "/opt/etc/xray-other/config.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Validate(tt.path)
			if err == nil {
				t.Errorf("Validate() expected error, got nil")
			}
		})
	}
}

func TestPathValidatorEmptyRoots(t *testing.T) {
	validator := NewPathValidator([]string{})

	_, err := validator.Validate("/opt/etc/xray/config.json")
	if err == nil {
		t.Error("Expected error with empty allowed roots")
	}
}

func TestPathValidatorCleanPath(t *testing.T) {
	validator := NewPathValidator([]string{"/opt/etc/xray"})

	// Путь с множественными слешами должен быть очищен
	got, err := validator.Validate("/opt/etc/xray//configs///config.json")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	expected := filepath.Clean("/opt/etc/xray/configs/config.json")
	if got != expected {
		t.Errorf("Validate() = %v, want %v", got, expected)
	}
}
