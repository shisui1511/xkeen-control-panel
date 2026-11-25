package routing

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Арбитр выбора ручных групп (SPX-01, D-01, D-04…D-07, D-14): один держатель
// группы по приоритету «лимит трафика > окно расписания (или его перехват) >
// ручной выбор». Узел ставится тем же выбором, что и клик на «Прокси»
// (selectArbiter), исходный выбор до первого захвата лежит в State.Live[arbiter] и
// возвращается, когда группу больше никто не держит. Окна групп —
// State.Live[schedules]. Отдельной точки применения у арбитра нет.

// Ключи State.Live арбитра. В вход каталога и профиля Mihomo не входят
// (selectionLiveKeys).
const (
	// LiveKeySchedules — окна расписания: ID группы → список окон.
	LiveKeySchedules = "schedules"
	// LiveKeyArbiter — записи удержания: ID группы → исходный выбор и держатель.
	LiveKeyArbiter = "arbiter"
)

// Держатели группы в записи арбитра и виды удержания в виде «Прокси».
const (
	holderQuota    = "quota"
	holderSchedule = "schedule"
	holderOverride = "override"
	// holdNodeMissing — действующее окно пропущено: его узла нет среди вариантов
	// группы (D-07). Держателем в записи не бывает.
	holdNodeMissing = "node_missing"
)

const (
	// arbiterTick — период сверки окон с выбором групп.
	arbiterTick = 15 * time.Second
	// selectLockWait — сколько клик пользователя ждёт замок выбора, пока тик арбитра
	// ставит выборы групп: дольше — отказ ErrSelectBusy вместо зависшего запроса.
	selectLockWait = 5 * time.Second
	// arbiterMinYear — часы роутера раньше этого года считаются несинхронизированными:
	// окна не применяются, удержание лимитом действует (A9).
	arbiterMinYear = 2025
	// noticeSchedulePrefix — уведомление арбитра по группе: routing_schedule:<gid>,
	// Reason — node_missing (узла окна нет) или stalled (ядро не приняло выбор).
	noticeSchedulePrefix = "routing_schedule:"
	noticeReasonStalled  = "stalled"
)

// ErrQuotaHeld — группу держит исчерпанный лимит трафика: клик по узлу отклоняется
// до сброса лимита (D-05).
var ErrQuotaHeld = errors.New("routing: группу держит лимит трафика")

// HoldSource — источник удержаний лимитами трафика (служба лимитов). Подключается
// в cmd/xcp/main.go через SetHoldSource; без источника лимиты группы не держат.
// Инвариант: в удержания попадают только лимиты с действием block или direct
// (services.holdingAction); decide всё равно не принимает удержание без служебного
// узла (quotaMember == ""), чтобы будущий источник не оставил группу без решения.
type HoldSource interface {
	Holds() services.LimitHolds
}

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
	// Window — ID окна, которое держит группу (или окна перехвата).
	Window string `json:"window,omitempty"`
	// Override — участник, выбранный пользователем поверх окна (D-04).
	Override string `json:"override,omitempty"`
	// Until — конец перехвата (unix, секунды): конец окна.
	Until int64 `json:"until,omitempty"`
	// Limit — ID лимита, который держит группу.
	Limit string `json:"limit,omitempty"`
	// Stalled — последний выбор арбитра для группы не принят ядром.
	Stalled bool `json:"stalled,omitempty"`
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

// --- решение ---

// clockTrusted — часы роутера похожи на синхронизированные (A9).
func clockTrusted(t time.Time) bool { return t.Year() >= arbiterMinYear }

// quotaMember — служебный вариант, на который лимит с действием action ставит
// группу; "" — действие выбор не держит.
func quotaMember(action string) string {
	switch action {
	case "block":
		return memberReject
	case "direct":
		return memberDirect
	}
	return ""
}

// holdFor — удержание ручной группы gid: свой исчерпанный лимит, иначе общий
// (D-14). Вызывается только для ручных групп.
func holdFor(h services.LimitHolds, gid string) *services.LimitHold {
	if x, ok := h.Groups[gid]; ok {
		return &x
	}
	return h.All
}

