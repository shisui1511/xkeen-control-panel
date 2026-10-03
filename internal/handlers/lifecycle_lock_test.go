package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// newLockedApplyTestAPI — API с production-проводкой: applier из SetKernelService
// ждёт именно api.lifecycleMu. Статусы процессов — xray запущен.
func newLockedApplyTestAPI(t *testing.T) *API {
	t.Helper()
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))
	api.SetKernelService(api.kernelSvc)
	api.kernelSvc.SetProcessStatesSource(func() []services.KernelProcessState {
		return []services.KernelProcessState{
			{Name: "xray", Status: "running", PID: 10},
			{Name: "mihomo", Status: "stopped"},
		}
	})
	return api
}

// Фоновое применение (подписки) ждёт идущую операцию жизненного цикла.
func TestLifecycleLock_BackgroundApplyWaitsForServiceOp(t *testing.T) {
	api := newLockedApplyTestAPI(t)
	if api.KernelApplier() == nil {
		t.Fatal("applier не собран")
	}

	api.lifecycleMu.Lock()
	done := make(chan services.ApplyResult, 1)
	go func() { done <- api.KernelApplier().Apply("xray") }()

	select {
	case res := <-done:
		t.Fatalf("Apply вернулся при удерживаемом lifecycleMu: %+v", res)
	case <-time.After(200 * time.Millisecond):
	}
	if log := api.xkeenSvc.GetRestartLog(); len(log) != 0 {
		t.Errorf("скрипт XKeen вызван при занятом замке: %+v", log)
	}

	api.lifecycleMu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Apply не вернулся после освобождения lifecycleMu")
	}
}

// ServiceControl action=apply с реальным applier под общим замком не блокирует сам себя.
func TestLifecycleLock_ServiceControlApplyNoSelfDeadlock(t *testing.T) {
	api := newLockedApplyTestAPI(t)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rr := httptest.NewRecorder()
		api.ServiceControl(rr, httptest.NewRequest(http.MethodPost, "/api/service/control?action=apply&kernel=xray", nil))
		done <- rr
	}()
	select {
	case rr := <-done:
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("самоблокировка: ServiceControl apply не ответил за 2 с")
	}
	if !api.lifecycleMu.TryLock() {
		t.Fatal("замок не освобождён после запроса")
	}
	api.lifecycleMu.Unlock()
}

// countingApplier — applier с запущенным xray и счётчиком рестартов.
func countingApplier(restarts *int32) *services.KernelApplier {
	return services.NewKernelApplierFunc(
		func(name string) string {
			if name == "xray" {
				return "running"
			}
			return "stopped"
		},
		func() string { return "xray" },
		func() (string, error) {
			atomic.AddInt32(restarts, 1)
			return "ok", nil
		},
	)
}

// lifecycleBypassCase — обходной путь рестарта: запрос, который должен ответить
// 409 при занятом замке, и проверка отсутствия побочных эффектов.
type lifecycleBypassCase struct {
	name string
	// setup собирает API и возвращает обработчик запроса и проверку «ничего не
	// изменилось» (вызывается после запроса при занятом замке).
	setup func(t *testing.T) (api *API, call func() *httptest.ResponseRecorder, unchanged func(t *testing.T))
}

