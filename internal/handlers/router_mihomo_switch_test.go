//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Переход панель ↔ свой профиль Mihomo одной операцией на настоящей панели
// устройства (PART-02, PART-03; D-06…D-10): POST /api/configlayer/routing/mihomo
// {action: activate|release|return}. Вердикт — процессы ядер в /proc (pidof,
// независимо от панели), ссылка config.yaml и файлы профилей и Xray на диске;
// ответы и снимок слоя — вспомогательные. Свои профили rt-sw-* создаются копией
// действующего config.yaml; в t.Cleanup стенд возвращается при любом исходе:
// маршрутизация отпускается, config.yaml, каталог профилей и файлы Xray — как были.

const (
	// rtSwBase — маршрут перехода.
	rtSwBase = rtRoutingBase + "/mihomo"
	// rtSwA, rtSwB, rtSwC — свои профили сценариев.
	rtSwA = "rt-sw-a"
	rtSwB = "rt-sw-b"
	rtSwC = "rt-sw-c"
	// rtSwPanel — имя профиля панели в ответах: под ним активным профилем числится
	// профиль панели, пока Mihomo у неё.
	rtSwPanel = "xcp-main"
)

// rtSwStand — исходное состояние устройства для возврата после сценария.
type rtSwStand struct {
	t       *testing.T
	tg      routertest.Target
	cfg     string // config.yaml Mihomo
	link    string // текст исходного симлинка
	linkErr error  // не nil — config.yaml был обычным файлом
	data    []byte // содержимое действующего config.yaml
	mode    os.FileMode
	profDir string          // каталог профилей
	existed bool            // каталог профилей был до сценария
	before  map[string]bool // имена в каталоге профилей до сценария
	xray    map[string]string
}

// rtSwPrepare снимает исходное состояние, убирает следы маршрутизации, создаёт своё
// профили names копией действующего config.yaml и регистрирует возврат стенда.
// Выполняется до взятия маршрутизации: возврат идёт после очистки взятия.
func rtSwPrepare(t *testing.T, e *rtSrc, tg routertest.Target, names ...string) *rtSwStand {
	t.Helper()
	s := &rtSwStand{
		t: t, tg: tg, mode: 0o600,
		cfg:     filepath.Join(rtRtMihomoDir, "config.yaml"),
		profDir: filepath.Join(rtRtMihomoDir, "profiles"),
	}
	data, err := os.ReadFile(s.cfg)
	if err != nil {
		t.Fatalf("config.yaml Mihomo не читается: %v", err)
	}
	s.data = data
	if s.link, s.linkErr = os.Readlink(s.cfg); s.linkErr == nil {
		if strings.HasPrefix(strings.TrimSuffix(filepath.Base(s.link), ".yaml"), "rt-sw-") {
			t.Fatalf("config.yaml указывает на профиль прошлого прогона: %s", filepath.Base(s.link))
		}
	} else if info, err := os.Stat(s.cfg); err == nil {
		s.mode = info.Mode().Perm()
	}
	s.removeOwn()
	entries, dirErr := os.ReadDir(s.profDir)
	s.existed = dirErr == nil
	s.before = map[string]bool{}
	for _, en := range entries {
		s.before[en.Name()] = true
	}

	e.rtRtPurge()
	s.xray = rtCoresDirSums(t)
	t.Cleanup(s.restore)

	if err := os.MkdirAll(s.profDir, 0o755); err != nil {
		t.Fatalf("каталог профилей Mihomo не создан: %v", err)
	}
	for _, name := range names {
		if err := os.WriteFile(s.profile(name), data, s.mode); err != nil {
			t.Fatalf("профиль %s не записан: %v", name, err)
		}
	}
	return s
}

func (s *rtSwStand) profile(name string) string {
	return filepath.Join(s.profDir, name+".yaml")
}

// read читает профиль; нет файла — провал.
func (s *rtSwStand) read(name string) []byte {
	s.t.Helper()
	data, err := os.ReadFile(s.profile(name))
	if err != nil {
		s.t.Fatalf("профиль %s не читается: %v", name, err)
	}
	return data
}

// removeOwn удаляет профили rt-sw-* (остатки прошлого прогона и свои) и профили
// config-xkeen*, которых до сценария не было (панель сохраняет в них прежний обычный
// файл при смене профиля, D-27).
func (s *rtSwStand) removeOwn() {
	own, _ := filepath.Glob(filepath.Join(s.profDir, "rt-sw-*.yaml"))
	for _, f := range own {
		_ = os.Remove(f)
	}
	kept, _ := filepath.Glob(filepath.Join(s.profDir, "config-xkeen*.yaml"))
	for _, f := range kept {
		if s.before != nil && !s.before[filepath.Base(f)] {
			_ = os.Remove(f)
		}
	}
}

