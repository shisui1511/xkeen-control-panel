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
)

// TestCapabilities_MihomoOffline verifies that when the Mihomo API is not
// reachable, the capabilities endpoint returns reachable=false without error.
func TestCapabilities_MihomoOffline(t *testing.T) {
	// Use a guaranteed-unreachable URL (port 1 is generally closed).
	api := &API{
		cfg: &config.Config{
			MihomoAPIURL: "http://127.0.0.1:1",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()

	api.Capabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var envelope APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("expected success=true, got false: %v", envelope.Error)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var resp CapabilitiesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode capabilities data: %v", err)
	}

	if resp.Mihomo.Reachable {
		t.Error("expected Mihomo.Reachable=false when API is offline, got true")
	}
}

// TestCapabilities_MihomoOnline verifies that when the Mihomo API responds
// with 200 OK, the capabilities endpoint returns reachable=true.
func TestCapabilities_MihomoOnline(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	api := &API{
		cfg: &config.Config{
			MihomoAPIURL: ts.URL,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()

	api.Capabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var envelope APIResponse
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("expected success=true, got false: %v", envelope.Error)
	}
	data, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var resp CapabilitiesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode capabilities data: %v", err)
	}

	if !resp.Mihomo.Reachable {
		t.Error("expected Mihomo.Reachable=true when API is online, got false")
	}
}

// TestCapabilities_ActiveKernelWhenStopped: ни одно ядро не запущено —
// active_kernel берётся из init-скрипта XKeen, а не "none" (иначе DAT Manager
// скрывал все базы, а конструктор открывался без ядра).
func TestCapabilities_ActiveKernelWhenStopped(t *testing.T) {
	init := filepath.Join(t.TempDir(), "S05xkeen")
	if err := os.WriteFile(init, []byte("name_client=\"xray\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	xk := services.NewXKeenService(buildStubBinary(t, "XKeen is not running", 0), t.TempDir())
	xk.InitScript = init
	api := &API{cfg: &config.Config{MihomoAPIURL: "http://127.0.0.1:1"}, xkeenSvc: xk}

	rr := httptest.NewRecorder()
	api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	var envelope struct {
		Data CapabilitiesResponse `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ActiveKernel != "xray" {
		t.Errorf("active_kernel = %q, want xray", envelope.Data.ActiveKernel)
	}
}

// TestCapabilities_ApplyRestarts: предсказание рестарта при применении берётся
// у того же KernelApplier, что и решает при apply.
func TestCapabilities_ApplyRestarts(t *testing.T) {
	get := func(t *testing.T, statuses map[string]string) CapabilitiesResponse {
		t.Helper()
		xk := services.NewXKeenService(buildStubBinary(t, "XKeen is not running", 0), t.TempDir())
		api := &API{
			cfg:      &config.Config{MihomoAPIURL: "http://127.0.0.1:1"},
			xkeenSvc: xk,
			kernelApplier: services.NewKernelApplierFunc(
				func(name string) string { return statuses[name] },
				func() string { return "xray" },
				func() (string, error) {
					t.Error("рестарт не должен вызываться из capabilities")
					return "", nil
				},
			),
		}
		rr := httptest.NewRecorder()
		api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
		var envelope struct {
			Data CapabilitiesResponse `json:"data"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}

	running := get(t, map[string]string{"xray": "running", "mihomo": "running"})
	if !running.ApplyRestarts["xray"] || running.ApplyRestarts["mihomo"] {
		t.Errorf("xray running: apply_restarts = %v, want xray:true mihomo:false", running.ApplyRestarts)
	}

	stopped := get(t, map[string]string{"xray": "stopped", "mihomo": "stopped"})
	if stopped.ApplyRestarts["xray"] || stopped.ApplyRestarts["mihomo"] {
		t.Errorf("all stopped: apply_restarts = %v, want both false", stopped.ApplyRestarts)
	}
}

// TestCapabilities_ApplyRestartsWithoutApplier: без KernelApplier поле
// отсутствует, паники нет.
func TestCapabilities_ApplyRestartsWithoutApplier(t *testing.T) {
	xk := services.NewXKeenService(buildStubBinary(t, "XKeen is not running", 0), t.TempDir())
	api := &API{cfg: &config.Config{MihomoAPIURL: "http://127.0.0.1:1"}, xkeenSvc: xk}
	rr := httptest.NewRecorder()
	api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if _, ok := envelope.Data["apply_restarts"]; ok {
		t.Errorf("apply_restarts присутствует без applier: %s", envelope.Data["apply_restarts"])
	}
}
