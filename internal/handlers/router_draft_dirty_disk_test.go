//go:build router

// router-area: configlayer

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
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Помощник нагрузки на носитель (rtStartDirtyLoad) общий для сценариев черновика и входа.
//
// Правка черновика слоя «Конфигурация», пока на раздел панели идёт большая запись.
// Раздел панели на роутере — USB-флешка с ext4 (data=ordered). Когда файл состояния
// слоя писался в обработчике правки, ответ ждал диск: fsync — фиксации журнала, а с
// ней записи на носитель всех выделенных в транзакции данных, в том числе чужих;
// подмена файла переименованием — записи прежней версии файла, стоящей в очереди
// устройства за чужими данными. На флешке arm64-стенда (около 1 МБ/с) это десятки
// секунд, и модаль «Сохранить» на вкладке «Узлы» не закрывалась. Теперь файл пишется
// в фоне. Вердикт — время ответа маршрута панели; если предусловие стенда не набрано
// (носитель не начал запись за отведённое время), сценарий пропускается с замерами.

const (
	// rtDirtyBytes — сколько данных отправить на носитель перед правками: на флешке
	// arm64-стенда это около 20 с записи, на mipsle — секунды. Объём больше очереди
	// блочного устройства стендов (128 запросов по 180 КБ, около 22 МБ): запись
	// файла под такой нагрузкой ждёт и журнал, и место в очереди.
	rtDirtyBytes = 24 << 20
	// rtWritebackMin — сколько данных должно быть в записи на носитель, когда
	// начинаются правки (/proc/meminfo, Writeback): иначе нагрузки нет.
	rtWritebackMin = 4 << 20
	// rtWritebackWait — сколько ждать, пока носитель начнёт принимать запись. На
	// флешке arm64-стенда фоновый fsync временного файла может сначала ждать идущей
	// фиксации журнала или фоновой записи того же inode и только потом отправлять
	// страницы на носитель; 5 с для этого мало (наблюдалось 16 КБ в Writeback через
	// 5 с при идущем fsync, прогон 9340659f).
	rtWritebackWait = 30 * time.Second
	// rtDraftEditFloor — нижняя граница предела ответа на правку. Сам предел —
	// четверть времени, за которое носитель записал чужие данные: правка, которая
	// пишет файл в обработчике, ждёт эту запись, а правка без записи в
	// обработчике отвечает за миллисекунды.
	rtDraftEditFloor = 250 * time.Millisecond
	// rtDraftEdits — правок подряд.
	rtDraftEdits = 3
)

