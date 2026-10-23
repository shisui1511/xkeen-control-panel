//go:build router

package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

// applyPasses — «Применить» не отклонено и прошло: применено или черновик уже
// совпадает с применённым (nothing_to_apply), когда правки вернули его к прежнему.
func (e *rtEnv) applyPasses(t *testing.T) {
	t.Helper()
	res, err := e.applyResult(t)
	if err != nil {
		t.Fatalf("«Применить» отклонено: %v", err)
	}
	if !res.OK || (res.Code != configlayer.ResultApplied && res.Code != configlayer.ResultNothingToApply) {
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

// rtxYesNo — «да» или «нет» для лога.
func rtxYesNo(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

// Ни при каком состоянии трафик не уходит мимо VPN: все узлы «авто» мертвы —
// соединение обрывается. Контроль воспроизводит исследованную утечку: balancer
// без fallbackTag при первом исходящем freedom (как пользовательский
// 04_outbounds.json без .tail) отдаёт трафик напрямую (поправка к 143 Q6).
func TestRouterXrayBalancerFailClosed(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	// Мёртвые узлы: на этих портах loopback никто не слушает.
	e.addMyRaw(t, "socks5://127.0.0.1:9#dead-1", "socks5://127.0.0.1:10#dead-2")
	auto, ok := rtGroupByName(e.groups(t), "Авто")
	if !ok {
		t.Fatal("нет группы «Авто»")
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: auto.ID}); err != nil {
		t.Fatalf("SetFinal: %v", err)
	}
	if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: rtxDefaultProbeURL, IntervalS: 10, TimeoutS: 3}); err != nil {
		t.Fatalf("SaveProbe: %v", err)
	}
	e.takeAndApply(t)

	bals, _ := rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))["balancers"].([]any)
	if len(bals) != 1 || bals[0].(map[string]any)["fallbackTag"] != TagBlock {
		t.Fatalf("balancer без fallbackTag xcp-block: %v", bals)
	}

	// Основной шаг: после двух циклов наблюдения (не меньше 25 с) ни один запрос
	// через группу из мёртвых узлов не проходит.
	port := rtFreePort(t)
	proc := rtStartXrayDir(t, rtXrayCoreDir(t, e.xrayDir(), port), port)
	attempts, passed := 0, 0
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Second) {
		attempts++
		if _, err := rtSocksGet(port, rtxDefaultProbeURL, 6*time.Second); err == nil {
			passed++
		}
	}
	if !proc.alive() {
		t.Fatalf("процесс ядра завершился во время проверки: %s", proc.tail())
	}
	t.Logf("запросов через группу из мёртвых узлов: %d, прошло: %d", attempts, passed)
	if passed > 0 {
		t.Errorf("утечка мимо VPN: %d из %d запросов прошли при мёртвых узлах", passed, attempts)
	}

	// Контроль: тот же каталог, но без fallbackTag и с freedom первым исходящим.
	t.Run("control_without_fallback", func(t *testing.T) {
		ctrlPort := rtFreePort(t)
		dir := rtXrayCoreDir(t, e.xrayDir(), ctrlPort)
		routingPath := filepath.Join(dir, xrayRoutingFile)
		raw, err := os.ReadFile(routingPath)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		for _, b := range rtxRouting(t, doc)["balancers"].([]any) {
			delete(b.(map[string]any), "fallbackTag")
		}
		if raw, err = json.Marshal(doc); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(routingPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		// Файл без .tail встаёт в начало и становится исходящим по умолчанию.
		const userOutbounds = `{"outbounds":[{"tag":"direct","protocol":"freedom"}]}`
		if err := os.WriteFile(filepath.Join(dir, "04_outbounds.json"), []byte(userOutbounds), 0o600); err != nil {
			t.Fatal(err)
		}
		rtStartXrayDir(t, dir, ctrlPort)

		leak := false
		for end := time.Now().Add(75 * time.Second); time.Now().Before(end); time.Sleep(3 * time.Second) {
			if _, err := rtSocksGet(ctrlPort, rtxDefaultProbeURL, 6*time.Second); err == nil {
				leak = true
				break
			}
		}
		if !leak {
			// Нет утечки: либо её правда нет, либо у стенда нет интернета вообще.
			client := &http.Client{Timeout: 8 * time.Second}
			resp, err := client.Get(rtxDefaultProbeURL)
			if err != nil {
				t.Skip("у стенда нет доступа в интернет: контроль утечки не выполнен")
			}
			_ = resp.Body.Close()
		}
		t.Logf("утечка без fallbackTag: %s", rtxYesNo(leak))
	})
}

