package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMihomoService_New(t *testing.T) {
	svc := NewMihomoService("/opt/bin/mihomo", "/opt/sbin/xkeen", "/opt/etc/mihomo")
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.BinaryPath != "/opt/bin/mihomo" {
		t.Fatalf("expected BinaryPath '/opt/bin/mihomo', got %s", svc.BinaryPath)
	}
	if svc.XKeenPath != "/opt/sbin/xkeen" {
		t.Fatalf("expected XKeenPath '/opt/sbin/xkeen', got %s", svc.XKeenPath)
	}
	if svc.ConfigDir != "/opt/etc/mihomo" {
		t.Fatalf("expected ConfigDir '/opt/etc/mihomo', got %s", svc.ConfigDir)
	}
}

func TestMihomoService_Status_Stopped(t *testing.T) {
	svc := NewMihomoService("/nonexistent/binary", "", "/nonexistent/dir")

	// Create dummy pidof that returns empty string
	tmpDir := t.TempDir()
	pidofPath := filepath.Join(tmpDir, "pidof")
	os.WriteFile(pidofPath, []byte("#!/bin/sh\nexit 1\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status != "stopped" {
		t.Fatalf("expected 'stopped', got %s", status)
	}
}

func TestMihomoService_Status_Running(t *testing.T) {
	svc := NewMihomoService("/opt/bin/mihomo", "", "/opt/etc/mihomo")

	// Redirect procDir to a temp dir so isShortLivedOrHelperProcess won't fail to read cmdline
	tmpDir := t.TempDir()
	origProcDir := procDir
	procDir = tmpDir
	defer func() { procDir = origProcDir }()

	// Create a dummy cmdline for PID 12345
	pidDir := filepath.Join(tmpDir, "12345")
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "cmdline"), []byte("/opt/bin/mihomo\x00-c\x00/opt/etc/mihomo/config.yaml\x00"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create dummy pidof that returns a pid
	binDir := t.TempDir()
	pidofPath := filepath.Join(binDir, "pidof")
	os.WriteFile(pidofPath, []byte("#!/bin/sh\necho \"12345\"\n"), 0755)

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+oldPath)
	defer os.Setenv("PATH", oldPath)

	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !strings.Contains(status, "running (pid: 12345)") {
		t.Fatalf("expected 'running', got %s", status)
	}
}

func TestMihomoService_ParseConfig(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantCtrl   string
		wantSecret string
		wantErr    bool
	}{
		{
			name: "standard config with double quotes",
			yaml: `
port: 7890
socks-port: 7891
external-controller: 127.0.0.1:9090
secret: "my-secret-token"
`,
			wantCtrl:   "127.0.0.1:9090",
			wantSecret: "my-secret-token",
		},
		{
			name: "single quotes and comments",
			yaml: `
# This is a comment
external-controller: '127.0.0.1:9095' # api port
secret: 'another_secret' # token here
`,
			wantCtrl:   "127.0.0.1:9095",
			wantSecret: "another_secret",
		},
		{
			name: "commented out keys",
			yaml: `
# external-controller: 127.0.0.1:9090
# secret: secret
external-controller: 127.0.0.1:9091
secret: real_secret
`,
			wantCtrl:   "127.0.0.1:9091",
			wantSecret: "real_secret",
		},
		{
			name: "external-controller-secret key",
			yaml: `
external-controller: 127.0.0.1:9092
external-controller-secret: super_secret
`,
			wantCtrl:   "127.0.0.1:9092",
			wantSecret: "super_secret",
		},
		{
			name: "missing keys",
			yaml: `
port: 7890
`,
			wantCtrl:   "",
			wantSecret: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			err := os.WriteFile(configPath, []byte(tt.yaml), 0644)
			if err != nil {
				t.Fatalf("failed to write config.yaml: %v", err)
			}

			svc := NewMihomoService("", "", tmpDir)
			ctrl, secret, err := svc.ParseConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if ctrl != tt.wantCtrl {
				t.Errorf("ParseConfig() ctrl = %q, want %q", ctrl, tt.wantCtrl)
			}
			if secret != tt.wantSecret {
				t.Errorf("ParseConfig() secret = %q, want %q", secret, tt.wantSecret)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		svc := NewMihomoService("", "", "/nonexistent/dir")
		_, _, err := svc.ParseConfig()
		if err == nil {
			t.Error("expected error for nonexistent file, got nil")
		}
	})
}

func TestMihomoService_ValidateMihomoConfig(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantValid    bool
		wantErrCodes []string
		wantWarnCodes []string
		wantErr      bool
	}{
		{
			name: "full valid config",
			yaml: `
external-controller: 127.0.0.1:9090
proxy-groups:
  - name: Proxy
    type: select
    proxies: []
rules:
  - MATCH,DIRECT
proxies:
  - name: test
    type: socks5
`,
			wantValid:    true,
			wantErrCodes: []string{},
			wantWarnCodes: []string{},
		},
		{
			name: "missing external-controller",
			yaml: `
proxy-groups:
  - name: Proxy
    type: select
    proxies: []
rules:
  - MATCH,DIRECT
proxies:
  - name: test
`,
			wantValid:    false,
			wantErrCodes: []string{"no_external_controller"},
		},
		{
			name: "empty external-controller",
			yaml: `
external-controller: ""
proxy-groups:
  - name: Proxy
    type: select
    proxies: []
rules:
  - MATCH,DIRECT
proxies:
  - name: test
`,
			wantValid:    false,
			wantErrCodes: []string{"no_external_controller"},
		},
		{
			name: "with external-controller but no proxy-groups",
			yaml: `
external-controller: 127.0.0.1:9090
rules:
  - MATCH,DIRECT
proxies:
  - name: test
`,
			wantValid:     true,
			wantErrCodes:  []string{},
			wantWarnCodes: []string{"no_proxy_groups"},
		},
		{
			name: "no proxies and no proxy-providers",
			yaml: `
external-controller: 127.0.0.1:9090
proxy-groups:
  - name: Proxy
rules:
  - MATCH,DIRECT
`,
			wantValid:     true,
			wantWarnCodes: []string{"no_proxies_or_providers"},
		},
		{
			name: "no rules",
			yaml: `
external-controller: 127.0.0.1:9090
proxy-groups:
  - name: Proxy
proxies:
  - name: test
`,
			wantValid:     true,
			wantWarnCodes: []string{"no_rules"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write config.yaml: %v", err)
			}

			svc := NewMihomoService("", "", tmpDir)
			result, err := svc.ValidateMihomoConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMihomoConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if result.Valid != tt.wantValid {
				t.Errorf("ValidateMihomoConfig() Valid = %v, want %v", result.Valid, tt.wantValid)
			}
			for _, code := range tt.wantErrCodes {
				found := false
				for _, issue := range result.Errors {
					if issue.Code == code {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error code %q not found in errors: %+v", code, result.Errors)
				}
			}
			for _, code := range tt.wantWarnCodes {
				found := false
				for _, issue := range result.Warnings {
					if issue.Code == code {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning code %q not found in warnings: %+v", code, result.Warnings)
				}
			}
		})
	}

	t.Run("missing config file returns error", func(t *testing.T) {
		svc := NewMihomoService("", "", "/nonexistent/dir")
		_, err := svc.ValidateMihomoConfig()
		if err == nil {
			t.Error("expected error for nonexistent config, got nil")
		}
	})
}
