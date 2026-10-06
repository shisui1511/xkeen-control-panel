package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

// --- задача 2: защита файлов панели в Редакторе, установка ядра ---

// editorHarness подключает к стенду слоя сервис конфигов и валидатор путей:
// корни Xray, Mihomo и каталог данных (для симлинка) разрешены.
func editorHarness(t *testing.T, ho layerHarnessOpts) *layerHarness {
	t.Helper()
	h := newLayerHarness(t, ho)
	allowed := []string{h.roots.Xray, h.roots.Mihomo, h.dataDir}
	h.cfg.MihomoConfigDir = h.roots.Mihomo
	h.cfg.AllowedRoots = allowed
	h.api.configSvc = services.NewConfigService(h.roots.Xray, allowed)
	h.api.pathVal = utils.NewPathValidator(allowed)
	return h
}

// editorCall вызывает обработчик Редактора напрямую.
func editorCall(t *testing.T, api *API, handler http.HandlerFunc, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Accept-Language", "ru")
	rr := httptest.NewRecorder()
	i18n.Middleware(handler).ServeHTTP(rr, req)
	return rr
}

// managedDiagPath применяет диагностический файл и возвращает его абсолютный путь.
func managedDiagPath(t *testing.T, h *layerHarness) string {
	t.Helper()
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)
	h.applyDiagAndWait(t, c)
	path := filepath.Join(h.roots.Xray, configlayer.DiagXrayRel)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("файл панели не записан: %v", err)
	}
	return path
}

func assertFileManaged409(t *testing.T, rr *httptest.ResponseRecorder, what string) {
	t.Helper()
	var resp APIResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if rr.Code != http.StatusConflict || resp.Code != "file_managed" {
		t.Fatalf("%s: статус %d code=%q тело %s, нужен 409 file_managed", what, rr.Code, resp.Code, rr.Body.String())
	}
	if resp.Error == "" || strings.HasPrefix(resp.Error, "configlayer.") {
		t.Errorf("%s: сообщение не переведено: %q", what, resp.Error)
	}
}

