//go:build router

package handlers

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Активация профилей Mihomo на настоящей панели устройства (PART-03, D-23).
// Вердикт — процессы ядра в /proc (pidof, независимо от панели), ссылка и файлы
// профилей на диске и ответ панели. Слой «Конфигурация» на время сценариев
// выключен (старая карточка профилей); флаг, ссылка config.yaml и профили
// возвращаются в t.Cleanup.

const (
	rtPrfBase = "rt-pid-base"
	rtPrfA    = "rt-pid-a"
)

// rtPrfStand — подготовленный стенд: профили base и a с одинаковым содержимым.
type rtPrfStand struct {
	t    *testing.T
	e    *rtSrc
	dir  string
	base string // профиль, который был активен до сценария (или принят как rt-pid-base)
}

func (s *rtPrfStand) configPath() string { return filepath.Join(s.dir, "config.yaml") }

func (s *rtPrfStand) profilePath(name string) string {
	return filepath.Join(s.dir, "profiles", name+".yaml")
}

// activeName — профиль, на который сейчас указывает config.yaml ("" — не ссылка на профиль).
func (s *rtPrfStand) activeName() string {
	target, err := os.Readlink(s.configPath())
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(target), ".yaml")
}

func (s *rtPrfStand) readProfile(name string) []byte {
	s.t.Helper()
	raw, err := os.ReadFile(s.profilePath(name))
	if err != nil {
		s.t.Fatalf("профиль %s не читается: %v", name, err)
	}
	return raw
}

// rtPrfRemoveTestProfiles удаляет профили rt-pid-* (остатки прошлого прогона и свои).
func rtPrfRemoveTestProfiles(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "profiles", "rt-pid-*.yaml"))
	for _, f := range files {
		_ = os.Remove(f)
	}
}

// rtPrfPrepare выключает слой, снимает исходное состояние config.yaml, принимает
// обычный файл как rt-pid-base (или берёт активный профиль) и создаёт rt-pid-a копией.
func rtPrfPrepare(t *testing.T, kernel string) *rtPrfStand {
	t.Helper()
	e := rtNewSrc(t)
	e.setLayer(false)
	s := &rtPrfStand{t: t, e: e, dir: rtRtMihomoDir}
	cfg := s.configPath()

	link, linkErr := os.Readlink(cfg)
	var data []byte
	mode := os.FileMode(0o600)
	if linkErr != nil {
		var err error
		if data, err = os.ReadFile(cfg); err != nil {
			t.Fatalf("config.yaml Mihomo не читается: %v", err)
		}
		if info, err := os.Stat(cfg); err == nil {
			mode = info.Mode().Perm()
		}
	} else if strings.HasPrefix(strings.TrimSuffix(filepath.Base(link), ".yaml"), "rt-pid-") {
		t.Fatalf("config.yaml указывает на профиль прошлого прогона: %s", filepath.Base(link))
	}
	_, dirErr := os.Stat(filepath.Join(s.dir, "profiles"))
	profilesExisted := dirErr == nil
	rtPrfRemoveTestProfiles(s.dir)

	t.Cleanup(func() {
		tmp := cfg + ".rt-restore"
		_ = os.Remove(tmp)
		var err error
		if linkErr == nil {
			err = os.Symlink(link, tmp)
		} else {
			err = os.WriteFile(tmp, data, mode)
		}
		if err == nil {
			err = os.Rename(tmp, cfg)
		}
		if err != nil {
			t.Errorf("возврат config.yaml: %v", err)
		}
		rtPrfRemoveTestProfiles(s.dir)
		if !profilesExisted {
			_ = os.Remove(filepath.Join(s.dir, "profiles"))
		}
		// Содержимое профилей одинаково, перезапуск ядру не нужен, кроме случая, когда
		// сценарий оставил его остановленным.
		if kernel == "mihomo" {
			rtPrfEnsureRunning(t)
		}
	})

	list := e.call(http.MethodGet, "/api/mihomo/profiles", nil)
	if list.Status != http.StatusOK {
		t.Fatalf("GET /api/mihomo/profiles: %d", list.Status)
	}
	obj := list.object(t)
	if managed, _ := obj["managed"].(bool); managed {
		s.base, _ = obj["active"].(string)
	} else {
		s.base = rtPrfBase
		if r := e.call(http.MethodPost, "/api/mihomo/profiles/adopt", map[string]string{"name": rtPrfBase}); r.Status != http.StatusOK {
			t.Fatalf("adopt %s: %d", rtPrfBase, r.Status)
		}
	}
	if s.base == "" {
		t.Fatal("активный профиль не определён")
	}
	if r := e.call(http.MethodPost, "/api/mihomo/profiles/create", map[string]any{"name": rtPrfA, "from": s.base}); r.Status != http.StatusOK {
		t.Fatalf("create %s: %d", rtPrfA, r.Status)
	}
	return s
}

// rtPrfEnsureRunning поднимает Mihomo, если сценарий оставил его остановленным.
func rtPrfEnsureRunning(t *testing.T) {
	t.Helper()
	if pids, _ := rtRstKernelPIDs("mihomo"); len(pids) > 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, stderr, code := routertest.Exec(ctx, "xkeen", "-restart")
	if code != 0 {
		t.Errorf("возврат Mihomo: xkeen -restart код %d: %.200s", code, stderr)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if pids, _ := rtRstKernelPIDs("mihomo"); len(pids) == 1 {
			return
		}
		time.Sleep(time.Second)
	}
	t.Errorf("Mihomo не поднялся после возврата стенда")
}

