//go:build router

package routing

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Пресеты маршрутизации (146-10): состав правил и групп по спискам dat стенда,
// план «уйдёт/придёт», применение в черновик, взятие с пресетом. Ядро в сценарии
// не читается и не переключается.

// rtpNewEnv создаёт окружение с индексом dat стенда и одним узлом в «Моих узлах»
// (хук создаёт группы «Авто» и «Прокси»). Без dat сценарий пропускается.
func rtpNewEnv(t *testing.T, opts ...rtOption) *rtEnv {
	t.Helper()
	ix := lists.NewIndex("")
	files, err := ix.Files()
	if err != nil || len(files) == 0 {
		t.Skipf("в каталоге dat стенда нет файлов списков: %v", err)
	}
	opts = append(opts, rtWithDeps(func(d *Deps) { d.Lists = ix }))
	e := rtNewEnv(t, opts...)
	e.addMyLinks(t, "vless_ws")
	return e
}

// rtpUserRule сохраняет правило пользователя «example.org -> DIRECT».
func (e *rtEnv) rtpUserRule(t *testing.T) Rule {
	t.Helper()
	_, r, err := e.svc.SaveRule(e.rev(t), Rule{
		Enabled: true, Target: Target{Kind: KindDirect},
		Conds: []Cond{{Type: CondDomain, Values: []string{"example.org"}}},
	})
	if err != nil {
		t.Fatalf("SaveRule правила пользователя: %v", err)
	}
	return r
}

// rtpChoice собирает выбор для пресета по данным диалога: страна по умолчанию (или
// первая доступная), до двух сервисов, все галочки включены. ok=false — пресет
// на стенде недоступен.
func rtpChoice(view PresetsView, id string) (PresetChoice, bool) {
	for _, p := range view.Presets {
		if p.ID != id {
			continue
		}
		c := PresetChoice{ID: id, Services: []string{}, Flags: PresetFlags{QUIC: true, Torrent: true, Ads: true}}
		if p.NeedsCountry {
			if len(p.Countries) == 0 {
				return c, false
			}
			c.Country = p.Countries[0]
			for _, code := range p.Countries {
				if code == view.DefaultCountry {
					c.Country = code
				}
			}
		}
		if p.NeedsServices {
			for _, sv := range view.Services {
				if len(c.Services) < 2 {
					c.Services = append(c.Services, sv.ID)
				}
			}
		}
		return c, true
	}
	return PresetChoice{}, false
}

// rtpAssertShape проверяет, что в черновике после пресета: правило пользователя
// первое и без пометки, остальные правила с пометкой «из пресета», число правил
// совпадает с планом, нет значений geoip: в domain.
func rtpAssertShape(t *testing.T, e *rtEnv, user Rule, plan PresetPlan, id string) RulesConfig {
	t.Helper()
	rules := e.rules(t)
	if len(rules.Items) == 0 || rules.Items[0].ID != user.ID || rules.Items[0].Origin != "" {
		t.Fatalf("%s: правило пользователя не первое или получило пометку: %+v", id, rules.Items)
	}
	if want := 1 + len(plan.Add); len(rules.Items) != want {
		t.Errorf("%s: правил %d, по плану %d", id, len(rules.Items), want)
	}
	for _, r := range rules.Items[1:] {
		if r.Origin != OriginPreset || !r.Enabled {
			t.Errorf("%s: правило пресета без пометки или выключено: %+v", id, r)
		}
		for _, c := range r.Conds {
			if c.Type == CondDomain {
				for _, v := range c.Values {
					if strings.HasPrefix(strings.ToLower(v), "geoip:") {
						t.Errorf("%s: в domain значение %q", id, v)
					}
				}
			}
		}
	}
	if rules.Preset == nil || rules.Preset.ID != id {
		t.Errorf("%s: Preset в секции rules: %+v", id, rules.Preset)
	}
	return rules
}

