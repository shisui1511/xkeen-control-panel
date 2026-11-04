//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Сторож DNS роутера (D-13) на настоящей панели устройства: упавшее ядро-
// перехватчик поднимается сторожем, ядро, сменённое мимо панели (командой XKeen в
// терминале), возвращает DNS прошивке. Вердикт — факты устройства: процессы ядер
// (/proc), кто держит порт 53, ответ DNS через 127.0.0.1:53, override в рабочей
// конфигурации прошивки, история действий XKeen. Стенд готовит и возвращает
// rtDNSPrepare (router_dns_enable_test.go).

const (
	// rtDNSGuardWait — сколько ждать, пока сторож поднимет упавшее ядро: три
	// промаха по 5 с, перезапуск ядра и повторная проба через 15 с.
	rtDNSGuardWait = 90 * time.Second
	// rtDNSGuardChangedWait — сколько ждать возврата DNS прошивке после смены ядра
	// мимо панели.
	rtDNSGuardChangedWait = 30 * time.Second
	// rtDNSGuardQuietAfter — пауза сторожа после перезапуска ядра из панели (окно
	// штатного перезапуска XKeen, 60 с) с запасом.
	rtDNSGuardQuietAfter = 65 * time.Second
	// rtDNSGuardRestartAction — запись истории действий: перезапуск ядра сторожем.
	rtDNSGuardRestartAction = "dns_takeover_guard_restart"
)

// rtDNSRestartEntry — запись истории действий XKeen (GET /api/service/restart-log).
type rtDNSRestartEntry struct {
	Timestamp int64  `json:"timestamp"`
	Action    string `json:"action"`
	Success   bool   `json:"success"`
}

// rtDNSRestartLog читает историю действий XKeen.
func (e *rtSrc) rtDNSRestartLog() []rtDNSRestartEntry {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/service/restart-log", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/service/restart-log: %d", r.Status)
	}
	var entries []rtDNSRestartEntry
	if err := json.Unmarshal(r.Raw, &entries); err != nil {
		e.t.Fatalf("история действий не разбирается: %v", err)
	}
	return entries
}

// rtDNSActionSince — в истории действий есть успешная запись action не раньше since
// (unix-секунды).
func (e *rtSrc) rtDNSActionSince(action string, since int64) bool {
	e.t.Helper()
	for _, en := range e.rtDNSRestartLog() {
		if en.Timestamp >= since && en.Action == action && en.Success {
			return true
		}
	}
	return false
}

// rtDNSGuardQuiet ждёт конца паузы сторожа после последнего запуска, перезапуска
// или смены ядра из панели (окно штатного перезапуска XKeen): иначе проверка
// сторожа меряет паузу, а не его реакцию.
func (e *rtSrc) rtDNSGuardQuiet() {
	e.t.Helper()
	var last int64
	for _, en := range e.rtDNSRestartLog() {
		if en.Action == "restart" || en.Action == "start" || strings.HasPrefix(en.Action, "switch_kernel") {
			last = max(last, en.Timestamp)
		}
	}
	if wait := time.Until(time.Unix(last, 0).Add(rtDNSGuardQuietAfter)); wait > 0 {
		e.t.Logf("пауза сторожа после перезапуска ядра: ждём %.0f с", wait.Seconds())
		time.Sleep(wait)
	}
}

// rtDNSKill9 гасит процессы ядра сигналом SIGKILL (так же процесс гасит нехватка
// памяти) и возвращает погашенные PID.
func rtDNSKill9(t *testing.T, kernel string) []int {
	t.Helper()
	pids, out := rtRstKernelPIDs(kernel)
	if len(pids) == 0 {
		t.Fatalf("ядро %s не запущено перед сбоем: %s", kernel, out)
	}
	for _, pid := range pids {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, stderr, code := routertest.Exec(ctx, "kill", "-9", strconv.Itoa(pid))
		cancel()
		if code != 0 {
			t.Fatalf("kill -9 %d (%s): код %d %s", pid, kernel, code, strings.TrimSpace(stderr))
		}
	}
	return pids
}

