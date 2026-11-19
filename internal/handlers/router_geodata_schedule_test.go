//go:build router

// router-area: geodata

package handlers

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/rtenv"
)

// Расписание обновления баз панелью на настоящем устройстве, в настоящем времени и
// на настоящей панели. Служебного вызова «выполнить сейчас» нет (D-29): сценарии
// ставят настоящее расписание через API и ждут слот по часам роутера.
//
//   - TestRouterGeodataScheduleWaitsApply: слот наступает при идущем «Применить» —
//     запуск ждёт (run.waiting_apply) и стартует после конца применения (D-29, D-11).
//   - TestRouterGeodataCatchUp: слот прошёл, пока панель была остановлена init-скриптом, —
//     запуск через 3 минуты после старта и ровно один за период (D-29, D-14).
//
// Сценарии обходят окно 05:20–05:45 по часам роутера (снимок стенда и cron XKeen) и
// возвращают расписание панели и crontab в исходное состояние.

const (
	// rtGeoCronFile — crontab root стенда (путь сервиса менеджера по умолчанию).
	rtGeoCronFile = "/opt/var/spool/cron/crontabs/root"
	// rtGeoRunLimit — предел ожидания конца настоящего запуска `xkeen -ug`.
	rtGeoRunLimit = 20 * time.Minute
)

// rtGeoSched — расписание панели из снимка.
type rtGeoSched struct {
	enabled           bool
	day, hour, minute int
	suspended         bool
	now               time.Time
	hasNext           bool
}

// rtGeoSection возвращает вложенный объект снимка.
func rtGeoSection(snap map[string]any, key string) map[string]any {
	m, _ := snap[key].(map[string]any)
	return m
}

// rtGeoSchedOf разбирает расписание и часы роутера из снимка.
func rtGeoSchedOf(t *testing.T, snap map[string]any) rtGeoSched {
	t.Helper()
	sch := rtGeoSection(snap, "schedule")
	if sch == nil {
		t.Fatalf("в снимке менеджера нет schedule")
	}
	var s rtGeoSched
	s.enabled, _ = sch["enabled"].(bool)
	s.suspended, _ = sch["suspended"].(bool)
	day, _ := sch["day"].(float64)
	hour, _ := sch["hour"].(float64)
	minute, _ := sch["minute"].(float64)
	s.day, s.hour, s.minute = int(day), int(hour), int(minute)
	nowText, _ := sch["now"].(string)
	now, err := time.Parse(time.RFC3339, nowText)
	if err != nil {
		t.Fatalf("schedule.now %q не время: %v", nowText, err)
	}
	s.now = now
	_, s.hasNext = sch["next"]
	return s
}

// rtGeoRun читает булев признак объекта run снимка.
func rtGeoRun(snap map[string]any, key string) bool {
	v, _ := rtGeoSection(snap, "run")[key].(bool)
	return v
}

// rtGeoLastAt возвращает last_run.at снимка; нулевое время — запусков не было.
func rtGeoLastAt(t *testing.T, snap map[string]any) time.Time {
	t.Helper()
	text, _ := rtGeoSection(snap, "last_run")["at"].(string)
	if text == "" {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("last_run.at %q не время: %v", text, err)
	}
	return at
}

// rtGeoSkipWindowAt пропускает тест, если отрезок от now на span (часы роутера, как их
// отдала панель) задевает окно 05:20–05:45: снимок стенда и cron XKeen.
func rtGeoSkipWindowAt(t *testing.T, now time.Time, span time.Duration) {
	t.Helper()
	end := now.Add(span)
	for off := 0; off <= 1; off++ {
		from := time.Date(now.Year(), now.Month(), now.Day()+off, 5, 20, 0, 0, now.Location())
		to := from.Add(25 * time.Minute)
		if now.Before(to) && end.After(from) {
			t.Skip("часы стенда: отрезок теста задевает окно 05:20-05:45 (cron XKeen и снимок)")
		}
	}
}

// rtGeoCeilMinute — ближайшая целая минута не раньше at.
func rtGeoCeilMinute(at time.Time) time.Time {
	slot := at.Truncate(time.Minute)
	if slot.Before(at) {
		slot = slot.Add(time.Minute)
	}
	return slot
}

