//go:build router

package sources

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Подписки на устройстве: запрос под ядро, HWID, снимки, добавление и «Применить».
// Сеть настоящая: клиент панели `utils.SafeHTTPClient`, обёрнутый только наблюдателем
// заголовков исходящих запросов (ответов он не подменяет). Поведение с живым
// провайдером проверяется, когда задан XCP_RT_SUBSCRIPTION_URL (адрес подписки из
// локального окружения запуска; в код, отчёты и PR он не попадает); без него проверка
// пропускается, а проверки отказов идут на настоящей сети устройства.

// rtReq — заголовки одного исходящего запроса подписки.
type rtReq struct {
	UA, HWID, OS, Model, Ver string
}

// rtCapture наблюдает исходящие запросы клиента панели.
type rtCapture struct {
	mu   sync.Mutex
	reqs []rtReq
	// before вызывается перед отправкой запроса (действие пользователя, пока запрос
	// идёт); ответ не подменяется.
	before func()
}

func (c *rtCapture) all() []rtReq {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]rtReq(nil), c.reqs...)
}

// rtObserver — RoundTripper, который записывает заголовки и передаёт запрос дальше
// настоящему транспорту клиента.
type rtObserver struct {
	base http.RoundTripper
	c    *rtCapture
}

func (o rtObserver) RoundTrip(r *http.Request) (*http.Response, error) {
	o.c.mu.Lock()
	o.c.reqs = append(o.c.reqs, rtReq{
		UA: r.Header.Get("User-Agent"), HWID: r.Header.Get("x-hwid"), OS: r.Header.Get("x-device-os"),
		Model: r.Header.Get("x-device-model"), Ver: r.Header.Get("x-ver-os"),
	})
	before := o.c.before
	o.c.mu.Unlock()
	if before != nil {
		before()
	}
	return o.base.RoundTrip(r)
}

// rtObservedClient — настоящий клиент с защитой от SSRF и наблюдателем заголовков.
func rtObservedClient(c *rtCapture) func(time.Duration) *http.Client {
	return func(d time.Duration) *http.Client {
		cl := utils.SafeHTTPClient(d)
		cl.Transport = rtObserver{base: cl.Transport, c: c}
		return cl
	}
}

// rtDevice — заголовки устройства для проверок (значения не секретны).
func rtDevice() (string, string, string) { return "XCP-Router", "Linux", "5.1.6" }

// rtSubscriptionEnv — окружение с наблюдателем запросов и постоянным HWID.
func rtSubscriptionEnv(t *testing.T, xray, mihomo bool, installedBinaries bool) (*rtEnv, *rtCapture) {
	t.Helper()
	cap := &rtCapture{}
	binX, binM := false, false
	if installedBinaries {
		binX, binM = xray, mihomo
	}
	e := rtNewEnvWith(t, binX, binM, func(d *Deps) {
		d.Client = rtObservedClient(cap)
		// Провайдер выдаёт один слот устройства: с настоящей подпиской запросы идут с HWID
		// самой панели (XCP_RT_HWID), иначе провайдер отвечает заглушками (all_stubs).
		hwid := "A1B2C3D4E5F6"
		if real := strings.TrimSpace(os.Getenv("XCP_RT_HWID")); real != "" && os.Getenv("XCP_RT_SUBSCRIPTION_URL") != "" {
			hwid = real
		}
		d.HWID = func() string { return hwid }
		d.Device = rtDevice
		d.KernelVersion = func(k string) string {
			if k == kernelMihomo {
				return "v1.19.31"
			}
			return ""
		}
	})
	e.installed = configlayer.InstalledKernels{Xray: xray, Mihomo: mihomo}
	return e, cap
}

// rtDraftSubscription читает секцию подписки черновика по номеру (единственная).
func rtDraftSubscription(t *testing.T, e *rtEnv) (string, SubscriptionConfig, LiveSources) {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for name := range st.Draft {
		if id, ok := sourceID(name); ok && id != MySourceID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) != 1 {
		t.Fatalf("секций подписок в черновике: %v", ids)
	}
	cfg, err := parseSubscription(st.Draft[sectionName(ids[0])])
	if err != nil {
		t.Fatal(err)
	}
	live, err := readLive(st.Live)
	if err != nil {
		t.Fatal(err)
	}
	return ids[0], cfg, live
}

