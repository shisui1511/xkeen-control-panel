package routing

import (
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Виды объектов, для которых считается Impact.
const (
	ImpactGroup  = "group"
	ImpactRule   = "rule"
	ImpactSource = "source"
	ImpactNode   = "node"
)

// Роли группы в зависимости от удаляемого объекта (GroupRef.Role).
const (
	// RoleMember — удаляемая группа входит в состав ручной группы.
	RoleMember = "member"
	// RoleComposition — источник или узел входит в состав группы.
	RoleComposition = "composition"
	// RoleReserve — узел служит резервным узлом группы «резерв».
	RoleReserve = "reserve"
)

// summaryRawRule — краткое описание «своего правила ядра»: текст правила в
// описание не попадает.
const summaryRawRule = "своё правило ядра"

// maxSummaryValues — сколько значений первого условия показывает краткое условие.
const maxSummaryValues = 3

// RuleRef — правило, которое ссылается на удаляемый объект.
type RuleRef struct {
	ID string `json:"id"`
	// Index — номер правила в списке, с 1.
	Index int `json:"index"`
	// Summary — краткое условие: первые значения первого условия.
	Summary string `json:"summary"`
	Target  Target `json:"target"`
}

// GroupRef — группа, затронутая удалением.
type GroupRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Role — member, composition или reserve.
	Role string `json:"role"`
}

// BalancerLoss — потеря узлов группой Xray: сколько узлов она теряет.
type BalancerLoss struct {
	Group string `json:"group"`
	Lost  int    `json:"lost"`
}

// MemberLoss — потеря участника группой Mihomo.
type MemberLoss struct {
	Group  string `json:"group"`
	Member string `json:"member"`
}

// KernelImpact — последствия удаления по ядрам; пустой срез — ядро не затронуто
// или не установлено.
type KernelImpact struct {
	Xray   []BalancerLoss `json:"xray"`
	Mihomo []MemberLoss   `json:"mihomo"`
}

// Impact — что затронет удаление группы, правила, источника или узла (D-34,
// D-35). Секретов узлов в нём нет: только имена и ссылки.
type Impact struct {
	// Kind — group, rule, source или node.
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Rules — правила с целью на удаляемую группу.
	Rules []RuleRef `json:"rules"`
	// Groups — группы, из состава которых уйдёт удаляемое.
	Groups []GroupRef `json:"groups"`
	// Final — на удаляемую группу ссылается «Остальное».
	Final bool `json:"final"`
	// Empties — группы, которые после удаления станут пустыми для всех
	// установленных ядер.
	Empties []GroupRef   `json:"empties"`
	Kernels KernelImpact `json:"kernels"`
}

// newImpact — пустой Impact: срезы всегда [], не null.
func newImpact(kind, id string) Impact {
	return Impact{
		Kind: kind, ID: id,
		Rules: []RuleRef{}, Groups: []GroupRef{}, Empties: []GroupRef{},
		Kernels: KernelImpact{Xray: []BalancerLoss{}, Mihomo: []MemberLoss{}},
	}
}

// ruleSummary — краткое условие правила для диалога каскада: тип и первые
// значения первого условия.
func ruleSummary(r Rule) string {
	if r.Raw != nil {
		return summaryRawRule
	}
	if len(r.Conds) == 0 {
		return ""
	}
	c := r.Conds[0]
	vals := make([]string, 0, len(c.Values)+len(c.Lists))
	vals = append(vals, c.Values...)
	for _, l := range c.Lists {
		vals = append(vals, l.File+":"+l.Tag)
	}
	more := ""
	if len(vals) > maxSummaryValues {
		vals, more = vals[:maxSummaryValues], "…"
	}
	out := c.Type
	if len(vals) > 0 {
		out += ": " + strings.Join(vals, ", ") + more
	}
	if len(r.Conds) > 1 {
		out += " +" + strconv.Itoa(len(r.Conds)-1)
	}
	return out
}

// groupRefs — зависимости от удаляемой группы в секциях: правила с целью на
// неё, ручные группы, где она участник, «Остальное».
type groupRefs struct {
	rules  []RuleRef
	groups []GroupRef
	final  bool
}

func (r groupRefs) any() bool { return len(r.rules) > 0 || len(r.groups) > 0 || r.final }

// refsOfGroup собирает ссылки на группу id.
func refsOfGroup(groups []Group, rules RulesConfig, id string) groupRefs {
	var out groupRefs
	for i, r := range rules.Items {
		if r.Target.Kind == KindGroup && r.Target.ID == id {
			out.rules = append(out.rules, RuleRef{ID: r.ID, Index: i + 1, Summary: ruleSummary(r), Target: r.Target})
		}
	}
	out.final = rules.Final.Kind == KindGroup && rules.Final.ID == id
	for _, g := range groups {
		if g.ID == id || g.Mode != ModeManual {
			continue
		}
		for _, m := range g.Members {
			if m.Kind == KindGroup && m.ID == id {
				out.groups = append(out.groups, GroupRef{ID: g.ID, Name: g.Name, Role: RoleMember})
				break
			}
		}
	}
	return out
}

