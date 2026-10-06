package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// fakeXKeenBinary создаёт скрипт вместо xkeen: печатает ok и завершается успешно.
func fakeXKeenBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xkeen")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// executeConsole вызывает ConsoleExecute напрямую (без маршрутизации).
func executeConsole(t *testing.T, api *API, command string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"command":` + jsonString(command) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/console/execute", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.ConsoleExecute(rr, req)
	return rr
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Команда XKeen из консоли панели делает манифест недостоверным (D-09):
// слой сверяется сразу, а не через минуту.
func TestHook_ConsoleRequestsCheck(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
	h.api.consoleSvc = services.NewConsoleService(fakeXKeenBinary(t))
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)
	h.applyDiagAndWait(t, c)

	// Файл панели изменён мимо слоя (например, восстановлением бэкапа XKeen).
	path := filepath.Join(h.roots.Xray, configlayer.DiagXrayRel)
	if err := os.WriteFile(path, []byte(`{"manual":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rr := executeConsole(t, h.api, "-v")
	if rr.Code != http.StatusOK {
		t.Fatalf("ConsoleExecute: %d %s", rr.Code, rr.Body.String())
	}
	c.wait(t, "files", 2*time.Second, func(d string) bool { return strings.Contains(d, `"drift_count":1`) })
}

// Выключенный флаг и отсутствие слоя: консоль работает как прежде, без событий и паники.
func TestHook_DisabledLayerNoop(t *testing.T) {
	cases := map[string]layerHarnessOpts{
		"флаг выключен":  {Enabled: false},
		"слой не создан": {Enabled: true, NoLayer: true},
	}
	for name, ho := range cases {
		t.Run(name, func(t *testing.T) {
			h := newLayerHarness(t, ho)
			h.api.consoleSvc = services.NewConsoleService(fakeXKeenBinary(t))
			rr := executeConsole(t, h.api, "-v")
			if rr.Code != http.StatusOK {
				t.Fatalf("ConsoleExecute: %d %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), "ok") {
				t.Errorf("ответ без вывода команды: %s", rr.Body.String())
			}
			// Вызов хука напрямую безопасен и для nil-слоя, и для выключенного флага.
			h.api.layerRequestCheck()
		})
	}
}

// Восстановление снимка перечитывает файл состояния слоя с диска: иначе
// состояние в памяти затёрло бы восстановленный файл (D-09, T-144-41).
func TestHook_SnapshotRestoreReloadsState(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true})
	snapSvc := services.NewSnapshotService(h.dataDir, []string{h.dataDir})
	h.api.snapshotSvc = snapSvc

	// Состояние на момент снимка: ревизия черновика 0.
	meta, err := snapSvc.Create("hook-test")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// После снимка слой меняет черновик: ревизия 1 и в памяти, и на диске.
	env := h.do(t, http.MethodPost, "/api/configlayer/draft", `{"revision":0,"section":"diag","value":{"enabled":true}}`)
	if env.Status != http.StatusOK {
		t.Fatalf("POST draft: %d %s", env.Status, env.Raw)
	}
	if got := h.layer.Snapshot().DraftRevision; got != 1 {
		t.Fatalf("до восстановления ревизия = %d, нужна 1", got)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rr := httptest.NewRecorder()
		h.api.SnapshotRestore(rr, httptest.NewRequest(http.MethodPost, "/api/snapshots/"+meta.ID+"/restore", nil))
		done <- rr
	}()
	select {
	case rr := <-done:
		if rr.Code != http.StatusOK {
			t.Fatalf("SnapshotRestore: %d %s", rr.Code, rr.Body.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SnapshotRestore завис (замки)")
	}

	// Файл на диске вернулся к ревизии 0 — слой должен видеть то же.
	if got := h.layer.Snapshot().DraftRevision; got != 0 {
		t.Errorf("после восстановления ревизия в памяти = %d, нужна 0 (состояние с диска)", got)
	}
	// Замок жизненного цикла отпущен.
	lock := h.api.LifecycleLock()
	if !lock.TryLock() {
		t.Fatal("замок жизненного цикла остался занятым")
	}
	lock.Unlock()
}
