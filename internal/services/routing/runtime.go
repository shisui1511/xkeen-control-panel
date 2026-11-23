package routing

// Вид страницы «Прокси» по применённому состоянию и Live (фаза 150, PRX-01).
// Наружу уходят только ссылки, подписи и признаки совместимости узлов.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/proxyping"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

const (
	RuntimeStatePanel       = "panel"
	RuntimeStateStopped     = "stopped"
	RuntimeStateUserProfile = "user_profile"
	RuntimeStateUserManaged = "user_managed"
	RuntimeStateConflict    = "conflict"
	RuntimeModePanel        = "panel"
	RuntimeModeUserManaged  = "user_managed"
	RuntimeModeUserProfile  = "user_profile"
	RuntimeModeNotInstalled = "not_installed"
	RuntimeGroupSource      = "source"
)

type RuntimeView struct {
	Kernel   string         `json:"kernel"`
	Running  []string       `json:"running"`
	State    string         `json:"state"`
	NoSocket bool           `json:"no_socket"`
	Modes    RuntimeModes   `json:"modes"`
	Probe    ProbeConfig    `json:"probe"`
	Groups   []RuntimeGroup `json:"groups"`
	// Clock — часы роутера (процесса панели): по ним действуют окна расписания
	// (D-08), карточка показывает время окон в этом поясе.
	Clock RuntimeClock `json:"clock"`
}

// RuntimeClock — момент и пояс часов роутера.
type RuntimeClock struct {
	// Now — unix, секунды.
	Now int64 `json:"now"`
	// Zone — сокращение пояса (например, MSK или UTC).
	Zone string `json:"zone"`
	// Offset — смещение пояса от UTC в секундах.
	Offset int `json:"offset"`
}

// RuntimeHold — чем арбитр держит группу сейчас (строка состояния карточки).
type RuntimeHold struct {
	// Kind — "schedule", "override", "quota" или "node_missing".
	Kind      string `json:"kind"`
	Limit     string `json:"limit,omitempty"`
	LimitName string `json:"limit_name,omitempty"`
	ResetAt   int64  `json:"reset_at,omitempty"`
	// Until — конец удержания (unix, секунды): конец окна или перехвата.
	Until   int64  `json:"until,omitempty"`
	Window  string `json:"window,omitempty"`
	Member  string `json:"member,omitempty"`
	Stalled bool   `json:"stalled,omitempty"`
}

// ScheduleWindowView — окно расписания группы в виде «Прокси».
type ScheduleWindowView struct {
	ScheduleWindow
	// MemberLabel — подпись участника окна по применённому состоянию.
	MemberLabel string `json:"member_label"`
	// Missing — участника окна нет среди вариантов группы (D-07).
	Missing bool `json:"missing"`
	// Active — окно действует сейчас.
	Active bool `json:"active"`
}

type RuntimeModes struct {
	Xray   string `json:"xray"`
	Mihomo string `json:"mihomo"`
}

type RuntimeGroup struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Mode         string        `json:"mode"`
	Current      string        `json:"current"`
	CurrentLabel string        `json:"current_label"`
	Chain        []RuntimeLink `json:"chain"`
	Fallback     string        `json:"fallback,omitempty"`
	Selectable   bool          `json:"selectable"`
	Unconfirmed  bool          `json:"unconfirmed"`
	// Switching — группа в компоненте идущей на сервере операции выбора или
	// «Повторить» (D-17); любая вкладка блокирует её плитки.
	Switching  bool            `json:"switching"`
	LockedWith []string        `json:"locked_with"`
	Counts     KernelCounts    `json:"counts"`
	Members    []RuntimeMember `json:"members"`
	// Hold — удержание группы арбитром; nil — группу никто не держит.
	Hold *RuntimeHold `json:"hold,omitempty"`
	// Schedule — окна расписания ручной группы; у остальных пусто.
	Schedule []ScheduleWindowView `json:"schedule"`
}

type RuntimeLink struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name"`
}

type RuntimeMember struct {
	Member string `json:"member"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref,omitempty"`
	Group  string `json:"group,omitempty"`
	Name   string `json:"name"`
	Proto  string `json:"proto,omitempty"`
	Source string `json:"source,omitempty"`
	// SourceName — имя источника узла: заголовок окна диагностики источника.
	SourceName string `json:"source_name,omitempty"`
	Xray       bool   `json:"xray"`
	Mihomo     bool   `json:"mihomo"`
}

