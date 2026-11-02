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

// Обычный маршрут «Сделать активным» при слое «Конфигурация» (PART-02, PART-03; D-06,
// D-08, D-12, D-24, D-27). Вердикт — процессы ядер в /proc, ссылка config.yaml и файлы
// профилей на диске; ответы панели и снимок слоя — вспомогательные. Профили сценариев
// создаются копией действующего config.yaml; стенд возвращает rtSwPrepare (config.yaml,
// каталог профилей, config-xkeen*, файлы Xray), свои файлы — t.Cleanup сценария.

const (
	rtOwnA = "rt-own-a"
	rtOwnB = "rt-own-b"

	rtPrfActivate = "/api/mihomo/profiles/activate"
	rtPrfList     = "/api/mihomo/profiles"
)

// rtOwnPrepare готовит стенд сценария: исходное состояние снимается rtSwPrepare, свои
// профили names пишутся копией действующего config.yaml, в t.Cleanup удаляются по
// маске prefix* (до возврата стенда, он зарегистрирован раньше).
func rtOwnPrepare(t *testing.T, e *rtSrc, prefix string, names ...string) *rtSwStand {
	t.Helper()
	s := rtSwPrepare(t, e)
	rtOwnRemove(s, prefix)
	t.Cleanup(func() { rtOwnRemove(s, prefix) })
	if err := os.MkdirAll(s.profDir, 0o755); err != nil {
		t.Fatalf("каталог профилей Mihomo не создан: %v", err)
	}
	for _, name := range names {
		if err := os.WriteFile(s.profile(name), s.data, s.mode); err != nil {
			t.Fatalf("профиль %s не записан: %v", name, err)
		}
	}
	return s
}

// rtOwnRemove удаляет профили сценария по маске.
func rtOwnRemove(s *rtSwStand, prefix string) {
	files, _ := filepath.Glob(filepath.Join(s.profDir, prefix+"*.yaml"))
	for _, f := range files {
		_ = os.Remove(f)
	}
}

// rtOwnOpenDraft открывает черновик правкой параметров проб Xray и возвращает
// ревизию и число изменений.
func (e *rtSrc) rtOwnOpenDraft() (int64, int) {
	e.t.Helper()
	probe, _ := e.rtRtView()["probe"].(map[string]any)
	interval, _ := probe["interval_s"].(float64)
	if interval == 0 {
		e.t.Fatalf("в представлении нет probe.interval_s: %v", probe)
	}
	probe["interval_s"] = int(interval) + 1
	if r := e.call(http.MethodPost, rtRoutingBase+"/probe", map[string]any{"revision": e.rtRtRevision(), "probe": probe}); r.Status != http.StatusOK {
		e.t.Fatalf("probe: %d %s", r.Status, r.Code)
	}
	changes := e.rtRtDraftChanges()
	if changes == 0 {
		e.t.Fatal("правка проб не открыла черновик")
	}
	return e.rtRtRevision(), changes
}

// rtOwnActivate вызывает обычный маршрут активации и требует 200.
func (e *rtSrc) rtOwnActivate(name string) map[string]any {
	e.t.Helper()
	r := e.callSlow(http.MethodPost, rtPrfActivate, map[string]string{"name": name})
	if r.Status != http.StatusOK {
		e.t.Fatalf("activate %s: %d %s", name, r.Status, r.Code)
	}
	return r.object(e.t)
}

// Трасса (D-06, D-08): Mihomo у панели → обычная активация rt-own-a идёт через
// конвейер, панель отпускает Mihomo на свой профиль (ссылка на rt-own-a, файла xcp-main
// нет, routing.mihomo.taken false) → смена на rt-own-b при открытом черновике идёт
// обычной активацией, черновик не тронут → возврат панели → активация при открытом
// черновике отвечает 409 draft_open без изменений на диске. Ветка mihomo — перезапуск
// с новым PID, ветка xray — outcome saved_kernel_inactive. Метка ядра: сценарий читает
// активное ядро и проверяет его процесс.
func TestRouterProfileActivateOwnership(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	rtOwnPrepare(t, e, "rt-own-", rtOwnA, rtOwnB)
	m := rtCoresLiveMihomoTake(t, e, tg)
	pid := rtRstKernelPID(t, tg.Core)

	// Панель владеет Mihomo: активация идёт через конвейер.
	pid = rtSwExpectKernel(t, tg.Core, e.rtOwnActivate(rtOwnA), rtOwnA, pid)
	m.taken = false
	if !rtSwLinkOn(t, "profiles/"+rtOwnA+".yaml") {
		t.Fatalf("после активации config.yaml не указывает на %s", rtOwnA)
	}
	if _, err := os.Stat(filepath.Join(rtRtMihomoDir, rtRtPanelProfile)); err == nil {
		t.Error("после активации файл профиля панели остался на диске")
	}
	if k, _ := e.rtSwRouting()["mihomo"].(map[string]any); k == nil || k["taken"] != false {
		t.Errorf("после активации routing.mihomo: %v, ожидалось taken=false", e.rtSwRouting()["mihomo"])
	}

	// Смена своего профиля на другой при открытом черновике: черновик не касается.
	rev, changes := e.rtOwnOpenDraft()
	pid = rtSwExpectKernel(t, tg.Core, e.rtOwnActivate(rtOwnB), rtOwnB, pid)
	if !rtSwLinkOn(t, "profiles/"+rtOwnB+".yaml") {
		t.Fatalf("после смены config.yaml не указывает на %s", rtOwnB)
	}
	if got := e.rtRtRevision(); got != rev {
		t.Errorf("смена профиля: ревизия черновика %d -> %d", rev, got)
	}
	if got := e.rtRtDraftChanges(); got != changes {
		t.Errorf("смена профиля: изменений черновика %d -> %d", changes, got)
	}
	e.resetDraft()

	// Возврат панели, затем активация при открытом черновике: отказ без изменений.
	pid = rtSwExpectKernel(t, tg.Core, e.rtSwCall("return", ""), rtSwPanel, pid)
	m.taken = true
	if !rtSwLinkOn(t, rtRtPanelProfile) {
		t.Fatalf("после возврата config.yaml не указывает на %s", rtRtPanelProfile)
	}
	rev, changes = e.rtOwnOpenDraft()
	disk := rtRtScanDisk(t)
	r := e.callSlow(http.MethodPost, rtPrfActivate, map[string]string{"name": rtOwnA})
	e.rtRtExpect("активация при открытом черновике", r, http.StatusConflict, "draft_open")
	if got := rtRtScanDisk(t); got.mihomoTarget != disk.mihomoTarget || got.mihomoSum != disk.mihomoSum {
		t.Error("активация при открытом черновике изменила config.yaml")
	}
	if got := e.rtRtRevision(); got != rev {
		t.Errorf("отказ: ревизия черновика %d -> %d", rev, got)
	}
	if got := e.rtRtDraftChanges(); got != changes {
		t.Errorf("отказ: изменений черновика %d -> %d", changes, got)
	}
	e.resetDraft()

	// Стенд к возврату: Mihomo отпущен на прежний профиль (rt-own-b).
	rtSwExpectKernel(t, tg.Core, e.rtSwCall("release", ""), rtOwnB, pid)
	m.taken = false
}