// Сквозная проверка с настоящим провайдером: URL → два запроса (UA Xray и UA Mihomo,
// один HWID) → снимки по ядрам → «Добавить» без повторных запросов → «Применить» →
// свои файлы у каждого ядра, ожидающие снимки стали применёнными.
func TestRouterSubscriptionTracer(t *testing.T) {
	subURL := os.Getenv("XCP_RT_SUBSCRIPTION_URL")
	if subURL == "" {
		t.Skip("нет XCP_RT_SUBSCRIPTION_URL: настоящая подписка не задана")
	}
	rtNeedKernels(t, true, true)
	e, cap := rtSubscriptionEnv(t, true, true, true)
	ctx := context.Background()

	view, err := e.svc.Preview(ctx, inputOf(subURL))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(view.Subscriptions) != 1 || view.Token == "" {
		t.Fatalf("предпросмотр: подписок %d, токен %q", len(view.Subscriptions), view.Token)
	}
	sub := view.Subscriptions[0]
	for _, k := range previewKernels {
		pk := sub.Kernels[k]
		if !pk.Installed || pk.Error != nil || pk.Count == 0 {
			t.Fatalf("ядро %s: установлено %v, рабочих %d, ошибка %v", k, pk.Installed, pk.Count, pk.Error)
		}
	}

	reqs := cap.all()
	if len(reqs) != 2 {
		t.Fatalf("запросов к провайдеру на предпросмотр: %d, нужно 2 (по одному на ядро)", len(reqs))
	}
	if reqs[0].HWID == "" || reqs[0].HWID != reqs[1].HWID || reqs[0].OS != reqs[1].OS || reqs[0].Model != reqs[1].Model || reqs[0].Ver != reqs[1].Ver {
		t.Errorf("запросы несут разный HWID или заголовки устройства: %+v", reqs)
	}
	uas := []string{reqs[0].UA, reqs[1].UA}
	sort.Strings(uas)
	if uas[0] != "ClashMeta/1.19.31; mihomo/1.19.31" || uas[1] != uaXray {
		t.Errorf("User-Agent запросов: %q", uas)
	}

	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := len(cap.all()); got != 2 {
		t.Errorf("«Добавить» повторил запрос к провайдеру: запросов %d", got)
	}
	id, cfg, live := rtDraftSubscription(t, e)
	if cfg.URL != subURL || !cfg.Enabled || cfg.Interval.Mode != IntervalProvider {
		t.Errorf("секция подписки: enabled %v, interval %+v", cfg.Enabled, cfg.Interval)
	}
	src := live[id]
	if src == nil {
		t.Fatal("живых данных источника нет")
	}
	for _, k := range previewKernels {
		kl := src.Kernels[k]
		if kl == nil || kl.Pending == nil || kl.Applied != nil || kl.Status.Code != StatusOK {
			t.Fatalf("ядро %s после «Добавить»: %+v", k, kl)
		}
	}

	done := e.applyAndWait(t)
	if done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение: %+v", done.Result)
	}
	xrayFile := filepath.Join(e.roots.Xray, "04_outbounds.xcp-"+id+".tail.json")
	mihomoFile := filepath.Join(e.roots.Mihomo, "proxy_providers", "xcp-"+id+".yaml")
	for _, p := range []string{xrayFile, mihomoFile} {
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Errorf("файл подписки не записан: %s: %v", filepath.Base(p), err)
		}
	}
	_, _, live = rtDraftSubscription(t, e)
	for _, k := range previewKernels {
		kl := live[id].Kernels[k]
		if kl.Applied == nil || kl.Pending != nil || kl.Status.Code != StatusOK || kl.Delta.Added == 0 {
			t.Errorf("ядро %s после «Применить»: applied %v, pending %v, статус %s, +%d", k, kl.Applied != nil, kl.Pending != nil, kl.Status.Code, kl.Delta.Added)
		}
	}
	if got := len(cap.all()); got != 2 {
		t.Errorf("за весь сценарий провайдер получил %d запросов, нужно 2", got)
	}
}

