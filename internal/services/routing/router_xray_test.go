//go:build router

package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Материализация групп и правил в Xray: настоящий слой, настоящий
// `xray run -test` устройства в конвейере «Применить», файлы панели читаются с
// диска. Ядро стенда не трогается (Applier не задан), ядро тест не читает и не
// переключает — метка ядра не нужна.

// rtxDefaultProbeURL — адрес проверки узлов по умолчанию.
const rtxDefaultProbeURL = "https://www.gstatic.com/generate_204"

// xrayDir — каталог файлов Xray слоя окружения.
func (e *rtEnv) xrayDir() string { return filepath.Join(e.dir, "xray") }

// xrayDoc читает файл панели Xray как JSON-объект.
func (e *rtEnv) xrayDoc(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.xrayDir(), name))
	if err != nil {
		t.Fatalf("файл панели %s: %v", name, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("файл панели %s не JSON: %v", name, err)
	}
	return doc
}

// xrayFileExists сообщает, есть ли файл панели на диске.
func (e *rtEnv) xrayFileExists(name string) bool {
	_, err := os.Stat(filepath.Join(e.xrayDir(), name))
	return err == nil
}

// applyOK запускает «Применить» и требует успеха (проверка ядром пройдена).
func (e *rtEnv) applyOK(t *testing.T) {
	t.Helper()
	res, err := e.applyResult(t)
	if err != nil {
		t.Fatalf("«Применить» отклонено: %v", err)
	}
	if !res.OK || res.Code != configlayer.ResultApplied {
		t.Fatalf("«Применить»: ok=%v code=%q kernel=%q rolled_back=%v issues=%+v", res.OK, res.Code, res.Kernel, res.RolledBack, res.Issues)
	}
}

// takeAndApply берёт маршрутизацию под панель и применяет.
func (e *rtEnv) takeAndApply(t *testing.T) {
	t.Helper()
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	e.applyOK(t)
}

// rtxOutbounds индексирует исходящие файла групп по тегу.
func rtxOutbounds(t *testing.T, doc map[string]any) map[string]map[string]any {
	t.Helper()
	list, _ := doc["outbounds"].([]any)
	out := make(map[string]map[string]any, len(list))
	for _, x := range list {
		ob, ok := x.(map[string]any)
		if !ok {
			t.Fatalf("исходящий не объект: %T", x)
		}
		tag, _ := ob["tag"].(string)
		if _, dup := out[tag]; dup {
			t.Errorf("тег исходящего %q повторён", tag)
		}
		out[tag] = ob
	}
	return out
}

// rtxRouting достаёт объект routing из файла.
func rtxRouting(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	r, ok := doc["routing"].(map[string]any)
	if !ok {
		t.Fatalf("в файле нет объекта routing: %v", doc)
	}
	return r
}

