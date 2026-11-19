// Package setupwizard — служба мастера первого запуска (фаза 149.1): один
// серверный расчёт шагов 1–6 для страницы мастера и карточки дашборда, признак
// автооткрытия и видимости карточки, отметка «пройдено» без браузера.
//
// Служба создаётся только в cmd/xcp/main.go, Stop вызывается там же через defer.
// Чтение вида (View) ничего не пишет: признак мастера ставят наблюдатель, хук
// после применения и POST-маршруты.
package setupwizard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Идентификаторы шагов в порядке мастера.
const (
	StepKernel   = "kernel"
	StepSections = "sections"
	StepNodes    = "nodes"
	StepRouting  = "routing"
	StepApply    = "apply"
	StepStart    = "start"
	// TotalSteps — шагов в прогрессе мастера и карточки; проверка IP (шаг 7) в
	// прогресс не входит (D-05).
	TotalSteps = 6
)

// KernelState — состояние ядер для шага 6: активное ядро, запущенные процессы и
// конфликт (запущены оба).
type KernelState struct {
	// Kernel — "xray" | "mihomo" | "none"; при конфликте пуст.
	Kernel string
	// Running — запущенные ядра.
	Running []string
	// Conflict — запущены оба ядра.
	Conflict bool
}

// Deps — зависимости службы; создаются в cmd/xcp/main.go.
type Deps struct {
	// Layer — слой «Конфигурация»: признак мастера и факты шагов 2–5.
	Layer *configlayer.Layer
	// Installed — установленные ядра.
	Installed func() configlayer.InstalledKernels
	// XKeenInstalled — бинарник XKeen есть на диске.
	XKeenInstalled func() bool
	// Kernels — активное ядро и запущенные процессы (кэш службы ядер).
	Kernels func() KernelState
	// XrayTakeable — число секций Xray, которые ещё можно взять в черновик (не в
	// черновике и без блокировки); false — посчитать не удалось. Необязательна:
	// без неё пометки «частично» нет. Зовётся только чтением вида (GET) — она
	// читает вид секций ядра, для тика наблюдателя это дорого.
	XrayTakeable func() (int, bool)
	// Interval — тик наблюдателя; 0 — defaultInterval.
	Interval time.Duration
}

// View — ответ GET /api/configlayer/wizard. Только счётчики, коды и
// идентификаторы пресета и страны: URL подписок, имён узлов и адресов нет.
type View struct {
	// Status — признак мастера: "" | "closed" | "done" | "hidden".
	Status string `json:"status"`
	// Empty — набор секций черновика пуст: ни источника, ни секции Xray под
	// панелью, маршрутизация не взята (149 D-22).
	Empty bool `json:"empty"`
	// AutoOpen — мастер открывается сам при входе в раздел: статус пуст и
	// состояние полностью пустое (149 D-22).
	AutoOpen bool `json:"auto_open"`
	// CardVisible — карточка «Настроить VPN» видна: статус не done и не hidden и
	// не все шаги 1–6 выполнены (D-06, D-10, D-11).
	CardVisible bool `json:"card_visible"`
	// DoneCount — выполненные шаги 1–6.
	DoneCount int `json:"done_count"`
	// Total — шагов в прогрессе (TotalSteps).
	Total int `json:"total"`
	// Current — номер (1…6) первого невыполненного шага; 0 — выполнены все.
	Current int         `json:"current"`
	Kernels KernelsView `json:"kernels"`
	Steps   []Step      `json:"steps"`
	// TickS — период проверки шагов наблюдателем, с. По нему клиенты и роутерные
	// проверки считают ожидание отметки «пройдено» и обновления вида: срок у
	// процесса панели, как total_s у окна DNS.
	TickS int `json:"tick_s"`
}

// KernelsView — установленность XKeen и ядер, запущенные ядра и конфликт.
// Активное ядро клиент берёт из своих сторов: здесь его нет.
type KernelsView struct {
	XKeen    bool     `json:"xkeen"`
	Xray     bool     `json:"xray"`
	Mihomo   bool     `json:"mihomo"`
	Running  []string `json:"running"`
	Conflict bool     `json:"conflict"`
}

