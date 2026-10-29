package geodata

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Schedule — расписание обновления баз панелью (день, час, минута по часам роутера).
type Schedule = ScheduleState

const (
	// catchUpDelay — через сколько после старта панели запускается пропущенный слот
	// (панель или роутер были выключены в плановое время).
	catchUpDelay = 3 * time.Minute
	// retryDelay — через сколько повторяется запуск, целиком упавший из-за сети.
	retryDelay = time.Hour
	// schedMaxWait — планировщик проверяет время не реже этого срока: смена пояса или
	// правка расписания подхватываются следующей проверкой.
	schedMaxWait = 30 * time.Second
	// schedMinWait — нижняя граница ожидания между проверками.
	schedMinWait = time.Second
)

// Триггеры запусков планировщика (LastRun.Trigger).
const (
	triggerSchedule = "schedule"
	triggerCatchup  = "catchup"
	triggerRetry    = "retry"
)

// schedState — рабочее состояние расписания и cron: то, что не хранится в файле
// состояния. Одно поле в Service, чтобы правка service.go оставалась минимальной.
type schedState struct {
	// cronMu сериализует чтение-правку-запись crontab.
	cronMu sync.Mutex

	// Кэш последнего чтения crontab (под Service.mu): без изменения времени и размера
	// тик файл не перечитывает.
	cronSeen    bool
	cronMissing bool
	cronMtime   time.Time
	cronSize    int64

	// Поля планировщика читает и пишет только его горутина.
	begin        time.Time // старт сервиса (монотонные часы: скачок времени по NTP не мешает)
	initDone     bool
	catchPending bool
	catchAt      time.Time
	ntpAt        time.Time // когда последний раз спрашивали ndmc (часы процесса, не d.Now)

	// ntp — последний известный ntpState; читают все, пишет refreshNTP.
	ntp atomic.Int32
}

// slotAt — время слота расписания в день d по часам роутера.
func slotAt(d time.Time, sch Schedule) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), sch.Hour, sch.Minute, 0, 0, time.Local)
}

// nextSlot — ближайший слот строго после now. Ежедневно — каждый день, иначе —
// ближайший такой день недели (0 — воскресенье).
func nextSlot(now time.Time, sch Schedule) time.Time {
	now = now.In(time.Local)
	for i := 0; i <= 7; i++ {
		c := slotAt(now.AddDate(0, 0, i), sch)
		if sch.Day >= 0 && int(c.Weekday()) != sch.Day {
			continue
		}
		if c.After(now) {
			return c
		}
	}
	return slotAt(now.AddDate(0, 0, 1), sch)
}

// prevSlot — последний слот не позже now.
func prevSlot(now time.Time, sch Schedule) time.Time {
	now = now.In(time.Local)
	for i := 0; i <= 7; i++ {
		c := slotAt(now.AddDate(0, 0, -i), sch)
		if sch.Day >= 0 && int(c.Weekday()) != sch.Day {
			continue
		}
		if !c.After(now) {
			return c
		}
	}
	return slotAt(now.AddDate(0, 0, -7), sch)
}

// markSlotLocked считает прошедший слот отработанным: расписание только что создано,
// перенято или изменено, слоты до этого момента запускать нельзя. Часы не
// синхронизированы — ничего не делает. Вызывается под Service.mu.
func (s *Service) markSlotLocked() {
	now := s.d.Now()
	if !s.clockValid(now) {
		return
	}
	s.st.LastSlotDone = prevSlot(now, s.st.Schedule).Format(time.RFC3339)
}

// validSchedule проверяет границы расписания.
func validSchedule(sch Schedule) bool {
	return sch.Hour >= 0 && sch.Hour <= 23 && sch.Minute >= 0 && sch.Minute <= 59 &&
		sch.Day >= -1 && sch.Day <= 6
}

// SetSchedule сохраняет расписание панели. crontab не трогается: выключение
// расписания означает «без расписания», cron XKeen при этом не возвращается (D-13).
// Сохранение включённого расписания снимает приостановку (D-36), если перенимать
// нечего: строки cron XKeen нет или её расписание нестандартное. Стандартную строку
// перенимает только кнопка «Перенять расписание» — до неё приостановка остаётся.
// Код отказа — schedule_invalid.
func (s *Service) SetSchedule(sch Schedule) error {
	if !validSchedule(sch) {
		return ErrScheduleInvalid
	}
	s.sched.cronMu.Lock()
	defer s.sched.cronMu.Unlock()
	resume := false
	if sch.Enabled {
		info, err := s.readCron()
		resume = err == nil && (!info.found || !info.standard)
	}
	s.mu.Lock()
	s.st.Schedule = sch
	if resume {
		s.st.Suspended = false
	}
	if !sch.Enabled {
		s.st.RetryAt = time.Time{}
		s.st.RetryFailed = false
	}
	s.markSlotLocked()
	s.mu.Unlock()
	s.changed()
	s.syncCron(true)
	return nil
}

