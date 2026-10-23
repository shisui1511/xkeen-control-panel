//go:build router

package routing

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Трафик через профиль панели на настоящем Mihomo: fail-closed, резервный узел и
// маршрутизация по правилам. Профиль строит генератор 146-11 и записывает слой
// «Применить» (mihomo -t); процесс Mihomo отдельный, в каталоге теста: к профилю
// добавляются только socks-порт на loopback и сокет контроллера в каталоге теста
// (правка в копии, сокет и ядро стенда не трогаются). «Узел» — процесс Xray с
// socks-входящим и журналом доступа (rtStartNode). Цель прямых запросов и адрес
// проверки узлов — HTTP-сервер теста на loopback: интернет стенда не нужен, а
// утечка мимо группы видна как прошедший запрос. В лог идут только «да/нет» и
// числа.

// rtmtTarget поднимает HTTP-сервер на loopback: /generate_204 отвечает 204, остальное
// 200. Возвращает адрес проверки узлов и адрес цели запросов.
func rtmtTarget(t *testing.T) (probeURL, targetURL string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("сервер цели: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate_204" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, "ok")
	})}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	base := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(l.Addr().(*net.TCPAddr).Port))
	return base + "/generate_204", base + "/ok"
}

// rtmtStart запускает отдельный процесс Mihomo c профилем панели окружения: как
// rtMihomoCore.start, но профиль перед записью можно изменить (mutate).
func rtmtStart(t *testing.T, e *rtEnv, c *rtMihomoCore, mutate func(doc map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(e.mihomoPath(configlayer.PanelMihomoProfile))
	if err != nil {
		t.Fatalf("профиль панели: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("профиль панели не YAML: %v", err)
	}
	for _, k := range []string{"redir-port", "tproxy-port", "routing-mark"} {
		delete(doc, k)
	}
	sock := filepath.Join(c.dir, "c.sock")
	if len(sock) > 100 {
		t.Fatalf("путь сокета контроллера длиннее предела unix-сокета: %d знаков", len(sock))
	}
	_ = os.Remove(sock)
	doc["socks-port"] = c.port
	doc["bind-address"] = "127.0.0.1"
	doc["allow-lan"] = false
	doc["external-controller-unix"] = sock
	if mutate != nil {
		mutate(doc)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.dir, "config.yaml"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	rtCopyFiles(t, e.mihomoPath(proxyProviderDir), filepath.Join(c.dir, proxyProviderDir))
	rtCopyFiles(t, e.mihomoPath(mihomoRuleProviderDir), filepath.Join(c.dir, mihomoRuleProviderDir))
	p := rtStartMihomoCore(t, c.dir, c.port)
	c.mu.Lock()
	c.proc = p
	c.mu.Unlock()
	rtWaitCond(t, 30*time.Second, "Clash API отдельного процесса", c.svc.HasController)
}

// rtmtGroupsRules заменяет группы и правила профиля (groups == nil — групп нет).
func rtmtGroupsRules(groups []any, rules []string) func(doc map[string]any) {
	return func(doc map[string]any) {
		if groups == nil {
			delete(doc, "proxy-groups")
		} else {
			doc["proxy-groups"] = groups
		}
		doc["rules"] = rules
	}
}

// rtmtProbe делает запросы через socks процесса раз в step в течение wait и
// возвращает число попыток и прошедших.
func rtmtProbe(port int, rawURL string, wait, step time.Duration) (attempts, passed int) {
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(step) {
		attempts++
		if _, err := rtSocksGet(port, rawURL, 6*time.Second); err == nil {
			passed++
		}
	}
	return attempts, passed
}

// rtmtVmRSS — резидентная память процесса в кБ из /proc/<pid>/status; 0 — не прочитана.
func rtmtVmRSS(pid int) int {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			f := strings.Fields(rest)
			if len(f) > 0 {
				n, _ := strconv.Atoi(f[0])
				return n
			}
		}
	}
	return 0
}

// rtmtMeasure — провайдеры замера A1/A2: одни и те же строки категории dat в
// format: text и format: yaml.
type rtmtMeasure struct {
	textName, yamlName string
	lines              int
}

// rtmtPrepareMeasure выбирает самую большую категорию geosite стенда (с домен-частью,
// не больше предела Mihomo) и пишет её в два файла провайдеров в каталоге процесса.
// nil — подходящей категории нет.
func rtmtPrepareMeasure(t *testing.T, dir string) *rtmtMeasure {
	t.Helper()
	ix := lists.NewIndex("")
	files, err := ix.Files()
	if err != nil {
		return nil
	}
	type cand struct {
		file, tag string
		count     int
	}
	var cands []cand
	for _, f := range files {
		if f.Kind != lists.KindSite || !listFileRE.MatchString(f.Name) {
			continue
		}
		tags, err := ix.Tags(f.Name)
		if err != nil {
			continue
		}
		for _, tg := range tags {
			if listTagRE.MatchString(tg.Tag) {
				cands = append(cands, cand{f.Name, tg.Tag, tg.Count})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].count > cands[j].count })
	for i, c := range cands {
		if i >= 5 {
			break
		}
		cat, err := ix.Extract(c.file, c.tag)
		if err != nil || len(cat.Domains) == 0 {
			continue
		}
		return rtmtWriteMeasure(t, dir, cat.Domains)
	}
	return nil
}

// rtmtWriteMeasure пишет строки провайдера в text и yaml.
func rtmtWriteMeasure(t *testing.T, dir string, lines []string) *rtmtMeasure {
	t.Helper()
	m := &rtmtMeasure{textName: "xcp-rt-text", yamlName: "xcp-rt-yaml", lines: len(lines)}
	pdir := filepath.Join(dir, mihomoRuleProviderDir)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, m.textName+".txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := yaml.Marshal(map[string]any{"payload": lines})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdir, m.yamlName+".yaml"), doc, 0o600); err != nil {
		t.Fatal(err)
	}
	return m
}