// decideInput — всё, от чего зависит держатель группы в момент now.
type decideInput struct {
	rec    arbiterRecord
	hasRec bool
	// hold — исчерпанный лимит, который держит группу; nil — нет.
	hold *services.LimitHold
	// clockOK — часы похожи на синхронизированные; иначе окон нет (hasWin=false).
	clockOK bool
	// win, winEnd, hasWin — действующее окно и его конец.
	win    ScheduleWindow
	winEnd time.Time
	hasWin bool
	// winAvail — узел окна среди вариантов группы и пригоден для выбора.
	winAvail bool
	// cur — выбор группы в Live ("" — выбора нет).
	cur string
	// def — выбор по умолчанию: ставится вместо пустого или пропавшего Origin.
	def string
	// usable — участник среди вариантов группы и пригоден для выбора.
	usable func(member string) bool
	now    time.Time
}

// decision — что арбитр делает с группой.
type decision struct {
	// member — желаемый участник; "" — выбор не менять.
	member string
	// rec и write — запись удержания, которую надо записать до выбора.
	rec   arbiterRecord
	write bool
	// drop — после поставленного выбора запись снимается (возврат к Origin).
	drop bool
	// kind — вид удержания: holderQuota, holderOverride, holderSchedule,
	// holdNodeMissing или "" (группу никто не держит).
	kind string
}

// decide — приоритет держателей группы (D-04…D-07, D-14), чистая функция:
//  1. Исчерпанный лимит — служебный REJECT (block) или DIRECT (direct); Origin
//     фиксируется при первом захвате, перехват окна в записи сохраняется: он
//     переживает срабатывание и сброс лимита внутри того же окна.
//  2. Действующее окно и живой перехват этого окна — узел перехвата.
//  3. Действующее окно с доступным узлом — узел окна; смена окна сбрасывает
//     перехват.
//  4. Действующее окно без узла (D-07) — окно пропускается: выбор не меняется;
//     запись, оставшаяся от лимита или прошлого окна, возвращает Origin.
//  5. Нет держателя, запись есть — Origin (пустой или пропавший — выбор по
//     умолчанию), после поставленного выбора запись снимается. При
//     несинхронизированных часах записи окон не трогаются.
func decide(in decideInput) decision {
	origin := in.cur
	if in.hasRec {
		origin = in.rec.Origin
	}
	if in.hold != nil {
		if m := quotaMember(in.hold.Action); m != "" {
			next := in.rec
			if !in.hasRec {
				next = arbiterRecord{Origin: in.cur}
			}
			next.Holder, next.Limit = holderQuota, in.hold.LimitID
			return decision{member: m, rec: next, write: !in.hasRec || next != in.rec, kind: holderQuota}
		}
	}
	switch {
	case in.hasWin && overrideLive(in):
		next := in.rec
		next.Holder, next.Limit = holderOverride, ""
		return decision{member: in.rec.Override, rec: next, write: next != in.rec, kind: holderOverride}
	case in.hasWin && in.winAvail:
		next := arbiterRecord{Origin: origin, Holder: holderSchedule, Window: in.win.ID, Stalled: in.rec.Stalled}
		return decision{member: in.win.Member, rec: next, write: !in.hasRec || next != in.rec, kind: holderSchedule}
	case in.hasWin:
		if in.hasRec && (in.rec.Holder != holderSchedule || in.rec.Window != in.win.ID) {
			d := returnOrigin(in)
			d.kind = holdNodeMissing
			return d
		}
		return decision{kind: holdNodeMissing}
	case !in.clockOK && in.hasRec && in.rec.Holder != holderQuota:
		return decision{}
	case in.hasRec:
		return returnOrigin(in)
	}
	return decision{}
}

// overrideLive — запись держит перехват действующего окна, срок не вышел и узел
// перехвата пригоден.
func overrideLive(in decideInput) bool {
	r := in.rec
	return in.hasRec && (r.Holder == holderOverride || r.Holder == holderQuota) &&
		r.Window == in.win.ID && r.Override != "" && in.now.Unix() < r.Until && in.usable(r.Override)
}

