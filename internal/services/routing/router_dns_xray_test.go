//go:build router

package routing

import (
	"encoding/json"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Пути DNS при перехвате на Xray (DNSF-04): файлы генераторов маршрутизации,
// секций и источников с живыми данными перехвата проверяются настоящим
// `xray run -test`, затем отдельный процесс Xray на копии файлов отвечает на
// запросы через входящий перехвата (в копии слушатель перенесён с :53 на
// свободный порт loopback). Метки ядра нет: тест запускает свой процесс Xray в
// каталоге теста, активное ядро стенда не читает и не переключает, порт 53 не
// занимает. Резолвер прошивки — 127.0.0.1:53 (прошивка без override). «Узел» —
// rtStartNode: соединение DoH к адресу апстрима в его журнале доступа значит,
// что запрос прошёл через группу. В журнал теста — только да/нет.

const (
	// rtdxExtAlive и rtdxExtDead — нейтральные внешние имена (разные, чтобы
	// второе не попало в кеш).
	rtdxExtAlive = "example.com."
	rtdxExtDead  = "www.example.com."
	// rtdxDirectName — домен правила с целью direct (D-21).
	rtdxDirectName = "www.example.org"
	// rtdxNodeName — имя сервера узла, заданного доменом (D-18).
	rtdxNodeName = "example.org"
	// rtdxZone — своя локальная зона перехвата.
	rtdxZone = "rt-zone.test"
	// rtdxFirmware — резолвер прошивки без override.
	rtdxFirmware = "127.0.0.1:53"
	// rtdxSectionValue — значение секции DNS Xray: localhost под перехватом —
	// петля и отбрасывается, остаются умолчания D-17; queryStrategy пользователя
	// переносится как есть.
	rtdxSectionValue = `{"servers":["localhost"],"queryStrategy":"UseIPv4"}`
)

// rtdxSetup — окружение сценария: слой, узел и группы.
type rtdxSetup struct {
	e         *rtEnv
	accessLog string
	proxyID   string
	autoID    string
	// svc — служебный домен роутера без точки в конце; пусто — бренд не определён.
	svc string
}

// rtdxNewSetup готовит окружение: узел rt-node — единственный участник группы
// «Прокси» (цель перехвата по умолчанию), правило direct с доменом, маршрутизация
// взята в панель (в черновике).
func rtdxNewSetup(t *testing.T) *rtdxSetup {
	t.Helper()
	rtNeedBins(t, rtXrayBin)
	nodePort, accessLog := rtStartNode(t)
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	// Второй узел задан доменным именем (D-18): в группу он не входит, но его имя
	// резолвится мимо группы.
	e.addMyRaw(t, "socks5://127.0.0.1:"+strconv.Itoa(nodePort)+"#rt-node", "socks5://"+rtdxNodeName+":1#rt-named")
	ref := ""
	for _, n := range e.catalog(t).Xray {
		if n.Node.Name == "rt-node" {
			ref = n.Ref
		}
	}
	if ref == "" {
		t.Fatal("узла rt-node нет в каталоге Xray")
	}
	e.replaceGroups(t, func(cfg *GroupsConfig) {
		for i := range cfg.Items {
			g := &cfg.Items[i]
			if g.Name != configlayer.DefaultDNSGroupName {
				continue
			}
			g.Mode = ModeManual
			g.Members = nil
			g.Sources = GroupSources{}
			g.Include, g.Exclude = nil, nil
			g.Picks = []string{ref}
		}
	})
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{rtdxDirectName}}},
	}); err != nil {
		t.Fatalf("SaveRule (домен → DIRECT): %v", err)
	}
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	cfg := e.groups(t)
	proxyG, okP := rtGroupByName(cfg, configlayer.DefaultDNSGroupName)
	auto, okA := rtGroupByName(cfg, "Авто")
	if !okP || !okA {
		t.Fatalf("нет групп по умолчанию: %+v", cfg.Items)
	}
	return &rtdxSetup{
		e: e, accessLog: accessLog, proxyID: proxyG.ID, autoID: auto.ID,
		svc: strings.TrimSuffix(rtdsServiceDomain(), "."),
	}
}

