package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// layerProcs — управляемые состояния процессов ядер для тестового слоя.
type layerProcs struct {
	mu sync.Mutex
	m  map[string]services.KernelProcessState
}

func newLayerProcs() *layerProcs {
	return &layerProcs{m: map[string]services.KernelProcessState{
		"xray":   {Name: "xray", Status: "not_installed"},
		"mihomo": {Name: "mihomo", Status: "not_installed"},
	}}
}

func (p *layerProcs) set(name, status string, pid int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[name] = services.KernelProcessState{Name: name, Status: status, PID: pid}
}

func (p *layerProcs) states() []services.KernelProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return []services.KernelProcessState{p.m["xray"], p.m["mihomo"]}
}

func (p *layerProcs) status(name string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[name].Status
}

// bump даёт всем запущенным ядрам новый PID (имитация успешного рестарта).
func (p *layerProcs) bump() {
	for _, st := range p.states() {
		if st.Status == "running" {
			p.set(st.Name, "running", st.PID+1)
		}
	}
}

// layerMihomo — заглушка управления Mihomo: слой с ней не ходит в сеть.
type layerMihomo struct{}

func (layerMihomo) ReloadConfig(string) error { return nil }
func (layerMihomo) ProviderCount(context.Context, string, string) (int, error) {
	return 0, fmt.Errorf("нет ответа")
}

// layerHarnessOpts — настройка тестового API со слоем.
type layerHarnessOpts struct {
	// Enabled — начальное значение флага config_layer.
	Enabled bool
	// DevMode — режим разработчика.
	DevMode bool
	// XrayRunning — фейковый xray установлен и запущен.
	XrayRunning bool
	// NoLayer — API без слоя (main.go его не подключил).
	NoLayer bool
	// XraySleepSec — сколько секунд фейковый xray «проверяет» конфигурацию
	// (держит применение занятым).
	XraySleepSec int
	// Restart подменяет перезапуск ядра (nil — все запущенные ядра получают новый PID).
	Restart func(h *layerHarness) (string, error)
}

// layerHarness — API с реальным Layer на временных каталогах и фейковых ядрах,
// HTTP-сервер поверх маршрутов слоя и настроек.
type layerHarness struct {
	api     *API
	cfg     *config.Config
	layer   *configlayer.Layer
	roots   configlayer.Roots
	dataDir string
	procs   *layerProcs
	srv     *httptest.Server
	// restarts — сколько раз вызывался перезапуск ядра.
	restarts atomic.Int32
	// eventsDone получает значение при выходе каждого обработчика SSE.
	eventsDone chan struct{}
}

func fastLayerTimings(t *testing.T) {
	t.Helper()
	oldC, oldP, oldS := configlayer.RestartConfirmTimeout, configlayer.RestartPollInterval, configlayer.RestartStableWindow
	configlayer.RestartConfirmTimeout = 2 * time.Second
	configlayer.RestartPollInterval = 10 * time.Millisecond
	configlayer.RestartStableWindow = 50 * time.Millisecond
	t.Cleanup(func() {
		configlayer.RestartConfirmTimeout, configlayer.RestartPollInterval, configlayer.RestartStableWindow = oldC, oldP, oldS
	})
}

func writeLayerTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newLayerHarness(t *testing.T, ho layerHarnessOpts) *layerHarness {
	t.Helper()
	fastLayerTimings(t)
	h := &layerHarness{
		dataDir:    t.TempDir(),
		procs:      newLayerProcs(),
		eventsDone: make(chan struct{}, 8),
	}
	h.roots = configlayer.Roots{Xray: t.TempDir(), Mihomo: t.TempDir()}
	writeLayerTestFile(t, filepath.Join(h.roots.Xray, "01_log.json"), `{"log":{}}`)
	writeLayerTestFile(t, filepath.Join(h.roots.Mihomo, "config.yaml"), "mixed-port: 7890\n")

	xrayBin := ""
	if ho.XrayRunning {
		xrayBin = filepath.Join(t.TempDir(), "xray")
		// Фейковый xray: проверка конфигурации всегда успешна, настоящий не запускается.
		script := "#!/bin/sh\nexit 0\n"
		if ho.XraySleepSec > 0 {
			script = fmt.Sprintf("#!/bin/sh\nsleep %d\nexit 0\n", ho.XraySleepSec)
		}
		if err := os.WriteFile(xrayBin, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		h.procs.set("xray", "running", 100)
	}

	h.cfg = &config.Config{
		DataDir:       h.dataDir,
		XRayConfigDir: h.roots.Xray,
		ConfigLayer:   ho.Enabled,
		DevMode:       ho.DevMode,
		ConfigPath:    filepath.Join(t.TempDir(), "config.json"),
	}
	h.api = &API{cfg: h.cfg}

	if !ho.NoLayer {
		restart := ho.Restart
		if restart == nil {
			restart = func(h *layerHarness) (string, error) {
				h.procs.bump()
				return "", nil
			}
		}
		applier := services.NewKernelApplierFunc(h.procs.status, func() string { return "" }, func() (string, error) {
			h.restarts.Add(1)
			return restart(h)
		}).WithLifecycleLock(h.api.LifecycleLock())
		layer, err := configlayer.New(configlayer.Options{
			DataDir: h.dataDir,
			Roots:   h.roots,
			Enabled: func() bool {
				h.cfg.RLock()
				defer h.cfg.RUnlock()
				return h.cfg.ConfigLayer
			},
			DevMode: func() bool {
				h.cfg.RLock()
				defer h.cfg.RUnlock()
				return h.cfg.DevMode
			},
			Binaries: func() configlayer.Binaries { return configlayer.Binaries{Xray: xrayBin} },
			XrayEnv:  func(string) []string { return nil },
			KernelVersions: func() []configlayer.KernelVersionInput {
				return []configlayer.KernelVersionInput{
					{Name: "xkeen", Installed: true, Version: "2.0"},
					{Name: "xray", Installed: xrayBin != "", Version: "1.8.24"},
					{Name: "mihomo", Installed: false},
				}
			},
			Applier:        applier,
			ProcessStates:  h.procs.states,
			Mihomo:         layerMihomo{},
			MihomoAPIReady: func() bool { return true },
			Lifecycle:      h.api.LifecycleLock(),
			DebounceDelay:  20 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("configlayer.New: %v", err)
		}
		h.layer = layer
		h.api.SetConfigLayer(layer)
		layer.Start()
		t.Cleanup(layer.Stop)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/configlayer/state", h.api.ConfigLayerState)
	mux.HandleFunc("/api/configlayer/events", func(w http.ResponseWriter, r *http.Request) {
		defer func() { h.eventsDone <- struct{}{} }()
		h.api.ConfigLayerEvents(w, r)
	})
	mux.HandleFunc("/api/configlayer/draft", h.api.ConfigLayerDraft)
	mux.HandleFunc("/api/configlayer/draft/reset", h.api.ConfigLayerDraftReset)
	mux.HandleFunc("/api/configlayer/apply", h.api.ConfigLayerApply)
	mux.HandleFunc("/api/configlayer/files/rebuild", h.api.ConfigLayerFilesRebuild)
	mux.HandleFunc("/api/configlayer/files/release", h.api.ConfigLayerFilesRelease)
	mux.HandleFunc("/api/configlayer/files/diff", h.api.ConfigLayerFilesDiff)
	mux.HandleFunc("/api/configlayer/notices/dismiss", h.api.ConfigLayerNoticesDismiss)
	mux.HandleFunc("/api/configlayer/diag", h.api.ConfigLayerDiag)
	mux.HandleFunc("/api/settings", h.api.SettingsGet)
	mux.HandleFunc("/api/settings/config-layer", h.api.SettingsConfigLayer)
	h.srv = httptest.NewServer(i18n.Middleware(mux))
	t.Cleanup(h.srv.Close)
	return h
}

// envelope — разобранный ответ API.
type envelope struct {
	Status int
	APIResponse
	Raw []byte
}

// data разбирает поле data ответа в карту сырых значений.
func (e envelope) data(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(e.Data(), &m); err != nil {
		t.Fatalf("data не объект: %v; тело: %s", err, e.Raw)
	}
	return m
}

// Data — сырое значение поля data.
func (e envelope) Data() []byte {
	var raw struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(e.Raw, &raw)
	return raw.Data
}

// do отправляет запрос на сервер стенда на русском языке.
func (h *layerHarness) do(t *testing.T, method, path, body string) envelope {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Language", "ru")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	env := envelope{Status: resp.StatusCode, Raw: raw}
	_ = json.Unmarshal(raw, &env.APIResponse)
	return env
}

// sseFrame — одно событие SSE.
type sseFrame struct {
	Event string
	Data  string
}

// sseClient — подключение к GET /api/configlayer/events.
type sseClient struct {
	frames chan sseFrame
	cancel context.CancelFunc
}

func (h *layerHarness) subscribe(t *testing.T) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/configlayer/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("подключение к SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		resp.Body.Close()
		t.Fatalf("SSE: статус %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		cancel()
		resp.Body.Close()
		t.Fatalf("SSE: Content-Type %q", ct)
	}
	c := &sseClient{frames: make(chan sseFrame, 64), cancel: cancel}
	go func() {
		defer close(c.frames)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		var cur sseFrame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if cur.Event != "" {
					c.frames <- cur
				}
				cur = sseFrame{}
			case strings.HasPrefix(line, "event: "):
				cur.Event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	t.Cleanup(cancel)
	return c
}

// wait ждёт событие name, для которого pred (может быть nil) вернул true.
func (c *sseClient) wait(t *testing.T, name string, timeout time.Duration, pred func(string) bool) sseFrame {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				t.Fatalf("поток SSE закрыт, ожидалось событие %q", name)
			}
			if f.Event == name && (pred == nil || pred(f.Data)) {
				return f
			}
		case <-deadline:
			t.Fatalf("не дождались события %q за %v", name, timeout)
		}
	}
}

func TestHandler_StateShape(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	env := h.do(t, http.MethodGet, "/api/configlayer/state", "")
	if env.Status != http.StatusOK || !env.Success {
		t.Fatalf("статус %d, тело %s", env.Status, env.Raw)
	}
	data := env.data(t)
	for _, key := range []string{"enabled", "dev_mode", "draft_revision", "draft_changes", "drift_count", "files", "kernels", "features", "apply", "notices"} {
		if _, ok := data[key]; !ok {
			t.Errorf("в снимке нет ключа %q: %s", key, env.Raw)
		}
	}
	var enabled bool
	_ = json.Unmarshal(data["enabled"], &enabled)
	if !enabled {
		t.Error("enabled должен быть true")
	}
	var kernels []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data["kernels"], &kernels); err != nil {
		t.Fatalf("kernels: %v", err)
	}
	var names []string
	for _, k := range kernels {
		names = append(names, k.Name)
	}
	if got := strings.Join(names, ","); got != "xkeen,xray,mihomo" {
		t.Errorf("строки ядер = %q, нужно xkeen,xray,mihomo", got)
	}
}

