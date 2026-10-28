//go:build router

package handlers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
)

// Расписание обновления баз панелью на настоящем устройстве, в настоящем времени и
// на настоящей панели. Служебного вызова «выполнить сейчас» нет (D-29): сценарии
// ставят настоящее расписание через API и ждут слот по часам роутера.
//
//   - TestRouterGeodataScheduleWaitsApply: слот наступает при идущем «Применить» —
//     запуск ждёт (run.waiting_apply) и стартует после конца применения (D-29, D-11).
//   - TestRouterGeodataCatchUp: слот прошёл, пока панель была остановлена init-скриптом, —
//     запуск через 3 минуты после старта и ровно один за период (D-29, D-14).
//   - TestRouterGeodataFlagCron: возврат cron XKeen и приостановка расписания при
//     настоящем переключении флага слоя в панели (D-13, D-36).
//
// Сценарии обходят окно 05:20–05:45 по часам роутера (снимок стенда и cron XKeen) и
// возвращают расписание панели, crontab и флаг слоя в исходное состояние.

const (
	// rtGeoCronFile — crontab root стенда (путь сервиса менеджера по умолчанию).
	rtGeoCronFile = "/opt/var/spool/cron/crontabs/root"
	// rtGeoCatchUpDelay — через сколько после старта панели запускается пропущенный слот
	// (catchUpDelay сервиса менеджера).
	rtGeoCatchUpDelay = 3 * time.Minute
	// rtGeoRunLimit — предел ожидания конца настоящего запуска `xkeen -ug`.
	rtGeoRunLimit = 20 * time.Minute
	// rtGeoOwnLine — строка задачи XKeen, если в crontab стенда её нет (время задачи стенда).
	rtGeoOwnLine = "30 5 * * * /opt/sbin/xkeen -ug"
)

// rtGeoCronXKeen — стандартная строка XKeen: минута, час и день недели.
var rtGeoCronXKeen = regexp.MustCompile(`^\s*(\d{1,2})\s+(\d{1,2})\s+\*\s+\*\s+([0-6*])\s+/opt/sbin/xkeen\s+-ug\s*$`)

// rtGeoUGLine — любая действующая строка crontab с командой `xkeen -ug`.
var rtGeoUGLine = regexp.MustCompile(`^\s*[^#\s].*/opt/sbin/xkeen\s+-ug(\s|$)`)

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

