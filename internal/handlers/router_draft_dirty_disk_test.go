//go:build router

package handlers

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Правка черновика слоя «Конфигурация», пока на раздел панели идёт большая запись.
// Раздел панели на роутере — USB-флешка с ext4 (data=ordered). Когда файл состояния
// слоя писался в обработчике правки, ответ ждал диск: fsync — фиксации журнала, а с
// ней записи на носитель всех выделенных в транзакции данных, в том числе чужих;
// подмена файла переименованием — записи прежней версии файла, стоящей в очереди
// устройства за чужими данными. На флешке arm64-стенда (около 1 МБ/с) это десятки
// секунд, и модаль «Сохранить» на вкладке «Узлы» не закрывалась. Теперь файл пишется
// в фоне. Вердикт — время ответа маршрута панели.

const (
	// rtDirtyBytes — сколько данных отправить на носитель перед правками: на флешке
	// arm64-стенда это около 20 с записи, на mipsle — секунды. Объём больше очереди
	// блочного устройства стендов (128 запросов по 180 КБ, около 22 МБ): запись
	// файла под такой нагрузкой ждёт и журнал, и место в очереди.
	rtDirtyBytes = 24 << 20
	// rtWritebackMin — сколько данных должно быть в записи на носитель, когда
	// начинаются правки (/proc/meminfo, Writeback): иначе нагрузки нет.
	rtWritebackMin = 4 << 20
	// rtDraftEditFloor — нижняя граница предела ответа на правку. Сам предел —
	// четверть времени, за которое носитель записал чужие данные: правка, которая
	// пишет файл в обработчике, ждёт эту запись, а правка без записи в
	// обработчике отвечает за миллисекунды.
	rtDraftEditFloor = 250 * time.Millisecond
	// rtDraftEdits — правок подряд.
	rtDraftEdits = 3
)

func TestRouterDraftEditWithDirtyDisk(t *testing.T) {
	e := rtNewSrc(t)
	e.enableLayer()
	// Включение слоя запускает фоновую сборку; её запись состояния держит замок
	// состояния, и правка ждала бы её, а не носитель.
	rtWaitApplyIdle(t, e)

	dir := config.Default().DataDir
	if _, err := os.Stat(filepath.Join(dir, configlayer.StateFileName)); err != nil {
		t.Fatalf("файл состояния слоя в %s: %v", dir, err)
	}
	rev := e.revision()

	f, err := os.CreateTemp(dir, "rt-dirty-*")
	if err != nil {
		t.Fatalf("временный файл в %s: %v", dir, err)
	}
	chunk := make([]byte, 1<<20)
	for i := range chunk {
		chunk[i] = byte(i)
	}
	for written := 0; written < rtDirtyBytes; written += len(chunk) {
		if _, err := f.Write(chunk); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			t.Fatalf("запись временного файла: %v", err)
		}
	}
	// fsync временного файла в фоне: блоки его данных выделяются сразу и попадают
	// в текущую транзакцию журнала, а запись на носитель идёт, пока идут правки.
	var (
		syncErr  error
		syncTook time.Duration
	)
	synced := make(chan struct{})
	syncStart := time.Now()
	go func() {
		syncErr = f.Sync()
		syncTook = time.Since(syncStart)
		close(synced)
	}()
	t.Cleanup(func() {
		// следующий тест не должен начинаться под этой нагрузкой
		<-synced
		_ = f.Close()
		_ = os.Remove(f.Name())
	})

	wb := rtWaitWriteback(t, synced)
	t.Logf("в записи на носитель: %d МБ", wb>>20)
	// Запрос без записи файла: начало отправки данных на носитель занимает
	// процессор mipsle, и первая правка мерила бы его, а не себя.
	if r := e.call(http.MethodGet, "/api/auth/me", nil); r.Status != http.StatusOK {
		t.Fatalf("GET /api/auth/me: %d %s", r.Status, r.Code)
	}

	var took [rtDraftEdits]time.Duration
	for i := range took {
		start := time.Now()
		r := e.call(http.MethodPost, "/api/configlayer/draft/reset", map[string]int64{"revision": rev})
		took[i] = time.Since(start)
		if r.Status != http.StatusOK {
			t.Fatalf("правка %d: сброс черновика: %d %s", i+1, r.Status, r.Code)
		}
		t.Logf("правка %d: %v", i+1, took[i].Round(time.Millisecond))
		next, _ := r.object(t)["draft_revision"].(float64)
		rev = int64(next)
	}

	<-synced
	if syncErr != nil {
		t.Fatalf("fsync временного файла: %v", syncErr)
	}
	limit := max(rtDraftEditFloor, syncTook/4)
	t.Logf("чужие данные записаны на носитель за %v, предел правки %v",
		syncTook.Round(time.Millisecond), limit.Round(time.Millisecond))
	for i, d := range took {
		if d > limit {
			t.Errorf("правка %d: сброс черновика ответил за %v — ждал записи чужих данных на носитель (%v, предел %v)",
				i+1, d.Round(time.Millisecond), syncTook.Round(time.Millisecond), limit.Round(time.Millisecond))
		}
	}
}

// rtWaitApplyIdle ждёт, пока на панели не идёт применение (сборка после включения
// слоя, фоновое обновление источника).
func rtWaitApplyIdle(t *testing.T, e *rtSrc) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		r := e.call(http.MethodGet, "/api/configlayer/state", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
		}
		apply, _ := r.object(t)["apply"].(map[string]any)
		if running, _ := apply["running"].(bool); !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("применение на панели не закончилось за 2 мин")
		}
		time.Sleep(time.Second)
	}
}

// rtWaitWriteback ждёт, пока в записи на носитель не окажется хотя бы
// rtWritebackMin, и возвращает объём. Если fsync закончился раньше, носитель
// записал данные быстрее, чем начались правки: сценарий на этом стенде не
// воспроизводится.
func rtWaitWriteback(t *testing.T, synced <-chan struct{}) int64 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		wb := rtMeminfo(t, "Writeback")
		if wb >= rtWritebackMin {
			return wb
		}
		select {
		case <-synced:
			t.Skipf("носитель записал %d МБ раньше, чем начались правки: нагрузки нет", rtDirtyBytes>>20)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("за 5 с в записи на носитель не набралось %d МБ (сейчас %d КБ)", rtWritebackMin>>20, wb>>10)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// rtMeminfo возвращает поле /proc/meminfo в байтах.
func rtMeminfo(t *testing.T, field string) int64 {
	t.Helper()
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		t.Fatalf("/proc/meminfo: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok || name != field {
			continue
		}
		kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			t.Fatalf("/proc/meminfo %s: %v", field, err)
		}
		return kb << 10
	}
	t.Fatalf("/proc/meminfo: нет поля %s", field)
	return 0
}
