package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestMihomoProfileHandlers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "mihomo")
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles", "default.yaml"), []byte("mode: rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}

	api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
	svc := services.NewMihomoProfileService(dir, filepath.Join(root, "xcp"))
	api.SetMihomoProfileService(svc)
	// No core in tests: skip validation and restarts.
	svc.Validate = func(string) error { return nil }
	svc.CoreActive = func() bool { return false }

	call := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if path == "/api/mihomo/profiles" {
			api.MihomoProfiles(rec, req)
		} else {
			api.MihomoProfileAction(rec, req)
		}
		return rec
	}

	if rec := call(http.MethodGet, "/api/mihomo/profiles", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":"default"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/create", `{"name":"work"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"name":"work"`) {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/create", `{"name":"../etc"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad name: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/delete", `{"name":"default"}`); rec.Code != http.StatusConflict {
		t.Fatalf("delete active: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/activate", `{"name":"work"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"active":"work"`) {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPost, "/api/mihomo/profiles/activate", `{"name":"nope"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("activate missing: %d", rec.Code)
	}
	if rec := call(http.MethodGet, "/api/mihomo/profiles/create", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET action: %d", rec.Code)
	}
}

// TestMihomoProfileActivate_CoreActiveByApplier: активация профиля перезапускает
// ядро, только если Mihomo — активное ядро и его процесс может работать (unknown
// считается запущенным). Иначе ответ несёт outcome, а откат при нездоровом ядре
// работает как раньше.
func TestMihomoProfileActivate_CoreActiveByApplier(t *testing.T) {
	cases := []struct {
		name         string
		configured   string
		mihomoStatus string
		healthy      bool
		wantRestarts int32
		wantOutcome  string
		wantRestart  bool
		wantRollback bool
		wantActive   string
	}{
		{name: "mihomo running", configured: "mihomo", mihomoStatus: "running", healthy: true, wantRestarts: 1, wantRestart: true, wantActive: "work"},
		{name: "mihomo unknown counts as running", configured: "mihomo", mihomoStatus: "unknown", healthy: true, wantRestarts: 1, wantRestart: true, wantActive: "work"},
		{name: "mihomo stopped", configured: "mihomo", mihomoStatus: "stopped", healthy: true, wantOutcome: "saved_kernel_stopped", wantActive: "work"},
		{name: "xray active", configured: "xray", mihomoStatus: "running", healthy: true, wantOutcome: "saved_kernel_inactive", wantActive: "work"},
		{name: "unhealthy core rolls back", configured: "mihomo", mihomoStatus: "running", healthy: false, wantRestarts: 2, wantRestart: true, wantRollback: true, wantActive: "default"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "mihomo")
			if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, n := range []string{"default", "work"} {
				if err := os.WriteFile(filepath.Join(dir, "profiles", n+".yaml"), []byte("mode: rule\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(filepath.Join(dir, "profiles", "default.yaml"), filepath.Join(dir, "config.yaml")); err != nil {
				t.Fatal(err)
			}

			api := &API{cfg: &config.Config{MihomoConfigDir: dir}}
			api.kernelApplier = services.NewKernelApplierFunc(
				func(name string) string {
					switch name {
					case "mihomo":
						return tc.mihomoStatus
					case "xray":
						return "running"
					}
					return "not_installed"
				},
				func() string { return tc.configured },
				func() (string, error) { return "", nil },
			)
			svc := services.NewMihomoProfileService(dir, filepath.Join(root, "xcp"))
			api.SetMihomoProfileService(svc)
			svc.Validate = func(string) error { return nil }
			var restarts int32
			svc.Restart = func() error {
				atomic.AddInt32(&restarts, 1)
				return nil
			}
			svc.Healthy = func() bool { return tc.healthy }

			rec := httptest.NewRecorder()
			api.MihomoProfileAction(rec, httptest.NewRequest(http.MethodPost, "/api/mihomo/profiles/activate", strings.NewReader(`{"name":"work"}`)))
			if rec.Code != http.StatusOK {
				t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
			}
			var env struct {
				Data struct {
					Active     string `json:"active"`
					Restarted  bool   `json:"restarted"`
					RolledBack bool   `json:"rolled_back"`
					Outcome    string `json:"outcome"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode: %v: %s", err, rec.Body.String())
			}
			if got := atomic.LoadInt32(&restarts); got != tc.wantRestarts {
				t.Errorf("restarts = %d, want %d", got, tc.wantRestarts)
			}
			if env.Data.Outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", env.Data.Outcome, tc.wantOutcome)
			}
			if env.Data.Restarted != tc.wantRestart {
				t.Errorf("restarted = %t, want %t", env.Data.Restarted, tc.wantRestart)
			}
			if env.Data.RolledBack != tc.wantRollback {
				t.Errorf("rolled_back = %t, want %t", env.Data.RolledBack, tc.wantRollback)
			}
			if env.Data.Active != tc.wantActive {
				t.Errorf("active = %q, want %q", env.Data.Active, tc.wantActive)
			}
		})
	}
}
