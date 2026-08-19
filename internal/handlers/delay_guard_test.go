package handlers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDelayGuard_Sequential(t *testing.T) {
	guard := NewDelayGuard(10, 2*time.Second)

	var active int32
	var maxActive int32
	var wg sync.WaitGroup

	numReqs := 10
	wg.Add(numReqs)

	for i := 0; i < numReqs; i++ {
		go func() {
			defer wg.Done()
			release, err := guard.Acquire(context.Background())
			if err != nil {
				t.Errorf("unexpected acquire error: %v", err)
				return
			}
			curr := atomic.AddInt32(&active, 1)
			if curr > atomic.LoadInt32(&maxActive) {
				atomic.StoreInt32(&maxActive, curr)
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			release()
		}()
	}

	wg.Wait()

	if maxActive > 1 {
		t.Fatalf("expected max active concurrency <= 1, got %d", maxActive)
	}
	if guard.QueueLength() != 0 {
		t.Fatalf("expected queue length 0, got %d", guard.QueueLength())
	}
}

func TestDelayGuard_QueueLimit(t *testing.T) {
	// guard with capacity 1 and maxQueue 2
	guard := NewDelayGuard(2, 2*time.Second)

	// Hold the slot
	release, err := guard.Acquire(context.Background())
	if err != nil {
		t.Fatalf("failed initial acquire: %v", err)
	}
	defer release()

	var wg sync.WaitGroup
	var queueFullCount int32

	// Try to start 4 goroutines (2 should fit in queue, 2 should get ErrQueueFull)
	wg.Add(4)
	for i := 0; i < 4; i++ {
		go func() {
			defer wg.Done()
			_, err := guard.Acquire(context.Background())
			if err == ErrQueueFull {
				atomic.AddInt32(&queueFullCount, 1)
			}
		}()
	}

	// Give goroutines time to hit the guard
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&queueFullCount) < 2 {
		t.Errorf("expected at least 2 queue full errors, got %d", queueFullCount)
	}
}

func TestDelayGuard_Timeout(t *testing.T) {
	guard := NewDelayGuard(5, 50*time.Millisecond)

	// Hold the slot
	release, err := guard.Acquire(context.Background())
	if err != nil {
		t.Fatalf("failed initial acquire: %v", err)
	}
	defer release()

	// Next acquire should time out after 50ms
	start := time.Now()
	_, err = guard.Acquire(context.Background())
	elapsed := time.Since(start)

	if err != ErrWaitTimeout {
		t.Fatalf("expected ErrWaitTimeout, got %v", err)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("timeout returned too quickly: %v", elapsed)
	}
	if guard.QueueLength() != 0 {
		t.Fatalf("expected queue length 0 after timeout, got %d", guard.QueueLength())
	}
}

func TestDelayGuard_ContextCancel(t *testing.T) {
	guard := NewDelayGuard(5, 2*time.Second)

	// Hold the slot
	release, err := guard.Acquire(context.Background())
	if err != nil {
		t.Fatalf("failed initial acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	_, err = guard.Acquire(ctx)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if guard.QueueLength() != 0 {
		t.Fatalf("expected queue length 0 after cancel, got %d", guard.QueueLength())
	}
}