func TestRouterProfileActivatePID(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	kernel := tg.Core
	if kernel != "xray" && kernel != "mihomo" {
		t.Fatalf("неизвестное ядро цели %q", kernel)
	}
	pidBefore := rtRstKernelPID(t, kernel)
	s := rtPrfPrepare(t, kernel)
	if pid := rtRstKernelPID(t, kernel); pid != pidBefore {
		t.Fatalf("PID %s сменился при подготовке: %d -> %d", kernel, pidBefore, pid)
	}

	r := s.e.callSlow(http.MethodPost, "/api/mihomo/profiles/activate", map[string]string{"name": rtPrfA})
	if r.Status != http.StatusOK {
		t.Fatalf("activate %s: %d %s", rtPrfA, r.Status, r.Code)
	}
	res := r.object(t)
	restarted, _ := res["restarted"].(bool)
	rolledBack, _ := res["rolled_back"].(bool)
	if rolledBack {
		t.Fatalf("активация откатилась: ошибка %q", res["error"])
	}

	switch kernel {
	case "mihomo":
		if !restarted {
			t.Fatalf("restarted=false при работающем Mihomo; outcome %q", res["outcome"])
		}
		if got := s.activeName(); got != rtPrfA {
			t.Fatalf("config.yaml указывает на %q, ожидался %s", got, rtPrfA)
		}
		pidAfter := rtRstKernelPID(t, "mihomo")
		if pidAfter == pidBefore {
			t.Fatalf("PID mihomo не сменился после активации (%d): успех засчитан по старому процессу", pidBefore)
		}
		rtRstHoldPID(t, "mihomo", pidAfter)
	default:
		if restarted {
			t.Fatalf("restarted=true при активном Xray")
		}
		if out, _ := res["outcome"].(string); out != "saved_kernel_inactive" {
			t.Fatalf("outcome %q, ожидался saved_kernel_inactive", out)
		}
		if got := s.activeName(); got != rtPrfA {
			t.Fatalf("config.yaml указывает на %q, ожидался %s", got, rtPrfA)
		}
		if pid := rtRstKernelPID(t, "xray"); pid != pidBefore {
			t.Fatalf("PID xray сменился при сохранении профиля Mihomo: %d -> %d", pidBefore, pid)
		}
	}
}

func TestRouterProfileActivateRollback(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	if tg.Core != "mihomo" {
		t.Skip("нужен активный Mihomo")
	}
	pidBefore := rtRstKernelPID(t, "mihomo")
	s := rtPrfPrepare(t, "mihomo")
	baseBytes, aBytes := s.readProfile(s.base), s.readProfile(rtPrfA)

	// Настоящий отказ запуска: пока config.yaml указывает на rt-pid-a, процесс
	// Mihomo гасится сигналом KILL. Подтверждение панели не доходит до окна
	// устойчивости и по сроку откатывает активацию. Горутина останавливается
	// в t.Cleanup раньше, чем стенд возвращается.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			if s.activeName() != rtPrfA {
				continue
			}
			pids, _ := rtRstKernelPIDs("mihomo")
			for _, pid := range pids {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})

	// Предел ожидания ответа — routertest.SlowCallTimeout у callSlow, он больше
	// срока подтверждения RestartConfirmTimeout вместе с откатом.
	r := s.e.callSlow(http.MethodPost, "/api/mihomo/profiles/activate", map[string]string{"name": rtPrfA})
	cancel()
	wg.Wait()

	if r.Status != http.StatusOK {
		t.Fatalf("activate %s: %d %s", rtPrfA, r.Status, r.Code)
	}
	res := r.object(t)
	if rolledBack, _ := res["rolled_back"].(bool); !rolledBack {
		t.Fatalf("rolled_back=false при погашенном процессе: restarted=%v", res["restarted"])
	}
	if active, _ := res["active"].(string); active != s.base {
		t.Fatalf("active %q после отката, ожидался %s", active, s.base)
	}
	if msg, _ := res["error"].(string); msg == "" {
		t.Fatal("после отката текст error пуст")
	}
	if got := s.activeName(); got != s.base {
		t.Fatalf("config.yaml указывает на %q после отката, ожидался %s", got, s.base)
	}
	// D-23: откат возвращает ссылку, содержимое профилей не переписывается.
	if !bytes.Equal(s.readProfile(s.base), baseBytes) || !bytes.Equal(s.readProfile(rtPrfA), aBytes) {
		t.Fatal("содержимое профилей изменилось при откате")
	}

	// Откат перезапускает Mihomo на прежнем профиле: ждём новый устойчивый процесс.
	var pidAfter int
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if pids, _ := rtRstKernelPIDs("mihomo"); len(pids) == 1 {
			pidAfter = pids[0]
			break
		}
		time.Sleep(time.Second)
	}
	if pidAfter == 0 {
		t.Fatal("после отката Mihomo не запущен")
	}
	if pidAfter == pidBefore {
		t.Fatalf("PID mihomo не сменился (%d): процесс не перезапускался", pidBefore)
	}
	rtRstHoldPID(t, "mihomo", pidAfter)
}
