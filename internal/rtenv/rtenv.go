// Package rtenv читает из окружения процесса сокращённые сроки срабатывания
// панели — те, которые роутерные тесты пережидают (окно подтверждения DNS,
// паузы сторожа перехвата, догон geodata).
//
// Переменные XCP_RT_* задаёт только обвязка тестового стенда на время прогона
// (через xcp.env; D-15, D-16 фазы 148.1.1): у пользователя их нет, и панель
// работает на прежних сроках. Значение читается один раз при старте процесса;
// переменная может только сократить срок — не ниже нижней границы и не выше
// значения по умолчанию. Каждая заданная переменная оставляет заметку, которую
// main пишет в журнал строкой «xcp-rt: …» после баннера старта: по ней смоук
// отличает стенд с сокращёнными сроками от обычного запуска.
package rtenv

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Var — срок, сокращаемый переменной окружения Name (целое число секунд) в
// пределах [Min; Default].
type Var struct {
	Name    string
	Default time.Duration
	Min     time.Duration
}

// Сроки, читаемые из окружения. Умолчания равны прежним константам пакетов.
var (
	// DNSWindow — окно подтверждения перехвата DNS (dnstakeover.windowDuration).
	DNSWindow = Var{Name: "XCP_RT_DNS_WINDOW_S", Default: 60 * time.Second, Min: 20 * time.Second}
	// DNSGuardBusyGrace — пауза сторожа перехвата после операции над ядрами и
	// после старта панели (dnstakeover.guardBusyGrace).
	DNSGuardBusyGrace = Var{Name: "XCP_RT_DNS_GUARD_BUSY_GRACE_S", Default: 60 * time.Second, Min: 10 * time.Second}
	// DNSGuardRearm — не чаще этого сторож возвращает перехват
	// (dnstakeover.guardRearmMinInterval).
	DNSGuardRearm = Var{Name: "XCP_RT_DNS_GUARD_REARM_S", Default: 90 * time.Second, Min: 10 * time.Second}
	// GeodataCatchUp — через сколько после старта панели запускается пропущенный
	// слот обновления баз (geodata.catchUpDelay). Нижняя граница — шаг
	// планировщика schedMaxWait: раньше слот всё равно не заметят.
	GeodataCatchUp = Var{Name: "XCP_RT_GEODATA_CATCHUP_S", Default: 3 * time.Minute, Min: 30 * time.Second}
)

var (
	notesMu sync.Mutex
	notes   []string
)

// Parse разбирает значение raw переменной v: пустое — Default без заметки;
// не целое число секунд, меньше Min или больше Default — Default и заметка с
// причиной; иначе — значение и заметка о принятом сроке.
func (v Var) Parse(raw string) (time.Duration, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return v.Default, ""
	}
	// Границы сравниваются в секундах: умножение большого числа на time.Second
	// переполнилось бы.
	def, low := secs(v.Default), secs(v.Min)
	n, err := strconv.Atoi(raw)
	switch {
	case err != nil:
		return v.Default, fmt.Sprintf("%s=%q отклонено: не целое число секунд, срок %d с", v.Name, raw, def)
	case n < low:
		return v.Default, fmt.Sprintf("%s=%d отклонено: меньше нижней границы %d с, срок %d с", v.Name, n, low, def)
	case n > def:
		return v.Default, fmt.Sprintf("%s=%d отклонено: больше значения по умолчанию %d с, срок %d с", v.Name, n, def, def)
	}
	return time.Duration(n) * time.Second, fmt.Sprintf("%s=%d (по умолчанию %d с)", v.Name, n, def)
}

// Get читает переменную из окружения процесса (Parse) и запоминает непустую
// заметку для Notes. Вызывается один раз при инициализации пакета-потребителя.
func (v Var) Get() time.Duration {
	d, note := v.Parse(os.Getenv(v.Name))
	if note != "" {
		notesMu.Lock()
		notes = append(notes, note)
		notesMu.Unlock()
	}
	return d
}

// Notes — заметки о заданных переменных (принятых и отклонённых) в порядке
// чтения; без переменных пусто.
func Notes() []string {
	notesMu.Lock()
	defer notesMu.Unlock()
	return append([]string(nil), notes...)
}

func secs(d time.Duration) int { return int(d / time.Second) }
