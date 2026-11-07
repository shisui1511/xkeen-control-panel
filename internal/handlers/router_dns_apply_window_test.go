//go:build router

// router-area: dns

package handlers

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// «Применить» при включённом перехвате DNS роутера на настоящей панели
// устройства (D-08, D-26, D-27, D-29): после каждого пользовательского применения
// служба пробует оба пути через 127.0.0.1:53; правка DNS-секции ядра-перехватчика,
// секции dns_takeover или группы-цели открывает окно подтверждения вида apply, без
// подтверждения откатывается только это применение, а правки пользователя остаются
// в черновике. Вердикт — применённое состояние в файле слоя, вид службы, владелец
// порта 53 по /proc. Стенд готовит и возвращает rtDNSPrepare (router_dns_enable_test.go).

// rtDNSApplyServer — сервер, который сценарий добавляет в DNS-секцию ядра: DoH по
// IP, не совпадающий с умолчаниями D-17.
const rtDNSApplyServer = "https://8.8.4.4/dns-query"

// rtDNSLastCheckAt — last_check.at_ms из вида (0 — проверки не было).
func rtDNSLastCheckAt(v map[string]any) int64 {
	lc, _ := v["last_check"].(map[string]any)
	return rtDNSNum(lc, "at_ms")
}

// rtDNSWaitApplyCheck ждёт конца пробы после «Применить»: last_check новее
// before и операция службы закончилась (state не applying).
func (e *rtSrc) rtDNSWaitApplyCheck(before int64) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(rtDNSStateWait)
	for {
		v := e.rtDNSView()
		if st, _ := v["state"].(string); st != "applying" && rtDNSLastCheckAt(v) > before {
			return v
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("проба после «Применить» не закончилась за %v: state %q, last_check.at_ms %d (до применения %d), шаги %v",
				rtDNSStateWait, v["state"], rtDNSLastCheckAt(v), before, v["steps"])
		}
		time.Sleep(time.Second)
	}
}

// rtDNSApplyChecked — «Применить» при включённом перехвате: применение успешно,
// затем ожидание пробы службы; возвращает вид после неё.
func (e *rtSrc) rtDNSApplyChecked(label string) map[string]any {
	e.t.Helper()
	before := rtDNSLastCheckAt(e.rtDNSWaitDone())
	e.rtRtApplyOK(label)
	return e.rtDNSWaitApplyCheck(before)
}

// rtDNSApplyKept — «Применить» с подтверждением окна, если оно открылось; при
// перехвате, который не включён (state не on и не window), — обычное «Применить».
func (e *rtSrc) rtDNSApplyKept(label string) {
	e.t.Helper()
	if st, _ := e.rtDNSWaitDone()["state"].(string); st != "on" && st != "window" {
		e.rtRtApplyOK(label)
		return
	}
	if st, _ := e.rtDNSApplyChecked(label)["state"].(string); st == "window" {
		e.rtDNSKeep()
	}
}

// rtDNSAppliedHas — применённая секция section в файле слоя содержит needle.
func rtDNSAppliedHas(t *testing.T, section, needle string) bool {
	t.Helper()
	return strings.Contains(string(rtDNSLayerApplied(t)[section]), needle)
}

// rtDNSAddServer добавляет сервер в список серверов DNS-секции ядра в черновике.
func (e *rtSrc) rtDNSAddServer(kernel, server string) {
	e.t.Helper()
	form, advanced := e.rtDNSSectionForm(kernel)
	key := rtDNSServersKey(kernel)
	list, _ := form[key].([]any)
	form[key] = append(slices.Clone(list), server)
	r := e.rtCoresPost("/save", map[string]any{"kernel": kernel, "section": "dns", "form": form, "advanced": advanced})
	if r.Status != http.StatusOK {
		e.t.Fatalf("save dns (%s): %d %s", kernel, r.Status, r.Code)
	}
}