// runtimeSnapshot собирает все данные из одной версии состояния и каталога.
func (s *Service) runtimeSnapshot() (RuntimeView, appliedModel, error) {
	// Отметки идущих выборов читаются до и после состояния и объединяются: отметка,
	// видимая хотя бы в одном из двух чтений, перекрывает оба края окна, в котором
	// чтение состояния (с обращением к диску) успевает захватить маркер уже снятого
	// выбора (снятие маркера идёт раньше снятия отметки).
	kinds := s.inflightKinds()
	// Цели идущих выборов быстрого пути (уже в Mihomo, ещё не в Live) читаются до
	// состояния: фоновый проход снимает цель после записи Live, и при чтении после
	// состояния цель могла быть снята уже после того, как ReadState вернул прежний
	// Live, — вид показал бы прежний узел без подстановки. Прочитанная заранее цель
	// перекрывает Live любого из двух исходов; её возврат виден со следующим
	// событием proxies. Отметки читаются до и после состояния по другой причине —
	// см. выше.
	targets := s.inflightTargets()
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return RuntimeView{}, appliedModel{}, err
	}
	for id, kind := range s.inflightKinds() {
		if kinds[id] == "" {
			kinds[id] = kind
		}
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return RuntimeView{}, appliedModel{}, err
	}
	active := services.ActiveKernelState{}
	if s.d.KernelState != nil {
		active = s.d.KernelState()
	}
	probe, err := parseProbe(st.Applied[ProbeSection], s.d.GOARCH)
	if err != nil {
		probe = defaultProbe(s.d.GOARCH)
	}
	desired := configlayer.DesiredRouting(st.Applied)
	mode := func(kernel string, installed, taken bool, foreign string) string {
		if !installed {
			return RuntimeModeNotInstalled
		}
		if taken && desired.ManagedFor(kernel) {
			return RuntimeModePanel
		}
		return foreign
	}
	now := s.d.Now()
	zone, offset := now.Zone()
	v := RuntimeView{
		Kernel: active.Kernel, Running: append([]string{}, active.Running...),
		Probe: probe, Groups: []RuntimeGroup{},
		Clock: RuntimeClock{Now: now.Unix(), Zone: zone, Offset: offset},
		Modes: RuntimeModes{
			Xray:   mode(configlayer.KernelXray, am.installed.Xray, st.Routing.XrayTaken, RuntimeModeUserManaged),
			Mihomo: mode(configlayer.KernelMihomo, am.installed.Mihomo, st.Routing.MihomoTaken, RuntimeModeUserProfile),
		},
	}
	switch {
	case active.Conflict:
		v.State = RuntimeStateConflict
	case runtimeRunning(v, configlayer.KernelMihomo) && v.Modes.Mihomo == RuntimeModeUserProfile:
		v.State = RuntimeStateUserProfile
		if active.Kernel == configlayer.KernelMihomo && s.d.Mihomo != nil {
			info, err := s.d.Mihomo.ParseControllerConfig()
			v.NoSocket = err == nil && info.Type != "unix"
		}
	case runtimeRunning(v, configlayer.KernelXray) && v.Modes.Xray == RuntimeModeUserManaged:
		v.State = RuntimeStateUserManaged
	case len(v.Running) == 0:
		v.State = RuntimeStateStopped
	default:
		v.State = RuntimeStatePanel
	}
	xray, mihomo := runtimeRefs(am.cat.Xray), runtimeRefs(am.cat.Mihomo)
	switch v.State {
	case RuntimeStatePanel, RuntimeStateStopped:
		live, unconfirmed := liveSelection(st.Live), readUnconfirmed(st.Live)
		scheds, recs := readSchedules(st.Live), readArbiter(st.Live)
		holds := s.limitHolds()
		// Цель выбора подставляется только группе с отметкой select: ядро уже несёт
		// узел, Live его ещё не записал. Копия live — общая для current и цепочек
		// всех групп, поэтому внешняя группа с участником-группой видит ту же цель.
		for id, member := range targets {
			if kinds[id] == inflightSelect {
				live[id] = member
			}
		}
		for _, g := range am.groups.Items {
			r := Resolve(g, am.union, am.groups.Items)
			// Select отказывает при маркере xray на компоненте, поэтому маркер
			// xray у группы с отметкой select записан этим идущим выбором:
			// известное намерение не показывается неизвестным (D-16). Маркер
			// mihomo и маркеры во время «Повторить» видны как есть.
			marker := unconfirmed[g.ID]
			ownMarker := marker == configlayer.KernelXray && kinds[g.ID] == inflightSelect
			rg := RuntimeGroup{
				ID: g.ID, Name: g.Name, Mode: string(g.Mode),
				Chain: []RuntimeLink{}, LockedWith: []string{}, Members: []RuntimeMember{},
				Selectable:  g.Mode == ModeManual && desired.AnyManaged(),
				Unconfirmed: marker != "" && !ownMarker,
				Switching:   kinds[g.ID] != "",
			}
			for _, id := range linkedGroups(am.groups.Items, g.ID) {
				if id != g.ID {
					rg.LockedWith = append(rg.LockedWith, id)
				}
			}
			for _, m := range r.Members {
				member := memberString(m)
				rg.Members = append(rg.Members, RuntimeMember{
					Member: member, Kind: m.Kind, Group: m.ID, Name: am.label(member),
				})
			}
			for _, n := range r.Nodes {
				rg.Members = append(rg.Members, runtimeNode(n, xray, mihomo))
			}
			rg.Counts = runtimeCounts(rg.Members, am.installed)
			if g.Mode == ModeManual {
				rg.Current = effectiveSelection(g, live[g.ID], am.avail, nodeRefs(r.Nodes)...)
				rg.CurrentLabel = am.label(rg.Current)
				rg.Chain = runtimeChain(g, am, live)
				rg.Schedule, rg.Hold = runtimeSchedule(g, am, live[g.ID], scheds[g.ID], recs, holdFor(holds, g.ID), now)
			}
			if g.Mode == ModeFallback {
				rg.Fallback = g.Reserve
			}
			v.Groups = append(v.Groups, rg)
		}
	case RuntimeStateUserManaged:
		bySource := map[string]int{}
		for _, n := range am.cat.Xray {
			i, ok := bySource[n.Source]
			if !ok {
				i = len(v.Groups)
				bySource[n.Source] = i
				v.Groups = append(v.Groups, RuntimeGroup{
					ID: "source:" + n.Source, Name: n.SourceName, Mode: RuntimeGroupSource,
					Chain: []RuntimeLink{}, LockedWith: []string{}, Members: []RuntimeMember{},
				})
			}
			v.Groups[i].Members = append(v.Groups[i].Members, runtimeNode(n, xray, mihomo))
		}
		for i := range v.Groups {
			v.Groups[i].Counts = runtimeCounts(v.Groups[i].Members, am.installed)
		}
	}
	for i := range v.Groups {
		if v.Groups[i].Schedule == nil {
			v.Groups[i].Schedule = []ScheduleWindowView{}
		}
	}
	return v, am, nil
}