// Step — один шаг мастера и его счётчики.
type Step struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
	// Kernels — шаг «секции»: взято ли что-то у каждого установленного ядра.
	Kernels map[string]KernelTake `json:"kernels,omitempty"`
	// Partial — шаг «секции»: у Xray взята часть незаблокированных секций.
	Partial *Partial `json:"partial,omitempty"`
	// Nodes и Sources — шаг «узлы»: рабочие узлы и источники черновика.
	Nodes   *int `json:"nodes,omitempty"`
	Sources *int `json:"sources,omitempty"`
	// Taken, Preset, Country — шаг «маршрутизация»: панель держит маршрутизацию,
	// выбранный пресет и страна пресета.
	Taken   *bool  `json:"taken,omitempty"`
	Preset  string `json:"preset,omitempty"`
	Country string `json:"country,omitempty"`
	// DraftChanges, AppliedEmpty, ApplyRunning — шаг «применение».
	DraftChanges *int  `json:"draft_changes,omitempty"`
	AppliedEmpty *bool `json:"applied_empty,omitempty"`
	ApplyRunning *bool `json:"apply_running,omitempty"`
}

// KernelTake — взято ли что-то у ядра в шаге «секции».
type KernelTake struct {
	Done bool `json:"done"`
}

// Partial — «частично, N из M»: N секций Xray в черновике из M = N + ещё можно
// взять.
type Partial struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// wizardWriteWait — сколько ответ POST close|hide|reset ждёт записи признака.
// Запись ждёт свою очередь и замок хранилища, который держат синхронные записи
// слоя с fsync (применение, журнал): на флешке роутера под чужой записью это
// секунды. Не дождались — ответ несёт статус с учётом этой записи, запись
// завершается сама, итог публикует событие wizard (G-149.1-6).
const wizardWriteWait = time.Second

// writeOp — запись признака мастера из POST-маршрута.
type writeOp int

const (
	// writeClose — «Закрыть» (closed).
	writeClose writeOp = iota
	// writeHide — «Скрыть» (hidden).
	writeHide
	// writeReset — сброс признака (режим разработчика).
	writeReset
)

// status — признак, который ставит запись; у сброса — пустой.
func (op writeOp) status() configlayer.WizardStatus {
	switch op {
	case writeClose:
		return configlayer.WizardClosed
	case writeHide:
		return configlayer.WizardHidden
	}
	return ""
}

// String — имя записи для журнала.
func (op writeOp) String() string {
	switch op {
	case writeClose:
		return "close"
	case writeHide:
		return "hide"
	}
	return "reset"
}

// rank — ранг признака для свёртки статуса: пусто — 0, closed — 1, done и
// hidden — 2 (как монотонная запись слоя).
func rank(s configlayer.WizardStatus) int {
	switch s {
	case configlayer.WizardClosed:
		return 1
	case configlayer.WizardDone, configlayer.WizardHidden:
		return 2
	}
	return 0
}

// apply — статус после записи op поверх cur: сброс — пустой, close и hide —
// запрошенный, если его ранг выше.
func (op writeOp) apply(cur configlayer.WizardStatus) configlayer.WizardStatus {
	if op == writeReset {
		return ""
	}
	if st := op.status(); rank(st) > rank(cur) {
		return st
	}
	return cur
}

// pendingWrite — запись в очереди службы, ещё не выполненная.
type pendingWrite struct {
	id uint64
	op writeOp
}

// writeResult — итог записи признака: статус после неё и ошибка.
type writeResult struct {
	status configlayer.WizardStatus
	err    error
}

// writeCall — передача итога записи вызывающему: тот, кто перестал ждать
// (abandoned), итог не получает — ошибку пишет в журнал сама запись.
type writeCall struct {
	mu        sync.Mutex
	abandoned bool
	res       chan writeResult
}

