package geodata

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Признак синхронизации часов роутера для планировщика. Год не доверяется: до NTP
// прошивка стартует с запасным временем (например, датой сборки), и слоты вычислялись
// бы по неверным часам. NDM сообщает синхронизацию в `ndmc -c "show ntp status"`;
// флаг ядра STA_UNSYNC (adjtimex) для этого не годится: прошивка ставит время шагом и
// флаг остаётся взведённым и при синхронных часах.

const (
	// clockMinYear — часы с годом меньше заведомо не синхронизированы. Это нижняя
	// граница на устройствах без ndmc; пересмотреть, когда она отстанет от года сборки
	// самой старой прошивки, на которой панель ещё поддерживается.
	clockMinYear = 2025
	// ntpRecheck — как часто повторяется запрос, когда часы синхронизированы или ndmc
	// недоступен: синхронные часы сами не рассинхронизируются, падающий запрос не должен
	// порождать процесс на каждом тике.
	ntpRecheck = 10 * time.Minute
	// ntpTimeout — предел ожидания ответа ndmc.
	ntpTimeout = 3 * time.Second
)

// ntpState — что известно о синхронизации часов роутера.
type ntpState int32

const (
	ntpUnknown  ntpState = iota // ndmc недоступен или ответ не разобран: решает год
	ntpSynced                   // NDM считает часы верными
	ntpUnsynced                 // NDM сообщает, что часы не синхронизированы
)

// ndmcNTPStatus — вывод `ndmc -c "show ntp status"`.
func ndmcNTPStatus(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "ndmc", "-c", "show ntp status").Output()
}

// parseNTPStatus разбирает вывод `show ntp status`. Часы верны, если NDM получил время
// по NTP (synchronized), от NDSS (ndsstime) или владелец задал его вручную (usertime).
// Нет ключа synchronized — вывод не разобран, это ntpUnknown, а не «не синхронизированы».
func parseNTPStatus(out []byte) ntpState {
	seen, ok := false, false
	for _, line := range strings.Split(string(out), "\n") {
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "synchronized":
			seen = true
			ok = ok || v == "yes"
		case "ndsstime", "usertime":
			ok = ok || v == "yes"
		}
	}
	switch {
	case !seen:
		return ntpUnknown
	case ok:
		return ntpSynced
	}
	return ntpUnsynced
}

// refreshNTP обновляет признак синхронизации. Вызывается только из schedStep: процесс
// ndmc не должен запускаться под Service.mu, а остальные читатели берут готовое
// значение из clockValid. Пока часы не синхронизированы, запрос идёт на каждом тике.
func (s *Service) refreshNTP() {
	st := ntpState(s.sched.ntp.Load())
	if st != ntpUnsynced && !s.sched.ntpAt.IsZero() && time.Since(s.sched.ntpAt) < ntpRecheck {
		return
	}
	s.sched.ntpAt = time.Now()
	ctx, cancel := context.WithTimeout(s.ctx, ntpTimeout)
	defer cancel()
	out, err := s.d.NTPStatus(ctx)
	if err != nil {
		s.sched.ntp.Store(int32(ntpUnknown))
		return
	}
	s.sched.ntp.Store(int32(parseNTPStatus(out)))
}

// clockValid — часы роутера выглядят синхронизированными: год не меньше clockMinYear и
// NDM не сообщает о несинхронизированных часах. Пока слоты не вычисляются, догон не
// планируется. Безопасна для вызова под Service.mu.
func (s *Service) clockValid(now time.Time) bool {
	return now.Year() >= clockMinYear && ntpState(s.sched.ntp.Load()) != ntpUnsynced
}
