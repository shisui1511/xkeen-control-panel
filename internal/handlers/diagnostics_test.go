package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestSanitizeYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "redact sensitive keys",
			input: `
proxies:
  - name: "proxy1"
    type: shadowsocks
    password: "secretpassword"
    secret: "mysecret"
    uuid: "1234-abcd"
    public-key: "pubkey123"
`,
			expected: `*REDACTED*`,
		},
		{
			name:     "invalid yaml returns as is",
			input:    `[invalid`,
			expected: `[invalid`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBytes, err := sanitizeYAML([]byte(tc.input))
			if err != nil {
				t.Fatalf("sanitizeYAML failed: %v", err)
			}
			got := string(gotBytes)
			if tc.name == "redact sensitive keys" {
				// verify keys are redacted
				for _, key := range []string{"secretpassword", "mysecret", "1234-abcd", "pubkey123"} {
					if bytes.Contains(gotBytes, []byte(key)) {
						t.Errorf("expected sensitive key %q to be redacted, got: %s", key, got)
					}
				}
				if !bytes.Contains(gotBytes, []byte("*REDACTED*")) {
					t.Errorf("expected output to contain *REDACTED*, got: %s", got)
				}
			} else {
				if got != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, got)
				}
			}
		})
	}
}

func TestSanitizeJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "redact sensitive keys",
			input: `
{
  "outbounds": [
    {
      "settings": {
        "vnext": [
          {
            "users": [
              {
                "id": "user-uuid-123",
                "secret": "user-secret",
                "password": "user-password",
                "publicKey": "user-pubkey",
                "password_hash": "my-hash-val",
                "passwordHash": "my-other-hash-val"
              }
            ]
          }
        ]
      }
    }
  ]
}
`,
			expected: `*REDACTED*`,
		},
		{
			name:     "invalid json returns as is",
			input:    `{"unclosed`,
			expected: `{"unclosed`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBytes, err := sanitizeJSON([]byte(tc.input))
			if err != nil {
				t.Fatalf("sanitizeJSON failed: %v", err)
			}
			got := string(gotBytes)
			if tc.name == "redact sensitive keys" {
				for _, key := range []string{"user-uuid-123", "user-secret", "user-password", "user-pubkey", "my-hash-val", "my-other-hash-val"} {
					if bytes.Contains(gotBytes, []byte(key)) {
						t.Errorf("expected sensitive key %q to be redacted, got: %s", key, got)
					}
				}
				if !bytes.Contains(gotBytes, []byte("*REDACTED*")) {
					t.Errorf("expected output to contain *REDACTED*, got: %s", got)
				}
			} else {
				if got != tc.expected {
					t.Errorf("expected %q, got %q", tc.expected, got)
				}
			}
		})
	}
}

func TestSanitizeNested(t *testing.T) {
	yamlInput := `
nested:
  array:
    - map:
        secret: "nested-secret"
`
	gotBytes, err := sanitizeYAML([]byte(yamlInput))
	if err != nil {
		t.Fatalf("sanitizeYAML failed: %v", err)
	}
	if bytes.Contains(gotBytes, []byte("nested-secret")) {
		t.Errorf("expected nested-secret to be redacted, got: %s", string(gotBytes))
	}
	if !bytes.Contains(gotBytes, []byte("*REDACTED*")) {
		t.Errorf("expected output to contain *REDACTED*, got: %s", string(gotBytes))
	}

	jsonInput := `
{
  "nested": {
    "array": [
      {
        "map": {
          "secret": "nested-secret-json"
        }
      }
    ]
  }
}
`
	gotBytesJSON, err := sanitizeJSON([]byte(jsonInput))
	if err != nil {
		t.Fatalf("sanitizeJSON failed: %v", err)
	}
	if bytes.Contains(gotBytesJSON, []byte("nested-secret-json")) {
		t.Errorf("expected nested-secret-json to be redacted, got: %s", string(gotBytesJSON))
	}
	if !bytes.Contains(gotBytesJSON, []byte("*REDACTED*")) {
		t.Errorf("expected output to contain *REDACTED*, got: %s", string(gotBytesJSON))
	}
}

func TestSanitizeExcludesSubscriptions(t *testing.T) {
	tests := []struct {
		path   string
		expect bool
	}{
		{"/opt/etc/xcp/subscriptions/foo.yaml", true},
		{"/opt/etc/xcp/subscriptions/bar.txt", true},
		{"/opt/etc/mihomo/config.yaml", false},
		{"/opt/etc/xray/configs/05_outbounds.json", false},
		{"somefile.txt", true},
		{"/opt/etc/xcp/subscriptions/nested/sub.json", true},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := shouldExcludeFromDiagnostics(tc.path)
			if got != tc.expect {
				t.Errorf("shouldExcludeFromDiagnostics(%q) = %v, want %v", tc.path, got, tc.expect)
			}
		})
	}
}

