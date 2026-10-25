package routing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Пресеты маршрутизации (GRP-04, D-24…D-31): состав правил и групп задан данными
// (presets.json) над каталогом списков, а ссылки на списки берутся из dat роутера
// (lists.Resolve). Пресет создаёт обычные правила с пометкой «из пресета» и
// обычные группы; правила пользователя он не трогает.

//go:embed presets.json
var presetsJSON []byte

// Идентификаторы пресетов.
const (
	PresetAllProxy      = "all_proxy"
	PresetExceptCountry = "except_country"
	PresetSelective     = "selective"
	PresetEmpty         = "empty"
)

// Коды причин ErrInvalidInput для пресетов (поле Reason).
const (
	reasonPresetUnknown      = "preset_unknown"
	reasonCountryUnavailable = "country_unavailable"
	reasonCatalogUnavailable = "catalog_unavailable"
	reasonServiceUnknown     = "service_unknown"
	reasonPresetInvalid      = "preset_invalid"
)

// Цели в шаблонах пресетов.
const (
	tmplProxy        = "proxy"
	tmplDirect       = "direct"
	tmplReject       = "reject"
	tmplServiceGroup = "service_group"
)

// Виды элементов шаблона.
const (
	tmplKindCIDR    = "cidr"
	tmplKindService = "service"
	tmplKindCond    = "cond"
	tmplKindCountry = "country"
)

// defaultPresetCountry — страна по умолчанию (D-31).
const defaultPresetCountry = "RU"

// presetTmpl — элемент шаблона: одно правило или набор правил по списку.
type presetTmpl struct {
	// When — условие включения: always, flag:quic|torrent|ads, country_blocked,
	// country_site, country_ip, services, service:<id>, country_is:<код>.
	When string `json:"when"`
	// Kind — cidr (литералы сетей), service (списки сервиса каталога), cond
	// (готовые условия), country (списки выбранной страны; часть задаёт When).
	Kind    string       `json:"kind"`
	Conds   []presetCond `json:"conds"`
	Values  []string     `json:"values"`
	Service string       `json:"service"`
	// Target — proxy, direct, reject или service_group (группа сервиса, если
	// включена галочка D-27, иначе «Прокси»).
	Target string `json:"target"`
}

type presetCond struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type presetDef struct {
	NeedsCountry  bool         `json:"needs_country"`
	NeedsServices bool         `json:"needs_services"`
	Plain         bool         `json:"plain"`
	Final         string       `json:"final"`
	Body          []presetTmpl `json:"body"`
}

type presetData struct {
	V              int                  `json:"v"`
	HiddenServices []string             `json:"hidden_services"`
	Order          []string             `json:"order"`
	Prefix         []presetTmpl         `json:"prefix"`
	Presets        map[string]presetDef `json:"presets"`
}

var presetsCfg = mustLoadPresets()

func validTmplTarget(t string) bool {
	return t == tmplProxy || t == tmplDirect || t == tmplReject || t == tmplServiceGroup
}

func validTmplWhen(w string) bool {
	switch {
	case w == "always", w == "country_blocked", w == "country_site", w == "country_ip", w == "services":
		return true
	case strings.HasPrefix(w, "flag:"):
		return slices.Contains([]string{"quic", "torrent", "ads"}, strings.TrimPrefix(w, "flag:"))
	case strings.HasPrefix(w, "service:"), strings.HasPrefix(w, "country_is:"):
		return strings.Contains(w, ":") && !strings.HasSuffix(w, ":")
	}
	return false
}

