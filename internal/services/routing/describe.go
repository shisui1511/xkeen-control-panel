package routing

import (
	"encoding/json"
	"reflect"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Строки изменений секций маршрутизации (D-12): группы и правила — по
// items[].id, финальное правило и пресет — отдельными строками, проверка узлов —
// одной строкой, пометки geodata — по файлу. Секция, которая не разбирается или
// отличается только порядком элементов, строк не даёт: её покрывает запасная
// строка слоя.

// describerID — идентификатор описателя маршрутизации в реестре слоя.
const describerID = "routing_rows"

// routingDescriber — описатель секций groups, rules, probe и geodata: чистая
// функция над секциями, без ввода-вывода и замков.
type routingDescriber struct{}

// Describer возвращает описатель строк изменений для configlayer.Hooks.
func (s *Service) Describer() configlayer.Describer { return routingDescriber{} }

func (routingDescriber) ID() string { return describerID }

func (routingDescriber) Owns(section string) bool {
	switch section {
	case GroupsSection, RulesSection, ProbeSection, GeodataSection:
		return true
	}
	return false
}

func (routingDescriber) Describe(applied, draft configlayer.Sections) []configlayer.ChangeRow {
	var out []configlayer.ChangeRow
	if _, ok := configlayer.SectionChange(applied, draft, GroupsSection); ok {
		out = append(out, describeGroups(applied[GroupsSection], draft[GroupsSection])...)
	}
	if _, ok := configlayer.SectionChange(applied, draft, RulesSection); ok {
		out = append(out, describeRules(applied, draft)...)
	}
	if kind, ok := configlayer.SectionChange(applied, draft, ProbeSection); ok {
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabGroups, Kind: kind, Entity: configlayer.EntityProbe,
			ID: ProbeSection, Label: ProbeSection, Section: ProbeSection,
		})
	}
	if _, ok := configlayer.SectionChange(applied, draft, GeodataSection); ok {
		out = append(out, describeGeodata(applied[GeodataSection], draft[GeodataSection])...)
	}
	return out
}

// describeGroups — строка на группу, которая добавлена, удалена или изменена.
func describeGroups(appliedRaw, draftRaw json.RawMessage) []configlayer.ChangeRow {
	a, errA := parseGroups(appliedRaw)
	d, errD := parseGroups(draftRaw)
	if errA != nil || errD != nil {
		return nil
	}
	was := make(map[string]Group, len(a.Items))
	for _, g := range a.Items {
		was[g.ID] = g
	}
	var out []configlayer.ChangeRow
	row := func(kind string, g Group) {
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabGroups, Kind: kind, Entity: configlayer.EntityGroup,
			ID: g.ID, Label: g.Name, Section: GroupsSection,
		})
	}
	// Повторяющийся id в черновике или применённом не даёт повторных строк.
	seen := make(map[string]bool, len(d.Items))
	for _, g := range d.Items {
		if seen[g.ID] {
			continue
		}
		seen[g.ID] = true
		old, ok := was[g.ID]
		switch {
		case !ok:
			row(configlayer.ChangeAdded, g)
		case !reflect.DeepEqual(old, g):
			row(configlayer.ChangeChanged, g)
		}
	}
	for _, g := range a.Items {
		if !seen[g.ID] {
			seen[g.ID] = true
			row(configlayer.ChangeRemoved, g)
		}
	}
	return out
}

// describeRules — строка на правило по items[].id, строка финального правила и
// строка пресета.
func describeRules(applied, draft configlayer.Sections) []configlayer.ChangeRow {
	a, errA := parseRules(applied[RulesSection])
	d, errD := parseRules(draft[RulesSection])
	if errA != nil || errD != nil {
		return nil
	}
	was := make(map[string]Rule, len(a.Items))
	for _, r := range a.Items {
		was[r.ID] = r
	}
	var out []configlayer.ChangeRow
	row := func(kind string, r Rule) {
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabRules, Kind: kind, Entity: configlayer.EntityRule,
			ID: r.ID, Label: ruleSummary(r), Section: RulesSection,
		})
	}
	// Повторяющийся id в черновике или применённом не даёт повторных строк.
	seen := make(map[string]bool, len(d.Items))
	for _, r := range d.Items {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		old, ok := was[r.ID]
		switch {
		case !ok:
			row(configlayer.ChangeAdded, r)
		case !reflect.DeepEqual(old, r):
			row(configlayer.ChangeChanged, r)
		}
	}
	for _, r := range a.Items {
		if !seen[r.ID] {
			seen[r.ID] = true
			row(configlayer.ChangeRemoved, r)
		}
	}
	if a.Final != d.Final {
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabRules, Kind: configlayer.ChangeChanged, Entity: configlayer.EntityRulesFinal,
			ID: "final", Label: targetLabel(d.Final, draft[GroupsSection]), Section: RulesSection,
		})
	}
	if !reflect.DeepEqual(a.Preset, d.Preset) {
		kind, preset := configlayer.ChangeChanged, d.Preset
		switch {
		case a.Preset == nil:
			kind = configlayer.ChangeAdded
		case d.Preset == nil:
			kind, preset = configlayer.ChangeRemoved, a.Preset
		}
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabRules, Kind: kind, Entity: configlayer.EntityPreset,
			ID: preset.ID, Label: preset.ID, Section: RulesSection,
		})
	}
	return out
}

// targetLabel — имя цели правила для строки: имя группы черновика или вид цели
// (direct, reject); группа, которой в черновике нет, — её ID.
func targetLabel(t Target, groupsRaw json.RawMessage) string {
	if t.Kind != KindGroup {
		return t.Kind
	}
	if cfg, err := parseGroups(groupsRaw); err == nil {
		for _, g := range cfg.Items {
			if g.ID == t.ID {
				return g.Name
			}
		}
	}
	return t.ID
}

// describeGeodata — строка на пометку drop[], которой нет с другой стороны или
// у которой сменился режим.
func describeGeodata(appliedRaw, draftRaw json.RawMessage) []configlayer.ChangeRow {
	a, errA := ParseGeodataDraft(appliedRaw)
	d, errD := ParseGeodataDraft(draftRaw)
	if errA != nil || errD != nil {
		return nil
	}
	was := make(map[string]string, len(a.Drop))
	for _, x := range a.Drop {
		was[x.File] = x.Mode
	}
	var out []configlayer.ChangeRow
	row := func(kind, file string) {
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabLists, Kind: kind, Entity: configlayer.EntityGeodata,
			ID: file, Label: file, Section: GeodataSection,
		})
	}
	seen := make(map[string]bool, len(d.Drop))
	for _, x := range d.Drop {
		if seen[x.File] {
			continue
		}
		seen[x.File] = true
		mode, ok := was[x.File]
		switch {
		case !ok:
			row(configlayer.ChangeAdded, x.File)
		case mode != x.Mode:
			row(configlayer.ChangeChanged, x.File)
		}
	}
	for _, x := range a.Drop {
		if !seen[x.File] {
			seen[x.File] = true
			row(configlayer.ChangeRemoved, x.File)
		}
	}
	return out
}
