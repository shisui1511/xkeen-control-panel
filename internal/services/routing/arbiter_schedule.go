package routing

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Окна расписания ручных групп (D-08…D-11): разбор и проверка окна, отрезки на
// недельной оси, пересечения, действующее окно и правка окон группы.

const (
	minutesPerDay  = 24 * 60
	minutesPerWeek = 7 * minutesPerDay
	// maxWindowsPerGroup — предел окон одной группы.
	maxWindowsPerGroup = 24
)

// ErrWindowNotFound — у группы нет окна с таким ID (или нет действующего окна для
// «Вернуть расписание»).
var ErrWindowNotFound = errors.New("routing: окно расписания не найдено")

// ScheduleOverlapError — окно пересекается с другим окном той же группы (D-11).
// Window — ID окна, с которым конфликт.
type ScheduleOverlapError struct{ Window string }

func (e *ScheduleOverlapError) Error() string {
	return "routing: окно пересекается с окном " + e.Window
}

// parseClock переводит HH:MM в минуты суток 0…1439.
func parseClock(hhmm string) (int, error) {
	if len(hhmm) != 5 || hhmm[2] != ':' {
		return 0, errors.New("routing: время не в формате HH:MM")
	}
	for _, i := range []int{0, 1, 3, 4} {
		if hhmm[i] < '0' || hhmm[i] > '9' {
			return 0, errors.New("routing: время не в формате HH:MM")
		}
	}
	h, _ := strconv.Atoi(hhmm[:2])
	m, _ := strconv.Atoi(hhmm[3:])
	if h > 23 {
		return 0, errors.New("routing: час вне 00…23")
	}
	if m > 59 {
		return 0, errors.New("routing: минуты вне 00…59")
	}
	return h*60 + m, nil
}

// validateWindow проверяет дни, время и участника окна и приводит дни к порядку с
// понедельника.
func validateWindow(w *ScheduleWindow) error {
	if len(w.Days) == 0 || len(w.Days) > 7 {
		return invalid("days", "invalid")
	}
	seen := map[int]bool{}
	for _, d := range w.Days {
		if d < 0 || d > 6 || seen[d] {
			return invalid("days", "invalid")
		}
		seen[d] = true
	}
	sort.Ints(w.Days)
	from, err := parseClock(w.From)
	if err != nil {
		return invalid("from", "invalid")
	}
	to, err := parseClock(w.To)
	if err != nil || to == from {
		return invalid("to", "invalid")
	}
	if w.Member == "" {
		return invalid("member", "invalid")
	}
	return nil
}

// windowSegments — полуоткрытые отрезки окна на недельной оси минут
// [0, minutesPerWeek), отсчёт с полуночи понедельника. Обычное окно дня d —
// [d·1440+from, d·1440+to); окно через полночь — [d·1440+from, d·1440+1440+to), часть
// за концом недели (окно воскресенья) переносится в начало недели вторым отрезком.
// Испорченное окно отрезков не даёт.
func windowSegments(w ScheduleWindow) [][2]int {
	from, err1 := parseClock(w.From)
	to, err2 := parseClock(w.To)
	if err1 != nil || err2 != nil || from == to {
		return nil
	}
	out := make([][2]int, 0, len(w.Days)+1)
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			continue
		}
		start := d*minutesPerDay + from
		end := d*minutesPerDay + to
		if from > to {
			end += minutesPerDay
		}
		if end > minutesPerWeek {
			out = append(out, [2]int{start, minutesPerWeek}, [2]int{0, end - minutesPerWeek})
			continue
		}
		out = append(out, [2]int{start, end})
	}
	return out
}

// windowsOverlap сообщает, что окна a и b пересекаются хотя бы одной минутой
// недели (включено окно или нет, здесь не важно).
func windowsOverlap(a, b ScheduleWindow) bool {
	sb := windowSegments(b)
	for _, x := range windowSegments(a) {
		for _, y := range sb {
			if x[0] < y[1] && y[0] < x[1] {
				return true
			}
		}
	}
	return false
}