// observation — одно наблюдение: факты слоя, установленность и состояние ядер.
type observation struct {
	// seq — номер начала наблюдения (часы службы clock).
	seq   uint64
	inst  configlayer.InstalledKernels
	xkeen bool
	facts configlayer.WizardFacts
	ks    KernelState
	// take — счёт секций Xray, которые ещё можно взять, из кэша службы.
	take takeCount
}

// takeCount — счёт секций Xray, которые ещё можно взять, парой с числом взятых
// секций Xray того же наблюдения: пометка «частично» показывается, только если
// число взятых в виде совпадает с forTaken — счёт и число взятых из разных
// наблюдений не смешиваются.
type takeCount struct {
	seq      uint64
	known    bool
	n        int
	forTaken int
}

// viewWait — сколько GET мастера ждёт наблюдения. Факты слоя ждут замок
// хранилища, который держат синхронные записи слоя с fsync (применение, журнал),
// а счёт секций читает файлы Xray — под нагрузкой на носитель это секунды. В
// простое чтение быстрее секунды, и вид свежий; не дождались — последнее
// наблюдение, дочитанное позже отличие публикуется событием wizard (G-149.1-6).
const viewWait = time.Second

// slowViewLogEvery — не чаще раза за этот срок пишется строка о долгом чтении
// вида: при взятых секциях Xray оно медленное постоянно, а смоук считает
// повторы чаще раза в минуту сбоем.
const slowViewLogEvery = 5 * time.Minute

// readCall — одно чтение вида: done закрывается по его завершении; served —
// ключи видов, отданных из кэша, пока оно шло (отличие от них публикуется).
type readCall struct {
	done   chan struct{}
	served []string
}

// readTimes — длительности частей наблюдения для строк журнала.
type readTimes struct {
	inst, facts, kernels, take time.Duration
}

// Service — служба мастера первого запуска.
type Service struct {
	d Deps

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
	started  atomic.Bool
	slowLog  atomic.Int64 // UnixNano последней строки о долгом чтении вида
	done     chan struct{}

	// tickMu — только запись признака (WR-03): done тика по фактам слоя,
	// перечитанным под ним; closed, hidden и сброс — через очередь submitWrite.
	// Ядра под ним не перечитываются: сброс и «Закрыть» ядро не меняют, гарантию
	// WR-03 дают факты слоя, перечитанные под замком записи. Ответ POST его не
	// ждёт дольше wizardWriteWait, проверки наблюдателя его не держат на чтениях
	// (G-149.1-6).
	tickMu sync.Mutex

	// checkMu — одна проверка шагов за раз (тик наблюдателя и хук после
	// применения); под ним же подпись вектора шагов прошлой проверки.
	checkMu sync.Mutex
	lastSig string

	// cacheMu — кэш наблюдения, чтения вида и статус службы (не tickMu: ответы
	// POST и GET читают их, не дожидаясь записи признака).
	cacheMu sync.Mutex
	// reading — идущее чтение вида; nextRead — следующее, общее для всех GET,
	// пришедших за время идущего (начнётся после их прихода, поэтому свежее).
	reading  *readCall
	nextRead *readCall
	// clock — часы службы: номер начала наблюдения и завершения записи.
	clock uint64
	// obs — последнее наблюдение (nil — ещё не было); take — последний счёт
	// секций Xray, которые можно взять.
	obs  *observation
	take takeCount
	// base — последний известный статус (из результата записи или из
	// наблюдения); baseSet — он задан. Не задан — статус фактов наблюдения.
	base    configlayer.WizardStatus
	baseSet bool
	// lastWriteAt — часы завершения последней записи: наблюдение, начатое
	// раньше, базу не меняет.
	lastWriteAt uint64
	// pending — записи в очереди, ещё не выполненные (порядок поступления);
	// writeID — номер последней поставленной; writeTail закрывается по
	// завершении последней поставленной записи (следующая ждёт его).
	pending   []pendingWrite
	writeID   uint64
	writeTail chan struct{}
}