// runtimeSchedule — окна ручной группы g для вида и удержание группы арбитром
// (лимит, перехват, окно, пропущенное окно, отказ ядра) по тому же решению, что у
// тика (decide, runtimeHold). cur — выбор группы в Live.
func runtimeSchedule(g Group, am appliedModel, cur string, ws []ScheduleWindow, recs map[string]arbiterRecord, hold *services.LimitHold, now time.Time) ([]ScheduleWindowView, *RuntimeHold) {
	out := make([]ScheduleWindowView, 0, len(ws))
	rec, hasRec := recs[g.ID]
	if len(ws) == 0 && !hasRec && hold == nil {
		// Группу никто не держит; варианты группы (Resolve по ядрам) на каждый
		// опрос вида не считаются.
		return out, nil
	}
	in := arbiterInput(g, am, cur, ws, rec, hasRec, hold, now)
	labels := map[string]string{}
	if len(ws) > 0 {
		for _, o := range appliedOptions(g, am) {
			labels[o.Member] = o.Label
		}
	}
	for _, x := range ws {
		label, ok := labels[x.Member]
		out = append(out, ScheduleWindowView{
			ScheduleWindow: x, MemberLabel: label, Missing: !ok,
			Active: in.hasWin && x.ID == in.win.ID,
		})
	}
	return out, runtimeHold(in, decide(in))
}

func runtimeRunning(v RuntimeView, kernel string) bool {
	for _, k := range v.Running {
		if k == kernel {
			return true
		}
	}
	return false
}

