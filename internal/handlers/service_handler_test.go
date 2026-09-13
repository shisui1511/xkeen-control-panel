package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	return &API{
		cfg:       cfg,
		xkeenSvc:  services.NewXKeenService(binaryPath, tmpDir),
		kernelSvc: services.NewKernelService(t.TempDir()),
		pathVal:   utils.NewPathValidator(cfg.AllowedRoots),
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
		}
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

	// 4. Enabled = true
	enabled := true
	body, _ := json.Marshal(map[string]*bool{"enabled": &enabled})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/service/dns-redirect", bytes.NewReader(body))
	recGood := httptest.NewRecorder()
	api.ServiceDNSRedirect(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Errorf("expected 200 for good DNS redirect, got %d: %s", recGood.Code, recGood.Body.String())
	}
}
