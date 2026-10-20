package main

import (
	"log"
	"runtime"
	"sync/atomic"
	"time"
)

// Главный поток процесса закреплён за главной горутиной: LockOSThread в init
// оставляет main на этом потоке, и другие горутины на нём не выполняются. Сигнал
// процессу (killall из init-скрипта) ядро отдаёт главному потоку, если тот не
// блокирует сигнал и у него нет других ожидающих, — даже когда поток стоит в
// непрерываемом ожидании; другие потоки сигнал не видят, пока не получат свой.
// Без закрепления планировщик Go выполнял бы на главном потоке любые горутины, и
// горутина, ждущая носитель (запись файла на флешку в застое), задерживала бы
// SIGTERM на секунды: слушатель HTTP оставался открытым, остановка не начиналась.
// После запуска главная горутина только ждёт сигнал в select, поэтому главный поток
// спит в прерываемом ожидании и забирает сигнал сразу.
func init() {
	runtime.LockOSThread()
}

// Сроки остановки по SIGTERM/SIGINT. HTTP-сервер ждёт идущие запросы не дольше
// shutdownHTTPTimeout, весь выход — не дольше shutdownDeadline: застрявший шаг
// (дочерний процесс, сеть, замок, запись на носитель) бросается. Срок взводится
// до первой строки журнала, журнал пишется асинхронно (utils.AsyncWriter), и
// аварийный выход сам на носитель не пишет. Финальные записи состояния
// (AuthService.Stop, TrafficQuotaService.Stop) остаются шагами остановки — без
// fsync: пропуск терял бы сессии и счётчики трафика при каждом перезапуске. Но
// каждая идёт в своей горутине со своим бюджетом (shutdownSteps.save): под
// застоем журнала ext4 создание файла стоит секундами, и шаг на главной горутине
// съедал весь срок. Поток, уже стоящий в непрерываемом ожидании носителя, ни
// срок, ни выход процесса не прервут — процесс живёт до возврата системного
// вызова; поэтому init-скрипт S99xcp (scripts/setup.sh) ждёт настоящего выхода
// процесса, а не окно rc.func (около 10 с, после которого запуск ответил бы
// «already running»).
const (
	shutdownHTTPTimeout = 5 * time.Second
	shutdownDeadline    = 8 * time.Second
	// shutdownLogDrain — сколько выход ждёт запись очереди журнала на носитель.
	shutdownLogDrain = time.Second
	// shutdownSlowStep — шаг остановки дольше этого попадает в журнал.
	shutdownSlowStep = time.Second
	// shutdownSaveBudget — сколько остановка ждёт шаг с финальной записью
	// состояния. Обычно запись идёт миллисекунды; не уложилась — остановка идёт
	// дальше, не дожидаясь носителя.
	shutdownSaveBudget = time.Second
	// shutdownSaveReserve — сколько до срока остаётся на шаги после финальных
	// записей и дозапись журнала: бюджет записи урезается, чтобы не съесть срок,
	// даже если HTTP-сервер ждал запросы весь shutdownHTTPTimeout.
	shutdownSaveReserve = shutdownLogDrain + 500*time.Millisecond
)

// shutdownSteps отслеживает отложенные шаги остановки main(): медленный шаг
// попадает в журнал, а при истечении срока процесс завершается с именем шага,
// на котором застрял.
type shutdownSteps struct {
	current atomic.Pointer[string]
	armed   atomic.Bool
	// deadline — момент срока (UnixNano); 0 — срок не взведён.
	deadline atomic.Int64
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

// save выполняет шаг name с финальной записью состояния (для defer в main) в
// своей горутине и ждёт его не дольше shutdownSaveBudget и не дольше, чем
// позволяет срок (shutdownSaveReserve до него). Не уложившийся шаг продолжается
// до выхода процесса: успеет — состояние записано; нет — на носителе остаётся
// прежний файл целиком (подмена переименованием, utils.AtomicWriteFileNoSync).
// Главная горутина ждёт шаг в select, поэтому главный поток в непрерываемое
// ожидание носителя не попадает.
func (s *shutdownSteps) save(name string, fn func()) {
	s.enter(name)
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	wait := shutdownSaveBudget
	if dl := s.deadline.Load(); dl != 0 {
		wait = min(wait, max(time.Until(time.Unix(0, dl))-shutdownSaveReserve, 0))
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-done:
		if d := time.Since(start); d >= shutdownSlowStep {
			log.Printf("Shutdown: %s took %s", name, d.Round(time.Millisecond))
		}
	case <-t.C:
		log.Printf("Shutdown: %s not finished in %s, continuing without waiting for storage", name, wait.Round(time.Millisecond))
	}
}

// arm запускает срок остановки: по истечении d вызывается exit с сообщением о
// шаге, на котором остановка застряла. Повторный вызов ничего не делает.
func (s *shutdownSteps) arm(d time.Duration, exit func(format string, v ...any)) {
	if !s.armed.CompareAndSwap(false, true) {
		return
	}
	s.deadline.Store(time.Now().Add(d).UnixNano())
	time.AfterFunc(d, func() {
		step := "-"
		if p := s.current.Load(); p != nil {
			step = *p
		}
		exit("Shutdown: deadline %s exceeded at step %s, exiting without waiting", d, step)
	})
}