// New проверяет обязательные зависимости и создаёт службу. Фоновой работы New
// не начинает.
func New(d Deps) (*Service, error) {
	switch {
	case d.Layer == nil:
		return nil, errors.New("setupwizard: не задан слой конфигурации")
	case d.Installed == nil:
		return nil, errors.New("setupwizard: не задана установленность ядер")
	case d.XKeenInstalled == nil:
		return nil, errors.New("setupwizard: не задана установленность XKeen")
	case d.Kernels == nil:
		return nil, errors.New("setupwizard: не задано состояние ядер")
	}
	ctx, cancel := context.WithCancel(context.Background())
	tail := make(chan struct{})
	close(tail)
	return &Service{d: d, ctx: ctx, cancel: cancel, done: make(chan struct{}), writeTail: tail}, nil
}

// evaluation — итог расчёта шагов 1–6.
type evaluation struct {
	steps     []Step
	doneCount int
	current   int
}

// probe — пробник счётчиков источников для фактов слоя.
func probe(inst configlayer.InstalledKernels) configlayer.WizardProbe {
	return configlayer.WizardProbe{Nodes: func(s, live configlayer.Sections) (int, int) {
		return sources.CountUsableNodes(s, live, inst)
	}}
}

// rulesFacts — пресет, страна и число правил секции rules черновика; секции нет
// или она испорчена — пусто и 0.
func rulesFacts(raw json.RawMessage) (preset, country string, items int) {
	if len(raw) == 0 {
		return "", "", 0
	}
	var rc routing.RulesConfig
	if err := json.Unmarshal(raw, &rc); err != nil {
		return "", "", 0
	}
	if rc.Preset != nil {
		preset, country = rc.Preset.ID, rc.Preset.Country
	}
	return preset, country, len(rc.Items)
}

// evaluate считает шаги 1–6 по фактам слоя, установленности и состоянию ядер
// (D-04). Ничего не пишет; пометку «частично» не считает — её добавляет View.
func evaluate(f configlayer.WizardFacts, inst configlayer.InstalledKernels, xkeen bool, ks KernelState) evaluation {
	anyKernel := inst.Xray || inst.Mihomo

	// Шаг 1: XKeen и хотя бы одно ядро.
	kernel := Step{ID: StepKernel, Done: xkeen && anyKernel}

	// Шаг 2: у каждого установленного ядра взято: Xray — хотя бы одна секция в
	// черновике, Mihomo — маршрутизация (профиль панели).
	takes := map[string]KernelTake{}
	if inst.Xray {
		takes[configlayer.KernelXray] = KernelTake{Done: f.XrayTaken >= 1}
	}
	if inst.Mihomo {
		takes[configlayer.KernelMihomo] = KernelTake{Done: f.RoutingMihomo}
	}
	sectionsDone := anyKernel
	for _, t := range takes {
		sectionsDone = sectionsDone && t.Done
	}
	sections := Step{ID: StepSections, Done: sectionsDone, Kernels: takes}

	// Шаг 3: хотя бы один рабочий узел; источник без скачанного набора узлов не
	// даёт (D-04.3).
	nodes, srcs := f.Nodes, f.Sources
	nodesStep := Step{ID: StepNodes, Done: nodes >= 1, Nodes: &nodes, Sources: &srcs}

	// Шаг 4: маршрутизация взята и выбран пресет или есть правила: закрытие
	// диалога пресета без выбора шаг не закрывает (D-04.4, A1).
	preset, country, items := rulesFacts(f.Rules)
	taken := f.RoutingAny
	routingStep := Step{
		ID:      StepRouting,
		Done:    taken && (preset != "" || items > 0),
		Taken:   &taken,
		Preset:  preset,
		Country: country,
	}

	// Шаг 5: неприменённых изменений нет, применение не идёт, применённое не
	// пусто (D-04.5).
	changes, appliedEmpty, running := f.DraftChanges, f.AppliedEmpty, f.ApplyRunning
	apply := Step{
		ID:           StepApply,
		Done:         changes == 0 && !running && !appliedEmpty,
		DraftChanges: &changes,
		AppliedEmpty: &appliedEmpty,
		ApplyRunning: &running,
	}

	// Шаг 6: активное ядро запущено и конфликта нет (D-04.6, D-15).
	startDone := !ks.Conflict &&
		(ks.Kernel == configlayer.KernelXray || ks.Kernel == configlayer.KernelMihomo) &&
		slices.Contains(ks.Running, ks.Kernel)
	start := Step{ID: StepStart, Done: startDone}

	ev := evaluation{steps: []Step{kernel, sections, nodesStep, routingStep, apply, start}}
	for i, st := range ev.steps {
		if st.Done {
			ev.doneCount++
		} else if ev.current == 0 {
			ev.current = i + 1
		}
	}
	return ev
}

