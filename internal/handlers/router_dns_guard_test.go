//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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
