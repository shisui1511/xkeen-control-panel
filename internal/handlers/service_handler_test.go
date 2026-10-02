package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// newServiceTestAPI создаёт API с подменным xkeen-бинарником для изолированного тестирования.
func newServiceTestAPI(t *testing.T, binaryPath string) *API {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		XKeenBinary:  binaryPath,
		AllowedRoots: []string{tmpDir},
	}
	api := &API{
		cfg:      cfg,
		xkeenSvc: services.NewXKeenService(binaryPath, tmpDir),
		pathVal:  utils.NewPathValidator(cfg.AllowedRoots),
	}
	// SetKernelService подключает запасные источники активного ядра; applier,
	// который он собирает, тестам не нужен (кому нужен — ставит свой).
	api.SetKernelService(services.NewKernelService(t.TempDir()))
	api.kernelApplier = nil
	// Подтверждение переключения идёт на фейковых часах: тесты не ждут реальные 15 с.
	fakeNow, fakeSleep := fakeSwitchClock()
	api.kernelSwitcher = services.NewKernelSwitcherFunc(
		api.kernelSvc.ProcessStates, fakeNow, fakeSleep, 15*time.Second, 500*time.Millisecond)
	return api
}

// fakeSwitchClock — часы, в которых sleep сдвигает время без реального ожидания.
func fakeSwitchClock() (func() time.Time, func(time.Duration)) {
	var mu sync.Mutex
	cur := time.Unix(1_700_000_000, 0)
	return func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return cur
		}, func(d time.Duration) {
			mu.Lock()
			cur = cur.Add(d)
			mu.Unlock()
		}
}

// decodeSwitchResult разбирает ответ switch_kernel: конверт data → SwitchResult.
func decodeSwitchResult(t *testing.T, rr *httptest.ResponseRecorder) services.SwitchResult {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Success bool                  `json:"success"`
		Data    services.SwitchResult `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode switch response: %v: %s", err, rr.Body.String())
	}
	if !env.Success {
		t.Fatalf("success=false: %s", rr.Body.String())
	}
	return env.Data
}

// serviceStatusData выполняет GET /api/service/status и разбирает data.
func serviceStatusData(t *testing.T, api *API) ServiceStatusResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	api.ServiceStatus(rr, httptest.NewRequest(http.MethodGet, "/api/service/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var envelope struct {
		Data ServiceStatusResponse `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

// TestServiceStatus_KernelConflict: оба процесса запущены — active_kernel "both",
// kernel_conflict, running_kernels; PID и uptime пусты, ядро по порядку не выбирается.
func TestServiceStatus_KernelConflict(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "XKeen is running", 0))
	api.kernelSvc.SetProcessStatesSource(func() []services.KernelProcessState {
		return []services.KernelProcessState{
			{Name: "xray", Status: "running", PID: 10, Uptime: "5м"},
			{Name: "mihomo", Status: "running", PID: 11, Uptime: "5м"},
		}
	})

	resp := serviceStatusData(t, api)
	if resp.ActiveKernel != "both" || !resp.KernelConflict {
		t.Errorf("active_kernel=%q kernel_conflict=%v, want both/true", resp.ActiveKernel, resp.KernelConflict)
	}
	if want := []string{"xray", "mihomo"}; !reflect.DeepEqual(resp.RunningKernels, want) {
		t.Errorf("running_kernels = %v, want %v", resp.RunningKernels, want)
	}
	if !resp.IsRunning {
		t.Error("is_running = false, want true при конфликте")
	}
	if resp.PID != 0 || resp.Uptime != "" {
		t.Errorf("pid=%d uptime=%q, при конфликте должны быть пусты", resp.PID, resp.Uptime)
	}
}

// TestServiceStatus_SingleKernelRunning: один процесс — его PID и uptime, без конфликта.
func TestServiceStatus_SingleKernelRunning(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "XKeen is running", 0))
	api.kernelSvc.SetProcessStatesSource(func() []services.KernelProcessState {
		return []services.KernelProcessState{
			{Name: "xray", Status: "stopped"},
			{Name: "mihomo", Status: "running", PID: 11, Uptime: "7м"},
		}
	})

	resp := serviceStatusData(t, api)
	if resp.ActiveKernel != "mihomo" || resp.KernelConflict || !resp.IsRunning {
		t.Errorf("active_kernel=%q kernel_conflict=%v is_running=%v, want mihomo/false/true",
			resp.ActiveKernel, resp.KernelConflict, resp.IsRunning)
	}
	if resp.PID != 11 || resp.Uptime != "7м" {
		t.Errorf("pid=%d uptime=%q, want 11 / 7м", resp.PID, resp.Uptime)
	}
}