// live — живые данные перехвата на Xray с резолвером прошивки без override.
func (s *rtdxSetup) live() configlayer.DNSTakeoverLive {
	return configlayer.DNSTakeoverLive{
		Enabled: true, Kernel: configlayer.KernelXray,
		FirmwareResolver: rtdxFirmware, ServiceDomain: s.svc,
	}
}

// input — вход генераторов: черновик окружения, секция DNS Xray под панелью,
// параметры перехвата и его живые данные (nil — перехвата нет).
func (s *rtdxSetup) input(t *testing.T, params configlayer.DNSTakeoverParams, live *configlayer.DNSTakeoverLive) configlayer.Input {
	t.Helper()
	in := inputOf(s.e.state(t))
	in.Sections = maps.Clone(in.Sections)
	if in.Sections == nil {
		in.Sections = configlayer.Sections{}
	}
	cs, err := configlayer.EncodeCoreSection(configlayer.CoreSection{Managed: true, Value: json.RawMessage(rtdxSectionValue)})
	if err != nil {
		t.Fatal(err)
	}
	in.Sections[configlayer.SectionXrayDNS] = cs
	p, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	in.Sections[configlayer.SectionDNSTakeover] = p
	in.Live = maps.Clone(in.Live)
	if in.Live == nil {
		in.Live = configlayer.Sections{}
	}
	if live != nil {
		raw, err := json.Marshal(live)
		if err != nil {
			t.Fatal(err)
		}
		in.Live[configlayer.LiveKeyDNSTakeover] = raw
	}
	return in
}

// rtdxGenerate собирает файлы Xray генераторов источников, маршрутизации и
// секций ядра. Файл, который выпустили два генератора, — провал: у каждого файла
// один производитель на запуск.
func rtdxGenerate(t *testing.T, e *rtEnv, in configlayer.Input) map[string][]byte {
	t.Helper()
	gens := slices.Concat(e.src.Generators(), e.svc.Generators(), []configlayer.Generator{cores.XrayGenerator()})
	out := map[string][]byte{}
	owner := map[string]string{}
	for _, g := range gens {
		files, err := g.Generate(in, e.installed)
		if err != nil {
			t.Fatalf("генератор %s: %v", g.ID(), err)
		}
		for _, f := range files {
			if f.Kernel != configlayer.KernelXray {
				continue
			}
			if prev, dup := owner[f.RelPath]; dup {
				t.Fatalf("файл %s выпустили два генератора: %s и %s", f.RelPath, prev, g.ID())
			}
			owner[f.RelPath] = g.ID()
			out[f.RelPath] = f.Content
		}
	}
	return out
}

// rtdxDoc разбирает файл из набора как JSON-объект.
func rtdxDoc(t *testing.T, files map[string][]byte, name string) map[string]any {
	t.Helper()
	data, ok := files[name]
	if !ok {
		t.Fatalf("файла %s нет среди файлов генераторов", name)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("файл %s не JSON: %v", name, err)
	}
	return doc
}

// rtdxWrite пишет файлы набора в новый каталог теста.
func rtdxWrite(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-dnsx-")
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rtdxCheck проверяет набор файлов настоящим `xray run -test`.
func rtdxCheck(t *testing.T, files map[string][]byte) {
	t.Helper()
	dir := rtdxWrite(t, files)
	if errLine := rtdsCheck(rtXrayBin, []string{"run", "-test", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), "")); errLine != "" {
		t.Fatalf("xray run -test отверг файлы перехвата: %s", errLine)
	}
}

// rtdxCore — отдельный процесс Xray на копии файлов.
type rtdxCore struct {
	dir       string
	readyPort int
	addr      string
	proc      *rtProc
}

// rtdxStart запускает отдельный Xray на копии набора: слушатель перехвата
// переносится на свободный порт loopback, блок api панели убирается (его порт
// занят ядром стенда).
func rtdxStart(t *testing.T, files map[string][]byte) *rtdxCore {
	t.Helper()
	files = maps.Clone(files)
	delete(files, configlayer.PanelXrayAPIFile)
	dnsPort := rtFreePort(t)
	doc := rtdxDoc(t, files, xrayDNSInboundsFile)
	list, _ := doc["inbounds"].([]any)
	if len(list) == 0 {
		t.Fatal("в файле слушателя нет входящих")
	}
	first, _ := list[0].(map[string]any)
	first["listen"] = "127.0.0.1"
	first["port"] = dnsPort
	doc["inbounds"] = []any{first}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	files[xrayDNSInboundsFile] = raw
	readyPort := rtFreePort(t)
	dir := rtXrayCoreDir(t, rtdxWrite(t, files), readyPort)
	c := &rtdxCore{dir: dir, readyPort: readyPort, addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))}
	c.proc = rtStartXrayDir(t, dir, readyPort)
	return c
}

