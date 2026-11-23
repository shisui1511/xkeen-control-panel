//go:build router

// router-area: routing

package routing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
)

// Блок api Xray — отдельный файл панели 99_xcp-api.json (143 Q5): api.listen на
// loopback и сервисы, которые нужны панели, без входящего, правила api, policy и
// routing. Файл пишется вместе с маршрутизацией панели, только при отсутствии
// чужого api, и работает при правилах routing в другом файле. Проверяется
// настоящими `xray run -test` и изолированным процессом Xray с файлами
// генератора; рабочее ядро стенда не трогается (Applier не задан), ядро тест не
// читает и не переключает — метка ядра не нужна.

// rtxaOldAPIFile — 00_api.json старого слоя: api без listen, входящий
// dokodemo-door tag api и правило api в routing.
const rtxaOldAPIFile = `{
  "stats": {},
  "api": {"tag": "api", "services": ["StatsService", "RoutingService", "LoggerService"]},
  "inbounds": [{"tag": "api", "listen": "127.0.0.1", "port": 10085, "protocol": "dokodemo-door", "settings": {"address": "127.0.0.1"}}],
  "routing": {"rules": [{"type": "field", "inboundTag": ["api"], "outboundTag": "api"}]}
}
`

// rtxaForeignRules — чужой файл с правилами routing (случай P4: правила лежат
// не там, где api).
const rtxaForeignRules = `{
  "routing": {"rules": [{"type": "field", "domain": ["domain:extra.example.test"], "outboundTag": "` + TagDirect + `"}]}
}
`

// rtxaServices читает services блока api из файла панели.
func rtxaServices(t *testing.T, e *rtEnv) (api map[string]any, services []string) {
	t.Helper()
	doc := e.xrayDoc(t, configlayer.PanelXrayAPIFile)
	api, _ = doc["api"].(map[string]any)
	if api == nil {
		t.Fatalf("в %s нет объекта api: %v", configlayer.PanelXrayAPIFile, doc)
	}
	return api, rtxStrings(api["services"])
}

// rtxaRun выполняет команду xray и возвращает вывод; ненулевой код — провал.
func rtxaRun(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rtXrayBin, args...)
	cmd.Env = utils.XrayAssetEnv(os.Environ(), "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray %v: %v: %s", args[:2], err, rtRedact(string(out)))
	}
	return string(out)
}

// rtxaCopyDir копирует *.json файлов панели в новый каталог теста.
func rtxaCopyDir(t *testing.T, from string) string {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-api-")
	rtCopyFiles(t, from, dir)
	return dir
}

// rtxaConnectProxy — настоящий HTTP CONNECT-прокси к локальной цели со счётчиком.
func rtxaConnectProxy(t *testing.T, target string) (string, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	var mu sync.Mutex
	active := map[net.Conn]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != target {
			http.Error(w, "target", http.StatusForbidden)
			return
		}
		dst, err := net.DialTimeout("tcp", target, time.Second)
		if err != nil {
			http.Error(w, "connect", http.StatusBadGateway)
			return
		}
		defer dst.Close()
		client, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		mu.Lock()
		active[client] = true
		mu.Unlock()
		defer func() { mu.Lock(); delete(active, client); mu.Unlock() }()
		_ = client.SetDeadline(time.Now().Add(5 * time.Second))
		_ = dst.SetDeadline(time.Now().Add(5 * time.Second))
		hits.Add(1)
		if _, err := rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { _, _ = io.Copy(dst, rw); _ = dst.Close(); close(done) }()
		_, _ = io.Copy(client, dst)
		_ = client.Close()
		<-done
	}))
	t.Cleanup(func() {
		srv.Close()
		mu.Lock()
		defer mu.Unlock()
		for conn := range active {
			_ = conn.Close()
		}
	})
	return srv.URL, &hits
}

