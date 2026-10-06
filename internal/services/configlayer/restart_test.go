package configlayer

import (
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