// restart перезапускает процесс на тех же файлах (пустой кеш DNS).
func (c *rtdxCore) restart(t *testing.T) {
	t.Helper()
	c.proc.stop()
	c.proc = rtStartXrayDir(t, c.dir, c.readyPort)
}

// rtdxDoHCount — сколько соединений DoH к апстриму видел узел.
func rtdxDoHCount(accessLog string) int {
	data, err := os.ReadFile(accessLog)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), rtdsDoHSeen)
}

// rtdxDNSServers — серверы объекта dns файла 99_xcp-dns.json.
func rtdxDNSServers(t *testing.T, files map[string][]byte) (dns map[string]any, servers []any) {
	t.Helper()
	dns, _ = rtdxDoc(t, files, xrayDNSFile)["dns"].(map[string]any)
	if dns == nil {
		t.Fatal("в файле 99_xcp-dns.json нет объекта dns")
	}
	servers, _ = dns["servers"].([]any)
	return dns, servers
}

// rtdxServerWith ищет сервер-объект, в domains которого есть значение.
func rtdxServerWith(servers []any, domain string) map[string]any {
	for _, s := range servers {
		obj, ok := s.(map[string]any)
		if ok && slices.Contains(rtxStrings(obj["domains"]), domain) {
			return obj
		}
	}
	return nil
}