// Подписку можно добавить, даже если запрос не удался (D-18): адрес в локальной сети
// отклоняет защита от SSRF настоящего клиента панели. Оба ядра идут с одним HWID и
// заголовками устройства, а секрет из пути адреса нигде не выводится.
func TestRouterSubscriptionAddDespiteError(t *testing.T) {
	e, cap := rtSubscriptionEnv(t, true, true, false)
	const secret = "tokensecret123"
	view, err := e.svc.Preview(context.Background(), inputOf("http://127.0.0.1:1/"+secret))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(view.Subscriptions) != 1 || view.Addable != 1 || view.Token == "" {
		t.Fatalf("предпросмотр: %+v", view)
	}
	sub := view.Subscriptions[0]
	for _, k := range previewKernels {
		pk := sub.Kernels[k]
		if !pk.Installed || pk.Count != 0 || pk.Error == nil || pk.Error.Code != "ssrf_blocked" {
			t.Errorf("ядро %s: %+v", k, pk)
		}
	}
	if sub.Error == nil || sub.Error.Code != "ssrf_blocked" || len(sub.Warnings) != 0 {
		t.Errorf("общая ошибка подписки: %+v, предупреждений %d", sub.Error, len(sub.Warnings))
	}

	reqs := cap.all()
	if len(reqs) != 2 || reqs[0].HWID != e.svc.d.HWID() || reqs[0].HWID != reqs[1].HWID ||
		reqs[0].OS != "Linux" || reqs[0].OS != reqs[1].OS || reqs[0].Model != reqs[1].Model || reqs[0].Ver != "5.1.6" || reqs[0].Ver != reqs[1].Ver {
		t.Errorf("заголовки запросов под ядра: %+v", reqs)
	}
	uas := []string{reqs[0].UA, reqs[1].UA}
	sort.Strings(uas)
	if uas[0] != "ClashMeta/1.19.31; mihomo/1.19.31" || uas[1] != uaXray {
		t.Errorf("User-Agent запросов: %q", uas)
	}

	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Error("секрет из пути адреса попал в предпросмотр")
	}

	res, err := e.svc.Add(e.rev(t), view.Token)
	if err != nil {
		t.Fatalf("Add при ошибке запроса: %v", err)
	}
	if res.Subscriptions != 1 || res.Nodes != 0 {
		t.Errorf("итог «Добавить»: %+v", res)
	}
	id, cfg, live := rtDraftSubscription(t, e)
	if !cfg.Enabled || cfg.URL == "" || cfg.Interval.Mode != IntervalProvider {
		t.Errorf("секция подписки: %+v", cfg)
	}
	for _, k := range previewKernels {
		kl := live[id].Kernels[k]
		if kl == nil || kl.Pending != nil || kl.Applied != nil || kl.Status.Code != StatusNetwork || kl.Status.ErrorCode != "ssrf_blocked" {
			t.Errorf("ядро %s после «Добавить»: %+v", k, kl)
		}
	}
	state, _ := json.Marshal(live)
	if strings.Contains(string(state), secret) {
		t.Error("секрет из пути адреса попал в живые данные")
	}
	if got := len(cap.all()); got != 2 {
		t.Errorf("«Добавить» повторил запрос: запросов %d", got)
	}
}

// Неустановленное ядро не запрашивается; ключи JSON подписки — контракт клиента.
func TestRouterSubscriptionNotInstalledKernelAndJSONKeys(t *testing.T) {
	e, cap := rtSubscriptionEnv(t, true, false, false)
	view, err := e.svc.Preview(context.Background(), inputOf("http://127.0.0.1:1/x"))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if got := cap.all(); len(got) != 1 || got[0].UA != uaXray {
		t.Fatalf("запросы при одном установленном ядре: %+v", got)
	}
	sub := view.Subscriptions[0]
	if sub.Kernels["mihomo"].Installed || sub.Kernels["mihomo"].Error != nil {
		t.Errorf("неустановленное ядро: %+v", sub.Kernels["mihomo"])
	}

	raw, err := json.Marshal(sub)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	keys := func(m map[string]json.RawMessage) string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	if got := keys(top); got != "error,kernels,line,name,url_masked,warnings" {
		t.Errorf("ключи подписки: %s", got)
	}
	if string(top["warnings"]) != "[]" {
		t.Errorf("warnings: %s", top["warnings"])
	}
	var kernels map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["kernels"], &kernels); err != nil {
		t.Fatal(err)
	}
	if len(kernels) != 2 {
		t.Errorf("ядер в kernels: %d", len(kernels))
	}
	if got := keys(kernels["xray"]); got != "count,error,installed,skipped,stubs" {
		t.Errorf("ключи ядра с ошибкой: %s", got)
	}
	if got := keys(kernels["mihomo"]); got != "count,installed,skipped,stubs" {
		t.Errorf("ключи неустановленного ядра: %s", got)
	}
}