// buildStubBinary компилирует простой stub-бинарник из Go-кода в tmpDir.
// stub ведёт себя так: завершается с exit code 0 и выводит output на stdout.
func buildStubBinary(t *testing.T, output string, exitCode int) string {
	t.Helper()
	tmpDir := t.TempDir()

	binPath := filepath.Join(tmpDir, "xkeen-stub")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nexit %d\n", output, exitCode)
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatalf("write stub script: %v", err)
	}

	return binPath
}

// TestServiceControl_InvalidAction проверяет что неизвестный action возвращает 400.
func TestServiceControl_InvalidAction(t *testing.T) {
	binPath := buildStubBinary(t, "ok", 0)
	api := newServiceTestAPI(t, binPath)

	req := httptest.NewRequest(http.MethodPost, "/api/service/control?action=unknown", nil)
	rr := httptest.NewRecorder()

	api.ServiceControl(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown action, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestServiceControl_MethodNotAllowed проверяет что GET-запрос возвращает 405.
func TestServiceControl_MethodNotAllowed(t *testing.T) {
	binPath := buildStubBinary(t, "ok", 0)
	api := newServiceTestAPI(t, binPath)

	req := httptest.NewRequest(http.MethodGet, "/api/service/control?action=start", nil)
	rr := httptest.NewRecorder()

	api.ServiceControl(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestServiceControl_SwitchKernel_InvalidKernel проверяет что некорректное имя ядра возвращает 400.
func TestServiceControl_SwitchKernel_InvalidKernel(t *testing.T) {
	binPath := buildStubBinary(t, "ok", 0)
	api := newServiceTestAPI(t, binPath)

	// Невалидное ядро — должно вернуть 400
	for _, badKernel := range []string{"", "v2ray", "sing-box", "../etc/passwd"} {
		req := httptest.NewRequest(http.MethodPost,
			"/api/service/control?action=switch_kernel&kernel="+badKernel, nil)
		rr := httptest.NewRecorder()

		api.ServiceControl(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("kernel=%q: expected 400, got %d: %s", badKernel, rr.Code, rr.Body.String())
		}
	}
}

// TestServiceControl_SwitchKernel_ValidNames проверяет что valide имена ядер принимаются,
// а не отвергаются на этапе валидации (итоговый код зависит от stub).
func TestServiceControl_SwitchKernel_ValidNames(t *testing.T) {
	// stub завершается с exit 0 — имитация успешного переключения
	binPath := buildStubBinary(t, "Ядро переключено", 0)
	api := newServiceTestAPI(t, binPath)

	for _, kernel := range []string{"xray", "mihomo"} {
		req := httptest.NewRequest(http.MethodPost,
			"/api/service/control?action=switch_kernel&kernel="+kernel, nil)
		rr := httptest.NewRecorder()

		api.ServiceControl(rr, req)

		// Не должно быть 400 (ошибка валидации) или 405
		if rr.Code == http.StatusBadRequest || rr.Code == http.StatusMethodNotAllowed {
			t.Errorf("kernel=%q: unexpected validation error %d: %s", kernel, rr.Code, rr.Body.String())
			continue
		}
		if res := decodeSwitchResult(t, rr); res.Outcome == "" || res.New != kernel {
			t.Errorf("kernel=%q: outcome=%q new=%q, want непустой outcome и new=%q", kernel, res.Outcome, res.New, kernel)
		}
	}
}

// stepStates — источник состояний процессов: первые n чтений отдаёт before, затем after.
func stepStates(n int, before, after []services.KernelProcessState) func() []services.KernelProcessState {
	var calls int32
	return func() []services.KernelProcessState {
		if int(atomic.AddInt32(&calls, 1)) <= n {
			return before
		}
		return after
	}
}

// TestServiceControl_SwitchOutcomeSwitched: старое ядро остановилось, новое
// работает — outcome switched, основание — процессы, а не код выхода скрипта.
func TestServiceControl_SwitchOutcomeSwitched(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "\x1b[32mядро переключено\x1b[0m\n\nготово", 0))
	xray := []services.KernelProcessState{{Name: "xray", Status: "running", PID: 10}, {Name: "mihomo", Status: "stopped"}}
	mihomo := []services.KernelProcessState{{Name: "xray", Status: "stopped"}, {Name: "mihomo", Status: "running", PID: 11}}
	// Первые чтения (определение старого ядра, затем первый такт опроса) — xray, дальше mihomo.
	api.kernelSvc.SetProcessStatesSource(stepStates(2, xray, mihomo))

	rr := httptest.NewRecorder()
	api.ServiceControl(rr, httptest.NewRequest(http.MethodPost, "/api/service/control?action=switch_kernel&kernel=mihomo", nil))

	res := decodeSwitchResult(t, rr)
	if res.Outcome != services.SwitchSwitched || res.Old != "xray" || res.New != "mihomo" {
		t.Errorf("result = %+v, want switched xray -> mihomo", res)
	}
	if res.OldRunning || !res.NewRunning {
		t.Errorf("old_running=%v new_running=%v, want false/true", res.OldRunning, res.NewRunning)
	}
	if strings.Contains(res.Output, "\x1b") || !strings.Contains(res.Output, "ядро переключено") {
		t.Errorf("output = %q, want текст скрипта без ANSI", res.Output)
	}
	if strings.Contains(res.Output, "\n\n") {
		t.Errorf("output = %q, пустые строки должны быть отброшены", res.Output)
	}
}

// TestServiceControl_SwitchOutcomeNewNotStarted: скрипт отработал с кодом 0, но
// новое ядро не поднялось — 200 с исходом new_not_started, не успех.
func TestServiceControl_SwitchOutcomeNewNotStarted(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))
	api.kernelSvc.SetProcessStatesSource(func() []services.KernelProcessState {
		return []services.KernelProcessState{{Name: "xray", Status: "stopped"}, {Name: "mihomo", Status: "stopped"}}
	})

	rr := httptest.NewRecorder()
	api.ServiceControl(rr, httptest.NewRequest(http.MethodPost, "/api/service/control?action=switch_kernel&kernel=mihomo", nil))

	res := decodeSwitchResult(t, rr)
	if res.Outcome != services.SwitchNewNotStarted || res.NewRunning {
		t.Errorf("result = %+v, want new_not_started", res)
	}
}