// rtGeoSetSchedule ставит расписание панели настоящим вызовом API.
func (e *rtSrc) rtGeoSetSchedule(enabled bool, day, hour, minute int) {
	e.t.Helper()
	r := e.call(http.MethodPost, rtGeoBase+"/schedule", map[string]any{"enabled": enabled, "day": day, "hour": hour, "minute": minute})
	if r.Status != http.StatusOK {
		e.t.Fatalf("POST schedule {enabled: %v, day: %d, hour: %d, minute: %d}: %d %s", enabled, day, hour, minute, r.Status, r.Code)
	}
}

// rtGeoWaitIdle ждёт, пока менеджер не идёт и не ждёт «Применить» (три опроса подряд).
func (e *rtSrc) rtGeoWaitIdle(limit time.Duration) {
	e.t.Helper()
	deadline := time.Now().Add(limit)
	calm := 0
	for calm < 3 {
		if time.Now().After(deadline) {
			e.t.Fatalf("менеджер geodata не освободился за %v", limit)
		}
		snap := e.rtGeoSnapshot()
		if rtGeoRun(snap, "running") || rtGeoRun(snap, "waiting_apply") {
			calm = 0
		} else {
			calm++
		}
		time.Sleep(time.Second)
	}
}

// rtGeoKeepState запоминает расписание панели и crontab; в t.Cleanup возвращает
// crontab дословно и расписание — прежними значениями. Расписание, которое панель сама
// приостановила (осталось от прежних версий панели), сценарий не проверяет: слот такого расписания
// не срабатывает.
func (e *rtSrc) rtGeoKeepState() rtGeoSched {
	e.t.Helper()
	orig := rtGeoSchedOf(e.t, e.rtGeoSnapshot())
	if orig.suspended {
		e.t.Skip("расписание панели приостановлено (осталось от прежних версий панели): слот не сработает до «Перенять расписание»")
	}
	cron, readErr := os.ReadFile(rtGeoCronFile)
	e.t.Cleanup(func() {
		rtGeoRestoreCron(e.t, cron, readErr == nil)
		r := e.call(http.MethodPost, rtGeoBase+"/schedule", map[string]any{"enabled": orig.enabled, "day": orig.day, "hour": orig.hour, "minute": orig.minute})
		if r.Status != http.StatusOK {
			e.t.Errorf("возврат расписания: %d %s", r.Status, r.Code)
		}
	})
	return orig
}

// rtGeoRestoreCron возвращает crontab дословно: файла не было — файл убирается.
func rtGeoRestoreCron(t *testing.T, data []byte, existed bool) {
	t.Helper()
	cur, err := os.ReadFile(rtGeoCronFile)
	if existed {
		if err == nil && string(cur) == string(data) {
			return
		}
		mode := os.FileMode(0o600)
		if st, err := os.Stat(rtGeoCronFile); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.WriteFile(rtGeoCronFile, data, mode); err != nil {
			t.Errorf("возврат crontab: %v", err)
		}
		return
	}
	if err == nil {
		if err := os.Remove(rtGeoCronFile); err != nil {
			t.Errorf("возврат crontab (файла не было): %v", err)
		}
	}
}

// rtGeoNeedXKeen пропускает тест, если XKeen не установлен.
func rtGeoNeedXKeen(t *testing.T) {
	t.Helper()
	bin := config.Default().XKeenBinary
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("XKeen не установлен (%s): запуск -ug невозможен", bin)
	}
}

// rtGeoSkipNetwork пропускает тест, если последний запуск целиком упал из-за сети
// стенда: итог запуска тогда не про планировщик.
func rtGeoSkipNetwork(t *testing.T, snap map[string]any) {
	t.Helper()
	if reason, _ := rtGeoSection(snap, "last_run")["reason"].(string); reason == "run_network" {
		t.Skip("сеть стенда: запуск -ug упал по run_network, порядок запуска проверен выше")
	}
}

// rtGeoNodeCleanup: в конце сценария «Моих узлов» нет ни в черновике, ни в применённом.
func (e *rtSrc) rtGeoNodeCleanup() {
	e.t.Helper()
	e.t.Cleanup(func() {
		e.rtRstIdle()
		id := e.rtRstMyEntry()
		if id == "" {
			return
		}
		e.rtRstDeleteMyNode(id)
		if e.rtRtDraftChanges() > 0 {
			before := e.rtRstStartApply()
			e.rtRstWaitApply(before)
			e.rtRstIdle()
		}
	})
}