// restore возвращает config.yaml, каталог профилей и сверяет файлы Xray.
func (s *rtSwStand) restore() {
	t := s.t
	s.removeOwn()
	tmp := s.cfg + ".rt-restore"
	_ = os.Remove(tmp)
	var err error
	if s.linkErr == nil {
		err = os.Symlink(s.link, tmp)
	} else {
		err = os.WriteFile(tmp, s.data, s.mode)
	}
	if err == nil {
		err = os.Rename(tmp, s.cfg)
	}
	if err != nil {
		t.Errorf("возврат config.yaml: %v", err)
	}
	if !s.existed {
		_ = os.Remove(s.profDir)
	}
	if got := rtCoresDirSums(t); !reflect.DeepEqual(got, s.xray) {
		t.Errorf("файлы каталога конфигураций Xray после сценария отличаются от исходных")
	}
	// Содержимое профилей одинаково, перезапуск ядру не нужен, кроме случая, когда
	// сценарий оставил его остановленным.
	if s.tg.Core == "mihomo" {
		rtPrfEnsureRunning(t)
	}
}

// rtSwLinkOn — config.yaml указывает на profiles/<name>.yaml.
func rtSwLinkOn(t *testing.T, rel string) bool {
	t.Helper()
	return strings.HasSuffix(rtRtScanDisk(t).mihomoTarget, rel)
}

