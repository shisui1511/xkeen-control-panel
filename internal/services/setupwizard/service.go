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
	"slices"
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
	// без неё пометки «частично» нет. Зовётся только из View — она читает вид
	// секций ядра, для тика наблюдателя это дорого.
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

// Service — служба мастера первого запуска.
type Service struct {
	d Deps

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
	started  atomic.Bool
	done     chan struct{}

	// tickMu — одна проверка за раз (тик наблюдателя и хук после применения);
	// под ним же подпись вектора шагов прошлой проверки.
	tickMu  sync.Mutex
	lastSig string
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
	return &Service{d: d, ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
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

// View — статус мастера и шагов 1–6 (GET /api/configlayer/wizard). Ничего не
// пишет.
func (s *Service) View() View {
	inst := s.d.Installed()
	xkeen := s.d.XKeenInstalled()
	ks := s.d.Kernels()
	f := s.d.Layer.WizardFacts(probe(inst))
	ev := evaluate(f, inst, xkeen, ks)

	// «Частично, N из M» — только у Xray и только пока есть что взять: N — секции
	// в черновике, M = N + незаблокированные секции не в черновике (заблокированные
	// «Взять остальные» не возьмёт, в M их нет).
	if inst.Xray && f.XrayTaken >= 1 && s.d.XrayTakeable != nil {
		if n, ok := s.d.XrayTakeable(); ok && n > 0 {
			ev.steps[1].Partial = &Partial{Done: f.XrayTaken, Total: f.XrayTaken + n}
		}
	}

	status := f.Status
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
			XKeen:    xkeen,
			Xray:     inst.Xray,
			Mihomo:   inst.Mihomo,
			Running:  running,
			Conflict: ks.Conflict,
		},
		Steps: ev.steps,
	}
}

// Close — «Закрыть» мастер (D-10): признак closed отменяет автооткрытие,
// карточка остаётся. Идемпотентно и не понижает done или hidden.
func (s *Service) Close() (View, error) {
	return s.mark(configlayer.WizardClosed)
}

// Hide — «Скрыть» карточку (D-11): признак hidden действует как «пройдено».
// Идемпотентно.
func (s *Service) Hide() (View, error) {
	return s.mark(configlayer.WizardHidden)
}

// Reset снимает признак мастера — только в режиме разработчика
// (configlayer.ErrDevModeRequired), ручка проверок карточки и автооткрытия.
func (s *Service) Reset() (View, error) {
	if err := s.d.Layer.ResetWizard(); err != nil {
		return View{}, err
	}
	return s.View(), nil
}

// mark ставит признак мастера; «не изменилось» — не ошибка.
func (s *Service) mark(status configlayer.WizardStatus) (View, error) {
	if _, err := s.d.Layer.MarkWizard(status); err != nil {
		return View{}, err
	}
	return s.View(), nil
}