// Маршрутизация по правилам на настоящем Xray: домен в DIRECT не доходит до
// узла, остальное доходит, REJECT обрывает соединение. Узел — второй
// локальный процесс Xray с журналом доступа, интернет не нужен.
func TestRouterXrayRulesTraffic(t *testing.T) {
	nodePort, accessLog := rtStartNode(t)
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyRaw(t, "socks5://127.0.0.1:"+strconv.Itoa(nodePort)+"#rt-node")

	_, nodeGroup, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Узел", Mode: ModeManual, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
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
	e.takeAndApply(t)

	port := rtFreePort(t)
	proc := rtStartXrayDir(t, rtXrayCoreDir(t, e.xrayDir(), port), port)

	// Остальное доходит до узла (запрос может упасть на DNS узла — это не важно).
	_, _ = rtSocksGet(port, "http://proxy.example.test/", 8*time.Second)
	sawProxy := rtWaitNodeSaw(accessLog, "proxy.example.test", 10*time.Second)
	_, _ = rtSocksGet(port, "http://direct.example.test/", 6*time.Second)
	_, errReject := rtSocksGet(port, "http://reject.example.test/", 6*time.Second)
	time.Sleep(2 * time.Second)
	sawDirect := rtNodeSaw(accessLog, "direct.example.test")
	sawReject := rtNodeSaw(accessLog, "reject.example.test")

	if !proc.alive() {
		t.Fatalf("процесс ядра завершился: %s", proc.tail())
	}
	t.Logf("узел видел: остальное %s, DIRECT %s, REJECT %s; запрос REJECT не прошёл: %s",
		rtxYesNo(sawProxy), rtxYesNo(sawDirect), rtxYesNo(sawReject), rtxYesNo(errReject != nil))
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
}

