//go:build router

// router-area: dns

package handlers

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Возврат DNS роутера прошивке, который не удался с первого раза, на настоящей
// панели устройства: неудачное выключение повторяется (кнопкой и выключением флага
// слоя), запись файлов без слушателя :53 не теряется из-за занятого замка
// жизненного цикла, а откат «Применить» по пробе не оставляет окно, которое потом
// включает слушатель :53 при снятом override. Отказ вызывается настоящим сбоем
// устройства: правила iptables закрывают 127.0.0.1:53, и панель не получает ответа
// прошивки, или идёт перезапуск ядра из API служб. Вердикт — вид службы, override
// в рабочей конфигурации прошивки, слушатель панели в файлах ядра, строка proxy_dns,
// владелец порта 53 по /proc. Стенд готовит и возвращает rtDNSPrepare.

// rtDNSLoopbackRules — правила, закрывающие 127.0.0.1:53 для процессов роутера.
var rtDNSLoopbackRules = [][]string{
	{"OUTPUT", "-o", "lo", "-d", "127.0.0.1", "-p", "udp", "--dport", "53", "-j", "DROP"},
	{"OUTPUT", "-o", "lo", "-d", "127.0.0.1", "-p", "tcp", "--dport", "53", "-j", "DROP"},
}

// rtDNSIptables выполняет iptables с действием op над правилом rule; код выхода.
func rtDNSIptables(op string, rule []string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, code := routertest.Exec(ctx, "iptables", append([]string{"-w", op}, rule...)...)
	return code
}

// rtDNSBlockLoopback53 закрывает 127.0.0.1:53 (DROP в OUTPUT на lo для udp и tcp):
// ни прошивка, ни ядро не отвечают панели через loopback. Пока правила нужны,
// горутина возвращает их, если цепочку перестроили. unblock снимает правила
// (повторный вызов безопасен); t.Cleanup снимает их при любом исходе раньше
// возврата стенда, зарегистрированного до вызова.
func rtDNSBlockLoopback53(t *testing.T) (unblock func()) {
	t.Helper()
	ensure := func() {
		for _, r := range rtDNSLoopbackRules {
			if rtDNSIptables("-C", r) != 0 {
				rtDNSIptables("-I", r)
			}
		}
	}
	ensure()
	for _, r := range rtDNSLoopbackRules {
		if rtDNSIptables("-C", r) != 0 {
			t.Fatalf("правило iptables не поставлено: %v", r)
		}
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			ensure()
		}
	}()
	var once sync.Once
	unblock = func() {
		once.Do(func() {
			close(stop)
			wg.Wait()
			for _, r := range rtDNSLoopbackRules {
				for i := 0; i < 5 && rtDNSIptables("-C", r) == 0; i++ {
					rtDNSIptables("-D", r)
				}
				if rtDNSIptables("-C", r) == 0 {
					t.Errorf("правило iptables не снято: %v", r)
				}
			}
		})
	}
	t.Cleanup(unblock)
	return unblock
}