func TestRouterXrayAPIBlock(t *testing.T) {
	rtNeedBins(t, rtXrayBin)
	apiPort := rtFreePort(t)
	newEnv := func(t *testing.T) *rtEnv {
		t.Helper()
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) {
			d.XrayAPIPort = func() int { return apiPort }
		}))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		return e
	}

	t.Run("hot_swap_manual_group", func(t *testing.T) {
		var targetHits atomic.Int64
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			targetHits.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(target.Close)
		u, err := url.Parse(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		proxyA, hitsA := rtxaConnectProxy(t, u.Host)
		proxyB, hitsB := rtxaConnectProxy(t, u.Host)
		version := rtxaRun(t, "version")
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) {
			d.XrayAPIPort = func() int { return apiPort }
			d.XrayBin = func() string { return rtXrayBin }
			d.KernelVersion = func(string) string { return version }
		}))
		e.addMyRaw(t, proxyA+"#hot-a", proxyB+"#hot-b")
		cat := e.catalog(t).Xray
		a, b := rtsNode(t, cat, "hot-a"), rtsNode(t, cat, "hot-b")
		_, inner, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Горячая", Mode: ModeManual, Sources: GroupSources{All: true}})
		if err != nil {
			t.Fatal(err)
		}
		_, outer, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Внешняя", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: inner.ID}}})
		if err != nil {
			t.Fatal(err)
		}
		e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = []Group{inner, outer} })
		e.takeAndApply(t)
		ctx := context.Background()
		if _, err := e.svc.Select(ctx, inner.ID, memberNode+a.Ref); err != nil {
			t.Fatal(err)
		}
		dir := rtxaCopyDir(t, e.xrayDir())
		inPort := rtFreePort(t)
		extra, err := json.Marshal(map[string]any{
			"inbounds": []any{map[string]any{"tag": "rt-hot-in", "listen": "127.0.0.1", "port": inPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
			"routing":  map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"rt-hot-in"}, "outboundTag": GroupTag(outer.ID)}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "99_zz_rt_hot.json"), extra, 0600); err != nil {
			t.Fatal(err)
		}
		proc := rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""), apiPort)
		request := func() {
			t.Helper()
			status, err := rtSocksGet(inPort, target.URL, 3*time.Second)
			if err != nil || status != http.StatusNoContent {
				t.Fatalf("локальный запрос: статус=%d ошибка=%t", status, err != nil)
			}
		}
		request()
		if hitsA.Load() != 1 || hitsB.Load() != 0 || targetHits.Load() != 1 {
			t.Fatalf("первый путь: A=%d B=%d цель=%d", hitsA.Load(), hitsB.Load(), targetHits.Load())
		}
		old, err := os.ReadFile(filepath.Join(dir, xrayGroupsFile))
		if err != nil {
			t.Fatal(err)
		}
		res, err := e.svc.Select(ctx, inner.ID, memberNode+b.Ref)
		if err != nil || res.Kernels[configlayer.KernelXray] != selPendingStopped {
			t.Fatalf("подготовка выбора: ошибка=%t исход=%s", err != nil, res.Kernels[configlayer.KernelXray])
		}
		next, err := os.ReadFile(filepath.Join(e.xrayDir(), xrayGroupsFile))
		if err != nil {
			t.Fatal(err)
		}
		files := []configlayer.HotFile{{RelPath: xrayGroupsFile, Action: configlayer.ActionWrite, Old: old, OldExisted: true, New: next}}
		changed, _, _, err := hotTags(old, next)
		if err != nil || len(changed) != 2 {
			t.Fatalf("изменённые теги: число=%d ошибка=%t", len(changed), err != nil)
		}
		started := time.Now()
		if err := e.svc.xrayHot(ctx, files); err != nil {
			t.Fatalf("горячая замена: %v", err)
		}
		t.Logf("hot-swap: tags=%d dur=%s", len(changed), time.Since(started))
		server := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
		if configlayer.FeatureAvailable(configlayer.KernelXray, version, configlayer.FeatureXrayListOutbounds) == configlayer.Available {
			out := rtxaRun(t, "api", "lso", "-s", server)
			if !strings.Contains(out, GroupTag(inner.ID)) || !strings.Contains(out, GroupTag(outer.ID)) {
				t.Fatal("lso: нет обоих тегов")
			}
		}
		request()
		if hitsA.Load() != 1 || hitsB.Load() != 1 || targetHits.Load() != 2 {
			t.Fatalf("новый путь: A=%d B=%d цель=%d", hitsA.Load(), hitsB.Load(), targetHits.Load())
		}
		if !proc.alive() {
			t.Fatal("изолированный процесс не работает после замены")
		}

		// Отказы до первого rmo не меняют работающий путь.
		bad := append([]configlayer.HotFile(nil), files...)
		bad[0].New = []byte(`{"outbounds":[]}`)
		var hotErr *configlayer.XrayHotError
		if err := e.svc.xrayHot(ctx, bad); !errors.As(err, &hotErr) || hotErr.Reason != configlayer.HotReasonNotApplicable || hotErr.Partial {
			t.Fatal("смена набора тегов: неверный код или partial")
		}
		port := rtFreePort(t)
		e.svc.d.XrayAPIPort = func() int { return port }
		if err := e.svc.xrayHot(ctx, files); !errors.As(err, &hotErr) || hotErr.Reason != configlayer.HotReasonNoAPI || hotErr.Partial {
			t.Fatal("API недоступен: неверный код или partial")
		}
		e.svc.d.XrayAPIPort = func() int { return apiPort }
		request()
		if hitsA.Load() != 1 || hitsB.Load() != 2 || targetHits.Load() != 3 {
			t.Fatal("отказ изменил рабочий путь")
		}

		rtxaRun(t, "api", "rmo", "-s", server, GroupTag(outer.ID))
		beforeA, beforeB, beforeTarget := hitsA.Load(), hitsB.Load(), targetHits.Load()
		if _, err := rtSocksGet(inPort, target.URL, 2*time.Second); err == nil {
			t.Fatal("запрос после удаления тега прошёл")
		}
		if hitsA.Load() != beforeA || hitsB.Load() != beforeB || targetHits.Load() != beforeTarget {
			t.Fatalf("fail-closed: A=%d B=%d цель=%d", hitsA.Load()-beforeA, hitsB.Load()-beforeB, targetHits.Load()-beforeTarget)
		}
	})

	// Spike D-18 (X1–X5, X7): тела ручной группы freedom/blackhole горячей
	// заменой, счётчики StatsService по тегам при замене, рестарте и reset.
	// Изолированный процесс Xray на копии файлов генератора, Applier не задан,
	// ядро стенда тест не читает и не переключает — метка ядра не нужна (как у
	// теста целиком). Факты — строки FACT, тест падает только при поломке
	// подготовки.
	t.Run("group_bodies_and_counters", func(t *testing.T) { rtxsGroupBodies(t, apiPort) })

	// Spike D-18 (X6): балансировщик с подменой цели через `xray api bo` на
	// изолированном процессе с ручным файлом конфигурации (без генератора панели).
	// Ядро стенда не затрагивается — метка ядра не нужна.
	t.Run("balancer_override_facts", func(t *testing.T) { rtxsBalancerOverride(t, apiPort) })

	// Сокращённая проверка Xray запуска выбора (150-17, F4, T-150-48): вердикт
	// настоящего `xray run -test` по изменённым исходящим совпадает с полной
	// ValidateXray каталога окружения для принимаемого и отвергаемого содержимого
	// xcp-groups; отказ сокращённой — путь к полной. В журнал — только вердикты и коды.
	t.Run("validate_changed_agrees_with_full", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) {
			d.XrayAPIPort = func() int { return apiPort }
			d.XrayBin = func() string { return rtXrayBin }
		}))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		_, g, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Проверка", Mode: ModeManual, Sources: GroupSources{All: true}})
		if err != nil {
			t.Fatal(err)
		}
		e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = []Group{g} })
		e.takeAndApply(t)
		groupsPath := filepath.Join(e.xrayDir(), xrayGroupsFile)
		old, err := os.ReadFile(groupsPath)
		if err != nil {
			t.Fatal(err)
		}
		// (1) Тело исходящего группы заменено телом другого узла окружения — так,
		// как его пишет выбор.
		ctx := context.Background()
		var next []byte
		for _, n := range e.catalog(t).Xray {
			if _, err := e.svc.Select(ctx, g.ID, memberNode+n.Ref); err != nil {
				continue
			}
			if data, err := os.ReadFile(groupsPath); err == nil && string(data) != string(old) {
				next = data
				break
			}
		}
		if next == nil {
			t.Fatal("выбор другого узла не изменил xcp-groups")
		}
		// (2) Тот же исходящий с прежним тегом и протоколом, который Xray отвергает.
		var doc map[string]any
		if err := json.Unmarshal(next, &doc); err != nil {
			t.Fatal(err)
		}
		obs, _ := doc["outbounds"].([]any)
		replaced := false
		for i, it := range obs {
			if ob, _ := it.(map[string]any); ob != nil && ob["tag"] == GroupTag(g.ID) {
				obs[i] = map[string]any{"tag": GroupTag(g.ID), "protocol": "xcp-unknown-protocol"}
				replaced = true
			}
		}
		if !replaced {
			t.Fatal("в xcp-groups нет исходящего группы")
		}
		doc["outbounds"] = obs
		bad, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}

		roots := configlayer.Roots{Xray: e.xrayDir(), Mihomo: filepath.Join(e.dir, "mihomo")}
		tmpBase := rtWorkSubdir(t, "rt-val-")
		env := func(string) []string { return utils.XrayAssetEnv(os.Environ(), "") }
		cases := []struct {
			name          string
			before, after []byte
			want          bool
		}{
			{"accept", old, next, true},
			{"reject", next, bad, false},
		}
		for _, c := range cases {
			if _, _, _, err := hotTags(c.before, c.after); err != nil {
				t.Fatalf("%s: hotTags: %v", c.name, err)
			}
			files := []configlayer.HotFile{{RelPath: xrayGroupsFile, Action: configlayer.ActionWrite, Old: c.before, OldExisted: true, New: c.after}}
			short := e.svc.xrayValidateChanged(ctx, rtXrayBin, tmpBase, files)
			plan := configlayer.Plan{Files: []configlayer.FilePlan{{
				Key: configlayer.ManifestKey(configlayer.KernelXray, xrayGroupsFile), Kernel: configlayer.KernelXray,
				RelPath: xrayGroupsFile, Action: configlayer.ActionWrite, Content: c.after,
			}}}
			full := configlayer.ValidateXray(ctx, tmpBase, roots, rtXrayBin, plan, env)
			t.Logf("validate-changed: case=%s short=%t full=%t code=%s", c.name, short, full.OK, full.Code)
			if short != c.want || full.OK != c.want {
				t.Errorf("%s: сокращённая проверка %t, полная %t, ожидалось %t", c.name, short, full.OK, c.want)
			}
			if short && !full.OK {
				t.Errorf("%s: сокращённая проверка приняла то, что отвергла полная", c.name)
			}
		}
	})

	t.Run("panel_api_works_with_rules_elsewhere", func(t *testing.T) {
		e := newEnv(t)
		e.takeAndApply(t)

		api, svcs := rtxaServices(t, e)
		if api["tag"] != "api" || api["listen"] != "127.0.0.1:"+strconv.Itoa(apiPort) {
			t.Errorf("api: tag=%v listen=%v, ожидались api и 127.0.0.1:%d", api["tag"], api["listen"], apiPort)
		}
		want := []string{"HandlerService", "LoggerService", "ObservatoryService", "RoutingService", "StatsService"}
		if !rtxEqualStrings(svcs, want) {
			t.Errorf("services %v, ожидались %v (группа «авто» даёт наблюдение)", svcs, want)
		}
		doc := e.xrayDoc(t, configlayer.PanelXrayAPIFile)
		if _, has := doc["stats"]; !has {
			t.Errorf("в файле api нет stats: %v", doc)
		}
		for _, k := range []string{"inbounds", "routing", "policy"} {
			if _, has := doc[k]; has {
				t.Errorf("в файле api есть ключ %s: замена объекта целиком стёрла бы чужие значения", k)
			}
		}
		for _, r := range rtxRules(t, rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))) {
			if rtxEqualStrings(rtxStrings(r["inboundTag"]), []string{"api"}) {
				t.Errorf("в routing панели есть правило api: %v", r)
			}
		}

		// Правила routing — в другом файле, api остаётся рабочим.
		dir := rtxaCopyDir(t, e.xrayDir())
		if err := os.WriteFile(filepath.Join(dir, "05_extra.json"), []byte(rtxaForeignRules), 0o600); err != nil {
			t.Fatal(err)
		}
		rtxaRun(t, "run", "-test", "-confdir", dir)

		// Изолированный процесс: слушает только API на loopback.
		rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""), apiPort)
		server := "127.0.0.1:" + strconv.Itoa(apiPort)
		rtxaRun(t, "api", "lso", "-s", server)
		rtxaRun(t, "api", "statssys", "-s", server)

		groups, err := parseGroups(e.state(t).Applied[GroupsSection])
		if err != nil {
			t.Fatalf("применённые группы: %v", err)
		}
		var balancerTag string
		for _, group := range groups.Items {
			if group.Mode == ModeAuto {
				balancerTag = GroupTag(group.ID)
				break
			}
		}
		if balancerTag == "" {
			t.Fatal("среди применённых групп нет группы авто")
		}
		balancerOutput := rtxaRun(t, "api", "bi", "-s", server, balancerTag)
		grpcService := services.NewXrayGRPCService(server)
		grpcService.Start()
		t.Cleanup(grpcService.Stop)
		target, known, err := grpcService.BalancerTarget(context.Background(), balancerTag)
		if err != nil {
			t.Fatalf("BalancerTarget: %v", err)
		}
		t.Logf("balancer-info: known=%t", known)
		if known && !strings.Contains(balancerOutput, target) {
			t.Errorf("цель gRPC отсутствует в ответе api bi: known=%t, длина цели=%d", known, len(target))
		}

		info := services.FindXrayAPIFragment(dir)
		if !info.APIPresent || filepath.Base(info.Path) != configlayer.PanelXrayAPIFile {
			t.Errorf("FindXrayAPIFragment: present=%v файл=%s, ожидался %s", info.APIPresent, filepath.Base(info.Path), configlayer.PanelXrayAPIFile)
		}
	})

	t.Run("foreign_api_keeps_old_rule", func(t *testing.T) {
		e := newEnv(t)
		if err := os.WriteFile(filepath.Join(e.xrayDir(), "00_api.json"), []byte(rtxaOldAPIFile), 0o600); err != nil {
			t.Fatal(err)
		}
		e.takeAndApply(t)

		if e.xrayFileExists(configlayer.PanelXrayAPIFile) {
			t.Errorf("при чужом api панель записала %s: у ключа api должен быть один владелец", configlayer.PanelXrayAPIFile)
		}
		rules := rtxRules(t, rtxRouting(t, e.xrayDoc(t, xrayRoutingFile)))
		if len(rules) == 0 || !rtxEqualStrings(rtxStrings(rules[0]["inboundTag"]), []string{"api"}) || rules[0]["outboundTag"] != "api" {
			t.Errorf("первое правило routing панели %v, ожидалось inboundTag [api] → outboundTag api", rules)
		}
		rtxaRun(t, "run", "-test", "-confdir", rtxaCopyDir(t, e.xrayDir()))
		info := services.FindXrayAPIFragment(e.xrayDir())
		if !info.APIPresent || filepath.Base(info.Path) != "00_api.json" {
			t.Errorf("FindXrayAPIFragment: present=%v файл=%s, ожидался 00_api.json", info.APIPresent, filepath.Base(info.Path))
		}
	})

	t.Run("no_observatory_service_without_observation", func(t *testing.T) {
		e := newEnv(t)
		// Все группы — ручные: balancer-ов и файла наблюдения нет.
		e.replaceGroups(t, func(cfg *GroupsConfig) {
			for i := range cfg.Items {
				cfg.Items[i].Mode = ModeManual
				cfg.Items[i].Members = []Member{{Kind: KindDirect}}
			}
		})
		e.takeAndApply(t)

		if e.xrayFileExists(xrayObservatoryFile) {
			t.Errorf("файл наблюдения %s записан без групп «авто» и «резерв»", xrayObservatoryFile)
		}
		_, svcs := rtxaServices(t, e)
		want := []string{"HandlerService", "LoggerService", "RoutingService", "StatsService"}
		if !rtxEqualStrings(svcs, want) {
			t.Errorf("services %v, ожидались %v (без наблюдения ObservatoryService не нужен)", svcs, want)
		}
		rtxaRun(t, "run", "-test", "-confdir", rtxaCopyDir(t, e.xrayDir()))
	})
}

