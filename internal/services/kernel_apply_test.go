package services

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// applyFake — управляемые статусы ядер и счётчик вызовов рестарта.
type applyFake struct {
	mu         sync.Mutex
	statuses   map[string]string
	configured string
	restarts   int32
}

func (f *applyFake) applier(restart func() (string, error)) *KernelApplier {
	if restart == nil {
		restart = func() (string, error) {
			atomic.AddInt32(&f.restarts, 1)
			return "ok", nil
		}
	}
	return NewKernelApplierFunc(
		func(name string) string {
			f.mu.Lock()
			defer f.mu.Unlock()
			if s, ok := f.statuses[name]; ok {
				return s
			}
			return "not_installed"
		},
		func() string { return f.configured },
		restart,
	)
}

func (f *applyFake) setStatus(name, status string) {
	f.mu.Lock()
	f.statuses[name] = status
	f.mu.Unlock()
}

func TestKernelMayRun(t *testing.T) {
	cases := map[string]bool{
		"running":        true,
		"unknown":        true,
		"not_accessible": true,
		"stopped":        false,
		"not_installed":  false,
		"":               true,
	}
	for status, want := range cases {
		if got := KernelMayRun(status); got != want {
			t.Errorf("KernelMayRun(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestKernelApplier_Outcomes(t *testing.T) {
	cases := []struct {
		name       string
		xray       string
		mihomo     string
		target     string
		wantOut    ApplyOutcome
		wantKernel string
		wantActive bool
		wantCalls  int32
	}{
		{"xray running", "running", "stopped", "xray", ApplyRestarted, "xray", true, 1},
		{"xray unknown", "unknown", "stopped", "xray", ApplyRestarted, "xray", true, 1},
		{"xray not_accessible", "not_accessible", "stopped", "xray", ApplyRestarted, "xray", true, 1},
		{"xray stopped", "stopped", "stopped", "xray", ApplySavedKernelStopped, "xray", false, 0},
		{"xray not_installed", "not_installed", "stopped", "xray", ApplySavedKernelStopped, "xray", false, 0},
		{"mihomo target, xray running", "running", "running", "mihomo", ApplySavedKernelInactive, "mihomo", true, 0},
		{"mihomo target, xray stopped", "stopped", "running", "mihomo", ApplySavedKernelInactive, "mihomo", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &applyFake{configured: "xray", statuses: map[string]string{"xray": tc.xray, "mihomo": tc.mihomo}}
			res := f.applier(nil).Apply(tc.target)
			if res.Outcome != tc.wantOut {
				t.Errorf("outcome = %q, want %q", res.Outcome, tc.wantOut)
			}
			if res.Kernel != tc.wantKernel {
				t.Errorf("kernel = %q, want %q", res.Kernel, tc.wantKernel)
			}
			if res.ActiveKernel != "xray" {
				t.Errorf("active_kernel = %q, want xray", res.ActiveKernel)
			}
			if res.ActiveRunning != tc.wantActive {
				t.Errorf("active_running = %v, want %v", res.ActiveRunning, tc.wantActive)
			}
			if got := atomic.LoadInt32(&f.restarts); got != tc.wantCalls {
				t.Errorf("restart calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

// Идемпотентность: повторное применение даёт тот же исход и не запускает
// остановленное ядро.
func TestKernelApplier_Idempotent(t *testing.T) {
	f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running"}}
	a := f.applier(nil)
	for i := 0; i < 2; i++ {
		if res := a.Apply("xray"); res.Outcome != ApplyRestarted {
			t.Fatalf("running apply #%d: outcome = %q", i, res.Outcome)
		}
	}
	if got := atomic.LoadInt32(&f.restarts); got != 2 {
		t.Errorf("running: restart calls = %d, want 2", got)
	}

	f2 := &applyFake{configured: "xray", statuses: map[string]string{"xray": "stopped"}}
	a2 := f2.applier(nil)
	for i := 0; i < 2; i++ {
		if res := a2.Apply("xray"); res.Outcome != ApplySavedKernelStopped {
			t.Fatalf("stopped apply #%d: outcome = %q", i, res.Outcome)
		}
	}
	if got := atomic.LoadInt32(&f2.restarts); got != 0 {
		t.Errorf("stopped: restart calls = %d, want 0", got)
	}
}

func TestKernelApplier_ActiveResolution(t *testing.T) {
	t.Run("configured wins", func(t *testing.T) {
		f := &applyFake{configured: "mihomo", statuses: map[string]string{"xray": "running", "mihomo": "stopped"}}
		res := f.applier(nil).Apply(ApplyTargetActive)
		if res.Kernel != "mihomo" || res.Outcome != ApplySavedKernelStopped {
			t.Errorf("got %+v, want mihomo saved_kernel_stopped", res)
		}
		if f.restarts != 0 {
			t.Errorf("restart calls = %d, want 0", f.restarts)
		}
	})
	t.Run("no configured, mihomo running", func(t *testing.T) {
		f := &applyFake{statuses: map[string]string{"xray": "stopped", "mihomo": "running"}}
		res := f.applier(nil).Apply(ApplyTargetActive)
		if res.Kernel != "mihomo" || res.ActiveKernel != "mihomo" || res.Outcome != ApplyRestarted {
			t.Errorf("got %+v, want mihomo restarted", res)
		}
	})
	t.Run("nothing known, target active", func(t *testing.T) {
		f := &applyFake{statuses: map[string]string{"xray": "stopped", "mihomo": "stopped"}}
		res := f.applier(nil).Apply(ApplyTargetActive)
		if res.Outcome != ApplySavedKernelStopped || res.Kernel != "" {
			t.Errorf("got %+v, want saved_kernel_stopped without kernel", res)
		}
		if f.restarts != 0 {
			t.Errorf("restart calls = %d, want 0", f.restarts)
		}
	})
	t.Run("nothing known, concrete target decided by its status", func(t *testing.T) {
		f := &applyFake{statuses: map[string]string{"xray": "stopped", "mihomo": "stopped"}}
		res := f.applier(nil).Apply("xray")
		if res.Outcome != ApplySavedKernelStopped || res.Kernel != "xray" {
			t.Errorf("got %+v, want xray saved_kernel_stopped", res)
		}
	})
	t.Run("no arguments means active", func(t *testing.T) {
		f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running"}}
		if res := f.applier(nil).Apply(); res.Outcome != ApplyRestarted || res.Kernel != "xray" {
			t.Errorf("got %+v, want xray restarted", res)
		}
	})
}

// Неизвестные цели (WR-01): не паника и не рестарт при любом состоянии ядер.
func TestKernelApplier_UnknownTargets(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		xray       string
		targets    []string
	}{
		{"unknown, active running", "xray", "running", []string{"foo"}},
		{"empty name, active running", "xray", "running", []string{""}},
		{"unknown, active stopped", "xray", "stopped", []string{"foo"}},
		{"unknown, no active kernel", "", "stopped", []string{"foo"}},
		{"several unknown", "xray", "running", []string{"foo", "", "bar"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &applyFake{configured: tc.configured, statuses: map[string]string{"xray": tc.xray, "mihomo": "stopped"}}
			a := f.applier(nil)
			res := a.Apply(tc.targets...)
			if res.Outcome != ApplySavedKernelInactive {
				t.Errorf("outcome = %q, want %q", res.Outcome, ApplySavedKernelInactive)
			}
			if res.Kernel != "" {
				t.Errorf("kernel = %q, want empty", res.Kernel)
			}
			if got := a.Preview(tc.targets...).Outcome; got != ApplySavedKernelInactive {
				t.Errorf("Preview outcome = %q, want %q", got, ApplySavedKernelInactive)
			}
			if got := atomic.LoadInt32(&f.restarts); got != 0 {
				t.Errorf("restart calls = %d, want 0", got)
			}
		})
	}

	t.Run("unknown next to a valid target still applies the valid one", func(t *testing.T) {
		f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running"}}
		res := f.applier(nil).Apply("foo", "xray")
		if res.Outcome != ApplyRestarted || res.Kernel != "xray" {
			t.Errorf("got %+v, want xray restarted", res)
		}
	})
}

func TestKernelApplier_MultiTarget(t *testing.T) {
	f := &applyFake{configured: "mihomo", statuses: map[string]string{"xray": "stopped", "mihomo": "running"}}
	res := f.applier(nil).Apply("xray", "mihomo")
	if res.Outcome != ApplyRestarted || res.Kernel != "mihomo" {
		t.Errorf("got %+v, want mihomo restarted", res)
	}
	if got := atomic.LoadInt32(&f.restarts); got != 1 {
		t.Errorf("restart calls = %d, want 1 (одно применение — один рестарт)", got)
	}
}

func TestKernelApplier_PreviewMatchesApply(t *testing.T) {
	f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running", "mihomo": "running"}}
	a := f.applier(nil)
	if !a.WillRestart("xray") {
		t.Error("WillRestart(xray) = false, want true")
	}
	if a.WillRestart("mihomo") {
		t.Error("WillRestart(mihomo) = true, want false: mihomo не активно")
	}
	if got := atomic.LoadInt32(&f.restarts); got != 0 {
		t.Errorf("Preview запустил рестарт: %d", got)
	}
	f.setStatus("xray", "stopped")
	if a.WillRestart("xray") {
		t.Error("WillRestart(xray) = true при остановленном ядре")
	}
}

func TestKernelApplier_RestartFailed(t *testing.T) {
	f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running"}}
	long := strings.Repeat("ж", 400)
	a := f.applier(func() (string, error) {
		return "\x1b[31mstarting\x1b[0m\n\x1b[1;31mFAILED to start xray\x1b[0m\n" + long + "\n", errors.New("exit status 1")
	})
	res := a.Apply("xray")
	if res.Outcome != ApplyRestartFailed {
		t.Fatalf("outcome = %q, want restart_failed", res.Outcome)
	}
	if strings.Contains(res.Error, "\x1b") {
		t.Errorf("error содержит ANSI: %q", res.Error)
	}
	if len(res.Error) > applyErrorMaxBytes {
		t.Errorf("len(error) = %d, want <= %d", len(res.Error), applyErrorMaxBytes)
	}
	if res.Error == "" {
		t.Error("error пуст")
	}

	short := f.applier(func() (string, error) {
		return "\x1b[31mFAILED to start xray\x1b[0m\n", errors.New("exit status 1")
	}).Apply("xray")
	if short.Error != "FAILED to start xray" {
		t.Errorf("error = %q, want последние строки вывода без ANSI", short.Error)
	}

	empty := f.applier(func() (string, error) { return "", errors.New("exit status 7") }).Apply("xray")
	if empty.Error != "exit status 7" {
		t.Errorf("empty output: error = %q, want err.Error()", empty.Error)
	}
}

func TestKernelApplier_ConcurrentApply(t *testing.T) {
	var running, maxRunning, calls int32
	f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running"}}
	a := f.applier(func() (string, error) {
		cur := atomic.AddInt32(&running, 1)
		for {
			m := atomic.LoadInt32(&maxRunning)
			if cur <= m || atomic.CompareAndSwapInt32(&maxRunning, m, cur) {
				break
			}
		}
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return "ok", nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if res := a.Apply("xray"); res.Outcome != ApplyRestarted {
				t.Errorf("outcome = %q, want restarted", res.Outcome)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&maxRunning); got != 1 {
		t.Errorf("одновременных рестартов = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&calls); got != 10 {
		t.Errorf("restart calls = %d, want 10", got)
	}
}

// Решение принимается по свежему статусу после захвата мьютекса: ядро,
// остановленное во время ожидания, не запускается вторым применением.
func TestKernelApplier_FreshStatusAfterLock(t *testing.T) {
	f := &applyFake{configured: "xray", statuses: map[string]string{"xray": "running"}}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	a := f.applier(func() (string, error) {
		atomic.AddInt32(&f.restarts, 1)
		once.Do(func() { close(started) })
		<-release
		return "ok", nil
	})

	first := make(chan ApplyResult, 1)
	go func() { first <- a.Apply("xray") }()
	<-started

	second := make(chan ApplyResult, 1)
	go func() { second <- a.Apply("xray") }()
	time.Sleep(20 * time.Millisecond)
	f.setStatus("xray", "stopped")
	close(release)

	if res := <-first; res.Outcome != ApplyRestarted {
		t.Errorf("first outcome = %q, want restarted", res.Outcome)
	}
	if res := <-second; res.Outcome != ApplySavedKernelStopped {
		t.Errorf("second outcome = %q, want saved_kernel_stopped", res.Outcome)
	}
	if got := atomic.LoadInt32(&f.restarts); got != 1 {
		t.Errorf("restart calls = %d, want 1", got)
	}
}
