package services

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

// stopFixture — фейковый procDir с бинарниками ядер и подменой сигнала.
type stopFixture struct {
	t       *testing.T
	proc    string
	mihomo  string
	xray    string
	svc     *KernelService
	mu      sync.Mutex
	signals []sentSignal
}

type sentSignal struct {
	pid int
	sig syscall.Signal
}

func newStopFixture(t *testing.T) *stopFixture {
	t.Helper()
	f := &stopFixture{t: t, proc: t.TempDir()}
	bins := t.TempDir()
	f.mihomo = filepath.Join(bins, "mihomo")
	f.xray = filepath.Join(bins, "xray")
	for _, b := range []string{f.mihomo, f.xray} {
		if err := os.WriteFile(b, []byte("fake binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	origProc, origWait, origInterval, origSignal := procDir, kernelStopWait, kernelStopInterval, signalProcess
	procDir = f.proc
	kernelStopWait = 2 * time.Second
	kernelStopInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		procDir, kernelStopWait, kernelStopInterval, signalProcess = origProc, origWait, origInterval, origSignal
	})

	f.svc = NewKernelService(t.TempDir())
	f.svc.kernels["mihomo"].BinaryPath = f.mihomo
	f.svc.kernels["xray"].BinaryPath = f.xray

	// По умолчанию сигнал завершает процесс: каталог PID исчезает из /proc.
	signalProcess = func(pid int, sig syscall.Signal) error {
		f.record(pid, sig)
		return os.RemoveAll(filepath.Join(f.proc, strconv.Itoa(pid)))
	}
	return f
}

func (f *stopFixture) record(pid int, sig syscall.Signal) {
	f.mu.Lock()
	f.signals = append(f.signals, sentSignal{pid, sig})
	f.mu.Unlock()
}

func (f *stopFixture) sent() []sentSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentSignal(nil), f.signals...)
}

