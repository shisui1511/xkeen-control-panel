package utils

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// AsyncWriterLimit — сколько байт строк журнала ждут записи на носитель, прежде
// чем новые начнут отбрасываться: около тысячи типичных строк.
const AsyncWriterLimit = 256 << 10

// asyncDroppedFormat — строка журнала об отброшенных строках (с меткой времени в
// формате пакета log, чтобы её понимал DeduplicatingWriter).
const asyncDroppedFormat = "%s [log] %d line(s) dropped: log queue full, storage did not accept writes\n"

// asyncEntry — строка журнала (flushed == nil) или метка: flushed закрывается,
// когда всё, что поставлено раньше, записано; closeAfter — после записи закрыть
// нижний writer.
type asyncEntry struct {
	line       []byte
	flushed    chan struct{}
	closeAfter bool
}

// AsyncWriter пишет журнал без ожидания носителя: Write копирует строку в
// ограниченную очередь и сразу возвращается, запись в нижний writer идёт в своей
// горутине. Журнал панели лежит на USB-флешке с ext4 (data=ordered): под чужой
// записью write(2) ждёт фиксации журнала ФС секундами, и синхронная запись строки
// держала обработчик запроса (ответ уходит после строки журнала доступа) и поток
// остановки по сигналу. Очередь ограничена AsyncWriterLimit байт; при
// переполнении строка отбрасывается и считается, итог пишется строкой журнала,
// когда носитель снова принимает запись.
type AsyncWriter struct {
	mu      sync.Mutex
	queue   []asyncEntry
	queued  int
	limit   int
	dropped int
	closed  bool
	wake    chan struct{}
	w       io.Writer
	now     func() time.Time
}

// NewAsyncWriter запускает запись в w. Если w реализует io.Closer, Close закрывает
// его в той же горутине после записи очереди.
func NewAsyncWriter(w io.Writer, limit int) *AsyncWriter {
	if limit <= 0 {
		limit = AsyncWriterLimit
	}
	a := &AsyncWriter{
		limit: limit,
		wake:  make(chan struct{}, 1),
		w:     w,
		now:   time.Now,
	}
	go a.loop()
	return a
}

// Write ставит копию p в очередь (пакет log переиспользует буфер строки) и не
// ждёт записи. Ошибок не возвращает: строка, не вошедшая в очередь, считается
// отброшенной.
func (a *AsyncWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	a.mu.Lock()
	if a.closed || a.queued+len(p) > a.limit {
		a.dropped++
		a.mu.Unlock()
		return len(p), nil
	}
	a.queue = append(a.queue, asyncEntry{line: append([]byte(nil), p...)})
	a.queued += len(p)
	a.mu.Unlock()
	a.signal()
	return len(p), nil
}

// Flush ждёт, пока запишется всё, что поставлено в очередь до вызова, но не
// дольше timeout. true — записано. Сам на носитель не пишет.
func (a *AsyncWriter) Flush(timeout time.Duration) bool {
	ch, ok := a.mark(false)
	if !ok {
		return false
	}
	return asyncWait(ch, timeout)
}

// Close перестаёт принимать строки, дописывает очередь и закрывает нижний writer
// в горутине записи; ждёт этого не дольше timeout. Повторный вызов ничего не
// ждёт.
func (a *AsyncWriter) Close(timeout time.Duration) error {
	ch, ok := a.mark(true)
	if !ok {
		return nil
	}
	if !asyncWait(ch, timeout) {
		return fmt.Errorf("журнал не дописан за %s: носитель не принимает запись", timeout)
	}
	return nil
}

// mark ставит метку в очередь; false — writer уже закрыт.
func (a *AsyncWriter) mark(closeAfter bool) (chan struct{}, bool) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, false
	}
	ch := make(chan struct{})
	a.queue = append(a.queue, asyncEntry{flushed: ch, closeAfter: closeAfter})
	if closeAfter {
		a.closed = true
	}
	a.mu.Unlock()
	a.signal()
	return ch, true
}

func (a *AsyncWriter) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func asyncWait(ch <-chan struct{}, timeout time.Duration) bool {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-t.C:
		return false
	}
}

func (a *AsyncWriter) loop() {
	for {
		a.mu.Lock()
		for len(a.queue) == 0 {
			a.mu.Unlock()
			<-a.wake
			a.mu.Lock()
		}
		batch := a.queue
		a.queue = nil
		a.queued = 0
		dropped := a.dropped
		a.dropped = 0
		a.mu.Unlock()

		for _, e := range batch {
			if e.flushed == nil {
				_, _ = a.w.Write(e.line)
				continue
			}
			if dropped > 0 {
				a.writeDropped(dropped)
				dropped = 0
			}
			if e.closeAfter {
				if c, ok := a.w.(io.Closer); ok {
					_ = c.Close()
				}
				close(e.flushed)
				return
			}
			close(e.flushed)
		}
		if dropped > 0 {
			a.writeDropped(dropped)
		}
	}
}

func (a *AsyncWriter) writeDropped(n int) {
	_, _ = fmt.Fprintf(a.w, asyncDroppedFormat, a.now().Format("2006/01/02 15:04:05"), n)
}
