package routing

import (
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// KernelCounts — число узлов группы по ядрам. nil — ядро не установлено (или
// список узлов не удалось собрать: ошибку источника сообщают его собственные
// проверки).
type KernelCounts struct {
	Xray   *int `json:"xray"`
	Mihomo *int `json:"mihomo"`
}

// MemberOption — вариант участника для строки «Сейчас» ручной группы.
type MemberOption struct {
	// Member — "group:<id>", "direct", "reject" или "node:<ref>".
	Member string `json:"member"`
	// Label — имя группы, DIRECT, REJECT или имя узла.
	Label string `json:"label"`
}

// GroupView — группа в представлении: поля группы, состав по ядрам, проблемы и
// предупреждения и варианты участника. Текущий выбор (current, current_label)
// добавит 146-15.
type GroupView struct {
	Group
	Counts        KernelCounts   `json:"counts"`
	Issues        []IssueView    `json:"issues"`
	Warnings      []IssueView    `json:"warnings"`
	MemberOptions []MemberOption `json:"member_options"`
}

// RuleView — правило в представлении: поля правила, номер с 1, ядро, для
// которого оно существует, проблемы и предупреждения. В предупреждениях — и
// проверки качества правил (exact_dup, overlap, below_catchall с номером
// перекрывающего правила); «Применить» они не блокируют.
type RuleView struct {
	Rule
	Index int `json:"index"`
	// Only — "" (правило для обоих ядер), "xray" или "mihomo": bittorrent есть
	// только в Xray, своё правило ядра — только в своём ядре.
	Only     string      `json:"only"`
	Issues   []IssueView `json:"issues"`
	Warnings []IssueView `json:"warnings"`
}

// FeatureView — доступность функции на установленной версии ядра.
type FeatureView struct {
	// Status — available, unavailable или undetermined.
	Status string `json:"status"`
	// Min — минимальная версия ядра для функции.
	Min string `json:"min"`
}

// RoutingView — представление маршрутизации для API и вкладок «Группы» и
// «Правила». Секретов узлов в нём нет: только имена и ссылки.
type RoutingView struct {
	// Revision — ревизия черновика, на которой построено представление.
	Revision int64 `json:"revision"`
	// Installed — какие ядра установлены ("xray", "mihomo").
	Installed map[string]bool `json:"installed"`
	// Desired — желаемое routing.managed черновика.
	Desired bool `json:"desired"`
	// HasSources — в черновике есть хотя бы один источник.
	HasSources bool `json:"has_sources"`
	// DefaultsDone — группы по умолчанию уже создавались.
	DefaultsDone bool        `json:"defaults_done"`
	Groups       []GroupView `json:"groups"`
	Rules        []RuleView  `json:"rules"`
	// Final — цель «Остального».
	Final Target `json:"final"`
	// Probe — параметры проверки узлов; ProbeDefaults — их значения по умолчанию
	// для архитектуры.
	Probe         ProbeConfig `json:"probe"`
	ProbeDefaults ProbeConfig `json:"probe_defaults"`
	// Issues — проблемы проверки, посчитанные для черновика всегда. При
	// managed=false клиент показывает их предупреждениями: они не блокируют
	// «Применить только узлы» (D-01).
	Issues []IssueView `json:"issues"`
	// Features — доступность функций групп по версиям установленных ядер.
	Features map[string]FeatureView `json:"features"`
}

// ruleOnly определяет, для какого ядра существует правило.
func ruleOnly(r Rule) string {
	if r.Raw != nil {
		switch r.Raw.Kernel {
		case configlayer.KernelXray, configlayer.KernelMihomo:
			return r.Raw.Kernel
		}
	}
	for _, c := range r.Conds {
		if c.Type == CondBittorrent {
			return configlayer.KernelXray
		}
	}
	return ""
}

// byKey раскладывает проблемы по ключу места; проблемы без ключа остаются
// только в общем списке.
func byKey(list []IssueView) map[string][]IssueView {
	out := make(map[string][]IssueView, len(list))
	for _, is := range list {
		if is.Key != "" {
			out[is.Key] = append(out[is.Key], is)
		}
	}
	return out
}

// orEmpty заменяет nil пустым срезом: в JSON нет null.
func orEmpty(list []IssueView) []IssueView {
	if list == nil {
		return []IssueView{}
	}
	return list
}

// optCount возвращает число для установленного ядра и nil для остальных.
func optCount(on bool, n int) *int {
	if !on {
		return nil
	}
	return &n
}

// memberOptions строит варианты участника ручной группы: сначала участники
// группы по порядку (группы, DIRECT, REJECT), затем узлы Xray и Mihomo без
// повторов. У «авто» и «резерва» вариантов нет.
func memberOptions(g Group, members []Member, groups []Group, xray, mihomo []sources.CatalogNode) []MemberOption {
	out := []MemberOption{}
	if g.Mode != ModeManual {
		return out
	}
	names := make(map[string]string, len(groups))
	for _, x := range groups {
		names[x.ID] = x.Name
	}
	for _, m := range members {
		switch m.Kind {
		case KindGroup:
			out = append(out, MemberOption{Member: "group:" + m.ID, Label: names[m.ID]})
		case KindDirect:
			out = append(out, MemberOption{Member: KindDirect, Label: "DIRECT"})
		case KindReject:
			out = append(out, MemberOption{Member: KindReject, Label: "REJECT"})
		}
	}
	seen := map[string]bool{}
	for _, list := range [][]sources.CatalogNode{xray, mihomo} {
		for _, n := range list {
			if seen[n.Ref] {
				continue
			}
			seen[n.Ref] = true
			out = append(out, MemberOption{Member: "node:" + n.Ref, Label: n.Display})
		}
	}
	return out
}

// featureViews считает доступность функций групп: функции Xray — по версии Xray,
// Mihomo — по версии Mihomo; неустановленное ядро — undetermined.
func (s *Service) featureViews(installed configlayer.InstalledKernels) map[string]FeatureView {
	table := []struct {
		kernel  string
		on      bool
		feature configlayer.Feature
	}{
		{configlayer.KernelXray, installed.Xray, configlayer.FeatureXrayAutoGroup},
		{configlayer.KernelXray, installed.Xray, configlayer.FeatureXrayFallbackGroup},
		{configlayer.KernelMihomo, installed.Mihomo, configlayer.FeatureMihomoGroups},
	}
	out := make(map[string]FeatureView, len(table))
	for _, f := range table {
		status := configlayer.Undetermined
		if f.on {
			status = configlayer.FeatureAvailable(f.kernel, s.d.KernelVersion(f.kernel), f.feature)
		}
		out[string(f.feature)] = FeatureView{Status: string(status), Min: configlayer.FeatureMinVersion(f.feature)}
	}
	return out
}

// View строит представление маршрутизации по черновику. Состав группы считает
// Resolve, проблемы и предупреждения — analyze, те же, что у проверки до записи.
// Испорченная секция groups, rules или probe не роняет представление: секция
// отдаётся пустой или по умолчанию, а проблема section_invalid — в Issues.
func (s *Service) View() (RoutingView, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return RoutingView{}, err
	}
	in := inputOf(st)
	installed := s.d.Installed()
	issues, warnings := analyze(in, installed, s.d.Sources.Catalog, s.d.KernelVersion)
	issuesByKey, warningsByKey := byKey(issues), byKey(warnings)

	groups, gerr := parseGroups(st.Draft[GroupsSection])
	if gerr != nil {
		groups = GroupsConfig{V: sectionVersion, Items: []Group{}}
	}
	rules, rerr := parseRules(st.Draft[RulesSection])
	if rerr != nil {
		rules = RulesConfig{V: sectionVersion, Items: []Rule{}, Final: Target{Kind: KindDirect}}
	}
	probe, perr := parseProbe(st.Draft[ProbeSection], s.d.GOARCH)
	if perr != nil {
		probe = defaultProbe(s.d.GOARCH)
	}

	var cat sources.Catalog
	haveCat := false
	if installed.Xray || installed.Mihomo {
		if c, err := s.d.Sources.Catalog(in, installed); err == nil {
			cat, haveCat = c, true
		}
	}
	var xrayNodes, mihomoNodes []sources.CatalogNode
	if installed.Xray {
		xrayNodes = cat.Xray
	}
	if installed.Mihomo {
		mihomoNodes = cat.Mihomo
	}

	view := RoutingView{
		Revision:      st.DraftRevision,
		Installed:     map[string]bool{configlayer.KernelXray: installed.Xray, configlayer.KernelMihomo: installed.Mihomo},
		Desired:       configlayer.DesiredRouting(st.Draft).Managed,
		HasSources:    len(draftSourceIDs(st.Draft)) > 0,
		DefaultsDone:  groups.DefaultsDone,
		Groups:        make([]GroupView, 0, len(groups.Items)),
		Rules:         make([]RuleView, 0, len(rules.Items)),
		Final:         rules.Final,
		Probe:         probe,
		ProbeDefaults: defaultProbe(s.d.GOARCH),
		Issues:        orEmpty(issues),
		Features:      s.featureViews(installed),
	}

	for _, g := range groups.Items {
		xr := Resolve(g, xrayNodes, groups.Items)
		mr := Resolve(g, mihomoNodes, groups.Items)
		var counts KernelCounts
		if haveCat {
			counts = KernelCounts{Xray: optCount(installed.Xray, len(xr.Nodes)), Mihomo: optCount(installed.Mihomo, len(mr.Nodes))}
		}
		key := "group:" + g.ID
		view.Groups = append(view.Groups, GroupView{
			Group:         g,
			Counts:        counts,
			Issues:        orEmpty(issuesByKey[key]),
			Warnings:      orEmpty(warningsByKey[key]),
			MemberOptions: memberOptions(g, xr.Members, groups.Items, xrayNodes, mihomoNodes),
		})
	}
	lint := lintRules(rules.Items)
	for i, r := range rules.Items {
		key := "rule:" + r.ID
		warns := append(append([]IssueView{}, warningsByKey[key]...), lint[r.ID]...)
		view.Rules = append(view.Rules, RuleView{
			Rule:     r,
			Index:    i + 1,
			Only:     ruleOnly(r),
			Issues:   orEmpty(issuesByKey[key]),
			Warnings: warns,
		})
	}
	return view, nil
}
