package services

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// scriptOutputLimit ограничивает сохраняемый вывод скрипта: остальное
// дочитывается и отбрасывается.
const scriptOutputLimit = 1 << 20

// scriptOutputGrace — сколько ждать остаток вывода после выхода скрипта,
// прежде чем снять снимок.
const scriptOutputGrace = 2 * time.Second

// scriptOutput собирает общий stdout/stderr скрипта через собственный пайп.
// Скрипты xkeen запускают ядро в фоне, и оно может унаследовать вывод:
// закрыть читающий конец нельзя (Go-бинарник ядра умрёт от SIGPIPE при
// записи в fd 1/2), держать всё в памяти тоже. Поэтому после снимка пайп
// дочитывается в io.Discard, пока его не закроет последний потомок.
type scriptOutput struct {
	mu     sync.Mutex
	buf    []byte
	sealed bool
	done   chan struct{}
	w      *os.File
}

func (o *scriptOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.sealed {
		if room := scriptOutputLimit - len(o.buf); room > 0 {
			o.buf = append(o.buf, p[:min(room, len(p))]...)
		}
	}
	return len(p), nil
}

// attachScriptOutput подключает пайп к stdout/stderr cmd. Вызывать до Start.
func attachScriptOutput(cmd *exec.Cmd) (*scriptOutput, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	o := &scriptOutput{done: make(chan struct{}), w: w}
	cmd.Stdout = w
	cmd.Stderr = w
	go func() {
		_, _ = io.Copy(o, r)
		_ = r.Close()
		close(o.done)
	}()
	return o, nil
}

// started закрывает пишущий конец в родителе: иначе EOF не наступит никогда.
// Вызывать сразу после Start (и при его ошибке).
func (o *scriptOutput) started() {
	_ = o.w.Close()
}

// snapshot ждёт EOF не дольше grace после выхода скрипта и возвращает
// собранный вывод; всё, что придёт позже, отбрасывается.
func (o *scriptOutput) snapshot(grace time.Duration) string {
	select {
	case <-o.done:
	case <-time.After(grace):
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sealed = true
	return string(o.buf)
}