func TestDiagnosticsAuth(t *testing.T) {
	api := &API{}
	// Verify method restriction
	req := httptest.NewRequest(http.MethodPost, "/api/system/diagnostics", nil)
	w := httptest.NewRecorder()
	api.DiagnosticsDownload(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestDiagnosticsDownload(t *testing.T) {
	tmpDir := t.TempDir()
	api := newTestAPI(t, tmpDir)

	// Set configuration paths
	logPath := filepath.Join(tmpDir, "test.log")
	if err := os.WriteFile(logPath, []byte("test log line"), 0644); err != nil {
		t.Fatal(err)
	}
	api.cfg.LogPath = logPath
	api.cfg.LogSources = []string{logPath}
	api.cfg.XRayConfigDir = filepath.Join(tmpDir, "xray")
	api.cfg.MihomoConfigDir = filepath.Join(tmpDir, "mihomo")
	api.cfg.DataDir = filepath.Join(tmpDir, "xcp")
	api.cfg.ConfigPath = filepath.Join(api.cfg.DataDir, "config.json")
	api.cfg.AllowedRoots = append(api.cfg.AllowedRoots, tmpDir, api.cfg.XRayConfigDir, api.cfg.MihomoConfigDir, api.cfg.DataDir)
	api.pathVal = utils.NewPathValidator(api.cfg.AllowedRoots)

	// Create directories and config files
	if err := os.MkdirAll(api.cfg.XRayConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(api.cfg.MihomoConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(api.cfg.DataDir, 0755); err != nil {
		t.Fatal(err)
	}

	xrayConfig := filepath.Join(api.cfg.XRayConfigDir, "config.json")
	if err := os.WriteFile(xrayConfig, []byte(`{"id": "user-secret-id", "public-key": "not-redacted"}`), 0644); err != nil {
		t.Fatal(err)
	}

	mihomoConfig := filepath.Join(api.cfg.MihomoConfigDir, "config.yaml")
	if err := os.WriteFile(mihomoConfig, []byte("secret: \"my-secret\"\npassword: \"my-pass\""), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file in subscriptions to ensure it is excluded
	subDir := filepath.Join(api.cfg.DataDir, "subscriptions")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	subFile := filepath.Join(subDir, "subscription.yaml")
	if err := os.WriteFile(subFile, []byte("sensitive: data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Request diagnostics download
	req := httptest.NewRequest(http.MethodGet, "/api/system/diagnostics", nil)
	rr := httptest.NewRecorder()

	api.DiagnosticsDownload(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/gzip" {
		t.Errorf("expected Content-Type application/gzip, got %s", contentType)
	}

	// Parse the tar.gz stream and verify contents
	gr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	filesFound := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar entry: %v", err)
		}
		var content bytes.Buffer
		if _, err := io.Copy(&content, tr); err != nil {
			t.Fatalf("failed to copy tar content: %v", err)
		}
		filesFound[hdr.Name] = content.String()
	}

	// Verify that expected files exist and are sanitized
	if _, ok := filesFound["logs/test.log"]; !ok {
		t.Error("expected logs/test.log to be in the archive")
	}

	if xrayBody, ok := filesFound["configs/xray/config.json"]; ok {
		if strings.Contains(xrayBody, "user-secret-id") {
			t.Error("expected xray config secret to be redacted")
		}
		if !strings.Contains(xrayBody, "*REDACTED*") {
			t.Error("expected xray config to contain REDACTED placeholder")
		}
	} else {
		t.Error("expected configs/xray/config.json to be in the archive")
	}

	if mihomoBody, ok := filesFound["configs/mihomo/config.yaml"]; ok {
		if strings.Contains(mihomoBody, "my-secret") || strings.Contains(mihomoBody, "my-pass") {
			t.Error("expected mihomo config secrets to be redacted")
		}
	} else {
		t.Error("expected configs/mihomo/config.yaml to be in the archive")
	}

	// Verify subscription files are excluded
	for name := range filesFound {
		if strings.Contains(name, "subscription.yaml") || strings.Contains(name, "subscriptions") {
			t.Errorf("expected subscription file %s to be excluded", name)
		}
	}

	// Verify iptables-rules.txt exists
	if _, ok := filesFound["iptables-rules.txt"]; !ok {
		t.Error("expected iptables-rules.txt to be in the archive")
	}
}

func TestAddFileToTar(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := addFileToTar(tw, "test.txt", []byte("hello world"))
	if err != nil {
		t.Fatalf("addFileToTar failed: %v", err)
	}

	tw.Close()
	gw.Close()

	// Verify we can read it back
	gr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader failed: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar reader next failed: %v", err)
	}

	if hdr.Name != "test.txt" {
		t.Errorf("expected file name test.txt, got %s", hdr.Name)
	}

	content := make([]byte, hdr.Size)
	_, err = io.ReadFull(tr, content)
	if err != nil {
		t.Fatalf("tar read content failed: %v", err)
	}

	if string(content) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(content))
	}
}
