//go:build router

// router-area: dns

package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Перехват DNS роутера при действиях с ядром из панели (D-14, D-15) на настоящей
// панели устройства: остановка ядра-перехватчика возвращает DNS прошивке и помнит
// перехват, запуск возвращает его пробой без окна, смена ядра переносит перехват
// на новое ядро с окном подтверждения. Вердикт — факты устройства: кто держит
// порт 53 (/proc), ответ DNS через 127.0.0.1:53, override в рабочей конфигурации
// прошивки, слушатель панели в файлах ядра, процессы ядер. Стенд готовит и
// возвращает rtDNSPrepare (router_dns_enable_test.go).

// rtDNSRestoreWait — сколько ждать возврата перехвата после запуска ядра.
const rtDNSRestoreWait = 120 * time.Second

// rtDNSExpectKernelStopped — после остановки ядра-перехватчика: state waiting с
// причиной kernel_stopped, перехват желаем, порт 53 у прошивки и она отвечает,
// override как до включения, в файлах ядра нет слушателя панели, ядро не запущено.
func (e *rtSrc) rtDNSExpectKernelStopped(kernel string, overrideBefore bool) map[string]any {
	e.t.Helper()
	v := e.rtDNSWaitState("waiting", rtDNSStateWait)
	if reason, _ := v["waiting_reason"].(string); reason != "kernel_stopped" {
		e.t.Errorf("waiting_reason %q, ожидалось kernel_stopped", reason)
	}
	if desired, _ := v["desired"].(bool); !desired {
		e.t.Error("после остановки ядра desired=false, перехват должен помниться")
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		e.t.Errorf("после остановки ядра порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		e.t.Error("после остановки ядра прошивка не отвечает через 127.0.0.1:53")
	}
	if got := rtDNSOverrideOn(e.t); got != overrideBefore {
		e.t.Errorf("после остановки ядра override=%v, до включения был %v", got, overrideBefore)
	}
	if rtDNSKernelHasListener(kernel) {
		e.t.Errorf("после остановки ядра в файлах %s остался слушатель :53", kernel)
	}
	if pids, out := rtRstKernelPIDs(kernel); len(pids) != 0 {
		e.t.Errorf("после остановки ядро %s работает: %s", kernel, out)
	}
	return v
}

// Трасса: подготовка → включение и «Оставить» → остановка ядра через API служб →
// state waiting (kernel_stopped), desired=true, :53 у прошивки, override прежний,
// файлы ядра без слушателя → запуск ядра через API служб → в пределах 120 с state
// on без промежуточного окна, :53 у ядра, проба возврата (source auto) по двум
// группам успешна — служебный домен разрешён через прошивку, адрес резолвера в
// возвращённой конфигурации актуален. Метка ядра: сценарий останавливает и
// запускает активное ядро.
func TestRouterDNSKernelStop(t *testing.T) {
	routertest.Slow(t, "остановка и запуск ядра, возврат перехвата DNS в пределах 120 с")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSEnableOK()
	t.Cleanup(func() {
		c := e.with(t)
		if pids, _ := rtRstKernelPIDs(kernel); len(pids) == 0 {
			c.rtDNSServiceControl("start")
			rtCoresLiveWaitStatus(t)
		}
		c.rtDNSEnsureOff()
	})

	e.rtDNSServiceControl("stop")
	stopped := e.rtDNSExpectKernelStopped(kernel, overrideBefore)
	before := rtDNSLastCheckAt(stopped)

	e.rtDNSServiceControl("start")
	deadline := time.Now().Add(rtDNSRestoreWait)
	sawWindow := false
	var v map[string]any
	for {
		v = e.rtDNSView()
		st, _ := v["state"].(string)
		if st == "window" {
			sawWindow = true
		}
		if st == "on" {
			break
		}
		if time.Now().After(deadline) {
			code, detail := rtDNSFailure(v)
			t.Fatalf("перехват не вернулся за %v после запуска ядра: state %q, сбой %q %.300s; шаги %v", rtDNSRestoreWait, st, code, detail, v["steps"])
		}
		time.Sleep(time.Second)
	}
	if sawWindow {
		t.Error("возврат перехвата после запуска ядра прошёл через окно подтверждения")
	}
	e.rtDNSExpectOn(v, kernel)
	if at := rtDNSLastCheckAt(v); at <= before {
		t.Errorf("проба возврата не записана: last_check.at_ms %d, до запуска %d", at, before)
	}
}

// rtDNSSwitchPath — смена ядра через API служб.
const rtDNSSwitchPath = "/api/service/control?action=switch_kernel&kernel="

// rtDNSSwitchKernel меняет ядро через API служб: 200 и outcome switched.
func (e *rtSrc) rtDNSSwitchKernel(kernel string) {
	e.t.Helper()
	r := e.callSlow(http.MethodPost, rtDNSSwitchPath+kernel, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("смена ядра на %s: %d %s", kernel, r.Status, r.Code)
	}
	if outcome, _ := r.object(e.t)["outcome"].(string); outcome != "switched" {
		e.t.Fatalf("смена ядра на %s: outcome %q, ожидалось switched", kernel, outcome)
	}
}

// rtDNSExpectSwitchWindow ждёт окна переноса перехвата на ядро to: state window,
// window.kind=kernel_switch, kernel=to, проба переноса (source auto, новее before)
// по двум группам успешна — служебный домен разрешён через прошивку, адрес
// резолвера в конфигурации нового ядра актуален.
func (e *rtSrc) rtDNSExpectSwitchWindow(to string, before int64) map[string]any {
	e.t.Helper()
	v := e.rtDNSWaitState("window", rtDNSStateWait)
	if kind, _ := rtDNSWindow(v)["kind"].(string); kind != "kernel_switch" {
		e.t.Errorf("после смены ядра window.kind %q, ожидалось kernel_switch", kind)
	}
	if got, _ := v["kernel"].(string); got != to {
		e.t.Errorf("после смены ядра kernel %q, ожидалось %s", got, to)
	}
	if at := rtDNSLastCheckAt(v); at <= before {
		e.t.Errorf("проба переноса не записана: last_check.at_ms %d, до смены %d", at, before)
	}
	lc, _ := v["last_check"].(map[string]any)
	if src, _ := lc["source"].(string); src != "auto" {
		e.t.Errorf("last_check.source %q, ожидалось auto", src)
	}
	groups, _ := lc["groups"].([]any)
	if len(groups) != 2 {
		e.t.Errorf("групп пробы переноса %d, ожидалось две (external, local)", len(groups))
	}
	for _, it := range groups {
		g, _ := it.(map[string]any)
		if ok, _ := g["ok"].(bool); !ok {
			e.t.Errorf("проба переноса %v не прошла: %v", g["label"], g["reason"])
		}
	}
	return v
}

// rtDNSKernelSwitchPrepare готовит стенд к смене ядра с перехватом: подготовка перехвата на
// активном ядре, второе ядро установлено, его DNS-секция у панели; очистка возвращает
// исходное ядро и выключает перехват. Возвращает второе ядро; второго нет — Skip.
func rtDNSKernelSwitchPrepare(t *testing.T, e *rtSrc, kernel string) string {
	t.Helper()
	other := map[string]string{configlayer.KernelXray: configlayer.KernelMihomo, configlayer.KernelMihomo: configlayer.KernelXray}[kernel]
	if other == "" {
		t.Fatalf("неизвестное ядро цели %q", kernel)
	}
	rtDNSPrepare(t, e, kernel)
	if installed, _ := e.rtCoresView(other)["installed"].(bool); !installed {
		t.Skipf("%s не установлен на стенде: перехват некуда переносить", other)
	}
	if other == configlayer.KernelXray && e.rtDNSCoresOwner(other) != "panel" {
		r := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"dns"}, "ack_comments": true})
		if r.Status != http.StatusOK {
			t.Fatalf("take dns (xray): %d %s", r.Status, r.Code)
		}
		t.Cleanup(func() {
			c := e.with(t)
			c.rtRstIdle()
			if r := c.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "dns"}); r.Status != http.StatusOK {
				t.Errorf("очистка: release dns (xray): %d %s", r.Status, r.Code)
			}
			if c.rtRtDraftChanges() > 0 {
				c.rtRtApplyOK("очистка: DNS-секция второго ядра")
			}
		})
		e.rtRtApplyOK("DNS-секция второго ядра")
	}
	if owner := e.rtDNSCoresOwner(other); owner != "panel" {
		t.Fatalf("DNS-секция %s у %q, ожидалась panel", other, owner)
	}
	t.Cleanup(func() {
		c := e.with(t)
		c.rtDNSEnsureOff()
		if _, last := rtSwitchRaceSettled(kernel, other, time.Second); last == "" {
			return
		}
		_ = c.callSlow(http.MethodPost, rtDNSSwitchPath+kernel, nil)
		if _, last := rtSwitchRaceSettled(kernel, other, rtSwitchRaceWait); last != "" {
			t.Errorf("очистка: ядро %s не вернулось за %v: %s", kernel, rtSwitchRaceWait, last)
		}
		c.rtDNSEnsureOff()
	})
	return other
}

