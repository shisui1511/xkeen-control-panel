package configlayer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// fastRestartTimings сокращает ожидания шага перезапуска на время теста.
func fastRestartTimings(t *testing.T) {
	t.Helper()
	oldC, oldP, oldS := RestartConfirmTimeout, RestartPollInterval, RestartStableWindow
	RestartConfirmTimeout = 2 * time.Second
	RestartPollInterval = 10 * time.Millisecond
	RestartStableWindow = 50 * time.Millisecond
	t.Cleanup(func() {
		RestartConfirmTimeout, RestartPollInterval, RestartStableWindow = oldC, oldP, oldS
	})
}

// fakeProcs — управляемые состояния процессов ядер (порядок xray, mihomo).
type fakeProcs struct {
	mu sync.Mutex
	m  map[string]services.KernelProcessState
}

func newFakeProcs() *fakeProcs {
	return &fakeProcs{m: map[string]services.KernelProcessState{
		"xray":   {Name: "xray", Status: "not_installed"},
		"mihomo": {Name: "mihomo", Status: "not_installed"},
	}}
}

func (f *fakeProcs) set(name, status string, pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[name] = services.KernelProcessState{Name: name, Status: status, PID: pid}
}

func (f *fakeProcs) states() []services.KernelProcessState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []services.KernelProcessState{f.m["xray"], f.m["mihomo"]}
}

func (f *fakeProcs) status(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[name].Status
}

// procApplier — настоящий services.KernelApplier поверх fakeProcs; считает
// вызовы ApplyLocked. Рестарт имитирует функция restart.
type procApplier struct {
	inner *services.KernelApplier
	mu    sync.Mutex
	calls []string
}

func newProcApplier(procs *fakeProcs, restart func() (string, error)) *procApplier {
	return &procApplier{inner: services.NewKernelApplierFunc(procs.status, func() string { return "" }, restart)}
}

func (a *procApplier) Preview(targets ...string) services.ApplyResult {
	return a.inner.Preview(targets...)
}

func (a *procApplier) ApplyLocked(targets ...string) services.ApplyResult {
	a.mu.Lock()
	a.calls = append(a.calls, targets...)
	a.mu.Unlock()
	return a.inner.ApplyLocked(targets...)
}

func (a *procApplier) applyCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

func restartOf(t *testing.T, v ApplyView, kernel string) RestartView {
	t.Helper()
	for _, r := range v.Restart {
		if r.Kernel == kernel {
			return r
		}
	}
	t.Fatalf("нет RestartView для %s в %+v", kernel, v.Restart)
	return RestartView{}
}

func TestApply_XrayRestartConfirmed(t *testing.T) {
	fastRestartTimings(t)
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	procs := newFakeProcs()
	procs.set("xray", "running", 100)
	var env *testEnv
	manifestAtRestart := -1
	restarts := 0
	applier := newProcApplier(procs, func() (string, error) {
		restarts++
		manifestAtRestart = len(env.Store.Snapshot().Manifest)
		procs.set("xray", "running", 200)
		return "", nil
	})
	env = newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}, DevMode: true, Applier: applier, Procs: procs.states})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if restarts != 1 {
		t.Errorf("restart вызван %d раз, want 1", restarts)
	}
	if rv := restartOf(t, view, KernelXray); rv.Outcome != "restarted" || rv.NoteCode != "" {
		t.Errorf("RestartView = %+v, want xray restarted", rv)
	}
	if manifestAtRestart != 0 {
		t.Errorf("манифест на момент рестарта содержит %d записей, want 0 (коммит после рестарта)", manifestAtRestart)
	}
	if got := len(env.Store.Snapshot().Manifest); got != 1 {
		t.Errorf("манифест после применения = %d записей, want 1", got)
	}
	if s := stepOf(t, view, StepRestart); s.State != StepDone {
		t.Errorf("restart = %+v, want done", s)
	}
	if env.Store.Snapshot().Journal != nil {
		t.Error("журнал записи не очищен после коммита")
	}
}

func TestApply_StoppedKernelNotStarted(t *testing.T) {
	fastRestartTimings(t)
	binDir := t.TempDir()
	xray := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	procs := newFakeProcs()
	procs.set("xray", "stopped", 0)
	restarts := 0
	applier := newProcApplier(procs, func() (string, error) {
		restarts++
		return "", nil
	})
	env := newTestPipeline(t, pipeOpts{Bins: Binaries{Xray: xray}, DevMode: true, Applier: applier, Procs: procs.states})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultApplied || view.Result.Written != 1 {
		t.Fatalf("Result = %+v, want applied, Written 1", view.Result)
	}
	if restarts != 0 {
		t.Errorf("restart вызван %d раз у остановленного ядра, want 0", restarts)
	}
	if rv := restartOf(t, view, KernelXray); rv.Outcome != "deferred" || rv.NoteCode != "kernel_stopped" {
		t.Errorf("RestartView = %+v, want xray deferred kernel_stopped", rv)
	}
	if s := stepOf(t, view, StepRestart); s.State != StepDeferred || s.NoteCode != "kernel_stopped" {
		t.Errorf("restart = %+v, want deferred kernel_stopped", s)
	}
	if got := len(env.Store.Snapshot().Manifest); got != 1 {
		t.Errorf("манифест = %d записей, want 1 (файлы записаны и закоммичены)", got)
	}
}

