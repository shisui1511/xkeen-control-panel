//go:build router

// router-area: dns

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/rtenv"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/dnstakeover"
)

// Окно подтверждения перехвата DNS роутера на настоящей панели устройства
// (DNSF-05, D-05…D-07): после включения state window с окном 60 с; «Оставить»
// закрывает его (повтор — тот же ответ), «Откатить сейчас» и истёкший таймер
// возвращают DNS прошивке, строку proxy_dns и конфиг ядра к снимку до включения.
// Вердикт — факты устройства: кто держит порт 53 (/proc), override в рабочей
// конфигурации прошивки, слушатель панели в файлах ядра, применённое состояние в
// файле слоя. Стенд готовит и возвращает rtDNSPrepare (router_dns_enable_test.go).

const (
	rtDNSRollbackPath    = rtDNSBase + "/rollback"
	rtDNSOutcomeSeenPath = rtDNSBase + "/outcome-seen"
	// rtDNSOutcomeNotice — префикс уведомления слоя об исходе без пользователя.
	rtDNSOutcomeNotice = "dns_outcome:"
)

// rtDNSWindowDur — окно подтверждения, которое действует у процесса панели
// (XCP_RT_DNS_WINDOW_S, по умолчанию 60 с; D-05): тест не держит копию срока.
func rtDNSWindowDur(t testing.TB) time.Duration {
	t.Helper()
	return routertest.Deadline(t, rtenv.DNSWindow)
}

// rtDNSTimeoutWaitOf — сколько ждать автоотката: окно панели плюс откат с
// перезапуском ядра. Берётся в момент ожидания (после возможного перезапуска панели).
func rtDNSTimeoutWaitOf(t testing.TB) time.Duration {
	t.Helper()
	return rtDNSWindowDur(t) + rtDNSStateWait
}

// rtDNSWaitState ждёт состояния want не дольше limit и возвращает вид.
func (e *rtSrc) rtDNSWaitState(want string, limit time.Duration) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(limit)
	for {
		v := e.rtDNSView()
		if st, _ := v["state"].(string); st == want {
			return v
		}
		if time.Now().After(deadline) {
			code, detail := rtDNSFailure(v)
			e.t.Fatalf("state %q не наступило за %v: state %q, сбой %q %.300s; шаги %v", want, limit, v["state"], code, detail, v["steps"])
		}
		time.Sleep(2 * time.Second)
	}
}

// rtDNSNum — число из вида (JSON number).
func rtDNSNum(m map[string]any, key string) int64 {
	f, _ := m[key].(float64)
	return int64(f)
}

