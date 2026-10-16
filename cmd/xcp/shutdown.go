package main

import (
	"log"
	"sync/atomic"
	"time"
)

// Сроки остановки по SIGTERM/SIGINT. Init-скрипт Entware (rc.func) ждёт выхода
// процесса около 10 с; не дождался — запуск видит живой процесс, отвечает
// «already running», и после выхода старого процесса панель остаётся лежать.
// HTTP-сервер ждёт идущие запросы не дольше shutdownHTTPTimeout, весь выход — не
// дольше shutdownDeadline: застрявший шаг (дочерний процесс, сеть, замок)
// бросается. Поток внутри fsync срок не прервёт — процесс не завершится, пока
// системный вызов не вернётся, — поэтому шаги остановки пишут файлы без fsync
// (AuthService.Stop, TrafficQuotaService.Stop).
const (
	shutdownHTTPTimeout = 5 * time.Second
	shutdownDeadline    = 8 * time.Second
	// shutdownSlowStep — шаг остановки дольше этого попадает в журнал.
	shutdownSlowStep = time.Second
)

// shutdownSteps отслеживает отложенные шаги остановки main(): медленный шаг
// попадает в журнал, а при истечении срока процесс завершается с именем шага,
// на котором застрял.
type shutdownSteps struct {
	current atomic.Pointer[string]
	armed   atomic.Bool
}

// enter отмечает начало шага name.
func (s *shutdownSteps) enter(name string) {
	s.current.Store(&name)
}

// run выполняет шаг name (для defer в main) и пишет в журнал, если он шёл
// дольше shutdownSlowStep.
func (s *shutdownSteps) run(name string, fn func()) {
	s.enter(name)
	start := time.Now()
	fn()
	if d := time.Since(start); d >= shutdownSlowStep {
		log.Printf("Shutdown: %s took %s", name, d.Round(time.Millisecond))
	}
}

// arm запускает срок остановки: по истечении d вызывается exit с сообщением о
// шаге, на котором остановка застряла. Повторный вызов ничего не делает.
func (s *shutdownSteps) arm(d time.Duration, exit func(format string, v ...any)) {
	if !s.armed.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(d, func() {
		step := "-"
		if p := s.current.Load(); p != nil {
			step = *p
		}
		exit("Shutdown: deadline %s exceeded at step %s, exiting without waiting", d, step)
	})
}