// Непустые содержимое провайдера: пустой файл сборка отклоняет.
const (
	providerV1 = "proxies:\n  - {name: n0, type: direct}\n"
	providerV2 = "proxies:\n  - {name: n0, type: direct}\n  - {name: n1, type: direct}\n"
)

// fileGen — тестовый генератор одного файла; содержимое и ожидание меняются
// между запусками.
type fileGen struct {
	mu      sync.Mutex
	kernel  string
	rel     string
	kind    FileKind
	content string
	expect  *Expectation
}

func (g *fileGen) ID() string { return "test-file" }

func (g *fileGen) Generate(Sections, InstalledKernels) ([]GeneratedFile, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return []GeneratedFile{{Kernel: g.kernel, RelPath: g.rel, Kind: g.kind, Content: []byte(g.content), Expect: g.expect}}, nil
}

func (g *fileGen) set(content string) {
	g.mu.Lock()
	g.content = content
	g.mu.Unlock()
}

func newMihomoGen(content string, exp *Expectation) *fileGen {
	return &fileGen{kernel: KernelMihomo, rel: "proxy_providers/xcp-a.yaml", kind: KindMihomoProxyProvider, content: content, expect: exp}
}

func newXrayGen(content string) *fileGen {
	return &fileGen{kernel: KernelXray, rel: "04_outbounds.xcp-a.tail.json", kind: KindXrayJSON, content: content}
}

// fakeMihomo — управляемый Clash API Mihomo.
type fakeMihomo struct {
	mu          sync.Mutex
	reloadErr   error
	reloadPaths []string
	count       func(call int) (int, error)
	countCalls  int
}

func (m *fakeMihomo) ReloadConfig(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadPaths = append(m.reloadPaths, path)
	return m.reloadErr
}

func (m *fakeMihomo) ProviderCount(_ context.Context, _, _ string) (int, error) {
	m.mu.Lock()
	m.countCalls++
	n, f := m.countCalls, m.count
	m.mu.Unlock()
	if f == nil {
		return 0, errors.New("нет ответа")
	}
	return f(n)
}

func (m *fakeMihomo) setReloadErr(err error) {
	m.mu.Lock()
	m.reloadErr = err
	m.mu.Unlock()
}

func (m *fakeMihomo) paths() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.reloadPaths...)
}

func hasStepNote(events []Event, id StepID, note string) bool {
	for _, ev := range events {
		if sv, ok := ev.Data.(StepView); ok && ev.Type == EventApplyStep && sv.ID == id && sv.NoteCode == note {
			return true
		}
	}
	return false
}

func TestApply_MihomoHotReload(t *testing.T) {
	fastRestartTimings(t)
	binDir := t.TempDir()
	mihomoBin := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	procs := newFakeProcs()
	procs.set("mihomo", "running", 100)
	restarts := 0
	applier := newProcApplier(procs, func() (string, error) { restarts++; return "", nil })
	mh := &fakeMihomo{}
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Mihomo: mihomoBin}, DevMode: true,
		Applier: applier, Procs: procs.states, Mihomo: mh, APIReady: func() bool { return true },
	})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if rv := restartOf(t, view, KernelMihomo); rv.Outcome != "hot_reloaded" {
		t.Errorf("RestartView = %+v, want hot_reloaded", rv)
	}
	if calls := applier.applyCalls(); len(calls) != 0 || restarts != 0 {
		t.Errorf("ApplyLocked вызван %v, restart %d раз; want ни разу", calls, restarts)
	}
	want := filepath.Join(env.Roots.Mihomo, "config.yaml")
	if got := mh.paths(); len(got) != 1 || got[0] != want {
		t.Errorf("ReloadConfig получил %v, want [%s]", got, want)
	}
	if s := stepOf(t, view, StepRestart); s.State != StepDone {
		t.Errorf("restart = %+v, want done", s)
	}
}