// --- Spike D-18: факты Xray (X1–X7) ---

const (
	// rtxsDataPath — путь запросов сценариев на цели; rtxsProbePath — путь проверки
	// узлов группы «авто». Цель считает только запросы сценариев: проверки
	// наблюдения не смешиваются с ними.
	rtxsDataPath  = "/data"
	rtxsProbePath = "/probe"
	// rtxsSpikeFile — дополнительный файл каталога процесса: socks-входящие
	// сценариев, правила на них и (во второй части) policy счётчиков исходящих.
	rtxsSpikeFile = "99_zz_rt_spike.json"
	// rtxsReqTimeout — срок одного запроса сценария.
	rtxsReqTimeout = 3 * time.Second
)

// rtxsVersionRe — номер версии в первой строке `xray version`.
var rtxsVersionRe = regexp.MustCompile(`(?i)xray\s+v?(\d+(?:\.\d+)+)`)

// rtxsRunSoft выполняет команду xray без провала теста: отсутствие подкоманды —
// факт, а не поломка подготовки.
func rtxsRunSoft(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, rtXrayBin, args...)
	cmd.Env = utils.XrayAssetEnv(os.Environ(), "")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// rtxsVersion — только номер версии Xray (без даты сборки и строки Go).
func rtxsVersion(out string) string {
	if m := rtxsVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return "unknown"
}

