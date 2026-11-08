package configlayer

import (
	"log"
	"sort"
	"sync"
)

// Строки изменений черновика (D-12, CFG-02): число неприменённых изменений и
// окно «Изменения перед применением» говорят на языке сущностей (подписка, узел,
// группа, правило, секция ядра), а не секций состояния. Обе стороны берут строки
// из одного реестра описателей, поэтому их число совпадает по построению.
//
// Описатель — чистая функция над секциями: без чтения диска, без обращения к
// Layer и без замков. Реестр вызывается под замком хранилища.

// Вкладки, к которым относится строка изменения.
const (
	TabNodes  = "nodes"
	TabGroups = "groups"
	TabRules  = "rules"
	TabLists  = "lists"
	TabCores  = "cores"
	TabOwner  = "owner"
)

// Виды изменения.
const (
	ChangeAdded   = "added"
	ChangeChanged = "changed"
	ChangeRemoved = "removed"
)

// Сущности строк изменений. Для EntitySectionOwner и EntityRoutingOwner
// ChangeAdded — взять под управление панели, ChangeRemoved — отпустить.
const (
	EntitySource       = "source"
	EntityMyNode       = "my_node"
	EntityGroup        = "group"
	EntityRule         = "rule"
	EntityRulesFinal   = "rules_final"
	EntityProbe        = "probe"
	EntityPreset       = "preset"
	EntityGeodata      = "geodata"
	EntitySection      = "section"
	EntitySectionOwner = "section_owner"
	EntityRoutingOwner = "routing_owner"
	EntityDNSTakeover  = "dns_takeover"
	EntityDiag         = "diag"
	EntityOther        = "other"
)

// ChangeRow — строка изменения черновика относительно применённого состояния.
// Label — имя как в интерфейсе (подписка, узел, группа, идентификатор секции):
// ссылок, ключей и других секретов в нём нет.
type ChangeRow struct {
	Tab     string `json:"tab"`
	Kind    string `json:"kind"`
	Entity  string `json:"entity"`
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kernel  string `json:"kernel,omitempty"`
	Section string `json:"section"`
}

// Describer — описатель секций черновика: превращает расхождение черновика и
// применённого состояния в строки изменений. Owns сообщает, какие секции он
// описывает; Describe получает секции целиком и возвращает строки только своих
// секций. Описатель вызывается под замком хранилища: без ввода-вывода, без
// замков и без обращения к слою.
type Describer interface {
	ID() string
	Owns(section string) bool
	Describe(applied, draft Sections) []ChangeRow
}

// SectionChange сообщает, как секция name черновика отличается от применённой:
// ChangeAdded — есть только в черновике, ChangeRemoved — только в применённом,
// ChangeChanged — значения не равны (SectionEqual). ok=false — секции равны или
// её нет с обеих сторон.
func SectionChange(applied, draft Sections, name string) (kind string, ok bool) {
	a, inApplied := applied[name]
	d, inDraft := draft[name]
	switch {
	case inDraft && !inApplied:
		return ChangeAdded, true
	case inApplied && !inDraft:
		return ChangeRemoved, true
	case inDraft && !SectionEqual(d, a):
		return ChangeChanged, true
	}
	return "", false
}

// tabOrder — порядок вкладок в списке строк.
var tabOrder = map[string]int{
	TabNodes: 0, TabGroups: 1, TabRules: 2, TabLists: 3, TabCores: 4, TabOwner: 5,
}

// describeRegistry — описатели строк изменений. Список меняется только до Start
// слоя, но правки черновика возможны и раньше, поэтому он под своим замком
// (порядок замков: замок хранилища, затем mu реестра).
type describeRegistry struct {
	mu   sync.RWMutex
	list []Describer
}

// has сообщает, занят ли идентификатор описателя.
func (r *describeRegistry) has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.list {
		if d.ID() == id {
			return true
		}
	}
	return false
}