// addTo подключает оба провайдера замера к профилю: они только загружаются, правил
// на них нет.
func (m *rtmtMeasure) addTo(doc map[string]any) {
	rp, _ := doc["rule-providers"].(map[string]any)
	if rp == nil {
		rp = map[string]any{}
	}
	rp[m.textName] = map[string]any{"type": "file", "behavior": "domain", "format": "text",
		"path": "./" + mihomoRuleProviderDir + "/" + m.textName + ".txt"}
	rp[m.yamlName] = map[string]any{"type": "file", "behavior": "domain", "format": "yaml",
		"path": "./" + mihomoRuleProviderDir + "/" + m.yamlName + ".yaml"}
	doc["rule-providers"] = rp
}

// Ни при каком состоянии трафик не уходит мимо VPN: группа из мёртвых узлов
// обрывает соединение (основной шаг), группа без участников уходит в REJECT только
// с empty-fallback (контроль Pitfall 2), «резерв» работает при мёртвых основных и
// не мешает живому основному (A4). На том же процессе — замер A1/A2: время PUT
// провайдера правил и рост VmRSS в format: text против format: yaml.
func TestRouterMihomoFailClosed(t *testing.T) {
	probeURL, targetURL := rtmtTarget(t)
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core))
	e.writeUserProfile(t)
	// Мёртвые узлы: на этих портах loopback никто не слушает.
	e.addMyRaw(t, "socks5://127.0.0.1:9#dead-1", "socks5://127.0.0.1:10#dead-2")
	auto, ok := rtGroupByName(e.groups(t), "Авто")
	if !ok {
		t.Fatal("нет группы «Авто»")
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: auto.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: probeURL, IntervalS: 10, TimeoutS: 3}); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}
	e.takeAndApply(t)

	// База: цель достижима напрямую через Mihomo, иначе «запрос не прошёл» ничего
	// не доказывает.
	direct := rtNewMihomoCore(t)
	rtmtStart(t, e, direct, rtmtGroupsRules(nil, []string{"MATCH,DIRECT"}))
	if _, err := rtSocksGet(direct.port, targetURL, 8*time.Second); err != nil {
		t.Skip("цель на loopback недоступна через Mihomo напрямую: проверка утечки неприменима")
	}
	direct.stop()

	meas := rtmtPrepareMeasure(t, core.dir)
	rtmtStart(t, e, core, func(doc map[string]any) {
		if meas != nil {
			meas.addTo(doc)
		}
	})

	// Основной шаг: после нескольких циклов проверки (не меньше 25 с) ни один
	// запрос через «авто» из мёртвых узлов не проходит.
	t.Run("dead_group_blocks", func(t *testing.T) {
		attempts, passed := rtmtProbe(core.port, targetURL, 30*time.Second, 2*time.Second)
		if core.pid() == 0 {
			t.Fatalf("процесс Mihomo завершился во время проверки: %s", core.proc.tail())
		}
		t.Logf("запросов через группу из мёртвых узлов: %d, прошло: %d", attempts, passed)
		if passed > 0 {
			t.Errorf("утечка мимо VPN: %d из %d запросов прошли при мёртвых узлах", passed, attempts)
		}
	})

	// Контроль Pitfall 2: select с use и filter, под который не подходит ни один
	// узел. Без empty-fallback пустая группа превращается в COMPATIBLE (прямое
	// соединение), с empty-fallback: REJECT — соединение обрывается.
	t.Run("empty_group_empty_fallback", func(t *testing.T) {
		group := func(fallback bool) []any {
			g := map[string]any{
				"name": "Пусто", "type": "select",
				"use": []string{"xcp-my"}, "filter": "^rt-no-such-node$",
			}
			if fallback {
				g["empty-fallback"] = "REJECT"
			}
			return []any{g}
		}
		rules := []string{"MATCH,Пусто"}

		noFB := rtNewMihomoCore(t)
		rtmtStart(t, e, noFB, rtmtGroupsRules(group(false), rules))
		leak := false
		for i := 0; i < 3 && !leak; i++ {
			_, err := rtSocksGet(noFB.port, targetURL, 6*time.Second)
			leak = err == nil
		}
		t.Logf("утечка пустой группы без empty-fallback: %s", rtxYesNo(leak))
		noFB.stop()

		withFB := rtNewMihomoCore(t)
		rtmtStart(t, e, withFB, rtmtGroupsRules(group(true), rules))
		attempts, passed := rtmtProbe(withFB.port, targetURL, 9*time.Second, 3*time.Second)
		t.Logf("запросов через пустую группу с empty-fallback: %d, прошло: %d", attempts, passed)
		if passed > 0 {
			t.Errorf("пустая группа с empty-fallback: REJECT пропустила %d из %d запросов", passed, attempts)
		}
	})

	// Замер A1/A2 на том же процессе.
	t.Run("provider_format_measure", func(t *testing.T) {
		if meas == nil {
			t.Skip("в каталоге dat стенда нет категории geosite с доменами: замер формата провайдера пропущен")
		}
		pid := core.pid()
		if pid == 0 {
			t.Fatal("процесс Mihomo не работает")
		}
		t.Logf("записей в провайдерах: %d; VmRSS процесса до замера: %d кБ", meas.lines, rtmtVmRSS(pid))
		for _, p := range []struct{ label, name string }{{"text", meas.textName}, {"yaml", meas.yamlName}} {
			before := rtmtVmRSS(pid)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			start := time.Now()
			err := core.svc.UpdateProvider(ctx, "rules", p.name)
			took := time.Since(start)
			count, cerr := core.svc.ProviderCount(ctx, "rules", p.name)
			cancel()
			if err != nil {
				t.Errorf("PUT /providers/rules (%s): %v", p.label, err)
				continue
			}
			after := rtmtVmRSS(pid)
			t.Logf("format %s: PUT %d мс, ruleCount %d (ошибка чтения: %s), VmRSS до %d кБ, после %d кБ",
				p.label, took.Milliseconds(), count, rtxYesNo(cerr != nil), before, after)
		}
	})

	t.Run("reserve_main_dead", func(t *testing.T) { rtmtReserveCase(t, false) })
	t.Run("reserve_main_alive", func(t *testing.T) { rtmtReserveCase(t, true) })
}

