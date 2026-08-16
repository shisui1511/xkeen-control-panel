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
			name: "unix domain socket config",
			yaml: `
external-controller-unix: /opt/var/run/mihomo.sock
secret: "unix-secret"
`,
			wantCtrl:   "/opt/var/run/mihomo.sock",
			wantSecret: "unix-secret",
		},
		{
			name: "both unix and tcp configured - unix takes precedence",
			yaml: `
external-controller: 127.0.0.1:9090
external-controller-unix: /opt/var/run/mihomo.sock
secret: "dual-secret"
`,
			wantCtrl:   "/opt/var/run/mihomo.sock",
			wantSecret: "dual-secret",
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

func TestMihomoService_ParseControllerConfig(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantType     string
		wantTarget   string
		wantSecret   string
		wantInsecure bool
		wantErr      bool
	}{
		{
			name: "unix domain socket",
			yaml: `
external-controller-unix: "/opt/var/run/mihomo.sock"
secret: "sec123"
`,
			wantType:     "unix",
			wantTarget:   "/opt/var/run/mihomo.sock",
			wantSecret:   "sec123",
			wantInsecure: false,
		},
		{
			name: "insecure 0.0.0.0 tcp",
			yaml: `
external-controller: 0.0.0.0:9090
secret: "sec123"
`,
			wantType:     "tcp",
			wantTarget:   "0.0.0.0:9090",
			wantSecret:   "sec123",
			wantInsecure: true,
		},
		{
			name: "insecure :port tcp",
			yaml: `
external-controller: :9090
`,
			wantType:     "tcp",
			wantTarget:   ":9090",
			wantSecret:   "",
			wantInsecure: true,
		},
		{
			name: "secure 127.0.0.1 tcp",
			yaml: `
external-controller: 127.0.0.1:9090
secret: "sec123"
`,
			wantType:     "tcp",
			wantTarget:   "127.0.0.1:9090",
			wantSecret:   "sec123",
			wantInsecure: false,
		},
		{
			name: "no controller",
			yaml: `
port: 7890
`,
			wantType:     "none",
			wantTarget:   "",
			wantSecret:   "",
			wantInsecure: false,
		},
		{
			name: "relative unix domain socket resolves to config dir",
			yaml: `
external-controller-unix: ./mihomo-api.sock
secret: "sec123"
`,
			wantType:     "unix",
			wantTarget:   "__CONFIG_DIR__/mihomo-api.sock",
			wantSecret:   "sec123",
			wantInsecure: false,
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

			wantTarget := tt.wantTarget
			if strings.HasPrefix(wantTarget, "__CONFIG_DIR__") {
				wantTarget = filepath.Join(tmpDir, strings.TrimPrefix(wantTarget, "__CONFIG_DIR__/"))
			}

			svc := NewMihomoService("", "", tmpDir)
			info, err := svc.ParseControllerConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseControllerConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if info.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", info.Type, tt.wantType)
			}
			if info.Target != wantTarget {
				t.Errorf("Target = %q, want %q", info.Target, wantTarget)
			}
			if info.Secret != tt.wantSecret {
				t.Errorf("Secret = %q, want %q", info.Secret, tt.wantSecret)
			}
			if info.IsInsecure != tt.wantInsecure {
				t.Errorf("IsInsecure = %v, want %v", info.IsInsecure, tt.wantInsecure)
			}
		})
	}
}

func TestMihomoService_ValidateMihomoConfig(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantValid     bool
		wantErrCodes  []string
		wantWarnCodes []string
		wantErr       bool
	}{
		{
			name: "full valid config with 127.0.0.1",
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
			wantValid:     true,
			wantErrCodes:  []string{},
			wantWarnCodes: []string{},
		},
		{
			name: "full valid config with unix domain socket",
			yaml: `
external-controller-unix: /opt/var/run/mihomo.sock
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
			wantValid:     true,
			wantErrCodes:  []string{},
			wantWarnCodes: []string{},
		},
		{
			name: "insecure external-controller 0.0.0.0 produces warning",
			yaml: `
external-controller: 0.0.0.0:9090
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
			wantValid:     true,
			wantErrCodes:  []string{},
			wantWarnCodes: []string{"insecure_external_controller"},
		},
		{
			name: "insecure external-controller :port produces warning",
			yaml: `
external-controller: ":9090"
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
			wantValid:     true,
			wantErrCodes:  []string{},
			wantWarnCodes: []string{"insecure_external_controller"},
		},
		{
			name: "missing external-controller and unix socket",
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

func TestMihomoService_EnsureSocketDirAndCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	sockDir := filepath.Join(tmpDir, "run")
	sockPath := filepath.Join(sockDir, "mihomo.sock")

	configYAML := `
external-controller-unix: ` + sockPath + `
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	svc := NewMihomoService("/nonexistent/bin", "", tmpDir)

	if err := svc.EnsureSocketDir(); err != nil {
		t.Fatalf("EnsureSocketDir failed: %v", err)
	}
	if fi, err := os.Stat(sockDir); err != nil || !fi.IsDir() {
		t.Fatalf("expected socket dir %s to exist as directory", sockDir)
	}

	// Create dummy socket file
	if err := os.WriteFile(sockPath, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	// Cleanup when process is stopped should remove socket
	if err := svc.CleanupStaleSocket(); err != nil {
		t.Fatalf("CleanupStaleSocket failed: %v", err)
	}
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatalf("expected socket file %s to be deleted", sockPath)
	}
}