// TestServiceControl_SwitchOutcomeScriptFailure: ошибка самого скрипта — 500 как раньше.
func TestServiceControl_SwitchOutcomeScriptFailure(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "boom", 1))
	rr := httptest.NewRecorder()
	api.ServiceControl(rr, httptest.NewRequest(http.MethodPost, "/api/service/control?action=switch_kernel&kernel=mihomo", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestServiceControl_XKeenNotInstalled проверяет поведение когда бинарник XKeen отсутствует.
// Хендлер должен вернуть 500, а не паниковать.
func TestServiceControl_XKeenNotInstalled(t *testing.T) {
	// Указываем несуществующий путь к бинарнику
	api := newServiceTestAPI(t, "/nonexistent/xkeen-binary")

	for _, action := range []string{"start", "stop", "restart"} {
		req := httptest.NewRequest(http.MethodPost, "/api/service/control?action="+action, nil)
		rr := httptest.NewRecorder()

		api.ServiceControl(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Errorf("action=%q (no binary): expected 500, got %d: %s", action, rr.Code, rr.Body.String())
		}
	}
}

// TestServiceControl_SwitchKernel_XKeenNotInstalled проверяет switch_kernel без бинарника.
func TestServiceControl_SwitchKernel_XKeenNotInstalled(t *testing.T) {
	api := newServiceTestAPI(t, "/nonexistent/xkeen-binary")

	req := httptest.NewRequest(http.MethodPost,
		"/api/service/control?action=switch_kernel&kernel=xray", nil)
	rr := httptest.NewRecorder()

	api.ServiceControl(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("switch_kernel (no binary): expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestServiceStatus_OK проверяет что ServiceStatus возвращает 200 для корректного бинарника.
func TestServiceStatus_OK(t *testing.T) {
	// stub имитирует вывод команды -status
	binPath := buildStubBinary(t, "XKeen is not running", 0)
	api := newServiceTestAPI(t, binPath)

	req := httptest.NewRequest(http.MethodGet, "/api/service/status", nil)
	rr := httptest.NewRecorder()

	api.ServiceStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for ServiceStatus, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestServiceStatus_MethodNotAllowed проверяет что POST на ServiceStatus возвращает 405.
func TestServiceStatus_MethodNotAllowed(t *testing.T) {
	binPath := buildStubBinary(t, "ok", 0)
	api := newServiceTestAPI(t, binPath)

	req := httptest.NewRequest(http.MethodPost, "/api/service/status", nil)
	rr := httptest.NewRecorder()

	api.ServiceStatus(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestServiceRestartLog(t *testing.T) {
	binPath := buildStubBinary(t, "ok", 0)
	api := newServiceTestAPI(t, binPath)

	// 1. Method Not Allowed (POST)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/service/restart-log", nil)
	recPost := httptest.NewRecorder()
	api.ServiceRestartLog(recPost, reqPost)
	if recPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", recPost.Code)
	}

	// 2. GET -> 200 OK
	reqGet := httptest.NewRequest(http.MethodGet, "/api/service/restart-log", nil)
	recGet := httptest.NewRecorder()
	api.ServiceRestartLog(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("expected 200 for GET, got %d", recGet.Code)
	}
}

func TestServiceDNSRedirect(t *testing.T) {
	binPath := buildStubBinary(t, "DNS proxying updated", 0)
	api := newServiceTestAPI(t, binPath)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/service/dns-redirect", nil)
	recGet := httptest.NewRecorder()
	api.ServiceDNSRedirect(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/service/dns-redirect", bytes.NewReader([]byte("{invalid")))
	recBadJSON := httptest.NewRecorder()
	api.ServiceDNSRedirect(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. Enabled is nil
	reqNil := httptest.NewRequest(http.MethodPost, "/api/service/dns-redirect", bytes.NewReader([]byte(`{}`)))
	recNil := httptest.NewRecorder()
	api.ServiceDNSRedirect(recNil, reqNil)
	if recNil.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when enabled is nil, got %d", recNil.Code)
	}

	// 4. Enabled = true (router DNS keeps answering)
	api.xkeenSvc.SetDNSProbe(func(context.Context) error { return nil })
	enabled := true
	body, _ := json.Marshal(map[string]*bool{"enabled": &enabled})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/service/dns-redirect", bytes.NewReader(body))
	recGood := httptest.NewRecorder()
	api.ServiceDNSRedirect(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Errorf("expected 200 for good DNS redirect, got %d: %s", recGood.Code, recGood.Body.String())
	}

	// 5. Router stops resolving: redirection is rolled back with 409
	api.xkeenSvc.SetDNSProbe(func(context.Context) error { return errors.New("no answer") })
	reqDead := httptest.NewRequest(http.MethodPost, "/api/service/dns-redirect", bytes.NewReader(body))
	recDead := httptest.NewRecorder()
	api.ServiceDNSRedirect(recDead, reqDead)
	if recDead.Code != http.StatusConflict {
		t.Errorf("expected 409 when DNS dies, got %d: %s", recDead.Code, recDead.Body.String())
	}
}

// TestServiceStatus_XKeenNotInstalled: без XKeen статус отдаётся успешно с
// xkeen_installed=false — по нему UI предлагает установку (раньше был 500,
// и карточка установки не появлялась).
func TestServiceStatus_XKeenNotInstalled(t *testing.T) {
	api := newServiceTestAPI(t, "/nonexistent/xkeen-binary")
	api.SetXKeenInstaller(&services.XKeenInstaller{InitDir: t.TempDir()})

	rr := httptest.NewRecorder()
	api.ServiceStatus(rr, httptest.NewRequest(http.MethodGet, "/api/service/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data ServiceStatusResponse `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.XKeenInstalled || !resp.Data.XKeenInstallerAvailable {
		t.Errorf("got installed=%v available=%v, want false/true",
			resp.Data.XKeenInstalled, resp.Data.XKeenInstallerAvailable)
	}
}

// TestServiceStatus_XKeenSetupIncomplete: бинарник XKeen есть, а init-скрипта
// нет — установка прервана, UI должен снова предложить установщик.
func TestServiceStatus_XKeenSetupIncomplete(t *testing.T) {
	bin := buildStubBinary(t, "XKeen is not running", 0)
	for _, tc := range []struct {
		name       string
		initScript bool
		want       bool
	}{
		{"init script missing", false, true},
		{"setup complete", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newServiceTestAPI(t, bin)
			initDir := t.TempDir()
			if tc.initScript {
				if err := os.WriteFile(filepath.Join(initDir, "S05xkeen"), nil, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			api.SetXKeenInstaller(&services.XKeenInstaller{InitDir: initDir})

			rr := httptest.NewRecorder()
			api.ServiceStatus(rr, httptest.NewRequest(http.MethodGet, "/api/service/status", nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			var resp struct {
				Data ServiceStatusResponse `json:"data"`
			}
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if !resp.Data.XKeenInstalled || resp.Data.XKeenSetupIncomplete != tc.want {
				t.Errorf("got installed=%v incomplete=%v, want true/%v",
					resp.Data.XKeenInstalled, resp.Data.XKeenSetupIncomplete, tc.want)
			}
		})
	}
}

// newApplyTestAPI — API с фейковым KernelApplier: статусы задаются картой,
// вызовы рестарта считаются.
func newApplyTestAPI(t *testing.T, configured string, statuses map[string]string) (*API, *int32) {
	t.Helper()
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))
	var restarts int32
	api.kernelApplier = services.NewKernelApplierFunc(
		func(name string) string {
			if s, ok := statuses[name]; ok {
				return s
			}
			return "not_installed"
		},
		func() string { return configured },
		func() (string, error) {
			atomic.AddInt32(&restarts, 1)
			return "ok", nil
		},
	)
	return api, &restarts
}

func decodeApplyResult(t *testing.T, body []byte) services.ApplyResult {
	t.Helper()
	var env struct {
		Success bool                 `json:"success"`
		Data    services.ApplyResult `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode apply response: %v: %s", err, body)
	}
	if !env.Success {
		t.Fatalf("success=false: %s", body)
	}
	return env.Data
}

// TestServiceControl_Apply: остановленное целевое ядро не запускается, запущенное
// перезапускается ровно один раз, чужое не трогается.
func TestServiceControl_Apply(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		statuses   map[string]string
		query      string
		want       services.ApplyResult
		wantCalls  int32
	}{
		{
			name: "xray stopped", configured: "xray",
			statuses: map[string]string{"xray": "stopped", "mihomo": "stopped"},
			query:    "kernel=xray", wantCalls: 0,
			want: services.ApplyResult{Outcome: services.ApplySavedKernelStopped, Kernel: "xray", ActiveKernel: "xray", ActiveRunning: false},
		},
		{
			name: "xray not_installed", configured: "xray",
			statuses: map[string]string{"xray": "not_installed"},
			query:    "kernel=xray", wantCalls: 0,
			want: services.ApplyResult{Outcome: services.ApplySavedKernelStopped, Kernel: "xray", ActiveKernel: "xray", ActiveRunning: false},
		},
		{
			name: "xray running", configured: "xray",
			statuses: map[string]string{"xray": "running"},
			query:    "kernel=xray", wantCalls: 1,
			want: services.ApplyResult{Outcome: services.ApplyRestarted, Kernel: "xray", ActiveKernel: "xray", ActiveRunning: true},
		},
		{
			name: "xray unknown", configured: "xray",
			statuses: map[string]string{"xray": "unknown"},
			query:    "kernel=xray", wantCalls: 1,
			want: services.ApplyResult{Outcome: services.ApplyRestarted, Kernel: "xray", ActiveKernel: "xray", ActiveRunning: true},
		},
		{
			name: "mihomo while xray runs", configured: "xray",
			statuses: map[string]string{"xray": "running", "mihomo": "stopped"},
			query:    "kernel=mihomo", wantCalls: 0,
			want: services.ApplyResult{Outcome: services.ApplySavedKernelInactive, Kernel: "mihomo", ActiveKernel: "xray", ActiveRunning: true},
		},
		{
			// Работающий процесс важнее name_client: mihomo — активное ядро.
			name: "mihomo runs alone, running wins over configured", configured: "xray",
			statuses: map[string]string{"xray": "stopped", "mihomo": "running"},
			query:    "kernel=mihomo", wantCalls: 1,
			want: services.ApplyResult{Outcome: services.ApplyRestarted, Kernel: "mihomo", ActiveKernel: "mihomo", ActiveRunning: true},
		},
		{
			name: "active resolves to configured", configured: "mihomo",
			statuses: map[string]string{"xray": "stopped", "mihomo": "running"},
			query:    "kernel=active", wantCalls: 1,
			want: services.ApplyResult{Outcome: services.ApplyRestarted, Kernel: "mihomo", ActiveKernel: "mihomo", ActiveRunning: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, restarts := newApplyTestAPI(t, tc.configured, tc.statuses)
			req := httptest.NewRequest(http.MethodPost, "/api/service/control?action=apply&"+tc.query, nil)
			rr := httptest.NewRecorder()
			api.ServiceControl(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := decodeApplyResult(t, rr.Body.Bytes()); got != tc.want {
				t.Errorf("result = %+v, want %+v", got, tc.want)
			}
			if got := atomic.LoadInt32(restarts); got != tc.wantCalls {
				t.Errorf("restart calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

// TestServiceControl_ApplyConflictOutcome: при двух запущенных ядрах применение
// отдаёт исход saved_kernel_conflict и не перезапускает ни одно ядро.
func TestServiceControl_ApplyConflictOutcome(t *testing.T) {
	for _, query := range []string{"kernel=xray", "kernel=mihomo", "kernel=active"} {
		t.Run(query, func(t *testing.T) {
			api, restarts := newApplyTestAPI(t, "xray", map[string]string{"xray": "running", "mihomo": "running"})
			req := httptest.NewRequest(http.MethodPost, "/api/service/control?action=apply&"+query, nil)
			rr := httptest.NewRecorder()
			api.ServiceControl(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			got := decodeApplyResult(t, rr.Body.Bytes())
			if got.Outcome != services.ApplySavedKernelConflict || got.ActiveKernel != "both" {
				t.Errorf("result = %+v, want saved_kernel_conflict with active_kernel=both", got)
			}
			if n := atomic.LoadInt32(restarts); n != 0 {
				t.Errorf("restart calls = %d, want 0", n)
			}
		})
	}
}

// TestServiceControl_ApplyRestartFailed: ошибка рестарта — 200 и outcome
// restart_failed с полем error.
func TestServiceControl_ApplyRestartFailed(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))
	api.kernelApplier = services.NewKernelApplierFunc(
		// Только xray запущен: при двух запущенных ядрах рестарта нет (конфликт).
		func(name string) string {
			if name == "xray" {
				return "running"
			}
			return "stopped"
		},
		func() string { return "xray" },
		func() (string, error) { return "\x1b[31mboom\x1b[0m", errors.New("exit status 1") },
	)
	req := httptest.NewRequest(http.MethodPost, "/api/service/control?action=apply&kernel=xray", nil)
	rr := httptest.NewRecorder()
	api.ServiceControl(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := decodeApplyResult(t, rr.Body.Bytes())
	if got.Outcome != services.ApplyRestartFailed || got.Error != "boom" {
		t.Errorf("result = %+v, want restart_failed with error boom", got)
	}
}

// TestServiceControl_ApplyNoApplier: без KernelApplier ничего не перезапускается.
func TestServiceControl_ApplyNoApplier(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))
	req := httptest.NewRequest(http.MethodPost, "/api/service/control?action=apply&kernel=xray", nil)
	rr := httptest.NewRecorder()
	api.ServiceControl(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeApplyResult(t, rr.Body.Bytes()); got.Outcome != services.ApplySavedKernelStopped {
		t.Errorf("outcome = %q, want saved_kernel_stopped", got.Outcome)
	}
}

// TestServiceControl_ApplyInvalidKernel: allow-list kernel с точным сравнением,
// рестарта нет.
func TestServiceControl_ApplyInvalidKernel(t *testing.T) {
	api, restarts := newApplyTestAPI(t, "xray", map[string]string{"xray": "running"})

	for _, query := range []string{
		"action=apply",
		"action=apply&kernel=",
		"action=apply&kernel=Xray",
		"action=apply&kernel=MIHOMO",
		"action=apply&kernel=both",
		"action=apply&kernel=xray%20",
		"action=apply&kernel=..%2Fetc",
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/service/control?"+query, nil)
		rr := httptest.NewRecorder()
		api.ServiceControl(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", query, rr.Code, rr.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/service/control?action=apply&kernel=xray", nil)
	rr := httptest.NewRecorder()
	api.ServiceControl(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: expected 405, got %d", rr.Code)
	}

	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Errorf("restart calls = %d, want 0", got)
	}
}

// applyPathTestAPI — API с каталогами xray (симлинк на реальный каталог),
// mihomo и other внутри разрешённого корня.
func applyPathTestAPI(t *testing.T, configured string, statuses map[string]string) (api *API, xrayLink, mihomoDir, otherDir string, restarts *int32) {
	t.Helper()
	root := t.TempDir()
	realXray := filepath.Join(root, "real-xray")
	xrayLink = filepath.Join(root, "xray")
	mihomoDir = filepath.Join(root, "mihomo")
	otherDir = filepath.Join(root, "other")
	for _, d := range []string{realXray, mihomoDir, otherDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(realXray, xrayLink); err != nil {
		t.Skipf("symlink недоступен: %v", err)
	}
	api, restarts = newApplyTestAPI(t, configured, statuses)
	api.cfg = &config.Config{
		XKeenBinary:     api.cfg.XKeenBinary,
		AllowedRoots:    []string{root},
		XRayConfigDir:   xrayLink,
		MihomoConfigDir: mihomoDir,
	}
	api.pathVal = utils.NewPathValidator(api.cfg.AllowedRoots)
	return api, xrayLink, mihomoDir, otherDir, restarts
}

// TestServiceControl_ApplyByPath: path=<файл> выбирает ядро по каталогу файла.
func TestServiceControl_ApplyByPath(t *testing.T) {
	statuses := map[string]string{"xray": "stopped", "mihomo": "running"} // активное ядро — работающий mihomo
	api, xrayDir, mihomoDir, otherDir, restarts := applyPathTestAPI(t, "mihomo", statuses)

	apply := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/service/control?action=apply&path="+path, nil)
		rr := httptest.NewRecorder()
		api.ServiceControl(rr, req)
		return rr
	}

	// Классификация пути: xray / mihomo / active (в том числе через симлинк).
	realXrayFile := filepath.Join(filepath.Dir(xrayDir), "real-xray", "04_outbounds.json")
	for path, want := range map[string]string{
		filepath.Join(xrayDir, "04_outbounds.json"): "xray",
		realXrayFile:                                  "xray",
		filepath.Join(mihomoDir, "config.yaml"):       "mihomo",
		filepath.Join(otherDir, "xkeen.json"):         services.ApplyTargetActive,
		filepath.Join(filepath.Dir(xrayDir), "x.txt"): services.ApplyTargetActive,
	} {
		validated, err := api.pathVal.Validate(path)
		if err != nil {
			t.Fatalf("validate %s: %v", path, err)
		}
		if got := api.kernelForConfigPath(validated); got != want {
			t.Errorf("kernelForConfigPath(%s) = %q, want %q", path, got, want)
		}
	}

	// Файл каталога Xray (адресованный через симлинк) при активном mihomo:
	// цель xray, активное ядро не трогается.
	rr := apply(filepath.Join(xrayDir, "04_outbounds.json"))
	if rr.Code != http.StatusOK {
		t.Fatalf("xray path: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := decodeApplyResult(t, rr.Body.Bytes()); got.Outcome != services.ApplySavedKernelInactive || got.Kernel != "xray" {
		t.Errorf("xray path: result = %+v, want saved_kernel_inactive for xray", got)
	}
	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Errorf("restart calls = %d, want 0", got)
	}

	// Файл каталога Mihomo — цель mihomo, оно активно и запущено.
	rr = apply(filepath.Join(mihomoDir, "config.yaml"))
	if got := decodeApplyResult(t, rr.Body.Bytes()); got.Outcome != services.ApplyRestarted || got.Kernel != "mihomo" {
		t.Errorf("mihomo path: result = %+v, want restarted mihomo", got)
	}
	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Errorf("restart calls = %d, want 1", got)
	}

	// Пути вне разрешённых корней и с «..» — 403, рестарта нет.
	for _, bad := range []string{"/etc/passwd", filepath.Join(otherDir, "..", "..", "etc", "passwd"), "relative/../../x"} {
		if rr := apply(bad); rr.Code != http.StatusForbidden {
			t.Errorf("path %q: expected 403, got %d: %s", bad, rr.Code, rr.Body.String())
		}
	}
	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Errorf("restart calls after rejected paths = %d, want 1", got)
	}
}

// decodeServiceStatus разбирает ответ /api/service/status.
func decodeServiceStatus(t *testing.T, rr *httptest.ResponseRecorder) ServiceStatusResponse {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Data ServiceStatusResponse `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

// TestServiceStatus_FromCache: `xkeen -status` начал падать после одного успеха —
// ответ 200 из кэша: прежний raw, stale=true и возраст последнего успеха.
func TestServiceStatus_FromCache(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))

	var offset atomic.Int64 // секунды сдвига часов кэша
	base := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	cache := services.NewXKeenStatusCacheFunc(
		func(context.Context) (string, error) {
			if fail.Load() {
				return "", errors.New("timeout exceeded")
			}
			return "XKeen is running", nil
		},
		nil,
		func() time.Time { return base.Add(time.Duration(offset.Load()) * time.Second) },
		time.Hour,
	)
	cache.Start()
	defer cache.Stop()
	api.SetXKeenStatusCache(cache)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cache.RefreshNow(ctx)

	fresh := decodeServiceStatus(t, serveStatus(api))
	if fresh.Stale || fresh.Raw != "XKeen is running" || fresh.AgeSeconds == nil || *fresh.AgeSeconds != 0 {
		t.Fatalf("свежий снимок: stale=%v raw=%q age=%v", fresh.Stale, fresh.Raw, fresh.AgeSeconds)
	}

	fail.Store(true)
	offset.Store(45)
	cache.RefreshNow(ctx)

	rr := serveStatus(api)
	got := decodeServiceStatus(t, rr)
	if !got.Stale {
		t.Error("stale = false при падающем xkeen -status")
	}
	if got.Raw != "XKeen is running" {
		t.Errorf("raw = %q, want прежний вывод", got.Raw)
	}
	if got.AgeSeconds == nil || *got.AgeSeconds != 45 {
		t.Errorf("age_seconds = %v, want 45", got.AgeSeconds)
	}
}

func serveStatus(api *API) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	api.ServiceStatus(rr, httptest.NewRequest(http.MethodGet, "/api/service/status", nil))
	return rr
}

// TestServiceStatus_ColdCacheNever500: ни одного успешного опроса — 200,
// stale=true, без age_seconds, «остановлено» по ошибке опроса не выставляется,
// активное ядро берётся из настройки XKeen.
func TestServiceStatus_ColdCacheNever500(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "ok", 0))
	api.kernelSvc = nil // процессов ядер на машине разработчика не опрашиваем
	initScript := filepath.Join(t.TempDir(), "S05xkeen")
	if err := os.WriteFile(initScript, []byte("name_client=\"mihomo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	api.xkeenSvc.InitScript = initScript

	cache := services.NewXKeenStatusCacheFunc(
		func(context.Context) (string, error) { return "", errors.New("timeout exceeded") },
		nil, nil, time.Hour,
	)
	cache.Start()
	defer cache.Stop()
	api.SetXKeenStatusCache(cache)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cache.RefreshNow(ctx)

	rr := serveStatus(api)
	got := decodeServiceStatus(t, rr)
	if !got.Stale {
		t.Error("stale = false на холодном кэше")
	}
	if got.AgeSeconds != nil {
		t.Errorf("age_seconds = %d, want отсутствует", *got.AgeSeconds)
	}
	if got.Raw != "" {
		t.Errorf("raw = %q, want пусто", got.Raw)
	}
	if got.IsRunning {
		t.Error("is_running = true без запущенных процессов")
	}
	if got.ActiveKernel != "mihomo" {
		t.Errorf("active_kernel = %q, want mihomo (ConfiguredKernel)", got.ActiveKernel)
	}
	if bytes.Contains(rr.Body.Bytes(), []byte(`"age_seconds"`)) {
		t.Errorf("поле age_seconds попало в JSON: %s", rr.Body.String())
	}
}

// TestServiceStatus_StatusErrorNoCacheNever500: без кэша и с падающим
// xkeen -status ответ всё равно 200 со stale=true.
func TestServiceStatus_StatusErrorNoCacheNever500(t *testing.T) {
	api := newServiceTestAPI(t, buildStubBinary(t, "broken", 1))
	api.kernelSvc = nil

	got := decodeServiceStatus(t, serveStatus(api))
	if !got.Stale {
		t.Error("stale = false при падающем xkeen -status")
	}
	if got.IsRunning {
		t.Error("is_running = true по устаревшему выводу")
	}
}
