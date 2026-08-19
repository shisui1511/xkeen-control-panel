package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func TestMihomoGroups_DefaultConfig(t *testing.T) {
	tmpDir := t.TempDir()
	mihomoDir := filepath.Join(tmpDir, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		t.Fatal(err)
	}

	configYAML := `mixed-port: 7890
allow-lan: true
proxies:
  - name: direct
    type: direct
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - AUTO
  - name: AUTO
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - direct
  - name: DIRECT
    type: select
  - name: "FallBack"
    type: fallback
    proxies:
      - PROXY
`
	if err := os.WriteFile(filepath.Join(mihomoDir, "config.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	api := &API{
		cfg: &config.Config{
			MihomoConfigDir: mihomoDir,
			AllowedRoots:    []string{tmpDir},
		},
		pathVal:   utils.NewPathValidator([]string{tmpDir}),
		mihomoSvc: services.NewMihomoService("", "", mihomoDir),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/groups", nil)
	rr := httptest.NewRecorder()

	api.MihomoGroups(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp MihomoGroupsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	expected := []string{"PROXY", "AUTO", "FallBack"}
	if len(resp.Groups) != len(expected) {
		t.Fatalf("expected %d groups, got %d: %v", len(expected), len(resp.Groups), resp.Groups)
	}
	for i, exp := range expected {
		if resp.Groups[i] != exp {
			t.Errorf("at index %d: expected %q, got %q", i, exp, resp.Groups[i])
		}
	}
}

func TestMihomoGroups_CustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	customFile := filepath.Join(tmpDir, "custom-profile.yaml")

	yaml := `proxy-groups:
  - name: MyGroup1
    type: select
  - name: MyGroup2
    type: url-test
`
	if err := os.WriteFile(customFile, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	api := &API{
		cfg: &config.Config{
			MihomoConfigDir: tmpDir,
			AllowedRoots:    []string{tmpDir},
		},
		pathVal: utils.NewPathValidator([]string{tmpDir}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/groups?path="+customFile, nil)
	rr := httptest.NewRecorder()

	api.MihomoGroups(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp MihomoGroupsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(resp.Groups) != 2 || resp.Groups[0] != "MyGroup1" || resp.Groups[1] != "MyGroup2" {
		t.Errorf("unexpected groups: %v", resp.Groups)
	}
}

func TestMihomoGroups_MissingConfigReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	emptyDir := filepath.Join(tmpDir, "empty")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatal(err)
	}

	api := &API{
		cfg: &config.Config{
			MihomoConfigDir: emptyDir,
			AllowedRoots:    []string{tmpDir},
		},
		pathVal: utils.NewPathValidator([]string{tmpDir}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/groups", nil)
	rr := httptest.NewRecorder()

	api.MihomoGroups(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp MihomoGroupsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Groups) != 0 {
		t.Errorf("expected 0 groups, got %v", resp.Groups)
	}
}

func TestMihomoGroups_ForbiddenPath(t *testing.T) {
	tmpDir := t.TempDir()
	api := &API{
		cfg: &config.Config{
			MihomoConfigDir: tmpDir,
			AllowedRoots:    []string{tmpDir},
		},
		pathVal: utils.NewPathValidator([]string{tmpDir}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mihomo/groups?path=/etc/shadow", nil)
	rr := httptest.NewRecorder()

	api.MihomoGroups(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403 Forbidden, got %d", rr.Code)
	}
}

func TestMihomoGroups_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	api := &API{
		cfg:     &config.Config{},
		pathVal: utils.NewPathValidator([]string{tmpDir}),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/mihomo/groups", nil)
	rr := httptest.NewRecorder()

	api.MihomoGroups(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}