func TestApply_MihomoHotReloadFallback(t *testing.T) {
	fastRestartTimings(t)
	binDir := t.TempDir()
	mihomoBin := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	procs := newFakeProcs()
	procs.set("mihomo", "running", 100)
	applier := newProcApplier(procs, func() (string, error) {
		procs.set("mihomo", "running", 200)
		return "", nil
	})
	mh := &fakeMihomo{reloadErr: errors.New("status 503")}
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Mihomo: mihomoBin}, DevMode: true,
		Applier: applier, Procs: procs.states, Mihomo: mh, APIReady: func() bool { return true },
	})
	writeDiagBoth(t, env)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if rv := restartOf(t, view, KernelMihomo); rv.Outcome != "restarted_after_reload_failed" {
		t.Errorf("RestartView = %+v, want restarted_after_reload_failed", rv)
	}
	if calls := applier.applyCalls(); len(calls) != 1 || calls[0] != "mihomo" {
		t.Errorf("ApplyLocked вызван %v, want [mihomo]", calls)
	}
	if !hasStepNote(env.drainEvents(), StepRestart, "hot_reload_failed_restarting") {
		t.Error("пояснение hot_reload_failed_restarting не опубликовано")
	}
}

func TestApply_MihomoExpectMismatch(t *testing.T) {
	fastRestartTimings(t)
	binDir := t.TempDir()
	mihomoBin := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	procs := newFakeProcs()
	procs.set("mihomo", "running", 100)
	applier := newProcApplier(procs, func() (string, error) {
		procs.set("mihomo", "running", 200)
		return "", nil
	})
	// Горячая перезагрузка видит пустой провайдер; после рестарта — три узла.
	mh := &fakeMihomo{count: func(call int) (int, error) {
		if call == 1 {
			return 0, nil
		}
		return 3, nil
	}}
	gen := newMihomoGen(providerV1, &Expectation{ProviderType: "proxies", ProviderName: "xcp-a", Count: 3})
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Mihomo: mihomoBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states, Mihomo: mh, APIReady: func() bool { return true },
	})

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultApplied {
		t.Fatalf("Result = %+v, want applied", view.Result)
	}
	if rv := restartOf(t, view, KernelMihomo); rv.Outcome != "restarted_after_reload_failed" {
		t.Errorf("RestartView = %+v, want restarted_after_reload_failed (сверка Expect не сошлась)", rv)
	}
	if calls := applier.applyCalls(); len(calls) != 1 {
		t.Errorf("ApplyLocked вызван %v, want один раз", calls)
	}
	if mh.countCalls < 2 {
		t.Errorf("ProviderCount вызван %d раз, want >= 2 (до и после рестарта)", mh.countCalls)
	}
}

// setRollbackTimings ускоряет ожидание процесса, который не поднимется.
func setRollbackTimings(t *testing.T) {
	t.Helper()
	fastRestartTimings(t)
	RestartConfirmTimeout = 300 * time.Millisecond
}

// applyBaseline применяет первую версию файла и возвращает состояние до сбоя.
func applyBaseline(t *testing.T, env *testEnv, gen *fileGen, content string) State {
	t.Helper()
	gen.set(content)
	env.setDraft(t, "note", `{"a":1}`)
	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})
	if view.Result == nil || !view.Result.OK || view.Result.Code != ResultApplied {
		t.Fatalf("базовое применение = %+v, want applied", view.Result)
	}
	env.setDraft(t, "note", `{"a":2}`)
	return env.Store.Snapshot()
}

func requireRolledBack(t *testing.T, env *testEnv, view ApplyView, abs, wantContent string, before State, kernel string) {
	t.Helper()
	r := view.Result
	if r == nil || r.OK || r.Code != ResultRestartFailed || !r.RolledBack || r.Kernel != kernel {
		t.Fatalf("Result = %+v, want restart_failed_rolled_back, RolledBack, kernel %s", r, kernel)
	}
	if r.Message == "" {
		t.Error("итог без сообщения")
	}
	if got := mustRead(t, abs); got != wantContent {
		t.Errorf("файл после отката = %q, want %q (байты прежней версии)", got, wantContent)
	}
	after := env.Store.Snapshot()
	if after.Journal != nil {
		t.Errorf("журнал не очищен: %+v", after.Journal)
	}
	for k, e := range before.Manifest {
		if after.Manifest[k].Hash != e.Hash {
			t.Errorf("манифест %s изменён: %s -> %s", k, e.Hash, after.Manifest[k].Hash)
		}
	}
	if !SectionEqual(after.Applied["note"], before.Applied["note"]) {
		t.Errorf("Applied изменён: %s -> %s", before.Applied["note"], after.Applied["note"])
	}
	if !SectionEqual(after.Draft["note"], before.Draft["note"]) {
		t.Errorf("черновик тронут: %s -> %s", before.Draft["note"], after.Draft["note"])
	}
	if s := stepOf(t, view, StepRestart); s.State != StepFailed {
		t.Errorf("restart = %+v, want failed", s)
	}
}