// weekday0Mon — день недели t в нумерации окон: 0 — понедельник … 6 — воскресенье.
func weekday0Mon(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

// hasDay сообщает, что день d есть среди дней окна.
func hasDay(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

// atClock — момент дня t (со сдвигом addDays) в минуту суток min, в поясе t.
func atClock(t time.Time, addDays, min int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()+addDays, min/60, min%60, 0, 0, t.Location())
}

// activeWindow возвращает включённое окно, действующее в момент now, и его конец.
// Обычное окно (From < To) действует в дни Days с From до To. Окно через полночь
// (From > To) действует в день из Days с From до полуночи (конец — завтра в To) и в
// следующий за ним день с полуночи до To (конец — сегодня в To). Окна одной группы
// не пересекаются, поэтому подходящее окно одно; при порче данных берётся первое.
func activeWindow(ws []ScheduleWindow, now time.Time) (ScheduleWindow, time.Time, bool) {
	day := weekday0Mon(now)
	prev := (day + 6) % 7
	cur := now.Hour()*60 + now.Minute()
	for _, w := range ws {
		if !w.Enabled {
			continue
		}
		from, err1 := parseClock(w.From)
		to, err2 := parseClock(w.To)
		if err1 != nil || err2 != nil || from == to {
			continue
		}
		if from < to {
			if hasDay(w.Days, day) && from <= cur && cur < to {
				return w, atClock(now, 0, to), true
			}
			continue
		}
		if hasDay(w.Days, day) && cur >= from {
			return w, atClock(now, 1, to), true
		}
		if hasDay(w.Days, prev) && cur < to {
			return w, atClock(now, 0, to), true
		}
	}
	return ScheduleWindow{}, time.Time{}, false
}

// --- правка окон ---

// manualAppliedGroup возвращает применённую ручную группу gid и модель применённого
// состояния; иначе ErrGroupNotApplied (D-10).
func (s *Service) manualAppliedGroup(gid string) (Group, appliedModel, configlayer.StateView, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return Group{}, appliedModel{}, st, err
	}
	if !configlayer.DesiredRouting(st.Applied).AnyManaged() {
		return Group{}, appliedModel{}, st, ErrGroupNotApplied
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return Group{}, appliedModel{}, st, err
	}
	g, ok := am.group(gid)
	if !ok || g.Mode != ModeManual {
		return Group{}, appliedModel{}, st, ErrGroupNotApplied
	}
	return g, am, st, nil
}

// scheduleChanged будит арбитр и сообщает клиентам о смене окон.
func (s *Service) scheduleChanged() {
	if s.arb != nil {
		s.arb.Kick()
	}
	s.d.Layer.PublishProxies(configlayer.ProxiesKindStructure)
}