func runtimeRefs(list []sources.CatalogNode) map[string]bool {
	refs := make(map[string]bool, len(list))
	for _, n := range list {
		refs[n.Ref] = true
	}
	return refs
}

func runtimeNode(n sources.CatalogNode, xray, mihomo map[string]bool) RuntimeMember {
	return RuntimeMember{
		Member: memberNode + n.Ref, Kind: "node", Ref: n.Ref, Name: n.Display,
		Proto: n.Node.Protocol, Source: n.Source, SourceName: n.SourceName,
		Xray: xray[n.Ref], Mihomo: mihomo[n.Ref],
	}
}

func runtimeCounts(members []RuntimeMember, installed configlayer.InstalledKernels) KernelCounts {
	xray, mihomo := 0, 0
	for _, m := range members {
		if m.Xray {
			xray++
		}
		if m.Mihomo {
			mihomo++
		}
	}
	return KernelCounts{Xray: optCount(installed.Xray, xray), Mihomo: optCount(installed.Mihomo, mihomo)}
}

// runtimeMihomo использует тот же расчёт имён копий и скрытых групп, что
// генератор профиля, без повторного чтения состояния или каталога.
func runtimeMihomo(am appliedModel) *mihomoBuild {
	b := newMihomoBuild(mihomoBuild{groups: am.groups.Items, catalog: am.cat.Mihomo, groupsOK: true})
	b.prepare()
	return b
}

func runtimeMihomoNames(b *mihomoBuild, g Group) map[string]string {
	names := b.nodeNames(g)
	if mg := b.shapeByID[g.ID]; mg != nil {
		for ref, name := range b.copyName {
			if name != "" && (containsString(mg.copies, name) || mg.reserve == name) {
				names[ref] = name
			}
		}
	}
	return names
}

// probeProvider — провайдер, через который меряется узел ref под именем name в
// профиле: имя узла уникально только внутри провайдера источника, и одноимённые
// узлы разных источников по одному имени не различить. Пусто — имя ищется в
// /proxies: это копия узла в proxies профиля (имя уникально, freeName) или имя,
// которого нет в файле провайдера.
func (b *mihomoBuild) probeProvider(ref, name string) string {
	if name == "" || b.copyName[ref] == name {
		return ""
	}
	if n, ok := b.nodeByRef[ref]; ok && n.MihomoName == name {
		return providerName(n.Source)
	}
	return ""
}

// ambiguousNames — имена, которые в профиле носят разные узлы (одноимённые
// узлы разных источников, копия с именем узла другого источника) или узел и
// группа профиля: факты Mihomo по такому имени нельзя приписать одному узлу.
func (b *mihomoBuild) ambiguousNames() map[string]bool {
	owner := map[string]string{}
	out := map[string]bool{}
	add := func(name, ref string) {
		if name == "" {
			return
		}
		if o, ok := owner[name]; !ok {
			owner[name] = ref
		} else if o != ref {
			out[name] = true
		}
	}
	for _, n := range b.catalog {
		add(n.MihomoName, n.Ref)
	}
	for ref, name := range b.copyName {
		add(name, ref)
	}
	// В /proxies Mihomo лежат и группы профиля: узел провайдера с именем группы
	// получил бы её историю. Имена групп отмечают совпавшее имя узла.
	for _, g := range b.groups {
		for _, name := range []string{g.Name, hiddenName(g.ID)} {
			if _, taken := owner[name]; taken {
				out[name] = true
			}
		}
	}
	return out
}

func runtimeChain(g Group, am appliedModel, live map[string]string) []RuntimeLink {
	out := []RuntimeLink{}
	seen := map[string]bool{}
	for g.Mode == ModeManual && !seen[g.ID] {
		seen[g.ID] = true
		r := Resolve(g, am.union, am.groups.Items)
		current := effectiveSelection(g, live[g.ID], am.avail, nodeRefs(r.Nodes)...)
		switch {
		case strings.HasPrefix(current, memberGroup):
			id := strings.TrimPrefix(current, memberGroup)
			out = append(out, RuntimeLink{Kind: KindGroup, ID: id, Name: am.label(current)})
			g, _ = am.group(id)
		case strings.HasPrefix(current, memberNode):
			return append(out, RuntimeLink{Kind: "node", Ref: strings.TrimPrefix(current, memberNode), Name: am.label(current)})
		case current == memberDirect || current == memberReject:
			return append(out, RuntimeLink{Kind: current, Name: am.label(current)})
		default:
			return out
		}
	}
	return out
}

