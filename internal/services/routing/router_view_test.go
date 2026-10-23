//go:build router

package routing

import (
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// rtVersionRE — число версии в выводе `xray version` и `mihomo -v`.
var rtVersionRE = regexp.MustCompile(`\d+\.\d+\.\d+|alpha-[0-9a-f]{7,}`)

// rtBinVersion читает версию настоящего бинарника ядра устройства; не
// прочиталась — пустая строка (версия не определена).
func rtBinVersion(bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return ""
	}
	return rtVersionRE.FindString(string(out))
}

// withVersions — копия сервиса с заданными версиями ядер: представление читает
// версии из Deps.KernelVersion.
func withVersions(e *rtEnv, xray, mihomo string) *Service {
	d := e.svc.d
	d.KernelVersion = func(kernel string) string {
		if kernel == "xray" {
			return xray
		}
		return mihomo
	}
	return &Service{d: d}
}

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

// Правила, варианты участника, доступность функций и испорченные секции.
func TestRouterRoutingViewRules(t *testing.T) {
	e := rtNewEnv(t)
	e.addMyLinks(t, rtLinkNames...)
	cat := e.catalog(t)

	proxy, ok := rtGroupByName(e.groups(t), "Прокси")
	if !ok {
		t.Fatal("нет группы «Прокси»")
	}
	auto, _ := rtGroupByName(e.groups(t), "Авто")

	// Варианты участника: «Прокси» — группа «Авто» первой, затем узлы без повторов;
	// у «Авто» вариантов нет.
	v := e.view(t)
	opts := rtViewGroup(t, v, "Прокси").MemberOptions
	if len(opts) == 0 || opts[0].Member != "group:"+auto.ID || opts[0].Label != "Авто" {
		t.Fatalf("варианты участника «Прокси»: %+v", opts)
	}
	seen := map[string]bool{}
	for _, o := range opts[1:] {
		if !strings.HasPrefix(o.Member, "node:") || o.Label == "" || seen[o.Member] {
			t.Errorf("вариант участника %+v: не узел, без имени или повтор", o)
		}
		seen[o.Member] = true
	}
	for _, n := range cat.Mihomo {
		if !seen["node:"+n.Ref] {
			t.Errorf("узла %s нет среди вариантов участника", n.Ref)
		}
	}
	if got := rtViewGroup(t, v, "Авто").MemberOptions; len(got) != 0 {
		t.Errorf("у группы «авто» есть варианты участника: %+v", got)
	}

	// Правила: номера с 1, пометка ядра.
	toProxy := Target{Kind: KindGroup, ID: proxy.ID}
	for _, r := range []Rule{
		{Enabled: true, Conds: []Cond{{Type: CondDomain, Values: []string{"example.com"}}}, Target: toProxy},
		{Enabled: true, Conds: []Cond{{Type: CondBittorrent}}, Target: Target{Kind: KindDirect}},
		{Enabled: true, Raw: &RawRule{Kernel: "mihomo", Text: "DOMAIN,example.org"}, Target: toProxy},
		{Enabled: true, Raw: &RawRule{Kernel: "xray", Text: `{"domain":["example.net"]}`}, Target: toProxy},
	} {
		if _, _, err := e.svc.SaveRule(e.rev(t), r); err != nil {
			t.Fatalf("SaveRule: %v", err)
		}
	}
	v = e.view(t)
	wantOnly := []string{"", "xray", "mihomo", "xray"}
	if len(v.Rules) != len(wantOnly) {
		t.Fatalf("правил в представлении %d, ждали %d", len(v.Rules), len(wantOnly))
	}
	for i, r := range v.Rules {
		if r.Index != i+1 || r.Only != wantOnly[i] || len(r.Issues) != 0 || len(r.Warnings) != 0 {
			t.Errorf("правило %d: %+v", i+1, r)
		}
	}

	// Доступность функций: на стенде версии ядер достаточны (Xray 1.8.10+,
	// Mihomo 1.19.27+). Alpha-сборка Mihomo версии не имеет — undetermined.
	live := withVersions(e, rtBinVersion(rtXrayBin, "version"), rtBinVersion(rtMihomoBin, "-v"))
	lv, err := live.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	for _, f := range []string{"xray_auto_group", "xray_fallback_group", "mihomo_groups"} {
		fv, ok := lv.Features[f]
		if !ok || fv.Min == "" || (fv.Status != "available" && fv.Status != "undetermined") {
			t.Errorf("функция %s на стенде: %+v", f, fv)
		}
	}
	if lv.Features["xray_auto_group"].Min != "1.8.10" || lv.Features["mihomo_groups"].Min != "1.19.27" {
		t.Errorf("пороги функций: %+v", lv.Features)
	}
	for _, g := range lv.Groups {
		for _, w := range g.Warnings {
			if w.Code == "mode_unavailable" {
				t.Errorf("на стенде режим группы %q недоступен: %+v", g.Name, w)
			}
		}
	}

	// Старые версии ядер: режим «авто» и группы Mihomo недоступны, предупреждения
	// не блокируют «Применить».
	old := withVersions(e, "1.8.4", "1.19.1")
	ov, err := old.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	oa := rtViewGroup(t, ov, "Авто")
	if !rtViewHas(oa.Warnings, "mode_unavailable", "group", "Авто", "kernel", "xray", "min", "1.8.10") ||
		!rtViewHas(oa.Warnings, "mode_unavailable", "group", "Авто", "kernel", "mihomo", "min", "1.19.27") {
		t.Errorf("«Авто» на старых ядрах: %+v", oa.Warnings)
	}
	if fv := ov.Features["xray_auto_group"]; fv.Status != "unavailable" {
		t.Errorf("xray_auto_group на Xray 1.8.4: %+v", fv)
	}
	if len(ov.Issues) != 0 {
		t.Errorf("версии ядер дали блокирующие проблемы: %+v", ov.Issues)
	}
	if rtViewHas(rtViewGroup(t, ov, "Прокси").Warnings, "mode_unavailable", "kernel", "xray") {
		t.Errorf("ручная группа Xray не зависит от версии")
	}

	// Устаревший отдельный узел: ссылки нет ни в одном ядре.
	if _, _, err := e.svc.SaveGroup(e.rev(t), Group{Name: "Старый узел", Mode: ModeManual, Picks: []string{"my/deadbeef"}}); err != nil {
		t.Fatalf("SaveGroup: %v", err)
	}
	stale := rtViewGroup(t, e.view(t), "Старый узел")
	if !rtViewHas(stale.Warnings, "pick_stale", "group", "Старый узел") {
		t.Errorf("нет pick_stale для узла, которого нет в каталоге: %+v", stale.Warnings)
	}

	// Секреты узлов в представление не попадают.
	raw, err := json.Marshal(e.view(t))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, name := range []string{"vless_ws", "trojan_grpc", "vless_reality"} {
		u, err := url.Parse(rtLink(t, name))
		if err != nil || u.User == nil || len(u.User.Username()) < 8 {
			t.Fatalf("в ссылке %s нет секрета для проверки: %v", name, err)
		}
		if strings.Contains(string(raw), u.User.Username()) {
			t.Errorf("секрет узла %s попал в представление", name)
		}
	}

	// Испорченная секция не роняет представление: пустая секция и section_invalid.
	junk := map[string]json.RawMessage{
		GroupsSection: json.RawMessage(`"junk"`),
		RulesSection:  json.RawMessage(`{"v":7,"items":[]}`),
		ProbeSection:  json.RawMessage(`[1]`),
	}
	for name, body := range junk {
		e.putSection(t, name, body)
		jv := e.view(t)
		if !rtViewHas(jv.Issues, "section_invalid", "section", name) {
			t.Errorf("секция %s с мусором: Issues %+v", name, jv.Issues)
		}
		if jv.Groups == nil || jv.Rules == nil || jv.Issues == nil {
			t.Errorf("секция %s с мусором: срезы nil", name)
		}
		switch name {
		case GroupsSection:
			if len(jv.Groups) != 0 {
				t.Errorf("испорченная секция groups отдала группы: %+v", jv.Groups)
			}
		case RulesSection:
			if len(jv.Rules) != 0 {
				t.Errorf("испорченная секция rules отдала правила: %+v", jv.Rules)
			}
		case ProbeSection:
			if jv.Probe != jv.ProbeDefaults {
				t.Errorf("испорченная секция probe: %+v, по умолчанию %+v", jv.Probe, jv.ProbeDefaults)
			}
		}
		e.dropSection(t, name)
	}
}
