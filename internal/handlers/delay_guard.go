package handlers

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrQueueFull возвращается, когда очередь ожидания delay-запросов переполнена.
	ErrQueueFull = errors.New("delay queue full")
	// ErrWaitTimeout возвращается при превышении времени ожидания слота в очереди.
	ErrWaitTimeout = errors.New("delay queue wait timeout")
)

// DelayGuard ограничивает параллельные delay и healthcheck запросы к ядру Mihomo,
// гарантируя строгую последовательность (емкость семафора 1) и защищая CPU роутера от перегрузки.
type DelayGuard struct {
	sem         chan struct{}
	queueMu     sync.Mutex
	queueLen    int
	maxQueue    int
	waitTimeout time.Duration
}

// NewDelayGuard создает новый экземпляр DelayGuard с ограничением очереди и таймаутом ожидания.
func NewDelayGuard(maxQueue int, waitTimeout time.Duration) *DelayGuard {
	if maxQueue <= 0 {
		maxQueue = 32
	}
	if waitTimeout <= 0 {
		waitTimeout = 15 * time.Second
	}
	return &DelayGuard{
		sem:         make(chan struct{}, 1),
		maxQueue:    maxQueue,
		waitTimeout: waitTimeout,
	}
}

// Acquire пытается получить слот для выполнения delay-запроса или ставит запрос в очередь ожидания.
// Возвращает функцию release() для освобождения слота при успехе.
func (g *DelayGuard) Acquire(ctx context.Context) (func(), error) {
	g.queueMu.Lock()
	if g.queueLen >= g.maxQueue {
		g.queueMu.Unlock()
		return nil, ErrQueueFull
	}
	g.queueLen++
	g.queueMu.Unlock()

	timer := time.NewTimer(g.waitTimeout)
	defer timer.Stop()

	select {
	case g.sem <- struct{}{}:
		g.queueMu.Lock()
		g.queueLen--
		g.queueMu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				<-g.sem
			})
		}, nil

	case <-timer.C:
		g.queueMu.Lock()
		g.queueLen--
		g.queueMu.Unlock()
		return nil, ErrWaitTimeout

	case <-ctx.Done():
		g.queueMu.Lock()
		g.queueLen--
		g.queueMu.Unlock()
		return nil, ctx.Err()
	}
}

// QueueLength возвращает текущую длину очереди ожидания (для мониторинга и тестов).
func (g *DelayGuard) QueueLength() int {
	g.queueMu.Lock()
	defer g.queueMu.Unlock()
	return g.queueLen
}
