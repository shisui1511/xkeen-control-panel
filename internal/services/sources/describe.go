package sources

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Строки изменений секций источников (D-12): подписка — одна строка на
// источник, «Мои узлы» — строка на каждый добавленный, удалённый или изменённый
// узел. В Label — только имена: ссылки, адреса и их части туда не попадают.

// describerID — идентификатор описателя источников в реестре слоя.
const describerID = "sources_rows"

// sourcesDescriber — описатель секций src_<id> и src_my: чистая функция над
// секциями, без ввода-вывода и замков.
type sourcesDescriber struct{}

// Describer возвращает описатель строк изменений для configlayer.Hooks.
func (s *Service) Describer() configlayer.Describer { return sourcesDescriber{} }

func (sourcesDescriber) ID() string { return describerID }

func (sourcesDescriber) Owns(section string) bool { return strings.HasPrefix(section, SectionPrefix) }

func (sourcesDescriber) Describe(applied, draft configlayer.Sections) []configlayer.ChangeRow {
	names := map[string]bool{}
	for name := range draft {
		names[name] = true
	}
	for name := range applied {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		if strings.HasPrefix(name, SectionPrefix) {
			sorted = append(sorted, name)
		}
	}
	sort.Strings(sorted)

	var out []configlayer.ChangeRow
	for _, name := range sorted {
		kind, ok := configlayer.SectionChange(applied, draft, name)
		if !ok {
			continue
		}
		if name == MySection {
			out = append(out, describeMy(applied[name], draft[name])...)
			continue
		}
		id, ok := sourceID(name)
		if !ok {
			// Имя вне алфавита источников: строку даст запасной счёт слоя.
			continue
		}
		raw := draft[name]
		if kind == configlayer.ChangeRemoved {
			raw = applied[name]
		}
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabNodes, Kind: kind, Entity: configlayer.EntitySource,
			ID: id, Label: subscriptionLabel(id, raw), Section: name,
		})
	}
	return out
}

// subscriptionLabel — имя подписки из секции; без имени или при порче секции —
// идентификатор источника (URL в строку не попадает).
func subscriptionLabel(id string, raw json.RawMessage) string {
	var head struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &head); err == nil && strings.TrimSpace(head.Name) != "" {
		return head.Name
	}
	return id
}

// describeMy — строка на каждый элемент entries[] «Моих узлов», который
// добавлен, удалён или изменён, по его id. Если узлы те же, а секция изменилась
// (включение источника), — одна строка источника «Мои узлы». Секция не
// разбирается — строк нет, её покрывает запасная строка слоя.
func describeMy(appliedRaw, draftRaw json.RawMessage) []configlayer.ChangeRow {
	a, errA := parseMy(appliedRaw)
	d, errD := parseMy(draftRaw)
	if errA != nil || errD != nil {
		return nil
	}
	was := make(map[string]MyEntry, len(a.Entries))
	for _, e := range a.Entries {
		was[e.ID] = e
	}
	var out []configlayer.ChangeRow
	row := func(kind string, e MyEntry) {
		label := e.Name
		if strings.TrimSpace(label) == "" {
			label = e.ID
		}
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabNodes, Kind: kind, Entity: configlayer.EntityMyNode,
			ID: e.ID, Label: label, Section: MySection,
		})
	}
	seen := make(map[string]bool, len(d.Entries))
	for _, e := range d.Entries {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		old, ok := was[e.ID]
		switch {
		case !ok:
			row(configlayer.ChangeAdded, e)
		case old != e:
			row(configlayer.ChangeChanged, e)
		}
	}
	for _, e := range a.Entries {
		if !seen[e.ID] {
			seen[e.ID] = true
			row(configlayer.ChangeRemoved, e)
		}
	}
	if len(out) == 0 && a.Enabled != d.Enabled {
		out = append(out, configlayer.ChangeRow{
			Tab: configlayer.TabNodes, Kind: configlayer.ChangeChanged, Entity: configlayer.EntitySource,
			ID: MySourceID, Label: MySourceID, Section: MySection,
		})
	}
	return out
}
