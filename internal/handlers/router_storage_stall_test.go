//go:build router

package handlers

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Застой носителя панели, воспроизведённый заморозкой раздела (FIFREEZE). Под
// чужой записью на USB-флешку (ext4, data=ordered) фиксация журнала ФС ждёт запись
// всех упорядоченных данных, и любой системный вызов панели, которому нужна
// транзакция журнала (запись строки в xcp.log: обновление mtime), стоит секунды —
// 14 с в прогоне 20261017-152925-7e154ece. Нагрузка записью даёт это не каждый раз;
// заморозка раздела даёт тот же эффект детерминированно: запись на раздел ждёт
// оттаивания, чтение идёт как обычно.

const (
	// rtFIFREEZE, rtFITHAW — _IOWR('X', 119/120, int); значения одинаковы на arm64
	// и mips.
	rtFIFREEZE = 0xC0045877
	rtFITHAW   = 0xC0045878
	// rtThawGuardEnv — переменная охранного процесса заморозки:
	// <точка монтирования>:<unix-время оттаивания>.
	rtThawGuardEnv = "XCP_RT_THAW_GUARD"
)

// Охранный процесс: тот же тестовый бинарник, запущенный с rtThawGuardEnv,
// оттаивает раздел в назначенное время, даже если тест упал или его процесс убит.
func init() {
	v := os.Getenv(rtThawGuardEnv)
	if v == "" {
		return
	}
	mount, at, _ := strings.Cut(v, ":")
	if sec, err := strconv.ParseInt(at, 10, 64); err == nil {
		time.Sleep(time.Until(time.Unix(sec, 0)))
	}
	_ = rtFsIoctl(mount, rtFITHAW)
	os.Exit(0)
}

func rtFsIoctl(mount string, req uintptr) error {
	f, err := os.Open(mount)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, 0); e != 0 {
		return e
	}
	return nil
}

// rtMountPoint — точка монтирования, на которой лежит path (/proc/mounts, самый
// длинный префикс).
func rtMountPoint(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open("/proc/mounts")
	if err != nil {
		t.Fatalf("/proc/mounts: %v", err)
	}
	defer f.Close()
	best := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		mp := fields[1]
		if (path == mp || strings.HasPrefix(path, strings.TrimSuffix(mp, "/")+"/")) && len(mp) > len(best) {
			best = mp
		}
	}
	if best == "" || best == "/" {
		t.Skipf("%s не на отдельном разделе: заморозка задела бы корень", path)
	}
	return best
}

// rtFreezeStorage замораживает раздел с каталогом dir и возвращает функцию
// оттаивания (повторный вызов ничего не делает; вызывается и в Cleanup). Раздел
// оттает не позже чем через maxHold и без теста: охранный процесс в своей сессии.
func rtFreezeStorage(t *testing.T, dir string, maxHold time.Duration) func() {
	t.Helper()
	mount := rtMountPoint(t, dir)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("путь тестового бинарника: %v", err)
	}
	guard := exec.Command(self, "-test.run=^$")
	guard.Env = append(os.Environ(), rtThawGuardEnv+"="+mount+":"+strconv.FormatInt(time.Now().Add(maxHold).Unix(), 10))
	guard.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := guard.Start(); err != nil {
		t.Fatalf("охранный процесс заморозки: %v", err)
	}
	thaw := sync.OnceFunc(func() {
		if err := rtFsIoctl(mount, rtFITHAW); err != nil && !errors.Is(err, syscall.EINVAL) {
			t.Errorf("оттаивание %s: %v (охранный процесс оттает раздел сам)", mount, err)
			return
		}
		_ = guard.Process.Kill()
		_ = guard.Wait()
	})
	if err := rtFsIoctl(mount, rtFIFREEZE); err != nil {
		_ = guard.Process.Kill()
		_ = guard.Wait()
		t.Skipf("заморозка %s недоступна: %v", mount, err)
	}
	t.Cleanup(thaw)
	return thaw
}