// Состав пресетов по спискам стенда, план, применение и смена пресета.
func TestRouterPresetsCompose(t *testing.T) {
	e := rtpNewEnv(t, rtWithKernels(rtXrayOnly))
	user := e.rtpUserRule(t)

	view, err := e.svc.Presets()
	if err != nil {
		t.Fatalf("Presets: %v", err)
	}
	if len(view.Presets) != 4 {
		t.Fatalf("пресетов %d, ожидалось 4: %+v", len(view.Presets), view.Presets)
	}
	t.Logf("страна по умолчанию %q, сервисов %d, реклама в dat: %v", view.DefaultCountry, len(view.Services), view.AdsAvailable)

	// Последовательная смена пресетов: правило пользователя остаётся первым, а
	// правила прежнего пресета уходят.
	applied := 0
	for _, id := range []string{PresetAllProxy, PresetExceptCountry, PresetSelective, PresetEmpty} {
		c, ok := rtpChoice(view, id)
		if !ok {
			t.Logf("пресет %s на стенде недоступен (нет стран с данными)", id)
			continue
		}
		applied++
		plan, err := e.svc.PlanPreset(c)
		if err != nil {
			t.Fatalf("PlanPreset %s: %v", id, err)
		}
		if id != PresetEmpty && len(plan.Add) == 0 {
			t.Errorf("%s: план без правил", id)
		}
		if id == PresetEmpty && len(plan.Add) != 0 {
			t.Errorf("empty: план с правилами: %+v", plan.Add)
		}
		if plan.Kept != 1 {
			t.Errorf("%s: Kept=%d, ожидалось 1 правило пользователя", id, plan.Kept)
		}
		before := e.rules(t)
		prevPreset := 0
		for _, r := range before.Items {
			if r.Origin == OriginPreset {
				prevPreset++
			}
		}
		if len(plan.Remove) != prevPreset {
			t.Errorf("%s: в плане уходит %d, правил пресета в черновике %d", id, len(plan.Remove), prevPreset)
		}
		// План ничего не пишет.
		if after := e.rules(t); len(after.Items) != len(before.Items) {
			t.Fatalf("%s: PlanPreset изменил черновик", id)
		}

		if _, err := e.svc.ApplyPreset(e.rev(t), c); err != nil {
			t.Fatalf("ApplyPreset %s: %v", id, err)
		}
		rules := rtpAssertShape(t, e, user, plan, id)

		proxy, hasProxy := rtGroupByName(e.groups(t), defaultProxyGroupName)
		if _, hasAuto := rtGroupByName(e.groups(t), defaultAutoGroupName); id != PresetEmpty && (!hasProxy || !hasAuto) {
			t.Errorf("%s: нет групп «Прокси» и «Авто» после пресета", id)
		}
		switch id {
		case PresetAllProxy, PresetExceptCountry:
			if rules.Final.Kind != KindGroup || rules.Final.ID != proxy.ID {
				t.Errorf("%s: final %+v, ожидалась группа «Прокси»", id, rules.Final)
			}
		default:
			if rules.Final.Kind != KindDirect {
				t.Errorf("%s: final %+v, ожидался DIRECT", id, rules.Final)
			}
		}
		if id != PresetEmpty {
			// QUIC — первое правило пресета: UDP + 443 -> REJECT.
			q := rules.Items[1]
			if q.Target.Kind != KindReject || len(q.Conds) != 2 || q.Conds[0].Type != CondNetwork || q.Conds[1].Type != CondPort {
				t.Errorf("%s: первое правило пресета не QUIC: %+v", id, q)
			}
		} else if len(rules.Items) != 1 {
			t.Errorf("empty: после пресета правил %d, ожидалось одно (пользователя)", len(rules.Items))
		}
		if id == PresetExceptCountry {
			// Заблокированное в стране идёт в «Прокси» выше прямых правил страны
			// (T-146-31).
			firstProxy, firstDirectList := -1, -1
			for i, r := range rules.Items[1:] {
				listed := len(r.Conds) == 1 && (r.Conds[0].Type == CondGeosite || r.Conds[0].Type == CondGeoip)
				if listed && r.Target.Kind == KindGroup && firstProxy < 0 {
					firstProxy = i
				}
				if listed && r.Target.Kind == KindDirect && firstDirectList < 0 {
					firstDirectList = i
				}
			}
			if firstProxy >= 0 && firstDirectList >= 0 && firstProxy > firstDirectList {
				t.Errorf("except_country: списки блокировок (%d) ниже прямых списков (%d)", firstProxy, firstDirectList)
			}
		}
	}
	if applied == 0 {
		t.Skip("ни один пресет не доступен на dat стенда")
	}
	if rules := e.rules(t); len(rules.Items) != 1 || rules.Items[0].ID != user.ID {
		t.Errorf("после «пустого» пресета остались правила кроме пользовательского: %+v", rules.Items)
	}

	t.Run("flags", func(t *testing.T) {
		c, ok := rtpChoice(view, PresetAllProxy)
		if !ok {
			t.Skip("all_proxy недоступен")
		}
		listed := func(rules RulesConfig, target string) int {
			n := 0
			for _, r := range rules.Items {
				for _, cd := range r.Conds {
					if cd.Type == CondGeosite && r.Target.Kind == target {
						n++
					}
				}
			}
			return n
		}
		bt := func(rules RulesConfig) int {
			n := 0
			for _, r := range rules.Items {
				for _, cd := range r.Conds {
					if cd.Type == CondBittorrent {
						n++
					}
				}
			}
			return n
		}
		if _, err := e.svc.ApplyPreset(e.rev(t), c); err != nil {
			t.Fatalf("ApplyPreset: %v", err)
		}
		on := e.rules(t)
		if view.AdsAvailable && listed(on, KindReject) == 0 {
			t.Error("реклама есть в dat, а правила блока рекламы нет")
		}
		if bt(on) != 1 {
			t.Errorf("правил bittorrent %d, ожидалось 1", bt(on))
		}
		c.Flags = PresetFlags{}
		if _, err := e.svc.ApplyPreset(e.rev(t), c); err != nil {
			t.Fatalf("ApplyPreset без галочек: %v", err)
		}
		off := e.rules(t)
		if listed(off, KindReject) != 0 || bt(off) != 0 {
			t.Errorf("при снятых галочках остались правила рекламы (%d) или торрентов (%d)", listed(off, KindReject), bt(off))
		}
		for _, r := range off.Items[1:] {
			if len(r.Conds) == 2 {
				t.Errorf("при снятой галочке QUIC правило осталось: %+v", r)
			}
		}
		// Частные сети — всегда, без галочки.
		private := false
		for _, r := range off.Items {
			for _, cd := range r.Conds {
				if cd.Type == CondIP && len(cd.Values) > 0 && cd.Values[0] == "10.0.0.0/8" && r.Target.Kind == KindDirect {
					private = true
				}
			}
		}
		if !private {
			t.Error("частные сети -> DIRECT пропали при снятых галочках")
		}
	})

	t.Run("service_groups", func(t *testing.T) {
		c, ok := rtpChoice(view, PresetSelective)
		if !ok || len(c.Services) == 0 {
			t.Skip("selective или сервисы каталога на стенде недоступны")
		}
		c.Flags.ServiceGroups = true
		plan, err := e.svc.PlanPreset(c)
		if err != nil {
			t.Fatalf("PlanPreset: %v", err)
		}
		if len(plan.Groups) == 0 {
			t.Fatalf("в плане нет создаваемых групп сервисов: %+v", plan)
		}
		if _, err := e.svc.ApplyPreset(e.rev(t), c); err != nil {
			t.Fatalf("ApplyPreset: %v", err)
		}
		cfg := e.groups(t)
		proxy, _ := rtGroupByName(cfg, defaultProxyGroupName)
		auto, _ := rtGroupByName(cfg, defaultAutoGroupName)
		for _, name := range plan.Groups {
			g, ok := rtGroupByName(cfg, name)
			if !ok || g.Mode != ModeManual || len(g.Members) != 3 ||
				g.Members[0].ID != proxy.ID || g.Members[1].ID != auto.ID || g.Members[2].Kind != KindDirect {
				t.Errorf("группа сервиса %q: %+v", name, g)
			}
		}
		used := false
		for _, r := range e.rules(t).Items {
			for _, name := range plan.Groups {
				if g, _ := rtGroupByName(cfg, name); r.Target.Kind == KindGroup && r.Target.ID == g.ID {
					used = true
				}
			}
		}
		if !used {
			t.Error("ни одно правило не ведёт в группу сервиса")
		}
		// Повторное применение не плодит одноимённые группы.
		if _, err := e.svc.ApplyPreset(e.rev(t), c); err != nil {
			t.Fatalf("повторный ApplyPreset: %v", err)
		}
		if again := e.groups(t); len(again.Items) != len(cfg.Items) {
			t.Errorf("повторное применение изменило число групп: %d -> %d", len(cfg.Items), len(again.Items))
		}
	})

	t.Run("recreate_groups", func(t *testing.T) {
		c, ok := rtpChoice(view, PresetAllProxy)
		if !ok {
			t.Skip("all_proxy недоступен")
		}
		// Уберём правила пресета и обе группы (правило пользователя ведёт в DIRECT).
		if _, err := e.svc.ApplyPreset(e.rev(t), PresetChoice{ID: PresetEmpty}); err != nil {
			t.Fatalf("ApplyPreset empty: %v", err)
		}
		if _, err := e.svc.SetFinal(e.rev(t), Target{Kind: KindDirect}); err != nil {
			t.Fatalf("SetFinal: %v", err)
		}
		e.replaceGroups(t, func(cfg *GroupsConfig) { cfg.Items = []Group{} })
		plan, err := e.svc.PlanPreset(c)
		if err != nil {
			t.Fatalf("PlanPreset: %v", err)
		}
		if len(plan.Groups) < 2 {
			t.Errorf("пресет не пересоздаёт удалённые «Прокси» и «Авто»: %v", plan.Groups)
		}
		if _, err := e.svc.ApplyPreset(e.rev(t), c); err != nil {
			t.Fatalf("ApplyPreset: %v", err)
		}
		cfg := e.groups(t)
		proxy, okP := rtGroupByName(cfg, defaultProxyGroupName)
		auto, okA := rtGroupByName(cfg, defaultAutoGroupName)
		if !okP || !okA || len(proxy.Members) != 1 || proxy.Members[0].ID != auto.ID {
			t.Errorf("группы после пересоздания: %+v", cfg.Items)
		}
	})

	t.Run("errors", func(t *testing.T) {
		// Неизвестный пресет, страна без данных и служебный сервис отвергаются
		// кодом ErrInvalidInput; сервис отвергается после проверки страны.
		for _, tc := range []struct {
			name    string
			choice  PresetChoice
			reasons []string
		}{
			{"preset_unknown", PresetChoice{ID: "nope"}, []string{reasonPresetUnknown}},
			{"country_unavailable", PresetChoice{ID: PresetExceptCountry, Country: "ZZ"}, []string{reasonCountryUnavailable}},
			{"service_unknown", PresetChoice{ID: PresetSelective, Services: []string{"ads"}}, []string{reasonServiceUnknown, reasonCountryUnavailable}},
		} {
			_, err := e.svc.PlanPreset(tc.choice)
			var ie *InvalidInputError
			if !errors.Is(err, ErrInvalidInput) || !errors.As(err, &ie) {
				t.Errorf("%s: err=%v", tc.name, err)
				continue
			}
			if !slices.Contains(tc.reasons, ie.Reason) {
				t.Errorf("%s: причина %q, ожидалась одна из %v", tc.name, ie.Reason, tc.reasons)
			}
		}
	})
}