// rtSwCall вызывает действие перехода и возвращает разобранный ответ; статус не 200 — провал.
func (e *rtSrc) rtSwCall(action, profile string) map[string]any {
	e.t.Helper()
	body := map[string]string{"action": action}
	if profile != "" {
		body["profile"] = profile
	}
	r := e.callSlow(http.MethodPost, rtSwBase, body)
	if r.Status != http.StatusOK {
		e.t.Fatalf("%s: %d %s", action, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtSwRouting читает раздел routing снимка слоя.
func (e *rtSrc) rtSwRouting() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	routing, _ := r.object(e.t)["routing"].(map[string]any)
	if routing == nil {
		e.t.Fatalf("в снимке слоя нет routing: %.200s", r.Data)
	}
	return routing
}

// rtSwJSON — стабильная запись значения для сравнения «как до шага».
func rtSwJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

// rtSwSeedKeys записывает в «Прочие ключи» Mihomo hosts, ntp и sub-rules сценария и
// применяет: значения секций, которые обязаны пережить переход.
func (e *rtSrc) rtSwSeedKeys() {
	e.t.Helper()
	sec := rtCoresSection(e.rtCoresView("mihomo"), "other")
	if sec == nil {
		e.t.Fatal("в виде Mihomo нет секции other")
	}
	advanced, _ := sec["advanced"].(string)
	r := e.rtCoresPost("/save", map[string]any{
		"kernel": "mihomo", "section": "other", "form": map[string]any{},
		"advanced": rtCoresRetakeMihomoKeys(e.t, advanced),
	})
	if r.Status != http.StatusOK {
		e.t.Fatalf("save other (mihomo): %d %s", r.Status, r.Code)
	}
	e.rtRtApplyOK("правка «Прочих ключей» Mihomo")
	rtCoresRetakeExpectKeys(e.t, "до перехода")
}

// rtSwExpectKernel проверяет ответ перехода и ядро: при активном Mihomo — успех с
// новым устойчивым PID (restarted), при активном Xray — профиль сохранён без
// перезапуска (saved_kernel_inactive), PID Xray прежний. Возвращает PID активного ядра.
func rtSwExpectKernel(t *testing.T, tg routertest.Target, res map[string]any, active string, pidBefore int) int {
	t.Helper()
	if rb, _ := res["rolled_back"].(bool); rb {
		t.Fatalf("переход откатился: ошибка %q", res["error"])
	}
	if got, _ := res["active"].(string); got != active {
		t.Fatalf("active %q, ожидался %s", got, active)
	}
	restarted, _ := res["restarted"].(bool)
	switch tg.Core {
	case "mihomo":
		if !restarted {
			t.Fatalf("restarted=false при работающем Mihomo; outcome %q", res["outcome"])
		}
		pid := rtRstKernelPID(t, "mihomo")
		if pid == pidBefore {
			t.Fatalf("PID mihomo не сменился после перехода (%d): успех засчитан по старому процессу", pidBefore)
		}
		rtRstHoldPID(t, "mihomo", pid)
		return pid
	default:
		if restarted {
			t.Fatal("restarted=true при активном Xray")
		}
		if out, _ := res["outcome"].(string); out != "saved_kernel_inactive" {
			t.Fatalf("outcome %q, ожидался saved_kernel_inactive", out)
		}
		if pid := rtRstKernelPID(t, "xray"); pid != pidBefore {
			t.Fatalf("PID xray сменился при переходе Mihomo: %d -> %d", pidBefore, pid)
		}
		return pidBefore
	}
}

// Трасса (PART-03, A6): Mihomo у панели, в «Прочих ключах» записаны значения →
// activate rt-sw-a → ссылка на rt-sw-a, профиль панели удалён, Mihomo отпущен со
// значениями секций, Xray и его файлы не тронуты → return → профиль панели с теми же
// значениями, прежний профиль в снимке — rt-sw-a, файл rt-sw-a побайтно прежний →
// release → снова rt-sw-a. Активное ядро Mihomo — полный перезапуск с новым PID на
// каждом шаге; активное Xray — профиль сохраняется, PID Xray прежний. Метка ядра:
// сценарий читает активное ядро и проверяет его процесс.
func TestRouterMihomoSwitch(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	s := rtSwPrepare(t, e, tg, rtSwA)
	m := rtCoresLiveMihomoTake(t, e, tg)
	e.rtSwSeedKeys()

	aSum := rtRtSum(s.read(rtSwA))
	xrayFiles := rtCoresDirSums(t)
	xrayOwn := rtSwJSON(e.rtSwRouting()["xray"])
	pid := rtRstKernelPID(t, tg.Core)

	expectXrayUntouched := func(label string) {
		t.Helper()
		if got := rtSwJSON(e.rtSwRouting()["xray"]); got != xrayOwn {
			t.Errorf("%s: владение Xray в снимке изменилось: %s -> %s", label, xrayOwn, got)
		}
		if got := rtCoresDirSums(t); !reflect.DeepEqual(got, xrayFiles) {
			t.Errorf("%s: файлы каталога конфигураций Xray изменились", label)
		}
	}
	expectApplied := func(label string) {
		t.Helper()
		rtRstExpectApplied(t, e.rtRstApplyView())
		if n := e.rtRtDraftChanges(); n != 0 {
			t.Errorf("%s: в черновике %d изменений после синхронного применения", label, n)
		}
	}

	// activate: профиль панели сменяется своим профилем, Mihomo отпущен.
	pid = rtSwExpectKernel(t, tg, e.rtSwCall("activate", rtSwA), rtSwA, pid)
	expectApplied("activate")
	if !rtSwLinkOn(t, "profiles/"+rtSwA+".yaml") {
		t.Fatalf("после activate config.yaml не указывает на %s", rtSwA)
	}
	if _, err := os.Stat(filepath.Join(rtRtMihomoDir, rtRtPanelProfile)); err == nil {
		t.Error("после activate файл профиля панели остался на диске")
	}
	routing := e.rtSwRouting()
	if k, _ := routing["mihomo"].(map[string]any); k == nil || k["taken"] != false {
		t.Errorf("после activate routing.mihomo: %v, ожидалось taken=false", routing["mihomo"])
	}
	if routing["desired_mihomo"] != false {
		t.Errorf("после activate desired_mihomo=%v, ожидалось false", routing["desired_mihomo"])
	}
	if routing["mihomo_saved"] != true {
		t.Errorf("после activate mihomo_saved=%v, ожидалось true: значения секций обязаны сохраниться", routing["mihomo_saved"])
	}
	expectXrayUntouched("activate")
	m.taken = false

	// return: Mihomo снова у панели, значения секций вернулись, прежний профиль — rt-sw-a.
	pid = rtSwExpectKernel(t, tg, e.rtSwCall("return", ""), rtSwPanel, pid)
	expectApplied("return")
	m.taken = true
	if !rtSwLinkOn(t, rtRtPanelProfile) {
		t.Fatalf("после return config.yaml не указывает на %s", rtRtPanelProfile)
	}
	rtCoresRetakeExpectKeys(t, "после return")
	routing = e.rtSwRouting()
	k, _ := routing["mihomo"].(map[string]any)
	if k == nil || k["taken"] != true {
		t.Fatalf("после return routing.mihomo: %v, ожидалось taken=true", routing["mihomo"])
	}
	if prev, _ := k["prev_profile"].(string); prev != "profiles/"+rtSwA+".yaml" {
		t.Errorf("после return prev_profile %q, ожидался profiles/%s.yaml (A6)", prev, rtSwA)
	}
	if routing["desired_mihomo"] != true {
		t.Errorf("после return desired_mihomo=%v, ожидалось true", routing["desired_mihomo"])
	}
	if got := rtRtSum(s.read(rtSwA)); got != aSum {
		t.Error("файл rt-sw-a изменился после возврата панели")
	}
	expectXrayUntouched("return")

	// release: Mihomo уходит на прежний профиль — rt-sw-a, а не на исходный (A6).
	rtSwExpectKernel(t, tg, e.rtSwCall("release", ""), rtSwA, pid)
	expectApplied("release")
	m.taken = false
	if !rtSwLinkOn(t, "profiles/"+rtSwA+".yaml") {
		t.Fatalf("после release config.yaml не указывает на %s", rtSwA)
	}
	if _, err := os.Stat(filepath.Join(rtRtMihomoDir, rtRtPanelProfile)); err == nil {
		t.Error("после release файл профиля панели остался на диске")
	}
	if k, _ := e.rtSwRouting()["mihomo"].(map[string]any); k == nil || k["taken"] != false {
		t.Errorf("после release routing.mihomo: %v, ожидалось taken=false", e.rtSwRouting()["mihomo"])
	}
	if got := rtRtSum(s.read(rtSwA)); got != aSum {
		t.Error("файл rt-sw-a изменился после отпускания")
	}
	expectXrayUntouched("release")
}
