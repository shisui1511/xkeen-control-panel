//go:build router

package handlers

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
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
	s := rtPrfPrepare(t, kernel)
	// Включение слоя в подготовке запускает фоновую сборку, она может перезапустить
	// ядро: PID берётся после неё, сравнение идёт только по самой активации.
	pidBefore := rtRstKernelPID(t, kernel)

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

const (
	rtNamesA = "rt-names-a"
	rtNamesB = "rt-names-b"
	rtNamesC = "rt-names-c"
)

// rtNamesBackups — копии удалённых профилей rt-names-* в резервном каталоге панели.
func rtNamesBackups() []string {
	files, _ := filepath.Glob(filepath.Join(config.Default().DataDir, "backup", "mihomo-profiles", "rt-names-*.yaml"))
	return files
}

// rtNamesLinkTo переключает config.yaml на profiles/<name>.yaml напрямую, на диске:
// содержимое профилей одинаково, перезапуск ядру не нужен. Исходный вид config.yaml
// возвращает rtSwPrepare.
func rtNamesLinkTo(t *testing.T, s *rtSwStand, name string) {
	t.Helper()
	tmp := s.cfg + ".rt-link"
	_ = os.Remove(tmp)
	if err := os.Symlink(s.profile(name), tmp); err != nil {
		t.Fatalf("ссылка на %s: %v", name, err)
	}
	if err := os.Rename(tmp, s.cfg); err != nil {
		_ = os.Remove(tmp)
		t.Fatalf("config.yaml -> %s: %v", name, err)
	}
}

// rtNamesProfile ищет профиль в ответе списка.
func rtNamesProfile(list map[string]any, name string) map[string]any {
	items, _ := list["profiles"].([]any)
	for _, it := range items {
		if m, _ := it.(map[string]any); m != nil && m["name"] == name {
			return m
		}
	}
	return nil
}

