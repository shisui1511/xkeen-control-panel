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
}
