package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		kernelSvc: services.NewKernelService(),
		pathVal:   utils.NewPathValidator(cfg.AllowedRoots),
	}
}

// buildStubBinary компилирует простой stub-бинарник из Go-кода в tmpDir.
// stub ведёт себя так: завершается с exit code 0 и выводит output на stdout.
func buildStubBinary(t *testing.T, output string, exitCode int) string {
	t.Helper()
	tmpDir := t.TempDir()

	// Пишем stub main.go
	src := `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Print("` + output + `")
	os.Exit(` + strings.TrimSpace(string(rune('0'+exitCode))) + `)
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("write stub src: %v", err)
	}

	binPath := filepath.Join(tmpDir, "xkeen-stub")
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub binary: %v\n%s", err, out)
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