// add добавляет описатель в конец списка.
func (r *describeRegistry) add(d Describer) {
	r.mu.Lock()
	r.list = append(r.list, d)
	r.mu.Unlock()
}

// rows собирает строки изменений черновика st. Каждая секция, где черновик и
// применённое расходятся, но ни одна строка на неё не ссылается (описателя нет,
// он вернул пустой ответ или упал), даёт одну запасную строку EntityOther:
// строк больше нуля тогда и только тогда, когда черновик отличается. Порядок —
// по вкладкам (nodes, groups, rules, lists, cores, owner), внутри — по ID.
func (r *describeRegistry) rows(st State) []ChangeRow {
	changed := changedSections(st)
	if len(changed) == 0 {
		return []ChangeRow{}
	}
	r.mu.RLock()
	list := append([]Describer(nil), r.list...)
	r.mu.RUnlock()

	out := []ChangeRow{}
	for _, d := range list {
		if !ownsAny(d, changed) {
			continue
		}
		out = append(out, safeDescribe(d, st.Applied, st.Draft)...)
	}
	covered := make(map[string]bool, len(out))
	for _, row := range out {
		covered[row.Section] = true
	}
	for name, kind := range changed {
		if covered[name] {
			continue
		}
		out = append(out, ChangeRow{Tab: TabCores, Kind: kind, Entity: EntityOther, ID: name, Label: name, Section: name})
	}
	sortChangeRows(out)
	return out
}

// changedSections — секции, где черновик и применённое расходятся, с видом
// изменения.
func changedSections(st State) map[string]string {
	out := map[string]string{}
	for name := range st.Draft {
		if kind, ok := SectionChange(st.Applied, st.Draft, name); ok {
			out[name] = kind
		}
	}
	for name := range st.Applied {
		if _, ok := st.Draft[name]; !ok {
			out[name] = ChangeRemoved
		}
	}
	return out
}

// ownsAny — описатель отвечает хотя бы за одну изменённую секцию.
func ownsAny(d Describer, changed map[string]string) bool {
	for name := range changed {
		if d.Owns(name) {
			return true
		}
	}
	return false
}

// safeDescribe вызывает описатель, не давая его панике оставить замок
// хранилища занятым: упавший описатель строк не даёт, его секции получают
// запасные строки. Строки чужих секций отбрасываются.
func safeDescribe(d Describer, applied, draft Sections) (rows []ChangeRow) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("[configlayer] описатель %s упал: %v", d.ID(), p)
			rows = nil
		}
	}()
	for _, row := range d.Describe(applied, draft) {
		if d.Owns(row.Section) {
			rows = append(rows, row)
		}
	}
	return rows
}

// sortChangeRows упорядочивает строки по вкладке, затем по ID; равные ID —
// по секции, сущности и виду, чтобы порядок не зависел от обхода карт.
func sortChangeRows(rows []ChangeRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ta, tb := tabRank(a.Tab), tabRank(b.Tab); ta != tb {
			return ta < tb
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		if a.Entity != b.Entity {
			return a.Entity < b.Entity
		}
		return a.Kind < b.Kind
	})
}

// tabRank — место вкладки в порядке показа; неизвестная вкладка — в конце.
func tabRank(tab string) int {
	if n, ok := tabOrder[tab]; ok {
		return n
	}
	return len(tabOrder)
}

// builtinDescribers — описатели секций, формат которых знает сам слой: секции
// ядер, владение маршрутизацией, перехват DNS и диагностика.
func builtinDescribers() []Describer {
	return []Describer{
		coreSectionsDescriber{},
		routingOwnerDescriber{},
		wholeSectionDescriber{id: "dns_takeover_rows", section: SectionDNSTakeover, entity: EntityDNSTakeover},
		wholeSectionDescriber{id: "diag_rows", section: DiagSection, entity: EntityDiag},
	}
}