// mustLoadPresets разбирает presets.json и проверяет шаблоны при старте: ошибка
// данных — паника, как у каталога списков.
func mustLoadPresets() *presetData {
	var d presetData
	if err := json.Unmarshal(presetsJSON, &d); err != nil {
		panic(fmt.Sprintf("routing: presets.json: %v", err))
	}
	if strings.Contains(string(presetsJSON), "geoip:") {
		panic("routing: presets.json: geoip: в данных пресетов")
	}
	check := func(where string, ts []presetTmpl) {
		for _, t := range ts {
			if !validTmplWhen(t.When) || !validTmplTarget(t.Target) {
				panic(fmt.Sprintf("routing: presets.json: %s: when=%q target=%q", where, t.When, t.Target))
			}
			switch t.Kind {
			case tmplKindCIDR:
				if len(t.Values) == 0 {
					panic(fmt.Sprintf("routing: presets.json: %s: cidr без значений", where))
				}
			case tmplKindCond:
				if len(t.Conds) == 0 {
					panic(fmt.Sprintf("routing: presets.json: %s: cond без условий", where))
				}
			case tmplKindService, tmplKindCountry:
			default:
				panic(fmt.Sprintf("routing: presets.json: %s: вид %q", where, t.Kind))
			}
		}
	}
	check("prefix", d.Prefix)
	if len(d.Order) != len(d.Presets) {
		panic("routing: presets.json: order не совпадает с presets")
	}
	for _, id := range d.Order {
		def, ok := d.Presets[id]
		if !ok || (def.Final != tmplProxy && def.Final != tmplDirect && def.Final != tmplReject) {
			panic(fmt.Sprintf("routing: presets.json: пресет %q", id))
		}
		check(id, def.Body)
	}
	return &d
}

// PresetFlags — галочки диалога (D-26, D-27).
type PresetFlags struct {
	QUIC          bool `json:"quic"`
	Torrent       bool `json:"torrent"`
	Ads           bool `json:"ads"`
	ServiceGroups bool `json:"service_groups"`
}

// PresetChoice — выбор пользователя в диалоге пресета.
type PresetChoice struct {
	// ID — all_proxy, except_country, selective или empty.
	ID       string      `json:"id"`
	Country  string      `json:"country"`
	Services []string    `json:"services"`
	Flags    PresetFlags `json:"flags"`
}

// PresetInfo — описание пресета для диалога.
type PresetInfo struct {
	ID            string   `json:"id"`
	NeedsCountry  bool     `json:"needs_country"`
	NeedsServices bool     `json:"needs_services"`
	Countries     []string `json:"countries"`
}

// PresetsView — данные диалога: пресеты со странами, сервисы каталога, наличие
// списка рекламы, страна по умолчанию и текущий выбор из черновика.
type PresetsView struct {
	Presets        []PresetInfo        `json:"presets"`
	Services       []lists.ServiceView `json:"services"`
	AdsAvailable   bool                `json:"ads_available"`
	DefaultCountry string              `json:"default_country"`
	Current        *PresetState        `json:"current"`
}

// RuleSummary — краткое описание правила в плане смены пресета. TargetName —
// имя группы цели (группы, которой ещё нет, тоже): клиент не ищет её по ID.
type RuleSummary struct {
	Conds      []Cond `json:"conds"`
	Target     Target `json:"target"`
	TargetName string `json:"target_name,omitempty"`
}

// PresetPlan — что уйдёт и что придёт при применении пресета.
type PresetPlan struct {
	Remove []RuleSummary `json:"remove"`
	Add    []RuleSummary `json:"add"`
	// Groups — имена групп, которые будут созданы.
	Groups    []string `json:"groups"`
	Final     Target   `json:"final"`
	FinalName string   `json:"final_name,omitempty"`
	// Kept — число правил пользователя, которые остаются выше.
	Kept int `json:"kept"`
}

func presetInvalid(field, reason string) error { return invalid(field, reason) }