// returnOrigin — возврат к исходному выбору с последующим снятием записи.
func returnOrigin(in decideInput) decision {
	m := in.rec.Origin
	if !in.usable(m) {
		m = in.def
	}
	return decision{member: m, rec: in.rec, drop: true}
}

// arbiterInput собирает вход decide для ручной группы g. Варианты группы
// считаются лениво: группе без окон и записи они не нужны.
func arbiterInput(g Group, am appliedModel, cur string, ws []ScheduleWindow, rec arbiterRecord, hasRec bool, hold *services.LimitHold, now time.Time) decideInput {
	in := decideInput{rec: rec, hasRec: hasRec, hold: hold, cur: cur, now: now, clockOK: clockTrusted(now)}
	var opts map[string]bool
	in.usable = func(m string) bool {
		if m == "" {
			return false
		}
		if opts == nil {
			opts = map[string]bool{}
			for _, o := range appliedOptions(g, am) {
				opts[o.Member] = true
			}
		}
		return opts[m] && am.avail(m)
	}
	if in.clockOK {
		in.win, in.winEnd, in.hasWin = activeWindow(ws, now)
		if in.hasWin {
			in.winAvail = in.usable(in.win.Member)
		}
	}
	if hasRec {
		r := Resolve(g, am.union, am.groups.Items)
		in.def = effectiveSelection(g, "", am.avail, nodeRefs(r.Nodes)...)
	}
	return in
}

// runtimeHold — удержание группы для вида «Прокси». Лимит виден сразу (клик уже
// отклоняется), окно и перехват — когда запись арбитра их отражает (выбор
// поставлен или ставится); пропущенное окно — всегда. Отказ ядра при возврате к
// исходному выбору виден по записи с Stalled.
func runtimeHold(in decideInput, d decision) *RuntimeHold {
	stalled := in.hasRec && in.rec.Stalled
	switch d.kind {
	case holderQuota:
		return &RuntimeHold{Kind: holderQuota, Limit: in.hold.LimitID, LimitName: in.hold.Name,
			ResetAt: in.hold.ResetAt, Stalled: stalled}
	case holderOverride:
		if in.hasRec && in.rec.Holder == holderOverride {
			return &RuntimeHold{Kind: holderOverride, Until: in.rec.Until, Window: in.rec.Window,
				Member: in.rec.Override, Origin: in.rec.Origin, Stalled: stalled}
		}
	case holderSchedule:
		if in.hasRec && in.rec.Holder == holderSchedule && in.rec.Window == in.win.ID {
			return &RuntimeHold{Kind: holderSchedule, Until: in.winEnd.Unix(), Window: in.win.ID,
				Member: in.win.Member, Origin: in.rec.Origin, Stalled: stalled}
		}
	case holdNodeMissing:
		return &RuntimeHold{Kind: holdNodeMissing, Until: in.winEnd.Unix(), Window: in.win.ID, Member: in.win.Member}
	}
	if stalled {
		return &RuntimeHold{Kind: in.rec.Holder, Window: in.rec.Window, Limit: in.rec.Limit, Stalled: true}
	}
	return nil
}

// limitHolds — снимок удержаний лимитами (без арбитра или источника — пусто).
func (s *Service) limitHolds() services.LimitHolds {
	if s.arb == nil {
		return services.LimitHolds{}
	}
	return s.arb.holdsSnapshot()
}

// lockArbiter упорядочивает выбор пользователя и «Вернуть расписание» с тиком
// арбитра: запись удержания и выбор не перемежаются. Без арбитра — ничего. Тик
// держит замок на весь проход по группам, а синхронный выбор может занять десятки
// секунд, поэтому ожидание ограничено отменой запроса и selectLockWait: вместо
// зависшего запроса клиент получает ErrSelectBusy и повторяет клик.
func (s *Service) lockArbiter(ctx context.Context) (func(), error) {
	if s.arb == nil {
		return func() {}, nil
	}
	sem := s.arb.sel
	timer := time.NewTimer(selectLockWait)
	defer timer.Stop()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ErrSelectBusy
	case <-timer.C:
		return nil, ErrSelectBusy
	}
}

// --- фоновая служба ---

