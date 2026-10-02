package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
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