func TestRouterDraftEditWithDirtyDisk(t *testing.T) {
	routertest.Tier(t, routertest.Full, "нагрузка на диск: грязные страницы носителя при правке черновика")
	e := rtNewSrc(t)
	e.freshDraft()
	// Включение слоя запускает фоновую сборку; её запись состояния держит замок
	// состояния, и правка ждала бы её, а не носитель.
	rtWaitApplyIdle(t, e)

	dir := config.Default().DataDir
	if _, err := os.Stat(filepath.Join(dir, configlayer.StateFileName)); err != nil {
		t.Fatalf("файл состояния слоя в %s: %v", dir, err)
	}

	// Признак мастера под той же нагрузкой (G-149.1-6). До исправления пишущий
	// POST close ждал fsync файла состояния под замком хранилища (время записи
	// нагрузки, на флешке стенда около 20 с — как правка черновика до фоновой
	// записи), а GET мастера ждал этот замок в фактах слоя; без нагрузки тот же код
	// укладывался в 0,6 с, поэтому гейт стоит здесь. Подготовка — до нагрузки:
	// черновик удержан (наблюдатель не поставит done между сбросом и close), признак
	// сброшен, так что первая запись признака под нагрузкой — close. Очистки
	// зарегистрированы раньше rtStartDirtyLoad и идут после ожидания fsync нагрузки.
	rtDevMode(e, true)
	prevWizard := rtWizardStatus(rtWizardView(e))
	release := rtWizardHold(e)
	t.Cleanup(release)
	t.Cleanup(func() { rtWizardRestore(e, prevWizard) })
	rtWizardReset(e)

	rev := e.revision()

	load := rtStartDirtyLoad(t, dir)

	wb := rtWaitWriteback(t, load.synced)
	t.Logf("в записи на носитель: %d МБ", wb>>20)
	// Запрос без записи файла: начало отправки данных на носитель занимает
	// процессор mipsle, и первая правка мерила бы его, а не себя.
	if r := e.call(http.MethodGet, "/api/auth/me", nil); r.Status != http.StatusOK {
		t.Fatalf("GET /api/auth/me: %d %s", r.Status, r.Code)
	}

	// Пишущий close — в горутине: в ней только запрос и время, итог уходит в канал
	// и разбирается в основной горутине. Через 200 мс — GET мастера, пока запись
	// признака может ещё идти. Затем hide. Всё до правок черновика: сброс черновика
	// отпустил бы удержание.
	type wizardCall struct {
		status int
		raw    []byte
		err    error
		took   time.Duration
	}
	closeCh := make(chan wizardCall, 1)
	go func() {
		start := time.Now()
		status, raw, err := e.p.Call(http.MethodPost, rtWizardBase+"/close", nil)
		closeCh <- wizardCall{status: status, raw: raw, err: err, took: time.Since(start)}
	}()
	time.Sleep(200 * time.Millisecond)
	getStart := time.Now()
	getResp := e.call(http.MethodGet, rtWizardBase, nil)
	getTook := time.Since(getStart)
	closeRes := <-closeCh
	if closeRes.err != nil {
		t.Fatalf("POST %s/close под нагрузкой: %v", rtWizardBase, closeRes.err)
	}
	closeResp := e.decode(closeRes.status, closeRes.raw)
	hideStart := time.Now()
	hideResp := e.call(http.MethodPost, rtWizardBase+"/hide", nil)
	hideTook := time.Since(hideStart)

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

	syncTook, err := load.wait()
	if err != nil {
		t.Fatalf("fsync временного файла: %v", err)
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

	t.Logf("wizard-timing: dirty close=%dms get=%dms hide=%dms sync=%dms",
		closeRes.took.Milliseconds(), getTook.Milliseconds(), hideTook.Milliseconds(), syncTook.Milliseconds())
	if closeResp.Status != http.StatusOK {
		t.Errorf("POST %s/close под нагрузкой: %d %s", rtWizardBase, closeResp.Status, closeResp.Code)
	} else if st := rtWizardStatus(closeResp.object(t)); st != "closed" {
		t.Errorf("POST close под нагрузкой: статус %q, ожидался closed", st)
	}
	if getResp.Status != http.StatusOK {
		t.Errorf("GET %s под нагрузкой: %d %s", rtWizardBase, getResp.Status, getResp.Code)
	} else if st := rtWizardStatus(getResp.object(t)); st != "closed" {
		t.Errorf("GET мастера под нагрузкой: статус %q, ожидался closed", st)
	}
	if hideResp.Status != http.StatusOK {
		t.Errorf("POST %s/hide под нагрузкой: %d %s", rtWizardBase, hideResp.Status, hideResp.Code)
	} else if st := rtWizardStatus(hideResp.object(t)); st != "hidden" {
		t.Errorf("POST hide под нагрузкой: статус %q, ожидался hidden", st)
	}
	if closeRes.took > rtWizardPostMax {
		t.Errorf("POST close под нагрузкой ответил за %v (предел %v, чужие данные записаны за %v): ответ ждал записи признака на носитель или замка хранилища (G-149.1-6)",
			closeRes.took.Round(time.Millisecond), rtWizardPostMax, syncTook.Round(time.Millisecond))
	}
	if getTook > rtWizardGetMax {
		t.Errorf("GET мастера под нагрузкой ответил за %v (предел %v, чужие данные записаны за %v): чтение вида ждало замка хранилища или носителя (G-149.1-6)",
			getTook.Round(time.Millisecond), rtWizardGetMax, syncTook.Round(time.Millisecond))
	}
	if hideTook > rtWizardPostMax {
		t.Errorf("POST hide под нагрузкой ответил за %v (предел %v, чужие данные записаны за %v): ответ ждал записи признака на носитель или замка хранилища (G-149.1-6)",
			hideTook.Round(time.Millisecond), rtWizardPostMax, syncTook.Round(time.Millisecond))
	}
}

// rtDirtyLoad — идущая в фоне запись rtDirtyBytes чужих данных на носитель.
type rtDirtyLoad struct {
	synced   <-chan struct{}
	syncErr  error
	syncTook time.Duration
}

// wait ждёт конца fsync нагрузки и возвращает, сколько носитель её записывал.
func (l *rtDirtyLoad) wait() (time.Duration, error) {
	<-l.synced
	return l.syncTook, l.syncErr
}

// rtStartDirtyLoad пишет rtDirtyBytes во временный файл rt-dirty-* в dir и
// запускает его fsync в фоне: блоки данных выделяются сразу и попадают в текущую
// транзакцию журнала, а запись на носитель идёт, пока сценарий мерит ответы.
// Очистка (дождаться fsync, удалить файл) регистрируется в t.Cleanup.
func rtStartDirtyLoad(t *testing.T, dir string) *rtDirtyLoad {
	t.Helper()
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
	synced := make(chan struct{})
	l := &rtDirtyLoad{synced: synced}
	syncStart := time.Now()
	go func() {
		l.syncErr = f.Sync()
		l.syncTook = time.Since(syncStart)
		close(synced)
	}()
	t.Cleanup(func() {
		// следующий тест не должен начинаться под этой нагрузкой
		<-synced
		_ = f.Close()
		_ = os.Remove(f.Name())
	})
	return l
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
// записал данные быстрее, чем начались правки; если за rtWritebackWait нагрузка не
// набралась при идущем fsync, стенд её не дал: в обоих случаях сценарий на этом
// стенде не воспроизводится, это пропуск, а не вердикт продукта.
func rtWaitWriteback(t *testing.T, synced <-chan struct{}) int64 {
	t.Helper()
	start := time.Now()
	deadline := start.Add(rtWritebackWait)
	for {
		wb := rtMeminfo(t, "Writeback")
		if wb >= rtWritebackMin {
			t.Logf("нагрузка на носитель набралась за %v", time.Since(start).Round(time.Millisecond))
			return wb
		}
		select {
		case <-synced:
			t.Skipf("носитель записал %d МБ раньше, чем начались правки: нагрузки нет", rtDirtyBytes>>20)
		default:
		}
		if time.Now().After(deadline) {
			t.Skipf("за %v при идущем fsync в записи на носитель %d КБ (нужно %d МБ), грязных %d КБ: стенд не набрал нагрузку, сценарий не воспроизводится",
				rtWritebackWait, wb>>10, rtWritebackMin>>20, rtMeminfo(t, "Dirty")>>10)
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
