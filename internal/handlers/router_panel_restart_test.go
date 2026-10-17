//go:build router

package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Перезапуск настоящей панели устройства init-скриптом. Init-скрипт Entware ждёт
// выход старого процесса около 10 с; не дождался — запуск отвечает «already
// running», и после выхода старого процесса панель остаётся лежать. Сценарии:
// открытые потоки SSE (остановка ждала их до дедлайна http.Server.Shutdown, деплой
// на arm64, прогон 20261015-015433-24d29880) и идущая запись на раздел панели
// (остановка ждала fsync файлов состояния, прогон 20261016-085028-f37a028f).
// Вердикт — процессы xcp в /proc (pidof, независимо от панели), время выхода
// старого процесса, ответ новой панели по прежней сессии и журнал панели:
// остановка закончилась сама, без аварийного выхода по сроку. Окно init-скрипта
// отсчитывается от killall (rc.func печатает «Shutting down» прямо перед ним), а не
// от запуска команды: цепочка sh → env → S99xcp → pidof сразу после большой записи
// сама запускается секундами (exec busybox ждал чтения страницы с флешки 4,5 с,
// прогон 20261016-115824-f37a028f), и это не время остановки панели.

const (
	rtPanelInit = "/opt/etc/init.d/S99xcp"
	// rtPanelStopLimit — за сколько старый процесс обязан выйти после killall:
	// init-скрипт ждёт остановку около 10 с, затем запуск видит старый процесс.
	rtPanelStopLimit = 8 * time.Second
	// rtPanelStopMark — вывод rc.func перед killall при stop/restart.
	rtPanelStopMark = "Shutting down"
	// rtPanelShellMark — первая строка обёртки команды: оболочка запущена.
	rtPanelShellMark = "xcp-rt-shell-started"
	// rtPanelDeadlineMark — строка журнала панели об аварийном выходе по сроку
	// остановки (cmd/xcp): шаг остановки не уложился и был брошен.
	rtPanelDeadlineMark = "Shutdown: deadline"
	// rtPanelUpTimeout — предел ожидания ответа новой панели (mipsle читает
	// бинарник с флешки и поднимается десятки секунд).
	rtPanelUpTimeout = 90 * time.Second
)

// rtPanelPIDs — PID процессов панели: только запуски с -config (init-скрипт и
// самообновление), без «xcp -v» и других коротких вызовов бинарника.
func rtPanelPIDs() (pids []int, out string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, "pidof", "xcp")
	out = strings.TrimSpace(stdout + " " + stderr)
	if code != 0 {
		return nil, out
	}
	for _, f := range strings.Fields(stdout) {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			continue
		}
		raw, err := os.ReadFile("/proc/" + f + "/cmdline")
		if err != nil {
			continue
		}
		for _, a := range strings.Split(string(raw), "\x00") {
			if a == "-config" {
				pids = append(pids, n)
				break
			}
		}
	}
	return pids, out
}

// rtPanelANSI — цветовые коды вывода init-скриптов Entware.
var rtPanelANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// rtPanelInitCmd запускает init-скрипт панели с чистым окружением: новая панель не
// должна унаследовать переменные теста (адрес подписки, сессия, каталоги).
// Возвращает вывод скрипта одной строкой без цветовых кодов.
func rtPanelInitCmd(ctx context.Context, action string) (string, int) {
	return rtPanelInitRun(ctx, action, nil)
}

// rtPanelInitRun — rtPanelInitCmd, вывод которого по мере поступления пишется и в
// marks (может быть nil): по нему видно, когда оболочка запустилась и когда rc.func
// дошёл до killall.
func rtPanelInitRun(ctx context.Context, action string, marks *rtPanelMarks) (string, int) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "echo "+rtPanelShellMark+
		"; exec env -i PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root "+rtPanelInit+" "+action)
	cmd.Stdout = &out
	if marks != nil {
		cmd.Stdout = io.MultiWriter(&out, marks)
	}
	cmd.Stderr = &errOut
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		code = -1
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if errOut.Len() == 0 {
			errOut.WriteString(err.Error())
		}
	}
	text := strings.ReplaceAll(out.String()+" "+errOut.String(), rtPanelShellMark, "")
	return strings.Join(strings.Fields(rtPanelANSI.ReplaceAllString(text, "")), " "), code
}