// rtDNSOtherKernel — второе ядро.
func rtDNSOtherKernel(t *testing.T, kernel string) string {
	t.Helper()
	other := map[string]string{configlayer.KernelXray: configlayer.KernelMihomo, configlayer.KernelMihomo: configlayer.KernelXray}[kernel]
	if other == "" {
		t.Fatalf("неизвестное ядро цели %q", kernel)
	}
	return other
}

// Трасса: подготовка → включение и «Оставить» → конец паузы сторожа после
// перезапуска ядра включением → kill -9 процесса ядра-перехватчика (сбой мимо
// панели) → в пределах 90 с ядро снова запущено новым процессом, state on, :53 у
// ядра, внешнее имя разрешается, в истории действий — успешный перезапуск ядра
// сторожем. Метка ядра: сценарий гасит активное ядро.
func TestRouterDNSGuard(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	e.rtDNSEnableOK()
	t.Cleanup(func() {
		c := e.with(t)
		if pids, _ := rtRstKernelPIDs(kernel); len(pids) == 0 {
			c.rtDNSServiceControl("start")
			rtCoresLiveWaitStatus(t)
		}
		c.rtDNSEnsureOff()
	})
	e.rtDNSGuardQuiet()

	since := time.Now().Unix()
	killed := rtDNSKill9(t, kernel)
	deadline := time.Now().Add(rtDNSGuardWait)
	var v map[string]any
	for {
		pids, _ := rtRstKernelPIDs(kernel)
		fresh := len(pids) > 0 && !slices.ContainsFunc(pids, func(p int) bool { return slices.Contains(killed, p) })
		v = e.rtDNSView()
		st, _ := v["state"].(string)
		if fresh && st == "on" && dnstakeover.PortOwner().Kind == dnstakeover.OwnerKernel {
			break
		}
		if time.Now().After(deadline) {
			code, detail := rtDNSFailure(v)
			t.Fatalf("сторож не поднял %s за %v после kill -9: процессы %v (погашены %v), state %q, :53 %v, сбой %q %.300s; шаги %v",
				kernel, rtDNSGuardWait, pids, killed, st, dnstakeover.PortOwner(), code, detail, v["steps"])
		}
		time.Sleep(time.Second)
	}
	e.rtDNSExpectOn(v, kernel)
	if !e.rtDNSActionSince(rtDNSGuardRestartAction, since) {
		t.Errorf("в истории действий нет успешной записи %s после сбоя ядра", rtDNSGuardRestartAction)
	}
	if o := rtDNSOutcome(v); o != nil {
		t.Errorf("после подъёма ядра сторожем исход %v, ожидалось без исхода (DNS прошивке не возвращался)", o)
	}
}

// Трасса: подготовка (оба ядра установлены, иначе пропуск) → включение и
// «Оставить» на активном ядре → конец паузы сторожа → смена ядра командой XKeen
// (`xkeen -<другое>`, мимо панели) → в пределах 30 с state off, desired=false,
// исход kernel_changed с from и to и toast_pending, :53 у прошивки и она отвечает,
// override как до включения, в файлах прежнего ядра нет слушателя. Стенд
// возвращает исходное ядро через API служб и выключает перехват. Метка ядра:
// сценарий меняет активное ядро.
func TestRouterDNSGuardKernelChanged(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	other := rtDNSOtherKernel(t, kernel)
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	if installed, _ := e.rtCoresView(other)["installed"].(bool); !installed {
		t.Skipf("%s не установлен на стенде: ядро некуда сменить", other)
	}
	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSEnableOK()
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
	e.rtDNSGuardQuiet()

	rtDNSXKeen(t, "-"+other)
	v := e.rtDNSWaitState("off", rtDNSGuardChangedWait)
	if desired, _ := v["desired"].(bool); desired {
		t.Error("после смены ядра мимо панели desired=true")
	}
	o := rtDNSOutcome(v)
	if kind, _ := o["kind"].(string); kind != "kernel_changed" {
		t.Errorf("исход после смены ядра мимо панели %v, ожидался kind=kernel_changed", o)
	}
	if from, _ := o["from"].(string); from != kernel {
		t.Errorf("outcome.from %q, ожидалось %s", from, kernel)
	}
	if to, _ := o["to"].(string); to != other {
		t.Errorf("outcome.to %q, ожидалось %s", to, other)
	}
	if pending, _ := o["toast_pending"].(bool); !pending {
		t.Errorf("исход kernel_changed без toast_pending: %v", o)
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после смены ядра мимо панели порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после смены ядра мимо панели прошивка не отвечает через 127.0.0.1:53")
	}
	if got := rtDNSOverrideOn(t); got != overrideBefore {
		t.Errorf("после смены ядра мимо панели override=%v, до включения был %v", got, overrideBefore)
	}
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после смены ядра мимо панели в файлах %s остался слушатель :53", kernel)
	}
	if !e.rtDNSOutcomeNoticeShown() {
		t.Errorf("после смены ядра мимо панели в снимке слоя нет уведомления %s*", rtDNSOutcomeNotice)
	}
}