// presetCatalog строит каталог списков роутера для пресетов.
func (s *Service) presetCatalog() (lists.CatalogView, error) {
	if s.d.Lists == nil {
		return lists.CatalogView{}, presetInvalid("catalog", reasonCatalogUnavailable)
	}
	var entries []lists.GeodataEntry
	if s.d.Geodata != nil {
		if st, err := s.d.Geodata.Status(); err == nil {
			entries = st.Entries
		}
	}
	cat, err := lists.Resolve(s.d.Lists, entries)
	if err != nil {
		return lists.CatalogView{}, presetInvalid("catalog", reasonCatalogUnavailable)
	}
	return cat, nil
}

// selectableServices — сервисы каталога, которые можно выбрать в «выборочно»
// (без служебных: реклама, частные сети, торренты, РФ и игры).
func selectableServices(cat lists.CatalogView) []lists.ServiceView {
	out := make([]lists.ServiceView, 0, len(cat.Services))
	for _, sv := range cat.Services {
		if !slices.Contains(presetsCfg.HiddenServices, sv.ID) {
			out = append(out, sv)
		}
	}
	return out
}

func hasService(cat lists.CatalogView, id string) bool {
	for _, sv := range cat.Services {
		if sv.ID == id {
			return true
		}
	}
	return false
}

// countriesFor — страны, для которых пресет доступен: «всё кроме» — с IP-списком,
// «выборочно» — со списками блокировок (D-31).
func countriesFor(id string, def presetDef, cat lists.CatalogView) []string {
	out := []string{}
	if !def.NeedsCountry {
		return out
	}
	for _, c := range cat.Countries {
		switch {
		case id == PresetSelective && len(c.Blocked) == 0:
		case c.IP == nil && id == PresetExceptCountry:
		default:
			out = append(out, c.Code)
		}
	}
	return out
}

func defaultCountryOf(cat lists.CatalogView) string {
	for _, c := range cat.Countries {
		if c.Code == defaultPresetCountry {
			return c.Code
		}
	}
	if len(cat.Countries) > 0 {
		return cat.Countries[0].Code
	}
	return ""
}

// Presets возвращает данные диалога пресета по спискам dat роутера.
func (s *Service) Presets() (PresetsView, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return PresetsView{}, err
	}
	cat, err := s.presetCatalog()
	if err != nil {
		return PresetsView{}, err
	}
	view := PresetsView{
		Presets:        make([]PresetInfo, 0, len(presetsCfg.Order)),
		Services:       selectableServices(cat),
		AdsAvailable:   hasService(cat, "ads"),
		DefaultCountry: defaultCountryOf(cat),
	}
	for _, id := range presetsCfg.Order {
		def := presetsCfg.Presets[id]
		view.Presets = append(view.Presets, PresetInfo{
			ID: id, NeedsCountry: def.NeedsCountry, NeedsServices: def.NeedsServices,
			Countries: countriesFor(id, def, cat),
		})
	}
	if rules, err := parseRules(st.Draft[RulesSection]); err == nil {
		view.Current = rules.Preset
	}
	return view, nil
}

// presetBuilder собирает правила и группы пресета по каталогу роутера.
type presetBuilder struct {
	s       *Service
	cat     lists.CatalogView
	choice  PresetChoice
	def     presetDef
	country *lists.CountryView
	groups  []Group
	created []string
	taken   map[string]bool
	srcIDs  map[string]bool

	proxyID   string
	autoID    string
	svcGroups map[string]string
	rules     []Rule
}

func findGroupByName(groups []Group, name string) *Group {
	for i := range groups {
		if strings.EqualFold(groups[i].Name, name) {
			return &groups[i]
		}
	}
	return nil
}

// addGroup проверяет и добавляет новую группу, возвращает её ID.
func (b *presetBuilder) addGroup(g Group) (string, error) {
	if len(b.groups) >= maxGroups {
		return "", invalid("groups", reasonTooMany)
	}
	id, err := b.s.newUniqueID(b.taken)
	if err != nil {
		return "", err
	}
	g.ID = id
	if err := validateGroup(&g, b.groups, b.srcIDs); err != nil {
		return "", err
	}
	b.taken[id] = true
	b.groups = append(b.groups, g)
	b.created = append(b.created, g.Name)
	return id, nil
}

