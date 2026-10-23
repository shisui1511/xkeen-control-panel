//go:build router

package routing

import (
	"encoding/json"
	"strings"
	"testing"
)

// view читает представление маршрутизации.
func (e *rtEnv) view(t *testing.T) RoutingView {
	t.Helper()
	v, err := e.svc.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	return v
}

// rtViewGroup находит группу в представлении по имени.
func rtViewGroup(t *testing.T, v RoutingView, name string) GroupView {
	t.Helper()
	for _, g := range v.Groups {
		if g.Name == name {
			return g
		}
	}
	t.Fatalf("в представлении нет группы %q", name)
	return GroupView{}
}

// rtViewHas ищет в списке проблему с кодом и параметрами (пары ключ, значение).
func rtViewHas(list []IssueView, code string, kv ...string) bool {
	for _, is := range list {
		if is.Code != code {
			continue
		}
		ok := true
		for i := 0; i+1 < len(kv); i += 2 {
			if is.Params[kv[i]] != kv[i+1] {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Представление маршрутизации: группы со сводкой по ядрам, проблемы и
// предупреждения тем же расчётом, что у проверки до записи (D-01, D-13).
// Ядро не читается и не переключается — метки ядра нет.
func TestRouterRoutingView(t *testing.T) {
	e := rtNewEnv(t)
	e.addMyLinks(t, rtLinkNames...)

	// Группы по умолчанию и сводка по ядрам: tuic есть только в Mihomo.
	v := e.view(t)
	cat := e.catalog(t)
	if v.Revision != e.rev(t) || !v.HasSources || !v.DefaultsDone || v.Desired || !v.Installed["xray"] || !v.Installed["mihomo"] {
		t.Errorf("шапка представления: %+v", v)
	}
	auto := rtViewGroup(t, v, "Авто")
	if auto.Counts.Xray == nil || auto.Counts.Mihomo == nil ||
		*auto.Counts.Xray != len(cat.Xray) || *auto.Counts.Mihomo != len(cat.Mihomo) || *auto.Counts.Mihomo <= *auto.Counts.Xray {
		t.Errorf("сводка группы «Авто»: %+v, каталог xray=%d mihomo=%d", auto.Counts, len(cat.Xray), len(cat.Mihomo))
	}
	if len(auto.Issues) != 0 || len(auto.Warnings) != 0 || len(v.Issues) != 0 {
		t.Errorf("группы по умолчанию с проблемами: %+v / %+v / %+v", auto.Issues, auto.Warnings, v.Issues)
	}

	// managed=false: группа, пустая для обоих ядер, видна в Issues представления
	// (клиент покажет её предупреждением), а проверка до записи ничего не блокирует.
	var exclude []string
	seen := map[string]bool{}
	for _, n := range cat.Mihomo {
		if low := strings.ToLower(n.Display); !seen[low] {
			seen[low] = true
			exclude = append(exclude, n.Display)
		}
	}
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Пустая", Mode: ModeAuto, Sources: GroupSources{All: true}, Exclude: exclude}); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	v = e.view(t)
	empty := rtViewGroup(t, v, "Пустая")
	if !rtViewHas(v.Issues, "group_empty", "group", "Пустая") || !rtViewHas(empty.Issues, "group_empty", "group", "Пустая") {
		t.Errorf("представление не показывает пустую группу: общие %+v, группы %+v", v.Issues, empty.Issues)
	}
	if empty.Counts.Xray == nil || *empty.Counts.Xray != 0 || empty.Counts.Mihomo == nil || *empty.Counts.Mihomo != 0 {
		t.Errorf("сводка пустой группы: %+v", empty.Counts)
	}
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("проверка блокирует при managed=false: %+v", issues)
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

	// Группа, пустая только для Xray (D-13): предупреждение с ядром, «Применить»
	// не блокируется.
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
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Только tuic", Mode: ModeAuto, Picks: []string{onlyRef}}); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	if _, err := e.svc.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	v = e.view(t)
	only := rtViewGroup(t, v, "Только tuic")
	if !v.Desired {
		t.Errorf("после Take Desired=false")
	}
	if only.Counts.Xray == nil || *only.Counts.Xray != 0 || only.Counts.Mihomo == nil || *only.Counts.Mihomo != 1 {
		t.Errorf("сводка «Только tuic»: %+v", only.Counts)
	}
	if !rtViewHas(only.Warnings, "group_empty_kernel", "group", "Только tuic", "kernel", "xray") || len(only.Warnings) != 1 || len(only.Issues) != 0 {
		t.Errorf("«Только tuic»: предупреждения %+v, проблемы %+v", only.Warnings, only.Issues)
	}
	if issues := e.check(t); len(issues) != 0 {
		t.Errorf("группа, пустая для одного ядра, блокирует: %+v", issues)
	}
	if res, err := e.applyResult(t); err != nil || res == nil || !res.OK {
		t.Fatalf("«Применить» с группой, пустой для одного ядра: %v, %+v", err, res)
	}

	// В JSON нет null: срезы пустые, а не отсутствующие.
	raw, err := json.Marshal(e.view(t))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if js := string(raw); strings.Contains(js, "null") || !strings.Contains(js, `"rules":[]`) {
		t.Errorf("в представлении null или нет пустого списка правил: %s", js)
	}
}
