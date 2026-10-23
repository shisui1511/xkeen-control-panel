package routing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Имена групп по умолчанию (D-09). Это данные пользователя, не строки
// интерфейса: группы можно переименовать и удалить (D-37).
const (
	defaultAutoGroupName  = "Авто"
	defaultProxyGroupName = "Прокси"
)

// MihomoAPI — часть Clash API Mihomo, которая нужна выбору участника и пингу
// (используют 146-14 и 146-15).
type MihomoAPI interface {
	// HasController сообщает, настроен ли контроллер Mihomo.
	HasController() bool
	// ProxyNow возвращает выбранного участника группы.
	ProxyNow(ctx context.Context, group string) (string, error)
	// SelectProxy выбирает участника группы.
	SelectProxy(ctx context.Context, group, name string) error
	// ProxyDelay измеряет задержку узла.
	ProxyDelay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error)
}

// Deps — зависимости сервиса. Создаются в cmd/xcp/main.go.
type Deps struct {
	// DataDir — каталог данных панели.
	DataDir string
	// Layer — слой «Конфигурация»: черновик, живые данные, реестр проверок.
	Layer *configlayer.Layer
	// Sources — сервис источников: каталог узлов и хук групп по умолчанию.
	Sources *sources.Service
	// Lists — индекс списков dat XKeen.
	Lists *lists.Index
	// Geodata — запись geodata в xkeen.json.
	Geodata *lists.GeodataStore
	// Installed — какие ядра установлены сейчас (обязательно).
	Installed func() configlayer.InstalledKernels
	// KernelVersion — сырая версия ядра ("xray" или "mihomo"); пустая строка —
	// версия не определена (функции считаются доступными).
	KernelVersion func(kernel string) string
	// Running сообщает, запущено ли ядро.
	Running func(kernel string) bool
	// Mihomo — Clash API Mihomo.
	Mihomo MihomoAPI
	// Ping проверяет доступность узла (TLS-проверка, пока нет пинга через туннель).
	Ping func(ctx context.Context, n nodes.Node) error
	// GOARCH — архитектура для значений проверки по умолчанию (D-12); пусто —
	// runtime.GOARCH.
	GOARCH string
	// Now — источник времени (nil — time.Now).
	Now func() time.Time
	// Rand — источник случайности для ID (nil — crypto/rand).
	Rand io.Reader
}

// Service — сервис маршрутизации: правки секций groups, rules и probe черновика
// одной ревизией, состав группы и проверка до записи. Создаётся один раз в
// cmd/xcp/main.go.
type Service struct {
	d Deps

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// lmu защищает stopped: после Stop горутины не запускаются.
	lmu      sync.Mutex
	stopped  bool
	stopOnce sync.Once

	// Мемоизация генераторов: Registry.Build вызывается постоянно (Snapshot,
	// «Применить», фон), а вывод зависит только от входа.
	gmu      sync.Mutex
	gens     map[string][]configlayer.GeneratedFile
	genOrder []string

	// Мемоизация текста провайдеров списков dat для Mihomo (выгрузка большого
	// списка не повторяется на каждый вызов генератора).
	pmu       sync.Mutex
	provs     map[string][]byte
	provOrder []string
	provBytes int
}

// New создаёт сервис. Слой, сервис источников и источник «установленных ядер»
// обязательны. Регистрирует у сервиса источников хук групп по умолчанию.
func New(d Deps) (*Service, error) {
	if d.Layer == nil {
		return nil, errors.New("routing: не задан слой конфигурации")
	}
	if d.Sources == nil {
		return nil, errors.New("routing: не задан сервис источников")
	}
	if d.Installed == nil {
		return nil, errors.New("routing: не задан источник установленных ядер")
	}
	if d.KernelVersion == nil {
		d.KernelVersion = func(string) string { return "" }
	}
	if d.Running == nil {
		d.Running = func(string) bool { return false }
	}
	if d.GOARCH == "" {
		d.GOARCH = runtime.GOARCH
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Rand == nil {
		d.Rand = rand.Reader
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{d: d, ctx: ctx, cancel: cancel, gens: map[string][]configlayer.GeneratedFile{}, provs: map[string][]byte{}}
	d.Sources.SetDraftExtras(s.draftExtras)
	return s, nil
}

// Generators возвращает генераторы файлов ядер для Layer.RegisterGenerator:
// routing_xray (146-07) и routing_mihomo (146-11).
func (s *Service) Generators() []configlayer.Generator {
	return []configlayer.Generator{xrayGen{s: s}, mihomoGen{s: s}}
}

// Start запускает фоновые задачи сервиса. В этом плане задач нет: наблюдатель dat
// добавит 146-13, выбор участника — 146-14; они запускаются через Go.
func (s *Service) Start() {}

// Stop останавливает фоновые задачи и ждёт их завершения. Идемпотентен.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		s.lmu.Lock()
		s.stopped = true
		s.lmu.Unlock()
		s.cancel()
	})
	s.wg.Wait()
}

