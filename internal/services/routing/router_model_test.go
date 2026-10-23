//go:build router

package routing

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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
}