// Взятие с пресетом — одна правка черновика: routing.managed, группы и правила.
func TestRouterPresetsTakeWithPreset(t *testing.T) {
	e := rtpNewEnv(t, rtWithKernels(rtXrayOnly))
	view, err := e.svc.Presets()
	if err != nil {
		t.Fatalf("Presets: %v", err)
	}
	c, ok := rtpChoice(view, PresetAllProxy)
	if !ok {
		t.Skip("all_proxy недоступен")
	}
	before := e.rev(t)
	ev, err := e.svc.TakeWithPreset(before, c)
	if err != nil {
		t.Fatalf("TakeWithPreset: %v", err)
	}
	if ev.DraftRevision != before+1 || e.rev(t) != before+1 {
		t.Errorf("взятие с пресетом заняло несколько правок: было %d, стало %d", before, e.rev(t))
	}
	st := e.state(t)
	desired, err := configlayer.ParseRoutingDesired(st.Draft[configlayer.RoutingSection])
	if err != nil || !desired.Managed {
		t.Errorf("секция routing после взятия: %+v, err=%v", desired, err)
	}
	rules := e.rules(t)
	if len(rules.Items) == 0 {
		t.Fatal("после взятия с пресетом нет правил")
	}
	for _, r := range rules.Items {
		if r.Origin != OriginPreset {
			t.Errorf("правило без пометки «из пресета»: %+v", r)
		}
	}
	if _, err := e.svc.TakeWithPreset(before, c); !errors.Is(err, configlayer.ErrDraftConflict) {
		t.Errorf("повтор на устаревшей ревизии: %v, ожидался ErrDraftConflict", err)
	}
}