// rtPanelMarks отмечает момент первого появления каждой метки в потоке вывода.
type rtPanelMarks struct {
	mu   sync.Mutex
	seen bytes.Buffer
	at   map[string]time.Time
}

func newRtPanelMarks(names ...string) *rtPanelMarks {
	m := &rtPanelMarks{at: make(map[string]time.Time, len(names))}
	for _, n := range names {
		m.at[n] = time.Time{}
	}
	return m
}

func (m *rtPanelMarks) Write(b []byte) (int, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seen.Write(b)
	for n, at := range m.at {
		if at.IsZero() && bytes.Contains(m.seen.Bytes(), []byte(n)) {
			m.at[n] = now
		}
	}
	return len(b), nil
}

// get — момент появления метки name; нулевое время — метки не было.
func (m *rtPanelMarks) get(name string) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.at[name]
}

// rtPanelWaitUp ждёт панель с PID, отличным от old, которая отвечает на
// /api/auth/me по прежней сессии. Возвращает PID или 0 и последнее наблюдение.
func rtPanelWaitUp(p *routertest.Panel, old int, limit time.Duration) (int, string) {
	deadline := time.Now().Add(limit)
	last := ""
	for time.Now().Before(deadline) {
		pids, out := rtPanelPIDs()
		switch {
		case len(pids) != 1:
			last = "процессов панели " + strconv.Itoa(len(pids)) + " (pidof: " + out + ")"
		case pids[0] == old:
			last = "работает старый процесс " + strconv.Itoa(old)
		default:
			code, err := p.GetJSON("/api/auth/me", nil)
			if err == nil && code == http.StatusOK {
				return pids[0], ""
			}
			last = "новый процесс " + strconv.Itoa(pids[0]) + ", /api/auth/me: http " + strconv.Itoa(code)
			if err != nil {
				last += " (" + err.Error() + ")"
			}
		}
		time.Sleep(time.Second)
	}
	return 0, last
}

// rtPanelOpenStream открывает поток и возвращает канал, который закрывается, когда
// панель его завершила (EOF или обрыв соединения).
func rtPanelOpenStream(t *testing.T, ctx context.Context, p *routertest.Panel, path string, required bool) <-chan struct{} {
	t.Helper()
	resp, err := p.Stream(ctx, path)
	if err != nil {
		t.Fatalf("поток %s не открылся: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if required {
			t.Fatalf("поток %s: http %d", path, resp.StatusCode)
		}
		t.Logf("поток %s: http %d — без него", path, resp.StatusCode)
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}()
	return done
}

// rtPanelCurrent возвращает PID единственного процесса панели. Cleanup: что бы ни
// случилось, следующие тесты получают работающую панель.
func rtPanelCurrent(t *testing.T, p *routertest.Panel) int {
	t.Helper()
	pids, out := rtPanelPIDs()
	if len(pids) != 1 {
		t.Fatalf("процессов панели: %d (pidof: %q), ожидался ровно один", len(pids), out)
	}
	old := pids[0]
	t.Cleanup(func() {
		if pid, _ := rtPanelWaitUp(p, old, 5*time.Second); pid != 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		// Старый процесс мог ещё выходить: запуск при живом старом ответил бы
		// «already running» и ничего не запустил.
		for i := 0; i < 30; i++ {
			if _, err := os.Stat("/proc/" + strconv.Itoa(old)); err != nil {
				break
			}
			time.Sleep(time.Second)
		}
		if pids, _ := rtPanelPIDs(); len(pids) == 0 {
			_, _ = rtPanelInitCmd(ctx, "start")
		}
		if pid, last := rtPanelWaitUp(p, old, rtPanelUpTimeout); pid == 0 {
			t.Errorf("панель не поднялась после теста: %s", last)
		}
	})
	return old
}

// rtPanelRestartResult — итог перезапуска панели init-скриптом.
type rtPanelRestartResult struct {
	// stopTook — за сколько вышел старый процесс после killall (без метки killall —
	// после команды); -1 — не вышел за 30 с после команды.
	stopTook time.Duration
	// shellLag и killLag — через сколько после команды запустилась оболочка и
	// rc.func дошёл до killall; -1 — метки нет.
	shellLag, killLag time.Duration
	initOut           string
	initCode          int
	// newPID — PID новой панели, ответившей по прежней сессии; 0 — нет панели.
	newPID int
	// upLast — последнее наблюдение, если новой панели нет.
	upLast string
	// shutdownLog — строки журнала панели о сигнале и шагах остановки
	// («Received signal», «Shutdown: ...»);
	// logErr — журнал не прочитан (ротирован).
	shutdownLog []string
	logErr      error
}