// interval — период проверки шагов наблюдателем.
func (s *Service) interval() time.Duration {
	if s.d.Interval > 0 {
		return s.d.Interval
	}
	return defaultInterval
}

// startSeq — номер начала наблюдения с часов службы.
func (s *Service) startSeq() uint64 {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.clock++
	return s.clock
}

// readObservation — одно наблюдение для вида: установленность, факты слоя,
// состояние ядер и счёт секций Xray, которые ещё можно взять; каждая часть с
// замером. Номер начала берётся с часов службы до первого чтения.
func (s *Service) readObservation() (observation, readTimes) {
	o := observation{seq: s.startSeq()}
	var rt readTimes

	t := time.Now()
	o.inst = s.d.Installed()
	o.xkeen = s.d.XKeenInstalled()
	rt.inst = time.Since(t)

	t = time.Now()
	o.facts = s.d.Layer.WizardFacts(probe(o.inst))
	rt.facts = time.Since(t)

	t = time.Now()
	o.ks = s.d.Kernels()
	rt.kernels = time.Since(t)

	// Счёт не нужен (Xray не установлен или секций не взято) — прежний счёт в
	// кэше сбрасывается этим же наблюдением.
	o.take = takeCount{seq: o.seq}
	if o.inst.Xray && o.facts.XrayTaken >= 1 && s.d.XrayTakeable != nil {
		t = time.Now()
		n, ok := s.d.XrayTakeable()
		rt.take = time.Since(t)
		o.take = takeCount{seq: o.seq, known: ok, n: n, forTaken: o.facts.XrayTaken}
	}
	return o, rt
}

// observe ждёт наблюдения не дольше wait (0 — без предела): не больше одного
// чтения в работе и одного следующего. Чтения нет — запускается; чтение идёт —
// GET записывается на следующее, общее для всех GET, пришедших за время
// текущего. Не дождались — последнее наблюдение и false. Кэш пуст (панель только
// стартовала) — ждать без предела.
func (s *Service) observe(wait time.Duration) (observation, bool) {
	s.cacheMu.Lock()
	var c *readCall
	switch {
	case s.reading == nil:
		c = &readCall{done: make(chan struct{})}
		s.reading = c
		go s.runRead(c)
	case s.nextRead == nil:
		c = &readCall{done: make(chan struct{})}
		s.nextRead = c
	default:
		c = s.nextRead
	}
	hasCache := s.obs != nil
	s.cacheMu.Unlock()

	if wait > 0 && hasCache {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-c.done:
		case <-timer.C:
			o, _ := s.cached()
			return o, false
		}
	} else {
		<-c.done
	}
	o, _ := s.cached()
	return o, true
}

// slowViewDue отвечает, пора ли писать строку о долгом чтении вида: не чаще
// раза за slowViewLogEvery.
func (s *Service) slowViewDue() bool {
	now := time.Now().UnixNano()
	last := s.slowLog.Load()
	if last != 0 && now-last < int64(slowViewLogEvery) {
		return false
	}
	return s.slowLog.CompareAndSwap(last, now)
}