// rtGeoCron вызывает действие над cron XKeen.
func (e *rtSrc) rtGeoCron(action string) rtSrcResp {
	e.t.Helper()
	return e.call(http.MethodPost, rtGeoBase+"/cron", map[string]any{"action": action})
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
// приостановила (флаг слоя выключали), сценарий не проверяет: слот такого расписания
// не срабатывает.
func (e *rtSrc) rtGeoKeepState() rtGeoSched {
	e.t.Helper()
	orig := rtGeoSchedOf(e.t, e.rtGeoSnapshot())
	if orig.suspended {
		e.t.Skip("расписание панели приостановлено (флаг слоя выключали): слот не сработает до «Перенять расписание»")
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
	rtGeoNeedXKeen(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	e.rtGeoKeepState()
	e.rtGeoNodeCleanup()
	link := rtLinkText(t, "vless_ws")

	var applyDur time.Duration
	for attempt := 1; attempt <= 2; attempt++ {
		e.rtGeoWaitIdle(rtGeoRunLimit)
		snap := e.rtGeoSnapshot()
		base := time.Now()
		sch := rtGeoSchedOf(t, snap)
		rtGeoSkipWindowAt(t, sch.now, 15*time.Minute)
		lastAt0 := rtGeoLastAt(t, snap)

		slot := rtGeoCeilMinute(sch.now.Add(90 * time.Second))
		slotLocal := base.Add(slot.Sub(sch.now))
		e.rtGeoSetSchedule(true, -1, slot.Hour(), slot.Minute())
		e.rtGeoToggleNode(link)

		if wait := time.Until(slotLocal.Add(-3 * time.Second)); wait > 0 {
			time.Sleep(wait)
		}
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
			snap = e.rtGeoSnapshot()
			applyRunning, _ := e.rtRstApplyView()["running"].(bool)
			now := time.Now()
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
			time.Sleep(500 * time.Millisecond)
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
			t.Logf("применение закончилось раньше слота: повтор со сдвигом слота")
			continue
		}
		// Запуск стартовал после слота и после конца применения: его время не раньше
		// последнего наблюдения идущего применения (время в запуске — с точностью до секунды).
		if lastAt.Before(slot.Truncate(time.Second)) {
			t.Errorf("last_run.at %s раньше слота %s", lastAt.Format(time.RFC3339), slot.Format(time.RFC3339))
		}
		if !lastApplyRunning.IsZero() && lastAt.Before(lastApplyRunning.Truncate(time.Second)) {
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
	rtGeoNeedXKeen(t)
	e := rtNewSrc(t)
	e.enableLayer()
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

	// Пропущенный слот запускается через rtGeoCatchUpDelay после старта панели.
	var waited time.Duration
	deadline := startedAt.Add(rtGeoCatchUpDelay + 90*time.Second)
	for {
		snap = e.rtGeoSnapshot()
		if rtGeoRun(snap, "running") || rtGeoLastAt(t, snap).After(lastAt0) {
			waited = time.Since(startedAt)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("догон не запустился за %v после старта панели (слот %02d:%02d прошёл, пока панель стояла)",
				rtGeoCatchUpDelay+90*time.Second, slot.Hour(), slot.Minute())
		}
		time.Sleep(time.Second)
	}
	if waited < rtGeoCatchUpDelay-time.Minute {
		t.Errorf("догон запустился через %.0f с после старта панели, раньше задержки %v", waited.Seconds(), rtGeoCatchUpDelay)
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

// rtGeoCronLines возвращает строки crontab с командой `xkeen -ug` (комментарии не
// считаются); нет файла — нет строк.
func rtGeoCronLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(rtGeoCronFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("чтение crontab: %v", err)
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimRight(l, "\r")
		if rtGeoUGLine.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

// TestRouterGeodataFlagCron: возврат cron XKeen и приостановка расписания при настоящем
// переключении флага слоя в панели (D-13, D-36; проверяется и проводка
// settings.go → OnFlagOff/OnFlagOn). Панель перенимает строку cron; выключение флага
// возвращает строку в crontab дословно; включение оставляет расписание приостановленным —
// само оно не запускается и строку не снимает, пока не нажато «Перенять расписание».
func TestRouterGeodataFlagCron(t *testing.T) {
	if _, err := os.Stat(filepath.Dir(rtGeoCronFile)); err != nil {
		t.Skipf("каталога crontab нет: %v", err)
	}
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()

	origSnap := e.rtGeoSnapshot()
	orig := rtGeoSchedOf(t, origSnap)
	if orig.suspended {
		t.Skip("расписание панели приостановлено (флаг слоя выключали): исходное владение строкой неизвестно")
	}
	rtGeoSkipWindowAt(t, orig.now, 10*time.Minute)
	origCron, readErr := os.ReadFile(rtGeoCronFile)
	origExisted := readErr == nil
	cronFound := rtGeoSection(origSnap, "cron")["found"] == true
	panelOwned := !cronFound && orig.enabled

	// Возврат состояния: слой включён, строку получает XKeen, при исходном владении
	// расписанием панель перенимает её снова, затем crontab и расписание — прежними.
	t.Cleanup(func() {
		e.setLayer(true)
		e.rtRstIdle()
		if r := e.rtGeoCron("give"); r.Status != http.StatusOK {
			t.Errorf("возврат: cron give: %d %s", r.Status, r.Code)
		}
		if panelOwned {
			if r := e.rtGeoCron("adopt"); r.Status != http.StatusOK {
				t.Errorf("возврат: cron adopt: %d %s", r.Status, r.Code)
			}
		}
		rtGeoRestoreCron(t, origCron, origExisted)
		r := e.call(http.MethodPost, rtGeoBase+"/schedule", map[string]any{"enabled": orig.enabled, "day": orig.day, "hour": orig.hour, "minute": orig.minute})
		if r.Status != http.StatusOK {
			t.Errorf("возврат расписания: %d %s", r.Status, r.Code)
		}
	})

	// Исходное владение панели: строки у XKeen нет, расписание ведёт панель — строка
	// возвращается XKeen, чтобы дальше проверять перенятие.
	if panelOwned {
		if r := e.rtGeoCron("give"); r.Status != http.StatusOK {
			t.Fatalf("cron give при исходном владении панели: %d %s", r.Status, r.Code)
		}
	}
	lines := rtGeoCronLines(t)
	if len(lines) == 0 {
		cur, _ := os.ReadFile(rtGeoCronFile)
		text := string(cur)
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		mode := os.FileMode(0o600)
		if st, err := os.Stat(rtGeoCronFile); err == nil {
			mode = st.Mode().Perm()
		}
		if err := os.WriteFile(rtGeoCronFile, []byte(text+rtGeoOwnLine+"\n"), mode); err != nil {
			t.Fatalf("запись строки XKeen в crontab: %v", err)
		}
		lines = rtGeoCronLines(t)
	}
	if len(lines) == 0 {
		t.Fatalf("строки xkeen -ug в crontab нет и дописать её не удалось")
	}
	line := lines[0]
	m := rtGeoCronXKeen.FindStringSubmatch(line)
	if m == nil {
		t.Skip("строка xkeen -ug в crontab нестандартная: панель её не перенимает")
	}
	lineMinute, _ := strconv.Atoi(m[1])
	lineHour, _ := strconv.Atoi(m[2])
	lineDay := -1
	if m[3] != "*" {
		lineDay, _ = strconv.Atoi(m[3])
	}
	// Слот строки не должен наступить, пока расписание панели активно в сценарии.
	now := rtGeoSchedOf(t, e.rtGeoSnapshot()).now
	slotToday := time.Date(now.Year(), now.Month(), now.Day(), lineHour, lineMinute, 0, 0, now.Location())
	if d := slotToday.Sub(now); d > -time.Minute && d < 10*time.Minute {
		t.Skipf("слот строки XKeen (%02d:%02d) рядом с текущим временем: запуск по расписанию помешал бы сценарию", lineHour, lineMinute)
	}

	cronHas := func() bool {
		for _, l := range rtGeoCronLines(t) {
			if l == line {
				return true
			}
		}
		return false
	}

	// 1. Перенять: строки в файле нет, расписание панели — из строки.
	if r := e.rtGeoCron("adopt"); r.Status != http.StatusOK {
		t.Fatalf("cron adopt: %d %s", r.Status, r.Code)
	}
	if n := len(rtGeoCronLines(t)); n != 0 {
		t.Errorf("после adopt в crontab осталось строк xkeen -ug: %d", n)
	}
	sch := rtGeoSchedOf(t, e.rtGeoSnapshot())
	if !sch.enabled || sch.suspended || sch.hour != lineHour || sch.minute != lineMinute || sch.day != lineDay {
		t.Errorf("после adopt расписание: enabled=%v suspended=%v %02d:%02d день %d; ожидалось из строки %02d:%02d день %d",
			sch.enabled, sch.suspended, sch.hour, sch.minute, sch.day, lineHour, lineMinute, lineDay)
	}

	// 2. Выключение флага настоящим API панели: строка возвращается дословно.
	e.setLayer(false)
	if !cronHas() {
		t.Errorf("после выключения флага в crontab нет строки %q дословно (строки xkeen -ug: %q)", line, rtGeoCronLines(t))
	}

	// 3. Включение флага: расписание приостановлено, строка у XKeen.
	e.setLayer(true)
	e.rtRstIdle()
	snap := e.rtGeoSnapshot()
	sch = rtGeoSchedOf(t, snap)
	cron := rtGeoSection(snap, "cron")
	if !sch.suspended {
		t.Errorf("после включения флага schedule.suspended=false: расписание должно ждать «Перенять расписание»")
	}
	if sch.hasNext {
		t.Errorf("после включения флага у приостановленного расписания есть schedule.next")
	}
	if cron["found"] != true || cron["adoptable"] != true {
		t.Errorf("после включения флага cron: found=%v adoptable=%v; ожидалось true и true", cron["found"], cron["adoptable"])
	}
	lastRunBefore := rtGeoLastAt(t, snap)

	// 4. Тик сервиса: приостановленное расписание само не запускается и строку не снимает.
	time.Sleep(40 * time.Second)
	snap = e.rtGeoSnapshot()
	if !cronHas() {
		t.Errorf("за 40 с приостановки строка %q исчезла из crontab: расписание само сняло строку", line)
	}
	if rtGeoRun(snap, "running") || !rtGeoLastAt(t, snap).Equal(lastRunBefore) {
		t.Errorf("приостановленное расписание запустилось само: running=%v, last_run.at был %s, стал %s",
			rtGeoRun(snap, "running"), lastRunBefore.Format(time.RFC3339), rtGeoLastAt(t, snap).Format(time.RFC3339))
	}

	// 5. «Перенять расписание»: строки нет, расписание активно.
	if r := e.rtGeoCron("adopt"); r.Status != http.StatusOK {
		t.Fatalf("cron adopt после включения флага: %d %s", r.Status, r.Code)
	}
	if n := len(rtGeoCronLines(t)); n != 0 {
		t.Errorf("после повторного adopt в crontab осталось строк xkeen -ug: %d", n)
	}
	sch = rtGeoSchedOf(t, e.rtGeoSnapshot())
	if sch.suspended || !sch.hasNext {
		t.Errorf("после повторного adopt: suspended=%v, schedule.next есть=%v; ожидалось false и true", sch.suspended, sch.hasNext)
	}
}
