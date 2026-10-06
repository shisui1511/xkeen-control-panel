package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// diskConfigLayer читает значение config_layer из config.json на диске.
func diskConfigLayer(t *testing.T, h *layerHarness) bool {
	t.Helper()
	raw, err := os.ReadFile(h.cfg.ConfigPath)
	if err != nil {
		t.Fatalf("config.json не записан: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("config.json: %v", err)
	}
	v, ok := m["config_layer"]
	if !ok {
		t.Fatalf("в config.json нет ключа config_layer: %s", raw)
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		t.Fatalf("config_layer: %v", err)
	}
	return b
}

// findInDir ищет файл с именем name под root; возвращает пути всех совпадений.
func findInDir(t *testing.T, root, name string) []string {
	t.Helper()
	var found []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == name {
			found = append(found, path)
		}
		return nil
	})
	return found
}

func TestSettings_ConfigLayerInGet(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{})
	env := h.do(t, http.MethodGet, "/api/settings", "")
	if env.Status != http.StatusOK {
		t.Fatalf("GET /api/settings: %d %s", env.Status, env.Raw)
	}
	v, ok := env.data(t)["config_layer"]
	if !ok {
		t.Fatalf("в ответе нет config_layer: %s", env.Raw)
	}
	if string(v) != "false" {
		t.Errorf("config_layer = %s, по умолчанию должно быть false", v)
	}
}

func TestSettings_ConfigLayerEnable(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{XrayRunning: true})
	// SSE при выключенном флаге отвечает 404, поэтому слушаем шину слоя напрямую.
	events, cancel, err := h.layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	env := h.do(t, http.MethodPost, "/api/settings/config-layer", `{"enabled":true}`)
	if env.Status != http.StatusOK || string(env.data(t)["config_layer"]) != "true" {
		t.Fatalf("POST enable: %d %s", env.Status, env.Raw)
	}
	if !diskConfigLayer(t, h) {
		t.Error("config.json должен содержать config_layer: true")
	}
	// Слой собирает файлы из applied в фоне: событие apply_done с триггером flag_on.
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("шина слоя закрыта до apply_done")
			}
			if ev.Type != configlayer.EventApplyDone {
				continue
			}
			raw, _ := json.Marshal(ev.Data)
			if strings.Contains(string(raw), `"flag_on"`) {
				return
			}
		case <-deadline:
			t.Fatal("не дождались apply_done с триггером flag_on")
		}
	}
}

func TestSettings_ConfigLayerDisable(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)
	h.applyDiagAndWait(t, c)
	diagPath := filepath.Join(h.roots.Xray, configlayer.DiagXrayRel)
	if _, err := os.Stat(diagPath); err != nil {
		t.Fatalf("диагностический файл не записан: %v", err)
	}

	env := h.do(t, http.MethodPost, "/api/settings/config-layer", `{"enabled":false}`)
	if env.Status != http.StatusOK || string(env.data(t)["config_layer"]) != "false" {
		t.Fatalf("POST disable: %d %s", env.Status, env.Raw)
	}
	if _, err := os.Stat(diagPath); !os.IsNotExist(err) {
		t.Errorf("файл слоя должен быть удалён из рабочего каталога, err=%v", err)
	}
	if got := findInDir(t, h.dataDir, filepath.Base(diagPath)); len(got) == 0 {
		t.Error("копия файла слоя не найдена в наборе копий")
	}
	if diskConfigLayer(t, h) {
		t.Error("config.json должен содержать config_layer: false")
	}
	h.cfg.RLock()
	defer h.cfg.RUnlock()
	if h.cfg.ConfigLayer {
		t.Error("флаг в памяти должен быть снят")
	}
}

func TestSettings_ConfigLayerDisableFails(t *testing.T) {
	var failRestart atomic.Bool
	h := newLayerHarness(t, layerHarnessOpts{
		Enabled: true, DevMode: true, XrayRunning: true,
		Restart: func(h *layerHarness) (string, error) {
			if failRestart.Load() {
				return "", errors.New("не стартует")
			}
			h.procs.bump()
			return "", nil
		},
	})
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)
	h.applyDiagAndWait(t, c)
	// Первое сохранение config.json: флаг уже включён.
	if env := h.do(t, http.MethodPost, "/api/settings/config-layer", `{"enabled":true}`); env.Status != http.StatusOK {
		t.Fatalf("POST enable (уже включён): %d %s", env.Status, env.Raw)
	}
	failRestart.Store(true)

	env := h.do(t, http.MethodPost, "/api/settings/config-layer", `{"enabled":false}`)
	if env.Status != http.StatusInternalServerError || env.Code != "config_layer_disable_failed" {
		t.Fatalf("POST disable со сбоем: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
	if env.Detail == "" {
		t.Error("в ответе нет детали причины сбоя")
	}
	h.cfg.RLock()
	inMemory := h.cfg.ConfigLayer
	h.cfg.RUnlock()
	if !inMemory {
		t.Error("флаг должен остаться включённым в памяти")
	}
	if raw, err := os.ReadFile(h.cfg.ConfigPath); err == nil {
		if !diskConfigLayer(t, h) {
			t.Errorf("config.json: флаг должен остаться true: %s", raw)
		}
	}
}

func TestSettings_ConfigLayerDisableBusy(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true, XraySleepSec: 1})
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)
	h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`)
	if env := h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`); env.Status != http.StatusOK {
		t.Fatalf("apply: %d %s", env.Status, env.Raw)
	}

	env := h.do(t, http.MethodPost, "/api/settings/config-layer", `{"enabled":false}`)
	if env.Status != http.StatusConflict || env.Code != "apply_busy" {
		t.Fatalf("disable при идущем применении: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
	h.cfg.RLock()
	inMemory := h.cfg.ConfigLayer
	h.cfg.RUnlock()
	if !inMemory {
		t.Error("флаг не должен меняться при занятом применении")
	}
	c.wait(t, "apply_done", 15*time.Second, nil)
}

func TestSettings_ConfigLayerNoLayer(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{NoLayer: true})
	env := h.do(t, http.MethodPost, "/api/settings/config-layer", `{"enabled":true}`)
	if env.Status != http.StatusOK || string(env.data(t)["config_layer"]) != "true" {
		t.Fatalf("POST enable без слоя: %d %s", env.Status, env.Raw)
	}
	if !diskConfigLayer(t, h) {
		t.Error("ключ должен быть сохранён: слой подключится после перезапуска панели")
	}
}
