package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// consoleLifecycleAPI собирает API с поддельным xkeen: каждая команда дописывает
// свою строку в файл-журнал, по которому видно, запускался ли xkeen.
func consoleLifecycleAPI(t *testing.T) (*API, string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "xkeen")
	body := "#!/bin/sh\necho \"$1\" >> " + logPath + "\necho ok\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return &API{consoleSvc: services.NewConsoleService(script)}, logPath
}

func consoleExec(api *API, command string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	api.ConsoleExecute(rr, httptest.NewRequest(http.MethodPost, "/api/console/execute",
		bytes.NewBufferString(`{"command":"`+command+`"}`)))
	return rr
}

func callsLog(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(data)
}

// Запись B30 этапа 10: команды консоли, запускающие, останавливающие,
// перезапускающие и переключающие ядро, не должны идти параллельно с операцией
// жизненного цикла: при занятом замке ответ 409 и xkeen не запускается.
func TestConsoleExecute_LifecycleCommandsBusy(t *testing.T) {
	commands := []string{"-start", "-stop", "-restart", "-xray", "-mihomo", "-dns", "-ipv6", "-tp", "-ux", "-um"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			api, logPath := consoleLifecycleAPI(t)
			api.lifecycleMu.Lock()
			defer api.lifecycleMu.Unlock()

			rr := consoleExec(api, command)
			if rr.Code != http.StatusConflict {
				t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
			}
			if env := decodeErrorResponse(t, rr); env.Code != "kernel_op_in_progress" || env.Error == "" {
				t.Errorf("code=%q error=%q, want kernel_op_in_progress и текст", env.Code, env.Error)
			}
			if got := callsLog(t, logPath); got != "" {
				t.Errorf("xkeen запущен при занятом замке: %q", got)
			}
		})
	}
}

// При свободном замке команда выполняется, а после ответа замок свободен.
func TestConsoleExecute_LifecycleCommandReleasesLock(t *testing.T) {
	api, logPath := consoleLifecycleAPI(t)

	rr := consoleExec(api, "-restart")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := callsLog(t, logPath); got != "-restart\n" {
		t.Errorf("журнал вызовов xkeen = %q, want -restart", got)
	}
	if !api.lifecycleMu.TryLock() {
		t.Fatal("замок не освобождён после команды")
	}
	api.lifecycleMu.Unlock()
}

// Информационные и подготовительные команды замок не берут: статус и версию
// можно смотреть, пока идёт переключение ядра.
func TestConsoleExecute_InfoCommandsIgnoreLock(t *testing.T) {
	for _, command := range []string{"-status", "-v", "-h", "-about", "-cp", "-cpe", "-diag", "-auto", "-kb", "-xb", "-mb"} {
		t.Run(command, func(t *testing.T) {
			api, logPath := consoleLifecycleAPI(t)
			api.lifecycleMu.Lock()
			defer api.lifecycleMu.Unlock()

			rr := consoleExec(api, command)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200 при занятом замке, got %d: %s", rr.Code, rr.Body.String())
			}
			if got := callsLog(t, logPath); got != command+"\n" {
				t.Errorf("журнал вызовов xkeen = %q, want %s", got, command)
			}
		})
	}
}
