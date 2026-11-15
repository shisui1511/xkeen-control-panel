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
	Kernel  string         `json:"kernel"`
	Running []string       `json:"running"`
	State   string         `json:"state"`
	Modes   RuntimeModes   `json:"modes"`
	Probe   ProbeConfig    `json:"probe"`
	Groups  []RuntimeGroup `json:"groups"`
}

type RuntimeModes struct {
	Xray   string `json:"xray"`
	Mihomo string `json:"mihomo"`
}

type RuntimeGroup struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`
	Current      string          `json:"current"`
	CurrentLabel string          `json:"current_label"`
	Chain        []RuntimeLink   `json:"chain"`
	Fallback     string          `json:"fallback,omitempty"`
	Selectable   bool            `json:"selectable"`
	Unconfirmed  bool            `json:"unconfirmed"`
	LockedWith   []string        `json:"locked_with"`
	Counts       KernelCounts    `json:"counts"`
	Members      []RuntimeMember `json:"members"`
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
	Xray   bool   `json:"xray"`
	Mihomo bool   `json:"mihomo"`
}

// runtimeSnapshot собирает все данные из одной версии состояния и каталога.
func (s *Service) runtimeSnapshot() (RuntimeView, appliedModel, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return RuntimeView{}, appliedModel{}, err
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
	v := RuntimeView{
		Kernel: active.Kernel, Running: append([]string{}, active.Running...),
		Probe: probe, Groups: []RuntimeGroup{},
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
		for _, g := range am.groups.Items {
			r := Resolve(g, am.union, am.groups.Items)
			rg := RuntimeGroup{
				ID: g.ID, Name: g.Name, Mode: string(g.Mode),
				Chain: []RuntimeLink{}, LockedWith: []string{}, Members: []RuntimeMember{},
				Selectable:  g.Mode == ModeManual && desired.AnyManaged(),
				Unconfirmed: unconfirmed[g.ID] != "",
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
	return v, am, nil
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
		Proto: n.Node.Protocol, Source: n.Source, Xray: xray[n.Ref], Mihomo: mihomo[n.Ref],
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
	if v.Kernel == configlayer.KernelMihomo && v.Modes.Mihomo == RuntimeModePanel && s.d.Mihomo != nil {
		states, err = s.d.Mihomo.ProxiesState(ctx)
		if err != nil {
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
			for _, n := range am.cat.Mihomo {
				if name := states[g.Name].Now; name != "" && n.MihomoName == name {
					now.Ref, now.Known = n.Ref, true
				}
			}
			nodes := Resolve(g, am.cat.Mihomo, am.groups.Items).Nodes
			if n, ok := am.byRef[g.Reserve]; g.Mode == ModeFallback && ok {
				// В объединённом каталоге имя может быть пустым: берём вариант Mihomo.
				for _, mn := range am.cat.Mihomo {
					if mn.Ref == n.Ref {
						nodes = append(nodes, mn)
						break
					}
				}
			}
			for _, n := range nodes {
				if h := states[n.MihomoName].History; len(h) > 0 {
					last := h[len(h)-1]
					out.History[n.Ref] = NowDelay{MS: last.Delay, At: last.Time}
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
			req.Targets = append(req.Targets, proxyping.Target{
				Ref: n.Ref, Node: n.Node, MihomoName: mihomo[n.Ref].MihomoName,
				Xray: xray[n.Ref], Fingerprint: proxyping.Fingerprint(n.Node),
			})
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