// rtPanelAddr — адрес слушателя панели, через который ходят тесты (XCP_RT_PANEL).
func rtPanelAddr(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(os.Getenv("XCP_RT_PANEL"))
	if err != nil || u.Host == "" {
		t.Fatalf("адрес панели XCP_RT_PANEL не разобран: %v", err)
	}
	return u.Host
}

// rtPortAccepts — принимает ли слушатель addr соединения.
func rtPortAccepts(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// rtStallProbe — запрос без записи на носитель, строка журнала доступа которого
// пишется (ответ 405 на GET): обработчик не трогает носитель, ответ ждёт только
// журнал. Возвращает время ответа или ошибку (в том числе по таймауту клиента).
func rtStallProbe(p *routertest.Panel, timeout time.Duration) (time.Duration, int, error) {
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		code, _, err := p.Request(http.MethodGet, "/api/auth/setup", nil, routertest.AuthNone)
		done <- result{code, err}
	}()
	select {
	case r := <-done:
		return time.Since(start), r.code, r.err
	case <-time.After(timeout):
		return time.Since(start), 0, errors.New("нет ответа за " + timeout.String())
	}
}

// rtThreadCPU — процессорное время потока tid процесса pid (utime+stime из
// /proc/<pid>/task/<tid>/stat, в тиках ядра).
func rtThreadCPU(t *testing.T, pid, tid int) int64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/task/" + strconv.Itoa(tid) + "/stat")
	if err != nil {
		t.Fatalf("поток %d процесса %d: %v", tid, pid, err)
	}
	// поля после «(comm)»: состояние — третье поле stat, utime и stime — 14-е и 15-е
	i := strings.LastIndexByte(string(raw), ')')
	f := strings.Fields(string(raw[i+1:]))
	if i < 0 || len(f) < 13 {
		t.Fatalf("поток %d процесса %d: stat не разобран: %q", tid, pid, raw)
	}
	utime, err1 := strconv.ParseInt(f[11], 10, 64)
	stime, err2 := strconv.ParseInt(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		t.Fatalf("поток %d процесса %d: utime/stime не разобраны: %q", tid, pid, raw)
	}
	return utime + stime
}

// rtPanelLoad — сколько и как нагружать панель запросами в
// TestRouterPanelMainThreadIdle.
const (
	rtPanelLoadWorkers = 8
	rtPanelLoadFor     = 4 * time.Second
)