// rtxNorm приводит значение к виду после JSON (числа float64, списки []any):
// ожидаемое и прочитанное с диска сравниваются через reflect.DeepEqual.
func rtxNorm(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// rtxUserRules — правила routing без loopback-правил групп и без заключительного
// «Остальное»: то, что перевело правила пользователя.
func rtxUserRules(t *testing.T, routing map[string]any) []map[string]any {
	t.Helper()
	rules := rtxRules(t, routing)
	var out []map[string]any
	for i, r := range rules {
		if i == len(rules)-1 {
			continue
		}
		if tags := rtxStrings(r["inboundTag"]); len(tags) == 1 && strings.HasPrefix(tags[0], loopbackTagPrefix) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// rtxTestFiles проверяет файлы генераторов настоящим `xray run -test`: те же
// файлы, что слой пишет в каталог ядра, но без «Применить» (для случаев, которые
// проверка до записи блокирует).
func rtxTestFiles(t *testing.T, e *rtEnv, files []configlayer.GeneratedFile) {
	t.Helper()
	in := inputOf(e.state(t))
	all := append([]configlayer.GeneratedFile(nil), files...)
	for _, g := range e.src.Generators() {
		more, err := g.Generate(in, e.installed)
		if err != nil {
			t.Fatalf("генератор %s: %v", g.ID(), err)
		}
		all = append(all, more...)
	}
	dir := rtWorkSubdir(t, "rt-xtest-")
	for _, f := range all {
		if f.Kernel != configlayer.KernelXray {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, f.RelPath), f.Content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, rtXrayBin, "run", "-test", "-confdir", dir)
	cmd.Env = utils.XrayAssetEnv(os.Environ(), "")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray run -test отверг файлы: %v: %s", err, rtRedact(string(out)))
	}
}

// rtxDatRefs находит в каталоге dat стенда первый список geosite и первый список
// geoip, пригодные для правила; nil — списка нет.
func rtxDatRefs() (site, ip *lists.ListRef) {
	ix := lists.NewIndex("")
	files, err := ix.Files()
	if err != nil {
		return nil, nil
	}
	for _, f := range files {
		if (f.Kind != lists.KindSite && f.Kind != lists.KindIP) || !listFileRE.MatchString(f.Name) {
			continue
		}
		if (f.Kind == lists.KindSite && site != nil) || (f.Kind == lists.KindIP && ip != nil) {
			continue
		}
		tags, err := ix.Tags(f.Name)
		if err != nil {
			continue
		}
		for _, tg := range tags {
			if !listTagRE.MatchString(tg.Tag) {
				continue
			}
			ref := lists.ListRef{File: f.Name, Tag: tg.Tag, Kind: f.Kind}
			if f.Kind == lists.KindSite {
				site = &ref
			} else {
				ip = &ref
			}
			break
		}
		if site != nil && ip != nil {
			break
		}
	}
	return site, ip
}

// Перевод условий правил, вложенных групп и порогов версий в формы Xray. Каждый
// вариант проходит проверку ядром (настоящий xray run -test), форма проверяется
// по записанным файлам.
func TestRouterXrayRulesTranslate(t *testing.T) {
	t.Run("conditions", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
		e.addMyLinks(t, "vless_ws")
		reject := Target{Kind: KindReject}
		blk := TagBlock
		type tcase struct {
			name string
			rule Rule
			want map[string]any // nil — правила в файле нет
		}
		cases := []tcase{
			{"domain_all_prefixes", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondDomain,
				Values: []string{"example.com", "full:a.example.com", "keyword:ads", `regexp:^ad[0-9]+\.example\.com$`}}}},
				map[string]any{"domain": []string{"domain:example.com", "full:a.example.com", "keyword:ads", `regexp:^ad[0-9]+\.example\.com$`}, "outboundTag": blk}},
			{"ip_v4_v6", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondIP, Values: []string{"10.0.0.0/8", "2001:db8::/32"}}}},
				map[string]any{"ip": []string{"10.0.0.0/8", "2001:db8::/32"}, "outboundTag": blk}},
			{"port_list_and_range", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondPort, Values: []string{"443", "8000-9000"}}}},
				map[string]any{"port": "443,8000-9000", "outboundTag": blk}},
			{"network_tcp", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondNetwork, Values: []string{"tcp"}}}},
				map[string]any{"network": "tcp", "outboundTag": blk}},
			{"network_udp", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondNetwork, Values: []string{"udp"}}}},
				map[string]any{"network": "udp", "outboundTag": blk}},
			{"network_both", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondNetwork, Values: []string{"udp", "tcp"}}}},
				map[string]any{"network": "tcp,udp", "outboundTag": blk}},
			{"src_ip", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondSrcIP, Values: []string{"192.168.1.0/24"}}}},
				map[string]any{"source": []string{"192.168.1.0/24"}, "outboundTag": blk}},
			{"bittorrent", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondBittorrent}}},
				map[string]any{"protocol": []string{"bittorrent"}, "outboundTag": blk}},
			{"two_conditions_one_object", Rule{Enabled: true, Target: reject, Conds: []Cond{
				{Type: CondDomain, Values: []string{"both.example.com"}}, {Type: CondPort, Values: []string{"443"}}}},
				map[string]any{"domain": []string{"domain:both.example.com"}, "port": "443", "outboundTag": blk}},
			{"raw_xray_target_from_rule", Rule{Enabled: true, Target: Target{Kind: KindDirect}, Raw: &RawRule{Kernel: configlayer.KernelXray,
				Text: `{"protocol":["bittorrent"],"domain":["domain:raw.example.com"],"outboundTag":"junk"}`}},
				map[string]any{"protocol": []string{"bittorrent"}, "domain": []string{"domain:raw.example.com"}, "outboundTag": TagDirect}},
			{"raw_mihomo_absent", Rule{Enabled: true, Target: reject, Raw: &RawRule{Kernel: configlayer.KernelMihomo, Text: "DOMAIN-SUFFIX,example.com"}}, nil},
			{"disabled_absent", Rule{Enabled: false, Target: reject, Conds: []Cond{{Type: CondDomain, Values: []string{"off.example.com"}}}}, nil},
		}
		// Списки dat: без файлов dat на стенде варианты пропускаются.
		site, ipList := rtxDatRefs()
		if site != nil {
			cases = append(cases, tcase{"geosite_ext", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{*site}}}},
				map[string]any{"domain": []string{"ext:" + site.File + ":" + site.Tag}, "outboundTag": blk}})
		} else {
			t.Log("списков geosite в каталоге dat стенда нет: вариант geosite пропущен")
		}
		if ipList != nil {
			cases = append(cases, tcase{"geoip_ext", Rule{Enabled: true, Target: reject, Conds: []Cond{{Type: CondGeoip, Lists: []lists.ListRef{*ipList}}}},
				map[string]any{"ip": []string{"ext:" + ipList.File + ":" + ipList.Tag}, "outboundTag": blk}})
		} else {
			t.Log("списков geoip в каталоге dat стенда нет: вариант geoip пропущен")
		}

		var want []any
		for _, c := range cases {
			if _, _, err := e.svc.SaveRule(e.rev(t), c.rule); err != nil {
				t.Fatalf("SaveRule %s: %v", c.name, err)
			}
			if c.want != nil {
				want = append(want, rtxNorm(t, c.want))
			}
		}
		e.takeAndApply(t)

		routing := rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))
		var got []any
		for _, r := range rtxUserRules(t, routing) {
			got = append(got, r)
		}
		if !reflect.DeepEqual(rtxNorm(t, got), rtxNorm(t, want)) {
			gotJSON, _ := json.MarshalIndent(got, "", " ")
			wantJSON, _ := json.MarshalIndent(want, "", " ")
			t.Errorf("правила в файле не совпали с ожидаемыми\nполучено: %s\nожидалось: %s", gotJSON, wantJSON)
		}
		rules := rtxRules(t, routing)
		if last := rules[len(rules)-1]; last["network"] != "tcp,udp" || last["outboundTag"] != TagDirect {
			t.Errorf("заключительное правило: %v", last)
		}
	})

	t.Run("nesting_and_reserve", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		cat := e.catalog(t).Xray
		if len(cat) < 3 {
			t.Fatalf("в каталоге Xray %d узлов, ожидалось 3", len(cat))
		}
		x, y := cat[0], cat[1]

		save := func(g Group) Group {
			t.Helper()
			_, saved, err := e.svc.SaveGroup(e.rev(t), g)
			if err != nil {
				t.Fatalf("SaveGroup %s: %v", g.Name, err)
			}
			return saved
		}
		inner := save(Group{Name: "Внутренняя", Mode: ModeManual, Picks: []string{x.Ref}})
		outer := save(Group{Name: "Внешняя", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: inner.ID}}})
		withReserve := save(Group{Name: "Резерв с узлом", Mode: ModeFallback, Sources: GroupSources{All: true}, Reserve: y.Ref})
		viaReserve := save(Group{Name: "Через резерв", Mode: ModeManual, Members: []Member{{Kind: KindGroup, ID: withReserve.ID}}})
		// Резервного узла нет в каталоге Xray: fallbackTag — xcp-block.
		noReserve := save(Group{Name: "Резерв без узла", Mode: ModeFallback, Sources: GroupSources{All: true}, Reserve: "my/00000000"})
		e.takeAndApply(t)

		outs := rtxOutbounds(t, e.xrayDoc(t, xrayGroupsFile))
		for _, g := range []Group{inner, outer} {
			want, err := nodes.XrayOutbound(x.Node, GroupTag(g.ID))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rtxNorm(t, outs[GroupTag(g.ID)]), rtxNorm(t, want)) {
				t.Errorf("исходящий группы %s не равен копии выбранного узла под своим тегом", g.Name)
			}
		}
		if ob := outs[GroupTag(viaReserve.ID)]; ob == nil || ob["protocol"] != "loopback" ||
			ob["settings"].(map[string]any)["inboundTag"] != loopbackTagPrefix+viaReserve.ID {
			t.Errorf("группа, выбравшая «резерв»: %v (ожидался loopback)", ob)
		}

		routing := rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))
		bals := map[string]map[string]any{}
		for _, b := range routing["balancers"].([]any) {
			bm := b.(map[string]any)
			bals[bm["tag"].(string)] = bm
		}
		if b := bals[GroupTag(withReserve.ID)]; b == nil || b["fallbackTag"] != y.XrayTag {
			t.Errorf("«резерв» с узлом: %v (fallbackTag должен быть тегом резервного узла)", b)
		} else {
			var wantSel []string
			for _, tag := range rtxXrayTags(e, t) {
				if tag != y.XrayTag {
					wantSel = append(wantSel, tag)
				}
			}
			if sel := rtxStrings(b["selector"]); !rtxEqualStrings(sel, wantSel) {
				t.Errorf("selector «резерва» %v, ожидались основные узлы без резервного %v", sel, wantSel)
			}
		}
		if b := bals[GroupTag(noReserve.ID)]; b == nil || b["fallbackTag"] != TagBlock {
			t.Errorf("«резерв» без узла: %v (fallbackTag должен быть xcp-block)", b)
		}
		foundLoop := false
		for _, r := range rtxRules(t, routing) {
			if rtxEqualStrings(rtxStrings(r["inboundTag"]), []string{loopbackTagPrefix + viaReserve.ID}) && r["balancerTag"] == GroupTag(withReserve.ID) {
				foundLoop = true
			}
		}
		if !foundLoop {
			t.Error("нет правила loopback на balancer «резерва»")
		}
	})

	t.Run("empty_for_xray", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
		e.addMyLinks(t, "vless_ws")
		// Пустые для Xray группы блокируют «Применить» проверкой модели (они пусты
		// и для единственного установленного ядра), поэтому файлы берутся у
		// генератора напрямую и проверяются ядром без конвейера.
		const noMatch = "нет-такого-узла-xyz"
		_, m, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Пустая ручная", Mode: ModeManual, Sources: GroupSources{All: true}, Include: []string{noMatch}})
		if err != nil {
			t.Fatal(err)
		}
		_, a, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Пустая авто", Mode: ModeAuto, Sources: GroupSources{All: true}, Include: []string{noMatch}})
		if err != nil {
			t.Fatal(err)
		}
		for i, g := range []Group{m, a} {
			if _, _, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: Target{Kind: KindGroup, ID: g.ID},
				Conds: []Cond{{Type: CondDomain, Values: []string{"empty" + strconv.Itoa(i) + ".example.com"}}}}); err != nil {
				t.Fatalf("SaveRule: %v", err)
			}
		}
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		files, err := e.svc.Generators()[0].Generate(inputOf(e.state(t)), e.installed)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		byName := map[string][]byte{}
		for _, f := range files {
			byName[f.RelPath] = f.Content
		}
		var groupsDoc, routingDoc map[string]any
		if err := json.Unmarshal(byName[xrayGroupsFile], &groupsDoc); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(byName[xrayRoutingFile], &routingDoc); err != nil {
			t.Fatal(err)
		}
		if ob := rtxOutbounds(t, groupsDoc)[GroupTag(m.ID)]; ob == nil || ob["protocol"] != "blackhole" {
			t.Errorf("пустая ручная группа: %v (ожидался blackhole под тегом группы)", ob)
		}
		routing := rtxRouting(t, routingDoc)
		for _, b := range routing["balancers"].([]any) {
			if b.(map[string]any)["tag"] == GroupTag(a.ID) {
				t.Error("для пустой группы «авто» создан balancer")
			}
		}
		user := rtxUserRules(t, routing)
		if len(user) != 2 {
			t.Fatalf("правил пользователя %d, ожидалось 2", len(user))
		}
		for _, r := range user {
			if r["outboundTag"] != TagBlock || r["balancerTag"] != nil {
				t.Errorf("правило на пустую группу: %v (ожидался xcp-block)", r)
			}
		}
		rtxTestFiles(t, e, files)
	})

	t.Run("version_threshold", func(t *testing.T) {
		// Ниже 1.8.10 «авто» и «резерв» в Xray не создаются: цели — xcp-block,
		// «Применить» не блокируется, «Прокси» выбирает первый узел (D-09, D-42).
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly), rtWithDeps(func(d *Deps) {
			d.KernelVersion = func(kernel string) string {
				if kernel == configlayer.KernelXray {
					return "1.8.9"
				}
				return ""
			}
		}))
		e.addMyLinks(t, "vless_ws", "trojan_grpc")
		cfg := e.groups(t)
		auto, _ := rtGroupByName(cfg, "Авто")
		proxyG, _ := rtGroupByName(cfg, "Прокси")
		if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: auto.ID}); err != nil {
			t.Fatalf("SetFinal: %v", err)
		}
		e.takeAndApply(t)

		routing := rtxRouting(t, e.xrayDoc(t, xrayRoutingFile))
		if _, has := routing["balancers"]; has {
			t.Errorf("ниже 1.8.10 в routing есть balancers: %v", routing["balancers"])
		}
		rules := rtxRules(t, routing)
		if last := rules[len(rules)-1]; last["outboundTag"] != TagBlock || last["balancerTag"] != nil {
			t.Errorf("«Остальное» на «авто» ниже порога: %v (ожидался xcp-block)", last)
		}
		if e.xrayFileExists(xrayObservatoryFile) {
			t.Error("ниже 1.8.10 создан файл observatory")
		}
		ob := rtxOutbounds(t, e.xrayDoc(t, xrayGroupsFile))[GroupTag(proxyG.ID)]
		if ob == nil || ob["protocol"] == "loopback" || ob["protocol"] == "blackhole" {
			t.Errorf("«Прокси» ниже порога: %v (ожидалась копия первого узла)", ob)
		}
	})
}