func TestConfigSave_ManagedFileRejected(t *testing.T) {
	t.Run("managed_rejected_then_released", func(t *testing.T) {
		h := editorHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		path := managedDiagPath(t, h)
		before, _ := os.ReadFile(path)

		rr := editorCall(t, h.api, h.api.ConfigSave, "/api/config/save?path="+path, `{"x":1}`)
		assertFileManaged409(t, rr, "ConfigSave")
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Errorf("содержимое managed-файла изменилось: %q -> %q", before, after)
		}

		rr = editorCall(t, h.api, h.api.ConfigDelete, "/api/config/delete?path="+path, "")
		assertFileManaged409(t, rr, "ConfigDelete")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("managed-файл удалён: %v", err)
		}

		other := filepath.Join(h.roots.Xray, "01_log.json")
		rr = editorCall(t, h.api, h.api.ConfigRename, "/api/config/rename?old="+path+"&new="+filepath.Join(h.roots.Xray, "renamed.json"), "")
		assertFileManaged409(t, rr, "ConfigRename старый путь managed")
		rr = editorCall(t, h.api, h.api.ConfigRename, "/api/config/rename?old="+other+"&new="+path, "")
		assertFileManaged409(t, rr, "ConfigRename новый путь managed")
		if _, err := os.Stat(other); err != nil {
			t.Errorf("чужой файл переименован вопреки отказу: %v", err)
		}

		// Чужой (не управляемый слоем) файл правится как раньше.
		rr = editorCall(t, h.api, h.api.ConfigSave, "/api/config/save?path="+other, `{"log":{"loglevel":"warning"}}`)
		if rr.Code != http.StatusOK {
			t.Errorf("сохранение чужого файла: %d %s", rr.Code, rr.Body.String())
		}

		// «Отпустить управление»: файл становится ручным и правится.
		key := h.layerFileKey(t)
		if env := h.do(t, http.MethodPost, "/api/configlayer/files/release", `{"key":`+jsonString(key)+`}`); env.Status != http.StatusOK {
			t.Fatalf("release: %d %s", env.Status, env.Raw)
		}
		rr = editorCall(t, h.api, h.api.ConfigSave, "/api/config/save?path="+path, `{"x":1}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("сохранение отпущенного файла: %d %s", rr.Code, rr.Body.String())
		}
		if after, _ := os.ReadFile(path); string(after) != `{"x":1}` {
			t.Errorf("отпущенный файл не записан: %q", after)
		}
	})

	t.Run("flag_off_unchanged", func(t *testing.T) {
		h := editorHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		path := managedDiagPath(t, h)
		h.cfg.Lock()
		h.cfg.ConfigLayer = false
		h.cfg.Unlock()

		rr := editorCall(t, h.api, h.api.ConfigSave, "/api/config/save?path="+path, `{"x":2}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("сохранение при выключенном флаге: %d %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("no_layer_unchanged", func(t *testing.T) {
		h := editorHarness(t, layerHarnessOpts{Enabled: true, NoLayer: true})
		path := filepath.Join(h.roots.Xray, "01_log.json")
		rr := editorCall(t, h.api, h.api.ConfigSave, "/api/config/save?path="+path, `{"log":{}}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("сохранение без слоя: %d %s", rr.Code, rr.Body.String())
		}
	})
}

// Путь к managed-файлу через симлинк-каталог на корень Xray распознаётся (T-144-39).
func TestConfigSave_ManagedViaSymlinkDir(t *testing.T) {
	h := editorHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
	path := managedDiagPath(t, h)
	before, _ := os.ReadFile(path)

	link := filepath.Join(h.dataDir, "xray-link")
	if err := os.Symlink(h.roots.Xray, link); err != nil {
		t.Skipf("симлинки недоступны: %v", err)
	}
	viaLink := filepath.Join(link, configlayer.DiagXrayRel)

	rr := editorCall(t, h.api, h.api.ConfigSave, "/api/config/save?path="+viaLink, `{"x":1}`)
	assertFileManaged409(t, rr, "ConfigSave через симлинк")
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("содержимое managed-файла изменилось через симлинк: %q", after)
	}
}

// После успешной установки ядра слой в фоне собирает для него файлы (D-18);
// после неудачной установки сборки нет.
func TestKernelInstall_TriggersLayerBuild(t *testing.T) {
	t.Run("success_starts_build", func(t *testing.T) {
		h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		c := h.subscribe(t)
		c.wait(t, "snapshot", 5*time.Second, nil)

		h.api.onKernelInstallDone("xray", nil)
		c.wait(t, "apply_done", 10*time.Second, func(d string) bool { return strings.Contains(d, `"trigger":"kernel_installed"`) })
	})

	t.Run("failure_no_build", func(t *testing.T) {
		h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		c := h.subscribe(t)
		c.wait(t, "snapshot", 5*time.Second, nil)

		h.api.onKernelInstallDone("xray", errors.New("download failed"))
		select {
		case f := <-c.frames:
			if f.Event == "apply_done" || f.Event == "apply_step" {
				t.Fatalf("после неудачной установки запущена сборка: %s %s", f.Event, f.Data)
			}
		case <-time.After(700 * time.Millisecond):
		}
	})

	t.Run("no_layer_no_panic", func(t *testing.T) {
		api := &API{cfg: &config.Config{}}
		api.onKernelInstallDone("xray", nil)
	})

	t.Run("handler_failure_no_build", func(t *testing.T) {
		h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		kapi, _ := newKernelTestAPI(t)
		h.api.kernelSvc = kapi.kernelSvc // загрузка в нём всегда падает
		c := h.subscribe(t)
		c.wait(t, "snapshot", 5*time.Second, nil)

		if rr := postKernelInstall(h.api, "xray"); rr.Code != http.StatusOK {
			t.Fatalf("KernelInstall: %d %s", rr.Code, rr.Body.String())
		}
		waitKernelFailed(t, h.api, "xray")
		select {
		case f := <-c.frames:
			if f.Event == "apply_done" {
				t.Fatalf("неудачная установка запустила сборку: %s", f.Data)
			}
		case <-time.After(500 * time.Millisecond):
		}
	})
}