// rtGeoToggleNode меняет черновик так, чтобы «Применить» переписал файлы ядра и
// перезапустил его: добавляет узел «Моих узлов», а если он уже есть — удаляет.
func (e *rtSrc) rtGeoToggleNode(link string) {
	e.t.Helper()
	if id := e.rtRstMyEntry(); id != "" {
		e.rtRstDeleteMyNode(id)
		return
	}
	e.rtRstAddMyNode(link)
}

// TestRouterGeodataScheduleWaitsApply: слот расписания наступает, пока идёт «Применить»
// (D-29, D-11, D-14). Расписание ставится настоящим вызовом API на ближайшую целую
// минуту через 90 с и больше по часам роутера; за 3 с до слота запускается
// «Применить» с изменением черновика (запись файлов ядра и его перезапуск). Запуск
// -ug обязан ждать (run.waiting_apply=true), стартовать после конца применения и
// закончиться своим итогом. Применение закончилось раньше слота — слот сдвигается
// на следующий запуск, один повтор.
func TestRouterGeodataScheduleWaitsApply(t *testing.T) {
	routertest.Tier(t, routertest.Full, "реальное время: слот расписания geodata наступает через 90 с и больше по часам роутера")
	rtGeoNeedXKeen(t)
	e := rtNewSrc(t)
	e.freshDraft()
	e.rtRstIdle()
	e.rtGeoKeepState()
	e.rtGeoNodeCleanup()
	link := rtLinkText(t, "vless_ws")

	// Опережение слота началом «Применить». На Mihomo применение (горячая подмена) длится
	// около 2 с и при опережении 3 с кончается до слота; повтор берёт половину
	// измеренной длительности, чтобы слот пришёлся на середину применения.
	lead := 3 * time.Second
	var applyDur time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		e.rtGeoWaitIdle(rtGeoRunLimit)
		// Тест идёт на самом роутере, часы процесса и панели одни: слот считается по часам
		// процесса, без привязки к schedule.now. Привязка по запросу снимка давала погрешность
		// длительности запроса: после «-ug» снимок строится десятки секунд, schedule.now
		// несёт время построения с точностью до секунды, и «Применить» стартовало уже после
		// слота (409 geodata_busy). Совпадение часов проверяется по самому запросу.
		reqStart := time.Now()
		snap := e.rtGeoSnapshot()
		reqEnd := time.Now()
		sch := rtGeoSchedOf(t, snap)
		if sch.now.Before(reqStart.Add(-time.Second)) || sch.now.After(reqEnd) {
			t.Fatalf("schedule.now %s вне запроса снимка %s…%s: часы теста и панели расходятся",
				sch.now.Format(time.RFC3339), reqStart.Format(time.RFC3339), reqEnd.Format(time.RFC3339))
		}
		rtGeoSkipWindowAt(t, sch.now, 15*time.Minute)
		lastAt0 := rtGeoLastAt(t, snap)

		// слот по часам процесса, час и минута — в поясе панели
		slotLocal := rtGeoCeilMinute(time.Now().Add(90 * time.Second))
		slot := slotLocal.In(sch.now.Location())
		e.rtGeoSetSchedule(true, -1, slot.Hour(), slot.Minute())
		e.rtGeoToggleNode(link)

		// запись расписания и узла после «-ug» может занять десятки секунд: если запас
		// слота ушёл, «Применить» всё равно пришло бы после слота
		if time.Until(slotLocal.Add(-lead)) < 0 {
			t.Fatalf("попытка %d: подготовка слота %02d:%02d заняла дольше запаса, до слота осталось %v",
				attempt, slot.Hour(), slot.Minute(), time.Until(slotLocal).Round(time.Second))
		}
		time.Sleep(time.Until(slotLocal.Add(-lead)))
		applyStart := time.Now()
		e.rtRstStartApply()

		var (
			sawWaiting, sawRunning bool
			lastApplyRunning       time.Time
			applyEnd               time.Time
			lastAt                 time.Time
		)
		deadline := slotLocal.Add(rtGeoRunLimit)
		for {
			if time.Now().After(deadline) {
				t.Fatalf("попытка %d: запуск по расписанию не закончился за %v после слота (waiting_apply=%v, running=%v)", attempt, rtGeoRunLimit, sawWaiting, sawRunning)
			}
			// состояние «Применить» снимается раньше снимка менеджера и сразу: его время —
			// точка наблюдения; запрос снимка во время применения может занять секунды
			applyRunning, _ := e.rtRstApplyView()["running"].(bool)
			now := time.Now()
			snap = e.rtGeoSnapshot()
			if applyRunning {
				lastApplyRunning = now
			} else if applyEnd.IsZero() && !lastApplyRunning.IsZero() {
				applyEnd = now
			}
			if rtGeoRun(snap, "waiting_apply") {
				sawWaiting = true
			}
			running := rtGeoRun(snap, "running")
			if running {
				sawRunning = true
			}
			lastAt = rtGeoLastAt(t, snap)
			if lastAt.After(lastAt0) && !running && !rtGeoRun(snap, "waiting_apply") {
				break
			}
			// waiting_apply живёт, пока идёт применение (на Mihomo около секунды): пока оно не
			// закончилось, опрос частый, иначе флаг не успевает попасть в выборку
			if applyEnd.IsZero() {
				time.Sleep(50 * time.Millisecond)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}
		if !lastApplyRunning.IsZero() && !applyEnd.IsZero() {
			applyDur = applyEnd.Sub(applyStart)
		}
		t.Logf("попытка %d: слот %02d:%02d, применение длилось около %.1f с, waiting_apply=%v, running=%v, last_run.at=%s",
			attempt, slot.Hour(), slot.Minute(), applyDur.Seconds(), sawWaiting, sawRunning, lastAt.Format(time.RFC3339))
		e.rtRstIdle()
		if ok, code := rtRstResult(e.rtRstApplyView()); !ok {
			t.Errorf("«Применить» во время слота закончилось не успешно: код %q", code)
		}

		if !sawWaiting {
			if attempt == 2 {
				t.Fatalf("waiting_apply не наблюдался за две попытки: применение заканчивалось раньше слота (длительность последнего около %.1f с, запуск -ug по слоту ушёл без ожидания)", applyDur.Seconds())
			}
			if applyDur > 0 {
				lead = applyDur / 2
				if lead < 500*time.Millisecond {
					lead = 500 * time.Millisecond
				}
			}
			t.Logf("применение закончилось раньше слота: повтор со сдвигом слота, опережение %.1f с", lead.Seconds())
			continue
		}
		// Запуск стартовал после слота и после конца применения: его время не раньше
		// последнего наблюдения идущего применения. Время в запуске — с точностью до секунды,
		// а состояние «Применить» гаснет чуть позже отпускания замка, поэтому допуск — секунда.
		if lastAt.Before(slot.Truncate(time.Second)) {
			t.Errorf("last_run.at %s раньше слота %s", lastAt.Format(time.RFC3339), slot.Format(time.RFC3339))
		}
		if !lastApplyRunning.IsZero() && lastAt.Add(time.Second).Before(lastApplyRunning.Truncate(time.Second)) {
			t.Errorf("запуск -ug начался (%s) раньше, чем закончилось «Применить» (последнее наблюдение идущего применения %s)",
				lastAt.Format(time.RFC3339), lastApplyRunning.Format(time.RFC3339))
		}
		rtGeoSkipNetwork(t, snap)
		return
	}
}