// runRead — одно чтение вида: кладёт наблюдение в кэш, запускает следующее
// чтение, если оно записано, и публикует событие wizard, если вид отличается от
// отданного из кэша за время чтения. Одно чтение без цикла — не фоновая служба:
// Stop его не ждёт, ресурсов оно не держит.
func (s *Service) runRead(c *readCall) {
	start := time.Now()
	o, rt := s.readObservation()
	s.storeObservation(o)
	if took := time.Since(start); took > viewWait && s.slowViewDue() {
		log.Printf("setupwizard: чтение вида заняло %v (факты %v, установленность %v, ядра %v, секции Xray %v)",
			took, rt.facts, rt.inst, rt.kernels, rt.take)
	}

	s.cacheMu.Lock()
	served := c.served
	close(c.done)
	next := s.nextRead
	s.nextRead = nil
	s.reading = next
	if next != nil {
		go s.runRead(next)
	}
	now := ""
	if len(served) > 0 {
		now = s.viewKeyLocked()
	}
	s.cacheMu.Unlock()

	if slices.ContainsFunc(served, func(k string) bool { return k != now }) && s.ctx.Err() == nil {
		s.d.Layer.PublishWizard()
	}
}

// watchLate запоминает ключ вида, отданного из кэша: идущее или следующее чтение
// по завершении сравнит с ним свой вид. Чтений уже нет — сравнение сразу.
func (s *Service) watchLate(key string) {
	s.cacheMu.Lock()
	target := s.nextRead
	if target == nil {
		target = s.reading
	}
	if target != nil {
		target.served = append(target.served, key)
		s.cacheMu.Unlock()
		return
	}
	now := s.viewKeyLocked()
	s.cacheMu.Unlock()
	if now != key && s.ctx.Err() == nil {
		s.d.Layer.PublishWizard()
	}
}

// viewKeyLocked — ключ вида по кэшу и статусу службы (под cacheMu).
func (s *Service) viewKeyLocked() string {
	if s.obs == nil {
		return ""
	}
	o := *s.obs
	o.take = s.take
	return viewKey(s.buildView(o, s.currentStatusLocked()))
}

// viewKey — то, по чему клиенту стоит перечитать вид: подпись шагов, статус,
// запущенные ядра, конфликт и пометка «частично».
func viewKey(v View) string {
	var b strings.Builder
	b.WriteString(v.Status)
	b.WriteByte('|')
	for _, st := range v.Steps {
		if st.Done {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		if st.Partial != nil {
			fmt.Fprintf(&b, "(%d/%d)", st.Partial.Done, st.Partial.Total)
		}
	}
	fmt.Fprintf(&b, "|%d|%s|%v", v.Current, strings.Join(v.Kernels.Running, ","), v.Kernels.Conflict)
	return b.String()
}

// storeObservation кладёт наблюдение в кэш, если оно начато позже
// кэшированного; счёт секций — если он прочитан этим наблюдением. База статуса
// службы меняется, только если очередь записей пуста и наблюдение начато после
// завершения последней записи.
func (s *Service) storeObservation(o observation) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if o.take.seq == o.seq && o.seq > s.take.seq {
		s.take = o.take
	}
	if s.obs != nil && o.seq <= s.obs.seq {
		return
	}
	cached := o
	cached.take = takeCount{}
	s.obs = &cached
	if len(s.pending) == 0 && o.seq > s.lastWriteAt {
		s.base, s.baseSet = o.facts.Status, true
	}
}

// cached — последнее наблюдение вместе со счётом секций; false — наблюдений ещё
// не было.
func (s *Service) cached() (observation, bool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.obs == nil {
		return observation{}, false
	}
	o := *s.obs
	o.take = s.take
	return o, true
}

// currentStatus — статус для вида: база службы, свёрнутая с очередью ещё не
// выполненных записей (close и hide — запрошенный, если ранг выше; сброс —
// пустой).
func (s *Service) currentStatus() configlayer.WizardStatus {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	return s.currentStatusLocked()
}

// currentStatusLocked — currentStatus под cacheMu.
func (s *Service) currentStatusLocked() configlayer.WizardStatus {
	st := s.base
	if !s.baseSet && s.obs != nil {
		st = s.obs.facts.Status
	}
	for _, p := range s.pending {
		st = p.op.apply(st)
	}
	return st
}

