package routing

// Вид страницы «Прокси» по применённому состоянию и Live (фаза 150, PRX-01).
// Наружу уходят только ссылки, подписи и признаки совместимости узлов.

import (
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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
