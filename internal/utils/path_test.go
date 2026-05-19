package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathValidatorValidPaths(t *testing.T) {
	base := t.TempDir()
	xrayDir := filepath.Join(base, "opt", "etc", "xray")
	mihomoDir := filepath.Join(base, "opt", "etc", "mihomo")
	logDir := filepath.Join(base, "opt", "var", "log")

	for _, d := range []string{
		filepath.Join(xrayDir, "configs"),
		mihomoDir,
		logDir,
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	validator := NewPathValidator([]string{xrayDir, mihomoDir, logDir})

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "exact root match",
			path: xrayDir,
			want: xrayDir,
		},
		{
			name: "subdirectory",
			path: filepath.Join(xrayDir, "configs", "config.json"),
			want: filepath.Join(xrayDir, "configs", "config.json"),
		},
		{
			name: "path with dots",
			path: filepath.Join(xrayDir, ".", "configs", "config.json"),
			want: filepath.Join(xrayDir, "configs", "config.json"),
		},
		{
			name: "mihomo path",
			path: filepath.Join(mihomoDir, "config.yaml"),
			want: filepath.Join(mihomoDir, "config.yaml"),
		},
		{
			name: "log path",
			path: filepath.Join(logDir, "xkeen.log"),
			want: filepath.Join(logDir, "xkeen.log"),
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
	base := t.TempDir()
	xrayDir := filepath.Join(base, "opt", "etc", "xray")
	mihomoDir := filepath.Join(base, "opt", "etc", "mihomo")
	etcDir := filepath.Join(base, "etc")

	for _, d := range []string{xrayDir, mihomoDir, etcDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	validator := NewPathValidator([]string{xrayDir, mihomoDir})

	tests := []struct {
		name string
		path string
	}{
		{
			name: "path traversal up",
			path: filepath.Join(xrayDir, "..", "..", "..", "etc", "passwd"),
		},
		{
			name: "completely different path",
			path: filepath.Join(etcDir, "passwd"),
		},
		{
			name: "path traversal with dots",
			path: filepath.Join(xrayDir, "configs", "..", "..", "..", "..", "..", "..", "etc", "passwd"),
		},
		{
			name: "not allowed root",
			path: filepath.Join(base, "opt", "etc", "other", "file.txt"),
		},
		{
			name: "similar but not exact prefix",
			path: filepath.Join(base, "opt", "etc", "xray-other", "config.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Validate(tt.path)
			if err == nil {
				t.Errorf("Validate() expected error, got nil for path %q", tt.path)
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
	base := t.TempDir()
	xrayDir := filepath.Join(base, "opt", "etc", "xray")
	configsDir := filepath.Join(xrayDir, "configs")
	if err := os.MkdirAll(configsDir, 0755); err != nil {
		t.Fatal(err)
	}

	validator := NewPathValidator([]string{xrayDir})

	// Path with multiple slashes should be cleaned
	got, err := validator.Validate(xrayDir + "//configs///config.json")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	expected := filepath.Clean(filepath.Join(xrayDir, "configs", "config.json"))
	if got != expected {
		t.Errorf("Validate() = %v, want %v", got, expected)
	}
}

// T005: Symlink tests — EvalSymlinks behaviour

// (a) Symlink on parent directory pointing outside AllowedRoots → error
func TestPathValidator_SymlinkEscape(t *testing.T) {
	base := t.TempDir()

	// Create allowed/ and secret/ directories
	allowedDir := filepath.Join(base, "allowed")
	secretDir := filepath.Join(base, "secret")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create symlink: allowed/link -> ../secret (absolute path)
	linkPath := filepath.Join(allowedDir, "link")
	if err := os.Symlink(secretDir, linkPath); err != nil {
		t.Fatal(err)
	}

	validator := NewPathValidator([]string{allowedDir})

	// Attempt to access allowed/link/file.json — symlink points outside allowedDir
	_, err := validator.Validate(filepath.Join(linkPath, "file.json"))
	if err == nil {
		t.Error("Validate() expected error for symlink escaping AllowedRoots, got nil")
	}
}

// (b) Correct path without symlinks → passes
func TestPathValidator_NoSymlink_Valid(t *testing.T) {
	base := t.TempDir()
	allowedDir := filepath.Join(base, "allowed")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatal(err)
	}

	validator := NewPathValidator([]string{allowedDir})

	// File does not exist yet (new file write scenario) — parent dir does exist
	got, err := validator.Validate(filepath.Join(allowedDir, "config.json"))
	if err != nil {
		t.Errorf("Validate() unexpected error = %v", err)
	}
	if got == "" {
		t.Error("Validate() returned empty path")
	}
}

// (c) Symlink chain where parent symlink points outside → error
func TestPathValidator_SymlinkChain_NonExistentTarget(t *testing.T) {
	base := t.TempDir()

	allowedDir := filepath.Join(base, "allowed")
	outsideDir := filepath.Join(base, "outside")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Symlink inside allowed/ pointing to outside
	linkPath := filepath.Join(allowedDir, "evil")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatal(err)
	}

	validator := NewPathValidator([]string{allowedDir})

	// evil/nonexistent.json — parent resolves to outsideDir which is not allowed
	_, err := validator.Validate(filepath.Join(linkPath, "nonexistent.json"))
	if err == nil {
		t.Error("Validate() expected error for symlink chain escaping AllowedRoots, got nil")
	}
}
