package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

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

// newRawStatusCache — кэш статуса XKeen с заданным Raw. stale=true: второй
// опрос падает, снимок остаётся с прежним Raw, но помечен устаревшим.
func newRawStatusCache(t *testing.T, raw string, stale bool) *services.XKeenStatusCache {
	t.Helper()
	var calls atomic.Int32
	cache := services.NewXKeenStatusCacheFunc(
		func(context.Context) (string, error) {
			if calls.Add(1) > 1 {
				return "", errors.New("timeout exceeded")
			}
			return raw, nil
		},
		nil, nil, time.Hour,
	)
	cache.Start()
	t.Cleanup(cache.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cache.RefreshNow(ctx)
	if stale {
		cache.RefreshNow(ctx)
	}
	if got := cache.Snapshot(); got.Stale != stale || got.Raw != raw {
		t.Fatalf("подготовка снимка: stale=%v raw=%q", got.Stale, got.Raw)
	}
	return cache
}

// newConfiguredKernelAPI — API без запущенных процессов ядер, у XKeen
// настроенное ядро configured (name_client init-скрипта), статус — из кэша.
func newConfiguredKernelAPI(t *testing.T, configured string, cache *services.XKeenStatusCache) *API {
	t.Helper()
	initScript := filepath.Join(t.TempDir(), "S05xkeen")
	if err := os.WriteFile(initScript, []byte("name_client=\""+configured+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	xk := services.NewXKeenService(buildStubBinary(t, "XKeen is not running", 0), t.TempDir())
	xk.InitScript = initScript
	api := &API{cfg: &config.Config{MihomoAPIURL: "http://127.0.0.1:1"}, xkeenSvc: xk}
	api.SetXKeenStatusCache(cache)
	return api
}

func capabilitiesActiveKernel(t *testing.T, api *API) string {
	t.Helper()
	rr := httptest.NewRecorder()
	api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	var envelope struct {
		Data CapabilitiesResponse `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.ActiveKernel
}

// TestCapabilities_StaleRawIgnored: устаревший снимок `xkeen -status` не
// определяет активное ядро (после switch_kernel в нём ещё прежнее ядро) —
// берётся настроенное (G5-WR04).
func TestCapabilities_StaleRawIgnored(t *testing.T) {
	api := newConfiguredKernelAPI(t, "mihomo", newRawStatusCache(t, "xray is running", true))
	if got := capabilitiesActiveKernel(t, api); got != "mihomo" {
		t.Errorf("active_kernel = %q, want mihomo (устаревший Raw игнорируется)", got)
	}
}

// TestCapabilities_FreshRawUsed: свежий Raw без процессов определяет ядро
// раньше настроенного (запасной путь работает для свежих данных).
func TestCapabilities_FreshRawUsed(t *testing.T) {
	api := newConfiguredKernelAPI(t, "mihomo", newRawStatusCache(t, "xray is running", false))
	if got := capabilitiesActiveKernel(t, api); got != "xray" {
		t.Errorf("active_kernel = %q, want xray по свежему Raw", got)
	}
}

// TestCapabilities_BothWordsFallsBackToConfigured: в свежем Raw оба ядра —
// xray не выигрывает по порядку веток, решает настроенное ядро (G5-WR04).
func TestCapabilities_BothWordsFallsBackToConfigured(t *testing.T) {
	api := newConfiguredKernelAPI(t, "mihomo", newRawStatusCache(t, "xray and mihomo are running", false))
	if got := capabilitiesActiveKernel(t, api); got != "mihomo" {
		t.Errorf("active_kernel = %q, want mihomo (ConfiguredKernel при обоих словах)", got)
	}
}

// capabilitiesData разбирает ответ /api/capabilities вместе с «сырыми» полями.
func capabilitiesData(t *testing.T, api *API) (CapabilitiesResponse, map[string]json.RawMessage) {
	t.Helper()
	rr := httptest.NewRecorder()
	api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	var typed struct {
		Data CapabilitiesResponse `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &typed); err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	return typed.Data, raw.Data
}

// TestCapabilities_KernelConflict: оба процесса запущены — active_kernel "both",
// kernel_conflict true и running_kernels; один процесс — конфликта нет.
func TestCapabilities_KernelConflict(t *testing.T) {
	newAPI := func(states []services.KernelProcessState) *API {
		ksvc := services.NewKernelService(t.TempDir())
		ksvc.SetProcessStatesSource(func() []services.KernelProcessState { return states })
		api := &API{cfg: &config.Config{MihomoAPIURL: "http://127.0.0.1:1"}}
		api.SetKernelService(ksvc)
		return api
	}

	both, raw := capabilitiesData(t, newAPI([]services.KernelProcessState{
		{Name: "xray", Status: "running", PID: 10},
		{Name: "mihomo", Status: "running", PID: 11},
	}))
	if both.ActiveKernel != "both" || !both.KernelConflict {
		t.Errorf("оба running: active_kernel=%q kernel_conflict=%v, want both/true", both.ActiveKernel, both.KernelConflict)
	}
	if want := []string{"xray", "mihomo"}; !reflect.DeepEqual(both.RunningKernels, want) {
		t.Errorf("running_kernels = %v, want %v", both.RunningKernels, want)
	}
	if string(raw["kernel_conflict"]) != "true" {
		t.Errorf("kernel_conflict в JSON = %s, want true", raw["kernel_conflict"])
	}
	if both.XRay.GRPCReady {
		t.Error("grpc_ready при конфликте должен быть ложным")
	}

	one, raw := capabilitiesData(t, newAPI([]services.KernelProcessState{
		{Name: "xray", Status: "stopped"},
		{Name: "mihomo", Status: "running", PID: 11},
	}))
	if one.ActiveKernel != "mihomo" || one.KernelConflict {
		t.Errorf("один running: active_kernel=%q kernel_conflict=%v, want mihomo/false", one.ActiveKernel, one.KernelConflict)
	}
	if string(raw["kernel_conflict"]) != "false" {
		t.Errorf("kernel_conflict всегда присутствует в JSON, got %s", raw["kernel_conflict"])
	}
}

// TestCapabilities_XrayAPIAddr: при активном Xray и api-блоке в конфиге
// capabilities отдаёт адрес API (127.0.0.1:<порт>), иначе поле пусто.
func TestCapabilities_XrayAPIAddr(t *testing.T) {
	get := func(t *testing.T, confDir string, port int) XRayCapability {
		t.Helper()
		init := filepath.Join(t.TempDir(), "S05xkeen")
		if err := os.WriteFile(init, []byte("name_client=\"xray\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		xk := services.NewXKeenService(buildStubBinary(t, "XKeen is not running", 0), t.TempDir())
		xk.InitScript = init
		api := &API{
			cfg:      &config.Config{MihomoAPIURL: "http://127.0.0.1:1", XRayConfigDir: confDir, XRayAPIPort: port},
			xkeenSvc: xk,
		}
		rr := httptest.NewRecorder()
		api.Capabilities(rr, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
		var envelope struct {
			Data CapabilitiesResponse `json:"data"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.XRay
	}

	withAPI := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		body := `{"inbounds":[{"tag":"api","port":10085,"protocol":"dokodemo-door"}]}`
		if err := os.WriteFile(filepath.Join(dir, "03_inbounds.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("default port", func(t *testing.T) {
		x := get(t, withAPI(t), 0)
		if !x.GRPCReady || x.APIAddr != "127.0.0.1:10085" {
			t.Errorf("grpc_ready=%v api_addr=%q, want true 127.0.0.1:10085", x.GRPCReady, x.APIAddr)
		}
	})
	t.Run("configured port", func(t *testing.T) {
		x := get(t, withAPI(t), 10090)
		if !x.GRPCReady || x.APIAddr != "127.0.0.1:10090" {
			t.Errorf("grpc_ready=%v api_addr=%q, want true 127.0.0.1:10090", x.GRPCReady, x.APIAddr)
		}
	})
	t.Run("no api inbound", func(t *testing.T) {
		x := get(t, t.TempDir(), 10085)
		if x.GRPCReady || x.APIAddr != "" {
			t.Errorf("grpc_ready=%v api_addr=%q, want false empty", x.GRPCReady, x.APIAddr)
		}
	})
}