func TestHandler_DraftThenSSE(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	c := h.subscribe(t)
	first := c.wait(t, "snapshot", 5*time.Second, nil)
	if !strings.Contains(first.Data, `"draft_revision":0`) {
		t.Errorf("snapshot: %s", first.Data)
	}

	env := h.do(t, http.MethodPost, "/api/configlayer/draft", `{"revision":0,"section":"diag","value":{"enabled":true}}`)
	if env.Status != http.StatusOK || !env.Success {
		t.Fatalf("POST draft: %d %s", env.Status, env.Raw)
	}
	data := env.data(t)
	if string(data["draft_revision"]) != "1" || string(data["draft_changes"]) != "1" {
		t.Errorf("ответ draft: %s", env.Raw)
	}
	c.wait(t, "draft", 5*time.Second, func(d string) bool { return strings.Contains(d, `"draft_revision":1`) })

	// Отмена запроса завершает обработчик: горутина не висит.
	c.cancel()
	select {
	case <-h.eventsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("обработчик SSE не завершился после отмены запроса")
	}
}

func TestHandler_DraftConflict409(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	body := `{"revision":0,"section":"diag","value":{"enabled":true}}`
	if env := h.do(t, http.MethodPost, "/api/configlayer/draft", body); env.Status != http.StatusOK {
		t.Fatalf("первый POST: %d %s", env.Status, env.Raw)
	}
	env := h.do(t, http.MethodPost, "/api/configlayer/draft", body)
	if env.Status != http.StatusConflict || env.Code != "draft_conflict" {
		t.Fatalf("второй POST: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
	if env.Error == "" || env.Error == "configlayer.draft_conflict" || !strings.Contains(env.Error, "Черновик") {
		t.Errorf("сообщение не переведено: %q", env.Error)
	}
}

func TestHandler_DisabledReturns404(t *testing.T) {
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/configlayer/state", ""},
		{http.MethodGet, "/api/configlayer/events", ""},
		{http.MethodPost, "/api/configlayer/draft", `{"revision":0,"section":"diag","value":{}}`},
		{http.MethodPost, "/api/configlayer/draft/reset", `{"revision":0}`},
		{http.MethodPost, "/api/configlayer/apply", `{}`},
		{http.MethodPost, "/api/configlayer/files/rebuild", `{"all":true}`},
		{http.MethodPost, "/api/configlayer/files/release", `{"key":"x"}`},
		{http.MethodGet, "/api/configlayer/files/diff?key=x", ""},
		{http.MethodPost, "/api/configlayer/notices/dismiss", `{"id":"schema_reset"}`},
		{http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`},
	}
	cases := map[string]layerHarnessOpts{
		"флаг выключен":  {Enabled: false},
		"слой не создан": {Enabled: true, NoLayer: true},
	}
	for name, ho := range cases {
		t.Run(name, func(t *testing.T) {
			h := newLayerHarness(t, ho)
			for _, rt := range routes {
				env := h.do(t, rt.method, rt.path, rt.body)
				if env.Status != http.StatusNotFound || env.Code != "config_layer_disabled" {
					t.Errorf("%s %s: статус %d code=%q %s", rt.method, rt.path, env.Status, env.Code, env.Raw)
				}
			}
		})
	}
}

// layerFileKey возвращает ключ единственного (первого) файла слоя из снимка.
func (h *layerHarness) layerFileKey(t *testing.T) string {
	t.Helper()
	env := h.do(t, http.MethodGet, "/api/configlayer/state", "")
	var files []struct {
		Key   string `json:"key"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(env.data(t)["files"], &files); err != nil || len(files) == 0 {
		t.Fatalf("в снимке нет файлов: %v; %s", err, env.Raw)
	}
	return files[0].Key
}

// applyDiagAndWait кладёт диагностический файл в черновик, применяет и ждёт apply_done.
func (h *layerHarness) applyDiagAndWait(t *testing.T, c *sseClient) {
	t.Helper()
	env := h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`)
	if env.Status != http.StatusOK {
		t.Fatalf("diag add: %d %s", env.Status, env.Raw)
	}
	env = h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`)
	if env.Status != http.StatusOK {
		t.Fatalf("apply: %d %s", env.Status, env.Raw)
	}
	done := c.wait(t, "apply_done", 15*time.Second, nil)
	if !strings.Contains(done.Data, `"applied"`) {
		t.Fatalf("применение не завершилось успехом: %s", done.Data)
	}
}

func TestHandler_ApplyStarted(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)

	if env := h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`); env.Status != http.StatusOK {
		t.Fatalf("diag: %d %s", env.Status, env.Raw)
	}
	env := h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`)
	if env.Status != http.StatusOK || string(env.data(t)["started"]) != "true" {
		t.Fatalf("apply: %d %s", env.Status, env.Raw)
	}
	c.wait(t, "apply_step", 15*time.Second, nil)
	done := c.wait(t, "apply_done", 15*time.Second, nil)
	if !strings.Contains(done.Data, `"applied"`) {
		t.Errorf("apply_done: %s", done.Data)
	}
}