// kernelNodes — узлы установленного ядра для расчёта последствий.
type kernelNodes struct {
	name  string
	nodes []sources.CatalogNode
}

// installedKernelNodes отдаёт узлы каталога по установленным ядрам.
func installedKernelNodes(cat sources.Catalog, installed configlayer.InstalledKernels) []kernelNodes {
	var out []kernelNodes
	if installed.Xray {
		out = append(out, kernelNodes{configlayer.KernelXray, cat.Xray})
	}
	if installed.Mihomo {
		out = append(out, kernelNodes{configlayer.KernelMihomo, cat.Mihomo})
	}
	return out
}

// emptyResolved — у группы нет ни узлов, ни участников.
func emptyResolved(r Resolved) bool { return len(r.Nodes) == 0 && len(r.Members) == 0 }

// becomesEmpty сообщает, что группа до изменения не была пустой хотя бы для
// одного ядра, а после станет пустой для всех установленных.
func becomesEmpty(g Group, kernels []kernelNodes, before, after []Group, nodesAfter func(kernelNodes) []sources.CatalogNode) bool {
	if len(kernels) == 0 {
		return false
	}
	wasFull := false
	for _, k := range kernels {
		if !emptyResolved(Resolve(g, k.nodes, before)) {
			wasFull = true
		}
		if !emptyResolved(Resolve(g, nodesAfter(k), after)) {
			return false
		}
	}
	return wasFull
}

// Impact показывает, что затронет удаление группы, правила, источника или узла
// «Моих узлов» (kind: group, rule, source, node), по черновику. Ничего не
// меняет.
func (s *Service) Impact(kind, id string) (Impact, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return Impact{}, err
	}
	return s.impactOf(st, kind, id)
}

// impactOf считает Impact по состоянию st.
func (s *Service) impactOf(st configlayer.StateView, kind, id string) (Impact, error) {
	switch kind {
	case ImpactGroup, ImpactRule, ImpactSource, ImpactNode:
	default:
		return Impact{}, invalid("kind", reasonOutOfRange)
	}
	out := newImpact(kind, id)
	groups, err := parseGroups(st.Draft[GroupsSection])
	if err != nil {
		return Impact{}, err
	}
	rules, err := parseRules(st.Draft[RulesSection])
	if err != nil {
		return Impact{}, err
	}

	switch kind {
	case ImpactRule:
		if ruleIndex(rules.Items, id) < 0 {
			return Impact{}, ErrRuleNotFound
		}
		return out, nil
	case ImpactGroup:
		return s.impactGroup(st, out, groups.Items, rules)
	case ImpactSource:
		if _, ok := st.Draft[sources.SectionPrefix+id]; !ok {
			return Impact{}, sources.ErrSourceNotFound
		}
		return s.impactNodes(st, out, groups.Items, sourceScope{source: id})
	default:
		if _, err := s.d.Sources.NodeRaw(id); err != nil {
			return Impact{}, err
		}
		return s.impactNodes(st, out, groups.Items, sourceScope{ref: nodes.Ref(sources.MySourceID, id)})
	}
}

// catalogOf собирает каталог черновика; ошибка каталога (её сообщают проверки
// источников) оставляет последствия по ядрам пустыми.
func (s *Service) catalogOf(st configlayer.StateView) ([]kernelNodes, sources.Catalog, bool) {
	installed := s.d.Installed()
	if !installed.Xray && !installed.Mihomo {
		return nil, sources.Catalog{}, false
	}
	cat, err := s.d.Sources.Catalog(inputOf(st), installed)
	if err != nil {
		return nil, sources.Catalog{}, false
	}
	return installedKernelNodes(cat, installed), cat, true
}