func TestRouterDNSXrayPaths(t *testing.T) {
	s := rtdxNewSetup(t)
	params := configlayer.DNSTakeoverParams{Zones: []string{rtdxZone}}
	live := s.live()
	files := rtdxGenerate(t, s.e, s.input(t, params, &live))

	// Без перехвата: слушателя нет, объект dns пишет генератор секций без тега.
	plain := rtdxGenerate(t, s.e, s.input(t, params, nil))
	if _, has := plain[xrayDNSInboundsFile]; has {
		t.Error("без перехвата есть файл слушателя")
	}
	if dns, _ := rtdxDoc(t, plain, xrayDNSFile)["dns"].(map[string]any); dns == nil || dns["tag"] != nil {
		t.Errorf("без перехвата объект dns не тот: %v", dns)
	}

	// Слушатель: dokodemo-door на :53 без прозрачного перенаправления.
	inbounds, _ := rtdxDoc(t, files, xrayDNSInboundsFile)["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("входящих перехвата %d, ожидался один (IPv6 нет)", len(inbounds))
	}
	in0, _ := inbounds[0].(map[string]any)
	settings, _ := in0["settings"].(map[string]any)
	if in0["tag"] != TagDNSIn || in0["port"] != float64(53) || in0["protocol"] != "dokodemo-door" || in0["listen"] != "0.0.0.0" ||
		settings["network"] != "tcp,udp" || settings["followRedirect"] != nil {
		t.Errorf("входящий перехвата: %v", in0)
	}

	// Исходящий dns — последним в файле .tail.
	groupsDoc := rtdxDoc(t, files, xrayGroupsFile)
	outList, _ := groupsDoc["outbounds"].([]any)
	if last, _ := outList[len(outList)-1].(map[string]any); last["tag"] != TagDNSOut || last["protocol"] != "dns" {
		t.Errorf("последний исходящий групп: %v, ожидался %s (dns)", last, TagDNSOut)
	}

	// Правила DNS — первыми, раньше правил пользователя.
	rules := rtxRules(t, rtxRouting(t, rtdxDoc(t, files, xrayRoutingFile)))
	if len(rules) < 4 {
		t.Fatalf("правил %d: %v", len(rules), rules)
	}
	want := []struct {
		in, field, tag string
	}{
		{TagDNSIn, "outboundTag", TagDNSOut},
		{TagDNSDirect, "outboundTag", TagDirect},
		{TagDNSQuery, "outboundTag", GroupTag(s.proxyID)},
	}
	for i, w := range want {
		if !rtxEqualStrings(rtxStrings(rules[i]["inboundTag"]), []string{w.in}) || rules[i][w.field] != w.tag {
			t.Errorf("правило DNS %d: %v, ожидалось %s → %s", i, rules[i], w.in, w.tag)
		}
	}
	userAt := -1
	for i, r := range rules {
		if slices.Contains(rtxStrings(r["domain"]), "domain:"+rtdxDirectName) {
			userAt = i
		}
	}
	if userAt < len(want) {
		t.Errorf("правило пользователя на месте %d, правила DNS должны стоять раньше", userAt)
	}

	// Объект dns: тег запросов, сервер локальных зон, умолчания вместо петли.
	dns, servers := rtdxDNSServers(t, files)
	if dns["tag"] != TagDNSQuery || dns["queryStrategy"] != "UseIPv4" {
		t.Errorf("объект dns: tag=%v queryStrategy=%v", dns["tag"], dns["queryStrategy"])
	}
	zoneSrv := rtdxServerWith(servers, "domain:"+rtdxZone)
	if zoneSrv == nil || zoneSrv["tag"] != TagDNSDirect || zoneSrv["finalQuery"] != true || zoneSrv["skipFallback"] != true ||
		zoneSrv["address"] != "127.0.0.1" || zoneSrv["port"] != float64(53) {
		t.Errorf("сервер локальных зон: %v", zoneSrv)
	}
	if s.svc != "" && rtdxServerWith(servers, "domain:"+s.svc) == nil {
		t.Error("служебного домена нет в серверах локальных зон")
	}
	if slices.Contains(servers, any("localhost")) || !slices.Contains(servers, any("https://1.1.1.1/dns-query")) {
		t.Errorf("серверы пользователя под перехватом: петля не убрана или нет умолчаний D-17: %v", servers)
	}

	rtdxCheck(t, files)
	core := rtdxStart(t, files)
	ext := rtdsResolveWait(core.addr, rtdxExtAlive, 20*time.Second)
	saw := rtWaitNodeSaw(s.accessLog, rtdsDoHSeen, 5*time.Second)
	t.Logf("внешнее имя: %s, через группу: %s", rtxYesNo(ext.resolved()), rtxYesNo(saw))
	if !ext.resolved() || !saw {
		t.Errorf("внешнее имя через входящий перехвата: ответ %s, узел видел DoH %s", ext.code(), rtxYesNo(saw))
	}
	if s.svc != "" {
		local := rtdsResolveWait(core.addr, s.svc+".", 10*time.Second)
		t.Logf("служебный домен: %s", rtxYesNo(local.resolved()))
		if !local.resolved() {
			t.Errorf("служебный домен через прошивку не разрешился: %s", local.code())
		}
	} else {
		t.Log("бренд прошивки не определён: путь служебного домена не проверялся")
	}

	t.Run("user_dns_tags", func(t *testing.T) {
		// Файл пользователя с DNS-тегами без префикса панели, как в типовых
		// шаблонах; сортируется после файлов перехвата панели.
		userPort := rtFreePort(t)
		user, err := json.Marshal(map[string]any{
			"inbounds": []any{map[string]any{
				"tag": "dns-in", "listen": "127.0.0.1", "port": userPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{"address": "1.1.1.1", "port": 53, "network": "tcp,udp"},
			}},
			"outbounds": []any{map[string]any{"tag": "dns-out", "protocol": "dns"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		withUser := maps.Clone(files)
		withUser["05_user-dns.json"] = user
		rtdxCheck(t, withUser)
		before := rtdxDoHCount(s.accessLog)
		c := rtdxStart(t, withUser)
		r := rtdsResolveWait(c.addr, rtdxExtAlive, 20*time.Second)
		viaGroup := false
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
			if rtdxDoHCount(s.accessLog) > before {
				viaGroup = true
				break
			}
		}
		userListens := false
		if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(userPort)), 3*time.Second); err == nil {
			_ = conn.Close()
			userListens = true
		}
		t.Logf("с файлом пользователя: ответ %s, через группу %s, входящий пользователя слушает %s",
			rtxYesNo(r.resolved()), rtxYesNo(viaGroup), rtxYesNo(userListens))
		if !r.resolved() || !viaGroup {
			t.Errorf("входящий перехвата рядом с DNS-тегами пользователя: ответ %s, через группу %s", r.code(), rtxYesNo(viaGroup))
		}
		if !userListens {
			t.Error("входящий пользователя не слушает свой порт: фрагменты слились по тегу")
		}
	})

	t.Run("balancer", func(t *testing.T) {
		bf := rtdxGenerate(t, s.e, s.input(t, configlayer.DNSTakeoverParams{Group: s.autoID}, &live))
		rs := rtxRules(t, rtxRouting(t, rtdxDoc(t, bf, xrayRoutingFile)))
		if len(rs) < 3 {
			t.Fatalf("правил %d: %v", len(rs), rs)
		}
		if !rtxEqualStrings(rtxStrings(rs[2]["inboundTag"]), []string{TagDNSQuery}) ||
			rs[2]["balancerTag"] != GroupTag(s.autoID) || rs[2]["outboundTag"] != nil {
			t.Errorf("правило запросов DNS на авто-группу: %v", rs[2])
		}
		rtdxCheck(t, bf)
	})

	// Мёртвая группа: узел остановлен, ядро перезапущено (пустой кеш).
	if !rtdsStopNode(s.accessLog) {
		t.Fatal("узел не остановлен")
	}
	core.restart(t)

	t.Run("dead_group", func(t *testing.T) {
		dead := rtdsQuery(core.addr, rtdxExtDead, dnsmessage.TypeA, 6*time.Second)
		t.Logf("внешнее имя при мёртвой группе разрешилось: %s", rtxYesNo(dead.resolved()))
		if dead.resolved() {
			t.Error("внешнее имя разрешилось при мёртвой группе: DNS ушёл мимо туннеля (D-10)")
		}
		if s.svc == "" {
			t.Log("бренд прошивки не определён: локальный путь не проверялся")
			return
		}
		local := rtdsResolveWait(core.addr, s.svc+".", 10*time.Second)
		t.Logf("служебный домен при мёртвой группе: %s", rtxYesNo(local.resolved()))
		if !local.resolved() {
			t.Errorf("служебный домен при мёртвой группе не разрешился: %s", local.code())
		}
	})

	t.Run("nodes", func(t *testing.T) {
		// Имя узла: сначала DoH по IP напрямую, затем прошивка с finalQuery.
		name := "full:" + rtdxNodeName
		var doh, fw map[string]any
		for _, x := range servers {
			obj, ok := x.(map[string]any)
			if !ok || !slices.Contains(rtxStrings(obj["domains"]), name) {
				continue
			}
			if doh == nil {
				doh = obj
			} else if fw == nil {
				fw = obj
			}
		}
		if doh == nil || doh["address"] != "https://1.1.1.1/dns-query" || doh["tag"] != TagDNSDirect || doh["skipFallback"] != true {
			t.Errorf("сервер DoH для имён узлов: %v", doh)
		}
		if fw == nil || fw["address"] != "127.0.0.1" || fw["tag"] != TagDNSDirect || fw["finalQuery"] != true {
			t.Errorf("запасной сервер прошивки для имён узлов: %v", fw)
		}
		r := rtdsResolveWait(core.addr, rtdxNodeName+".", 15*time.Second)
		t.Logf("имя узла при мёртвой группе: %s", rtxYesNo(r.resolved()))
		if !r.resolved() {
			t.Errorf("имя узла не разрешилось при мёртвой группе: %s", r.code())
		}
	})

	t.Run("direct_rules", func(t *testing.T) {
		// Домен direct-правила: прошивка без finalQuery — запас через группу.
		srv := rtdxServerWith(servers, "domain:"+rtdxDirectName)
		if srv == nil || srv["address"] != "127.0.0.1" || srv["tag"] != TagDNSDirect || srv["finalQuery"] != nil || srv["skipFallback"] != true {
			t.Errorf("сервер direct-доменов: %v", srv)
		}
		r := rtdsResolveWait(core.addr, rtdxDirectName+".", 15*time.Second)
		t.Logf("домен direct-правила при мёртвой группе: %s", rtxYesNo(r.resolved()))
		if !r.resolved() {
			t.Errorf("домен direct-правила не разрешился через прошивку при мёртвой группе: %s", r.code())
		}
	})
}
