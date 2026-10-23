//go:build router

package routing

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// rtLinkNames — набор ссылок testdata/links для проверок состава: tuic умеет
// только Mihomo, остальные оба ядра.
var rtLinkNames = []string{"vless_ws", "trojan_grpc", "ss_plain", "vless_reality", "tuic"}

// rtGroupByName находит группу по имени.
func rtGroupByName(cfg GroupsConfig, name string) (Group, bool) {
	for _, g := range cfg.Items {
		if g.Name == name {
			return g, true
		}
	}
	return Group{}, false
}

// rtHasIssue ищет проблему с кодом и параметром.
func rtHasIssue(issues []configlayer.BuildIssue, code, param, value string) bool {
	for _, is := range issues {
		if is.Code == code && is.Params[param] == value {
			return true
		}
	}
	return false
}

// Группы по умолчанию появляются с первым источником одной ревизией и один раз;
// состав считает одна функция, предпросмотр даёт числа и имена по каждому ядру;
// узел tuic есть только в предпросмотре Mihomo, отдельный узел входит в группу
// мимо фильтров.
func TestRouterGroupsDefaultsAndResolve(t *testing.T) {
	e := rtNewEnv(t)

	// Источников нет — групп нет, «Применить» не блокируется даже при взятой
	// маршрутизации (нет групп — нет блока).
	if _, ok := e.state(t).Draft[GroupsSection]; ok {
		t.Fatal("секция groups есть до первого источника")
	}
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	if _, err := e.applyResult(t); err != nil {
		t.Fatalf("«Применить» без источников и групп заблокировано: %v", err)
	}

	// Первый Add: «Авто» и «Прокси» той же ревизией, что источник.
	before := e.rev(t)
	e.addMyLinks(t, rtLinkNames...)
	if after := e.rev(t); after != before+1 {
		t.Fatalf("первый Add изменил ревизию %d -> %d, ожидалось +1", before, after)
	}
	cfg := e.groups(t)
	if !cfg.DefaultsDone || len(cfg.Items) != 2 {
		t.Fatalf("группы по умолчанию: defaults_done=%v, групп %d", cfg.DefaultsDone, len(cfg.Items))
	}
	auto, okA := rtGroupByName(cfg, "Авто")
	proxy, okP := rtGroupByName(cfg, "Прокси")
	if !okA || !okP {
		t.Fatalf("нет групп «Авто» и «Прокси»: %+v", cfg.Items)
	}
	if auto.Mode != ModeAuto || !auto.Sources.All || len(auto.Members) != 0 {
		t.Errorf("«Авто» не режим auto по всем источникам: %+v", auto)
	}
	if proxy.Mode != ModeManual || !proxy.Sources.All ||
		len(proxy.Members) != 1 || proxy.Members[0].Kind != KindGroup || proxy.Members[0].ID != auto.ID {
		t.Errorf("«Прокси» не ручная с участником «Авто»: %+v", proxy)
	}
	if !idRE.MatchString(auto.ID) || !idRE.MatchString(proxy.ID) || auto.ID == proxy.ID {
		t.Errorf("ID групп не 8 hex или совпадают: %s, %s", auto.ID, proxy.ID)
	}

	// Второй Add группы не добавляет и секцию не меняет.
	rawBefore := string(e.state(t).Draft[GroupsSection])
	e.addMyLinks(t, "vless_grpc")
	if rawAfter := string(e.state(t).Draft[GroupsSection]); rawAfter != rawBefore {
		t.Errorf("второй Add изменил секцию groups")
	}

	// Удалённая «Авто» после нового источника не возвращается (defaults_done):
	// источники и «Авто» убраны одной правкой, затем добавлен новый источник.
	cfg = e.groups(t)
	kept := cfg.Items[:0:0]
	for _, g := range cfg.Items {
		if g.ID != auto.ID {
			g.Members = nil
			kept = append(kept, g)
		}
	}
	cfg.Items = kept
	raw, err := encodeGroups(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.layer.EditDraftBatch(e.rev(t), map[string]json.RawMessage{
		GroupsSection: raw, sources.MySection: nil,
	}, nil); err != nil {
		t.Fatalf("EditDraftBatch: %v", err)
	}
	e.addMyLinks(t, "vless_ws")
	cfg = e.groups(t)
	if _, back := rtGroupByName(cfg, "Авто"); back || len(cfg.Items) != 1 {
		t.Errorf("удалённая «Авто» вернулась или появились лишние группы: %d", len(cfg.Items))
	}
	// Возвращаем набор узлов для проверки состава.
	e.addMyLinks(t, rtLinkNames[1:]...)

	// Предпросмотр состава.
	cat := e.catalog(t)
	if len(cat.Xray) == 0 || len(cat.Mihomo) <= len(cat.Xray) {
		t.Fatalf("в данных нет узла только для Mihomo: xray=%d mihomo=%d", len(cat.Xray), len(cat.Mihomo))
	}
	all, err := e.svc.PreviewGroup(Group{Name: "Все", Mode: ModeAuto, Sources: GroupSources{All: true}})
	if err != nil {
		t.Fatalf("PreviewGroup: %v", err)
	}
	if all.Xray == nil || all.Mihomo == nil {
		t.Fatalf("предпросмотр без одного из установленных ядер: %+v", all)
	}
	if all.Xray.Count != len(cat.Xray) || all.Mihomo.Count != len(cat.Mihomo) ||
		len(all.Xray.Names) != all.Xray.Count || len(all.Mihomo.Names) != all.Mihomo.Count {
		t.Errorf("предпросмотр не равен каталогу: xray %d/%d, mihomo %d/%d",
			all.Xray.Count, len(cat.Xray), all.Mihomo.Count, len(cat.Mihomo))
	}
	if all.Mihomo.Count <= all.Xray.Count {
		t.Errorf("узел только для Mihomo не виден в предпросмотре: xray=%d mihomo=%d", all.Xray.Count, all.Mihomo.Count)
	}

	// Фильтр: «включить» в другом регистре, «исключить» по другому узлу.
	inc, exc := cat.Xray[0].Display, ""
	for _, n := range cat.Xray[1:] {
		if !strings.Contains(strings.ToLower(n.Display), strings.ToLower(inc)) {
			exc = n.Display
			break
		}
	}
	if exc == "" {
		t.Fatal("в данных нет узлов с разными именами")
	}
	wantMatches := func(list []sources.CatalogNode, with, without string) int {
		n := 0
		for _, c := range list {
			low := strings.ToLower(c.Display)
			if strings.Contains(low, strings.ToLower(with)) && (without == "" || !strings.Contains(low, strings.ToLower(without))) {
				n++
			}
		}
		return n
	}
	filtered, err := e.svc.PreviewGroup(Group{
		Name: "Фильтр", Mode: ModeAuto, Sources: GroupSources{All: true},
		Include: []string{strings.ToUpper(inc)}, Exclude: []string{strings.ToUpper(exc)},
	})
	if err != nil {
		t.Fatalf("PreviewGroup с фильтром: %v", err)
	}
	if w := wantMatches(cat.Xray, inc, exc); filtered.Xray.Count != w {
		t.Errorf("Xray: в составе %d узлов, ожидалось %d", filtered.Xray.Count, w)
	}
	if w := wantMatches(cat.Mihomo, inc, exc); filtered.Mihomo.Count != w {
		t.Errorf("Mihomo: в составе %d узлов, ожидалось %d", filtered.Mihomo.Count, w)
	}

	// Отдельный узел по ссылке входит мимо фильтров; узел только для Mihomo не
	// появляется в Xray и считается там устаревшим.
	var only sources.CatalogNode
	inXray := map[string]bool{}
	for _, n := range cat.Xray {
		inXray[n.Ref] = true
	}
	for _, n := range cat.Mihomo {
		if !inXray[n.Ref] {
			only = n
			break
		}
	}
	if only.Ref == "" {
		t.Fatal("нет узла только для Mihomo")
	}
	picked, err := e.svc.PreviewGroup(Group{
		Name: "Отдельный", Mode: ModeAuto, Sources: GroupSources{},
		Include: []string{"нет-такого-имени"}, Picks: []string{cat.Xray[0].Ref, only.Ref},
	})
	if err != nil {
		t.Fatalf("PreviewGroup с отдельными узлами: %v", err)
	}
	if picked.Xray.Count != 1 || picked.Mihomo.Count != 2 {
		t.Errorf("отдельные узлы: xray=%d (ожидался 1), mihomo=%d (ожидалось 2)", picked.Xray.Count, picked.Mihomo.Count)
	}
	if r := Resolve(Group{Mode: ModeAuto, Picks: []string{only.Ref}}, cat.Xray, nil); len(r.Nodes) != 0 || len(r.Stale) != 1 || r.Stale[0] != only.Ref {
		t.Errorf("узел только для Mihomo в Xray: узлов %d, устаревших %v", len(r.Nodes), r.Stale)
	}

	// Выражения: плохое отвергается, регистр учитывается.
	if _, err := e.svc.PreviewGroup(Group{Name: "Рег", Mode: ModeAuto, Regex: true, Include: []string{"("}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("плохое выражение принято предпросмотром: %v", err)
	}
}

// Пустая для всех установленных ядер группа блокирует «Применить» при взятой
// маршрутизации; после отказа от неё (Release) «Применить только узлы» проходит.
func TestRouterGroupEmptyBlocksApply(t *testing.T) {
	e := rtNewEnv(t)
	e.addMyLinks(t, rtLinkNames...)
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	if rules := e.rules(t); len(rules.Items) != 0 || rules.Final.Kind != KindDirect {
		t.Errorf("Take не создал «Остальное → DIRECT» в пустой секции правил: %+v", rules)
	}

	// Исключаем все узлы по именам: группа пуста для обоих ядер.
	cat := e.catalog(t)
	var exclude []string
	seen := map[string]bool{}
	for _, n := range cat.Mihomo {
		if low := strings.ToLower(n.Display); !seen[low] {
			seen[low] = true
			exclude = append(exclude, n.Display)
		}
	}
	_, saved, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Пустая", Mode: ModeAuto, Sources: GroupSources{All: true}, Exclude: exclude,
	})
	if err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if !idRE.MatchString(saved.ID) {
		t.Errorf("новой группе не назначен ID: %q", saved.ID)
	}
	if issues := e.check(t); !rtHasIssue(issues, "group_empty", "group", "Пустая") || len(issues) != 1 {
		t.Fatalf("проверка при пустой группе: %+v", issues)
	}
	_, err = e.applyResult(t)
	var ce *configlayer.ChecksError
	if !errors.Is(err, configlayer.ErrChecksFailed) || !errors.As(err, &ce) || !rtHasIssue(ce.Issues, "group_empty", "group", "Пустая") {
		t.Fatalf("«Применить» не заблокировано пустой группой: %v", err)
	}

	// «Применить только узлы»: после Release проверка ничего не блокирует.
	if _, err := e.svc.Release(e.rev(t)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("проверка блокирует при managed=false: %+v", issues)
	}
	if res, err := e.applyResult(t); err != nil || res == nil || res.Code != configlayer.ResultApplied {
		t.Fatalf("«Применить только узлы» не прошло: %v, %+v", err, res)
	}

	// Группа, пустая только для одного ядра, не блокирует «Применить» (D-13):
	// её единственный узел есть только в Mihomo.
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	e.replaceGroups(t, func(cfg *GroupsConfig) {
		kept := cfg.Items[:0:0]
		for _, g := range cfg.Items {
			if g.Name != "Пустая" {
				kept = append(kept, g)
			}
		}
		cfg.Items = kept
	})
	cat = e.catalog(t)
	inXray := map[string]bool{}
	for _, n := range cat.Xray {
		inXray[n.Ref] = true
	}
	var onlyRef string
	for _, n := range cat.Mihomo {
		if !inXray[n.Ref] {
			onlyRef = n.Ref
			break
		}
	}
	if onlyRef == "" {
		t.Fatal("нет узла только для Mihomo")
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Только Mihomo", Mode: ModeAuto, Picks: []string{onlyRef}}); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("группа, пустая для одного ядра, блокирует: %+v", issues)
	}
	if res, err := e.applyResult(t); err != nil || res == nil || !res.OK {
		t.Fatalf("«Применить» с группой, пустой для одного ядра: %v, %+v", err, res)
	}

	// Цикл, записанный в обход сервиса общим маршрутом черновика.
	e.replaceGroups(t, func(cfg *GroupsConfig) {
		cfg.Items = append(cfg.Items,
			Group{ID: "c0ffee01", Name: "Цикл А", Mode: ModeManual, Sources: GroupSources{All: true}, Members: []Member{{Kind: KindGroup, ID: "c0ffee02"}}},
			Group{ID: "c0ffee02", Name: "Цикл Б", Mode: ModeManual, Sources: GroupSources{All: true}, Members: []Member{{Kind: KindGroup, ID: "c0ffee01"}}},
		)
	})
	issues := e.check(t)
	if !rtHasIssue(issues, "group_cycle", "group", "Цикл А") || !rtHasIssue(issues, "group_cycle", "group", "Цикл Б") || len(issues) != 2 {
		t.Errorf("цикл вложенности: %+v", issues)
	}
	if _, err := e.applyResult(t); !errors.Is(err, configlayer.ErrChecksFailed) {
		t.Errorf("«Применить» при цикле: %v", err)
	}
	e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = cfg.Items[:len(cfg.Items)-2] })

	// Цель правила и «Остального» — несуществующая группа (висячая ссылка).
	dangling, err := encodeRules(RulesConfig{
		Items: []Rule{{ID: "deadbeef", Enabled: true, Conds: []Cond{{Type: CondDomain, Values: []string{"example.com"}}}, Target: Target{Kind: KindGroup, ID: "00000000"}}},
		Final: Target{Kind: KindGroup, ID: "00000000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, RulesSection, dangling)
	issues = e.check(t)
	if !rtHasIssue(issues, "target_missing", "rule", "1") || !rtHasIssue(issues, "target_missing", "rule", "final") || len(issues) != 2 {
		t.Errorf("висячая цель: %+v", issues)
	}
	e.dropSection(t, RulesSection)

	// Испорченная секция не роняет слой: проверка и снимок дают section_invalid.
	junk := map[string]json.RawMessage{
		GroupsSection: json.RawMessage(`"junk"`),
		RulesSection:  json.RawMessage(`{"v":7,"items":[]}`),
		ProbeSection:  json.RawMessage(`{"v":1,"url":"https://example.com/x","interval_s":1,"timeout_s":5}`),
	}
	for name, raw := range junk {
		e.putSection(t, name, raw)
		if issues := e.check(t); !rtHasIssue(issues, "section_invalid", "section", name) {
			t.Errorf("секция %s с мусором: проверка %+v", name, issues)
		}
		if snap := e.layer.Snapshot().Checks; !rtHasIssue(snap, "section_invalid", "section", name) {
			t.Errorf("секция %s с мусором: Snapshot().Checks %+v", name, snap)
		}
		if _, err := e.applyResult(t); !errors.Is(err, configlayer.ErrChecksFailed) {
			t.Errorf("«Применить» с испорченной секцией %s: %v", name, err)
		}
		e.dropSection(t, name)
	}
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("после очистки секций остались проблемы: %+v", issues)
	}
}

// rtWantInvalid требует ErrInvalidInput с полем и причиной.
func rtWantInvalid(t *testing.T, label string, err error, field, reason string) {
	t.Helper()
	var ie *InvalidInputError
	if !errors.Is(err, ErrInvalidInput) || !errors.As(err, &ie) {
		t.Errorf("%s: ожидалась ErrInvalidInput, получено %v", label, err)
		return
	}
	if ie.Field != field || ie.Reason != reason {
		t.Errorf("%s: поле %q причина %q, ожидалось %q %q", label, ie.Field, ie.Reason, field, reason)
	}
}

// Модель правил и групп: по одному отказу на ветку проверки, нормализация
// значений, пометка «из пресета» (правка снимает, выключение и перемещение —
// нет), порядок правил с неподвижным «Остальным», параметры проверки узлов.
func TestRouterRulesModel(t *testing.T) {
	e := rtNewEnv(t)
	tg := routertest.Current(t)

	// Параметры проверки по умолчанию (D-12): интервал зависит от архитектуры.
	def, err := e.svc.Probe()
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	wantInterval := 30
	if strings.HasPrefix(tg.Arch, "mips") {
		wantInterval = 120
	}
	if def.URL != "https://www.gstatic.com/generate_204" || def.IntervalS != wantInterval || def.TimeoutS != 5 {
		t.Errorf("параметры проверки по умолчанию на %s: %+v", tg.Arch, def)
	}

	e.addMyLinks(t, "vless_ws", "trojan_grpc")
	proxy, ok := rtGroupByName(e.groups(t), "Прокси")
	if !ok {
		t.Fatal("нет группы «Прокси»")
	}
	auto, _ := rtGroupByName(e.groups(t), "Авто")
	toProxy := Target{Kind: KindGroup, ID: proxy.ID}

	// --- правила: отказы по веткам проверки ---
	domainCond := func(vals ...string) []Cond { return []Cond{{Type: CondDomain, Values: vals}} }
	nine := make([]Cond, 9)
	for i := range nine {
		nine[i] = Cond{Type: CondBittorrent}
	}
	siteList := []lists.ListRef{{File: "geosite.dat", Tag: "CN"}}
	cases := []struct {
		name          string
		rule          Rule
		field, reason string
	}{
		{"без условий", Rule{Target: toProxy}, "conds", reasonCondsRequired},
		{"девять условий", Rule{Conds: nine, Target: toProxy}, "conds", reasonCondsTooMany},
		{"пустое значение", Rule{Conds: domainCond("  "), Target: toProxy}, "conds.0.values", reasonValuesRequired},
		{"домен с запятой", Rule{Conds: domainCond("a,b.com"), Target: toProxy}, "conds.0.values", reasonDomainInvalid},
		{"домен с двоеточием", Rule{Conds: domainCond("geosite:x"), Target: toProxy}, "conds.0.values", reasonDomainInvalid},
		{"плохое регулярное выражение", Rule{Conds: domainCond("regexp:("), Target: toProxy}, "conds.0.values", reasonRegexpInvalid},
		{"geosite без списков", Rule{Conds: []Cond{{Type: CondGeosite}}, Target: toProxy}, "conds.0.lists", reasonListsRequired},
		{"geosite с плохим файлом", Rule{Conds: []Cond{{Type: CondGeosite, Lists: []lists.ListRef{{File: "x.txt", Tag: "CN"}}}}, Target: toProxy}, "conds.0.lists", reasonListInvalid},
		{"geoip со списком другого вида", Rule{Conds: []Cond{{Type: CondGeoip, Lists: []lists.ListRef{{File: "geoip.dat", Tag: "RU", Kind: lists.KindSite}}}}, Target: toProxy}, "conds.0.lists", reasonListInvalid},
		{"geosite со значениями", Rule{Conds: []Cond{{Type: CondGeosite, Values: []string{"a.com"}, Lists: siteList}}, Target: toProxy}, "conds.0.values", reasonValuesForbid},
		{"домен со списками", Rule{Conds: []Cond{{Type: CondDomain, Values: []string{"a.com"}, Lists: siteList}}, Target: toProxy}, "conds.0.lists", reasonListsForbid},
		{"плохой IP", Rule{Conds: []Cond{{Type: CondIP, Values: []string{"999.1.1.1"}}}, Target: toProxy}, "conds.0.values", reasonIPInvalid},
		{"плохой IP источника", Rule{Conds: []Cond{{Type: CondSrcIP, Values: []string{"10.0.0.0/33"}}}, Target: toProxy}, "conds.0.values", reasonIPInvalid},
		{"порт 0", Rule{Conds: []Cond{{Type: CondPort, Values: []string{"0"}}}, Target: toProxy}, "conds.0.values", reasonPortInvalid},
		{"порт 70000", Rule{Conds: []Cond{{Type: CondPort, Values: []string{"70000"}}}, Target: toProxy}, "conds.0.values", reasonPortInvalid},
		{"диапазон портов наоборот", Rule{Conds: []Cond{{Type: CondPort, Values: []string{"5-3"}}}, Target: toProxy}, "conds.0.values", reasonPortInvalid},
		{"сеть icmp", Rule{Conds: []Cond{{Type: CondNetwork, Values: []string{"icmp"}}}, Target: toProxy}, "conds.0.values", reasonNetInvalid},
		{"bittorrent со значениями", Rule{Conds: []Cond{{Type: CondBittorrent, Values: []string{"x"}}}, Target: toProxy}, "conds.0.values", reasonValuesForbid},
		{"неизвестный тип условия", Rule{Conds: []Cond{{Type: "geoip2", Values: []string{"x"}}}, Target: toProxy}, "conds.0.type", reasonCondType},
		{"ошибка во втором условии", Rule{Conds: append(domainCond("a.com"), Cond{Type: CondPort, Values: []string{"x"}}), Target: toProxy}, "conds.1.values", reasonPortInvalid},
		{"цель — несуществующая группа", Rule{Conds: domainCond("a.com"), Target: Target{Kind: KindGroup, ID: "00000000"}}, "target", reasonTargetInvalid},
		{"цель — неизвестный вид", Rule{Conds: domainCond("a.com"), Target: Target{Kind: "node", ID: "x"}}, "target", reasonTargetInvalid},
		{"raw: и условия, и правило", Rule{Conds: domainCond("a.com"), Raw: &RawRule{Kernel: "mihomo", Text: "DOMAIN,a.com"}, Target: toProxy}, "raw", reasonRawWithConds},
		{"raw: неизвестное ядро", Rule{Raw: &RawRule{Kernel: "singbox", Text: "x"}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw xray: неизвестное поле", Rule{Raw: &RawRule{Kernel: "xray", Text: `{"domain":["a.com"],"hack":1}`}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw xray: type не field", Rule{Raw: &RawRule{Kernel: "xray", Text: `{"type":"chinaip","domain":["a.com"]}`}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw xray: без условий", Rule{Raw: &RawRule{Kernel: "xray", Text: `{"type":"field","outboundTag":"direct"}`}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw xray: geosite", Rule{Raw: &RawRule{Kernel: "xray", Text: `{"domain":["GeoSite:cn"]}`}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw xray: два объекта", Rule{Raw: &RawRule{Kernel: "xray", Text: `{"domain":["a.com"]}{"ip":["1.1.1.1"]}`}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw xray: не объект", Rule{Raw: &RawRule{Kernel: "xray", Text: `["a.com"]`}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw mihomo: MATCH", Rule{Raw: &RawRule{Kernel: "mihomo", Text: "MATCH,DIRECT"}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw mihomo: две строки", Rule{Raw: &RawRule{Kernel: "mihomo", Text: "DOMAIN,a.com\nDOMAIN,b.com"}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw mihomo: GEOSITE", Rule{Raw: &RawRule{Kernel: "mihomo", Text: "GEOSITE,cn"}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw mihomo: GEOIP внутри AND", Rule{Raw: &RawRule{Kernel: "mihomo", Text: "AND,((GEOIP,RU),(NETWORK,TCP))"}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw mihomo: скобки", Rule{Raw: &RawRule{Kernel: "mihomo", Text: "AND,((NETWORK,TCP)"}, Target: toProxy}, "raw", reasonRawInvalid},
		{"raw mihomo: неизвестный тип", Rule{Raw: &RawRule{Kernel: "mihomo", Text: "FOO,a.com"}, Target: toProxy}, "raw", reasonRawInvalid},
	}
	for _, c := range cases {
		_, _, err := e.svc.SaveRule(e.rev(t), c.rule)
		rtWantInvalid(t, c.name, err, c.field, c.reason)
	}
	if n := len(e.rules(t).Items); n != 0 {
		t.Fatalf("отвергнутые правила попали в черновик: %d", n)
	}

	// --- правила: нормализация принятых значений ---
	_, norm, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Origin: OriginPreset, Target: toProxy,
		Conds: []Cond{
			{Type: CondDomain, Values: []string{"Example.COM", "example.com", "пример.рф", "full:Sub.Example.com", "keyword:Ads", "regexp:^a+\\.b$"}},
			{Type: CondNetwork, Values: []string{"UDP", "tcp"}},
			{Type: CondPort, Values: []string{"443", "8000-9000"}},
			{Type: CondIP, Values: []string{"10.0.0.5/8", "1.1.1.1"}},
			{Type: CondGeosite, Lists: []lists.ListRef{{File: "geosite_zkeen.dat", Tag: "YOUTUBE"}}},
			{Type: CondBittorrent},
		},
	})
	if err != nil {
		t.Fatalf("SaveRule с допустимыми значениями: %v", err)
	}
	if norm.Origin != "" || !idRE.MatchString(norm.ID) {
		t.Errorf("новое правило: origin=%q id=%q (пометку «из пресета» через SaveRule поставить нельзя)", norm.Origin, norm.ID)
	}
	wantVals := []string{"example.com", "xn--e1afmkfd.xn--p1ai", "full:sub.example.com", "keyword:ads", "regexp:^a+\\.b$"}
	if strings.Join(norm.Conds[0].Values, "|") != strings.Join(wantVals, "|") {
		t.Errorf("значения домена: %v", norm.Conds[0].Values)
	}
	if strings.Join(norm.Conds[1].Values, "|") != "tcp|udp" || strings.Join(norm.Conds[3].Values, "|") != "10.0.0.0/8|1.1.1.1" ||
		norm.Conds[4].Lists[0].Kind != lists.KindSite {
		t.Errorf("нормализация сети, IP или списка: %+v", norm.Conds)
	}
	rawRule := Rule{Enabled: true, Target: Target{Kind: KindDirect, ID: "лишний"}, Raw: &RawRule{
		Kernel: "xray", Text: `{ "type": "field", "domain": ["domain:example.com"], "outboundTag": "proxy", "balancerTag": "b" }`}}
	_, rawSaved, err := e.svc.SaveRule(e.rev(t), rawRule)
	if err != nil {
		t.Fatalf("SaveRule со своим правилом Xray: %v", err)
	}
	if strings.Contains(rawSaved.Raw.Text, "outboundTag") || strings.Contains(rawSaved.Raw.Text, "balancerTag") || rawSaved.Target.ID != "" {
		t.Errorf("своё правило Xray оставило цель: %+v", rawSaved)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{Enabled: true, Target: Target{Kind: KindReject}, Raw: &RawRule{Kernel: "mihomo", Text: " DOMAIN-SUFFIX,example.com "}}); err != nil {
		t.Errorf("SaveRule со своим правилом Mihomo: %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t), Rule{ID: "00000000", Conds: domainCond("a.com"), Target: toProxy}); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("правка несуществующего правила: %v", err)
	}
	if _, _, err := e.svc.SaveRule(e.rev(t)-1, Rule{Conds: domainCond("a.com"), Target: toProxy}); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("устаревшая ревизия: %v", err)
	}

	// --- пометка «из пресета» (D-24) ---
	cfg := e.rules(t)
	preset := cfg.Items[0]
	for i := range cfg.Items {
		cfg.Items[i].Origin = OriginPreset
	}
	raw, err := encodeRules(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.putSection(t, RulesSection, raw)
	if _, err := e.svc.SetRuleEnabled(e.rev(t), preset.ID, false); err != nil {
		t.Fatalf("SetRuleEnabled: %v", err)
	}
	if r := e.rules(t).Items[0]; r.Enabled || r.Origin != OriginPreset {
		t.Errorf("выключение: enabled=%v origin=%q, пометка должна остаться", r.Enabled, r.Origin)
	}
	same := e.rules(t).Items[0]
	same.Enabled = true
	if _, saved, err := e.svc.SaveRule(e.rev(t), same); err != nil || saved.Origin != OriginPreset {
		t.Errorf("сохранение без правки условий: origin=%q err=%v", saved.Origin, err)
	}
	if _, err := e.svc.MoveRule(e.rev(t), preset.ID, 2); err != nil {
		t.Fatalf("MoveRule: %v", err)
	}
	if got := e.rules(t); got.Items[2].ID != preset.ID || got.Items[2].Origin != OriginPreset {
		t.Errorf("перемещение: правило не на месте или потеряло пометку: %+v", got.Items[2])
	}
	edited := e.rules(t).Items[2]
	edited.Conds = domainCond("other.example.com")
	if _, saved, err := e.svc.SaveRule(e.rev(t), edited); err != nil || saved.Origin != "" {
		t.Errorf("правка условия: origin=%q err=%v, пометка должна сняться", saved.Origin, err)
	}
	if o := e.rules(t).Items[2].Origin; o != "" {
		t.Errorf("пометка осталась в черновике: %q", o)
	}
	if got := e.rules(t).Items[0].Origin; got != OriginPreset {
		t.Errorf("правка одного правила сняла пометку у другого: %q", got)
	}

	// --- порядок и «Остальное» ---
	before := e.rules(t)
	ids := make([]string, len(before.Items))
	for i, r := range before.Items {
		ids[i] = r.ID
	}
	if _, err := e.svc.MoveRule(e.rev(t), ids[2], 0); err != nil {
		t.Fatalf("MoveRule вверх: %v", err)
	}
	after := e.rules(t)
	if after.Items[0].ID != ids[2] || after.Items[1].ID != ids[0] || after.Items[2].ID != ids[1] {
		t.Errorf("порядок после перемещения: %v", []string{after.Items[0].ID, after.Items[1].ID, after.Items[2].ID})
	}
	if after.Final != before.Final {
		t.Errorf("перемещение тронуло «Остальное»: %+v -> %+v", before.Final, after.Final)
	}
	_, err = e.svc.MoveRule(e.rev(t), ids[0], 3)
	rtWantInvalid(t, "MoveRule за пределы", err, "to", reasonOutOfRange)
	if _, err := e.svc.MoveRule(e.rev(t), "00000000", 0); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("перемещение несуществующего правила: %v", err)
	}
	if _, err := e.svc.DeleteRule(e.rev(t), ids[1]); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if n := len(e.rules(t).Items); n != 2 {
		t.Errorf("после удаления правил %d, ожидалось 2", n)
	}
	if _, err := e.svc.DeleteRule(e.rev(t), ids[1]); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("повторное удаление: %v", err)
	}
	_, err = e.svc.SetFinal(e.rev(t), Target{Kind: KindGroup, ID: "00000000"})
	rtWantInvalid(t, "SetFinal на несуществующую группу", err, "final", reasonTargetInvalid)
	if _, err := e.svc.SetFinal(e.rev(t), toProxy); err != nil {
		t.Fatalf("SetFinal на группу: %v", err)
	}
	if f := e.rules(t).Final; f != toProxy {
		t.Errorf("«Остальное»: %+v", f)
	}
	if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindReject, ID: "x"}); err != nil || e.rules(t).Final != (Target{Kind: KindReject}) {
		t.Errorf("«Остальное» на REJECT: %v, %+v", err, e.rules(t).Final)
	}

	// --- группы: отказы по веткам проверки ---
	good := Group{Name: "Рабочая", Mode: ModeManual, Sources: GroupSources{All: true}}
	with := func(f func(*Group)) Group {
		g := good
		f(&g)
		return g
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = "v" + strconv.Itoa(i)
	}
	groupCases := []struct {
		name          string
		group         Group
		field, reason string
	}{
		{"пустое имя", with(func(g *Group) { g.Name = "" }), "name", reasonNameEmpty},
		{"длинное имя", with(func(g *Group) { g.Name = strings.Repeat("я", 65) }), "name", reasonNameTooLong},
		{"запятая в имени", with(func(g *Group) { g.Name = "а,б" }), "name", reasonNameChars},
		{"скобка в имени", with(func(g *Group) { g.Name = "а(б" }), "name", reasonNameChars},
		{"управляющий символ", with(func(g *Group) { g.Name = "а\tб" }), "name", reasonNameChars},
		{"пробел по краям", with(func(g *Group) { g.Name = " ab" }), "name", reasonNameEdges},
		{"зарезервированное имя", with(func(g *Group) { g.Name = "Direct" }), "name", reasonNameReserved},
		{"неизвестный режим", with(func(g *Group) { g.Mode = "weird" }), "mode", reasonModeInvalid},
		{"слишком много значений фильтра", with(func(g *Group) { g.Include = many }), "include", reasonFilterTooMany},
		{"длинное значение фильтра", with(func(g *Group) { g.Exclude = []string{strings.Repeat("a", 65)} }), "exclude", reasonFilterTooLong},
		{"обратный апостроф", with(func(g *Group) { g.Include = []string{"a`b"} }), "include", reasonFilterBacktick},
		{"плохое выражение", with(func(g *Group) { g.Regex, g.Include = true, []string{"(("} }), "include", reasonRegexInvalid},
		{"апостроф в выражении", with(func(g *Group) { g.Regex, g.Exclude = true, []string{"a`b"} }), "exclude", reasonRegexBacktick},
		{"длинное выражение", with(func(g *Group) { g.Regex, g.Include = true, []string{strings.Repeat("a", 257)} }), "include", reasonFilterTooLong},
		{"несуществующий источник", with(func(g *Group) { g.Sources = GroupSources{IDs: []string{"deadbeef"}} }), "sources.ids", reasonSourceMissing},
		{"плохая ссылка на узел", with(func(g *Group) { g.Picks = []string{"my/zz"} }), "picks", reasonPickInvalid},
		{"участники у авто", with(func(g *Group) { g.Mode, g.Members = ModeAuto, []Member{{Kind: KindDirect}} }), "members", reasonMembersManual},
		{"неизвестный вид участника", with(func(g *Group) { g.Members = []Member{{Kind: "node", ID: "x"}} }), "members", reasonMemberKind},
		{"несуществующая группа-участник", with(func(g *Group) { g.Members = []Member{{Kind: KindGroup, ID: "00000000"}} }), "members", reasonMemberMissing},
		{"резерв без резервного узла", with(func(g *Group) { g.Mode = ModeFallback }), "reserve", reasonReserveNeeded},
		{"плохой резервный узел", with(func(g *Group) { g.Mode, g.Reserve = ModeFallback, "abc" }), "reserve", reasonReserveInvalid},
		{"резервный узел у ручной", with(func(g *Group) { g.Reserve = "my/00000001" }), "reserve", reasonReserveMode},
		{"участник — сама группа", with(func(g *Group) {
			g.ID, g.Name, g.Members = proxy.ID, "Прокси", []Member{{Kind: KindGroup, ID: proxy.ID}}
		}), "members", reasonMemberSelf},
	}
	for _, c := range groupCases {
		_, _, err := e.svc.SaveGroup(e.rev(t), c.group)
		rtWantInvalid(t, c.name, err, c.field, c.reason)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), with(func(g *Group) { g.Name = "ПРОКСИ" })); !errors.Is(err, ErrNameTaken) {
		t.Errorf("имя без учёта регистра: %v", err)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), with(func(g *Group) { g.ID = "00000000" })); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("правка несуществующей группы: %v", err)
	}
	if n := len(e.groups(t).Items); n != 2 {
		t.Fatalf("отвергнутые группы попали в черновик: %d", n)
	}

	// Допустимая группа: значения нормализуются, ID назначается.
	cat := e.catalog(t)
	_, nested, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Вложенная", Mode: ModeManual, Sources: GroupSources{All: true},
		Include: []string{" a ", "a", ""}, Members: []Member{
			{Kind: KindGroup, ID: proxy.ID}, {Kind: KindGroup, ID: proxy.ID}, {Kind: KindDirect, ID: "x"}, {Kind: KindReject}},
	})
	if err != nil {
		t.Fatalf("SaveGroup допустимой группы: %v", err)
	}
	if len(nested.Include) != 1 || nested.Include[0] != "a" || len(nested.Members) != 3 || nested.Members[1].ID != "" {
		t.Errorf("нормализация группы: include=%v members=%+v", nested.Include, nested.Members)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{
		Name: "Резервная", Mode: ModeFallback, Sources: GroupSources{All: true}, Reserve: cat.Xray[0].Ref,
	}); err != nil {
		t.Errorf("группа «резерв» с резервным узлом: %v", err)
	}
	// Ручная группа вкладывает «авто»; «авто» участников не принимает.
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Вложение авто", Mode: ModeManual, Sources: GroupSources{All: true}, Members: []Member{{Kind: KindGroup, ID: auto.ID}}}); err != nil {
		t.Errorf("ручная группа с вложенной «авто»: %v", err)
	}

	// Цикл: «Вложенная» содержит «Прокси», значит «Прокси» не может содержать «Вложенную».
	proxyNow, _ := rtGroupByName(e.groups(t), "Прокси")
	proxyNow.Members = append(proxyNow.Members, Member{Kind: KindGroup, ID: nested.ID})
	_, _, err = e.svc.SaveGroup(e.rev(t), proxyNow)
	var cyc *GroupCycleError
	if !errors.Is(err, ErrGroupCycle) || !errors.As(err, &cyc) || cyc.Group != "Прокси" {
		t.Errorf("цикл вложенности не отвергнут: %v", err)
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t)-1, good); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("устаревшая ревизия при сохранении группы: %v", err)
	}

	// --- параметры проверки узлов ---
	probeCases := []struct {
		name  string
		probe ProbeConfig
		field string
	}{
		{"таймаут равен интервалу", ProbeConfig{URL: def.URL, IntervalS: 30, TimeoutS: 30}, "timeout_s"},
		{"таймаут больше интервала", ProbeConfig{URL: def.URL, IntervalS: 30, TimeoutS: 60}, "timeout_s"},
		{"нулевой таймаут", ProbeConfig{URL: def.URL, IntervalS: 30, TimeoutS: 0}, "timeout_s"},
		{"интервал меньше 10 с", ProbeConfig{URL: def.URL, IntervalS: 5, TimeoutS: 2}, "interval_s"},
		{"интервал больше 3600 с", ProbeConfig{URL: def.URL, IntervalS: 4000, TimeoutS: 5}, "interval_s"},
		{"не http", ProbeConfig{URL: "ftp://example.com/x", IntervalS: 30, TimeoutS: 5}, "url"},
		{"адрес без хоста", ProbeConfig{URL: "https://", IntervalS: 30, TimeoutS: 5}, "url"},
		{"адрес с пробелом", ProbeConfig{URL: "https://example.com/a b", IntervalS: 30, TimeoutS: 5}, "url"},
	}
	for _, c := range probeCases {
		_, err := e.svc.SaveProbe(e.rev(t), c.probe)
		rtWantInvalid(t, c.name, err, c.field, reasonProbeInvalid)
	}
	want := ProbeConfig{V: 1, URL: "https://example.com/generate_204", IntervalS: 60, TimeoutS: 5}
	if _, err := e.svc.SaveProbe(e.rev(t), ProbeConfig{URL: want.URL, IntervalS: want.IntervalS, TimeoutS: want.TimeoutS}); err != nil {
		t.Fatalf("SaveProbe допустимых значений: %v", err)
	}
	if got, err := e.svc.Probe(); err != nil || got != want {
		t.Errorf("параметры проверки после сохранения: %+v, %v", got, err)
	}
}