// rtpKernels — ядра, установленные на устройстве.
func rtpKernels() configlayer.InstalledKernels {
	var k configlayer.InstalledKernels
	if _, err := os.Stat(rtXrayBin); err == nil {
		k.Xray = true
	}
	if _, err := os.Stat(rtMihomoBin); err == nil {
		k.Mihomo = true
	}
	return k
}

// rtpCountries выбирает страны для проверки ядрами (D-31): РФ, если она есть
// в данных, затем до трёх других: страна со списками блокировок и страны только
// с IP-списком наименьшего размера (крупные IP-списки проверяет тест списков).
func rtpCountries(t *testing.T, view PresetsView) []string {
	t.Helper()
	cat, err := lists.Resolve(lists.NewIndex(""), nil)
	if err != nil {
		t.Skipf("каталог списков dat стенда недоступен: %v", err)
	}
	size := func(ref *lists.ListRef) int {
		for _, l := range cat.Lists {
			if ref != nil && l.File == ref.File && l.Tag == ref.Tag {
				return l.Count
			}
		}
		return 0
	}
	var out []string
	if view.DefaultCountry == defaultPresetCountry {
		out = append(out, defaultPresetCountry)
	}
	var blocked, ipOnly []lists.CountryView
	for _, c := range cat.Countries {
		switch {
		case c.Code == defaultPresetCountry:
		case len(c.Blocked) > 0:
			blocked = append(blocked, c)
		default:
			ipOnly = append(ipOnly, c)
		}
	}
	slices.SortFunc(ipOnly, func(a, b lists.CountryView) int { return size(a.IP) - size(b.IP) })
	if len(blocked) > 0 {
		out = append(out, blocked[0].Code)
	}
	for _, c := range ipOnly {
		if len(out) >= 4 {
			break
		}
		out = append(out, c.Code)
	}
	return out
}