func TestHandler_ApplyCodes(t *testing.T) {
	t.Run("apply_busy", func(t *testing.T) {
		h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true, XraySleepSec: 1})
		c := h.subscribe(t)
		c.wait(t, "snapshot", 5*time.Second, nil)
		h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`)
		if env := h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`); env.Status != http.StatusOK {
			t.Fatalf("первый apply: %d %s", env.Status, env.Raw)
		}
		env := h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`)
		if env.Status != http.StatusConflict || env.Code != "apply_busy" {
			t.Fatalf("второй apply: %d code=%q %s", env.Status, env.Code, env.Raw)
		}
		c.wait(t, "apply_done", 15*time.Second, nil)
	})
	t.Run("kernel_op_in_progress", func(t *testing.T) {
		h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		lock := h.api.LifecycleLock()
		lock.Lock()
		defer lock.Unlock()
		env := h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`)
		if env.Status != http.StatusConflict || env.Code != "kernel_op_in_progress" {
			t.Fatalf("apply при занятом замке: %d code=%q %s", env.Status, env.Code, env.Raw)
		}
	})
	t.Run("drift_blocked", func(t *testing.T) {
		h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
		c := h.subscribe(t)
		c.wait(t, "snapshot", 5*time.Second, nil)
		h.applyDiagAndWait(t, c)
		before := h.restarts.Load()

		// Ручная правка файла панели в обход слоя.
		path := filepath.Join(h.roots.Xray, configlayer.DiagXrayRel)
		if err := os.WriteFile(path, []byte(`{"manual":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		env := h.do(t, http.MethodPost, "/api/configlayer/apply", `{}`)
		if env.Status != http.StatusConflict || env.Code != "drift_blocked" {
			t.Fatalf("apply при дрейфе: %d code=%q %s", env.Status, env.Code, env.Raw)
		}
		if h.restarts.Load() != before {
			t.Error("при дрейфе ядро не должно перезапускаться")
		}
	})
}

func TestHandler_FilesActions(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: true, XrayRunning: true})
	c := h.subscribe(t)
	c.wait(t, "snapshot", 5*time.Second, nil)
	h.applyDiagAndWait(t, c)
	key := h.layerFileKey(t)

	// Принять правку.
	env := h.do(t, http.MethodPost, "/api/configlayer/files/release", fmt.Sprintf(`{"key":%q}`, key))
	if env.Status != http.StatusOK {
		t.Fatalf("release: %d %s", env.Status, env.Raw)
	}
	data := env.data(t)
	if _, ok := data["files"]; !ok {
		t.Errorf("в ответе release нет files: %s", env.Raw)
	}
	if _, ok := data["drift_count"]; !ok {
		t.Errorf("в ответе release нет drift_count: %s", env.Raw)
	}
	if !strings.Contains(string(data["files"]), "released") {
		t.Errorf("файл не стал released: %s", env.Raw)
	}

	// diff отпущенного файла — 409.
	env = h.do(t, http.MethodGet, "/api/configlayer/files/diff?key="+url.QueryEscape(key), "")
	if env.Status != http.StatusConflict || env.Code != "file_released" {
		t.Errorf("diff released: %d code=%q %s", env.Status, env.Code, env.Raw)
	}

	// Пересобрать по ключу возвращает файл под управление.
	env = h.do(t, http.MethodPost, "/api/configlayer/files/rebuild", fmt.Sprintf(`{"keys":[%q]}`, key))
	if env.Status != http.StatusOK || string(env.data(t)["started"]) != "true" {
		t.Fatalf("rebuild по ключу: %d %s", env.Status, env.Raw)
	}
	c.wait(t, "apply_done", 15*time.Second, nil)

	// diff управляемого файла — обе стороны.
	env = h.do(t, http.MethodGet, "/api/configlayer/files/diff?key="+url.QueryEscape(key), "")
	if env.Status != http.StatusOK {
		t.Fatalf("diff: %d %s", env.Status, env.Raw)
	}
	for _, field := range []string{"key", "expected", "actual", "missing", "truncated"} {
		if _, ok := env.data(t)[field]; !ok {
			t.Errorf("в ответе diff нет поля %q: %s", field, env.Raw)
		}
	}

	// «Пересобрать всё» без дрейфа и неизвестные ключи — 404 unknown_key.
	cases := []struct{ name, method, path, body string }{
		{"rebuild all без дрейфа", http.MethodPost, "/api/configlayer/files/rebuild", `{"all":true}`},
		{"rebuild неизвестный ключ", http.MethodPost, "/api/configlayer/files/rebuild", `{"keys":["nope"]}`},
		{"release неизвестный ключ", http.MethodPost, "/api/configlayer/files/release", `{"key":"nope"}`},
		{"diff неизвестный ключ", http.MethodGet, "/api/configlayer/files/diff?key=nope", ""},
		{"diff путь вместо ключа", http.MethodGet, "/api/configlayer/files/diff?key=" + url.QueryEscape("../../etc/passwd"), ""},
	}
	for _, tc := range cases {
		env := h.do(t, tc.method, tc.path, tc.body)
		if env.Status != http.StatusNotFound || env.Code != "unknown_key" {
			t.Errorf("%s: %d code=%q %s", tc.name, env.Status, env.Code, env.Raw)
		}
	}
}

func TestHandler_NoticesDismiss(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	env := h.do(t, http.MethodPost, "/api/configlayer/notices/dismiss", `{"id":"nope"}`)
	if env.Status != http.StatusNotFound || env.Code != "unknown_notice" {
		t.Fatalf("неизвестное уведомление: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
}

func TestHandler_DiagRequiresDevMode(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true, DevMode: false})
	env := h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`)
	if env.Status != http.StatusForbidden || env.Code != "dev_mode_required" {
		t.Fatalf("diag вне dev_mode: %d code=%q %s", env.Status, env.Code, env.Raw)
	}

	h.cfg.Lock()
	h.cfg.DevMode = true
	h.cfg.Unlock()
	env = h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":7,"action":"add"}`)
	if env.Status != http.StatusConflict || env.Code != "draft_conflict" {
		t.Fatalf("diag с устаревшей ревизией: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
	env = h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"unknown"}`)
	if env.Status != http.StatusBadRequest || env.Code != "invalid_request" {
		t.Fatalf("неизвестное действие: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
	env = h.do(t, http.MethodPost, "/api/configlayer/diag", `{"revision":0,"action":"add"}`)
	if env.Status != http.StatusOK {
		t.Fatalf("diag в dev_mode: %d %s", env.Status, env.Raw)
	}
	if string(env.data(t)["draft_revision"]) != "1" {
		t.Errorf("ответ diag: %s", env.Raw)
	}
}

func TestHandler_DraftReset(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	h.do(t, http.MethodPost, "/api/configlayer/draft", `{"revision":0,"section":"diag","value":{"enabled":true}}`)
	env := h.do(t, http.MethodPost, "/api/configlayer/draft/reset", `{"revision":0}`)
	if env.Status != http.StatusConflict || env.Code != "draft_conflict" {
		t.Fatalf("reset со старой ревизией: %d code=%q %s", env.Status, env.Code, env.Raw)
	}
	env = h.do(t, http.MethodPost, "/api/configlayer/draft/reset", `{"revision":1}`)
	if env.Status != http.StatusOK || string(env.data(t)["draft_changes"]) != "0" {
		t.Fatalf("reset: %d %s", env.Status, env.Raw)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/configlayer/state"},
		{http.MethodPost, "/api/configlayer/events"},
		{http.MethodGet, "/api/configlayer/draft"},
		{http.MethodGet, "/api/configlayer/draft/reset"},
		{http.MethodGet, "/api/configlayer/apply"},
		{http.MethodGet, "/api/configlayer/files/rebuild"},
		{http.MethodGet, "/api/configlayer/files/release"},
		{http.MethodPost, "/api/configlayer/files/diff"},
		{http.MethodGet, "/api/configlayer/notices/dismiss"},
		{http.MethodGet, "/api/configlayer/diag"},
	}
	for _, rt := range routes {
		body := ""
		if rt.method == http.MethodPost {
			body = `{}`
		}
		if env := h.do(t, rt.method, rt.path, body); env.Status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: статус %d, нужен 405", rt.method, rt.path, env.Status)
		}
	}
}

func TestHandler_BodyLimit(t *testing.T) {
	h := newLayerHarness(t, layerHarnessOpts{Enabled: true})
	body := `{"revision":0,"section":"diag","value":"` + strings.Repeat("x", 2<<20) + `"}`
	env := h.do(t, http.MethodPost, "/api/configlayer/draft", body)
	if env.Status != http.StatusBadRequest && env.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("тело больше 1 МБ: статус %d, нужен 400 или 413", env.Status)
	}
	// Слой остался рабочим: черновик не изменился.
	state := h.do(t, http.MethodGet, "/api/configlayer/state", "")
	if string(state.data(t)["draft_revision"]) != "0" {
		t.Errorf("черновик изменился после отказа: %s", state.Raw)
	}
}