// buildView — поля ответа мастера по наблюдению и статусу. «Частично, N из M» —
// только у Xray и только пока есть что взять: N — секции в черновике, M = N +
// незаблокированные секции не в черновике (заблокированные «Взять остальные» не
// возьмёт, в M их нет); счёт берётся, только если он прочитан при том же числе
// взятых секций.
func (s *Service) buildView(o observation, status configlayer.WizardStatus) View {
	f, inst, ks := o.facts, o.inst, o.ks
	ev := evaluate(f, inst, o.xkeen, ks)

	t := o.take
	if inst.Xray && f.XrayTaken >= 1 && t.known && t.n > 0 && t.forTaken == f.XrayTaken {
		for i := range ev.steps {
			if ev.steps[i].ID == StepSections {
				ev.steps[i].Partial = &Partial{Done: f.XrayTaken, Total: f.XrayTaken + t.n}
				break
			}
		}
	}

	finished := status == configlayer.WizardDone || status == configlayer.WizardHidden
	running := ks.Running
	if running == nil {
		running = []string{}
	}
	return View{
		Status:      string(status),
		Empty:       f.Empty,
		AutoOpen:    status == "" && f.Empty,
		CardVisible: !finished && ev.doneCount < TotalSteps,
		DoneCount:   ev.doneCount,
		Total:       TotalSteps,
		Current:     ev.current,
		Kernels: KernelsView{
			XKeen:    o.xkeen,
			Xray:     inst.Xray,
			Mihomo:   inst.Mihomo,
			Running:  running,
			Conflict: ks.Conflict,
		},
		Steps: ev.steps,
		TickS: int(s.interval() / time.Second),
	}
}

// View — статус мастера и шагов 1–6 (GET /api/configlayer/wizard). Ничего не
// пишет. Ждёт наблюдения не дольше viewWait; не дождался — вид из последнего
// наблюдения, а дочитанное позже отличие публикуется событием wizard (клиенты
// перечитают GET). Статус — статус службы: база, свёрнутая с очередью записей.
// После запуска или остановки ядра из панели lifecycle-хук сбрасывает кэш
// состояния ядер, и следующее чтение видит новое состояние.
func (s *Service) View() View {
	o, fresh := s.observe(viewWait)
	v := s.buildView(o, s.currentStatus())
	if !fresh {
		s.watchLate(viewKey(v))
	}
	return v
}

// postView — вид для ответа POST close|hide|reset: status — результат записи,
// прочие поля (empty, auto_open, steps, kernels) — из последнего наблюдения
// службы, без чтения ядер, фактов слоя и файлов Xray. Они могут отставать на одно
// наблюдение: если оно старше изменения, наблюдатель увидит смену подписи шагов и
// опубликует событие wizard не позже периода проверки, клиент перечитает GET.
// Проверки, которым нужны свежие факты, читают GET. Кэш пуст (до первого
// наблюдения после старта панели) — одно наблюдение без предела.
func (s *Service) postView(status configlayer.WizardStatus) View {
	o, ok := s.cached()
	if !ok {
		o, _ = s.observe(0)
	}
	return s.buildView(o, status)
}

// submitWrite ставит запись признака в очередь службы и ждёт её не дольше
// wizardWriteWait. Записи выполняются по одной в порядке поступления, каждая
// под tickMu (WR-03: отметка done наблюдателя не вклинивается). Успела —
// статус из результата этой записи (сброс — пустой, даже если следующий тик
// поставит done) и её ошибка. Не успела — статус службы с учётом этой записи,
// запись завершается сама: итог публикуют Layer.MarkWizard/ResetWizard, ошибку —
// журнал и событие wizard.
func (s *Service) submitWrite(op writeOp) (configlayer.WizardStatus, error) {
	s.cacheMu.Lock()
	s.writeID++
	id := s.writeID
	prev := s.writeTail
	done := make(chan struct{})
	s.writeTail = done
	s.pending = append(s.pending, pendingWrite{id: id, op: op})
	s.cacheMu.Unlock()

	call := &writeCall{res: make(chan writeResult, 1)}
	go s.runWrite(id, op, prev, done, call)

	timer := time.NewTimer(wizardWriteWait)
	defer timer.Stop()
	select {
	case r := <-call.res:
		return r.status, r.err
	case <-timer.C:
	}
	call.mu.Lock()
	select {
	case r := <-call.res:
		call.mu.Unlock()
		return r.status, r.err
	default:
		call.abandoned = true
	}
	call.mu.Unlock()
	return s.currentStatus(), nil
}