// addProc создаёт /proc/<pid> с exe → bin и cmdline.
func (f *stopFixture) addProc(pid int, bin string, args ...string) {
	f.t.Helper()
	dir := filepath.Join(f.proc, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	cmdline := bin + "\x00"
	for _, a := range args {
		cmdline += a + "\x00"
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Symlink(bin, filepath.Join(dir, "exe")); err != nil {
		f.t.Fatal(err)
	}
}

func TestStopKernelProcess_SignalsVerifiedPID(t *testing.T) {
	f := newStopFixture(t)
	f.addProc(2001, f.mihomo, "-d", "/opt/etc/mihomo")
	f.addProc(2002, f.xray, "-config", "/opt/etc/xray.json") // чужое ядро

	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil {
		t.Fatalf("StopKernelProcess: %v", err)
	}
	if res.Outcome != KernelStopStopped || res.Kernel != "mihomo" {
		t.Errorf("result = %+v, want mihomo/stopped", res)
	}
	got := f.sent()
	if len(got) != 1 || got[0].pid != 2001 || got[0].sig != syscall.SIGTERM {
		t.Errorf("signals = %+v, want ровно один SIGTERM в 2001", got)
	}
	if _, statErr := os.Stat(filepath.Join(f.proc, "2002")); statErr != nil {
		t.Error("процесс чужого ядра затронут")
	}
}

func TestStopKernelProcess_ExeChangedNoSignal(t *testing.T) {
	f := newStopFixture(t)
	// PID 2001 числится ядром mihomo в источнике состояний, но его exe уже другой
	// бинарник (PID переиспользован): сигнал не отправляется.
	f.addProc(2001, f.xray)
	f.svc.SetProcessStatesSource(func() []KernelProcessState {
		return []KernelProcessState{{Name: "mihomo", Status: "running", PID: 2001}}
	})

	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil {
		t.Fatalf("StopKernelProcess: %v", err)
	}
	if res.Outcome != KernelStopStopped {
		t.Errorf("outcome = %q, want stopped (процесса ядра больше нет)", res.Outcome)
	}
	if got := f.sent(); len(got) != 0 {
		t.Errorf("signals = %+v, want пусто: exe не бинарник mihomo", got)
	}
}

func TestStopKernelProcess_StillRunningAfterWait(t *testing.T) {
	f := newStopFixture(t)
	f.addProc(2001, f.mihomo)
	signalProcess = func(pid int, sig syscall.Signal) error {
		f.record(pid, sig) // процесс игнорирует сигнал и остаётся в /proc
		return nil
	}
	kernelStopWait = 50 * time.Millisecond

	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil {
		t.Fatalf("StopKernelProcess: %v", err)
	}
	if res.Outcome != KernelStopStillRunning {
		t.Errorf("outcome = %q, want still_running", res.Outcome)
	}
	got := f.sent()
	if len(got) != 1 || got[0].sig != syscall.SIGTERM {
		t.Errorf("signals = %+v, want ровно один SIGTERM и никакого другого сигнала", got)
	}
}

func TestStopKernelProcess_HelperIgnored(t *testing.T) {
	f := newStopFixture(t)
	f.addProc(2003, f.mihomo, "convert-ruleset", "domain", "mrs", "a.yaml", "a.mrs")

	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil {
		t.Fatalf("StopKernelProcess: %v", err)
	}
	if res.Outcome != KernelStopNotRunning {
		t.Errorf("outcome = %q, want not_running (helper не ядро)", res.Outcome)
	}
	if got := f.sent(); len(got) != 0 {
		t.Errorf("signals = %+v, helper-процесс не должен получать сигнал", got)
	}
}

func TestStopKernelProcess_NotRunning(t *testing.T) {
	f := newStopFixture(t)
	f.addProc(2002, f.xray)

	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil {
		t.Fatalf("StopKernelProcess: %v", err)
	}
	if res.Outcome != KernelStopNotRunning {
		t.Errorf("outcome = %q, want not_running", res.Outcome)
	}
	if got := f.sent(); len(got) != 0 {
		t.Errorf("signals = %+v, want пусто", got)
	}
}

func TestStopKernelProcess_InvalidName(t *testing.T) {
	f := newStopFixture(t)
	f.addProc(2001, f.mihomo)
	for _, name := range []string{"../etc", "../etc/passwd", "v2ray", "", "Xray", "xray "} {
		if _, err := f.svc.StopKernelProcess(name); err == nil {
			t.Errorf("name=%q: ошибки нет, ожидался отказ белого списка", name)
		}
	}
	if got := f.sent(); len(got) != 0 {
		t.Errorf("signals = %+v, want пусто", got)
	}
}

// TestStopKernelProcess_RefusesLowPID: PID ≤ 1 никогда не получает сигнал
// (kill(0)/kill(-1) адресуются группам процессов).
func TestStopKernelProcess_RefusesLowPID(t *testing.T) {
	f := newStopFixture(t)
	f.svc.SetProcessStatesSource(func() []KernelProcessState {
		return []KernelProcessState{{Name: "mihomo", Status: "running", PID: 1}, {Name: "mihomo", Status: "running", PID: -1}}
	})
	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil {
		t.Fatalf("StopKernelProcess: %v", err)
	}
	if res.Outcome != KernelStopNotRunning {
		t.Errorf("outcome = %q, want not_running", res.Outcome)
	}
	if got := f.sent(); len(got) != 0 {
		t.Errorf("signals = %+v, want пусто", got)
	}
}

// TestStopKernelProcess_SignalErrors: ESRCH — не ошибка, иное (EPERM) — ошибка.
func TestStopKernelProcess_SignalErrors(t *testing.T) {
	f := newStopFixture(t)
	f.addProc(2001, f.mihomo)
	signalProcess = func(pid int, sig syscall.Signal) error {
		_ = os.RemoveAll(filepath.Join(f.proc, strconv.Itoa(pid)))
		return syscall.ESRCH
	}
	res, err := f.svc.StopKernelProcess("mihomo")
	if err != nil || res.Outcome != KernelStopStopped {
		t.Errorf("ESRCH: res=%+v err=%v, want stopped без ошибки", res, err)
	}

	f.addProc(2001, f.mihomo)
	signalProcess = func(int, syscall.Signal) error { return syscall.EPERM }
	if _, err := f.svc.StopKernelProcess("mihomo"); err == nil {
		t.Error("EPERM: ошибки нет, ожидалась ошибка сигнала")
	}
}
