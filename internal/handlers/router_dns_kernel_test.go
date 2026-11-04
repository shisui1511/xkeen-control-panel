//go:build router

package handlers

import (
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
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