// Правила пресетов проходят проверку обоими ядрами: для «всё через прокси»,
// «всё кроме страны» и «выборочно» и для РФ плюс до трёх других стран с данными
// на стенде берётся маршрутизация с пресетом и «Применить» с настоящими
// xray run -test и mihomo -t (D-31). Выполняется в план-сессии 146-16.
func TestRouterPresetsValidByKernels(t *testing.T) {
	k := rtpKernels()
	if !k.Xray && !k.Mihomo {
		t.Skip("на устройстве нет ни Xray, ни Mihomo")
	}
	probe := rtpNewEnv(t, rtWithKernels(k))
	view, err := probe.svc.Presets()
	if err != nil {
		t.Fatalf("Presets: %v", err)
	}
	countries := rtpCountries(t, view)

	type pair struct{ preset, country string }
	var pairs []pair
	for _, p := range view.Presets {
		switch p.ID {
		case PresetAllProxy:
			pairs = append(pairs, pair{p.ID, ""})
		case PresetExceptCountry, PresetSelective:
			for _, code := range countries {
				if slices.Contains(p.Countries, code) {
					pairs = append(pairs, pair{p.ID, code})
				}
			}
		}
	}
	if len(pairs) == 0 {
		t.Skip("ни один пресет не доступен на dat стенда")
	}
	for _, p := range pairs {
		name := p.preset + "/" + p.country
		if p.country == "" {
			name = p.preset
		}
		t.Run(name, func(t *testing.T) {
			e := rtpNewEnv(t, rtWithKernels(k))
			if k.Mihomo {
				e.writeUserProfile(t)
			}
			c, ok := rtpChoice(view, p.preset)
			if !ok {
				t.Skip("пресет недоступен")
			}
			c.Country = p.country
			c.Flags.ServiceGroups = p.preset == PresetSelective
			if _, err := e.svc.TakeWithPreset(e.rev(t), c); err != nil {
				t.Fatalf("TakeWithPreset: %v", err)
			}
			n := len(e.rules(t).Items)
			if n == 0 {
				t.Fatalf("пресет %s, страна %q: правил нет", p.preset, p.country)
			}
			e.applyOK(t)
			t.Logf("пресет %s, страна %q: правил %d, xray=%v mihomo=%v", p.preset, p.country, n, k.Xray, k.Mihomo)
		})
	}
}