// SaveScheduleWindow сохраняет окно расписания ручной группы gid: новое (пустой
// ID) или замена окна с тем же ID. Группа должна быть применённой и ручной (D-10),
// участник — среди вариантов группы по применённому состоянию. Окно не должно
// пересекаться с другими окнами группы, включая выключенные (D-11): иначе
// включение окна создало бы конфликт. Окон у группы не больше maxWindowsPerGroup.
// После записи арбитр сверяет группы сразу, клиенты получают событие структуры.
func (s *Service) SaveScheduleWindow(gid string, w ScheduleWindow) (ScheduleWindow, error) {
	g, am, _, err := s.manualAppliedGroup(gid)
	if err != nil {
		return ScheduleWindow{}, err
	}
	if err := validateWindow(&w); err != nil {
		return ScheduleWindow{}, err
	}
	if !memberAllowed(g, w.Member, am) {
		return ScheduleWindow{}, ErrMemberInvalid
	}
	if w.ID == "" {
		id, err := newID(s.d.Rand)
		if err != nil {
			return ScheduleWindow{}, err
		}
		w.ID = id
	} else if !idRE.MatchString(w.ID) {
		return ScheduleWindow{}, invalid("id", "invalid")
	}
	err = s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		all := readSchedules(live)
		list := all[gid]
		idx := -1
		for i := range list {
			if list[i].ID == w.ID {
				idx = i
				continue
			}
			if windowsOverlap(w, list[i]) {
				return &ScheduleOverlapError{Window: list[i].ID}
			}
		}
		if idx >= 0 {
			list[idx] = w
		} else {
			// Чужой ID окна клиент не выдумывает: новое окно получает ID сервера.
			for _, other := range all {
				for _, x := range other {
					if x.ID == w.ID {
						return invalid("id", "invalid")
					}
				}
			}
			if len(list) >= maxWindowsPerGroup {
				return invalid("windows", "too_many")
			}
			list = append(list, w)
		}
		all[gid] = list
		return writeSchedules(live, all)
	})
	if err != nil {
		return ScheduleWindow{}, err
	}
	s.scheduleChanged()
	return w, nil
}

// editWindow применяет fn к окну id группы gid в Live; окна нет — ErrWindowNotFound.
// fn возвращает новый список окон группы.
func (s *Service) editWindow(gid, id string, fn func(list []ScheduleWindow, i int) []ScheduleWindow) error {
	err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		all := readSchedules(live)
		list := all[gid]
		for i := range list {
			if list[i].ID == id {
				all[gid] = fn(list, i)
				return writeSchedules(live, all)
			}
		}
		return ErrWindowNotFound
	})
	if err != nil {
		return err
	}
	s.scheduleChanged()
	return nil
}

// DeleteScheduleWindow удаляет окно id группы gid. Удаление действующего окна
// возвращает группе исходный выбор следующим тиком арбитра (D-06).
func (s *Service) DeleteScheduleWindow(gid, id string) error {
	return s.editWindow(gid, id, func(list []ScheduleWindow, i int) []ScheduleWindow {
		return append(list[:i:i], list[i+1:]...)
	})
}

// SetScheduleWindowEnabled включает или выключает окно id группы gid. Пересечения
// при включении нет: SaveScheduleWindow проверяет и выключенные окна.
func (s *Service) SetScheduleWindowEnabled(gid, id string, enabled bool) error {
	return s.editWindow(gid, id, func(list []ScheduleWindow, i int) []ScheduleWindow {
		list[i].Enabled = enabled
		return list
	})
}

// RestoreSchedule — «Вернуть расписание»: снимает перехват действующего окна
// группы gid (запись арбитра — держатель окно, без Override и Until) и сразу
// ставит узел окна входом арбитра. Действующего окна нет — ErrWindowNotFound, узла
// окна нет среди вариантов группы — ErrMemberInvalid.
func (s *Service) RestoreSchedule(ctx context.Context, gid string) (SelectResult, error) {
	g, am, st, err := s.manualAppliedGroup(gid)
	if err != nil {
		return SelectResult{}, err
	}
	w, _, ok := activeWindow(readSchedules(st.Live)[gid], s.d.Now())
	if !ok {
		return SelectResult{}, ErrWindowNotFound
	}
	if !memberAllowed(g, w.Member, am) || !am.avail(w.Member) {
		return SelectResult{}, ErrMemberInvalid
	}
	err = s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		m := readArbiter(live)
		rec, had := m[gid]
		origin := liveSelection(live)[gid]
		if had {
			origin = rec.Origin
		}
		m[gid] = arbiterRecord{Origin: origin, Holder: holderSchedule, Window: w.ID}
		return writeArbiter(live, m)
	})
	if err != nil {
		return SelectResult{}, err
	}
	res, err := s.selectArbiter(ctx, gid, w.Member)
	s.scheduleChanged()
	return res, err
}