// Трасса: подготовка (оба ядра установлены, DNS-секции обоих у панели) →
// включение и «Оставить» на активном ядре → смена ядра через API служб → state
// window, window.kind=kernel_switch, kernel — новое, проба переноса по двум группам
// → «Оставить» → :53 у нового ядра, в файлах прежнего нет слушателя → обратная
// смена → окно переноса → без «Оставить» откат по таймеру: state off, :53 у
// прошивки, активным осталось переключённое ядро. Стенд возвращает исходное ядро
// и выключает перехват. Метка ядра: сценарий меняет активное ядро.
func TestRouterDNSKernelSwitch(t *testing.T) {
	routertest.Tier(t, routertest.Full, "реальное время: окно переноса DNS при смене ядра ждёт откат по таймеру")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	other := rtDNSKernelSwitchPrepare(t, e, kernel)
	on := e.rtDNSEnableOK()
	e.rtDNSSwitchKernel(other)
	e.rtDNSExpectSwitchWindow(other, rtDNSLastCheckAt(on))
	kept := e.rtDNSKeep()
	e.rtDNSExpectOn(kept, other)
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после переноса в файлах прежнего ядра %s остался слушатель :53", kernel)
	}

	e.rtDNSSwitchKernel(kernel)
	e.rtDNSExpectSwitchWindow(kernel, rtDNSLastCheckAt(kept))
	off := e.rtDNSWaitState("off", rtDNSTimeoutWait)
	if desired, _ := off["desired"].(bool); desired {
		t.Error("после отката окна переноса desired=true")
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после отката окна переноса порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после отката окна переноса прошивка не отвечает через 127.0.0.1:53")
	}
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после отката окна переноса в файлах %s остался слушатель :53", kernel)
	}
	if _, last := rtSwitchRaceSettled(kernel, other, 30*time.Second); last != "" {
		t.Errorf("после отката окна переноса активным должно остаться %s: %s", kernel, last)
	}
}