// runWrite — одна запись из очереди: ждёт предыдущую (prev), под tickMu пишет
// признак и обновляет статус службы, закрывает done и отдаёт итог. Одна запись
// без цикла — не фоновая служба.
func (s *Service) runWrite(id uint64, op writeOp, prev, done chan struct{}, call *writeCall) {
	queued := time.Now()
	<-prev
	waited := time.Since(queued)

	start := time.Now()
	s.tickMu.Lock()
	layer := s.d.Layer
	var r writeResult
	if op == writeReset {
		r.err = layer.ResetWizard()
	} else {
		var changed bool
		changed, r.err = layer.MarkWizard(op.status())
		if r.err == nil && changed {
			r.status = op.status()
		} else if r.err == nil {
			r.status = layer.WizardStatus()
		}
	}
	if r.err != nil {
		r.status = layer.WizardStatus()
	}
	// Статус службы — под tickMu: отметка done следующей записи не будет
	// перезаписана итогом этой.
	s.cacheMu.Lock()
	s.pending = slices.DeleteFunc(s.pending, func(p pendingWrite) bool { return p.id == id })
	s.clock++
	s.lastWriteAt = s.clock
	s.base, s.baseSet = r.status, true
	s.cacheMu.Unlock()
	s.tickMu.Unlock()
	close(done)

	took := time.Since(queued)
	if took > wizardWriteWait {
		log.Printf("setupwizard: запись признака %s заняла %v (ожидание очереди %v, слой %v)",
			op, took, waited, time.Since(start))
	}

	call.mu.Lock()
	abandoned := call.abandoned
	if !abandoned {
		call.res <- r
	}
	call.mu.Unlock()
	if abandoned && r.err != nil {
		// В тексте ошибки записи нет данных источников.
		log.Printf("setupwizard: признак %s не записан: %v", op, r.err)
		if s.ctx.Err() == nil {
			s.d.Layer.PublishWizard()
		}
	}
}

// Close — «Закрыть» мастер (D-10): признак closed отменяет автооткрытие,
// карточка остаётся. Идемпотентно и не понижает done или hidden.
func (s *Service) Close() (View, error) {
	return s.mark(writeClose)
}

// Hide — «Скрыть» карточку (D-11): признак hidden действует как «пройдено».
// Идемпотентно.
func (s *Service) Hide() (View, error) {
	return s.mark(writeHide)
}

// Reset снимает признак мастера — только в режиме разработчика
// (configlayer.ErrDevModeRequired), ручка проверок карточки и автооткрытия.
// Проверки, отвечающие кодом без записи, — синхронно, до очереди. Запись идёт
// через очередь под tickMu (WR-03): тик, прочитавший факты до сброса, допишет done
// раньше сброса, а не после него; в ответе статус — пустой, результат этой записи,
// без отметки следующего тика. Ответ не ждёт носителя, замка хранилища и тика
// дольше wizardWriteWait (G-149.1-6).
func (s *Service) Reset() (View, error) {
	if err := s.d.Layer.CheckWizardReset(); err != nil {
		return View{}, err
	}
	status, err := s.submitWrite(writeReset)
	if err != nil {
		return View{}, err
	}
	return s.postView(status), nil
}

// mark ставит признак мастера; «не изменилось» — не ошибка. Запись — через
// очередь под tickMu, как и отметка done наблюдателем (WR-03); ответ не ждёт
// носителя, замка хранилища и тика дольше wizardWriteWait (G-149.1-6).
func (s *Service) mark(op writeOp) (View, error) {
	status, err := s.submitWrite(op)
	if err != nil {
		return View{}, err
	}
	return s.postView(status), nil
}