// ensureBase находит или создаёт «Авто» и «Прокси» (D-37): группы обычные,
// пресет их только переиспользует по имени.
func (b *presetBuilder) ensureBase() error {
	if g := findGroupByName(b.groups, defaultAutoGroupName); g != nil {
		b.autoID = g.ID
	} else {
		id, err := b.addGroup(Group{Name: defaultAutoGroupName, Mode: ModeAuto, Sources: GroupSources{All: true}})
		if err != nil {
			return err
		}
		b.autoID = id
	}
	if g := findGroupByName(b.groups, defaultProxyGroupName); g != nil {
		b.proxyID = g.ID
		return nil
	}
	id, err := b.addGroup(Group{
		Name: defaultProxyGroupName, Mode: ModeManual, Sources: GroupSources{All: true},
		Members: []Member{{Kind: KindGroup, ID: b.autoID}},
	})
	if err != nil {
		return err
	}
	b.proxyID = id
	return nil
}

// serviceGroup возвращает ручную группу сервиса [«Прокси», «Авто», DIRECT]:
// существующая с тем же именем переиспользуется (D-27).
func (b *presetBuilder) serviceGroup(sv lists.ServiceView) (string, error) {
	if id, ok := b.svcGroups[sv.ID]; ok {
		return id, nil
	}
	if g := findGroupByName(b.groups, sv.NameRU); g != nil {
		b.svcGroups[sv.ID] = g.ID
		return g.ID, nil
	}
	id, err := b.addGroup(Group{
		Name: sv.NameRU, Mode: ModeManual,
		Members: []Member{{Kind: KindGroup, ID: b.proxyID}, {Kind: KindGroup, ID: b.autoID}, {Kind: KindDirect}},
	})
	if err != nil {
		return "", err
	}
	b.svcGroups[sv.ID] = id
	return id, nil
}

// target переводит цель шаблона в цель правила; svc — сервис для service_group.
func (b *presetBuilder) target(t string, svc *lists.ServiceView) (Target, error) {
	switch t {
	case tmplDirect:
		return Target{Kind: KindDirect}, nil
	case tmplReject:
		return Target{Kind: KindReject}, nil
	case tmplServiceGroup:
		if b.choice.Flags.ServiceGroups && svc != nil {
			id, err := b.serviceGroup(*svc)
			if err != nil {
				return Target{}, err
			}
			return Target{Kind: KindGroup, ID: id}, nil
		}
	}
	return Target{Kind: KindGroup, ID: b.proxyID}, nil
}

// emit добавляет правило пресета: пометка «из пресета», новый ID, проверка модели.
func (b *presetBuilder) emit(conds []Cond, t Target) error {
	id, err := b.s.newUniqueID(b.taken)
	if err != nil {
		return err
	}
	b.taken[id] = true
	r := Rule{ID: id, Enabled: true, Origin: OriginPreset, Conds: conds, Target: t}
	if err := validateRule(&r, b.groups); err != nil {
		return err
	}
	b.rules = append(b.rules, r)
	return nil
}

// emitRefs создаёт по правилу на вид списков: geosite и geoip в одном правиле
// дали бы «И» двух условий в одно поле Xray, поэтому правила раздельные. Вид без
// ссылок правила не создаёт.
func (b *presetBuilder) emitRefs(refs []lists.ListRef, t Target) error {
	var site, ip []lists.ListRef
	for _, r := range refs {
		switch r.Kind {
		case lists.KindSite:
			site = append(site, r)
		case lists.KindIP:
			ip = append(ip, r)
		}
	}
	if len(site) > 0 {
		if err := b.emit([]Cond{{Type: CondGeosite, Values: []string{}, Lists: site}}, t); err != nil {
			return err
		}
	}
	if len(ip) > 0 {
		return b.emit([]Cond{{Type: CondGeoip, Values: []string{}, Lists: ip}}, t)
	}
	return nil
}

