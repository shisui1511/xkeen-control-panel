//go:build router

package geodata

import "testing"

// Признак синхронизации часов от настоящего ndmc устройства (WR-06): планировщик
// получает слот расписания, а не отказывается считать его по «несинхронным» часам.
// Ядро тест не читает и не переключает — метки ядра нет.
func TestRouterGeodataClockSync(t *testing.T) {
	svc, _ := rtCronSetup(t)

	svc.refreshNTP()
	switch st := ntpState(svc.sched.ntp.Load()); st {
	case ntpSynced:
	case ntpUnsynced:
		t.Skip("NDM сообщает, что часы стенда не синхронизированы: сбой сети стенда, не дефект панели")
	default:
		t.Fatalf("ndmc -c \"show ntp status\": признак не разобран (состояние %d)", st)
	}
	if !svc.clockValid(svc.d.Now()) {
		t.Fatalf("часы синхронизированы по NDM, а clockValid отказывает: %v", svc.d.Now())
	}

	if err := svc.SetSchedule(Schedule{Enabled: true, Day: -1, Hour: 3, Minute: 17}); err != nil {
		t.Fatalf("SetSchedule: %v", err)
	}
	if v := rtCronSnap(t, svc); v.Schedule.Next == "" {
		t.Fatalf("включённое расписание при синхронных часах без времени следующего запуска: %+v", v.Schedule)
	}
}