// DNS-отказ: домен зарезервированной зоны .invalid не разрешается ни системным, ни
// резервным резолвером.
func TestRouterSubscriptionDNSFailed(t *testing.T) {
	e, _ := rtSubscriptionEnv(t, true, false, false)
	view, err := e.svc.Preview(context.Background(), inputOf("https://xcp-provider.invalid/sub"))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	pk := view.Subscriptions[0].Kernels["xray"]
	if pk.Error == nil || pk.Error.Code != "dns_failed" || pk.Error.Params["host"] != "xcp-provider.invalid" {
		t.Errorf("отказ DNS: %+v", pk.Error)
	}
	if view.Subscriptions[0].Error == nil || view.Addable != 1 {
		t.Errorf("подписка при отказе DNS: ошибка %v, к добавлению %d", view.Subscriptions[0].Error, view.Addable)
	}
}

// ErrPrivateNetwork: настоящий клиент панели отказывает адресу частной сети, и
// вызывающий отличает отказ защиты от сетевого сбоя через errors.Is.
func TestRouterErrPrivateNetwork(t *testing.T) {
	for _, target := range []string{"http://10.0.0.1/", "http://127.0.0.1:1/", "http://[::1]:1/"} {
		client := utils.SafeHTTPClient(5 * time.Second)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			t.Fatalf("запрос к адресу частной сети прошёл: %s", target)
		}
		if !errors.Is(err, utils.ErrPrivateNetwork) {
			t.Errorf("%s: errors.Is(ErrPrivateNetwork) = false", target)
		}
	}
}

// New() не создаёт каталогов и не обращается к диску и сети; при выключенном флаге
// слоя сервис следов не оставляет, а первая запись снимка создаёт каталог 0700.
func TestRouterServiceNewCreatesNoDirs(t *testing.T) {
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "src-nodirs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	roots := configlayer.Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	layer, err := configlayer.New(configlayer.Options{
		DataDir:  dir,
		Roots:    roots,
		Enabled:  func() bool { return false },
		Binaries: func() configlayer.Binaries { return configlayer.Binaries{} },
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)

	var mu sync.Mutex
	hwidCalls, clientCalls := 0, 0
	svc, err := New(Deps{
		DataDir:   dir,
		Layer:     layer,
		Installed: func() configlayer.InstalledKernels { return configlayer.InstalledKernels{Xray: true, Mihomo: true} },
		HWID:      func() string { mu.Lock(); hwidCalls++; mu.Unlock(); return "A1B2C3D4E5F6" },
		Client: func(d time.Duration) *http.Client {
			mu.Lock()
			clientCalls++
			mu.Unlock()
			return utils.SafeHTTPClient(d)
		},
	})
	if err != nil {
		t.Fatalf("sources.New: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config_layer")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("New создал каталог config_layer: %v", err)
	}
	// Выключенный слой: предпросмотр отказывает, сеть и диск не тронуты.
	if _, err := svc.Preview(context.Background(), inputOf("https://xcp-provider.invalid/sub")); !errors.Is(err, configlayer.ErrDisabled) {
		t.Errorf("предпросмотр при выключенном слое: %v", err)
	}
	mu.Lock()
	h, c := hwidCalls, clientCalls
	mu.Unlock()
	if h != 0 || c != 0 {
		t.Errorf("при выключенном слое: вызовов HWID %d, клиента %d", h, c)
	}
	if _, err := os.Stat(filepath.Join(dir, "config_layer")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("каталог config_layer создан при выключенном слое: %v", err)
	}

	// Первая запись снимка создаёт каталог с правами 0700 и файл 0600.
	hash, err := svc.blobs.put(nil)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	st, err := os.Stat(filepath.Join(dir, "config_layer", "snapshots"))
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("каталог снимков: %v, %v", st, err)
	}
	fst, err := os.Stat(filepath.Join(dir, "config_layer", "snapshots", hash+".json"))
	if err != nil || fst.Mode().Perm() != 0o600 {
		t.Errorf("файл снимка: %v, %v", fst, err)
	}
}

// Общий HWID старого и нового слоя: экспортированная обёртка стабильна, а сервис без
// своего HWID берёт именно её.
func TestRouterLoadOrGenerateHWIDShared(t *testing.T) {
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "src-hwid-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	e := rtNewEnv(t, false, false)
	want := services.LoadOrGenerateHWID(dir)
	if want == "" || services.LoadOrGenerateHWID(dir) != want {
		t.Fatalf("HWID нестабилен или пуст: %q", want)
	}
	svc, err := New(Deps{DataDir: dir, Layer: e.layer, Installed: func() configlayer.InstalledKernels { return configlayer.InstalledKernels{} }})
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.d.HWID(); got != want {
		t.Errorf("HWID сервиса не совпадает с общим: %q и %q", got, want)
	}
}
