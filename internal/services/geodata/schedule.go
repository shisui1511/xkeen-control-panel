package geodata

import (
	"sync"
	"time"
)

// Schedule — расписание обновления баз панелью (день, час, минута по часам роутера).
type Schedule = ScheduleState

// minClockYear — пока год часов роутера меньше (нет NTP), слоты не вычисляются.
const minClockYear = 2025

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
}

// clockValid — часы роутера выглядят синхронизированными.
func clockValid(now time.Time) bool { return now.Year() >= minClockYear }

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

// slotPeriod — период расписания: сутки или неделя.
func slotPeriod(sch Schedule) time.Duration {
	if sch.Day >= 0 {
		return 7 * 24 * time.Hour
	}
	return 24 * time.Hour
}

// markSlotLocked считает прошедший слот отработанным: расписание только что создано,
// перенято или изменено, слоты до этого момента запускать нельзя. Часы не
// синхронизированы — ничего не делает. Вызывается под Service.mu.
func (s *Service) markSlotLocked() {
	now := s.d.Now()
	if !clockValid(now) {
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
// Код отказа — schedule_invalid.
func (s *Service) SetSchedule(sch Schedule) error {
	if !validSchedule(sch) {
		return ErrScheduleInvalid
	}
	s.mu.Lock()
	s.st.Schedule = sch
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