// Имена профилей (D-24, D-12): префикс xcp- без учёта регистра занят панелью —
// профиль пользователя с таким именем не создать, не переименовать в него и не
// активировать; xcp-main нельзя удалить и переименовать; прочие файлы xcp- в списке не
// видны. Цель возврата (прежний профиль, пока Mihomo у панели) нельзя ни удалить, ни
// переименовать: 409 profile_return_target; после отпускания переименование проходит.
// Список отдаёт owner и return_target; удаление профиля пользователя оставляет копию.
// Метка ядра: отпускание проверяется по процессу активного ядра.
func TestRouterProfileNames(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	s := rtOwnPrepare(t, e, "rt-names-", rtNamesA, rtNamesB)
	t.Cleanup(func() {
		rtOwnRemove(s, "xcp-rt")
		for _, f := range rtNamesBackups() {
			_ = os.Remove(f)
		}
	})
	rtNamesLinkTo(t, s, rtNamesB)
	for _, f := range rtNamesBackups() {
		_ = os.Remove(f)
	}

	post := func(action string, body map[string]any) rtSrcResp {
		t.Helper()
		return e.call(http.MethodPost, "/api/mihomo/profiles/"+action, body)
	}

	// Зарезервированные имена при Mihomo не у панели.
	for _, name := range []string{"xcp-rt", "XCP-rt", "Xcp-rt"} {
		e.rtRtExpect("create "+name, post("create", map[string]any{"name": name, "from": rtNamesA}), http.StatusBadRequest, "profile_reserved")
	}
	e.rtRtExpect("create xcp-rt (пустой)", post("create", map[string]any{"name": "xcp-rt", "empty": true}), http.StatusBadRequest, "profile_reserved")
	e.rtRtExpect("rename в xcp-rt", post("rename", map[string]any{"name": rtNamesA, "new_name": "xcp-rt"}), http.StatusBadRequest, "profile_reserved")
	e.rtRtExpect("rename в XCP-rt", post("rename", map[string]any{"name": rtNamesA, "new_name": "XCP-rt"}), http.StatusBadRequest, "profile_reserved")
	e.rtRtExpect("rename xcp-main", post("rename", map[string]any{"name": "xcp-main", "new_name": "rt-names-x"}), http.StatusBadRequest, "profile_reserved")
	e.rtRtExpect("delete xcp-main", post("delete", map[string]any{"name": "xcp-main"}), http.StatusBadRequest, "profile_reserved")
	e.rtRtExpect("delete XCP-rt", post("delete", map[string]any{"name": "XCP-rt"}), http.StatusBadRequest, "profile_reserved")
	r := e.callSlow(http.MethodPost, rtPrfActivate, map[string]string{"name": "xcp-main"})
	e.rtRtExpect("activate xcp-main", r, http.StatusBadRequest, "profile_reserved")
	// adopt с именем панели отвергается так же, как create (T-147.1-27): перенос config.yaml
	// в profiles/xcp-*.yaml отдал бы файл слою как сироту.
	for _, name := range []string{"xcp-rt", "XCP-rt"} {
		e.rtRtExpect("adopt "+name, post("adopt", map[string]any{"name": name}), http.StatusBadRequest, "profile_reserved")
	}
	// Имя обычного файла XKeen и имена с суффиксом .yaml/.yml сервер не принимает
	// (WR-01 ревью): профиль profiles/config.yaml.yaml путался бы с обычным файлом.
	for _, name := range []string{"config.yaml", rtNamesA + ".yaml", rtNamesA + ".YML"} {
		if got := post("create", map[string]any{"name": name, "from": rtNamesA}); got.Status != http.StatusBadRequest {
			t.Errorf("create %q: %d %q, ожидалось 400", name, got.Status, got.Code)
		}
	}
	// Источник копии с префиксом xcp- — профиль панели, читать его нельзя (IN-02).
	e.rtRtExpect("create из xcp-orphan", post("create", map[string]any{"name": rtNamesA + "-copy", "from": "xcp-rt-orphan"}), http.StatusBadRequest, "profile_reserved")
	if _, err := os.Stat(s.profile(rtNamesA + "-copy")); err == nil {
		t.Error("копия из профиля панели создана")
	}
	if _, err := os.Stat(s.profile("xcp-rt")); err == nil {
		t.Error("профиль xcp-rt создан")
	}
	if _, err := os.Stat(s.profile(rtNamesA)); err != nil {
		t.Errorf("профиль %s исчез после отказов: %v", rtNamesA, err)
	}
	if !rtSwLinkOn(t, "profiles/"+rtNamesB+".yaml") {
		t.Errorf("отказы изменили config.yaml: не указывает на %s", rtNamesB)
	}

	// Панель владеет Mihomo: прежний профиль — цель возврата.
	m := rtCoresLiveMihomoTake(t, e, tg)
	if err := os.WriteFile(s.profile("xcp-rt-orphan"), s.data, s.mode); err != nil {
		t.Fatalf("файл сироты не записан: %v", err)
	}
	prevRel := "profiles/" + rtNamesB + ".yaml"
	if k, _ := e.rtSwRouting()["mihomo"].(map[string]any); k == nil || k["prev_profile"] != prevRel {
		t.Fatalf("prev_profile %v, ожидался %s", e.rtSwRouting()["mihomo"], prevRel)
	}
	e.rtRtExpect("activate xcp-main при панели", e.callSlow(http.MethodPost, rtPrfActivate, map[string]string{"name": "xcp-main"}), http.StatusBadRequest, "profile_reserved")
	e.rtRtExpect("delete цели возврата", post("delete", map[string]any{"name": rtNamesB}), http.StatusConflict, "profile_return_target")
	e.rtRtExpect("rename цели возврата", post("rename", map[string]any{"name": rtNamesB, "new_name": rtNamesC}), http.StatusConflict, "profile_return_target")
	if _, err := os.Stat(s.profile(rtNamesB)); err != nil {
		t.Errorf("цель возврата %s исчезла после отказов: %v", rtNamesB, err)
	}

	list := e.call(http.MethodGet, rtPrfList, nil)
	if list.Status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", rtPrfList, list.Status, list.Code)
	}
	state := list.object(t)
	if got, _ := state["return_target"].(string); got != rtNamesB {
		t.Errorf("return_target %q, ожидался %s", got, rtNamesB)
	}
	if p := rtNamesProfile(state, "xcp-main"); p == nil || p["owner"] != "panel" {
		t.Errorf("профиль xcp-main в списке: %v, ожидался owner panel", p)
	}
	if p := rtNamesProfile(state, rtNamesA); p == nil || p["owner"] != "user" {
		t.Errorf("профиль %s в списке: %v, ожидался owner user", rtNamesA, p)
	}
	if p := rtNamesProfile(state, "xcp-rt-orphan"); p != nil {
		t.Errorf("файл xcp-rt-orphan показан в списке профилей: %v", p)
	}

	// Удаление профиля пользователя оставляет копию в резервном каталоге панели.
	if r := post("delete", map[string]any{"name": rtNamesA}); r.Status != http.StatusOK {
		t.Fatalf("delete %s: %d %s", rtNamesA, r.Status, r.Code)
	}
	if _, err := os.Stat(s.profile(rtNamesA)); err == nil {
		t.Errorf("профиль %s остался после удаления", rtNamesA)
	}
	if len(rtNamesBackups()) == 0 {
		t.Error("после удаления копии профиля нет в резервном каталоге панели")
	}

	// После отпускания цели возврата нет: переименование проходит и ведёт ссылку.
	// PID берётся до вызова: аргументы вычисляются слева направо, иначе сравнение шло бы с уже новым процессом.
	pidBefore := rtRstKernelPID(t, tg.Core)
	rtSwExpectKernel(t, tg.Core, e.rtSwCall("release", ""), rtNamesB, pidBefore)
	m.taken = false
	if got := e.call(http.MethodGet, rtPrfList, nil).object(t)["return_target"]; got != nil && got != "" {
		t.Errorf("после отпускания return_target %v, ожидался пустой", got)
	}
	if r := post("rename", map[string]any{"name": rtNamesB, "new_name": rtNamesC}); r.Status != http.StatusOK {
		t.Fatalf("rename %s после отпускания: %d %s", rtNamesB, r.Status, r.Code)
	}
	if !rtSwLinkOn(t, "profiles/"+rtNamesC+".yaml") {
		t.Errorf("переименование активного профиля не повело ссылку на %s", rtNamesC)
	}
}

