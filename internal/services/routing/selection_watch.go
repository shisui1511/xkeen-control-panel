package routing

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Параметры наблюдателя выбора.
const (
	// selectionWatchInterval — как часто наблюдатель читает PID процесса Mihomo.
	selectionWatchInterval = 10 * time.Second
	// selectionWatchRetries — сколько циклов ждать ответа Clash API (около двух
	// минут при интервале по умолчанию), прежде чем сообщить об ошибке.
	selectionWatchRetries = 12
)

// SelectionWatcher сверяет выбор участников ручных групп с запущенным Mihomo при
// каждом новом процессе: store-selected восстанавливает из cache.db прежний выбор
// процесса, а источник истины — панель (State.Live). Новый процесс — запуск,
// перезапуск, переключение ядра с Xray на Mihomo, первый замер после старта
// панели. Наблюдатель не пишет состояние слоя и не запускает конвейер: только
// читает Live и обращается к Clash API. Создаётся и останавливается только в
// cmd/xcp/main.go.
type SelectionWatcher struct {
	s        *Service
	pid      func(kernel string) int
	interval time.Duration

	// Состояние цикла трогает только горутина run.
	lastPID int
	pending bool
	retries int

	ctx     context.Context
	cancel  context.CancelFunc
	started atomic.Bool
	done    chan struct{}
}

// NewSelectionWatcher создаёт наблюдателя. pid возвращает PID запущенного
// процесса ядра или 0; interval 0 — 10 секунд.
func NewSelectionWatcher(s *Service, pid func(kernel string) int, interval time.Duration) *SelectionWatcher {
	if interval <= 0 {
		interval = selectionWatchInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &SelectionWatcher{s: s, pid: pid, interval: interval, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

// Start запускает цикл наблюдения. Повторный вызов ничего не делает.
func (w *SelectionWatcher) Start() {
	if !w.started.CompareAndSwap(false, true) {
		return
	}
	go w.run()
}

// Stop останавливает цикл и ждёт его выхода. Идемпотентен; без Start не ждёт.
func (w *SelectionWatcher) Stop() {
	w.cancel()
	if w.started.Load() {
		<-w.done
	}
}

// run — цикл: первый замер сразу, затем по тикеру.
func (w *SelectionWatcher) run() {
	defer close(w.done)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		if w.ctx.Err() != nil {
			return
		}
		w.tick()
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick — один цикл наблюдения.
func (w *SelectionWatcher) tick() {
	p := w.pid(configlayer.KernelMihomo)
	if p == 0 {
		w.lastPID, w.pending, w.retries = 0, false, 0
		w.s.reconcilePending.Store(false)
		return
	}
	if p != w.lastPID {
		w.lastPID, w.pending, w.retries = p, true, 0
	}
	if w.s.reconcilePending.Swap(false) {
		w.pending, w.retries = true, 0
	}
	if !w.pending {
		return
	}
	err := w.s.reconcileMihomo(w.ctx)
	switch {
	case err == nil:
		w.pending = false
	case errors.Is(err, errReconcileRetry):
		w.retries++
		if w.retries >= selectionWatchRetries {
			w.s.failReconcile()
			w.pending = false
		}
	case w.ctx.Err() != nil:
		// Остановка: сверка прервана, дальше цикл не идёт.
	default:
		w.pending = false
	}
}