// parseSlot разбирает LastSlotDone; пусто или ошибка — нулевое время.
func parseSlot(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// scheduleActive — расписание панели должно срабатывать: включено, не приостановлено
// и слой «Конфигурация» включён.
func (s *Service) scheduleActive(sch Schedule, suspended bool) bool {
	return sch.Enabled && !suspended && s.d.Layer.Enabled()
}

// schedLoop — планировщик: засыпает до ближайшего события (слот, повтор, догон), но не
// дольше schedMaxWait. Останавливается вместе с сервисом (контекст Stop).
func (s *Service) schedLoop() {
	for {
		t := time.NewTimer(s.schedStep())
		select {
		case <-s.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// schedStep — одна проверка планировщика; возвращает, сколько ждать до следующей.
// Пока часы не синхронизированы (clockValid), слоты не вычисляются и догон не
// планируется.
func (s *Service) schedStep() time.Duration {
	s.refreshNTP()
	now := s.d.Now()
	if !s.clockValid(now) {
		return schedMaxWait
	}
	s.mu.Lock()
	sch, suspended, retryAt := s.st.Schedule, s.st.Suspended, s.st.RetryAt
	lastDone := parseSlot(s.st.LastSlotDone)
	s.mu.Unlock()
	active := s.scheduleActive(sch, suspended)

	// Догон при старте: слот между прошлым запуском и стартом панели пропущен.
	// Догоняется только последний слот (prevSlot не дальше одного периода от now),
	// сколько бы слотов ни прошло.
	if !s.sched.initDone {
		s.sched.initDone = true
		if active && !lastDone.IsZero() {
			prev := prevSlot(now, sch)
			if prev.After(lastDone) {
				delay := catchUpDelay - time.Since(s.sched.begin)
				if delay < 0 {
					delay = 0
				}
				s.sched.catchPending = true
				s.sched.catchAt = now.Add(delay)
			}
		}
	}
	if !active {
		s.sched.catchPending = false
		if !retryAt.IsZero() {
			s.mu.Lock()
			s.st.RetryAt = time.Time{}
			s.mu.Unlock()
			s.changed()
		}
		return schedMaxWait
	}
	prev := prevSlot(now, sch)
	if lastDone.IsZero() {
		// Слот ещё ни разу не отмечался (расписание создано при несинхронных часах):
		// прошедший не запускается.
		s.setLastSlot(prev)
		return schedMaxWait
	}
	if s.sched.catchPending && now.Before(s.sched.catchAt) {
		return clampWait(s.sched.catchAt.Sub(now))
	}

	trigger := ""
	switch {
	case prev.After(lastDone):
		trigger = triggerSchedule
		if s.sched.catchPending {
			trigger = triggerCatchup
		}
		s.setLastSlot(prev)
	case !retryAt.IsZero() && !now.Before(retryAt):
		trigger = triggerRetry
		s.mu.Lock()
		s.st.RetryAt = time.Time{}
		s.mu.Unlock()
		s.changed()
	}
	s.sched.catchPending = false
	if trigger != "" {
		s.runScheduled(trigger)
		return schedMinWait
	}
	wait := nextSlot(now, sch).Sub(now)
	if !retryAt.IsZero() && retryAt.After(now) && retryAt.Sub(now) < wait {
		wait = retryAt.Sub(now)
	}
	return clampWait(wait)
}

// clampWait ограничивает ожидание границами планировщика.
func clampWait(d time.Duration) time.Duration {
	if d > schedMaxWait {
		return schedMaxWait
	}
	if d < schedMinWait {
		return schedMinWait
	}
	return d
}

// setLastSlot отмечает слот отработанным до запуска: упавший запуск тот же слот не
// повторяет (повтор — отдельным событием RetryAt).
func (s *Service) setLastSlot(slot time.Time) {
	s.mu.Lock()
	s.st.LastSlotDone = slot.Format(time.RFC3339)
	s.mu.Unlock()
	s.changed()
}

// runScheduled запускает `xkeen -ug` по расписанию: ждёт конца идущего «Применить»
// (waiting_apply), затем по итогу назначает повтор через час (запуск целиком упал из-за
// сети) или отмечает, что повтор тоже не удался (уведомление, дальше по слотам).
func (s *Service) runScheduled(trigger string) {
	started := s.d.Now()
	err := s.run(s.ctx, false, trigger)
	if s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	lr := s.st.LastRun
	if lr == nil || lr.At.Before(started) {
		// Запуск не состоялся (замок слоя не получен): итог прежний.
		s.mu.Unlock()
		if err != nil {
			log.Printf("[geodata] запуск по расписанию (%s) не состоялся: %v", trigger, err)
		}
		return
	}
	if lr.Outcome == outcomeFailed && lr.Reason == RunErrNetwork {
		if trigger == triggerRetry {
			s.st.RetryFailed = true
			s.st.RetryAt = time.Time{}
		} else {
			s.st.RetryAt = s.d.Now().Add(retryDelay)
		}
	}
	s.mu.Unlock()
	s.changed()
}
