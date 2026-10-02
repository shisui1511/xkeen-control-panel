package services

import (
	"testing"
	"time"
)

// switchClock — фейковые часы: sleep сдвигает время, реального ожидания нет.
type switchClock struct {
	t      time.Time
	sleeps int
}

func (c *switchClock) now() time.Time { return c.t }
func (c *switchClock) sleep(d time.Duration) {
	c.t = c.t.Add(d)
	c.sleeps++
}

// newTestSwitcher собирает KernelSwitcher с фейковыми часами; states получает
// номер чтения (с нуля).
func newTestSwitcher(states func(call int) []KernelProcessState) (*KernelSwitcher, *switchClock, *int) {
	clock := &switchClock{t: time.Unix(1_700_000_000, 0)}
	calls := 0
	sw := NewKernelSwitcherFunc(
		func() []KernelProcessState {
			n := calls
			calls++
			return states(n)
		},
		clock.now, clock.sleep, 15*time.Second, 500*time.Millisecond,
	)
	return sw, clock, &calls
}

func running(names ...string) []KernelProcessState {
	out := []KernelProcessState{{Name: "xray", Status: "stopped"}, {Name: "mihomo", Status: "stopped"}}
	for i := range out {
		for _, n := range names {
			if out[i].Name == n {
				out[i].Status = "running"
			}
		}
	}
	return out
}

func TestKernelSwitcher_Switched(t *testing.T) {
	sw, _, calls := newTestSwitcher(func(call int) []KernelProcessState {
		switch {
		case call < 2:
			return running("xray")
		default:
			return running("mihomo")
		}
	})
	res := sw.Await("xray", "mihomo")
	if res.Outcome != SwitchSwitched {
		t.Fatalf("outcome = %q, want switched", res.Outcome)
	}
	if res.OldRunning || !res.NewRunning {
		t.Errorf("old_running=%v new_running=%v, want false/true", res.OldRunning, res.NewRunning)
	}
	if res.Old != "xray" || res.New != "mihomo" {
		t.Errorf("old/new = %q/%q, want xray/mihomo", res.Old, res.New)
	}
	if *calls != 3 {
		t.Errorf("state reads = %d, want 3 (подтверждено сразу после смены)", *calls)
	}
}

func TestKernelSwitcher_OldStillRunning(t *testing.T) {
	sw, clock, _ := newTestSwitcher(func(int) []KernelProcessState { return running("xray", "mihomo") })
	res := sw.Await("xray", "mihomo")
	if res.Outcome != SwitchOldStillRunning {
		t.Fatalf("outcome = %q, want old_still_running", res.Outcome)
	}
	if !res.OldRunning || !res.NewRunning {
		t.Errorf("old_running=%v new_running=%v, want true/true", res.OldRunning, res.NewRunning)
	}
	if got := clock.t.Sub(time.Unix(1_700_000_000, 0)); got != 15*time.Second {
		t.Errorf("ожидание = %v, want ровно дедлайн 15s", got)
	}
}

func TestKernelSwitcher_NewNotStarted(t *testing.T) {
	// Старое ядро остановилось, новое так и не поднялось.
	sw, _, _ := newTestSwitcher(func(call int) []KernelProcessState {
		if call == 0 {
			return running("xray")
		}
		return running()
	})
	res := sw.Await("xray", "mihomo")
	if res.Outcome != SwitchNewNotStarted {
		t.Fatalf("outcome = %q, want new_not_started", res.Outcome)
	}
	if res.NewRunning || res.OldRunning {
		t.Errorf("old_running=%v new_running=%v, want false/false (по последнему чтению)", res.OldRunning, res.NewRunning)
	}
}

func TestKernelSwitcher_NewNotStartedOldAlive(t *testing.T) {
	// Старое живо, новое не поднялось: old_running отражает последнее чтение.
	sw, _, _ := newTestSwitcher(func(int) []KernelProcessState { return running("xray") })
	res := sw.Await("xray", "mihomo")
	if res.Outcome != SwitchNewNotStarted || !res.OldRunning || res.NewRunning {
		t.Errorf("result = %+v, want new_not_started, old_running=true", res)
	}
}

func TestKernelSwitcher_FromNoneOrSame(t *testing.T) {
	for _, old := range []string{"none", "", "mihomo"} {
		// Целевое ядро mihomo работает рядом с живым xray: старое для none/same
		// не отслеживается, успех только по «новое работает».
		sw, _, _ := newTestSwitcher(func(int) []KernelProcessState { return running("xray", "mihomo") })
		res := sw.Await(old, "mihomo")
		if res.Outcome != SwitchSwitched {
			t.Errorf("old=%q: outcome = %q, want switched", old, res.Outcome)
		}
		if res.OldRunning {
			t.Errorf("old=%q: old_running = true, want false", old)
		}
	}
}

// TestKernelSwitcher_FakeClockBounded: дедлайн 15 с с шагом 500 мс — не больше 31
// чтения состояний, реального ожидания нет.
func TestKernelSwitcher_FakeClockBounded(t *testing.T) {
	sw, clock, calls := newTestSwitcher(func(int) []KernelProcessState { return running() })
	began := time.Now()
	res := sw.Await("xray", "mihomo")
	if res.Outcome != SwitchNewNotStarted {
		t.Fatalf("outcome = %q, want new_not_started", res.Outcome)
	}
	if *calls != 31 {
		t.Errorf("state reads = %d, want 31", *calls)
	}
	if clock.sleeps != 30 {
		t.Errorf("sleeps = %d, want 30", clock.sleeps)
	}
	if time.Since(began) > 2*time.Second {
		t.Errorf("реальное ожидание %v: часы должны быть фейковыми", time.Since(began))
	}
}