// Context — контекст жизни сервиса: отменяется Stop.
func (s *Service) Context() context.Context { return s.ctx }

// Go запускает фоновую задачу сервиса под учётом Stop; после Stop задача не
// запускается и Go возвращает false.
func (s *Service) Go(fn func()) bool {
	s.lmu.Lock()
	if s.stopped {
		s.lmu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.lmu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// inputOf — входные данные генераторов и проверок для черновика.
func inputOf(st configlayer.StateView) configlayer.Input {
	return configlayer.Input{Sections: st.Draft, Live: st.Live, Mode: configlayer.SourceDraft}
}

// newUniqueID возвращает ID, которого нет среди занятых.
func (s *Service) newUniqueID(taken map[string]bool) (string, error) {
	for range 16 {
		id, err := newID(s.d.Rand)
		if err != nil {
			return "", err
		}
		if !taken[id] {
			return id, nil
		}
	}
	return "", errors.New("routing: не удалось подобрать уникальный идентификатор")
}

// takenIDs — ID групп и правил, уже занятые в секциях.
func takenIDs(groups []Group, rules []Rule) map[string]bool {
	taken := make(map[string]bool, len(groups)+len(rules))
	for _, g := range groups {
		taken[g.ID] = true
	}
	for _, r := range rules {
		taken[r.ID] = true
	}
	return taken
}

// draftSourceIDs — идентификаторы источников, секции которых есть в черновике.
func draftSourceIDs(draft configlayer.Sections) map[string]bool {
	out := map[string]bool{}
	for name := range draft {
		if id, ok := strings.CutPrefix(name, sources.SectionPrefix); ok && id != "" {
			out[id] = true
		}
	}
	return out
}

// draftExtras — хук «Добавить» сервиса источников: когда в черновике появляется
// первый источник, той же правкой создаются группы «Авто» (все источники, режим
// «авто») и «Прокси» (ручная, участник «Авто» и все узлы). Один раз:
// defaults_done не даёт создать их снова, а группы, удалённые пользователем,
// не возвращаются (D-09, D-37). Источники, добавленные до этой фазы, групп по
// умолчанию не получают: до 1.0.0 без миграций (U-07).
func (s *Service) draftExtras(st configlayer.StateView, added []string) (map[string]json.RawMessage, error) {
	if len(added) == 0 {
		return nil, nil
	}
	cfg, err := parseGroups(st.Draft[GroupsSection])
	if err != nil || cfg.DefaultsDone {
		// Испорченную секцию сообщает проверка; добавлению источника она не мешает.
		return nil, nil
	}
	for name := range st.Draft {
		if strings.HasPrefix(name, sources.SectionPrefix) {
			return nil, nil
		}
	}
	rules, err := parseRules(st.Draft[RulesSection])
	if err != nil {
		rules = RulesConfig{}
	}
	taken := takenIDs(cfg.Items, rules.Items)
	autoID, err := s.newUniqueID(taken)
	if err != nil {
		return nil, err
	}
	taken[autoID] = true
	proxyID, err := s.newUniqueID(taken)
	if err != nil {
		return nil, err
	}
	cfg.DefaultsDone = true
	cfg.Items = append(cfg.Items,
		Group{ID: autoID, Name: defaultAutoGroupName, Mode: ModeAuto, Sources: GroupSources{All: true}},
		Group{ID: proxyID, Name: defaultProxyGroupName, Mode: ModeManual, Sources: GroupSources{All: true},
			Members: []Member{{Kind: KindGroup, ID: autoID}}},
	)
	raw, err := encodeGroups(cfg)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{GroupsSection: raw}, nil
}

// edit читает состояние, проверяет ревизию и записывает секции, которые вернул
// fn, одной правкой черновика. Конфликт ревизий — configlayer.ErrDraftConflict
// без записи.
func (s *Service) edit(rev int64, fn func(st configlayer.StateView) (map[string]json.RawMessage, error)) (configlayer.DraftEvent, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	if st.DraftRevision != rev {
		return configlayer.DraftEvent{}, configlayer.ErrDraftConflict
	}
	sections, err := fn(st)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.d.Layer.EditDraftBatch(rev, sections, nil)
}

// SaveGroup сохраняет группу в черновик. Пустой ID — новая группа.
func (s *Service) SaveGroup(rev int64, g Group) (configlayer.DraftEvent, Group, error) {
	var saved Group
	ev, err := s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		cfg, err := parseGroups(st.Draft[GroupsSection])
		if err != nil {
			return nil, err
		}
		rules, err := parseRules(st.Draft[RulesSection])
		if err != nil {
			return nil, err
		}
		idx := -1
		if g.ID != "" {
			for i := range cfg.Items {
				if cfg.Items[i].ID == g.ID {
					idx = i
				}
			}
			if idx < 0 {
				return nil, ErrGroupNotFound
			}
		}
		others := make([]Group, 0, len(cfg.Items))
		for i, o := range cfg.Items {
			if i != idx {
				others = append(others, o)
			}
		}
		if idx < 0 && len(cfg.Items) >= maxGroups {
			return nil, invalid("groups", reasonTooMany)
		}
		if err := validateGroup(&g, others, draftSourceIDs(st.Draft)); err != nil {
			return nil, err
		}
		if idx < 0 {
			if g.ID, err = s.newUniqueID(takenIDs(cfg.Items, rules.Items)); err != nil {
				return nil, err
			}
			cfg.Items = append(cfg.Items, g)
		} else {
			cfg.Items[idx] = g
		}
		for _, id := range groupCycles(cfg.Items) {
			if id == g.ID {
				return nil, &GroupCycleError{Group: g.Name}
			}
		}
		raw, err := encodeGroups(cfg)
		if err != nil {
			return nil, err
		}
		saved = g
		return map[string]json.RawMessage{GroupsSection: raw}, nil
	})
	if err != nil {
		return configlayer.DraftEvent{}, Group{}, err
	}
	return ev, saved, nil
}