// coreSectionsDescriber — секции ядер xray_* и mihomo_*: одна строка на
// изменённую секцию, сколько бы полей ни поменялось. Смена желания управлять
// секцией даёт строку владения вместо строки значения.
type coreSectionsDescriber struct{}

func (coreSectionsDescriber) ID() string { return "core_sections_rows" }

func (coreSectionsDescriber) Owns(section string) bool {
	_, _, ok := coreSectionOf(section)
	return ok
}

func (coreSectionsDescriber) Describe(applied, draft Sections) []ChangeRow {
	var out []ChangeRow
	for _, name := range coreSectionNames() {
		kind, ok := SectionChange(applied, draft, name)
		if !ok {
			continue
		}
		kernel, id, _ := coreSectionOf(name)
		row := ChangeRow{Tab: TabCores, Kind: kind, Entity: EntitySection, ID: name, Label: id, Kernel: kernel, Section: name}
		a, errA := ParseCoreSection(applied[name])
		d, errD := ParseCoreSection(draft[name])
		if errA == nil && errD == nil && a.Managed != d.Managed {
			row.Tab, row.Entity, row.Kind = TabOwner, EntitySectionOwner, ChangeRemoved
			if d.Managed {
				row.Kind = ChangeAdded
			}
		}
		out = append(out, row)
	}
	return out
}

// coreSectionOf разбирает имя секции ядра на ядро и идентификатор секции.
func coreSectionOf(section string) (kernel, id string, ok bool) {
	for _, x := range XraySectionIDs {
		if CoreSectionName(KernelXray, x) == section {
			return KernelXray, x, true
		}
	}
	for _, m := range MihomoSectionIDs {
		if CoreSectionName(KernelMihomo, m) == section {
			return KernelMihomo, m, true
		}
	}
	return "", "", false
}

// routingOwnerDescriber — секция владения маршрутизацией: строка на ядро, у
// которого изменилось желание управлять маршрутизацией. Смена своего профиля
// Mihomo при отпущенном Mihomo — строка ChangeChanged того же ядра.
type routingOwnerDescriber struct{}

func (routingOwnerDescriber) ID() string { return "routing_owner_rows" }

func (routingOwnerDescriber) Owns(section string) bool { return section == RoutingSection }

func (routingOwnerDescriber) Describe(applied, draft Sections) []ChangeRow {
	if _, ok := SectionChange(applied, draft, RoutingSection); !ok {
		return nil
	}
	a, errA := ParseRoutingDesired(applied[RoutingSection])
	d, errD := ParseRoutingDesired(draft[RoutingSection])
	if errA != nil || errD != nil {
		return nil
	}
	var out []ChangeRow
	for _, kernel := range []string{KernelXray, KernelMihomo} {
		was, now := a.ManagedFor(kernel), d.ManagedFor(kernel)
		row := ChangeRow{Tab: TabOwner, Entity: EntityRoutingOwner, ID: kernel, Label: kernel, Kernel: kernel, Section: RoutingSection}
		switch {
		case was != now && now:
			row.Kind = ChangeAdded
		case was != now:
			row.Kind = ChangeRemoved
		case kernel == KernelMihomo && !now && a.MihomoProfile != d.MihomoProfile:
			row.Kind = ChangeChanged
		default:
			continue
		}
		out = append(out, row)
	}
	return out
}

// wholeSectionDescriber — секция, которая показывается одной строкой целиком.
type wholeSectionDescriber struct {
	id      string
	section string
	entity  string
}

func (w wholeSectionDescriber) ID() string { return w.id }

func (w wholeSectionDescriber) Owns(section string) bool { return section == w.section }

func (w wholeSectionDescriber) Describe(applied, draft Sections) []ChangeRow {
	kind, ok := SectionChange(applied, draft, w.section)
	if !ok {
		return nil
	}
	return []ChangeRow{{Tab: TabCores, Kind: kind, Entity: w.entity, ID: w.section, Label: w.section, Section: w.section}}
}