func lifecycleBypassCases(restarts *int32) []lifecycleBypassCase {
	return []lifecycleBypassCase{
		{
			name: "xray grpc monitoring",
			setup: func(t *testing.T) (*API, func() *httptest.ResponseRecorder, func(*testing.T)) {
				tmpDir := t.TempDir()
				cfgPath := filepath.Join(tmpDir, "config.json")
				original := []byte(`{"inbounds":[{"tag":"socks-in","port":10808,"protocol":"socks"}]}`)
				if err := os.WriteFile(cfgPath, original, 0600); err != nil {
					t.Fatal(err)
				}
				api := &API{
					cfg:           &config.Config{XRayConfigDir: tmpDir, XRayAPIPort: 10085},
					pathVal:       utils.NewPathValidator([]string{tmpDir}),
					kernelApplier: countingApplier(restarts),
				}
				call := func() *httptest.ResponseRecorder {
					rr := httptest.NewRecorder()
					api.XrayGRPCMonitoring(rr, httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bytes.NewBufferString(`{"enabled": true}`)))
					return rr
				}
				unchanged := func(t *testing.T) {
					got, err := os.ReadFile(cfgPath)
					if err != nil || !bytes.Equal(got, original) {
						t.Errorf("config.json изменён при занятом замке: %v %q", err, got)
					}
				}
				return api, call, unchanged
			},
		},
		{
			name: "snapshot restore",
			setup: func(t *testing.T) (*API, func() *httptest.ResponseRecorder, func(*testing.T)) {
				api, _, configDir := newSnapshotTestAPI(t)
				api.kernelApplier = countingApplier(restarts)
				rec := httptest.NewRecorder()
				api.SnapshotCreate(rec, httptest.NewRequest(http.MethodPost, "/api/snapshots/create", nil))
				var meta services.SnapshotMeta
				if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil || meta.ID == "" {
					t.Fatalf("create snapshot: %v: %s", err, rec.Body.String())
				}
				samplePath := filepath.Join(configDir, "sample.txt")
				modified := []byte("modified-after-snapshot")
				if err := os.WriteFile(samplePath, modified, 0644); err != nil {
					t.Fatal(err)
				}
				call := func() *httptest.ResponseRecorder {
					rr := httptest.NewRecorder()
					api.SnapshotRestore(rr, httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/restore", nil))
					return rr
				}
				unchanged := func(t *testing.T) {
					got, err := os.ReadFile(samplePath)
					if err != nil || !bytes.Equal(got, modified) {
						t.Errorf("файл каталога затронут восстановлением при занятом замке: %v %q", err, got)
					}
				}
				return api, call, unchanged
			},
		},
		{
			name: "mihomo migrate socket",
			setup: func(t *testing.T) (*API, func() *httptest.ResponseRecorder, func(*testing.T)) {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.yaml")
				original := []byte("port: 7890\nexternal-controller: 0.0.0.0:9090\nsecret: \"test-secret\"\n")
				if err := os.WriteFile(configPath, original, 0644); err != nil {
					t.Fatal(err)
				}
				api := &API{
					cfg:       &config.Config{},
					mihomoSvc: services.NewMihomoService("", "", tmpDir),
				}
				call := func() *httptest.ResponseRecorder {
					rr := httptest.NewRecorder()
					api.MihomoMigrateSocket(rr, httptest.NewRequest(http.MethodPost, "/api/config/mihomo-migrate-socket", bytes.NewBufferString(`{"action": "apply"}`)))
					return rr
				}
				unchanged := func(t *testing.T) {
					got, err := os.ReadFile(configPath)
					if err != nil || !bytes.Equal(got, original) {
						t.Errorf("config.yaml изменён при занятом замке: %v %q", err, got)
					}
				}
				return api, call, unchanged
			},
		},
	}
}

// При идущей операции жизненного цикла обходные пути рестарта отвечают 409
// kernel_op_in_progress и не меняют файлы и процессы.
func TestLifecycleLock_BypassPathsBusy(t *testing.T) {
	var restarts int32
	for _, tc := range lifecycleBypassCases(&restarts) {
		t.Run(tc.name, func(t *testing.T) {
			atomic.StoreInt32(&restarts, 0)
			api, call, unchanged := tc.setup(t)
			api.lifecycleMu.Lock()
			defer api.lifecycleMu.Unlock()

			rr := call()
			if rr.Code != http.StatusConflict {
				t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
			}
			if env := decodeErrorResponse(t, rr); env.Code != "kernel_op_in_progress" || env.Error == "" {
				t.Errorf("code=%q error=%q, want kernel_op_in_progress и текст", env.Code, env.Error)
			}
			unchanged(t)
			if got := atomic.LoadInt32(&restarts); got != 0 {
				t.Errorf("рестартов при занятом замке = %d, want 0", got)
			}
		})
	}
}

// При свободном замке обходной путь работает как раньше, а после запроса замок
// свободен.
func TestLifecycleLock_BypassPathsReleaseLock(t *testing.T) {
	var restarts int32
	for _, tc := range lifecycleBypassCases(&restarts) {
		t.Run(tc.name, func(t *testing.T) {
			atomic.StoreInt32(&restarts, 0)
			api, call, _ := tc.setup(t)

			rr := call()
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if !api.lifecycleMu.TryLock() {
				t.Fatal("замок не освобождён после запроса")
			}
			api.lifecycleMu.Unlock()
		})
	}
}

// Предпросмотр миграции сокета ядро не перезапускает и замок не берёт.
func TestLifecycleLock_MigratePreviewNotLocked(t *testing.T) {
	var restarts int32
	cases := lifecycleBypassCases(&restarts)
	api, _, _ := cases[2].setup(t)
	api.lifecycleMu.Lock()
	defer api.lifecycleMu.Unlock()

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/config/mihomo-migrate-socket", nil),
		httptest.NewRequest(http.MethodPost, "/api/config/mihomo-migrate-socket", bytes.NewBufferString(`{"action": "preview"}`)),
	} {
		rr := httptest.NewRecorder()
		api.MihomoMigrateSocket(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s preview при занятом замке: expected 200, got %d: %s", req.Method, rr.Code, rr.Body.String())
		}
	}
}