// --- сверка на старте панели (D-16, R3, R8) -----------------------------------

const (
	// rtDNSReconcileRebootWait — сколько ждать возврата перехвата после старта
	// панели в имитации загрузки роутера: сверка (файлы без слушателя, подъём
	// ядра), пауза сторожа после старта и перезапуска ядра, возврат пробой.
	rtDNSReconcileRebootWait = 240 * time.Second
	// rtDNSReconcileOverrideWait — сколько ждать снятия лишнего override.
	rtDNSReconcileOverrideWait = 60 * time.Second
	// rtDNSReconcileAction — запись истории действий: сверка на старте.
	rtDNSReconcileAction = "dns_takeover_reconcile"
	// rtDNSReconcileOverrideAction — запись истории действий: лишний override снят.
	rtDNSReconcileOverrideAction = "dns_takeover_reconcile_override"
)

// rtDNSPanelStop останавливает панель init-скриптом и ждёт выхода процесса old.
func rtDNSPanelStop(t *testing.T, old int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), rtPanelRestartLimit+30*time.Second)
	defer cancel()
	out, code := rtPanelInitCmd(ctx, "stop")
	deadline := time.Now().Add(rtPanelRestartLimit)
	for {
		if _, err := os.Stat("/proc/" + strconv.Itoa(old)); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("панель %d не вышла за %v после stop (init: код %d, %q)", old, rtPanelRestartLimit, code, out)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// rtDNSPanelStart запускает панель init-скриптом и ждёт новую панель, отвечающую
// по прежней сессии.
func rtDNSPanelStart(t *testing.T, e *rtSrc, old int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, code := rtPanelInitCmd(ctx, "start")
	if pid, last := rtPanelWaitUp(e.p, old, rtPanelUpTimeout); pid == 0 {
		t.Fatalf("панель не поднялась за %v после start: %s (init: код %d, %q)", rtPanelUpTimeout, last, code, out)
	}
}

// rtDNSRestoreOverride возвращает override прошивки к значению before.
func rtDNSRestoreOverride(t *testing.T, before bool) {
	t.Helper()
	switch on := rtDNSOverrideOn(t); {
	case on && !before:
		rtDNSNdmc(t, "no "+rtDNSOverrideLine)
	case !on && before:
		rtDNSNdmc(t, rtDNSOverrideLine)
	}
}

// Метка ядра: подтест reboot перезапускает активное ядро и ждёт его подъёма.
func TestRouterDNSStartReconcile(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core

	// Трасса: подготовка → включение и «Оставить» → остановка панели
	// init-скриптом → `no opkg dns-override` (override не пережил перезагрузку) →
	// `xkeen -restart` (как загрузка роутера: ядро стартует с файлами, где слушатель
	// :53, а прошивка держит порт) → запуск панели → в пределах 240 с state on, :53
	// у ядра, ядро запущено, внешнее имя разрешается, в истории действий — сверка
	// на старте. t.Cleanup: override исходный, панель запущена, перехват выключен.
	t.Run("reboot", func(t *testing.T) {
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
			rtDNSRestoreOverride(t, overrideBefore)
		})
		old := rtPanelCurrent(t, e.p)

		rtDNSPanelStop(t, old)
		if rtDNSOverrideOn(t) {
			rtDNSNdmc(t, "no "+rtDNSOverrideLine)
		}
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			// Ядро с файлами слушателя :53 при занятом прошивкой порте может не
			// подняться (Xray) — код XKeen здесь не вердикт.
			_, stderr, code := routertest.Exec(ctx, config.Default().XKeenBinary, "-restart")
			pids, _ := rtRstKernelPIDs(kernel)
			t.Logf("xkeen -restart без override: код %d, процессов %s: %d %.200s", code, kernel, len(pids), strings.TrimSpace(stderr))
		}()
		since := time.Now().Unix()
		rtDNSPanelStart(t, e, old)

		deadline := time.Now().Add(rtDNSReconcileRebootWait)
		var v map[string]any
		for {
			v = e.rtDNSView()
			st, _ := v["state"].(string)
			pids, _ := rtRstKernelPIDs(kernel)
			owner := dnstakeover.PortOwner()
			if st == "on" && len(pids) > 0 && owner.Kind == dnstakeover.OwnerKernel && owner.Kernel == kernel {
				break
			}
			if time.Now().After(deadline) {
				code, detail := rtDNSFailure(v)
				t.Fatalf("перехват не вернулся за %v после старта панели: state %q (waiting_reason %v), процессов %s %d, :53 %v, сбой %q %.300s; шаги %v",
					rtDNSReconcileRebootWait, st, v["waiting_reason"], kernel, len(pids), owner, code, detail, v["steps"])
			}
			time.Sleep(2 * time.Second)
		}
		e.rtDNSExpectOn(v, kernel)
		if !rtDNSOverrideOn(t) {
			t.Error("после возврата перехвата override выключен")
		}
		if !e.rtDNSActionSince(rtDNSReconcileAction, since) {
			t.Errorf("в истории действий нет успешной записи %s после старта панели", rtDNSReconcileAction)
		}
	})

	// Трасса: слой включён, перехват выключен → остановка панели → `opkg
	// dns-override` из теста (override сохранён без панели, R3) → запуск панели →
	// в пределах 60 с override выключен, :53 у прошивки, прошивка отвечает через
	// 127.0.0.1:53, в истории действий — снятие override. t.Cleanup: override
	// исходный, панель запущена.
	t.Run("stale_override", func(t *testing.T) {
		e := rtNewSrc(t)
		e.enableLayer()
		e.rtRstIdle()
		e.rtDNSEnsureOff()
		overrideBefore := rtDNSOverrideOn(t)
		t.Cleanup(func() { rtDNSRestoreOverride(t, overrideBefore) })
		old := rtPanelCurrent(t, e.p)

		rtDNSPanelStop(t, old)
		if !rtDNSOverrideOn(t) {
			rtDNSNdmc(t, rtDNSOverrideLine)
		}
		since := time.Now().Unix()
		rtDNSPanelStart(t, e, old)

		deadline := time.Now().Add(rtDNSReconcileOverrideWait)
		for rtDNSOverrideOn(t) {
			if time.Now().After(deadline) {
				t.Fatalf("лишний override не снят за %v после старта панели: :53 %v", rtDNSReconcileOverrideWait, dnstakeover.PortOwner())
			}
			time.Sleep(2 * time.Second)
		}
		if !rtDNSResolves(dnstakeover.DefaultCheckName) {
			t.Error("после снятия override прошивка не отвечает через 127.0.0.1:53")
		}
		if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
			t.Errorf("после снятия override порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
		}
		if !e.rtDNSActionSince(rtDNSReconcileOverrideAction, since) {
			t.Errorf("в истории действий нет успешной записи %s после старта панели", rtDNSReconcileOverrideAction)
		}
	})
}