func (b *presetBuilder) serviceView(id string) *lists.ServiceView {
	for i := range b.cat.Services {
		if b.cat.Services[i].ID == id {
			return &b.cat.Services[i]
		}
	}
	return nil
}

// enabled сообщает, действует ли условие включения элемента шаблона.
func (b *presetBuilder) enabled(when string) bool {
	switch {
	case when == "always":
		return true
	case when == "flag:quic":
		return b.choice.Flags.QUIC
	case when == "flag:torrent":
		return b.choice.Flags.Torrent
	case when == "flag:ads":
		return b.choice.Flags.Ads
	case when == "country_blocked":
		return b.country != nil && len(b.country.Blocked) > 0
	case when == "country_site":
		return b.country != nil && b.country.Site != nil
	case when == "country_ip":
		return b.country != nil && b.country.IP != nil
	case when == "services":
		return len(b.choice.Services) > 0
	case strings.HasPrefix(when, "service:"):
		return b.serviceView(strings.TrimPrefix(when, "service:")) != nil
	case strings.HasPrefix(when, "country_is:"):
		return b.choice.Country != "" && strings.EqualFold(b.choice.Country, strings.TrimPrefix(when, "country_is:"))
	}
	return false
}

// expand разворачивает элемент шаблона в правила.
func (b *presetBuilder) expand(t presetTmpl) error {
	if !b.enabled(t.When) {
		return nil
	}
	switch t.Kind {
	case tmplKindCIDR:
		tg, err := b.target(t.Target, nil)
		if err != nil {
			return err
		}
		return b.emit([]Cond{{Type: CondIP, Values: slices.Clone(t.Values), Lists: []lists.ListRef{}}}, tg)
	case tmplKindCond:
		tg, err := b.target(t.Target, nil)
		if err != nil {
			return err
		}
		conds := make([]Cond, 0, len(t.Conds))
		for _, c := range t.Conds {
			conds = append(conds, Cond{Type: c.Type, Values: append([]string{}, c.Values...), Lists: []lists.ListRef{}})
		}
		return b.emit(conds, tg)
	case tmplKindService:
		ids := []string{t.Service}
		if t.Service == "" {
			ids = b.choice.Services
		}
		for _, id := range ids {
			sv := b.serviceView(id)
			if sv == nil {
				// Сервиса нет в dat роутера: правило без ссылок не создаётся.
				continue
			}
			tg, err := b.target(t.Target, sv)
			if err != nil {
				return err
			}
			if err := b.emitRefs(sv.Refs, tg); err != nil {
				return err
			}
		}
		return nil
	case tmplKindCountry:
		if b.country == nil {
			return nil
		}
		var refs []lists.ListRef
		switch t.When {
		case "country_blocked":
			refs = b.country.Blocked
		case "country_site":
			refs = []lists.ListRef{*b.country.Site}
		case "country_ip":
			refs = []lists.ListRef{*b.country.IP}
		}
		tg, err := b.target(t.Target, nil)
		if err != nil {
			return err
		}
		return b.emitRefs(refs, tg)
	}
	return nil
}

// checkNoGeoipInDomain отвергает значение geoip: в поле domain и списки не того
// вида в условии geosite: логики вроде geoip:!cn в domain нет (D-31).
func checkNoGeoipInDomain(rules []Rule) error {
	for _, r := range rules {
		for _, c := range r.Conds {
			for _, v := range c.Values {
				if c.Type == CondDomain && strings.HasPrefix(strings.ToLower(v), "geoip:") {
					return presetInvalid("rules", reasonPresetInvalid)
				}
			}
			if c.Type == CondGeosite {
				for _, l := range c.Lists {
					if l.Kind != lists.KindSite || strings.HasPrefix(strings.ToLower(l.Tag), "geoip:") {
						return presetInvalid("rules", reasonPresetInvalid)
					}
				}
			}
		}
	}
	return nil
}