// RuntimeView возвращает действующие группы, выбор и совместимость узлов.
func (s *Service) RuntimeView() (RuntimeView, error) {
	v, _, err := s.runtimeSnapshot()
	return v, err
}

var (
	ErrKernelRestarting = errors.New("routing: ядро перезапускается")
	ErrPingUnavailable  = errors.New("routing: пинг на этой странице недоступен: свой профиль Mihomo или конфликт ядер")
	ErrGroupNoNodes     = errors.New("routing: в группе нет узлов для активного ядра")
)

type RuntimeNow struct {
	Kernel  string              `json:"kernel"`
	Groups  map[string]GroupNow `json:"groups"`
	History map[string]NowDelay `json:"history"`
}

type GroupNow struct {
	Ref     string `json:"ref,omitempty"`
	Known   bool   `json:"known"`
	Fastest bool   `json:"fastest,omitempty"`
}

type NowDelay struct {
	MS int       `json:"ms"`
	At time.Time `json:"at"`
}

// RuntimeNow читает факты работающего ядра; пустая цель не заменяется догадкой.
func (s *Service) RuntimeNow(ctx context.Context) (RuntimeNow, error) {
	if s.d.InRestart != nil && s.d.InRestart() {
		return RuntimeNow{}, ErrKernelRestarting
	}
	v, am, err := s.runtimeSnapshot()
	if err != nil {
		return RuntimeNow{}, err
	}
	out := RuntimeNow{Kernel: v.Kernel, Groups: map[string]GroupNow{}, History: map[string]NowDelay{}}
	if v.State != RuntimeStatePanel || !runtimeRunning(v, v.Kernel) {
		return out, nil
	}
	var states map[string]services.ClashProxyState
	var mh *mihomoBuild
	var ambiguous map[string]bool
	if v.Kernel == configlayer.KernelMihomo && v.Modes.Mihomo == RuntimeModePanel && s.d.Mihomo != nil {
		mh = runtimeMihomo(am)
		ambiguous = mh.ambiguousNames()
		states, err = s.d.Mihomo.ProxiesState(ctx)
		if err != nil {
			// Ядро остановили между проверкой и запросом: фактов нет, это не сбой чтения
			if v2, _, e2 := s.runtimeSnapshot(); e2 == nil && !runtimeRunning(v2, v.Kernel) {
				return out, nil
			}
			return out, err
		}
	}
	for _, g := range am.groups.Items {
		if g.Mode != ModeAuto && g.Mode != ModeFallback {
			continue
		}
		now := GroupNow{}
		switch v.Kernel {
		case configlayer.KernelMihomo:
			if v.Modes.Mihomo != RuntimeModePanel {
				continue
			}
			if mh == nil {
				out.Groups[g.ID] = now
				continue
			}
			names := runtimeMihomoNames(mh, g)
			nodes := Resolve(g, am.cat.Mihomo, am.groups.Items).Nodes
			if n, ok := am.byRef[g.Reserve]; g.Mode == ModeFallback && ok {
				// В объединённом каталоге имя может быть пустым: берём вариант Mihomo.
				for _, mn := range am.cat.Mihomo {
					if mn.Ref == n.Ref {
						nodes = append(nodes, mn)
						names[mn.Ref] = mh.copyName[mn.Ref]
						break
					}
				}
			}
			name := states[g.Name].Now
			if g.Mode == ModeFallback && name == hiddenName(g.ID) {
				name = states[name].Now
			}
			// Цель известна, только если факт ядра различим: непустое имя
			// совпало ровно с одним узлом (повтор резервного узла — тот же Ref),
			// и в профиле это имя не носят разные узлы. Иначе «Сейчас» остаётся
			// неизвестным (D-16, D-26), а не последним совпадением по порядку.
			matched := map[string]bool{}
			for _, n := range nodes {
				if name != "" && name == names[n.Ref] {
					matched[n.Ref] = true
				}
				// История по имени, которое носят разные узлы, принадлежит
				// неизвестно какому из них: такой узел остаётся без истории.
				if h := states[names[n.Ref]].History; len(h) > 0 && !ambiguous[names[n.Ref]] {
					last := h[len(h)-1]
					out.History[n.Ref] = NowDelay{MS: last.Delay, At: last.Time}
				}
			}
			if len(matched) == 1 && !ambiguous[name] {
				for ref := range matched {
					now.Ref, now.Known = ref, true
				}
			}
		case configlayer.KernelXray:
			if v.Modes.Xray != RuntimeModePanel {
				continue
			}
			if s.d.XrayBalancer == nil {
				now.Fastest = true
			} else {
				target, known, err := s.d.XrayBalancer(ctx, GroupTag(g.ID))
				if err != nil {
					now.Fastest = true
				} else if known && target != "" {
					for _, n := range am.cat.Xray {
						if n.XrayTag == target {
							now.Ref, now.Known = n.Ref, true
							break
						}
					}
				}
			}
		default:
			continue
		}
		out.Groups[g.ID] = now
	}
	return out, nil
}