// rtDNSDraftHasServer — сервер есть в списке серверов DNS-секции черновика.
func (e *rtSrc) rtDNSDraftHasServer(kernel, server string) bool {
	e.t.Helper()
	form, _ := e.rtDNSSectionForm(kernel)
	return slices.Contains(rtDNSMihomoStrings(form[rtDNSServersKey(kernel)]), server)
}

// rtDNSRestoreSection возвращает секцию section черновика к значению orig из
// применённого состояния и применяет с подтверждением окна (t.Cleanup).
func (e *rtSrc) rtDNSRestoreSection(section string, orig json.RawMessage) {
	e.t.Helper()
	e.rtRstIdle()
	var value any = orig
	if len(orig) == 0 {
		value = nil
	}
	r := e.call(http.MethodPost, "/api/configlayer/draft", map[string]any{"revision": e.revision(), "section": section, "value": value})
	if r.Status != http.StatusOK {
		e.t.Errorf("очистка: возврат секции %s: %d %s", section, r.Status, r.Code)
		return
	}
	if e.rtRtDraftChanges() > 0 {
		e.rtDNSApplyKept("очистка: секция " + section)
	}
}

// Трасса: подготовка → перехват включён и подтверждён → в черновик DNS-секции
// ядра добавлен сервер → «Применить» → проба прошла, state window, window.kind=apply
// → без «Оставить» ждать окно и откат: state on (перехват остался), Applied
// DNS-секции без сервера, черновик содержит сервер (draft_changes > 0), :53 держит
// ядро, исход timeout. Подтест keep: то же «Применить» → окно apply → «Оставить» →
// Applied содержит сервер. Метка ядра: DNS-секция и перехват — у активного ядра.
func TestRouterDNSApplyWindow(t *testing.T) {
	routertest.Tier(t, routertest.Full, "реальное время: окно «Применить» DNS без «Оставить» ждёт откат по таймеру")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	e.rtDNSEnableOK()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	section := configlayer.CoreSectionName(kernel, "dns")
	orig := rtDNSLayerApplied(t)[section]
	t.Cleanup(func() { e.with(t).rtDNSRestoreSection(section, orig) })
	if rtDNSAppliedHas(t, section, rtDNSApplyServer) {
		t.Fatalf("сервер %s уже есть в применённой секции %s до сценария", rtDNSApplyServer, section)
	}

	e.rtDNSAddServer(kernel, rtDNSApplyServer)
	v := e.rtDNSApplyChecked("сервер в DNS-секции при перехвате")
	if st, _ := v["state"].(string); st != "window" {
		code, detail := rtDNSFailure(v)
		t.Fatalf("после «Применить» DNS-секции state %q, ожидалось window: сбой %q %.300s; шаги %v", st, code, detail, v["steps"])
	}
	if kind, _ := rtDNSWindow(v)["kind"].(string); kind != "apply" {
		t.Fatalf("после «Применить» DNS-секции window.kind %q, ожидалось apply", kind)
	}
	if lc, _ := v["last_check"].(map[string]any); lc["source"] != "auto" {
		t.Errorf("проба после «Применить»: last_check.source %v, ожидалось auto", lc["source"])
	}
	if !rtDNSAppliedHas(t, section, rtDNSApplyServer) {
		t.Errorf("в окне в применённой секции %s нет добавленного сервера", section)
	}

	on := e.rtDNSWaitState("on", rtDNSTimeoutWait)
	if code, detail := rtDNSFailure(on); code != "" {
		t.Errorf("после отката «Применить» сбой %q: %.300s", code, detail)
	}
	if desired, _ := on["desired"].(bool); !desired {
		t.Error("после отката «Применить» desired=false: перехват должен остаться")
	}
	if rtDNSAppliedHas(t, section, rtDNSApplyServer) {
		t.Errorf("после отката «Применить» в применённой секции %s остался сервер", section)
	}
	if n := e.rtRtDraftChanges(); n == 0 {
		t.Error("после отката «Применить» черновик закрыт: правки пользователя потеряны (D-27)")
	}
	if !e.rtDNSDraftHasServer(kernel, rtDNSApplyServer) {
		t.Error("после отката «Применить» в черновике DNS-секции нет добавленного сервера (D-27)")
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerKernel || got.Kernel != kernel {
		t.Errorf("после отката «Применить» порт 53 держит %q/%q, ожидалось ядро %s", got.Kind, got.Kernel, kernel)
	}
	if kind, _ := rtDNSOutcome(on)["kind"].(string); kind != "timeout" {
		t.Errorf("исход после отката «Применить» %v, ожидалось kind=timeout", rtDNSOutcome(on))
	}

	t.Run("keep", func(t *testing.T) {
		c := e.with(t)
		if !c.rtDNSDraftHasServer(kernel, rtDNSApplyServer) {
			c.rtDNSAddServer(kernel, rtDNSApplyServer)
		}
		v := c.rtDNSApplyChecked("сервер в DNS-секции при перехвате (keep)")
		if kind, _ := rtDNSWindow(v)["kind"].(string); kind != "apply" {
			code, detail := rtDNSFailure(v)
			t.Fatalf("повторное «Применить»: state %q, window %v, ожидалось окно apply: сбой %q %.300s", v["state"], v["window"], code, detail)
		}
		kept := c.rtDNSKeep()
		c.rtDNSExpectOn(kept, kernel)
		if !rtDNSAppliedHas(t, section, rtDNSApplyServer) {
			t.Errorf("после «Оставить» в применённой секции %s нет добавленного сервера", section)
		}
		if n := c.rtRtDraftChanges(); n != 0 {
			t.Errorf("после «Оставить» в черновике %d изменений, ожидался закрытый черновик", n)
		}
	})
}

// rtDNSDirectDomain — домен direct-правила сценария пробы (D-21): генератор
// выпускает его в direct-серверы DNS ядра-перехватчика.
const rtDNSDirectDomain = "example.net"

// rtDNSDirectEmitted — домен direct-правила есть в DNS ядра на диске: у Xray — в
// файле DNS-секции панели, у Mihomo — ключ «+.<домен>» в dns.nameserver-policy
// профиля панели.
func rtDNSDirectEmitted(t *testing.T, kernel, domain string) bool {
	t.Helper()
	switch kernel {
	case configlayer.KernelXray:
		data, err := os.ReadFile(filepath.Join(rtRtXrayDir, rtCoresDNSPanelFile))
		return err == nil && strings.Contains(string(data), domain)
	case configlayer.KernelMihomo:
		data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, rtRtPanelProfile))
		if err != nil {
			return false
		}
		var top struct {
			DNS struct {
				Policy map[string]any `yaml:"nameserver-policy"`
			} `yaml:"dns"`
		}
		if yaml.Unmarshal(data, &top) != nil {
			return false
		}
		_, ok := top.DNS.Policy["+."+domain]
		return ok
	}
	return false
}