// TestRouterGeodataCatchUp: догон слота после перезапуска панели (D-29, D-14). Слот
// ставится через 2–3 минуты, панель останавливается init-скриптом S99xcp (stop и start,
// как в router_panel_restart_test.go), слот проходит, панель
// запускается — пропущенный слот запускается через 3 минуты после старта, ровно один раз
// за период (повторного запуска нет).
func TestRouterGeodataCatchUp(t *testing.T) {
	routertest.Tier(t, routertest.Full, "реальное время: догоняющее обновление geodata ждёт минуты")
	rtGeoNeedXKeen(t)
	routertest.RealTime(t, rtenv.GeodataCatchUp)
	e := rtNewSrc(t)
	e.freshDraft()
	e.rtRstIdle()
	e.rtGeoKeepState()
	old := rtPanelCurrent(t, e.p)
	e.rtGeoWaitIdle(rtGeoRunLimit)

	snap := e.rtGeoSnapshot()
	base := time.Now()
	sch := rtGeoSchedOf(t, snap)
	rtGeoSkipWindowAt(t, sch.now, 20*time.Minute)
	lastAt0 := rtGeoLastAt(t, snap)

	slot := rtGeoCeilMinute(sch.now.Add(2 * time.Minute))
	slotLocal := base.Add(slot.Sub(sch.now))
	e.rtGeoSetSchedule(true, -1, slot.Hour(), slot.Minute())

	// У каждой команды init-скрипта свой срок: между stop и start тест ждёт слот и ещё 30 с,
	// общий контекст истёк бы раньше запуска панели.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stopCancel()
	out, code := rtPanelInitCmd(stopCtx, "stop")
	if code != 0 || strings.Contains(out, "failed") {
		t.Fatalf("остановка панели init-скриптом: код %d, %q", code, out)
	}
	if time.Until(slotLocal) < 10*time.Second {
		t.Fatalf("остановка панели заняла слишком долго: до слота осталось %v", time.Until(slotLocal).Round(time.Second))
	}
	time.Sleep(time.Until(slotLocal.Add(30 * time.Second)))

	startCtx, startCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer startCancel()
	out, code = rtPanelInitCmd(startCtx, "start")
	startedAt := time.Now()
	if code != 0 || strings.Contains(out, "failed") {
		t.Fatalf("запуск панели init-скриптом: код %d, %q", code, out)
	}
	pid, last := rtPanelWaitUp(e.p, old, rtPanelUpTimeout)
	if pid == 0 {
		t.Fatalf("панель не поднялась за %s: %s", rtPanelUpTimeout, last)
	}

	// Пропущенный слот запускается через задержку догона после старта панели
	// (catchUpDelay сервиса менеджера; срок — у нового процесса панели).
	catchUp := routertest.Deadline(t, rtenv.GeodataCatchUp)
	var waited time.Duration
	deadline := startedAt.Add(catchUp + 90*time.Second)
	for {
		snap = e.rtGeoSnapshot()
		if rtGeoRun(snap, "running") || rtGeoLastAt(t, snap).After(lastAt0) {
			waited = time.Since(startedAt)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("догон не запустился за %v после старта панели (слот %02d:%02d прошёл, пока панель стояла)",
				catchUp+90*time.Second, slot.Hour(), slot.Minute())
		}
		time.Sleep(time.Second)
	}
	if waited < catchUp-time.Minute {
		t.Errorf("догон запустился через %.0f с после старта панели, раньше задержки %v", waited.Seconds(), catchUp)
	}

	// Запуск идёт до конца, итог — новая запись last_run после слота.
	runStart := time.Now()
	finishDeadline := runStart.Add(rtGeoRunLimit)
	for {
		snap = e.rtGeoSnapshot()
		if !rtGeoRun(snap, "running") && !rtGeoRun(snap, "waiting_apply") && rtGeoLastAt(t, snap).After(lastAt0) {
			break
		}
		if time.Now().After(finishDeadline) {
			t.Fatalf("запуск догона не закончился за %v", rtGeoRunLimit)
		}
		time.Sleep(time.Second)
	}
	runDur := time.Since(runStart)
	lastAt := rtGeoLastAt(t, snap)
	t.Logf("догон: запуск через %.0f с после старта панели, идёт ещё около %.0f с, last_run.at=%s", waited.Seconds(), runDur.Seconds(), lastAt.Format(time.RFC3339))
	if lastAt.Before(slot.Truncate(time.Second)) {
		t.Errorf("last_run.at %s раньше слота %s", lastAt.Format(time.RFC3339), slot.Format(time.RFC3339))
	}
	if lastAt.Before(startedAt.Truncate(time.Second)) {
		t.Errorf("last_run.at %s раньше старта панели: запуск не догонял слот после перезапуска", lastAt.Format(time.RFC3339))
	}

	// Один запуск за период: следующего нет.
	time.Sleep(60 * time.Second)
	after := e.rtGeoSnapshot()
	if again := rtGeoLastAt(t, after); !again.Equal(lastAt) || rtGeoRun(after, "running") {
		t.Errorf("второй запуск за период: last_run.at был %s, стал %s (running=%v)",
			lastAt.Format(time.RFC3339), again.Format(time.RFC3339), rtGeoRun(after, "running"))
	}
	rtGeoSkipNetwork(t, snap)
}