// Трасса: подготовка (оба ядра установлены, DNS-секции обоих у панели) → включение и
// «Оставить» на активном ядре → смена ядра через API служб → окно переноса (state window,
// window.kind=kernel_switch) → перезапуск панели init-скриптом, окно не закрыто → старт
// панели откатывает неподтверждённый перенос (D-15, D-07): state off, desired=false,
// окна и открытого черновика нет, override как до включения, :53 у прошивки, слушателя нет
// в файлах ни одного ядра, активным остаётся новое ядро (смену ядра откат DNS не
// отменяет). Прерывание посреди прохода переноса недостижимо: проба по живой группе
// занимает доли секунды, а остановка панели на шаге перезапуска ядра не укладывается в
// срок. Метка ядра: сценарий меняет активное ядро.
func TestRouterDNSKernelSwitchWindowPanelRestart(t *testing.T) {
	routertest.Slow(t, "смена ядра туда и обратно и перезапуск панели посреди окна переноса")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	other := rtDNSKernelSwitchPrepare(t, e, kernel)
	overrideBefore := rtDNSOverrideOn(t)
	on := e.rtDNSEnableOK()
	old := rtPanelCurrent(t, e.p)

	e.rtDNSSwitchKernel(other)
	e.rtDNSExpectSwitchWindow(other, rtDNSLastCheckAt(on))
	res := rtPanelRestart(t, e.p, old, nil)
	rtPanelRestartVerdict(t, old, res, "перезапуск в окне подтверждения переноса перехвата DNS")

	off := e.rtDNSWaitState("off", rtDNSWindowS*time.Second)
	e.rtDNSExpectRolledBack(off, other, overrideBefore)
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после отката переноса в файлах прежнего ядра %s слушатель :53", kernel)
	}
	if _, last := rtSwitchRaceSettled(other, kernel, 30*time.Second); last != "" {
		t.Errorf("после отката переноса активным должно остаться %s: %s", other, last)
	}
}