// rtDNSExpectOnNoWindow — после пробы «Применить» без окна: state on, окна нет,
// last_check новее before с source auto, :53 держит ядро.
func rtDNSExpectOnNoWindow(t *testing.T, v map[string]any, kernel string, before int64) {
	t.Helper()
	if st, _ := v["state"].(string); st != "on" {
		code, detail := rtDNSFailure(v)
		t.Errorf("после «Применить» state %q, ожидалось on: сбой %q %.300s", st, code, detail)
	}
	if w := rtDNSWindow(v); w != nil {
		t.Errorf("после «Применить» без правки DNS открыто окно: %v", w)
	}
	if at := rtDNSLastCheckAt(v); at <= before {
		t.Errorf("last_check.at_ms %d не новее, чем до «Применить» (%d): пробы не было", at, before)
	}
	if lc, _ := v["last_check"].(map[string]any); lc["source"] != "auto" {
		t.Errorf("last_check.source %v, ожидалось auto", lc["source"])
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerKernel || got.Kernel != kernel {
		t.Errorf("порт 53 держит %q/%q, ожидалось ядро %s", got.Kind, got.Kernel, kernel)
	}
}

// rtDNSMySourceID — id источника «Мои узлы» (пусто — его нет).
func (e *rtSrc) rtDNSMySourceID() string {
	e.t.Helper()
	view := e.call(http.MethodGet, rtSourcesBase, nil)
	if view.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtSourcesBase, view.Status, view.Code)
	}
	srcs, _ := view.object(e.t)["sources"].([]any)
	for _, it := range srcs {
		src, _ := it.(map[string]any)
		if src["kind"] == "my" {
			id, _ := src["id"].(string)
			return id
		}
	}
	return ""
}