// impactGroup считает последствия удаления группы. Для Xray — узлы, которые
// теряет balancer удаляемой группы «авто»/«резерв»; для Mihomo — ручные группы,
// которые теряют участника.
func (s *Service) impactGroup(st configlayer.StateView, out Impact, groups []Group, rules RulesConfig) (Impact, error) {
	idx := -1
	for i := range groups {
		if groups[i].ID == out.ID {
			idx = i
		}
	}
	if idx < 0 {
		return Impact{}, ErrGroupNotFound
	}
	del := groups[idx]
	refs := refsOfGroup(groups, rules, out.ID)
	out.Rules, out.Final = append(out.Rules, refs.rules...), refs.final
	out.Groups = append(out.Groups, refs.groups...)

	kernels, _, ok := s.catalogOf(st)
	if !ok {
		return out, nil
	}
	after := groupsWithout(groups, out.ID)
	for _, k := range kernels {
		switch k.name {
		case configlayer.KernelXray:
			if del.Mode != ModeManual {
				if n := len(Resolve(del, k.nodes, groups).Nodes); n > 0 {
					out.Kernels.Xray = append(out.Kernels.Xray, BalancerLoss{Group: del.Name, Lost: n})
				}
			}
		case configlayer.KernelMihomo:
			for _, ref := range refs.groups {
				out.Kernels.Mihomo = append(out.Kernels.Mihomo, MemberLoss{Group: ref.Name, Member: del.Name})
			}
		}
	}
	same := func(k kernelNodes) []sources.CatalogNode { return k.nodes }
	for _, ref := range refs.groups {
		for _, g := range after {
			if g.ID == ref.ID && becomesEmpty(g, kernels, groups, after, same) {
				out.Empties = append(out.Empties, GroupRef{ID: g.ID, Name: g.Name, Role: RoleMember})
			}
		}
	}
	return out, nil
}

// groupsWithout возвращает группы без группы id; ссылки на неё в составах
// ручных групп отбрасывает Resolve (участник, которого нет в списке).
func groupsWithout(groups []Group, id string) []Group {
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		if g.ID != id {
			out = append(out, g)
		}
	}
	return out
}

// sourceScope — что удаляется из каталога: источник целиком (source) или один
// узел по ссылке (ref).
type sourceScope struct {
	source string
	ref    string
}

// hasRef сообщает, относится ли ссылка узла к удаляемому.
func (sc sourceScope) hasRef(ref string) bool {
	if sc.source != "" {
		return strings.HasPrefix(ref, sc.source+"/")
	}
	return ref == sc.ref
}

// hasNode сообщает, относится ли узел каталога к удаляемому.
func (sc sourceScope) hasNode(n sources.CatalogNode) bool {
	if sc.source != "" {
		return n.Source == sc.source
	}
	return n.Ref == sc.ref
}

// groupRoles находит роли группы по удаляемому: состав (источник в списке
// источников или отдельный узел) и резерв.
func (sc sourceScope) groupRoles(g Group) []string {
	var roles []string
	composed := false
	if sc.source != "" {
		for _, id := range g.Sources.IDs {
			if id == sc.source {
				composed = true
			}
		}
	}
	for _, ref := range g.Picks {
		if sc.hasRef(ref) {
			composed = true
		}
	}
	if composed {
		roles = append(roles, RoleComposition)
	}
	if g.Reserve != "" && sc.hasRef(g.Reserve) {
		roles = append(roles, RoleReserve)
	}
	return roles
}

// impactNodes считает последствия удаления источника или узла: группы со
// ссылками, потери узлов по ядрам и группы, которые станут пустыми.
func (s *Service) impactNodes(st configlayer.StateView, out Impact, groups []Group, sc sourceScope) (Impact, error) {
	for _, g := range groups {
		for _, role := range sc.groupRoles(g) {
			out.Groups = append(out.Groups, GroupRef{ID: g.ID, Name: g.Name, Role: role})
		}
	}
	kernels, cat, ok := s.catalogOf(st)
	if !ok {
		return out, nil
	}
	nodesAfter := func(k kernelNodes) []sources.CatalogNode {
		kept := make([]sources.CatalogNode, 0, len(k.nodes))
		for _, n := range k.nodes {
			if !sc.hasNode(n) {
				kept = append(kept, n)
			}
		}
		return kept
	}
	label := sc.ref
	if sc.source != "" {
		label = sc.source
		for _, src := range cat.Sources {
			if src.ID == sc.source && src.Name != "" {
				label = src.Name
			}
		}
	}
	for _, k := range kernels {
		after := nodesAfter(k)
		// Для узла участник — его имя в списке ядра; для источника — имя источника.
		member := label
		if sc.source == "" {
			for _, n := range k.nodes {
				if sc.hasNode(n) {
					member = n.Display
					break
				}
			}
		}
		for _, g := range groups {
			lost := len(Resolve(g, k.nodes, groups).Nodes) - len(Resolve(g, after, groups).Nodes)
			if lost <= 0 {
				continue
			}
			switch k.name {
			case configlayer.KernelXray:
				out.Kernels.Xray = append(out.Kernels.Xray, BalancerLoss{Group: g.Name, Lost: lost})
			case configlayer.KernelMihomo:
				out.Kernels.Mihomo = append(out.Kernels.Mihomo, MemberLoss{Group: g.Name, Member: member})
			}
		}
	}
	for _, g := range groups {
		if becomesEmpty(g, kernels, groups, groups, nodesAfter) {
			role := RoleComposition
			if roles := sc.groupRoles(g); len(roles) > 0 {
				role = roles[0]
			}
			out.Empties = append(out.Empties, GroupRef{ID: g.ID, Name: g.Name, Role: role})
		}
	}
	return out, nil
}