// presetResult — рассчитанный результат пресета.
type presetResult struct {
	choice PresetChoice
	groups GroupsConfig
	grew   bool
	rules  RulesConfig
	plan   PresetPlan
}

// normalizeChoice приводит выбор к каноничному виду и проверяет его по каталогу.
func normalizeChoice(c PresetChoice, cat lists.CatalogView) (PresetChoice, presetDef, error) {
	def, ok := presetsCfg.Presets[c.ID]
	if !ok {
		return c, def, presetInvalid("preset", reasonPresetUnknown)
	}
	out := PresetChoice{ID: c.ID, Services: []string{}, Flags: c.Flags}
	if def.Plain {
		out.Flags = PresetFlags{}
	}
	if !def.NeedsServices {
		out.Flags.ServiceGroups = false
	}
	if hasAds := hasService(cat, "ads"); !hasAds {
		out.Flags.Ads = false
	}
	if def.NeedsCountry {
		avail := countriesFor(c.ID, def, cat)
		code := strings.ToUpper(strings.TrimSpace(c.Country))
		if code == "" {
			switch {
			case slices.Contains(avail, defaultPresetCountry):
				code = defaultPresetCountry
			case len(avail) > 0:
				code = avail[0]
			}
		}
		if code == "" || !slices.Contains(avail, code) {
			return c, def, presetInvalid("country", reasonCountryUnavailable)
		}
		out.Country = code
	}
	if def.NeedsServices {
		sel := selectableServices(cat)
		for _, id := range c.Services {
			if slices.Contains(out.Services, id) {
				continue
			}
			if !slices.ContainsFunc(sel, func(sv lists.ServiceView) bool { return sv.ID == id }) {
				return c, def, presetInvalid("services", reasonServiceUnknown)
			}
			out.Services = append(out.Services, id)
		}
	}
	return out, def, nil
}

func groupNameByID(groups []Group, id string) string {
	for _, g := range groups {
		if g.ID == id {
			return g.Name
		}
	}
	return ""
}

func summarize(rules []Rule, groups []Group) []RuleSummary {
	out := make([]RuleSummary, 0, len(rules))
	for _, r := range rules {
		rs := RuleSummary{Conds: r.Conds, Target: r.Target}
		if r.Target.Kind == KindGroup {
			rs.TargetName = groupNameByID(groups, r.Target.ID)
		}
		out = append(out, rs)
	}
	return out
}

func presetFlagsMap(f PresetFlags) map[string]bool {
	return map[string]bool{"quic": f.QUIC, "torrent": f.Torrent, "ads": f.Ads, "service_groups": f.ServiceGroups}
}