// rtmtReserveCase проверяет «резерв» на отдельном процессе Mihomo (A4). mainAlive
// false: основные узлы мёртвые, запрос должен дойти до резервного узла-процесса.
// mainAlive true: основной — узел-процесс, запрос идёт через него, резервный узел
// запроса не видит.
func rtmtReserveCase(t *testing.T, mainAlive bool) {
	t.Helper()
	probeURL, _ := rtmtTarget(t)
	reservePort, reserveLog := rtStartNode(t)
	links := []string{}
	var mainLog string
	if mainAlive {
		var mainPort int
		mainPort, mainLog = rtStartNode(t)
		links = append(links, "socks5://127.0.0.1:"+strconv.Itoa(mainPort)+"#rt-main")
	} else {
		links = append(links, "socks5://127.0.0.1:9#rt-main-dead-1", "socks5://127.0.0.1:10#rt-main-dead-2")
	}
	links = append(links, "socks5://127.0.0.1:"+strconv.Itoa(reservePort)+"#rt-reserve")

	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core))
	e.writeUserProfile(t)
	e.addMyRaw(t, links...)
	reserveNode := rtsNode(t, e.catalog(t).Mihomo, "rt-reserve")
	_, group, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Резерв", Mode: ModeFallback, Sources: GroupSources{All: true}, Reserve: reserveNode.Ref,
	})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: group.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: probeURL, IntervalS: 10, TimeoutS: 3}); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}
	e.takeAndApply(t)
	rtmtStart(t, e, core, nil)

	if !mainAlive {
		const domain = "reserve.example.test"
		saw := false
		for end := time.Now().Add(90 * time.Second); time.Now().Before(end) && !saw; time.Sleep(2 * time.Second) {
			_, _ = rtSocksGet(core.port, "http://"+domain+"/", 5*time.Second)
			saw = rtWaitNodeSaw(reserveLog, domain, 2*time.Second)
		}
		now, nerr := core.svc.ProxyNow(context.Background(), "Резерв")
		t.Logf("запрос дошёл до резервного узла: %s; группа на резервном узле: %s", rtxYesNo(saw),
			rtxYesNo(nerr == nil && now == reserveNode.MihomoName))
		if !saw {
			t.Error("при мёртвых основных узлах запрос не дошёл до резервного узла (утечка или обрыв вместо резерва)")
		}
		return
	}

	const domain = "primary.example.test"
	_, _ = rtmtProbe(core.port, "http://"+domain+"/", 25*time.Second, 3*time.Second)
	sawMain := rtWaitNodeSaw(mainLog, domain, 5*time.Second)
	sawReserve := rtNodeSaw(reserveLog, domain)
	t.Logf("запрос дошёл до основного узла: %s, до резервного: %s", rtxYesNo(sawMain), rtxYesNo(sawReserve))
	if !sawMain {
		t.Error("при живом основном узле запрос не дошёл до основного узла")
	}
	if sawReserve {
		t.Error("при живом основном узле запрос ушёл на резервный узел")
	}
}