func TestApply_MihomoFallbackRollback(t *testing.T) {
	setRollbackTimings(t)
	binDir := t.TempDir()
	mihomoBin := writeFakeKernel(t, binDir, "mihomo", 0, "", 0)
	procs := newFakeProcs()
	procs.set("mihomo", "running", 100)
	failing := false
	applier := newProcApplier(procs, func() (string, error) {
		if failing {
			procs.set("mihomo", "stopped", 0)
		} else {
			procs.set("mihomo", "running", 100)
		}
		return "", nil
	})
	mh := &fakeMihomo{}
	gen := newMihomoGen(providerV1, nil)
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Mihomo: mihomoBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states, Mihomo: mh, APIReady: func() bool { return true },
	})
	before := applyBaseline(t, env, gen, providerV1)
	abs := filepath.Join(env.Roots.Mihomo, "proxy_providers/xcp-a.yaml")
	env.drainEvents()

	failing = true
	mh.setReloadErr(errors.New("status 503"))
	gen.set(providerV2)
	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	requireRolledBack(t, env, view, abs, providerV1, before, KernelMihomo)
	if calls := applier.applyCalls(); len(calls) != 2 {
		t.Errorf("ApplyLocked вызван %v, want дважды (рестарт и повтор на прежних файлах)", calls)
	}
	if rv := restartOf(t, view, KernelMihomo); rv.Outcome != "failed_rolled_back" || rv.NoteCode != "restart_failed_rolling_back" {
		t.Errorf("RestartView = %+v, want failed_rolled_back restart_failed_rolling_back", rv)
	}
	ev := env.drainEvents()
	if !hasStepNote(ev, StepRestart, "hot_reload_failed_restarting") || !hasStepNote(ev, StepRestart, "restart_failed_rolling_back") {
		t.Error("пояснения hot_reload_failed_restarting и restart_failed_rolling_back не опубликованы")
	}
}

func TestApply_XrayRestartFailedRollsBack(t *testing.T) {
	setRollbackTimings(t)
	binDir := t.TempDir()
	xrayBin := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	procs := newFakeProcs()
	procs.set("xray", "running", 100)
	failing := false
	applier := newProcApplier(procs, func() (string, error) {
		if failing {
			procs.set("xray", "stopped", 0)
		} else {
			procs.set("xray", "running", 100)
		}
		return "", nil
	})
	gen := newXrayGen("{}\n")
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Xray: xrayBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states,
	})
	before := applyBaseline(t, env, gen, "{}\n")
	abs := filepath.Join(env.Roots.Xray, "04_outbounds.xcp-a.tail.json")

	failing = true
	gen.set("{\"outbounds\":[]}\n")
	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	requireRolledBack(t, env, view, abs, "{}\n", before, KernelXray)
	if calls := applier.applyCalls(); len(calls) != 3 {
		t.Errorf("ApplyLocked вызван %v, want 3 раза (базовый, неудачный, повтор после отката)", calls)
	}
}

// recoverApplier умеет принудительный рестарт: после отката упавшее ядро
// поднимается им, а не ApplyLocked (тот не запускает остановленное).
type recoverApplier struct {
	*procApplier
	forced []string
}

func (a *recoverApplier) RestartLocked(kernel string) services.ApplyResult {
	a.forced = append(a.forced, kernel)
	return services.ApplyResult{Outcome: services.ApplyRestarted, Kernel: kernel}
}

func TestApply_RollbackUsesForcedRestart(t *testing.T) {
	setRollbackTimings(t)
	binDir := t.TempDir()
	xrayBin := writeFakeKernel(t, binDir, "xray", 0, "", 0)
	procs := newFakeProcs()
	procs.set("xray", "running", 100)
	base := newProcApplier(procs, func() (string, error) {
		procs.set("xray", "stopped", 0)
		return "", nil
	})
	applier := &recoverApplier{procApplier: base}
	gen := newXrayGen("{}\n")
	env := newTestPipeline(t, pipeOpts{
		Bins: Binaries{Xray: xrayBin}, Generators: []Generator{gen},
		Applier: applier, Procs: procs.states,
	})
	gen.set("{\"outbounds\":[]}\n")
	env.setDraft(t, "note", `{"a":1}`)

	view := env.P.Run(t.Context(), ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})

	if view.Result == nil || view.Result.Code != ResultRestartFailed || !view.Result.RolledBack {
		t.Fatalf("Result = %+v, want restart_failed_rolled_back", view.Result)
	}
	if calls := base.applyCalls(); len(calls) != 1 {
		t.Errorf("ApplyLocked вызван %v, want один раз", calls)
	}
	if len(applier.forced) != 1 || applier.forced[0] != KernelXray {
		t.Errorf("RestartLocked вызван %v, want [xray]", applier.forced)
	}
	if _, err := os.Stat(filepath.Join(env.Roots.Xray, "04_outbounds.xcp-a.tail.json")); err == nil {
		t.Error("файл новой записи остался после отката")
	}
}
