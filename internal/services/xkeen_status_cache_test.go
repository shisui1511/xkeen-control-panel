package services

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock — управляемые часы для кэша статуса.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// concurrencyProbe считает одновременные вызовы runStatus.
type concurrencyProbe struct {
	cur   atomic.Int32
	max   atomic.Int32
	calls atomic.Int32
}

func (p *concurrencyProbe) enter() {
	p.calls.Add(1)
	n := p.cur.Add(1)
	for {
		m := p.max.Load()
		if n <= m || p.max.CompareAndSwap(m, n) {
			return
		}
	}
}

func (p *concurrencyProbe) leave() { p.cur.Add(-1) }

func refreshCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func TestXKeenStatusCache_PausesFromCompletion(t *testing.T) {
	probe := &concurrencyProbe{}
	c := newXKeenStatusCacheFunc(func(context.Context) (string, error) {
		probe.enter()
		defer probe.leave()
		time.Sleep(60 * time.Millisecond)
		return "XKeen is running", nil
	}, nil, nil, 20*time.Millisecond)
	c.Start()
	time.Sleep(400 * time.Millisecond)
	c.Stop()

	// цикл опроса 60 мс + пауза 20 мс от завершения: не больше 400/80 + 1 запусков
	// (по тикеру в 20 мс их было бы около 20 наложенных)
	if got := probe.calls.Load(); got > 400/80+1 {
		t.Errorf("calls = %d, want <= %d (пауза считается от завершения опроса)", got, 400/80+1)
	}
	if got := probe.calls.Load(); got < 2 {
		t.Errorf("calls = %d, want >= 2 (цикл должен повторяться)", got)
	}
	if got := probe.max.Load(); got != 1 {
		t.Errorf("max concurrent = %d, want 1", got)
	}
}

func TestXKeenStatusCache_SingleFlight(t *testing.T) {
	probe := &concurrencyProbe{}
	c := newXKeenStatusCacheFunc(func(context.Context) (string, error) {
		probe.enter()
		defer probe.leave()
		time.Sleep(30 * time.Millisecond)
		return "XKeen is running", nil
	}, nil, nil, 20*time.Millisecond)
	c.Start()
	defer c.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				c.Invalidate()
				return
			}
			ctx, cancel := refreshCtx(t)
			defer cancel()
			c.RefreshNow(ctx)
		}(i)
	}
	wg.Wait()

	if got := probe.max.Load(); got != 1 {
		t.Errorf("max concurrent = %d, want 1", got)
	}
	if got := probe.calls.Load(); got == 0 {
		t.Error("runStatus ни разу не вызывался")
	}
}

func TestXKeenStatusCache_KeepsLastRawOnError(t *testing.T) {
	clock := newFakeClock()
	var fail atomic.Bool
	c := newXKeenStatusCacheFunc(func(context.Context) (string, error) {
		if fail.Load() {
			return "", errors.New("timeout exceeded")
		}
		return "XKeen is running", nil
	}, nil, clock.Now, time.Hour)
	c.Start()
	defer c.Stop()

	ctx, cancel := refreshCtx(t)
	defer cancel()
	first := c.RefreshNow(ctx)
	if first.Raw != "XKeen is running" || first.Stale {
		t.Fatalf("после успеха: raw=%q stale=%v", first.Raw, first.Stale)
	}

	clock.Advance(45 * time.Second)
	fail.Store(true)
	second := c.RefreshNow(ctx)
	if second.Raw != "XKeen is running" {
		t.Errorf("raw = %q, want прежний вывод", second.Raw)
	}
	if !second.Stale {
		t.Error("stale = false после ошибки опроса")
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("UpdatedAt сдвинулся: %v -> %v", first.UpdatedAt, second.UpdatedAt)
	}
	if second.LastErr == "" {
		t.Error("LastErr пуст после ошибки")
	}
	if age, ok := c.AgeSeconds(second); !ok || age != 45 {
		t.Errorf("age = %d ok=%v, want 45 true", age, ok)
	}
}

func TestXKeenStatusCache_ColdStart(t *testing.T) {
	c := newXKeenStatusCacheFunc(func(context.Context) (string, error) {
		return "", errors.New("boom")
	}, nil, nil, time.Hour)

	before := c.Snapshot()
	if !before.Stale || before.Raw != "" || !before.UpdatedAt.IsZero() {
		t.Errorf("до старта: %+v", before)
	}

	c.Start()
	defer c.Stop()
	ctx, cancel := refreshCtx(t)
	defer cancel()
	snap := c.RefreshNow(ctx)
	if !snap.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt = %v, want нулевой", snap.UpdatedAt)
	}
	if !snap.Stale {
		t.Error("stale = false на холодном старте")
	}
	if snap.Raw != "" {
		t.Errorf("raw = %q, want пусто", snap.Raw)
	}
	if _, ok := snap.AgeSeconds(time.Now()); ok {
		t.Error("AgeSeconds ok=true на холодном старте")
	}
}