// rtDNSExpectOff — перехват выключен и DNS у прошивки: state off без сбоя,
// desired=false, override как до включения, порт 53 у прошивки и она отвечает,
// слушателя панели в файлах ядра нет.
func (e *rtSrc) rtDNSExpectOff(v map[string]any, kernel string, overrideBefore bool) {
	e.t.Helper()
	if st, _ := v["state"].(string); st != "off" {
		code, detail := rtDNSFailure(v)
		e.t.Errorf("state %q, ожидалось off: сбой %q %.300s; шаги %v", st, code, detail, v["steps"])
	}
	if code, detail := rtDNSFailure(v); code != "" {
		e.t.Errorf("после выключения сбой %q: %.300s", code, detail)
	}
	if desired, _ := v["desired"].(bool); desired {
		e.t.Error("после выключения desired=true")
	}
	if got := rtDNSOverrideOn(e.t); got != overrideBefore {
		e.t.Errorf("после выключения override=%v, до включения был %v", got, overrideBefore)
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		e.t.Errorf("после выключения порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if rtDNSKernelHasListener(kernel) {
		e.t.Errorf("после выключения в файлах %s остался слушатель :53", kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		e.t.Error("после выключения прошивка не отвечает через 127.0.0.1:53")
	}
}

// Трасса: подготовка → release_failed: перехват включён и подтверждён → 127.0.0.1:53
// закрыт → «Выключить»: проверка ответа прошивки не проходит, сбой → перехват
// остаётся желаемым → 127.0.0.1:53 открыт → повторное «Выключить» принято (не
// not_enabled) → state off, override как до включения, :53 у прошивки, слушателя
// нет. kernel_busy: перехват включён → перезапуск ядра из API служб держит замок
// жизненного цикла → «Выключить» во время перезапуска → запись без слушателя ждёт
// замок → state off без сбоя, слушателя в файлах ядра нет. Метка ядра: перехват
// ведёт активное ядро, слушатель — в его файлах.
func TestRouterDNSDisableRetry(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	t.Run("release_failed", func(t *testing.T) {
		c := e.with(t)
		overrideBefore := rtDNSOverrideOn(t)
		c.rtDNSEnableOK()
		t.Cleanup(func() { c.rtDNSEnsureOff() })

		unblock := rtDNSBlockLoopback53(t)
		c.rtDNSStart(rtDNSDisable)
		v := c.rtDNSWaitDone()
		unblock()
		code, _ := rtDNSFailure(v)
		if code == "" {
			t.Fatalf("выключение при закрытом 127.0.0.1:53 прошло без сбоя: state %q, шаги %v", v["state"], v["steps"])
		}
		if desired, _ := v["desired"].(bool); !desired {
			t.Errorf("после неудачного выключения (сбой %q) desired=false: перехват должен остаться желаемым", code)
		}

		r := c.rtDNSPost(rtDNSDisable)
		if r.Status != http.StatusOK {
			t.Fatalf("повторное выключение после сбоя %q: %d %s, ожидалось 200 started", code, r.Status, r.Code)
		}
		c.rtDNSExpectOff(c.rtDNSWaitDone(), kernel, overrideBefore)
	})

	t.Run("kernel_busy", func(t *testing.T) {
		c := e.with(t)
		overrideBefore := rtDNSOverrideOn(t)
		c.rtDNSEnableOK()
		t.Cleanup(func() { c.rtDNSEnsureOff() })

		type restartResp struct {
			status int
			err    error
		}
		done := make(chan restartResp, 1)
		go func() {
			status, _, err := c.p.CallSlow(http.MethodPost, "/api/service/control?action=restart", nil)
			done <- restartResp{status, err}
		}()
		// Обработчик служб держит замок жизненного цикла, пока идёт xkeen -restart.
		time.Sleep(700 * time.Millisecond)
		select {
		case r := <-done:
			t.Fatalf("перезапуск ядра закончился раньше выключения (%d %v): замок жизненного цикла не пойман", r.status, r.err)
		default:
		}
		c.rtDNSStart(rtDNSDisable)
		if r := <-done; r.err != nil || r.status != http.StatusOK {
			t.Errorf("перезапуск ядра во время выключения: %d %v", r.status, r.err)
		}
		rtCoresLiveWaitStatus(t)
		c.rtDNSExpectOff(c.rtDNSWaitDone(), kernel, overrideBefore)
	})
}

// Трасса: подготовка → proxy_dns=on и действующий перехват XKeen с :53 → перехват
// включён и подтверждён (строка off) → 127.0.0.1:53 закрыт → выключение флага слоя
// не удаётся, флаг остаётся включённым → 127.0.0.1:53 открыт → повторное выключение
// флага: 200, флаг снят, строка proxy_dns вернулась в on (D-36), override как до
// включения, :53 у прошивки, слушателя панели нет, в файле состояния desired=false.
// Слой включается обратно до возврата стенда. Метка ядра: возврат перезапускает
// активное ядро.
func TestRouterDNSFlagOffRetry(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	rtDNSRestoreProxyLine(t, e, rtDNSProxyOn(t))
	t.Cleanup(func() {
		c := e.with(t)
		c.setLayer(true)
		c.rtRstIdle()
	})
	rtDNSLegacyOn(t, e)
	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSRequireClosedDraft()
	e.rtDNSEnableOK()
	if rtDNSProxyOn(t) {
		t.Fatal("после включения перехвата строка proxy_dns осталась on")
	}

	unblock := rtDNSBlockLoopback53(t)
	r := e.callSlow(http.MethodPost, "/api/settings/config-layer", map[string]bool{"enabled": false})
	unblock()
	if r.Status == http.StatusOK {
		t.Fatal("выключение флага слоя при закрытом 127.0.0.1:53 прошло: ожидался сбой возврата DNS прошивке")
	}
	if !e.layerFlag() {
		t.Fatal("после неудачного выключения флаг слоя снят")
	}

	r = e.callSlow(http.MethodPost, "/api/settings/config-layer", map[string]bool{"enabled": false})
	if r.Status != http.StatusOK {
		t.Fatalf("повторное выключение флага слоя: %d %s %.300s", r.Status, r.Code, r.Detail)
	}
	if e.layerFlag() {
		t.Fatal("после повторного выключения флаг слоя остался включён")
	}
	if !rtDNSProxyOn(t) {
		t.Error("после повторного выключения флага строка proxy_dns не вернулась в on")
	}
	if got := rtDNSOverrideOn(t); got != overrideBefore {
		t.Errorf("после повторного выключения флага override=%v, до включения был %v", got, overrideBefore)
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после повторного выключения флага порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		t.Error("после повторного выключения флага прошивка не отвечает через 127.0.0.1:53")
	}
	if rtDNSKernelHasListener(kernel) {
		t.Errorf("после повторного выключения флага в файлах %s остался слушатель :53", kernel)
	}
	if desired, window := rtDNSStateFile(t); desired || window {
		t.Errorf("после повторного выключения флага в состоянии перехвата desired=%v, окно=%v", desired, window)
	}
}

// rtDNSApplyReleaseWait — сколько ждать отката «Применить» по пробе при закрытом
// 127.0.0.1:53: две пробы по два имени с повторами, возврат к снимку с
// перезапуском ядра и возврат DNS прошивке.
const rtDNSApplyReleaseWait = 6 * time.Minute

// rtDNSWaitStep ждёт, пока шаг id получит статус want, не дольше limit.
func (e *rtSrc) rtDNSWaitStep(id, want string, limit time.Duration) {
	e.t.Helper()
	deadline := time.Now().Add(limit)
	for {
		v := e.rtDNSView()
		if rtDNSStepStatus(v, id) == want {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("шаг %s не стал %s за %v: state %q, шаги %v", id, want, limit, v["state"], v["steps"])
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// rtDNSWaitSettled ждёт, пока операции службы закончатся и окно закроется: два
// наблюдения подряд с паузой 3 с (между операциями отката есть короткий зазор).
func (e *rtSrc) rtDNSWaitSettled(limit time.Duration) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(limit)
	calm := 0
	for {
		v := e.rtDNSView()
		if st, _ := v["state"].(string); st != "applying" && st != "window" {
			calm++
		} else {
			calm = 0
		}
		if calm >= 2 {
			return v
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("операции перехвата не закончились за %v: state %q, шаги %v", limit, v["state"], v["steps"])
		}
		time.Sleep(3 * time.Second)
	}
}

// Трасса: подготовка → перехват включён и подтверждён → сервер в DNS-секции ядра →
// «Применить» → окно apply → 127.0.0.1:53 закрыт → вторая правка DNS-секции →
// «Применить» → проба не проходит, откат к первому применению, повторная проба не
// проходит → на шаге возврата DNS прошивке 127.0.0.1:53 открыт → дедлайн окна
// прошёл, операции закончились: state waiting (перехват желаем, DNS у прошивки),
// окна нет, сбоя rollback_failed нет, слушателя панели в файлах ядра нет, override
// как до включения, :53 у прошивки. Метка ядра: DNS-секция и перехват — у
// активного ядра.
func TestRouterDNSApplyReleaseWindow(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	overrideBefore := rtDNSOverrideOn(t)

	e.rtDNSEnableOK()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	section := configlayer.CoreSectionName(kernel, "dns")
	orig := rtDNSLayerApplied(t)[section]
	t.Cleanup(func() { e.with(t).rtDNSRestoreSection(section, orig) })

	e.rtDNSAddServer(kernel, rtDNSApplyServer)
	v := e.rtDNSApplyChecked("сервер в DNS-секции при перехвате")
	w := rtDNSWindow(v)
	if kind, _ := w["kind"].(string); kind != "apply" {
		code, detail := rtDNSFailure(v)
		t.Fatalf("после «Применить» DNS-секции окно %v, ожидалось apply: state %q, сбой %q %.300s", w, v["state"], code, detail)
	}
	deadline := time.UnixMilli(rtDNSNum(w, "deadline_ms"))

	unblock := rtDNSBlockLoopback53(t)
	e.rtDNSAddServer(kernel, rtDNSUserServer)
	e.rtRtApplyOK("вторая правка DNS-секции при закрытом 127.0.0.1:53")
	// Проба и повторная проба не проходят, затем DNS отдаётся прошивке: ответ
	// прошивки на шаге возврата должен дойти.
	e.rtDNSWaitStep("release", "running", rtDNSApplyReleaseWait)
	unblock()

	if d := time.Until(deadline); d > 0 {
		time.Sleep(d + 5*time.Second)
	}
	v = e.rtDNSWaitSettled(rtDNSApplyReleaseWait)
	if st, _ := v["state"].(string); st != "waiting" {
		code, detail := rtDNSFailure(v)
		t.Errorf("после отката «Применить» по пробе state %q, ожидалось waiting (DNS у прошивки): сбой %q %.300s", st, code, detail)
	}
	if w := rtDNSWindow(v); w != nil {
		t.Errorf("после возврата DNS прошивке окно осталось: %v", w)
	}
	if code, detail := rtDNSFailure(v); code == "rollback_failed" {
		t.Errorf("после дедлайна окна сбой rollback_failed: %.300s", detail)
	}
	override := rtDNSOverrideOn(t)
	if override != overrideBefore {
		t.Errorf("после возврата DNS прошивке override=%v, до включения был %v", override, overrideBefore)
	}
	if !override && rtDNSKernelHasListener(kernel) {
		t.Errorf("override снят, а в файлах %s слушатель :53", kernel)
	}
	if got := dnstakeover.PortOwner(); !override && got.Kind != dnstakeover.OwnerFirmware {
		t.Errorf("после возврата DNS прошивке порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
}

// Трасса: подготовка → группа-цель перехвата с единственным мёртвым узлом (проба
// внешнего имени идёт до 20 с) → «Включить» → шаг probe running (Live перехвата уже
// включён, окна ещё нет) → перезапуск панели init-скриптом → остановка уложилась в
// срок, старт панели откатывает неподтверждённый перехват (D-07): state off,
// desired=false, окна нет, override как до включения, :53 у прошивки, слушателя
// панели в файлах ядра нет, черновик закрыт. Метка ядра: откат перезапускает
// активное ядро, слушатель — в его файлах.
func TestRouterDNSEnablePanelRestart(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	e.rtDNSDeadGroup(t)
	e.rtDNSRequireClosedDraft()
	overrideBefore := rtDNSOverrideOn(t)
	old := rtPanelCurrent(t, e.p)
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })

	e.rtDNSStart(rtDNSEnable)
	e.rtDNSWaitStep("probe", "running", rtDNSStateWait)
	res := rtPanelRestart(t, e.p, old, nil)
	rtPanelRestartVerdict(t, old, res, "перезапуск посреди пробы включения перехвата DNS")
	off := e.rtDNSWaitState("off", rtDNSWindowS*time.Second)
	e.rtDNSExpectRolledBack(off, kernel, overrideBefore)
}