// PingRequest собирает одну пачку по совместимости и пробе применённого состояния.
func (s *Service) PingRequest(scope, groupID string) (proxyping.Request, error) {
	if scope != proxyping.ScopeAll && scope != proxyping.ScopeGroup {
		return proxyping.Request{}, &InvalidInputError{Field: "scope", Reason: "ожидалось all или group"}
	}
	// Пачка во время перезапуска взяла бы переходное ядро и кончилась бы
	// kernel_changed или start_failed; клиент получает 409 kernel_restarting.
	if s.d.InRestart != nil && s.d.InRestart() {
		return proxyping.Request{}, ErrKernelRestarting
	}
	v, am, err := s.runtimeSnapshot()
	if err != nil {
		return proxyping.Request{}, err
	}
	if v.State == RuntimeStateConflict || v.State == RuntimeStateUserProfile {
		return proxyping.Request{}, ErrPingUnavailable
	}
	req := proxyping.Request{
		Scope: scope, Group: groupID, Kernel: v.Kernel, Method: proxyping.MethodXray,
		URL: v.Probe.URL, Timeout: time.Duration(v.Probe.TimeoutS) * time.Second,
		Targets: []proxyping.Target{},
	}
	if runtimeRunning(v, configlayer.KernelMihomo) && v.Modes.Mihomo == RuntimeModePanel {
		req.Method = proxyping.MethodMihomo
	}
	xray := runtimeRefs(am.cat.Xray)
	mihomo := make(map[string]sources.CatalogNode, len(am.cat.Mihomo))
	for _, n := range am.cat.Mihomo {
		mihomo[n.Ref] = n
	}
	var mh *mihomoBuild
	if req.Method == proxyping.MethodMihomo {
		mh = runtimeMihomo(am)
		for ref, name := range mh.copyName {
			if name != "" {
				n := mihomo[ref]
				n.MihomoName = name
				mihomo[ref] = n
			}
		}
	}
	seen, found := map[string]bool{}, false
	for _, g := range v.Groups {
		if scope == proxyping.ScopeGroup && g.ID != groupID {
			continue
		}
		found = true
		for _, m := range g.Members {
			if m.Kind != "node" || seen[m.Ref] ||
				(v.Kernel == configlayer.KernelXray && !m.Xray) ||
				(v.Kernel == configlayer.KernelMihomo && !m.Mihomo) {
				continue
			}
			n := am.byRef[m.Ref]
			seen[m.Ref] = true
			t := proxyping.Target{
				Ref: n.Ref, Node: n.Node, MihomoName: mihomo[n.Ref].MihomoName,
				Xray: xray[n.Ref], Fingerprint: proxyping.Fingerprint(n.Node),
			}
			if mh != nil {
				t.MihomoProvider = mh.probeProvider(n.Ref, t.MihomoName)
			}
			req.Targets = append(req.Targets, t)
		}
	}
	if scope == proxyping.ScopeGroup {
		if !found {
			return proxyping.Request{}, ErrGroupNotFound
		}
		if len(req.Targets) == 0 {
			return proxyping.Request{}, ErrGroupNoNodes
		}
	}
	return req, nil
}

// NodeFingerprints позволяет раннеру отбрасывать результаты прежних параметров.
func (s *Service) NodeFingerprints() (map[string]string, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return nil, err
	}
	am, err := s.appliedModelOf(st)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(am.union))
	for _, n := range am.union {
		out[n.Ref] = proxyping.Fingerprint(n.Node)
	}
	return out, nil
}