// editRules правит секцию rules (и читает группы для проверки целей) одной
// правкой черновика.
func (s *Service) editRules(rev int64, fn func(cfg *RulesConfig, groups []Group, st configlayer.StateView) error) (configlayer.DraftEvent, error) {
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		groups, err := parseGroups(st.Draft[GroupsSection])
		if err != nil {
			return nil, err
		}
		cfg, err := parseRules(st.Draft[RulesSection])
		if err != nil {
			return nil, err
		}
		if err := fn(&cfg, groups.Items, st); err != nil {
			return nil, err
		}
		raw, err := encodeRules(cfg)
		if err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{RulesSection: raw}, nil
	})
}

// ruleIndex возвращает позицию правила по ID (-1 — нет).
func ruleIndex(items []Rule, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

// ruleBody — часть правила, правка которой превращает правило пресета в
// правило пользователя: условия, цель и своё правило ядра (D-24).
func ruleBody(r Rule) string {
	b, err := json.Marshal(struct {
		Conds  []Cond   `json:"conds"`
		Target Target   `json:"target"`
		Raw    *RawRule `json:"raw"`
	}{r.Conds, r.Target, r.Raw})
	if err != nil {
		return ""
	}
	return string(b)
}

// SaveRule сохраняет правило в черновик. Пустой ID — новое правило (в конец
// списка, перед «Остальным»). Правка условий, цели или своего правила ядра у
// правила «из пресета» снимает пометку; выключение и перемещение — нет (D-24).
// Пометку «из пресета» через SaveRule поставить нельзя.
func (s *Service) SaveRule(rev int64, r Rule) (configlayer.DraftEvent, Rule, error) {
	var saved Rule
	ev, err := s.editRules(rev, func(cfg *RulesConfig, groups []Group, _ configlayer.StateView) error {
		idx := -1
		if r.ID != "" {
			if idx = ruleIndex(cfg.Items, r.ID); idx < 0 {
				return ErrRuleNotFound
			}
		} else if len(cfg.Items) >= maxRules {
			return invalid("rules", reasonTooMany)
		}
		if err := validateRule(&r, groups); err != nil {
			return err
		}
		if idx < 0 {
			id, err := s.newUniqueID(takenIDs(groups, cfg.Items))
			if err != nil {
				return err
			}
			r.ID, r.Origin = id, ""
			cfg.Items = append(cfg.Items, r)
		} else {
			old := cfg.Items[idx]
			r.Origin = old.Origin
			normalizeRule(&old)
			if r.Origin == OriginPreset && ruleBody(old) != ruleBody(r) {
				r.Origin = ""
			}
			cfg.Items[idx] = r
		}
		saved = r
		return nil
	})
	if err != nil {
		return configlayer.DraftEvent{}, Rule{}, err
	}
	return ev, saved, nil
}

// MoveRule переставляет правило на позицию to (с нуля). «Остальное» не
// двигается: оно всегда последнее. Пометка «из пресета» сохраняется.
func (s *Service) MoveRule(rev int64, id string, to int) (configlayer.DraftEvent, error) {
	return s.editRules(rev, func(cfg *RulesConfig, _ []Group, _ configlayer.StateView) error {
		from := ruleIndex(cfg.Items, id)
		if from < 0 {
			return ErrRuleNotFound
		}
		if to < 0 || to >= len(cfg.Items) {
			return invalid("to", reasonOutOfRange)
		}
		item := cfg.Items[from]
		rest := append(append(make([]Rule, 0, len(cfg.Items)), cfg.Items[:from]...), cfg.Items[from+1:]...)
		cfg.Items = append(append(rest[:to:to], item), rest[to:]...)
		return nil
	})
}

// SetRuleEnabled включает или выключает правило, не удаляя его.
func (s *Service) SetRuleEnabled(rev int64, id string, on bool) (configlayer.DraftEvent, error) {
	return s.editRules(rev, func(cfg *RulesConfig, _ []Group, _ configlayer.StateView) error {
		i := ruleIndex(cfg.Items, id)
		if i < 0 {
			return ErrRuleNotFound
		}
		cfg.Items[i].Enabled = on
		return nil
	})
}

// DeleteRule удаляет правило. На правило никто не ссылается, каскада нет.
func (s *Service) DeleteRule(rev int64, id string) (configlayer.DraftEvent, error) {
	return s.editRules(rev, func(cfg *RulesConfig, _ []Group, _ configlayer.StateView) error {
		i := ruleIndex(cfg.Items, id)
		if i < 0 {
			return ErrRuleNotFound
		}
		cfg.Items = append(cfg.Items[:i:i], cfg.Items[i+1:]...)
		return nil
	})
}

// SetFinal задаёт цель «Остального» (в Mihomo MATCH). Цель обязательна: группа,
// DIRECT или REJECT.
func (s *Service) SetFinal(rev int64, t Target) (configlayer.DraftEvent, error) {
	return s.editRules(rev, func(cfg *RulesConfig, groups []Group, _ configlayer.StateView) error {
		if err := validateTarget("final", &t, groups); err != nil {
			return err
		}
		cfg.Final = t
		return nil
	})
}

// SaveProbe сохраняет параметры проверки узлов (одни на панель, D-12).
func (s *Service) SaveProbe(rev int64, p ProbeConfig) (configlayer.DraftEvent, error) {
	p.V = sectionVersion
	if err := validateProbe(p); err != nil {
		return configlayer.DraftEvent{}, err
	}
	raw, err := encodeProbe(p)
	if err != nil {
		return configlayer.DraftEvent{}, err
	}
	return s.edit(rev, func(configlayer.StateView) (map[string]json.RawMessage, error) {
		return map[string]json.RawMessage{ProbeSection: raw}, nil
	})
}

// Probe возвращает параметры проверки узлов из черновика; нет секции — значения
// по умолчанию для архитектуры.
func (s *Service) Probe() (ProbeConfig, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return ProbeConfig{}, err
	}
	return parseProbe(st.Draft[ProbeSection], s.d.GOARCH)
}

