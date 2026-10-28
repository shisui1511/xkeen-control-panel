//go:build router

package geodata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Сценарии расписания и cron XKeen на настоящем crontab устройства. Исходное
// содержимое файла запоминается и возвращается в конце теста. Настоящие запуски по
// времени и догон проверяются на уровне API настоящей панели (146.1-17). Ядро тест
// не читает и не переключает — метки ядра нет.

const rtCronFile = "/opt/var/spool/cron/crontabs/root"

// rtCronRead читает crontab; нет файла — пустая строка.
func rtCronRead(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(rtCronFile)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("чтение crontab: %v", err)
	}
	return string(data)
}

// rtCronWrite заменяет crontab целиком.
func rtCronWrite(t *testing.T, content string) {
	t.Helper()
	if err := utils.AtomicReplaceFile(rtCronFile, []byte(content)); err != nil {
		t.Fatalf("запись crontab: %v", err)
	}
}

// rtCronHasLine — в содержимом есть строка line целиком.
func rtCronHasLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// rtCronHasUG — в содержимом есть строка `xkeen -ug`.
func rtCronHasUG(content string) bool {
	for _, l := range strings.Split(content, "\n") {
		if isUGLine(l) {
			return true
		}
	}
	return false
}

// rtCronSetup готовит сервис на настоящем crontab: прежнее содержимое возвращается
// в t.Cleanup. Возвращает сервис и строку XKeen на время, далёкое от текущего часа
// стенда (слот расписания в тесте не наступает).
func rtCronSetup(t *testing.T) (*Service, string) {
	t.Helper()
	tg := routertest.Current(t)
	if _, err := os.Stat(filepath.Dir(rtCronFile)); err != nil {
		t.Skipf("нет каталога %s: cron Entware не установлен", filepath.Dir(rtCronFile))
	}
	rtGeoSkipWindow(t, 25*time.Minute)

	orig, err := os.ReadFile(rtCronFile)
	origExists := err == nil
	t.Cleanup(func() {
		if origExists {
			if err := utils.AtomicReplaceFile(rtCronFile, orig); err != nil {
				t.Errorf("возврат crontab: %v", err)
			}
		} else {
			_ = os.Remove(rtCronFile)
		}
	})

	dir := rtGeoTempDir(t, tg.WorkDir)
	datDir := filepath.Join(dir, "dat")
	xkDir := filepath.Join(dir, "xkeen")
	for _, d := range []string{datDir, xkDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	layer := rtGeoLayer(t, dir)
	store := rtGeoStore(t, xkDir)
	svc, err := New(Deps{DataDir: dir, Layer: layer, Store: store, Index: lists.NewIndex(datDir), CronFile: rtCronFile})
	if err != nil {
		t.Fatalf("geodata.New: %v", err)
	}
	t.Cleanup(svc.Stop)

	hour := (time.Now().Hour() + 12) % 24
	line := fmt.Sprintf("37 %d * * * /opt/sbin/xkeen -ug", hour)
	rtCronWrite(t, withLine(withoutUGLines(string(orig)), line))
	svc.Start()
	return svc, line
}

// rtCronSnap — снимок менеджера.
func rtCronSnap(t *testing.T, svc *Service) View {
	t.Helper()
	v, err := svc.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return v
}

// TestRouterGeodataCron — перенятие строки cron XKeen, возврат при выключении флага
// и приостановка расписания панели (D-12, D-13, D-36).
func TestRouterGeodataCron(t *testing.T) {
	svc, line := rtCronSetup(t)

	v := rtCronSnap(t, svc)
	if !v.Cron.Found || !v.Cron.Adoptable {
		t.Fatalf("cron: found=%v adoptable=%v, ожидались оба true (строка %q)", v.Cron.Found, v.Cron.Adoptable, line)
	}

	// Перенять: строки нет, текст сохранён, расписание панели — те же час и минута.
	if err := svc.CronAction(CronAdopt); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if rtCronHasUG(rtCronRead(t)) {
		t.Fatalf("после adopt в crontab осталась строка -ug:\n%s", rtCronRead(t))
	}
	svc.mu.Lock()
	taken, sch := svc.st.CronTaken, svc.st.Schedule
	svc.mu.Unlock()
	if taken == nil || taken.Line != line {
		t.Fatalf("CronTaken = %+v, ожидалась строка %q", taken, line)
	}
	want, _ := parseCronLine(line)
	if !sch.Enabled || sch.Day != -1 || sch.Hour != want.Hour || sch.Minute != want.Minute {
		t.Fatalf("расписание после adopt: %+v, ожидалось ежедневно %d:%d", sch, want.Hour, want.Minute)
	}
	if v := rtCronSnap(t, svc); v.Schedule.Suspended || v.Cron.Found {
		t.Fatalf("после adopt: suspended=%v found=%v", v.Schedule.Suspended, v.Cron.Found)
	}

	// Выключение флага: строка вернулась дословно, расписание панели приостановлено.
	if err := svc.OnFlagOff(); err != nil {
		t.Fatalf("OnFlagOff: %v", err)
	}
	if got := rtCronRead(t); !rtCronHasLine(got, line) {
		t.Fatalf("после OnFlagOff нет строки %q:\n%s", line, got)
	}
	if v := rtCronSnap(t, svc); !v.Schedule.Suspended {
		t.Fatalf("после OnFlagOff расписание не приостановлено")
	}

	// Включение флага расписание само не запускает и строку не снимает.
	svc.OnFlagOn()
	if got := rtCronRead(t); !rtCronHasLine(got, line) {
		t.Fatalf("после OnFlagOn строка исчезла:\n%s", got)
	}
	if v := rtCronSnap(t, svc); !v.Schedule.Suspended {
		t.Fatalf("OnFlagOn снял приостановку расписания")
	}

	// Перенять снова: строки нет, приостановка снята.
	if err := svc.CronAction(CronAdopt); err != nil {
		t.Fatalf("повторный adopt: %v", err)
	}
	if rtCronHasUG(rtCronRead(t)) {
		t.Fatalf("после повторного adopt в crontab осталась строка -ug")
	}
	if v := rtCronSnap(t, svc); v.Schedule.Suspended {
		t.Fatalf("повторный adopt не снял приостановку")
	}
	if v := rtCronSnap(t, svc); v.Schedule.Next == "" {
		t.Fatalf("при активном расписании панели нет schedule.next")
	}

	// «Два расписания»: xkeen -ugc дописал строку при активном расписании панели.
	rtCronWrite(t, withLine(rtCronRead(t), line))
	deadline := time.Now().Add(45 * time.Second)
	for !rtCronSnap(t, svc).Cron.Two {
		if time.Now().After(deadline) {
			t.Fatalf("за 45 с тик не заметил вернувшуюся строку cron: cron.two=false")
		}
		time.Sleep(time.Second)
	}
	// Уведомление публикуется сразу после смены состояния, но не в той же секции под замком.
	foundNotice := false
	for noticeDeadline := time.Now().Add(10 * time.Second); !foundNotice && time.Now().Before(noticeDeadline); time.Sleep(100 * time.Millisecond) {
		for _, n := range svc.d.Layer.Snapshot().Notices {
			if n.ID == "geodata_cron_two" {
				foundNotice = true
			}
		}
	}
	if !foundNotice {
		t.Errorf("нет уведомления geodata_cron_two: уведомления %+v, cron %+v, enabled слоя %v", svc.d.Layer.Snapshot().Notices, rtCronSnap(t, svc).Cron, svc.d.Layer.Enabled())
	}

	// «Убрать задачу cron»: строки нет, расписание панели осталось включённым.
	if err := svc.CronAction(CronRemove); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rtCronHasUG(rtCronRead(t)) {
		t.Fatalf("после remove в crontab осталась строка -ug")
	}
	if v := rtCronSnap(t, svc); v.Cron.Two || !v.Schedule.Enabled {
		t.Fatalf("после remove: two=%v enabled=%v", v.Cron.Two, v.Schedule.Enabled)
	}

	// «Отдать расписание XKeen»: расписание панели выключено, строка на месте, возвращать нечего.
	rtCronWrite(t, withLine(rtCronRead(t), line))
	if err := svc.CronAction(CronGive); err != nil {
		t.Fatalf("give: %v", err)
	}
	if got := rtCronRead(t); !rtCronHasLine(got, line) {
		t.Fatalf("после give нет строки %q:\n%s", line, got)
	}
	svc.mu.Lock()
	taken, sch = svc.st.CronTaken, svc.st.Schedule
	svc.mu.Unlock()
	if sch.Enabled || taken != nil {
		t.Fatalf("после give: enabled=%v cron_taken=%+v", sch.Enabled, taken)
	}

	// Выключение расписания в самой панели означает «без расписания»: crontab не
	// трогается, строка XKeen не возвращается; выключение флага возвращает снятую
	// строку дословно и при выключенном расписании панели.
	if err := svc.CronAction(CronAdopt); err != nil {
		t.Fatalf("adopt перед выключением расписания: %v", err)
	}
	before := rtCronRead(t)
	off := svc.st.Schedule
	off.Enabled = false
	if err := svc.SetSchedule(off); err != nil {
		t.Fatalf("SetSchedule(enabled=false): %v", err)
	}
	if after := rtCronRead(t); after != before || rtCronHasUG(after) {
		t.Fatalf("SetSchedule(enabled=false) изменил crontab:\n%s\n---\n%s", before, after)
	}
	if err := svc.OnFlagOff(); err != nil {
		t.Fatalf("OnFlagOff при выключенном расписании панели: %v", err)
	}
	if got := rtCronRead(t); !rtCronHasLine(got, line) {
		t.Fatalf("строка cron_taken не вернулась при выключенном расписании панели:\n%s", got)
	}
	svc.OnFlagOn()

	// Расписание вне формата XKeen не перенимается.
	nonstd := "*/30 * * * * /opt/sbin/xkeen -ug"
	rtCronWrite(t, withLine(withoutUGLines(rtCronRead(t)), nonstd))
	v = rtCronSnap(t, svc)
	if !v.Cron.Found || v.Cron.Adoptable {
		t.Fatalf("нестандартная строка: found=%v adoptable=%v, ожидались true и false", v.Cron.Found, v.Cron.Adoptable)
	}
	if err := svc.CronAction(CronAdopt); lists.ErrorCode(err) != "cron_nonstandard" {
		t.Fatalf("adopt нестандартной строки: %v, ожидался cron_nonstandard", err)
	}
	if got := rtCronRead(t); !rtCronHasLine(got, nonstd) {
		t.Fatalf("отказавший adopt изменил crontab:\n%s", got)
	}

	// Строки не было, а расписание панели включено: выключение флага ставит строку XKeen.
	if err := svc.CronAction(CronGive); err != nil {
		t.Fatalf("give: %v", err)
	}
	rtCronWrite(t, withoutUGLines(rtCronRead(t)))
	daily := Schedule{Enabled: true, Day: -1, Hour: (time.Now().Hour() + 10) % 24, Minute: 15}
	if err := svc.SetSchedule(daily); err != nil {
		t.Fatalf("SetSchedule: %v", err)
	}
	if err := svc.SetSchedule(Schedule{Enabled: true, Day: 7, Hour: 1, Minute: 1}); lists.ErrorCode(err) != "schedule_invalid" {
		t.Fatalf("SetSchedule с днём 7: %v, ожидался schedule_invalid", err)
	}
	if err := svc.OnFlagOff(); err != nil {
		t.Fatalf("OnFlagOff без cron_taken: %v", err)
	}
	wantLine := fmt.Sprintf("15 %d * * * /opt/sbin/xkeen -ug", daily.Hour)
	if got := rtCronRead(t); !rtCronHasLine(got, wantLine) {
		t.Fatalf("после OnFlagOff нет строки %q:\n%s", wantLine, got)
	}
	svc.OnFlagOn()
}