// rtxRules достаёт правила routing.
func rtxRules(t *testing.T, routing map[string]any) []map[string]any {
	t.Helper()
	list, _ := routing["rules"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, x := range list {
		r, ok := x.(map[string]any)
		if !ok {
			t.Fatalf("правило не объект: %T", x)
		}
		out = append(out, r)
	}
	return out
}

// rtxStrings приводит JSON-массив строк к []string.
func rtxStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

// rtxEqualStrings сравнивает списки строк как есть.
func rtxEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rtxXrayTags — отсортированные теги узлов Xray каталога.
func rtxXrayTags(e *rtEnv, t *testing.T) []string {
	t.Helper()
	var tags []string
	for _, n := range e.catalog(t).Xray {
		tags = append(tags, n.XrayTag)
	}
	sort.Strings(tags)
	return tags
}

// Группы и правила черновика после «Применить» со взятием превращаются в файлы
// Xray и проходят проверку ядром; возврат маршрутизации убирает файлы панели.
func TestRouterXrayGroupsMaterialize(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")

	cfg := e.groups(t)
	auto, okA := rtGroupByName(cfg, "Авто")
	proxyG, okP := rtGroupByName(cfg, "Прокси")
	if !okA || !okP {
		t.Fatalf("нет групп по умолчанию: %+v", cfg.Items)
	}
	_, svcGroup, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Сервис", Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: proxyG.ID}, {Kind: KindGroup, ID: auto.ID}, {Kind: KindDirect}},
	})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{"direct.example.test"}}},
	}); err != nil {
		t.Fatalf("SaveRule (домен → DIRECT): %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindReject},
		Conds: []Cond{{Type: CondNetwork, Values: []string{"udp"}}, {Type: CondPort, Values: []string{"443"}}},
	}); err != nil {
		t.Fatalf("SaveRule (udp 443 → REJECT): %v", err)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: proxyG.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: rtxDefaultProbeURL, IntervalS: 20, TimeoutS: 5}); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}

	// «Применить» со взятием: проверка настоящим xray run -test на итоговой раскладке.
	e.takeAndApply(t)

	outs := rtxOutbounds(t, e.xrayDoc(t, xrayGroupsFile))
	if direct := outs[TagDirect]; direct == nil || direct["protocol"] != "freedom" {
		t.Errorf("xcp-direct: %v", direct)
	} else if mark := direct["streamSettings"].(map[string]any)["sockopt"].(map[string]any)["mark"]; mark != float64(255) {
		t.Errorf("метка xcp-direct %v, ожидалась 255", mark)
	}
	if block := outs[TagBlock]; block == nil || block["protocol"] != "blackhole" {
		t.Errorf("xcp-block: %v", block)
	}
	for _, g := range []Group{proxyG, svcGroup} {
		ob := outs[GroupTag(g.ID)]
		if ob == nil || ob["protocol"] != "loopback" {
			t.Errorf("исходящий группы %s: %v (ожидался loopback на «Авто»)", g.Name, ob)
			continue
		}
		if in := ob["settings"].(map[string]any)["inboundTag"]; in != loopbackTagPrefix+g.ID {
			t.Errorf("loopback группы %s: inboundTag %v", g.Name, in)
		}
	}
	if _, has := outs[GroupTag(auto.ID)]; has {
		t.Error("у группы «авто» есть исходящий: она должна быть balancer")
	}

	routing := rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))
	if routing["domainStrategy"] != "AsIs" {
		t.Errorf("domainStrategy %v, ожидался AsIs", routing["domainStrategy"])
	}
	wantSel := rtxXrayTags(e, t)
	bals, _ := routing["balancers"].([]any)
	if len(bals) != 1 {
		t.Fatalf("balancer-ов %d, ожидался один («Авто»)", len(bals))
	}
	bal := bals[0].(map[string]any)
	if bal["tag"] != GroupTag(auto.ID) || bal["fallbackTag"] != TagBlock {
		t.Errorf("balancer: tag=%v fallbackTag=%v", bal["tag"], bal["fallbackTag"])
	}
	if st, _ := bal["strategy"].(map[string]any); st["type"] != "leastPing" {
		t.Errorf("strategy balancer: %v", bal["strategy"])
	}
	if sel := rtxStrings(bal["selector"]); !rtxEqualStrings(sel, wantSel) {
		t.Errorf("selector balancer %v, ожидались явные теги каталога %v", sel, wantSel)
	}

	rules := rtxRules(t, routing)
	if len(rules) != 5 {
		t.Fatalf("правил %d, ожидалось 5 (два loopback, два правила пользователя, «Остальное»): %v", len(rules), rules)
	}
	for i, g := range []Group{proxyG, svcGroup} {
		r := rules[i]
		if !rtxEqualStrings(rtxStrings(r["inboundTag"]), []string{loopbackTagPrefix + g.ID}) || r["balancerTag"] != GroupTag(auto.ID) {
			t.Errorf("правило loopback %d: %v", i, r)
		}
	}
	if r := rules[2]; !rtxEqualStrings(rtxStrings(r["domain"]), []string{"domain:direct.example.test"}) || r["outboundTag"] != TagDirect {
		t.Errorf("правило «домен → DIRECT»: %v", r)
	}
	if r := rules[3]; r["network"] != "udp" || r["port"] != "443" || r["outboundTag"] != TagBlock {
		t.Errorf("правило «udp 443 → REJECT»: %v", r)
	}
	if r := rules[4]; r["network"] != "tcp,udp" || r["outboundTag"] != GroupTag(proxyG.ID) {
		t.Errorf("правило «Остальное»: %v", r)
	}

	obs, _ := e.xrayDoc(t, xrayObservatoryFile)["burstObservatory"].(map[string]any)
	if obs == nil {
		t.Fatal("в файле observatory нет burstObservatory")
	}
	ping, _ := obs["pingConfig"].(map[string]any)
	if ping["interval"] != "20s" || ping["timeout"] != "5s" || ping["sampling"] != float64(ProbeSampling) || ping["destination"] != rtxDefaultProbeURL {
		t.Errorf("pingConfig: %v", ping)
	}
	if sub := rtxStrings(obs["subjectSelector"]); !rtxEqualStrings(sub, wantSel) {
		t.Errorf("subjectSelector %v, ожидались %v", sub, wantSel)
	}

	// Возврат: файлов панели нет.
	if _, err := e.svc.Release(e.rev(t)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	e.applyOK(t)
	for _, name := range []string{xrayGroupsFile, xrayRoutingFile, xrayObservatoryFile} {
		if e.xrayFileExists(name) {
			t.Errorf("после возврата файл панели %s остался на диске", name)
		}
	}
}