// Главный поток панели не выполняет посторонних горутин. killall из init-скрипта
// шлёт сигнал процессу, и ядро отдаёт его главному потоку (лидеру группы), если тот
// не блокирует сигнал и у него нет других ожидающих, — даже когда поток в
// непрерываемом ожидании; остальные потоки сигнал не видят, пока не получат свой.
// Планировщик Go гоняет любые горутины и на главном потоке, поэтому горутина,
// ждущая носитель (запись файла на застывшем разделе), запирала SIGTERM: в прогоне
// 20261017-225050-8ab60d28 слушатель закрылся через 1,96 с после killall, на стенде
// главный поток стоял в openat → __sb_start_write на замороженном /opt, SIGTERM
// лежал в общей очереди процесса 1,1 с. Попадание горутины с записью на главный
// поток случайно, поэтому сценарий с заморозкой ловит это не каждый раз; здесь
// проверяется само условие: под нагрузкой запросами главный поток не получает
// процессорного времени (главная горутина ждёт сигнал, остальное идёт на других
// потоках).
func TestRouterPanelMainThreadIdle(t *testing.T) {
	p := routertest.NewPanel(t)
	pid := rtPanelCurrent(t, p)

	before := rtThreadCPU(t, pid, pid)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done, failed := 0, 0
	stop := time.Now().Add(rtPanelLoadFor)
	for w := 0; w < rtPanelLoadWorkers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			path := "/api/auth/me"
			if w%2 == 1 {
				path = "/api/service/status"
			}
			for time.Now().Before(stop) {
				code, _, err := p.Request(http.MethodGet, path, nil, routertest.AuthFull)
				mu.Lock()
				if err != nil || code != http.StatusOK {
					failed++
				} else {
					done++
				}
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	after := rtThreadCPU(t, pid, pid)
	if done == 0 {
		t.Fatalf("панель не ответила ни на один запрос нагрузки (неудачных: %d)", failed)
	}
	t.Logf("нагрузка: %d запросов за %s в %d потоков (неудачных %d); главный поток панели: %d → %d тиков",
		done, rtPanelLoadFor, rtPanelLoadWorkers, failed, before, after)
	if after != before {
		t.Errorf("главный поток панели выполнял горутины под нагрузкой (+%d тиков процессорного времени): горутина, ждущая носитель, может оказаться на нём, и SIGTERM процессу будет ждать её", after-before)
	}
}

// rtStallHold — сколько раздел остаётся замороженным после killall: застой короче
// срока остановки панели (8 с); застой дольше срока — TestRouterPanelRestartWithLongStall.
const rtStallHold = 3 * time.Second

// Перезапуск при застое носителя панели. Поток остановки первым делом писал строку
// «Received signal» в xcp.log и только потом взводил срок остановки; запись стояла
// 12–17 с, процесс выходил позже окна init-скрипта, и панель не поднималась
// (прогон 20261017-130553-7e154ece). Тот же синхронный журнал доступа держал ответ
// на правку черновика 12 с (TestRouterDraftEditWithDirtyDisk). Раздел панели
// замораживается до команды: запрос, строка журнала доступа которого пишется,
// отвечает сразу; после killall панель сразу перестаёт принимать соединения (ни
// доставка SIGTERM, ни поток остановки не ждут носитель: сигнал процессу ядро отдаёт
// главному потоку, см. TestRouterPanelMainThreadIdle); через rtStallHold раздел
// оттаивает, и перезапуск
// доходит до конца: новая панель отвечает по прежней сессии, срок остановки не
// истёк.
func TestRouterPanelRestartWithStalledStorage(t *testing.T) {
	p := routertest.NewPanel(t)
	old := rtPanelCurrent(t, p)
	addr := rtPanelAddr(t)
	quiet := rtWaitStorageQuiet(t, time.Minute)
	if !rtPortAccepts(addr) {
		t.Fatalf("слушатель панели не принимает соединения до заморозки")
	}

	dir := config.Default().DataDir
	thaw := rtFreezeStorage(t, dir, 40*time.Second)
	probeTook, probeCode, probeErr := rtStallProbe(p, 2*time.Second)

	var closedAfter time.Duration = -1
	res := rtPanelRestartWith(t, p, old, rtPanelHooks{
		afterKill: func() {
			start := time.Now()
			for time.Since(start) < rtStallHold {
				if !rtPortAccepts(addr) {
					closedAfter = time.Since(start)
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if wait := rtStallHold - time.Since(start); wait > 0 {
				time.Sleep(wait)
			}
			thaw()
		},
	})
	thaw()
	rtPanelRestartVerdict(t, old, res, "раздел "+filepath.Clean(dir)+" заморожен до команды, оттаял через "+rtStallHold.String()+" после killall; "+quiet)

	switch {
	case probeErr != nil:
		t.Errorf("запрос, строка журнала доступа которого пишется, при замороженном разделе: %v — ответ ждал запись журнала на носитель", probeErr)
	case probeTook > time.Second:
		t.Errorf("запрос при замороженном разделе ответил за %v (http %d) — ответ ждал запись журнала на носитель", probeTook.Round(time.Millisecond), probeCode)
	default:
		t.Logf("запрос при замороженном разделе ответил за %v (http %d)", probeTook.Round(time.Millisecond), probeCode)
	}
	switch {
	case closedAfter < 0:
		t.Errorf("после killall панель принимала соединения все %s заморозки — обработка SIGTERM ждала носитель", rtStallHold)
	case closedAfter > time.Second:
		t.Errorf("после killall панель перестала принимать соединения через %v — обработка SIGTERM ждала носитель", closedAfter.Round(time.Millisecond))
	default:
		t.Logf("после killall панель перестала принимать соединения через %v", closedAfter.Round(time.Millisecond))
	}
}

// rtLongStallHold — сколько раздел остаётся замороженным после killall в
// TestRouterPanelRestartWithLongStall: дольше срока остановки панели (8 с) с
// дозаписью журнала по сроку (1 с) и меньше SIGKILL init-скрипта через
// XCP_STOP_KILL_AFTER (20 с). rtLongStallStopEnd — не позже какого момента после
// killall главный поток старого процесса обязан закончить остановку: бюджеты
// финальных записей и дозаписи журнала (cmd/xcp/shutdown.go) с запасом, раньше
// срока.
const (
	rtLongStallHold    = 12 * time.Second
	rtLongStallStopEnd = 6 * time.Second
)

// rtStopEnd — как главный поток процесса закончил остановку (rtWatchStopEnd).
type rtStopEnd struct {
	// end — когда главный поток вышел (от start): зомби-лидер «Z», пока остальные
	// потоки ждут носитель, или процесс исчез; -1 — не вышел, пока шло наблюдение.
	end time.Duration
	// stuckFrom, stuck — начало и длительность самого долгого непрерывного
	// пребывания главного потока в состоянии D (stuckFrom -1 — не был); wchan и
	// stack — его ожидание в ядре и верх стека в начале этого пребывания.
	stuckFrom, stuck time.Duration
	wchan, stack     string
}

// rtWatchStopEnd раз в 20 мс смотрит главный поток процесса pid, пока процесс не
// исчезнет или не закрыт done. Остановку по сигналу выполняет главная горутина,
// закреплённая за главным потоком (cmd/xcp/shutdown.go): поток выходит, когда
// остановка закончена (main вернулся) или брошена по сроку. После выхода главного
// потока процесс живёт, пока другие его потоки стоят в непрерываемом ожидании
// носителя, а сам поток остаётся зомби, поэтому конец остановки виден по /proc
// раньше выхода процесса и без журнала панели, который при застое носителя не
// дописывается.
func rtWatchStopEnd(pid int, start time.Time, done <-chan struct{}) <-chan rtStopEnd {
	out := make(chan rtStopEnd, 1)
	go func() {
		res := rtStopEnd{end: -1, stuckFrom: -1}
		task := "/proc/" + strconv.Itoa(pid) + "/task/" + strconv.Itoa(pid)
		var spanFrom time.Duration = -1
		var spanWchan, spanStack string
		closeSpan := func(at time.Duration) {
			if spanFrom >= 0 && at-spanFrom > res.stuck {
				res.stuckFrom, res.stuck, res.wchan, res.stack = spanFrom, at-spanFrom, spanWchan, spanStack
			}
			spanFrom = -1
		}
		for {
			st, _ := rtProcState(pid)
			at := time.Since(start)
			if st == "D" {
				if spanFrom < 0 {
					spanFrom = at
					w, _ := os.ReadFile(task + "/wchan")
					spanWchan = strings.TrimSpace(string(w))
					spanStack = ""
					if raw, err := os.ReadFile(task + "/stack"); err == nil {
						var frames []string
						for _, l := range strings.Split(string(raw), "\n") {
							if f := strings.Fields(l); len(f) >= 2 {
								frames = append(frames, strings.SplitN(f[1], "+", 2)[0])
							}
							if len(frames) == 6 {
								break
							}
						}
						spanStack = strings.Join(frames, " < ")
					}
				}
			} else {
				closeSpan(at)
			}
			switch st {
			case "", "Z", "X", "x":
				res.end = at
				out <- res
				return
			}
			select {
			case <-done:
				closeSpan(time.Since(start))
				out <- res
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	return out
}

// rtWaitPollGap ждёт (не дольше limit), пока опрос XKeen панели pid не закончится:
// следующий начнётся не раньше чем через 10 с, и запуск скрипта при замороженном
// разделе не примешается к остановке. Возвращает описание для журнала теста.
func rtWaitPollGap(pid int, limit time.Duration) string {
	start := time.Now()
	seen := false
	for time.Since(start) < limit {
		poll, _ := rtPanelXKeenPoll(pid)
		switch {
		case poll != 0:
			seen = true
		case seen:
			return "опрос XKeen закончился за " + time.Since(start).Round(10*time.Millisecond).String() + " до заморозки"
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seen {
		return "опрос XKeen идёт дольше " + limit.String()
	}
	return "опроса XKeen за " + limit.String() + " не было"
}

// Перезапуск при застое носителя дольше срока остановки. Финальные записи состояния
// — сессии (шаг auth) и счётчики трафика (traffic-quota) — шли синхронно на главной
// горутине: под застоем журнала ext4 создание временного файла (ext4_create) стояло
// до 8,5 с, шаг traffic-quota съедал весь срок, и остановка бросалась по сроку
// (гейт 20261020-172121-9acdd677, «deadline 8s exceeded at step traffic-quota»;
// гейт 20261020-104418-cfaec7f7). Застой воспроизводится заморозкой раздела панели
// до команды restart; раздел оттаивает через rtLongStallHold после killall. Финальные
// записи пробуются, но у каждой свой бюджет: главный поток старого процесса
// заканчивает остановку не позже rtLongStallStopEnd после killall и не стоит в
// непрерываемом ожидании носителя, а процесс выходит, когда раздел оттает (потоки
// брошенных записей держат exit_group до возврата системного вызова). Сверх этого —
// общий вердикт перезапуска.
func TestRouterPanelRestartWithLongStall(t *testing.T) {
	p := routertest.NewPanel(t)
	old := rtPanelCurrent(t, p)
	quiet := rtWaitStorageQuiet(t, time.Minute)
	gap := rtWaitPollGap(old, 15*time.Second)

	dir := config.Default().DataDir
	thaw := rtFreezeStorage(t, dir, rtLongStallHold+40*time.Second)

	watchDone := make(chan struct{})
	var once sync.Once
	stopWatch := func() { once.Do(func() { close(watchDone) }) }
	defer stopWatch()
	ended := make(chan rtStopEnd, 1)
	res := rtPanelRestartWith(t, p, old, rtPanelHooks{
		afterKill: func() {
			end := rtWatchStopEnd(old, time.Now(), watchDone)
			time.Sleep(rtLongStallHold)
			thaw()
			ended <- <-end
		},
	})
	thaw()
	// Наблюдение заканчивается само с выходом процесса; без метки killall хука не было.
	stopWatch()
	end := rtStopEnd{end: -1, stuckFrom: -1}
	select {
	case end = <-ended:
	case <-time.After(rtLongStallHold + 5*time.Second):
	}
	rtPanelRestartVerdict(t, old, res, "раздел "+filepath.Clean(dir)+" заморожен до команды, оттаял через "+
		rtLongStallHold.String()+" после killall; "+quiet+"; "+gap)

	if end.stuckFrom >= 0 {
		t.Logf("главный поток старого процесса в непрерываемом ожидании дольше всего с %.1f с после killall, %.1f с: %q, стек: %s",
			end.stuckFrom.Seconds(), end.stuck.Seconds(), end.wchan, end.stack)
	}
	switch {
	case end.end < 0:
		t.Errorf("главный поток старого процесса не закончил остановку за %s после killall: остановка ждала носитель", rtLongStallHold)
	case end.end > rtLongStallStopEnd:
		t.Errorf("главный поток старого процесса закончил остановку через %.1f с после killall (предел %s): шаг остановки ждал носитель",
			end.end.Seconds(), rtLongStallStopEnd)
	default:
		t.Logf("главный поток старого процесса закончил остановку через %.1f с после killall", end.end.Seconds())
	}
	if end.stuck >= time.Second {
		t.Errorf("главный поток старого процесса стоял в непрерываемом ожидании %.1f с подряд: шаг остановки сам ждал носитель",
			end.stuck.Seconds())
	}
	// Условие сценария: потоки старого процесса ждали носитель до оттаивания.
	if res.stopTook >= 0 && res.stopTook < rtLongStallHold-time.Second {
		t.Errorf("старый процесс вышел за %.1f с после killall, раньше оттаивания раздела (%s): финальные записи не ждали носитель, условие сценария не выполнено",
			res.stopTook.Seconds(), rtLongStallHold)
	}
}
