//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

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