const (
	rtPlainA = "rt-plain-a"
	// rtPlainMode — права обычного config.yaml сценария: отличаются от прав профилей
	// панели (0600), чтобы откат был проверяем по правам.
	rtPlainMode = os.FileMode(0o640)
	rtKeepName  = "config-xkeen"
)

// rtPlainSet делает config.yaml обычным файлом с содержимым data и правами rtPlainMode
// (временный файл рядом и переименование поверх ссылки или файла).
func rtPlainSet(t *testing.T, s *rtSwStand, data []byte) {
	t.Helper()
	tmp := s.cfg + ".rt-plain"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, data, rtPlainMode); err != nil {
		t.Fatalf("временный config.yaml: %v", err)
	}
	if err := os.Chmod(tmp, rtPlainMode); err != nil {
		t.Fatalf("права config.yaml: %v", err)
	}
	if err := os.Rename(tmp, s.cfg); err != nil {
		_ = os.Remove(tmp)
		t.Fatalf("config.yaml обычным файлом: %v", err)
	}
}

// rtPlainExpectFile сверяет, что config.yaml — обычный файл с теми же байтами и правами.
func rtPlainExpectFile(t *testing.T, s *rtSwStand, label string, want []byte) {
	t.Helper()
	info, err := os.Lstat(s.cfg)
	if err != nil {
		t.Errorf("%s: config.yaml не читается: %v", label, err)
		return
	}
	if !info.Mode().IsRegular() {
		t.Errorf("%s: config.yaml не обычный файл (%s)", label, info.Mode())
		return
	}
	if info.Mode().Perm() != rtPlainMode {
		t.Errorf("%s: права config.yaml %v, ожидались %v", label, info.Mode().Perm(), rtPlainMode)
	}
	if got, err := os.ReadFile(s.cfg); err != nil || !bytes.Equal(got, want) {
		t.Errorf("%s: содержимое config.yaml изменилось: %v", label, err)
	}
}

// rtPlainKept — профили config-xkeen* каталога profiles/.
func rtPlainKept(s *rtSwStand) []string {
	files, _ := filepath.Glob(filepath.Join(s.profDir, rtKeepName+"*.yaml"))
	return files
}