// Трасса: подготовка → включение и «Оставить» → остановка ядра через API служб (state
// waiting, kernel_stopped) → группа-цель с мёртвым узлом (проба внешнего имени идёт до
// 20 с; при остановленном ядре проба применения не нужна) → запуск ядра → шаг probe
// возврата running → перезапуск панели init-скриптом → старт панели откатывает прерванный
// проход возврата (D-14, D-16): DNS у прошивки (override как до включения, слушателя в
// файлах ядра нет, прошивка отвечает), перехват остаётся желаемым с причиной ожидания
// probe, окна нет. Возврат живой группой без прерывания проверяет TestRouterDNSKernelStop.
// Метка ядра: сценарий останавливает и запускает активное ядро.
func TestRouterDNSRestorePanelRestart(t *testing.T) {
	routertest.Slow(t, "остановка и запуск ядра, перезапуск панели посреди возврата перехвата DNS")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSEnableOK()
	t.Cleanup(func() {
		c := e.with(t)
		if pids, _ := rtRstKernelPIDs(kernel); len(pids) == 0 {
			c.rtDNSServiceControl("start")
			rtCoresLiveWaitStatus(t)
		}
		c.rtDNSEnsureOff()
	})
	e.rtDNSServiceControl("stop")
	e.rtDNSExpectKernelStopped(kernel, overrideBefore)
	e.rtDNSDeadGroup(t)
	old := rtPanelCurrent(t, e.p)

	e.rtDNSServiceControl("start")
	e.rtDNSWaitStep("probe", "running", rtDNSStateWait)
	res := rtPanelRestart(t, e.p, old, nil)
	rtPanelRestartVerdict(t, old, res, "перезапуск посреди пробы возврата перехвата DNS")

	v := e.rtDNSWaitState("waiting", rtDNSStateWait)
	if reason, _ := v["waiting_reason"].(string); reason != "probe" {
		t.Errorf("после отката возврата waiting_reason %q, ожидалось probe", reason)
	}
	if desired, _ := v["desired"].(bool); !desired {
		t.Error("после отката возврата desired=false, перехват должен остаться желаемым")
	}
	if w := rtDNSWindow(v); w != nil {
		t.Errorf("после отката возврата окно осталось: %v", w)
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после отката возврата порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после отката возврата прошивка не отвечает через 127.0.0.1:53")
	}
	if got := rtDNSOverrideOn(t); got != overrideBefore {
		t.Errorf("после отката возврата override=%v, до включения был %v", got, overrideBefore)
	}
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после отката возврата в файлах %s остался слушатель :53", kernel)
	}
}
