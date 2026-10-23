//go:build router

package routing

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Ручные группы Mihomo по правилу D-36 (146-31, закрытие CR-01): группа без
// действующего участника в Mihomo не выпускается, правила на неё ведут в REJECT,
// как у Xray; участник по умолчанию стоит первым, DIRECT и REJECT не первые идут
// после узлов. Отдельный процесс Mihomo в каталоге теста (как в
// router_mihomo_traffic_test.go): ядро стенда не читается и не переключается,
// метка ядра не нужна. В лог идут только «да/нет» и числа, без имён узлов и
// адресов.

// rtmXrayOnlyLink — узел только для Xray: vless на loopback с транспортом kcp без
// маскировки (Mihomo kcp не умеет, код kcp_mihomo; Xray принимает kcpSettings без
// headerType и seed).
const rtmXrayOnlyLink = "vless://11111111-1111-4111-8111-111111111111@127.0.0.1:9?type=kcp&security=none&encryption=none#rt-xray-only"

// Группа сервиса [Прокси, Авто, DIRECT] при узлах только для Xray: в Mihomo у неё
// нет действующего участника (DIRECT стоит не первым), как у Xray. Группы в
// профиле нет, правило ведёт в REJECT, у группы предупреждение ядра, трафик цели на
// loopback через отдельный процесс Mihomo не проходит.
func TestRouterMihomoManualFailClosed(t *testing.T) {
	_, targetURL := rtmtTarget(t)
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithMihomoCore(core))
	e.writeUserProfile(t)
	e.addMyRaw(t, rtmXrayOnlyLink)

	cat := e.catalog(t)
	var inXray, inMihomo bool
	for _, n := range cat.Xray {
		inXray = inXray || n.Display == "rt-xray-only"
	}
	for _, n := range cat.Mihomo {
		inMihomo = inMihomo || n.Display == "rt-xray-only"
	}
	if !inXray || inMihomo {
		t.Fatalf("набор ссылок не даёт узла только для Xray: в каталоге Xray %s, в каталоге Mihomo %s",
			rtxYesNo(inXray), rtxYesNo(inMihomo))
	}

	cfg := e.groups(t)
	proxyG, okP := rtGroupByName(cfg, "Прокси")
	auto, okA := rtGroupByName(cfg, "Авто")
	if !okP || !okA {
		t.Fatalf("нет групп по умолчанию: %+v", cfg.Items)
	}
	_, svcGroup, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Сервис", Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: proxyG.ID}, {Kind: KindGroup, ID: auto.ID}, {Kind: KindDirect}},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Сервис): %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindGroup, ID: svcGroup.ID},
		Conds: []Cond{{Type: CondIP, Values: []string{"127.0.0.1/32"}}},
	}); err != nil {
		t.Fatalf("SaveRule (IP → Сервис): %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindReject}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	e.takeAndApply(t)

	// Профиль: правило на группу без действующего участника ведёт в REJECT, самой
	// группы нет.
	doc := e.mihomoProfile(t)
	var ruleLine string
	for _, line := range rtmStrings(doc["rules"]) {
		if strings.Contains(line, "127.0.0.1/32") {
			ruleLine = line
		}
	}
	if want := "IP-CIDR,127.0.0.1/32,REJECT,no-resolve"; ruleLine != want {
		t.Errorf("обход VPN в Mihomo: правило группы «Сервис» ведёт не в REJECT: %q", ruleLine)
	}
	if g, ok := rtmGroups(t, doc)["Сервис"]; ok {
		t.Errorf("обход VPN в Mihomo: группа «Сервис» выпущена, proxies %v", rtmStrings(g["proxies"]))
	}

	// Представление: группа пуста для Mihomo — предупреждение ядра, «Применить» не
	// блокируется (D-13).
	svcView := rtViewGroup(t, e.view(t), "Сервис")
	if !rtViewHas(svcView.Warnings, "group_empty_kernel", "group", "Сервис", "kernel", "mihomo") {
		t.Errorf("нет предупреждения «пуста для Mihomo» у группы «Сервис»: %+v", svcView.Warnings)
	}
	if len(svcView.Issues) != 0 {
		t.Errorf("группа «Сервис», пустая для одного ядра, с проблемами: %+v", svcView.Issues)
	}
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("группа, пустая для одного ядра, блокирует «Применить»: %+v", issues)
	}

	// Паритет с Xray: исходящий группы «Сервис» не freedom.
	if ob := rtsOutbound(t, e.xrayDir(), GroupTag(svcGroup.ID)); ob["protocol"] == "freedom" {
		t.Error("обход VPN в Xray: исходящий группы «Сервис» — freedom")
	}

	// Трафик: база — цель на loopback достижима напрямую через отдельный процесс
	// Mihomo, иначе «запрос не прошёл» ничего не доказывает.
	direct := rtNewMihomoCore(t)
	rtmtStart(t, e, direct, rtmtGroupsRules(nil, []string{"MATCH,DIRECT"}))
	if _, err := rtSocksGet(direct.port, targetURL, 8*time.Second); err != nil {
		t.Log("цель на loopback недоступна через Mihomo напрямую: проверка трафика пропущена")
		return
	}
	direct.stop()

	rtmtStart(t, e, core, nil)
	attempts, passed := rtmtProbe(core.port, targetURL, 9*time.Second, 3*time.Second)
	if core.pid() == 0 {
		t.Fatalf("процесс Mihomo завершился во время проверки: %s", core.proc.tail())
	}
	t.Logf("запросов через правило группы «Сервис»: %d, прошло: %d", attempts, passed)
	if passed > 0 {
		t.Errorf("утечка мимо VPN в Mihomo: %d из %d запросов прошли через группу «Сервис»", passed, attempts)
	}
}