// rtxsBoHelp — справка подкоманды `xray api bo` (`xray help api bo`, затем
// `xray api bo -h`: код выхода справки у флагов не нулевой); пусто — подкоманды
// bo нет.
func rtxsBoHelp() string {
	for _, args := range [][]string{{"help", "api", "bo"}, {"api", "bo", "-h"}} {
		out, _ := rtxsRunSoft(args...)
		lower := strings.ToLower(out)
		if !strings.Contains(lower, "unknown") && strings.Contains(lower, "balancer") && strings.Contains(lower, "override") {
			return out
		}
	}
	return ""
}

// rtxsMs — длительность в миллисекундах для факта.
func rtxsMs(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }

// rtxsCounters читает счётчики исходящих через тот же клиент, которым будет
// читать служба лимитов (xrayapi.Client.OutboundTraffic): тег → uplink+downlink.
// После перезапуска процесса соединение gRPC восстанавливается не сразу — чтение
// повторяется до 10 с; не прочитали — поломка подготовки.
func rtxsCounters(t *testing.T, svc *services.XrayGRPCService) map[string]int64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		client, release, err := svc.Acquire(ctx)
		if err == nil {
			stats, qerr := client.OutboundTraffic(ctx)
			release()
			if qerr == nil {
				cancel()
				out := make(map[string]int64, len(stats))
				for tag, p := range stats {
					out[tag] = p.Uplink + p.Downlink
				}
				return out
			}
			err = qerr
		}
		cancel()
		if time.Now().After(deadline) {
			t.Fatalf("подготовка: счётчики StatsService не прочитаны: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// rtxsRoles — роли тегов, чьи счётчики выросли между снимками, через запятую
// (роли, не сами теги); тег без роли — other, ничего не выросло — none.
func rtxsRoles(before, after map[string]int64, roles map[string]string) string {
	seen := map[string]bool{}
	var out []string
	for tag, v := range after {
		if v <= before[tag] {
			continue
		}
		role := roles[tag]
		if role == "" {
			role = "other"
		}
		if !seen[role] {
			seen[role] = true
			out = append(out, role)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// rtxsCounterState сравнивает счётчик тега до и после события: kept — значение
// не меньше прежнего, reset — меньше (обычно ноль), missing — счётчика нет.
func rtxsCounterState(before int64, hadBefore bool, after map[string]int64, tag string) string {
	v, ok := after[tag]
	switch {
	case !ok:
		return "missing"
	case !hadBefore:
		return "new"
	case v < before || (v == 0 && before > 0):
		return "reset"
	}
	return "kept"
}

// rtxsStep — итог одного запроса сценария: прошёл ли, сколько CONNECT получили
// узлы A и B, сколько запросов дошло до цели, роли выросших тегов.
type rtxsStep struct {
	err            error
	hitsA, hitsB   int64
	data           int64
	roles          string
	before, after  map[string]int64
	nodeHitsLeaked bool
}

// rtxsGroupBodies — подтест group_bodies_and_counters (X1–X5, X7).
func rtxsGroupBodies(t *testing.T, apiPort int) {
	started := time.Now()
	var dataHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == rtxsDataPath {
			dataHits.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	dataURL := target.URL + rtxsDataPath
	// A и B — узлы ручных групп; C — только узел группы «авто»: проверки
	// наблюдения идут через C и не попадают в счётчики CONNECT узлов A и B.
	proxyA, hitsA := rtxaConnectProxy(t, u.Host)
	proxyB, hitsB := rtxaConnectProxy(t, u.Host)
	proxyC, _ := rtxaConnectProxy(t, u.Host)
	version := rtxaRun(t, "version")
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) {
		d.XrayAPIPort = func() int { return apiPort }
		d.XrayBin = func() string { return rtXrayBin }
		d.KernelVersion = func(string) string { return version }
	}))
	e.addMyRaw(t, proxyA+"#rtxs-a", proxyB+"#rtxs-b", proxyC+"#rtxs-c")
	cat := e.catalog(t).Xray
	a, b, c := rtsNode(t, cat, "rtxs-a"), rtsNode(t, cat, "rtxs-b"), rtsNode(t, cat, "rtxs-c")
	save := func(g Group) Group {
		t.Helper()
		_, out, err := e.svc.SaveGroup(e.rev(t), g)
		if err != nil {
			t.Fatalf("подготовка: SaveGroup: %v", err)
		}
		return out
	}
	inner := save(Group{Name: "Спайк внутренняя", Mode: ModeManual, Picks: []string{a.Ref}})
	outer := save(Group{Name: "Спайк внешняя", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: inner.ID}, {Kind: KindDirect}, {Kind: KindReject}}})
	spike := save(Group{Name: "Спайк", Mode: ModeManual, Picks: []string{a.Ref, b.Ref}, Members: []Member{{Kind: KindDirect}, {Kind: KindReject}}})
	items := []Group{inner, outer, spike}
	autoOK := e.svc.xrayModeAvailable(configlayer.FeatureXrayAutoGroup)
	var auto, viaAuto Group
	if autoOK {
		if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: target.URL + rtxsProbePath, IntervalS: 10, TimeoutS: 3}); err != nil {
			t.Fatalf("подготовка: SaveProbe: %v", err)
		}
		auto = save(Group{Name: "Спайк авто", Mode: ModeAuto, Picks: []string{c.Ref}})
		viaAuto = save(Group{Name: "Спайк через авто", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: auto.ID}}})
		items = append(items, auto, viaAuto)
	}
	rtdsFact(t, "x3_auto_available", rtdsYesNo(autoOK))
	e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = items })
	e.takeAndApply(t)
	ctx := context.Background()
	if _, err := e.svc.Select(ctx, spike.ID, memberNode+a.Ref); err != nil {
		t.Fatalf("подготовка: выбор узла A: %v", err)
	}

	// Каталог процесса — копия файлов генератора и файл сценариев.
	dir := rtxaCopyDir(t, e.xrayDir())
	spikeTag := GroupTag(spike.ID)
	inSpike, inOuter, inVia, inAuto := rtFreePort(t), rtFreePort(t), rtFreePort(t), rtFreePort(t)
	inbound := func(tag string, port int) map[string]any {
		return map[string]any{"tag": tag, "listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}
	}
	inbounds := []any{inbound("rt-xs-spike", inSpike), inbound("rt-xs-outer", inOuter)}
	rules := []any{
		map[string]any{"type": "field", "inboundTag": []string{"rt-xs-spike"}, "outboundTag": spikeTag},
		map[string]any{"type": "field", "inboundTag": []string{"rt-xs-outer"}, "outboundTag": GroupTag(outer.ID)},
	}
	if autoOK {
		inbounds = append(inbounds, inbound("rt-xs-via", inVia), inbound("rt-xs-auto", inAuto))
		rules = append(rules,
			map[string]any{"type": "field", "inboundTag": []string{"rt-xs-via"}, "outboundTag": GroupTag(viaAuto.ID)},
			map[string]any{"type": "field", "inboundTag": []string{"rt-xs-auto"}, "balancerTag": GroupTag(auto.ID)},
		)
	}
	writeSpike := func(policy bool) {
		t.Helper()
		doc := map[string]any{"inbounds": inbounds, "routing": map[string]any{"rules": rules}}
		if policy {
			doc["policy"] = map[string]any{"system": map[string]any{"statsOutboundUplink": true, "statsOutboundDownlink": true}}
		}
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rtxsSpikeFile), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := utils.XrayAssetEnv(os.Environ(), "")
	server := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	grpcSvc := services.NewXrayGRPCService(server)
	grpcSvc.Start()
	t.Cleanup(grpcSvc.Stop)

	get := func(port int) error {
		status, err := rtSocksGet(port, dataURL, rtxsReqTimeout)
		if err == nil && status != http.StatusNoContent {
			return errors.New("неожиданный статус")
		}
		return err
	}

	// X2 (предусловие): есть ли счётчики исходящих только с файлом api панели,
	// без секции policy.
	writeSpike(false)
	proc := rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, env, apiPort)
	if err := get(inSpike); err != nil {
		t.Fatalf("подготовка: запрос через группу на узле A не прошёл (без policy)")
	}
	time.Sleep(300 * time.Millisecond)
	withoutPolicy := false
	for tag := range rtxsCounters(t, grpcSvc) {
		withoutPolicy = withoutPolicy || strings.HasPrefix(tag, groupTagPrefix)
	}
	rtdsFact(t, "x2_counters_without_policy", rtdsYesNo(withoutPolicy))
	proc.stop()
	writeSpike(true)
	proc = rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, env, apiPort)

	roles := map[string]string{
		spikeTag:           "group-own",
		GroupTag(outer.ID): "group-outer",
		GroupTag(inner.ID): "group-inner",
		a.XrayTag:          "node",
		b.XrayTag:          "node",
		TagDirect:          "direct",
		TagBlock:           "block",
	}
	if autoOK {
		// Тело группы «через авто» — loopback на балансировщик; C — узел «авто»,
		// его счётчик может расти и от проверок наблюдения.
		roles[GroupTag(viaAuto.ID)] = "loopback"
		roles[c.XrayTag] = "node-auto"
	}
	measure := func(port int) rtxsStep {
		t.Helper()
		before := rtxsCounters(t, grpcSvc)
		a0, b0, d0 := hitsA.Load(), hitsB.Load(), dataHits.Load()
		err := get(port)
		time.Sleep(300 * time.Millisecond)
		after := rtxsCounters(t, grpcSvc)
		st := rtxsStep{err: err, hitsA: hitsA.Load() - a0, hitsB: hitsB.Load() - b0, data: dataHits.Load() - d0, before: before, after: after}
		st.roles = rtxsRoles(before, after, roles)
		st.nodeHitsLeaked = st.hitsA+st.hitsB > 0
		return st
	}
	groupsCopy := filepath.Join(dir, xrayGroupsFile)
	// swap — выбор участника группы «Спайк» и горячая замена её тела, как в
	// hot_swap_manual_group; копия каталога получает новое тело (для X4).
	swap := func(member string) (time.Duration, bool) {
		t.Helper()
		old, err := os.ReadFile(groupsCopy)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.Select(ctx, spike.ID, member); err != nil {
			t.Fatalf("подготовка: выбор %s: %v", member, err)
		}
		next, err := os.ReadFile(filepath.Join(e.xrayDir(), xrayGroupsFile))
		if err != nil {
			t.Fatal(err)
		}
		files := []configlayer.HotFile{{RelPath: xrayGroupsFile, Action: configlayer.ActionWrite, Old: old, OldExisted: true, New: next}}
		begin := time.Now()
		err = e.svc.xrayHot(ctx, files)
		dur := time.Since(begin)
		if err != nil {
			reason := "error"
			var hotErr *configlayer.XrayHotError
			if errors.As(err, &hotErr) {
				reason = hotErr.Reason
			}
			rtdsFact(t, "x1_swap_"+member, "failed:"+reason)
			return dur, false
		}
		if err := os.WriteFile(groupsCopy, next, 0o600); err != nil {
			t.Fatal(err)
		}
		return dur, true
	}

	// X3: ручная группа → узел (исходный выбор A).
	st := measure(inSpike)
	if st.err != nil || st.hitsA != 1 || st.data != 1 {
		t.Fatalf("подготовка: путь через узел A: прошёл=%s A=%d B=%d цель=%d", rtdsYesNo(st.err == nil), st.hitsA, st.hitsB, st.data)
	}
	rtdsFact(t, "x3_manual_node", st.roles)

	// X2: счётчик тега группы до и после rmo→ado.
	v0, had0 := st.after[spikeTag]
	durReject, ok := swap(memberReject)
	if !ok {
		t.Skip("находка X1: горячая замена тела группы на blackhole не прошла (факт x1_swap_reject)")
	}
	rtdsFact(t, "x2_group_counter_after_swap", rtxsCounterState(v0, had0, rtxsCounters(t, grpcSvc), spikeTag))

	// X1: REJECT — запрос обрывается, узлы и цель ничего не получают.
	st = measure(inSpike)
	leakReject := st.nodeHitsLeaked
	rtdsFact(t, "x1_reject_blocks", rtdsYesNo(st.err != nil && !leakReject && st.data == 0))
	rtdsFact(t, "x3_manual_reject", st.roles)

	// X1: DIRECT — запрос доходит до цели мимо узлов.
	durDirect, ok := swap(memberDirect)
	if !ok {
		t.Skip("находка X1: горячая замена тела группы на freedom не прошла (факт x1_swap_direct)")
	}
	st = measure(inSpike)
	leakDirect := st.nodeHitsLeaked
	rtdsFact(t, "x1_direct_reaches_target", rtdsYesNo(st.err == nil && st.data == 1 && !leakDirect))
	rtdsFact(t, "x3_manual_direct", st.roles)

	// Утечка в узел — факт и находка для решения 152-03, не падение сессии:
	// регрессию «без утечки» выбранного механизма несут тесты лимитов (152-18).
	leak := leakReject || leakDirect
	rtdsFact(t, "x1_leak_to_node", rtdsYesNo(leak))
	if leak {
		t.Logf("находка X1: трафик ушёл в узел при REJECT=%s DIRECT=%s", rtdsYesNo(leakReject), rtdsYesNo(leakDirect))
	}
	rtdsFact(t, "x1_swap_reject_ms", rtxsMs(durReject))
	rtdsFact(t, "x1_swap_direct_ms", rtxsMs(durDirect))
	rtdsFact(t, "x1_swap_ms", rtxsMs(max(durReject, durDirect)))

	// X3: внешняя ручная → вложенная ручная (тело внешней — копия узла A).
	st = measure(inOuter)
	rtdsFact(t, "x3_nested_reached", rtdsYesNo(st.err == nil && st.data == 1))
	rtdsFact(t, "x3_nested_manual", st.roles)

	// X3: ручная → «авто» (loopback) и правило прямо на «авто». Балансировщику
	// нужен первый результат наблюдения: повтор до 15 с, пока запрос не дойдёт.
	if autoOK {
		for _, sc := range []struct {
			key  string
			port int
		}{{"x3_manual_auto", inVia}, {"x3_auto_rule", inAuto}} {
			var last rtxsStep
			reached := false
			for end := time.Now().Add(15 * time.Second); ; time.Sleep(time.Second) {
				last = measure(sc.port)
				reached = last.err == nil && last.data == 1
				if reached || time.Now().After(end) {
					break
				}
			}
			rtdsFact(t, sc.key+"_reached", rtdsYesNo(reached))
			rtdsFact(t, sc.key, last.roles)
		}
	}

	// X4: перезапуск процесса на том же каталоге (тело группы — freedom).
	beforeRestart := rtxsCounters(t, grpcSvc)
	vR, hadR := beforeRestart[spikeTag]
	proc.stop()
	proc = rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, env, apiPort)
	afterRestart := rtxsCounters(t, grpcSvc)
	state := "missing"
	if v, ok := afterRestart[spikeTag]; ok {
		switch {
		case v == 0:
			state = "zero"
		case hadR && v >= vR:
			state = "kept"
		default:
			state = "lower"
		}
	}
	rtdsFact(t, "x4_counter_after_restart", state)

	// X5: QueryStats с reset=true против чтения без reset.
	if st := measure(inSpike); st.err != nil {
		t.Fatalf("подготовка: запрос через группу после перезапуска не прошёл")
	}
	conn, err := grpc.NewClient(server, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("подготовка: клиент gRPC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	resp, err := statspb.NewStatsServiceClient(conn).QueryStats(qctx, &statspb.QueryStatsRequest{Pattern: "outbound>>>" + spikeTag + ">>>", Reset_: true})
	cancel()
	if err != nil {
		rtdsFact(t, "x5_reset_query", "failed")
	} else {
		var sum int64
		for _, s := range resp.GetStat() {
			sum += s.GetValue()
		}
		rtdsFact(t, "x5_reset_returns_value", rtdsYesNo(sum > 0))
		afterReset := "missing"
		if v, ok := rtxsCounters(t, grpcSvc)[spikeTag]; ok {
			afterReset = "kept"
			if v == 0 {
				afterReset = "zero"
			}
		}
		rtdsFact(t, "x5_after_reset", afterReset)
	}

	// X7: версия и подкоманды.
	rtdsFact(t, "x7_xray_version", rtxsVersion(version))
	rtdsFact(t, "x7_hot_swap_api", rtdsYesNo(configlayer.FeatureAvailable(configlayer.KernelXray, version, configlayer.FeatureXrayHotSwapAPI) == configlayer.Available))
	rtdsFact(t, "x7_lso", rtdsYesNo(configlayer.FeatureAvailable(configlayer.KernelXray, version, configlayer.FeatureXrayListOutbounds) == configlayer.Available))
	rtdsFact(t, "x7_bo_cli", rtdsYesNo(rtxsBoHelp() != ""))
	if !proc.alive() {
		t.Fatalf("подготовка: изолированный процесс завершился: %s", proc.tail())
	}
	rtdsFact(t, "xs_group_bodies_ms", rtxsMs(time.Since(started)))
}

// rtxsBalancerOverride — подтест balancer_override_facts (X6): ручной файл
// конфигурации с балансировщиком b1 (стратегия по умолчанию, selector из двух
// узлов, fallbackTag — первый узел) и подмена цели через `xray api bo`.
func rtxsBalancerOverride(t *testing.T, apiPort int) {
	started := time.Now()
	help := rtxsBoHelp()
	if help == "" {
		rtdsFact(t, "x6_bo_available", "no")
		t.Skip("FACT x6_bo_available=no: подкоманды xray api bo нет")
	}
	rtdsFact(t, "x6_bo_available", "yes")
	var dataHits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == rtxsDataPath {
			dataHits.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	dataURL := target.URL + rtxsDataPath
	proxy1, hits1 := rtxaConnectProxy(t, u.Host)
	proxy2, hits2 := rtxaConnectProxy(t, u.Host)
	httpOut := func(tag, proxyURL string) map[string]any {
		t.Helper()
		pu, err := url.Parse(proxyURL)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(pu.Port())
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"tag": tag, "protocol": "http", "settings": map[string]any{"servers": []any{map[string]any{"address": pu.Hostname(), "port": port}}}}
	}
	const n1, n2, balancer = "rt-bo-n1", "rt-bo-n2", "rt-bo-b1"
	inPort := rtFreePort(t)
	server := net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))
	cfg, err := json.Marshal(map[string]any{
		"log":   map[string]any{"loglevel": "warning"},
		"api":   map[string]any{"tag": "api", "listen": server, "services": []string{"RoutingService", "StatsService"}},
		"stats": map[string]any{},
		"inbounds": []any{map[string]any{"tag": "rt-bo-in", "listen": "127.0.0.1", "port": inPort, "protocol": "socks",
			"settings": map[string]any{"auth": "noauth", "udp": false}}},
		"outbounds": []any{httpOut(n1, proxy1), httpOut(n2, proxy2), freedomOutbound(TagDirect), blackholeOutbound(TagBlock)},
		"routing": map[string]any{
			"balancers": []any{map[string]any{"tag": balancer, "selector": []string{n1, n2}, "fallbackTag": n1}},
			"rules":     []any{map[string]any{"type": "field", "inboundTag": []string{"rt-bo-in"}, "balancerTag": balancer}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := rtWorkSubdir(t, "rt-bo-")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	env := utils.XrayAssetEnv(os.Environ(), "")
	proc := rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, env, apiPort)

	// send делает n запросов и возвращает приросты: узел 1, узел 2, цель, отказы.
	send := func(n int) (h1, h2, data int64, fails int) {
		a0, b0, d0 := hits1.Load(), hits2.Load(), dataHits.Load()
		for range n {
			status, err := rtSocksGet(inPort, dataURL, rtxsReqTimeout)
			if err != nil || status != http.StatusNoContent {
				fails++
			}
		}
		return hits1.Load() - a0, hits2.Load() - b0, dataHits.Load() - d0, fails
	}
	override := func(tag string) bool {
		_, err := rtxsRunSoft("api", "bo", "-s", server, "-b", balancer, tag)
		return err == nil
	}
	// Снятие подмены: флаг -r/-remove, если он есть в справке, иначе пустая цель.
	clearOverride := func() (string, bool) {
		if strings.Contains(help, "-remove") || strings.Contains(help, "-r,") || strings.Contains(help, "-r ") {
			_, err := rtxsRunSoft("api", "bo", "-s", server, "-b", balancer, "-r")
			return "remove_flag", err == nil
		}
		_, err := rtxsRunSoft("api", "bo", "-s", server, "-b", balancer, "")
		return "empty_arg", err == nil
	}

	h1, h2, data, fails := send(4)
	if data == 0 {
		t.Fatalf("подготовка: ни один запрос через балансировщик не дошёл до цели (отказов %d)", fails)
	}
	rtdsFact(t, "x6_default_spread", "n1:"+strconv.FormatInt(h1, 10)+",n2:"+strconv.FormatInt(h2, 10))

	cmdOK := override(n2)
	h1, h2, _, _ = send(5)
	rtdsFact(t, "x6_override_switches", rtdsYesNo(cmdOK && h1 == 0 && h2 == 5))

	cmdOK = override(TagBlock)
	h1, h2, data, fails = send(3)
	rtdsFact(t, "x6_override_block", rtdsYesNo(cmdOK && fails == 3 && h1+h2 == 0 && data == 0))

	cmdOK = override(TagDirect)
	h1, h2, data, _ = send(3)
	rtdsFact(t, "x6_override_direct", rtdsYesNo(cmdOK && data == 3 && h1+h2 == 0))

	mode, cmdOK := clearOverride()
	rtdsFact(t, "x6_empty_mode", mode)
	h1, h2, _, _ = send(4)
	rtdsFact(t, "x6_empty_clears", rtdsYesNo(cmdOK && h1+h2 == 4))

	// Подмена на DIRECT переживает ли перезапуск процесса.
	cmdOK = override(TagDirect)
	h1, h2, data, _ = send(1)
	if !cmdOK || data != 1 || h1+h2 != 0 {
		rtdsFact(t, "x6_override_after_restart", "unknown")
	} else {
		proc.stop()
		proc = rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, env, apiPort)
		h1, h2, data, _ = send(4)
		rtdsFact(t, "x6_override_after_restart", rtdsYesNo(data == 4 && h1+h2 == 0))
	}
	if !proc.alive() {
		t.Fatalf("подготовка: изолированный процесс завершился: %s", proc.tail())
	}
	rtdsFact(t, "xs_balancer_override_ms", rtxsMs(time.Since(started)))
}