// buildPreset рассчитывает группы и правила черновика после пресета. Правила
// пользователя (без пометки «из пресета») остаются выше в прежнем порядке.
func (s *Service) buildPreset(st configlayer.StateView, c PresetChoice) (presetResult, error) {
	cat, err := s.presetCatalog()
	if err != nil {
		return presetResult{}, err
	}
	choice, def, err := normalizeChoice(c, cat)
	if err != nil {
		return presetResult{}, err
	}
	groups, err := parseGroups(st.Draft[GroupsSection])
	if err != nil {
		return presetResult{}, err
	}
	cur, err := parseRules(st.Draft[RulesSection])
	if err != nil {
		return presetResult{}, err
	}

	b := &presetBuilder{
		s: s, cat: cat, choice: choice, def: def,
		groups: slices.Clone(groups.Items), taken: takenIDs(groups.Items, cur.Items),
		srcIDs: draftSourceIDs(st.Draft), svcGroups: map[string]string{},
	}
	if def.NeedsCountry {
		for i := range cat.Countries {
			if cat.Countries[i].Code == choice.Country {
				b.country = &cat.Countries[i]
			}
		}
	}
	finalTmpl := def.Final
	if !def.Plain {
		if err := b.ensureBase(); err != nil {
			return presetResult{}, err
		}
		for _, t := range presetsCfg.Prefix {
			if err := b.expand(t); err != nil {
				return presetResult{}, err
			}
		}
		for _, t := range def.Body {
			if err := b.expand(t); err != nil {
				return presetResult{}, err
			}
		}
	}
	final, err := b.target(finalTmpl, nil)
	if err != nil {
		return presetResult{}, err
	}
	if err := checkNoGeoipInDomain(b.rules); err != nil {
		return presetResult{}, err
	}

	var user, removed []Rule
	for _, r := range cur.Items {
		if r.Origin == OriginPreset {
			removed = append(removed, r)
		} else {
			user = append(user, r)
		}
	}
	if len(user)+len(b.rules) > maxRules {
		return presetResult{}, invalid("rules", reasonTooMany)
	}
	items := append(append(make([]Rule, 0, len(user)+len(b.rules)), user...), b.rules...)

	res := presetResult{
		choice: choice,
		grew:   len(b.created) > 0,
		groups: GroupsConfig{V: sectionVersion, DefaultsDone: groups.DefaultsDone, Items: b.groups},
		rules: RulesConfig{
			V: sectionVersion, Items: items, Final: final,
			Preset: &PresetState{ID: choice.ID, Country: choice.Country, Services: choice.Services, Flags: presetFlagsMap(choice.Flags)},
		},
	}
	if res.grew {
		res.groups.DefaultsDone = true
	}
	res.plan = PresetPlan{
		Remove:    summarize(removed, groups.Items),
		Add:       summarize(b.rules, b.groups),
		Groups:    append([]string{}, b.created...),
		Final:     final,
		FinalName: groupNameByID(b.groups, final.ID),
		Kept:      len(user),
	}
	return res, nil
}

// PlanPreset показывает, что уйдёт и что придёт при пресете, без записи.
func (s *Service) PlanPreset(c PresetChoice) (PresetPlan, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return PresetPlan{}, err
	}
	res, err := s.buildPreset(st, c)
	if err != nil {
		return PresetPlan{}, err
	}
	return res.plan, nil
}

// presetSections кодирует секции черновика после пресета.
func presetSections(res presetResult, managed bool) (map[string]json.RawMessage, error) {
	rules, err := encodeRules(res.rules)
	if err != nil {
		return nil, err
	}
	sections := map[string]json.RawMessage{RulesSection: rules}
	if res.grew {
		g, err := encodeGroups(res.groups)
		if err != nil {
			return nil, err
		}
		sections[GroupsSection] = g
	}
	if managed {
		raw, err := json.Marshal(configlayer.RoutingDesired{V: sectionVersion, Managed: true})
		if err != nil {
			return nil, err
		}
		sections[configlayer.RoutingSection] = raw
	}
	return sections, nil
}

// ApplyPreset применяет пресет к черновику одной правкой: группы и правила.
// Заменяются только правила с пометкой «из пресета» и «Остальное».
func (s *Service) ApplyPreset(rev int64, c PresetChoice) (configlayer.DraftEvent, error) {
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		res, err := s.buildPreset(st, c)
		if err != nil {
			return nil, err
		}
		return presetSections(res, false)
	})
}

// TakeWithPreset берёт маршрутизацию под панель и применяет пресет той же
// правкой черновика (D-04, D-25): routing.managed, группы и правила.
func (s *Service) TakeWithPreset(rev int64, c PresetChoice) (configlayer.DraftEvent, error) {
	return s.edit(rev, func(st configlayer.StateView) (map[string]json.RawMessage, error) {
		res, err := s.buildPreset(st, c)
		if err != nil {
			return nil, err
		}
		return presetSections(res, true)
	})
}