// Группа [группа, пустая для Mihomo, DIRECT] с узлами источника: DIRECT стоит не
// первым, поэтому по умолчанию не выбирается. В профиле proxies начинается с узла,
// DIRECT последний и после всех узлов группы (узлы пишутся копиями, без use), а
// настоящий процесс Mihomo без сверки выбирает узел, и запрос по правилу группы
// доходит до узла-процесса.
func TestRouterMihomoManualDefaultNode(t *testing.T) {
	nodePort, accessLog := rtStartNode(t)
	core := rtNewMihomoCore(t)
	e := rtNewEnv(t, rtWithMihomoCore(core))
	e.writeUserProfile(t)
	e.addMyRaw(t, "socks5://127.0.0.1:"+strconv.Itoa(nodePort)+"#rt-first", rtmXrayOnlyLink)

	cat := e.catalog(t)
	rtsNode(t, cat.Mihomo, "rt-first")
	xrayOnly := rtsNode(t, cat.Xray, "rt-xray-only")
	for _, n := range cat.Mihomo {
		if n.Ref == xrayOnly.Ref {
			t.Fatal("набор ссылок не даёт узла только для Xray: узел есть в каталоге Mihomo")
		}
	}

	// Группа, пустая для Mihomo: единственный отдельный узел умеет только Xray.
	_, onlyXray, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Только Xray", Mode: ModeAuto, Picks: []string{xrayOnly.Ref}})
	if err != nil {
		t.Fatalf("SaveGroup (Только Xray): %v", err)
	}
	_, nodeFirst, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Узел первым", Mode: ModeManual, Sources: GroupSources{All: true},
		Members: []Member{{Kind: KindGroup, ID: onlyXray.ID}, {Kind: KindDirect}},
	})
	if err != nil {
		t.Fatalf("SaveGroup (Узел первым): %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindGroup, ID: nodeFirst.ID},
		Conds: []Cond{{Type: CondDomain, Values: []string{"node-first.example.test"}}},
	}); err != nil {
		t.Fatalf("SaveRule (домен → Узел первым): %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindReject}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	e.takeAndApply(t)

	// Профиль: DIRECT не раньше узлов группы.
	g, ok := rtmGroups(t, e.mihomoProfile(t))["Узел первым"]
	if !ok {
		t.Fatal("группы «Узел первым» нет в профиле Mihomo")
	}
	proxies := rtmStrings(g["proxies"])
	if len(proxies) == 0 || proxies[0] == "DIRECT" || proxies[0] == "REJECT" {
		t.Errorf("DIRECT раньше узлов в Mihomo: группа начинается с %v", proxies)
	} else if proxies[len(proxies)-1] != "DIRECT" {
		t.Errorf("DIRECT не последний в группе «Узел первым»: %v", proxies)
	}
	if _, has := g["use"]; has {
		t.Errorf("DIRECT раньше узлов в Mihomo: узлы провайдера идут после proxies (%v)", g["use"])
	}

	// Процесс без сверки: Mihomo сам берёт первый элемент группы.
	rtmtStart(t, e, core, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now, err := core.svc.ProxyNow(ctx, "Узел первым")
	if err != nil {
		t.Fatalf("ProxyNow: %v", err)
	}
	if now == "DIRECT" || now == "REJECT" || (len(proxies) > 0 && now != proxies[0]) {
		t.Errorf("Mihomo до сверки выбрал %q, ожидался первый элемент группы %q", now, strings.Join(proxies[:min(1, len(proxies))], ""))
	}

	// Трафик: запрос по правилу группы доходит до узла-процесса (запрос может
	// упасть на DNS узла — важен лишь журнал узла).
	saw := false
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end) && !saw; {
		_, _ = rtSocksGet(core.port, "http://node-first.example.test/", 6*time.Second)
		saw = rtWaitNodeSaw(accessLog, "node-first.example.test", 2*time.Second)
	}
	if !saw {
		t.Error("запрос группы «Узел первым» не дошёл до узла: трафик мимо VPN")
	}
}