// Arbiter — фоновая служба выбора ручных групп. Раз в arbiterTick и по Kick
// сверяет фактический выбор в Live с тем, что положено сейчас (decide), и ставит
// недостающее через selectArbiter (D-01). Создаётся и останавливается только в
// cmd/xcp/main.go.
type Arbiter struct {
	s *Service

	// mu охраняет holds.
	mu    sync.Mutex
	holds HoldSource
	// sel — замок выбора ёмкостью 1: упорядочивает тик, клик пользователя (Select) и
	// RestoreSchedule. Канал, а не sync.Mutex: ожидание клика отменяется контекстом
	// запроса и ограничено по времени (lockArbiter).
	sel chan struct{}

	// lastErr — последняя записанная в журнал ошибка выбора по группе: повтор той
	// же ошибки каждый тик журнал не засоряет. noted — причина уведомления
	// routing_schedule по группе. Трогает только тик (под замком выбора).
	lastErr map[string]string
	noted   map[string]string

	// tail — пауза повтора сверки служебного хвоста Mihomo (arbiter_tail.go).
	tail tailRetry

	ctx     context.Context
	cancel  context.CancelFunc
	started atomic.Bool
	done    chan struct{}
	kick    chan struct{}
}

// NewArbiter создаёт арбитр и подключает его к сервису (Kick после правки окон,
// проверка клика в Select).
func NewArbiter(s *Service) *Arbiter {
	ctx, cancel := context.WithCancel(context.Background())
	a := &Arbiter{s: s, lastErr: map[string]string{}, noted: map[string]string{}, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), kick: make(chan struct{}, 1), sel: make(chan struct{}, 1)}
	s.arb = a
	return a
}

// SetHoldSource подключает источник удержаний лимитами; nil — лимитов нет.
func (a *Arbiter) SetHoldSource(h HoldSource) {
	a.mu.Lock()
	a.holds = h
	a.mu.Unlock()
}

