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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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
				t.Fatalf("%s: набор тегов изменился: %t", c.name, err != nil)
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