// rtDNSSaveGroup сохраняет группу маршрутизации в черновик.
func (e *rtSrc) rtDNSSaveGroup(group map[string]any) {
	e.t.Helper()
	r := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{"revision": e.rtRtRevision(), "group": group})
	if r.Status != http.StatusOK {
		e.t.Fatalf("groups/save: %d %s", r.Status, r.Code)
	}
}

// Трасса: подготовка → перехват включён и подтверждён → правило direct с доменом
// через API маршрутизации → «Применить» → окна нет (state on), last_check новее,
// чем до применения (проба D-29 прошла), домен выпущен в direct-серверы DNS ядра.
// Подтест probe_fail: группа-цель «Прокси» переведена правкой состава на
// единственный мёртвый узел → «Применить» → проба не прошла → применение откачено
// сразу: state on, окна нет, сбой probe_* с rolled_back, Applied групп прежний,
// черновик содержит правку (draft_changes > 0). Метка ядра: перехват и DNS-секция
// — у активного ядра.
func TestRouterDNSApplyProbe(t *testing.T) {
	routertest.Tier(t, routertest.Full, "внесение сбоя: мёртвый узел группы-цели обрывает пробу после «Применить» DNS")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	e.rtDNSEnableOK()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })

	rs := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{
		"revision": e.rtRtRevision(),
		"rule": map[string]any{
			"enabled": true,
			"conds":   []map[string]any{{"type": "domain", "values": []string{rtDNSDirectDomain}}},
			"target":  map[string]string{"kind": "direct"},
		},
	})
	if rs.Status != http.StatusOK {
		t.Fatalf("rules/save: %d %s", rs.Status, rs.Code)
	}
	rule, _ := rs.object(t)["rule"].(map[string]any)
	rid, _ := rule["id"].(string)
	t.Cleanup(func() {
		c := e.with(t)
		c.rtRstIdle()
		if r := c.call(http.MethodPost, rtRoutingBase+"/rules/delete", map[string]any{"revision": c.rtRtRevision(), "id": rid}); r.Status != http.StatusOK {
			t.Errorf("очистка: rules/delete: %d %s", r.Status, r.Code)
		}
		if c.rtRtDraftChanges() > 0 {
			c.rtDNSApplyKept("очистка: правило direct")
		}
	})
	before := rtDNSLastCheckAt(e.rtDNSWaitDone())
	v := e.rtDNSApplyChecked("правило direct при перехвате")
	rtDNSExpectOnNoWindow(t, v, kernel, before)
	if code, detail := rtDNSFailure(v); code != "" {
		t.Errorf("после «Применить» правил сбой %q: %.300s", code, detail)
	}
	if !rtDNSDirectEmitted(t, kernel, rtDNSDirectDomain) {
		t.Errorf("домен direct-правила не выпущен в DNS %s после «Применить»", kernel)
	}

	t.Run("probe_fail", func(t *testing.T) {
		c := e.with(t)
		group := rtRtFindGroup(c.rtRtView(), rtDNSProxyGroup)
		if group == nil {
			t.Fatalf("нет группы %q", rtDNSProxyGroup)
		}
		orig := maps.Clone(group)
		link := rtLinkText(t, "vless_ws")
		re := regexp.MustCompile(`@[^:/?#]+:\d+`)
		if !re.MatchString(link) {
			t.Fatal("в ссылке фикстуры нет адреса с портом")
		}
		c.rtRstAddMyNode(re.ReplaceAllString(link, "@127.0.0.1:"+rtRpClosedPort(t)))
		t.Cleanup(func() {
			cc := e.with(t)
			cc.rtRstIdle()
			cc.rtDNSSaveGroup(orig)
			for id := cc.rtRstMyEntry(); id != ""; id = cc.rtRstMyEntry() {
				cc.rtRstDeleteMyNode(id)
			}
			if cc.rtRtDraftChanges() > 0 {
				cc.rtDNSApplyKept("очистка: состав группы-цели")
			}
		})
		myID := c.rtDNSMySourceID()
		if myID == "" {
			t.Fatal("источника «Мои узлы» нет после добавления узла")
		}
		dead := maps.Clone(group)
		dead["sources"] = map[string]any{"all": false, "ids": []string{myID}}
		dead["include"] = []string{}
		dead["exclude"] = []string{}
		dead["picks"] = []string{}
		dead["members"] = []map[string]string{}
		dead["reserve"] = ""
		c.rtDNSSaveGroup(dead)

		appliedGroups := rtDNSLayerApplied(t)["groups"]
		before := rtDNSLastCheckAt(c.rtDNSWaitDone())
		v := c.rtDNSApplyChecked("группа-цель на мёртвом узле при перехвате")
		if st, _ := v["state"].(string); st != "on" {
			code, detail := rtDNSFailure(v)
			t.Fatalf("после отката по пробе state %q, ожидалось on: сбой %q %.300s; шаги %v", st, code, detail, v["steps"])
		}
		if w := rtDNSWindow(v); w != nil {
			t.Errorf("после отката по пробе открыто окно: %v", w)
		}
		code, _ := rtDNSFailure(v)
		if !strings.HasPrefix(code, "probe_") {
			t.Errorf("сбой после «Применить» с мёртвой группой %q, ожидался probe_*", code)
		}
		if f, _ := v["failure"].(map[string]any); f["rolled_back"] != true {
			t.Errorf("сбой пробы после «Применить»: rolled_back %v, ожидалось true", f["rolled_back"])
		}
		if at := rtDNSLastCheckAt(v); at <= before {
			t.Errorf("last_check.at_ms %d не новее, чем до «Применить» (%d)", at, before)
		}
		if !configlayer.SectionEqual(appliedGroups, rtDNSLayerApplied(t)["groups"]) {
			t.Error("после отката по пробе Applied групп отличается от прежнего")
		}
		if n := c.rtRtDraftChanges(); n == 0 {
			t.Error("после отката по пробе черновик закрыт: правка состава группы потеряна (draft_changes = 0, D-27)")
		}
		g := rtRtFindGroup(c.rtRtView(), rtDNSProxyGroup)
		src, _ := g["sources"].(map[string]any)
		if ids := rtDNSMihomoStrings(src["ids"]); !slices.Equal(ids, []string{myID}) {
			t.Errorf("после отката по пробе в черновике группы %q источники %v, ожидалась правка [мои узлы]", rtDNSProxyGroup, ids)
		}
		if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerKernel || got.Kernel != kernel {
			t.Errorf("после отката по пробе порт 53 держит %q/%q, ожидалось ядро %s", got.Kind, got.Kernel, kernel)
		}
		if !rtDNSResolves(dnstakeover.DefaultCheckName) {
			t.Error("после отката по пробе внешнее имя не разрешается через 127.0.0.1:53")
		}
	})
}