// holdsSnapshot — удержания лимитами сейчас; без источника — пусто.
func (a *Arbiter) holdsSnapshot() services.LimitHolds {
	a.mu.Lock()
	h := a.holds
	a.mu.Unlock()
	if h == nil {
		return services.LimitHolds{}
	}
	return h.Holds()
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
	if a == nil {
		return
	}
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

// kickAfterApply — Kick после успешного применения любого вида со снятием паузы
// сверки служебного хвоста: причина отказа могла уйти (дрейф решён, проверка
// проходит). Не блокирует; nil-безопасен.
func (a *Arbiter) kickAfterApply() {
	if a == nil {
		return
	}
	a.tail.reset()
	a.Kick()
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
		a.tick(a.ctx)
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
// перезапуск панели посреди окна) доводит следующий. Первым делом — сверка
// служебного хвоста профиля Mihomo (syncTail, D-13): до проверки AnyManaged и до
// решений по группам, без замка выбора (клик пользователя не ждёт конвейер). Решения
// читают состояние заново под замком выбора, то есть уже после сверки: selectArbiter идёт
// по новому профилю.
func (a *Arbiter) tick(ctx context.Context) {
	s := a.s
	if st, err := s.d.Layer.ReadState(); err == nil {
		a.syncTail(ctx, st)
	}
	select {
	case a.sel <- struct{}{}:
		defer func() { <-a.sel }()
	case <-ctx.Done():
		return
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	seen := map[string]bool{}
	defer a.pruneNotes(seen)
	if !configlayer.DesiredRouting(st.Applied).AnyManaged() {
		return
	}
	holds := a.holdsSnapshot()
	scheds, recs := readSchedules(st.Live), readArbiter(st.Live)
	if len(scheds) == 0 && len(recs) == 0 && len(holds.Groups) == 0 && holds.All == nil {
		return
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		// Без модели решать нечего: уведомления групп остаются как есть.
		for gid := range a.noted {
			seen[gid] = true
		}
		return
	}
	now := s.d.Now()
	sel, unconfirmed := liveSelection(st.Live), readUnconfirmed(st.Live)
	for _, g := range am.groups.Items {
		if ctx.Err() != nil {
			for gid := range a.noted {
				seen[gid] = true
			}
			return
		}
		// Не ручная группа арбитру не принадлежит (D-10); её записи снимает
		// CommitLive при «Применить».
		if g.Mode != ModeManual {
			continue
		}
		seen[g.ID] = true
		// Выбор с маркером неизвестного исхода не трогается до «Повторить» или
		// сверки.
		if unconfirmed[g.ID] != "" {
			continue
		}
		rec, hasRec := recs[g.ID]
		hold := holdFor(holds, g.ID)
		if !hasRec && len(scheds[g.ID]) == 0 && hold == nil {
			a.setNote(g, "", nil)
			continue
		}
		in := arbiterInput(g, am, sel[g.ID], scheds[g.ID], rec, hasRec, hold, now)
		a.settle(ctx, g, in, decide(in))
	}
}

// settle выполняет решение decide для группы g: запись удержания пишется до
// выбора (перезапуск панели посреди выбора не теряет исходный выбор), выбор —
// входом арбитра; возврат к Origin снимает запись после поставленного выбора.
func (a *Arbiter) settle(ctx context.Context, g Group, in decideInput, d decision) {
	s := a.s
	seen := in.rec
	if d.write {
		if !a.putRecord(g.ID, d.rec) {
			return
		}
		seen = d.rec
	}
	if d.member == "" {
		if d.kind == holdNodeMissing {
			a.setNote(g, holdNodeMissing, map[string]string{"window": in.win.ID})
		} else {
			a.setNote(g, "", nil)
		}
		return
	}
	if d.member != in.cur {
		res, err := s.selectArbiter(ctx, g.ID, d.member)
		if errors.Is(err, ErrSelectBusy) || errors.Is(err, ErrApplyBusy) ||
			errors.Is(err, ErrSelectionUnconfirmed) || ctx.Err() != nil {
			// Занято или остановка: следующий тик повторит.
			return
		}
		failed := err != nil || res.Reverted
		for _, out := range res.Kernels {
			if out == selFailed {
				failed = true
			}
		}
		if failed {
			if err == nil {
				err = errors.New("routing: ядро не приняло выбор")
			}
			a.logSelectErr(g.ID, err)
			a.setStalled(g.ID, seen, true)
			a.setNote(g, noticeReasonStalled, nil)
			return
		}
		if len(res.Unconfirmed) > 0 {
			// Исход неизвестен: маркер стоит, группа ждёт «Повторить» или сверку.
			return
		}
	}
	delete(a.lastErr, g.ID)
	if d.drop {
		a.dropRecord(g.ID, seen)
	} else if seen.Stalled {
		a.setStalled(g.ID, seen, false)
	}
	if d.kind == holdNodeMissing {
		a.setNote(g, holdNodeMissing, map[string]string{"window": in.win.ID})
	} else {
		a.setNote(g, "", nil)
	}
}

// logSelectErr пишет в журнал ошибку выбора группы не чаще одного раза на ту же
// ошибку. В журнал идут только ID группы и текст ошибки, без имён узлов.
func (a *Arbiter) logSelectErr(gid string, err error) {
	if a.lastErr[gid] == err.Error() {
		return
	}
	a.lastErr[gid] = err.Error()
	log.Printf("[routing] арбитр: выбор группы %s не поставлен: %v", gid, err)
}

// setNote ставит (reason != "") или снимает уведомление routing_schedule:<gid>.
// Та же причина повторно не ставится: закрытое пользователем уведомление не
// возвращается каждый тик.
func (a *Arbiter) setNote(g Group, reason string, params map[string]string) {
	if a.noted[g.ID] == reason {
		return
	}
	if reason == "" {
		delete(a.noted, g.ID)
		a.s.schedNotes.drop(a.s.d.Layer, g.ID)
		return
	}
	a.noted[g.ID] = reason
	p := map[string]string{"group": g.Name}
	for k, v := range params {
		p[k] = v
	}
	a.s.schedNotes.put(a.s.d.Layer, g.ID, configlayer.NoticeView{
		ID: noticeSchedulePrefix + g.ID, Kind: noticeWarning, Reason: reason, Params: p,
	})
}

// pruneNotes снимает уведомления групп, которых тик не видел (удалены или
// больше не ручные).
func (a *Arbiter) pruneNotes(seen map[string]bool) {
	for gid := range a.noted {
		if !seen[gid] {
			delete(a.noted, gid)
			a.s.schedNotes.drop(a.s.d.Layer, gid)
		}
	}
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

// setStalled ставит или снимает признак отказа ядра в записи группы, если её
// никто не сменил после чтения; сообщает, что запись изменена.
func (a *Arbiter) setStalled(gid string, seen arbiterRecord, v bool) bool {
	changed := false
	err := a.s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
		m := readArbiter(live)
		cur, ok := m[gid]
		if !ok || cur != seen || cur.Stalled == v {
			return nil
		}
		cur.Stalled = v
		m[gid] = cur
		changed = true
		return writeArbiter(live, m)
	})
	if err != nil {
		log.Printf("[routing] арбитр: признак отказа группы %s: %v", gid, err)
		return false
	}
	if changed {
		a.s.d.Layer.PublishProxies(configlayer.ProxiesKindStructure)
	}
	return changed
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

// --- клик пользователя ---

// beforeUserSelect — проверка клика пользователя по участнику member группы gid
// до выбора (D-04, D-05). Держит лимит — ErrQuotaHeld без обращения к ядру.
// Группу держит действующее окно (или окно, чей узел пропал после захвата) —
// клик становится перехватом до конца окна (holder=override, Until — конец окна,
// Origin не меняется); клик по узлу окна снимает перехват. Окна нет — запись,
// оставшаяся от лимита или прошлого окна, снимается: ручной выбор пользователя
// следующий тик не откатывает. undo возвращает прежнюю запись, если её никто не
// сменил; вызывается при неудаче выбора. Вызывается под замком выбора.
func (a *Arbiter) beforeUserSelect(gid, member string) (undo func(), err error) {
	s := a.s
	noop := func() {}
	st, err := s.d.Layer.ReadState()
	if err != nil || !configlayer.DesiredRouting(st.Applied).AnyManaged() {
		// Ошибку выбора отдаст selectOpts.
		return noop, nil
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return noop, nil
	}
	g, ok := am.group(gid)
	if !ok || g.Mode != ModeManual {
		return noop, nil
	}
	cur := liveSelection(st.Live)[gid]
	rec, hasRec := readArbiter(st.Live)[gid]
	in := arbiterInput(g, am, cur, readSchedules(st.Live)[gid], rec, hasRec, holdFor(a.holdsSnapshot(), gid), s.d.Now())
	if decide(in).kind == holderQuota {
		return nil, ErrQuotaHeld
	}
	origin := cur
	if hasRec {
		origin = rec.Origin
	}
	var next arbiterRecord
	keep := false
	switch {
	case in.hasWin && (in.winAvail || (hasRec && rec.Window == in.win.ID)):
		keep = true
		next = arbiterRecord{Origin: origin, Holder: holderSchedule, Window: in.win.ID}
		if member != in.win.Member {
			next.Holder, next.Override, next.Until = holderOverride, member, in.winEnd.Unix()
		}
		if hasRec && next == rec {
			return noop, nil
		}
	case !hasRec:
		return noop, nil
	}
	write := func(from arbiterRecord, fromOK bool, to arbiterRecord, toOK bool) bool {
		changed := false
		err := s.d.Layer.UpdateLive(func(live configlayer.Sections) error {
			m := readArbiter(live)
			if x, ok := m[gid]; ok != fromOK || (ok && x != from) {
				return nil
			}
			if toOK {
				m[gid] = to
			} else {
				delete(m, gid)
			}
			changed = true
			return writeArbiter(live, m)
		})
		if err != nil {
			log.Printf("[routing] арбитр: запись удержания группы %s: %v", gid, err)
			return false
		}
		if changed {
			s.d.Layer.PublishProxies(configlayer.ProxiesKindStructure)
		}
		return changed
	}
	if !write(rec, hasRec, next, keep) {
		return noop, nil
	}
	return func() { write(next, keep, rec, hasRec) }, nil
}