// Маршрутизация по правилам на настоящем Mihomo: домен в DIRECT не доходит до
// узла, остальное доходит, REJECT обрывает соединение; правило из udp и порта 443
// (логическое AND, A9) принято ядром и не задевает TCP на 443. Узел — локальный
// процесс Xray с журналом доступа, интернет не нужен.
func TestRouterMihomoRulesTraffic(t *testing.T) {
	nodePort, accessLog := rtStartNode(t)
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithKernels(rtMihomoOnly), rtWithMihomoCore(core))
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:"+strconv.Itoa(nodePort)+"#rt-node")

	_, nodeGroup, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Узел", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindReject},
		Conds: []Cond{{Type: CondNetwork, Values: []string{"udp"}}, {Type: CondPort, Values: []string{"443"}}},
	}); err != nil {
		t.Fatalf("SaveRule (udp 443 → REJECT): %v", err)
	}
	for _, r := range []struct {
		domain string
		target Target
	}{
		{"direct.example.test", Target{Kind: KindDirect}},
		{"reject.example.test", Target{Kind: KindReject}},
	} {
		if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
			Enabled: true, Target: r.target,
			Conds: []Cond{{Type: CondDomain, Values: []string{r.domain}}},
		}); err != nil {
			t.Fatalf("SaveRule: %v", err)
		}
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: nodeGroup.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	// «Применить» проверяет профиль настоящим mihomo -t (правило AND принято, A9).
	e.takeAndApply(t)
	rtmtStart(t, e, core, nil)

	// Остальное доходит до узла (запрос может упасть на DNS узла — это не важно).
	_, _ = rtSocksGet(core.port, "http://proxy.example.test/", 8*time.Second)
	sawProxy := rtWaitNodeSaw(accessLog, "proxy.example.test", 10*time.Second)
	_, _ = rtSocksGet(core.port, "http://direct.example.test/", 6*time.Second)
	_, errReject := rtSocksGet(core.port, "http://reject.example.test/", 6*time.Second)
	// TCP на 443: правило udp+443 его не задевает, запрос идёт на узел.
	_, _ = rtSocksGet(core.port, "https://tcp443.example.test/", 8*time.Second)
	sawTCP443 := rtWaitNodeSaw(accessLog, "tcp443.example.test", 10*time.Second)
	time.Sleep(2 * time.Second)
	sawDirect := rtNodeSaw(accessLog, "direct.example.test")
	sawReject := rtNodeSaw(accessLog, "reject.example.test")

	if core.pid() == 0 {
		t.Fatalf("процесс Mihomo завершился: %s", core.proc.tail())
	}
	t.Logf("узел видел: остальное %s, DIRECT %s, REJECT %s, TCP 443 %s; запрос REJECT не прошёл: %s",
		rtxYesNo(sawProxy), rtxYesNo(sawDirect), rtxYesNo(sawReject), rtxYesNo(sawTCP443), rtxYesNo(errReject != nil))
	if !sawProxy {
		t.Error("домен без правила не дошёл до узла (журнал узла пуст — маршрут в группу не работает)")
	}
	if sawDirect {
		t.Error("домен с правилом DIRECT дошёл до узла")
	}
	if sawReject {
		t.Error("домен с правилом REJECT дошёл до узла")
	}
	if errReject == nil {
		t.Error("запрос к домену с правилом REJECT прошёл: соединение не закрыто")
	}
	if !sawTCP443 {
		t.Error("TCP на порт 443 не дошёл до узла: правило udp+443 задело TCP")
	}
}
