//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Список провайдеров правил при остановленном Mihomo (D-21): панель читает
// config.yaml активного профиля и файлы на диске, к API ядра не обращается.
// Часть файловая: настоящий RouteTracerService над временным каталогом без
// ядра. Часть живая: GET /api/rule-providers/info на панели устройства при
// любом активном ядре — маршрут GateData, 409 быть не должно. Метка ядра не
// нужна: исход от ядра не зависит.

const rtRpProfile = `mode: rule
rule-providers:
  rt-http-file:
    type: http
    behavior: domain
    format: text
    url: "https://rules.example.invalid/a.txt?token=probe"
    path: ./rules/a.txt
    proxy: grp-a
  rt-http-nofile:
    type: http
    behavior: ipcidr
    format: mrs
    url: "https://rules.example.invalid/b.mrs"
    path: ./rules/b.mrs
  rt-file-nofile:
    type: file
    behavior: domain
    format: text
    path: ./rules/c.txt
  rt-file-empty:
    type: file
    behavior: domain
    format: text
    path: ./rules/d.txt
  rt-inline:
    type: inline
    behavior: domain
    payload:
      - "+.example.com"
      - "full.example.org"
rules:
  - MATCH,DIRECT
`

func TestRouterRuleProvidersStopped(t *testing.T) {
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "rp-stopped-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(rtRpProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "+.example.com\nfull.example.org\n"
	fileA := filepath.Join(dir, "rules", "a.txt")
	if err := os.WriteFile(fileA, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(fileA, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "d.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Без ядра и без сервиса Mihomo: список строится только по файлам.
	svc := services.NewRouteTracerService(nil, nil, dir)
	list, err := svc.ListRuleProviders()
	if err != nil {
		t.Fatalf("ListRuleProviders: %v", err)
	}
	byName := map[string]services.RuleProviderInfo{}
	for _, it := range list {
		byName[it.Name] = it
	}
	if len(byName) != 5 {
		t.Fatalf("провайдеров %d, нужно 5", len(byName))
	}

	a := byName["rt-http-file"]
	if a.Type != "http" || a.Proxy != "grp-a" || !a.FileExists ||
		a.FileSize != int64(len(body)) || a.FileMtime != mtime.Unix() {
		t.Errorf("rt-http-file: тип %q, proxy %q, файл %v, размер %d, дата %d", a.Type, a.Proxy, a.FileExists, a.FileSize, a.FileMtime)
	}
	if a.URL != "https://rules.example.invalid/a.txt" || strings.Contains(a.URL, "token") {
		t.Errorf("rt-http-file: адрес показан не очищенным: %q", a.URL)
	}
	if a.Path != filepath.Join("rules", "a.txt") {
		t.Errorf("rt-http-file: путь %q", a.Path)
	}
	if b := byName["rt-http-nofile"]; b.FileExists || b.FileSize != 0 || b.Proxy != "" || b.Format != "mrs" {
		t.Errorf("rt-http-nofile: %+v", b)
	}
	if c := byName["rt-file-nofile"]; c.FileExists || c.URL != "" || c.Type != "file" {
		t.Errorf("rt-file-nofile: %+v", c)
	}
	if d := byName["rt-file-empty"]; !d.FileExists || d.FileSize != 0 {
		t.Errorf("rt-file-empty: файл %v, размер %d", d.FileExists, d.FileSize)
	}
	if in := byName["rt-inline"]; in.Type != "inline" || in.Inline != 2 || !in.FileExists {
		t.Errorf("rt-inline: %+v", in)
	}

	// Живая панель: маршрут GateData отвечает 200 при любом активном ядре.
	e := rtNewSrc(t)
	r := e.call(http.MethodGet, "/api/rule-providers/info", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET /api/rule-providers/info при любом активном ядре: %d %s", r.Status, r.Code)
	}
	var live []map[string]any
	if err := json.Unmarshal(r.Data, &live); err != nil {
		t.Fatalf("data ответа не список: %v", err)
	}
	for _, it := range live {
		if u, _ := it["url"].(string); strings.Contains(u, "?") {
			t.Errorf("адрес провайдера %v показан с query", it["name"])
		}
	}
}

// Проверка адреса провайдера с учётом proxy (D-20). Настоящий процесс Mihomo
// запускается отдельно от ядра стенда во временном каталоге (unix-сокет в нём,
// портов нет): узлы rt-direct (type: direct) и rt-dead (socks5 на закрытый порт
// loopback). Профиль с провайдерами для панели лежит в отдельном каталоге: так
// отдельное ядро не пытается скачать провайдеры при старте. Проба через ядро —
// настоящий MihomoService.ProxyDelay. Работающее ядро стенда не трогается, метка
// ядра не нужна. Прямая проба идёт через SafeHTTPClient и loopback отвергает, так
// что для адресов теста «напрямую» всегда провал.

const (
	rtRpMihomoBin = "/opt/sbin/mihomo"
	// rtRpPublicURL — общедоступный адрес-константа для случая «напрямую да,
	// через прокси нет».
	rtRpPublicURL = "https://example.com/"
	// rtRpSecretMark — метка в query адреса теста: в текст ошибок попадать не должна.
	rtRpSecretMark = "rtprobesecret"
)

// rtRpStartCore запускает отдельный процесс Mihomo и ждёт ответа Clash API по
// unix-сокету. Процесс останавливается в t.Cleanup.
func rtRpStartCore(t *testing.T, dir string, svc *services.MihomoService) {
	t.Helper()
	logPath := filepath.Join(dir, "core.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(rtRpMihomoBin, "-d", dir, "-f", filepath.Join(dir, "config.yaml"))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("процесс mihomo не запустился: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			_ = cmd.Process.Kill()
			select {
			case <-exited:
			case <-time.After(20 * time.Second):
			}
		}
		_ = logFile.Close()
	})
	tail := func() string {
		data, _ := os.ReadFile(logPath)
		if len(data) > 2000 {
			data = data[len(data)-2000:]
		}
		return string(data)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if reachable, _ := svc.ProbeAPI(""); reachable {
			return
		}
		select {
		case <-exited:
			t.Fatalf("mihomo завершился до готовности API: %s", tail())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Clash API не ответил за 60 с: %s", tail())
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// rtRpClosedPort возвращает порт loopback, на котором никто не слушает.
func rtRpClosedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("порт: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	return port
}

func TestRouterRuleProviderCheckURL(t *testing.T) {
	tg := routertest.Current(t)
	if _, err := os.Stat(rtRpMihomoBin); err != nil {
		t.Skipf("Mihomo не установлен на устройстве: %v", err)
	}
	root, err := os.MkdirTemp(tg.WorkDir, "rp-check-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	coreDir := filepath.Join(root, "core")
	profDir := filepath.Join(root, "prof")
	for _, d := range []string{coreDir, profDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	testURL := srv.URL + "/rules.txt?token=" + rtRpSecretMark

	// Контроллер описан до proxies: так его читает ParseControllerConfig.
	core := "mode: rule\n" +
		"log-level: warning\n" +
		"external-controller-unix: " + filepath.Join(coreDir, "ctl.sock") + "\n" +
		"proxies:\n" +
		"  - {name: rt-direct, type: direct}\n" +
		"  - {name: rt-dead, type: socks5, server: 127.0.0.1, port: " + rtRpClosedPort(t) + "}\n" +
		"rules:\n" +
		"  - MATCH,DIRECT\n"
	if err := os.WriteFile(filepath.Join(coreDir, "config.yaml"), []byte(core), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := services.NewMihomoService(rtRpMihomoBin, "", coreDir)
	rtRpStartCore(t, coreDir, svc)

	provider := func(name, rawURL, proxy string) string {
		s := "  " + name + ":\n    type: http\n    behavior: domain\n    format: text\n" +
			"    url: \"" + rawURL + "\"\n    path: ./rules/" + name + ".txt\n"
		if proxy != "" {
			s += "    proxy: " + proxy + "\n"
		}
		return s
	}
	profile := "mode: rule\nrule-providers:\n" +
		provider("rt-via-ok", testURL, "rt-direct") +
		provider("rt-via-dead", testURL, "rt-dead") +
		provider("rt-via-absent", testURL, "rt-absent-proxy") +
		provider("rt-kw-direct", testURL, "DIRECT") +
		provider("rt-no-proxy", testURL, "") +
		provider("rt-public-dead", rtRpPublicURL, "rt-dead") +
		"rules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(filepath.Join(profDir, "config.yaml"), []byte(profile), 0o644); err != nil {
		t.Fatal(err)
	}
	tracer := services.NewRouteTracerService(nil, nil, profDir)

	calls := 0
	real := func(ctx context.Context, proxy, rawURL string) (int, error) {
		calls++
		return svc.ProxyDelay(ctx, proxy, rawURL, services.RuleProviderProbeTimeout)
	}
	check := func(name string, via services.ProxyProbe) *services.RuleProviderURLCheck {
		t.Helper()
		res, err := tracer.CheckRuleProviderURL(context.Background(), name, via)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, _ := json.Marshal(res)
		if strings.Contains(string(raw), rtRpSecretMark) {
			t.Errorf("%s: в ответе остался query адреса", name)
		}
		if strings.Contains(string(raw), srv.Listener.Addr().String()) {
			t.Errorf("%s: в тексте ответа адрес сервера теста", name)
		}
		return res
	}

	t.Run("ViaProxyOK", func(t *testing.T) {
		calls = 0
		res := check("rt-via-ok", real)
		if calls != 1 || res.ViaProxy == nil || !res.ViaProxy.OK || res.ViaProxy.Error != "" {
			t.Errorf("через rt-direct: вызовов %d, проба %+v", calls, res.ViaProxy)
		}
		if res.Direct != nil || res.DirectOnly || !res.OK || res.Proxy != "rt-direct" {
			t.Errorf("через rt-direct: прямая проба %+v, direct_only %v, ok %v, proxy %q", res.Direct, res.DirectOnly, res.OK, res.Proxy)
		}
	})

	t.Run("ViaProxyDead", func(t *testing.T) {
		res := check("rt-via-dead", real)
		if res.ViaProxy == nil || res.ViaProxy.OK || res.ViaProxy.Error == "" {
			t.Errorf("через rt-dead: проба %+v (ожидался отказ с текстом)", res.ViaProxy)
		}
		if res.Direct == nil || res.Direct.OK || res.OK {
			t.Errorf("через rt-dead: прямая проба %+v, ok %v (loopback напрямую отвергается)", res.Direct, res.OK)
		}
	})

	t.Run("ProxyAbsentInCore", func(t *testing.T) {
		res := check("rt-via-absent", real)
		if res.ViaProxy == nil || res.ViaProxy.OK || !strings.Contains(res.ViaProxy.Error, "нет в запущенном") {
			t.Errorf("прокси вне ядра: проба %+v (ожидался текст «нет в запущенном ядре»)", res.ViaProxy)
		}
		if res.Direct == nil {
			t.Error("прокси вне ядра: прямая проба не выполнена")
		}
	})

	t.Run("DirectOnlyWithoutProxy", func(t *testing.T) {
		for _, name := range []string{"rt-kw-direct", "rt-no-proxy"} {
			calls = 0
			res := check(name, real)
			if calls != 0 || res.ViaProxy != nil || res.Direct == nil || res.DirectOnly {
				t.Errorf("%s: вызовов %d, via %+v, direct %+v, direct_only %v", name, calls, res.ViaProxy, res.Direct, res.DirectOnly)
			}
		}
	})

	t.Run("CoreApiUnavailable", func(t *testing.T) {
		res := check("rt-via-ok", nil)
		if !res.DirectOnly || res.ViaProxy != nil || res.Direct == nil {
			t.Errorf("без API ядра: direct_only %v, via %+v, direct %+v", res.DirectOnly, res.ViaProxy, res.Direct)
		}
	})

	t.Run("DirectWorksViaProxyDead", func(t *testing.T) {
		res := check("rt-public-dead", real)
		if res.ViaProxy == nil || res.ViaProxy.OK || res.Direct == nil {
			t.Fatalf("общедоступный адрес через rt-dead: via %+v, direct %+v", res.ViaProxy, res.Direct)
		}
		// Сеть стенда — не дефект продукта: при пропаже интернета только пишем.
		if !res.Direct.OK {
			t.Logf("напрямую не прошло (сеть стенда?): код %d, текст %q", res.Direct.StatusCode, res.Direct.Error)
		}
	})
}
