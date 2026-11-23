package routing

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Арбитр выбора ручных групп (SPX-01, D-01, D-06, D-08): окна расписания ставят
// узел группы тем же выбором, что и клик на «Прокси» (selectOpts), исходный выбор
// до первого захвата окном лежит в State.Live[arbiter] и возвращается по концу
// окна. Окна групп — State.Live[schedules]. Отдельной точки применения у арбитра
// нет.

// Ключи State.Live арбитра. В вход каталога и профиля Mihomo не входят
// (selectionLiveKeys).
const (
	// LiveKeySchedules — окна расписания: ID группы → список окон.
	LiveKeySchedules = "schedules"
	// LiveKeyArbiter — записи удержания: ID группы → исходный выбор и держатель.
	LiveKeyArbiter = "arbiter"
)

// Держатели группы в записи арбитра.
const (
	holderQuota    = "quota"
	holderSchedule = "schedule"
	holderOverride = "override"
)

// arbiterTick — период сверки окон с выбором групп.
const arbiterTick = 15 * time.Second

// ScheduleWindow — окно расписания ручной группы: дни недели и интервал «с–по» по
// часам роутера (D-08). From > To — окно через полночь: с From дня из Days до To
// следующего дня.
type ScheduleWindow struct {
	ID string `json:"id"`
	// Days — дни недели: 0 — понедельник … 6 — воскресенье.
	Days []int `json:"days"`
	// From, To — HH:MM.
	From string `json:"from"`
	To   string `json:"to"`
	// Member — участник группы на время окна ("node:<ref>", "group:<id>", …).
	Member  string `json:"member"`
	Enabled bool   `json:"enabled"`
}

// arbiterRecord — запись удержания группы: исходный выбор (Origin), к которому
// группа вернётся, и кто её держит сейчас.
type arbiterRecord struct {
	// Origin — выбор группы в Live до первого захвата ("" — выбора не было).
	Origin string `json:"origin"`
	// Holder — holderSchedule, holderOverride или holderQuota.
	Holder string `json:"holder"`
	// Window — ID окна, которое держит группу.
	Window   string `json:"window,omitempty"`
	Override string `json:"override,omitempty"`
	// Until — конец перехвата (unix, секунды).
	Until int64  `json:"until,omitempty"`
	Limit string `json:"limit,omitempty"`
}

// --- чтение и запись Live ---

// readSchedules читает окна групп. Порча живых данных окна не отменяет
// применённое: расписание просто не действует.
func readSchedules(live configlayer.Sections) map[string][]ScheduleWindow {
	out := map[string][]ScheduleWindow{}
	raw, ok := live[LiveKeySchedules]
	if !ok || isEmptyRaw(raw) {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string][]ScheduleWindow{}
	}
	return out
}

// writeSchedules записывает окна; группы без окон снимаются, пустая карта удаляет
// ключ.
func writeSchedules(live configlayer.Sections, m map[string][]ScheduleWindow) error {
	for gid, ws := range m {
		if len(ws) == 0 {
			delete(m, gid)
		}
	}
	if len(m) == 0 {
		delete(live, LiveKeySchedules)
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	live[LiveKeySchedules] = raw
	return nil
}

// readArbiter читает записи удержания групп.
func readArbiter(live configlayer.Sections) map[string]arbiterRecord {
	out := map[string]arbiterRecord{}
	raw, ok := live[LiveKeyArbiter]
	if !ok || isEmptyRaw(raw) {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]arbiterRecord{}
	}
	return out
}

// writeArbiter записывает записи удержания; пустая карта удаляет ключ.
func writeArbiter(live configlayer.Sections, m map[string]arbiterRecord) error {
	if len(m) == 0 {
		delete(live, LiveKeyArbiter)
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	live[LiveKeyArbiter] = raw
	return nil
}

// --- фоновая служба ---

// Arbiter — фоновая служба выбора ручных групп по окнам расписания. Раз в
// arbiterTick и по Kick сверяет фактический выбор в Live с тем, что положено
// сейчас, и ставит недостающее через selectArbiter (D-01). Создаётся и
// останавливается только в cmd/xcp/main.go.
type Arbiter struct {
	s *Service

	// lastErr — последняя записанная в журнал ошибка выбора по группе: повтор той
	// же ошибки каждый тик журнал не засоряет. Трогает только горутина run.
	lastErr map[string]string

	ctx     context.Context
	cancel  context.CancelFunc
	started atomic.Bool
	done    chan struct{}
	kick    chan struct{}
}

// NewArbiter создаёт арбитр и подключает его к сервису (Kick после правки окон).
func NewArbiter(s *Service) *Arbiter {
	ctx, cancel := context.WithCancel(context.Background())
	a := &Arbiter{s: s, lastErr: map[string]string{}, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), kick: make(chan struct{}, 1)}
	s.arb = a
	return a
}

// Start запускает цикл. Повторный вызов ничего не делает.
func (a *Arbiter) Start() {
	if !a.started.CompareAndSwap(false, true) {
		return
	}
	go a.run()
}

// Stop останавливает цикл и ждёт его выхода. Идемпотентен; без Start не ждёт.
func (a *Arbiter) Stop() {
	a.cancel()
	if a.started.Load() {
		<-a.done
	}
}

// Kick просит сверку вне очереди; не блокирует (сверка уже запрошена — ничего).
func (a *Arbiter) Kick() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// run — цикл: первая сверка сразу, затем по тикеру и по Kick.
func (a *Arbiter) run() {
	defer close(a.done)
	t := time.NewTicker(arbiterTick)
	defer t.Stop()
	for {
		if a.ctx.Err() != nil {
			return
		}
		a.tick()
		select {
		case <-a.ctx.Done():
			return
		case <-t.C:
		case <-a.kick:
		}
	}
}