// rtDNSLayerApplied — применённое состояние слоя из его файла состояния в
// каталоге данных панели.
func rtDNSLayerApplied(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.Default().DataDir, configlayer.StateFileName))
	if err != nil {
		t.Fatalf("файл состояния слоя не читается: %v", err)
	}
	var st struct {
		Applied map[string]json.RawMessage `json:"applied"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("файл состояния слоя не разбирается: %v", err)
	}
	return st.Applied
}

// rtDNSAppliedDiff — секции, которыми различаются два применённых состояния.
func rtDNSAppliedDiff(a, b map[string]json.RawMessage) []string {
	var out []string
	for k, av := range a {
		if bv, ok := b[k]; !ok || !configlayer.SectionEqual(av, bv) {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// rtDNSOutcome — исход из вида (nil — исхода нет).
func rtDNSOutcome(v map[string]any) map[string]any {
	o, _ := v["outcome"].(map[string]any)
	return o
}

// rtDNSExpectTimeoutOutcome — исход автоотката: kind timeout, toast_pending=true.
func (e *rtSrc) rtDNSExpectTimeoutOutcome(v map[string]any) {
	e.t.Helper()
	o := rtDNSOutcome(v)
	if kind, _ := o["kind"].(string); kind != "timeout" {
		e.t.Errorf("исход после автоотката %v, ожидался kind=timeout", o)
	}
	if pending, _ := o["toast_pending"].(bool); !pending {
		e.t.Errorf("исход после автоотката без toast_pending: %v", o)
	}
}

// rtDNSOutcomeNoticeShown — в снимке слоя есть уведомление об исходе перехвата.
func (e *rtSrc) rtDNSOutcomeNoticeShown() bool {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	list, _ := r.object(e.t)["notices"].([]any)
	for _, it := range list {
		n, _ := it.(map[string]any)
		if id, _ := n["id"].(string); strings.HasPrefix(id, rtDNSOutcomeNotice) {
			return true
		}
	}
	return false
}

// rtDNSExpectRolledBack — после отката окна включения: state off, desired=false,
// порт 53 у прошивки, прошивка отвечает через 127.0.0.1:53, override как до
// включения, в файлах ядра нет слушателя панели, черновик закрыт.
func (e *rtSrc) rtDNSExpectRolledBack(v map[string]any, kernel string, overrideBefore bool) {
	e.t.Helper()
	if desired, _ := v["desired"].(bool); desired {
		e.t.Error("после отката окна desired=true")
	}
	if w := rtDNSWindow(v); w != nil {
		e.t.Errorf("после отката окно осталось: %v", w)
	}
	if got := dnstakeover.PortOwner(); got.Kind != dnstakeover.OwnerFirmware {
		e.t.Errorf("после отката порт 53 держит %q/%q, ожидалась прошивка", got.Kind, got.Kernel)
	}
	if !rtDNSResolves(dnstakeover.DefaultCheckName) {
		e.t.Error("после отката прошивка не отвечает через 127.0.0.1:53")
	}
	if got := rtDNSOverrideOn(e.t); got != overrideBefore {
		e.t.Errorf("после отката override=%v, до включения был %v", got, overrideBefore)
	}
	if rtDNSKernelHasListener(kernel) {
		e.t.Errorf("после отката в файлах %s остался слушатель :53", kernel)
	}
	if n := e.rtRtDraftChanges(); n != 0 {
		e.t.Errorf("после отката в черновике %d изменений, ожидался закрытый черновик", n)
	}
}

// Трасса: подготовка → включение → state window, window.kind=enable, total_s равен
// окну у процесса панели, до дедлайна от окна минус 10 с до окна по часам сервера, блокировка window, выключение в окне —
// 409 window_active → «Оставить» → state on, действующий перехват → повторное
// «Оставить» (вторая вкладка) → 200, state on. Метка ядра: перехват ведёт активное ядро.
func TestRouterDNSWindowKeep(t *testing.T) {
	routertest.Slow(t, "включение перехвата DNS с перезапуском ядра и окном подтверждения")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	v := e.rtDNSEnableWindow()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	win := rtDNSWindowDur(t)
	w := rtDNSWindow(v)
	if total := rtDNSNum(w, "total_s"); total != int64(win/time.Second) {
		t.Errorf("window.total_s %d, ожидалось %d", total, int64(win/time.Second))
	}
	left := time.Duration(rtDNSNum(w, "deadline_ms")-rtDNSNum(v, "server_now_ms")) * time.Millisecond
	if left < win-10*time.Second || left > win {
		t.Errorf("до дедлайна окна %v по часам сервера, ожидалось от %v до %v", left, win-10*time.Second, win)
	}
	if !e.rtDNSBlocks()["window"] {
		t.Error("в окне нет блокировки window")
	}
	if r := e.rtDNSPost(rtDNSDisable); r.Status != http.StatusConflict || r.Code != "window_active" {
		t.Errorf("выключение в окне: %d %q, ожидалось 409 window_active", r.Status, r.Code)
	}

	kept := e.rtDNSKeep()
	e.rtDNSExpectOn(kept, kernel)
	if st := rtDNSStepStatus(kept, "window"); st != "done" {
		t.Errorf("шаг window после «Оставить» %q, ожидалось done", st)
	}

	t.Run("repeat", func(t *testing.T) {
		c := e.with(t)
		r := c.rtDNSPost(rtDNSKeepPath)
		if r.Status != http.StatusOK {
			t.Fatalf("повторное «Оставить»: %d %s, ожидалось 200", r.Status, r.Code)
		}
		if st, _ := r.object(t)["state"].(string); st != "on" {
			t.Errorf("повторное «Оставить»: state %q, ожидалось on", st)
		}
	})
}

// Трасса: подготовка → снимок фактов (override, файлы ядра, Applied слоя) →
// включение → state window → ожидание без «Оставить» → откат по таймеру не раньше
// дедлайна: state off, :53 у прошивки, override как до включения, файлы ядра без
// слушателя и равны снимку, Applied слоя равен снимку до включения, черновик
// закрыт → исход timeout, уведомление, outcome-seen, следующее включение снимает
// исход. Метка ядра: откат перезапускает активное ядро.
func TestRouterDNSWindowTimeout(t *testing.T) {
	routertest.Tier(t, routertest.Full, "реальное время: окно подтверждения перехвата DNS ждёт дедлайн и откат по таймеру")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	routertest.RealTime(t, rtenv.DNSWindow)
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	e.rtDNSRequireClosedDraft()
	overrideBefore := rtDNSOverrideOn(t)
	files := rtDNSKernelFiles(t, kernel)
	applied := rtDNSLayerApplied(t)
	v := e.rtDNSEnableWindow()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	deadline := rtDNSNum(rtDNSWindow(v), "deadline_ms")

	off := e.rtDNSWaitState("off", rtDNSTimeoutWaitOf(t))
	if now := rtDNSNum(off, "server_now_ms"); now < deadline {
		t.Errorf("откат за %v до дедлайна окна: ожидался откат по таймеру", time.Duration(deadline-now)*time.Millisecond)
	}
	if code, detail := rtDNSFailure(off); code != "" {
		t.Errorf("после автооткатa сбой %q: %.300s", code, detail)
	}
	e.rtDNSExpectRolledBack(off, kernel, overrideBefore)
	if !rtCoresSumsEqual(files, rtDNSKernelFiles(t, kernel)) {
		t.Error("файлы ядра после автооткатa не равны снимку до включения")
	}
	if diff := rtDNSAppliedDiff(applied, rtDNSLayerApplied(t)); len(diff) > 0 {
		t.Errorf("Applied слоя после автооткатa отличается от снимка до включения секциями %v", diff)
	}

	// D-28: исход без пользователя — строка в карточке, уведомление слоя dns_,
	// однократный тост (outcome-seen снимает toast_pending); следующее включение
	// снимает исход и уведомление.
	t.Run("outcome", func(t *testing.T) {
		c := e.with(t)
		c.rtDNSExpectTimeoutOutcome(off)
		if !c.rtDNSOutcomeNoticeShown() {
			t.Errorf("после автоотката в снимке слоя нет уведомления %s*", rtDNSOutcomeNotice)
		}
		r := c.rtDNSPost(rtDNSOutcomeSeenPath)
		if r.Status != http.StatusOK {
			t.Fatalf("POST %s: %d %s", rtDNSOutcomeSeenPath, r.Status, r.Code)
		}
		o := rtDNSOutcome(r.object(t))
		if kind, _ := o["kind"].(string); kind != "timeout" {
			t.Errorf("после outcome-seen исход %v, ожидалось — остался timeout", o)
		}
		if pending, _ := o["toast_pending"].(bool); pending {
			t.Error("после outcome-seen toast_pending=true")
		}
		v := c.rtDNSEnableWindow()
		if o := rtDNSOutcome(v); o != nil {
			t.Errorf("после следующего включения исход остался: %v", o)
		}
		if c.rtDNSOutcomeNoticeShown() {
			t.Errorf("после следующего включения уведомление %s* осталось", rtDNSOutcomeNotice)
		}
		c.rtDNSEnsureOff()
	})
}

// Трасса: подготовка → включение → state window → «Откатить сейчас» →
// {started:true} → state off, DNS прошивке, override как до включения, слушателя
// нет → повторное «Откатить сейчас» и «Оставить» без окна — 409 no_window. Метка
// ядра: откат перезапускает активное ядро.
func TestRouterDNSWindowRollbackNow(t *testing.T) {
	routertest.Slow(t, "включение перехвата DNS и «Откатить сейчас»: два перезапуска ядра")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)

	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSEnableWindow()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })
	e.rtDNSStart(rtDNSRollbackPath)
	off := e.rtDNSWaitState("off", rtDNSStateWait)
	if code, detail := rtDNSFailure(off); code != "" {
		t.Errorf("после «Откатить сейчас» сбой %q: %.300s", code, detail)
	}
	e.rtDNSExpectRolledBack(off, kernel, overrideBefore)

	t.Run("no_window", func(t *testing.T) {
		c := e.with(t)
		for _, path := range []string{rtDNSRollbackPath, rtDNSKeepPath} {
			if r := c.rtDNSPost(path); r.Status != http.StatusConflict || r.Code != "no_window" {
				t.Errorf("POST %s без окна: %d %q, ожидалось 409 no_window", path, r.Status, r.Code)
			}
		}
	})
}

// Трасса: подготовка → включение → state window → перезапуск панели
// init-скриптом посреди окна (rtPanelRestart: новая панель отвечает по прежней
// сессии) → старт панели откатывает неподтверждённый перехват сразу, не дожидаясь
// дедлайна (D-07): state off в пределах окна панели после подъёма, :53 у прошивки,
// override как до включения, файлы ядра без слушателя, исход timeout с
// toast_pending и уведомление слоя (открытая вкладка после переподключения видит
// «Откачено: нет подтверждения»). Метка ядра: откат перезапускает активное ядро.
func TestRouterDNSWindowPanelRestart(t *testing.T) {
	routertest.Slow(t, "перезапуск панели init-скриптом посреди окна, откат при старте")
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	e := rtNewSrc(t)
	rtDNSPrepare(t, e, kernel)
	old := rtPanelCurrent(t, e.p)

	overrideBefore := rtDNSOverrideOn(t)
	e.rtDNSEnableWindow()
	t.Cleanup(func() { e.with(t).rtDNSEnsureOff() })

	res := rtPanelRestart(t, e.p, old, nil)
	rtPanelRestartVerdict(t, old, res, "перезапуск посреди окна подтверждения DNS")
	off := e.rtDNSWaitState("off", rtDNSWindowDur(t))
	e.rtDNSExpectRolledBack(off, kernel, overrideBefore)
	e.rtDNSExpectTimeoutOutcome(off)
	if !e.rtDNSOutcomeNoticeShown() {
		t.Errorf("после отката на старте панели в снимке слоя нет уведомления %s*", rtDNSOutcomeNotice)
	}
}