// rtPanelRestart перезапускает панель init-скриптом (restart) и мерит выход старого
// процесса old по /proc. afterExit (может быть nil) вызывается сразу после выхода
// старого процесса, до ожидания init-скрипта. Затем ждёт новую панель и читает
// журнал панели с момента команды.
func rtPanelRestart(t *testing.T, p *routertest.Panel, old int, afterExit func()) rtPanelRestartResult {
	t.Helper()
	logPath := config.Default().XCPLogPath
	if logPath == "" {
		logPath = "/opt/var/log/xcp.log"
	}
	var offset int64
	if st, err := os.Stat(logPath); err == nil {
		offset = st.Size()
	}

	start := time.Now()
	restartCtx, cancelRestart := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelRestart()
	type initResult struct {
		out  string
		code int
	}
	restarted := make(chan initResult, 1)
	marks := newRtPanelMarks(rtPanelShellMark, rtPanelStopMark)
	go func() {
		o, c := rtPanelInitRun(restartCtx, "restart", marks)
		restarted <- initResult{o, c}
	}()

	// Выход старого процесса — по /proc, независимо от панели.
	var res rtPanelRestartResult
	var exitAt time.Time
	for {
		if _, err := os.Stat("/proc/" + strconv.Itoa(old)); err != nil {
			exitAt = time.Now()
			break
		}
		if time.Since(start) > 30*time.Second {
			res.stopTook = -1
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if afterExit != nil {
		afterExit()
	}
	ir := <-restarted
	res.initOut, res.initCode = ir.out, ir.code
	lag := func(at time.Time) time.Duration {
		if at.IsZero() {
			return -1
		}
		return at.Sub(start)
	}
	res.shellLag, res.killLag = lag(marks.get(rtPanelShellMark)), lag(marks.get(rtPanelStopMark))
	if res.stopTook < 0 {
		return res
	}
	from := start
	if res.killLag >= 0 {
		from = start.Add(res.killLag)
	}
	// Выход замечен опросом раз в 0,1 с, вывод — через пайп: разница бывает обратной.
	res.stopTook = max(exitAt.Sub(from), 0)
	res.newPID, res.upLast = rtPanelWaitUp(p, old, rtPanelUpTimeout)

	window, err := rtReadLogFrom(logPath, offset)
	if err != nil {
		res.logErr = err
		return res
	}
	for _, line := range strings.Split(window, "\n") {
		if strings.Contains(line, "Shutdown: ") || strings.Contains(line, "Received signal") {
			res.shutdownLog = append(res.shutdownLog, strings.TrimSpace(line))
		}
	}
	return res
}

// rtPanelRestartVerdict — общий вердикт перезапуска: старый процесс вышел в окно
// init-скрипта, новая панель отвечает по прежней сессии, остановка не брошена по сроку.
func rtPanelRestartVerdict(t *testing.T, old int, res rtPanelRestartResult, details string) {
	t.Helper()
	if res.stopTook < 0 {
		t.Fatalf("старый процесс %d не вышел за 30 с после restart (%s; init: код %d, %q)",
			old, details, res.initCode, res.initOut)
	}
	since := "после killall"
	if res.killLag < 0 {
		since = "после команды (метки killall нет)"
	}
	t.Logf("старый процесс вышел за %.1f с %s; оболочка init через %.1f с, killall через %.1f с после команды; %s; init: код %d, %q; новый PID %d",
		res.stopTook.Seconds(), since, res.shellLag.Seconds(), res.killLag.Seconds(), details, res.initCode, res.initOut, res.newPID)
	if res.logErr != nil {
		t.Logf("журнал панели не прочитан (ротирован?): %v", res.logErr)
	}
	for _, line := range res.shutdownLog {
		t.Logf("журнал остановки: %s", line)
	}
	if res.newPID == 0 {
		t.Fatalf("после restart панели нет за %s: %s (init: код %d, %q)", rtPanelUpTimeout, res.upLast, res.initCode, res.initOut)
	}
	if res.stopTook > rtPanelStopLimit {
		t.Errorf("старый процесс выходил %.1f с %s (%s), предел %s", res.stopTook.Seconds(), since, details, rtPanelStopLimit)
	}
	for _, line := range res.shutdownLog {
		if strings.Contains(line, rtPanelDeadlineMark) {
			t.Errorf("остановка не уложилась в срок и брошена: %s", line)
		}
	}
}

func TestRouterPanelRestartWithOpenStreams(t *testing.T) {
	p := routertest.NewPanel(t)
	old := rtPanelCurrent(t, p)

	streamCtx, cancelStreams := context.WithCancel(context.Background())
	defer cancelStreams()
	updDone := rtPanelOpenStream(t, streamCtx, p, "/api/update/events", true)
	// Поток слоя есть, только когда слой «Конфигурация» включён.
	layerDone := rtPanelOpenStream(t, streamCtx, p, "/api/configlayer/events", false)
	// Поток должен быть установлен до команды: первое событие уже отдано.
	time.Sleep(2 * time.Second)

	streamEnd := func(done <-chan struct{}) string {
		if done == nil {
			return "-"
		}
		select {
		case <-done:
			return "закрыт"
		case <-time.After(5 * time.Second):
			return "открыт"
		}
	}
	var upd, layer string
	res := rtPanelRestart(t, p, old, func() {
		upd, layer = streamEnd(updDone), streamEnd(layerDone)
	})
	rtPanelRestartVerdict(t, old, res, "потоки: обновление "+upd+", слой "+layer)
}

// Перезапуск, пока на раздел панели идёт большая запись (rtStartDirtyLoad). Остановка
// писала traffic.json и sessions.json с fsync: под идущей записью на флешку fsync
// ждал фиксации журнала ext4 — 10 с и больше, — и init-скрипт не дожидался выхода.
// Если нагрузка не набралась, сценарий пропускается (rtWaitWriteback).
func TestRouterPanelRestartWithDirtyDisk(t *testing.T) {
	p := routertest.NewPanel(t)
	old := rtPanelCurrent(t, p)

	load := rtStartDirtyLoad(t, config.Default().DataDir)
	wb := rtWaitWriteback(t, load.synced)

	res := rtPanelRestart(t, p, old, nil)
	syncTook, err := load.wait()
	if err != nil {
		t.Fatalf("fsync временного файла: %v", err)
	}
	rtPanelRestartVerdict(t, old, res, "в записи на носитель было "+strconv.FormatInt(wb>>20, 10)+
		" МБ, чужие данные записаны за "+syncTook.Round(time.Millisecond).String())
}

// rtPanelXKeenSteps — шаги остановки панели, которые ждали опрос XKeen: кэш
// статуса и сторожевой таймер (имена шагов — cmd/xcp/main.go).
var rtPanelXKeenSteps = []string{"Shutdown: xkeen-status-cache took", "Shutdown: watchdog took"}

// rtProcState — состояние процесса из /proc/<pid>/stat («R», «S», «D», «T», «Z»);
// пусто — процесса нет.
func rtProcState(pid int) (state string, ppid int) {
	st, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", 0
	}
	// поля после «(comm)»: состояние, PPID
	i := bytes.LastIndexByte(st, ')')
	if i < 0 {
		return "", 0
	}
	f := strings.Fields(string(st[i+1:]))
	if len(f) < 2 {
		return "", 0
	}
	ppid, _ = strconv.Atoi(f[1])
	return f[0], ppid
}

// rtPanelXKeenPoll — опрос XKeen, запущенный панелью pid (`xkeen -status` кэша
// статуса или watchdog, `xkeen -v` кэша): PID дочернего процесса и его командная
// строка; 0 — опроса сейчас нет. Команды жизненного цикла XKeen не берутся.
func rtPanelXKeenPoll(pid int) (int, string) {
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		n, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if _, ppid := rtProcState(n); ppid != pid {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		xkeen, poll := false, false
		for _, a := range args {
			switch {
			case filepath.Base(a) == "xkeen":
				xkeen = true
			case a == "-status" || a == "-v":
				poll = true
			}
		}
		if xkeen && poll {
			return n, strings.Join(args, " ")
		}
	}
	return 0, ""
}

// rtWaitStorageQuiet ждёт (не дольше limit), пока в кэше страниц почти не
// останется грязных и записываемых данных, и возвращает описание состояния
// носителя для журнала теста.
func rtWaitStorageQuiet(t *testing.T, limit time.Duration) string {
	t.Helper()
	start := time.Now()
	for {
		dirty, wb := rtMeminfo(t, "Dirty"), rtMeminfo(t, "Writeback")
		if dirty+wb < 1<<20 || time.Since(start) > limit {
			return "носитель: грязных " + strconv.FormatInt(dirty>>10, 10) + " КБ, в записи " +
				strconv.FormatInt(wb>>10, 10) + " КБ (ожидание " + time.Since(start).Round(100*time.Millisecond).String() + ")"
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Перезапуск, пока идёт опрос XKeen панели (`xkeen -status` кэша статуса или
// watchdog) и не заканчивается. В полном прогоне так выглядел опрос под записью на
// флешку: страницы скрипта XKeen вытеснены из кэша, и скрипт читал их с носителя
// всё время записи (14 с вместо 0,2 с). Остановка ждала этот процесс — шаг кэша
// статуса или watchdog держал выход до срока 8 с, аварийный выход сам ждал флешку,
// и init-скрипт не дождался выхода (прогон 20261016-235133-f6573bfd). Здесь опрос
// останавливается сигналом SIGSTOP: он гарантированно не закончится сам, пока
// панель останавливается. Сверх общего вердикта: шаги остановки, ждавшие опрос
// XKeen, не идут секунду и дольше, а прерванный опрос не остаётся висеть.
func TestRouterPanelRestartDuringXKeenStatus(t *testing.T) {
	bin := config.Default().XKeenBinary
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("XKeen не установлен (%s): опроса XKeen у панели нет", bin)
	}
	p := routertest.NewPanel(t)
	old := rtPanelCurrent(t, p)

	// Сценарий проверяет только ожидание опроса XKeen: запись на носитель от
	// предыдущих проверок (перезапуск под нагрузкой, старт свежей панели) сюда не
	// примешивается — её держит отдельный сценарий WithDirtyDisk.
	quiet := rtWaitStorageQuiet(t, time.Minute)
	// Опрос идёт раз в 10 с после конца предыдущего, проверка watchdog — раз в 30 с.
	var poll int
	var pollCmd string
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if poll, pollCmd = rtPanelXKeenPoll(old); poll != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if poll == 0 {
		t.Skipf("за 45 с панель не запустила опрос XKeen: условие сценария не набралось")
	}
	if err := syscall.Kill(poll, syscall.SIGSTOP); err != nil {
		t.Skipf("опрос XKeen «%s» закончился раньше, чем его удалось остановить: %v", pollCmd, err)
	}
	t.Cleanup(func() {
		// Опрос, который никто не убил, продолжается и заканчивается сам.
		_ = syscall.Kill(poll, syscall.SIGCONT)
	})
	stopped := ""
	for end := time.Now().Add(time.Second); time.Now().Before(end) && stopped != "T"; time.Sleep(10 * time.Millisecond) {
		stopped, _ = rtProcState(poll)
	}
	if stopped != "T" {
		t.Skipf("опрос XKeen «%s» закончился раньше, чем остановился (состояние «%s»)", pollCmd, stopped)
	}

	pollAtExit := "-"
	res := rtPanelRestart(t, p, old, func() {
		pollAtExit, _ = rtProcState(poll)
	})
	rtPanelRestartVerdict(t, old, res, "опрос XKeen «"+pollCmd+"» остановлен SIGSTOP, при выходе панели — состояние «"+pollAtExit+"»; "+quiet)
	for _, line := range res.shutdownLog {
		for _, step := range rtPanelXKeenSteps {
			if strings.Contains(line, step) {
				t.Errorf("остановка ждала опрос XKeen: %s", line)
			}
		}
	}
	// Убитый опрос исчезает (или остаётся зомби, пока его не подберут).
	state := ""
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if state, _ = rtProcState(poll); state == "" || state == "Z" {
			return
		}
	}
	t.Errorf("прерванный остановкой опрос XKeen «%s» остался висеть: состояние «%s» через 5 с после выхода панели", pollCmd, state)
}