// setManaged записывает секцию routing черновика: желаемое состояние «маршрутизацией
// управляет панель». Фактическое взятие делает «Применить» (146-06).
func (s *Service) setManaged(rev int64, managed bool) (configlayer.DraftEvent, error) {
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		raw, err := json.Marshal(configlayer.RoutingDesired{V: sectionVersion, Managed: managed})
		if err != nil {
			return nil, err
		}
		sections := map[string]json.RawMessage{configlayer.RoutingSection: raw}
		if managed && isEmptyRaw(st.Draft[RulesSection]) {
			// «Начать с пустого» (D-04): «Остальное → DIRECT» явное.
			rules, err := encodeRules(RulesConfig{Final: Target{Kind: KindDirect}})
			if err != nil {
				return nil, err
			}
			sections[RulesSection] = rules
		}
		return sections, nil
	})
}

// Take записывает в черновик желание управлять маршрутизацией. Если секции
// правил нет, она создаётся с «Остальное → DIRECT».
func (s *Service) Take(rev int64) (configlayer.DraftEvent, error) { return s.setManaged(rev, true) }

// Release записывает в черновик отказ от управления маршрутизацией: проверка
// перестаёт блокировать «Применить только узлы».
func (s *Service) Release(rev int64) (configlayer.DraftEvent, error) {
	return s.setManaged(rev, false)
}