// tick — одна сверка. Сравнение идёт с фактическим Live, а не по событиям
// перехода окна, поэтому повтор безопасен: пропущенный тик (выбор занят,
// перезапуск панели посреди окна) доводит следующий.
func (a *Arbiter) tick() {
	s := a.s
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	if !configlayer.DesiredRouting(st.Applied).AnyManaged() {
		return
	}
	scheds, recs := readSchedules(st.Live), readArbiter(st.Live)
	if len(scheds) == 0 && len(recs) == 0 {
		return
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return
	}
	now := s.d.Now()
	sel, unconfirmed := liveSelection(st.Live), readUnconfirmed(st.Live)
	ids := make([]string, 0, len(scheds)+len(recs))
	for gid := range scheds {
		ids = append(ids, gid)
	}
	for gid := range recs {
		if _, ok := scheds[gid]; !ok {
			ids = append(ids, gid)
		}
	}
	sort.Strings(ids)
	for _, gid := range ids {
		if a.ctx.Err() != nil {
			return
		}
		g, ok := am.group(gid)
		// Не ручная или удалённая группа арбитру не принадлежит (D-10); выбор с
		// маркером неизвестного исхода не трогается до «Повторить» или сверки.
		if !ok || g.Mode != ModeManual || unconfirmed[gid] != "" {
			continue
		}
		a.settle(g, am, sel[gid], scheds[gid], recs, now)
	}
}

// settle приводит выбор группы g к положенному сейчас: узел активного окна или,
// вне окон, исходный выбор из записи арбитра.
func (a *Arbiter) settle(g Group, am appliedModel, cur string, ws []ScheduleWindow, recs map[string]arbiterRecord, now time.Time) {
	s := a.s
	opts := map[string]bool{}
	for _, o := range appliedOptions(g, am) {
		opts[o.Member] = true
	}
	usable := func(m string) bool { return m != "" && opts[m] && am.avail(m) }
	rec, hasRec := recs[g.ID]
	w, _, active := activeWindow(ws, now)
	var desired string
	returning := false
	switch {
	case active && usable(w.Member):
		desired = w.Member
		if !hasRec || rec.Holder != holderSchedule || rec.Window != w.ID {
			// Первый захват запоминает выбор до окна; переход из окна в окно
			// сохраняет исходный выбор первого захвата (D-06). Запись — до выбора:
			// перезапуск панели посреди выбора не теряет исходный выбор.
			next := arbiterRecord{Origin: cur, Holder: holderSchedule, Window: w.ID}
			if hasRec {
				next.Origin = rec.Origin
			}
			if !a.putRecord(g.ID, next) {
				return
			}
		}
	case active:
		// Узел окна пропал из вариантов группы (D-07): выбор не меняется.
		return
	case hasRec:
		returning = true
		desired = rec.Origin
		if !usable(desired) {
			// Исходного выбора не было или он пропал: ставится выбор по умолчанию
			// явно (тот же участник, что ядро взяло бы без выбора).
			r := Resolve(g, am.union, am.groups.Items)
			desired = effectiveSelection(g, "", am.avail, nodeRefs(r.Nodes)...)
		}
	default:
		return
	}
	if desired != "" && desired != cur {
		res, err := s.selectArbiter(a.ctx, g.ID, desired)
		if err == nil && res.Reverted {
			err = errors.New("routing: выбор возвращён ядром")
		}
		if err != nil {
			a.logSelectErr(g.ID, err)
			return
		}
	}
	delete(a.lastErr, g.ID)
	if returning {
		a.dropRecord(g.ID, rec)
	}
}

// logSelectErr пишет в журнал ошибку выбора группы, кроме занятости выбора и
// применения (следующий тик повторит), не чаще одного раза на ту же ошибку.
func (a *Arbiter) logSelectErr(gid string, err error) {
	if errors.Is(err, ErrSelectBusy) || errors.Is(err, ErrApplyBusy) || errors.Is(err, ErrSelectionUnconfirmed) ||
		a.ctx.Err() != nil {
		return
	}
	if a.lastErr[gid] == err.Error() {
		return
	}
	a.lastErr[gid] = err.Error()
	log.Printf("[routing] арбитр: выбор группы %s не поставлен: %v", gid, err)
}

// putRecord записывает запись удержания группы и сообщает клиентам.
func (a *Arbiter) putRecord(gid string, rec arbiterRecord) bool {
	err := a.s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		m := readArbiter(live)
		m[gid] = rec
		return writeArbiter(live, m)
	})
	if err != nil {
		log.Printf("[routing] арбитр: запись удержания группы %s: %v", gid, err)
		return false
	}
	a.s.d.Layer.PublishProxies(configlayer.ProxiesKindStructure)
	return true
}

// dropRecord снимает запись удержания группы, если её никто не сменил после
// чтения (новое окно могло успеть захватить группу).
func (a *Arbiter) dropRecord(gid string, seen arbiterRecord) {
	changed := false
	err := a.s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		m := readArbiter(live)
		if cur, ok := m[gid]; !ok || cur != seen {
			return nil
		}
		delete(m, gid)
		changed = true
		return writeArbiter(live, m)
	})
	if err != nil {
		log.Printf("[routing] арбитр: снятие удержания группы %s: %v", gid, err)
		return
	}
	if changed {
		a.s.d.Layer.PublishProxies(configlayer.ProxiesKindStructure)
	}
}
