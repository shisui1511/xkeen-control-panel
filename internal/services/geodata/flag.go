package geodata

import (
	"log"
	"time"
)

// OnFlagOff вызывается после выключения флага слоя «Конфигурация»: расписание
// обновления баз возвращается XKeen полностью (D-13).
//   - панель убирала строку cron XKeen при перенятии — она возвращается дословно, при
//     любом состоянии расписания панели;
//   - строки не было, а расписание панели включено — ставится строка XKeen с тем же
//     временем;
//   - строки не было и расписание панели выключено — crontab не трогается.
//
// Расписание панели помечается приостановленным (D-36): при включении флага само оно
// не запустится. Ошибка записи crontab возвращается, состояние при этом всё равно
// обновляется (CronTaken остаётся для повторной попытки).
func (s *Service) OnFlagOff() error {
	s.sched.cronMu.Lock()
	defer s.sched.cronMu.Unlock()

	s.mu.Lock()
	taken := s.st.CronTaken
	sch := s.st.Schedule
	s.mu.Unlock()

	var werr error
	cleared := false
	info, err := s.readCron()
	if err != nil {
		werr = ErrCronFailed
	} else {
		switch {
		case taken != nil:
			if !info.found {
				werr = s.writeCron(info.content, withLine(info.content, taken.Line))
			}
			cleared = werr == nil
		case sch.Enabled:
			if !info.found {
				werr = s.writeCron(info.content, withLine(info.content, cronLine(sch)))
			}
		}
	}
	if werr != nil {
		log.Printf("[geodata] возврат расписания XKeen в crontab не удался: %v", werr)
	}

	s.mu.Lock()
	s.st.Suspended = sch.Enabled || taken != nil
	s.st.CronTwo = false
	s.st.RetryAt = time.Time{}
	if cleared {
		s.st.CronTaken = nil
	}
	s.mu.Unlock()
	s.changed()
	s.syncCron(true)
	return werr
}

// OnFlagOn вызывается после включения флага слоя. Ничего не запускает: расписание
// панели остаётся приостановленным, пока владелец не перенимет его кнопкой.
func (s *Service) OnFlagOn() {
	s.changed()
	s.syncCron(true)
}
