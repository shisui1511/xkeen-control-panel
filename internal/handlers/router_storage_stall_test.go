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

// rtStallHold — сколько раздел остаётся замороженным после killall: меньше срока
// остановки панели (8 с), чтобы финальные записи состояния успели после оттаивания.
const rtStallHold = 3 * time.Second

// Перезапуск при застое носителя панели. Поток остановки первым делом писал строку
// «Received signal» в xcp.log и только потом взводил срок остановки; запись стояла
// 12–17 с, процесс выходил позже окна init-скрипта, и панель не поднималась
// (прогон 20261017-130553-7e154ece). Тот же синхронный журнал доступа держал ответ
// на правку черновика 12 с (TestRouterDraftEditWithDirtyDisk). Раздел панели
// замораживается до команды: запрос, строка журнала доступа которого пишется,
// отвечает сразу; после killall панель сразу перестаёт принимать соединения (поток
// остановки не ждёт носитель); через rtStallHold раздел оттаивает, и перезапуск
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
		t.Errorf("после killall панель принимала соединения все %s заморозки — поток остановки ждал носитель", rtStallHold)
	case closedAfter > time.Second:
		t.Errorf("после killall панель перестала принимать соединения через %v — поток остановки ждал носитель", closedAfter.Round(time.Millisecond))
	default:
		t.Logf("после killall панель перестала принимать соединения через %v", closedAfter.Round(time.Millisecond))
	}
}