// Перенос обычного config.yaml внутри активации (D-27): при включённом слое активация
// профиля на обычном файле XKeen одной операцией переносит файл в
// profiles/config-xkeen.yaml (содержимое то же), config.yaml ведёт на выбранный профиль,
// adopted называет копию; вернуться можно активацией config-xkeen; при занятом имени —
// суффикс -2. Отказ запуска (ветка mihomo, процесс гасится) возвращает обычный файл с
// теми же байтами и правами и убирает копию. При выключенном слое активация на обычном
// файле — 409, adopt работает. Метка ядра: сценарий читает активное ядро и проверяет его
// процесс. Вид config.yaml и файлы config-xkeen* возвращает rtSwPrepare.
func TestRouterProfileActivatePlain(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	s := rtOwnPrepare(t, e, "rt-plain-", rtPlainA)
	plain := append(append([]byte{}, s.data...), []byte("\n# rt-plain: прежний файл XKeen\n")...)
	keepPath := func(name string) string { return s.profile(name) }

	// Перенос обычного файла.
	rtPlainSet(t, s, plain)
	pid := rtRstKernelPID(t, tg.Core)
	res := e.rtOwnActivate(rtPlainA)
	pid = rtSwExpectKernel(t, tg.Core, res, rtPlainA, pid)
	if got, _ := res["adopted"].(string); got != rtKeepName {
		t.Fatalf("adopted %q, ожидался %s", got, rtKeepName)
	}
	if got, err := os.ReadFile(keepPath(rtKeepName)); err != nil || !bytes.Equal(got, plain) {
		t.Errorf("копия %s не совпадает побайтно с прежним config.yaml: %v", rtKeepName, err)
	}
	if !rtSwLinkOn(t, "profiles/"+rtPlainA+".yaml") {
		t.Fatalf("после активации config.yaml не указывает на %s", rtPlainA)
	}

	// Возврат активацией config-xkeen: ссылка, переноса уже нет.
	res = e.rtOwnActivate(rtKeepName)
	pid = rtSwExpectKernel(t, tg.Core, res, rtKeepName, pid)
	if got, _ := res["adopted"].(string); got != "" {
		t.Errorf("активация config-xkeen: adopted %q, ожидался пустой", got)
	}
	if !rtSwLinkOn(t, "profiles/"+rtKeepName+".yaml") {
		t.Fatalf("после возврата config.yaml не указывает на %s", rtKeepName)
	}

	// Коллизия: имя config-xkeen занято, копия получает суффикс.
	rtPlainSet(t, s, plain)
	res = e.rtOwnActivate(rtPlainA)
	rtSwExpectKernel(t, tg.Core, res, rtPlainA, pid)
	if got, _ := res["adopted"].(string); got != rtKeepName+"-2" {
		t.Fatalf("adopted %q при занятом имени, ожидался %s-2", got, rtKeepName)
	}
	if got, err := os.ReadFile(keepPath(rtKeepName + "-2")); err != nil || !bytes.Equal(got, plain) {
		t.Errorf("копия %s-2 не совпадает побайтно: %v", rtKeepName, err)
	}

	// Отказ запуска: пока config.yaml указывает на rt-plain-a, процесс гасится.
	if tg.Core == "mihomo" {
		rtPlainSet(t, s, plain)
		kept := rtPlainKept(s)
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
				target, err := os.Readlink(s.cfg)
				if err != nil || filepath.Base(target) != rtPlainA+".yaml" {
					continue
				}
				pids, _ := rtRstKernelPIDs("mihomo")
				for _, p := range pids {
					_ = syscall.Kill(p, syscall.SIGKILL)
				}
			}
		}()
		t.Cleanup(func() {
			cancel()
			wg.Wait()
		})
		pidBefore := rtRstKernelPID(t, "mihomo")
		r := e.callSlow(http.MethodPost, rtPrfActivate, map[string]string{"name": rtPlainA})
		cancel()
		wg.Wait()
		if r.Status != http.StatusOK {
			t.Fatalf("activate %s: %d %s", rtPlainA, r.Status, r.Code)
		}
		out := r.object(t)
		if rb, _ := out["rolled_back"].(bool); !rb {
			t.Fatalf("rolled_back=false при погашенном процессе: restarted=%v", out["restarted"])
		}
		if msg, _ := out["error"].(string); msg == "" {
			t.Error("после отката текст error пуст")
		}
		if got, _ := out["adopted"].(string); got != "" {
			t.Errorf("после отката adopted %q, ожидался пустой", got)
		}
		rtPlainExpectFile(t, s, "после отката", plain)
		if got := rtPlainKept(s); !reflect.DeepEqual(got, kept) {
			t.Errorf("после отката набор config-xkeen* изменился: было %v, стало %v", kept, got)
		}
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

	// Слой выключен: активация на обычном файле отказывает, adopt работает (CUT-01).
	e.setLayer(false)
	rtPlainSet(t, s, plain)
	r := e.callSlow(http.MethodPost, rtPrfActivate, map[string]string{"name": rtPlainA})
	if r.Status != http.StatusConflict {
		t.Errorf("активация на обычном файле при выключенном слое: %d %s, ожидался 409", r.Status, r.Code)
	}
	rtPlainExpectFile(t, s, "после отказа при выключенном слое", plain)
	if a := e.call(http.MethodPost, "/api/mihomo/profiles/adopt", map[string]string{"name": "rt-plain-adopt"}); a.Status != http.StatusOK {
		t.Fatalf("adopt при выключенном слое: %d %s", a.Status, a.Code)
	}
	if !rtSwLinkOn(t, "profiles/rt-plain-adopt.yaml") {
		t.Error("после adopt config.yaml не указывает на rt-plain-adopt")
	}
	if got, err := os.ReadFile(s.profile("rt-plain-adopt")); err != nil || !bytes.Equal(got, plain) {
		t.Errorf("adopt: содержимое профиля не совпадает с прежним config.yaml: %v", err)
	}

	// Слой возвращается включённым, как при снимке стенда: снимок файлов Xray и
	// сброс черновика в очистке идут при включённом слое (файлы слоя, например
	// диагностики, при выключении уходят из каталога).
	e.setLayer(true)
	e.rtRstIdle()
}
