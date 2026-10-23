package routing

import (
	"bytes"
	"errors"
	"log"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// mihomoGenID — идентификатор генератора Mihomo в реестре слоя.
const mihomoGenID = "routing_mihomo"

// Значения базы профиля панели (D-05).
const (
	// defaultMihomoRedirPort и defaultMihomoTProxyPort — порты, если в прежнем
	// профиле их нет (как у старого конструктора профиля).
	defaultMihomoRedirPort  = 5000
	defaultMihomoTProxyPort = 5001
	// mihomoRoutingMark — метка сокетов Mihomo: цепочка _out XKeen пропускает её
	// мимо прозрачного перенаправления.
	mihomoRoutingMark = 255
	// mihomoTolerance — допуск url-test в миллисекундах: группа не перескакивает
	// на узел, выигрывающий меньше.
	mihomoTolerance = 50
	// mihomoEmptyFallback — куда уходит трафик пустой группы: пустая группа Mihomo
	// без этого ключа отдаёт трафик в COMPATIBLE (DIRECT), RESEARCH Pitfall 2.
	mihomoEmptyFallback = mihomoReject
	// proxyProviderDir — каталог провайдеров узлов в каталоге Mihomo.
	proxyProviderDir = "proxy_providers"
)

// hiddenGroupSuffix — суффикс скрытой группы основных узлов «резерва».
const hiddenGroupSuffix = "-main"

// mihomoGen — генератор профиля Mihomo: база, провайдеры, группы и правила.
type mihomoGen struct{ s *Service }

// ID — идентификатор генератора в реестре.
func (mihomoGen) ID() string { return mihomoGenID }

// Generate выдаёт профиль панели и файлы провайдеров списков. Mihomo не
// установлен или желаемое routing.managed=false — файлов нет (D-01).
func (g mihomoGen) Generate(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	if !installed.Mihomo || !configlayer.DesiredRouting(in.Sections).Managed {
		return nil, nil
	}
	return g.s.generateMihomo(in, installed)
}

// mihomoGroup — материализованная группа: что стоит в proxy-groups.
type mihomoGroup struct {
	g Group
	// members — имена участников ручной группы (вложенные группы, DIRECT, REJECT).
	members []string
	// copies — имена копий отдельных узлов в proxies группы.
	copies []string
	// use, filter и exclude — провайдеры источников и фильтры имён.
	use             []string
	filter, exclude string
	// reserve — имя копии резервного узла; пусто — REJECT.
	reserve string
}

// mihomoBuild — расчёт профиля для одного входа. Только чтение и память.
type mihomoBuild struct {
	groups   []Group
	rules    RulesConfig
	probe    ProbeConfig
	catalog  []sources.CatalogNode
	ports    configlayer.MihomoPorts
	groupsOK bool

	byID      map[string]*Group
	nodeByRef map[string]sources.CatalogNode
	res       map[string]Resolved
	emit      map[string]bool
	visiting  map[string]bool

	lists     map[string]*mihomoList
	providers []mihomoProvider

	shapes   []*mihomoGroup
	used     map[string]bool
	copyName map[string]string
	copies   []*yaml.Node
}

// newMihomoBuild готовит расчёт: состав каждой группы по списку узлов Mihomo.
func newMihomoBuild(b mihomoBuild) *mihomoBuild {
	b.byID = make(map[string]*Group, len(b.groups))
	b.nodeByRef = make(map[string]sources.CatalogNode, len(b.catalog))
	b.res = make(map[string]Resolved, len(b.groups))
	b.emit = map[string]bool{}
	b.visiting = map[string]bool{}
	b.used = map[string]bool{}
	b.copyName = map[string]string{}
	for i := range b.groups {
		b.byID[b.groups[i].ID] = &b.groups[i]
	}
	for _, n := range b.catalog {
		b.nodeByRef[n.Ref] = n
	}
	for _, g := range b.groups {
		b.res[g.ID] = Resolve(g, b.catalog, b.groups)
	}
	return &b
}

// hiddenName — имя скрытой группы основных узлов «резерва».
func hiddenName(id string) string { return GroupTag(id) + hiddenGroupSuffix }

// mains — основные узлы группы: состав без резервного узла «резерва».
func (b *mihomoBuild) mains(g Group) []sources.CatalogNode {
	all := b.res[g.ID].Nodes
	if g.Mode != ModeFallback || g.Reserve == "" {
		return all
	}
	return withoutRef(all, g.Reserve)
}

// withoutRef возвращает узлы без узла с такой ссылкой.
func withoutRef(list []sources.CatalogNode, ref string) []sources.CatalogNode {
	out := make([]sources.CatalogNode, 0, len(list))
	for _, n := range list {
		if n.Ref != ref {
			out = append(out, n)
		}
	}
	return out
}

// isEmitted сообщает, что группа материализуется в профиле Mihomo. Группа, пустая
// для Mihomo (нет узлов и участников), не эмитируется (D-13); ниже порога
// FeatureMihomoGroups группы не эмитируются вовсе (D-45).
func (b *mihomoBuild) isEmitted(id string) bool {
	if !b.groupsOK {
		return false
	}
	if v, ok := b.emit[id]; ok {
		return v
	}
	g := b.byID[id]
	if g == nil || b.visiting[id] {
		// Цикл вложенности блокирует проверка; генератор не зацикливается.
		return false
	}
	b.visiting[id] = true
	var ok bool
	if g.Mode == ModeManual {
		r := b.res[id]
		ok = len(r.Nodes) > 0
		for _, m := range r.Members {
			if ok {
				break
			}
			ok = m.Kind != KindGroup || b.isEmitted(m.ID)
		}
	} else {
		ok = len(b.mains(*g)) > 0
	}
	delete(b.visiting, id)
	b.emit[id] = ok
	return ok
}

// split делит узлы группы на покрытые провайдерами с фильтрами (use) и отдельные
// узлы (Picks), которых use+filter не выбирают.
func (b *mihomoBuild) split(g Group) (base, picks []sources.CatalogNode) {
	plain := g
	plain.Picks = nil
	inBase := make(map[string]bool)
	for _, n := range Resolve(plain, b.catalog, nil).Nodes {
		inBase[n.Ref] = true
	}
	for _, n := range b.res[g.ID].Nodes {
		if inBase[n.Ref] {
			base = append(base, n)
		} else {
			picks = append(picks, n)
		}
	}
	return base, picks
}

// sourcesOf — источники узлов по порядку идентификаторов, без повторов.
func sourcesOf(list []sources.CatalogNode) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range list {
		if !seen[n.Source] {
			seen[n.Source] = true
			out = append(out, n.Source)
		}
	}
	sort.Strings(out)
	return out
}

// mihomoFilter превращает значения «включить» или «исключить» в filter Mihomo:
// подстроки без учёта регистра — (?i) и QuoteMeta через |; выражения Go (режим
// «Эксперт») — как есть через |. Пусто — фильтра нет.
func mihomoFilter(values []string, regex bool) string {
	var parts []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !regex {
			v = regexp.QuoteMeta(v)
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return ""
	}
	if regex {
		return strings.Join(parts, "|")
	}
	return "(?i)" + strings.Join(parts, "|")
}

// freeName подбирает имя, которого нет среди занятых (группы, скрытые группы,
// зарезервированные слова и другие копии): дубли получают суффикс « 2», « 3».
func (b *mihomoBuild) freeName(base string) string {
	taken := func(s string) bool {
		if b.used[s] {
			return true
		}
		for _, r := range reservedNames {
			if strings.EqualFold(s, r) {
				return true
			}
		}
		return false
	}
	name := base
	for i := 2; taken(name); i++ {
		name = base + " " + strconv.Itoa(i)
	}
	b.used[name] = true
	return name
}

// clashProxyNode строит YAML-узел копии узла под заданным именем: тот же вид, что
// у файла провайдера источника (порядок ключей и кавычки задаёт nodes.ClashFile).
func clashProxyNode(n nodes.Node, name string) (*yaml.Node, error) {
	p, err := nodes.ClashProxy(n, name)
	if err != nil {
		return nil, err
	}
	data, err := nodes.ClashFile([]map[string]any{p})
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 || len(doc.Content[0].Content) != 2 || len(doc.Content[0].Content[1].Content) != 1 {
		return nil, errors.New("routing: неожиданный вид прокси")
	}
	return doc.Content[0].Content[1].Content[0], nil
}

// copyOf возвращает имя копии отдельного узла в proxies профиля; копия одна на
// узел и общая для всех групп. Пусто — узел не записывается для Mihomo, группа
// на него не ссылается.
func (b *mihomoBuild) copyOf(n sources.CatalogNode) string {
	if name, ok := b.copyName[n.Ref]; ok {
		return name
	}
	base := n.MihomoName
	if base == "" {
		base = n.Display
	}
	name := b.freeName(base)
	node, err := clashProxyNode(n.Node, name)
	if err != nil {
		// Вердикт каталога OK означает, что пробная эмиссия прошла; сюда не доходим.
		log.Printf("[routing] узел %s не записан копией для Mihomo", n.Ref)
		b.copyName[n.Ref] = ""
		return ""
	}
	b.copyName[n.Ref] = name
	b.copies = append(b.copies, node)
	return name
}

// copiesOf возвращает имена копий узлов списка.
func (b *mihomoBuild) copiesOf(list []sources.CatalogNode) []string {
	var out []string
	for _, n := range list {
		if name := b.copyOf(n); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// uniqueNameIn сообщает, что имя узла в Mihomo встречается среди узлов
// источников ровно один раз: только тогда узел можно исключить фильтром по имени.
func (b *mihomoBuild) uniqueNameIn(name string, srcs []string) bool {
	count := 0
	for _, n := range b.catalog {
		if n.MihomoName == name && slices.Contains(srcs, n.Source) {
			count++
		}
	}
	return count == 1
}

// prepare считает материализацию групп: состав, провайдеры, фильтры и копии узлов.
func (b *mihomoBuild) prepare() {
	for _, g := range b.groups {
		b.used[g.Name] = true
		b.used[hiddenName(g.ID)] = true
	}
	if !b.groupsOK {
		return
	}
	for _, g := range b.groups {
		if b.isEmitted(g.ID) {
			b.shapes = append(b.shapes, b.shapeOf(g))
		}
	}
}

// shapeOf описывает материализацию одной группы.
func (b *mihomoBuild) shapeOf(g Group) *mihomoGroup {
	mg := &mihomoGroup{g: g}
	base, picks := b.split(g)
	if g.Mode == ModeManual {
		for _, m := range b.res[g.ID].Members {
			switch m.Kind {
			case KindDirect:
				mg.members = append(mg.members, mihomoDirect)
			case KindReject:
				mg.members = append(mg.members, mihomoReject)
			case KindGroup:
				if b.isEmitted(m.ID) {
					mg.members = append(mg.members, b.byID[m.ID].Name)
				}
			}
		}
	}
	filter := mihomoFilter(g.Include, g.Regex)
	exclude := mihomoFilter(g.Exclude, g.Regex)

	if g.Mode == ModeFallback {
		var reserve *sources.CatalogNode
		if n, ok := b.nodeByRef[g.Reserve]; ok && g.Reserve != "" {
			reserve = &n
		}
		baseMains := withoutRef(base, g.Reserve)
		reserveInBase := len(baseMains) != len(base)
		use := sourcesOf(baseMains)
		copyAll := false
		if reserveInBase && len(baseMains) > 0 && reserve != nil {
			// Резервный узел входит в источник группы: из основных его убирает
			// фильтр по точному имени, а если имя не единственное — основные узлы
			// группы записываются копиями.
			if b.uniqueNameIn(reserve.MihomoName, use) {
				pat := "^" + regexp.QuoteMeta(reserve.MihomoName) + "$"
				if exclude != "" {
					pat += "|" + exclude
				}
				exclude = pat
			} else {
				copyAll = true
			}
		}
		if copyAll {
			mg.copies = append(mg.copies, b.copiesOf(baseMains)...)
		} else if len(baseMains) > 0 {
			mg.use, mg.filter, mg.exclude = use, filter, exclude
		}
		mg.copies = append(mg.copies, b.copiesOf(withoutRef(picks, g.Reserve))...)
		if reserve != nil {
			mg.reserve = b.copyOf(*reserve)
		}
		return mg
	}

	if len(base) > 0 {
		mg.use, mg.filter, mg.exclude = sourcesOf(base), filter, exclude
	}
	mg.copies = b.copiesOf(picks)
	return mg
}

// --- YAML ---

func yKey(k string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k} }

// yStr — строка: yaml.v3 сам берёт кавычки там, где обычная запись читалась бы
// как другой тип или ломала бы разбор (двоеточие, решётка, апостроф).
func yStr(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

func yInt(i int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(i)}
}

func yBool(v bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
}

// ySeq — последовательность строк; flow — в одну строку.
func ySeq(items []string, flow bool) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode}
	if flow {
		n.Style = yaml.FlowStyle
	}
	for _, s := range items {
		n.Content = append(n.Content, yStr(s))
	}
	return n
}

// yMap — отображение из пар «ключ, значение» в заданном порядке.
func yMap(pairs ...any) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(pairs); i += 2 {
		n.Content = append(n.Content, yKey(pairs[i].(string)), pairs[i+1].(*yaml.Node))
	}
	return n
}

// baseSection — база профиля (D-05): порты прозрачного прокси и доступ из LAN
// прежнего активного профиля, метка сокетов, режим правил, сокет контроллера и
// сохранение выбора групп. Ключей автозагрузки geodata в профиле нет: данные
// списков панель выгружает сама.
func baseSection(p configlayer.MihomoPorts) *yaml.Node {
	redir, tproxy := p.Redir, p.TProxy
	if redir <= 0 {
		redir = defaultMihomoRedirPort
	}
	if tproxy <= 0 {
		tproxy = defaultMihomoTProxyPort
	}
	allowLAN := true
	if p.AllowLAN != nil {
		allowLAN = *p.AllowLAN
	}
	bind := p.BindAddress
	if bind == "" {
		bind = "*"
	}
	return yMap(
		"redir-port", yInt(redir),
		"tproxy-port", yInt(tproxy),
		"allow-lan", yBool(allowLAN),
		"bind-address", yStr(bind),
		"mode", yStr("rule"),
		"routing-mark", yInt(mihomoRoutingMark),
		"external-controller-unix", yStr(services.DefaultMihomoSocketPath),
		"profile", yMap("store-selected", yBool(true)),
	)
}

// snifferSection — определение домена по трафику: destination не подменяется
// (override-destination false), IP остаётся для правил по IP (D-23).
func snifferSection() *yaml.Node {
	return yMap("sniffer", yMap(
		"enable", yBool(true),
		"force-dns-mapping", yBool(true),
		"parse-pure-ip", yBool(true),
		"override-destination", yBool(false),
		"sniff", yMap(
			"HTTP", yMap("ports", ySeq([]string{"80", "8080-8880"}, true)),
			"TLS", yMap("ports", ySeq([]string{"443", "8443"}, true)),
			"QUIC", yMap("ports", ySeq([]string{"443", "8443"}, true)),
		),
	))
}

// dnsSection — DNS Mihomo: включён, без IPv6, запросы идут системному
// распознавателю (A6: значение проверяет mihomo -t в роутерном сценарии).
func dnsSection() *yaml.Node {
	return yMap("dns", yMap(
		"enable", yBool(true),
		"ipv6", yBool(false),
		"nameserver", ySeq([]string{"system"}, true),
	))
}

// providersSection — провайдеры узлов источников: файлы proxy_providers/xcp-<id>.yaml
// панели. nil — источников с узлами Mihomo нет.
func providersSection(srcs []string) *yaml.Node {
	if len(srcs) == 0 {
		return nil
	}
	m := &yaml.Node{Kind: yaml.MappingNode}
	for _, id := range srcs {
		name := "xcp-" + id
		m.Content = append(m.Content, yKey(name), yMap(
			"type", yStr("file"),
			"path", yStr("./"+proxyProviderDir+"/"+name+".yaml"),
			"health-check", yMap("enable", yBool(false)),
		))
	}
	return yMap("proxy-providers", m)
}

// ruleProvidersSection — текстовые провайдеры правил из списков dat. nil —
// списков нет.
func ruleProvidersSection(ps []mihomoProvider) *yaml.Node {
	if len(ps) == 0 {
		return nil
	}
	m := &yaml.Node{Kind: yaml.MappingNode}
	for _, p := range ps {
		m.Content = append(m.Content, yKey(p.name), yMap(
			"type", yStr("file"),
			"behavior", yStr(p.behavior),
			"format", yStr("text"),
			"path", yStr("./"+mihomoRuleProviderDir+"/"+p.name+".txt"),
		))
	}
	return yMap("rule-providers", m)
}

// proxiesSection — копии отдельных и резервных узлов. nil — копий нет.
func proxiesSection(b *mihomoBuild) *yaml.Node {
	if len(b.copies) == 0 {
		return nil
	}
	return yMap("proxies", &yaml.Node{Kind: yaml.SequenceNode, Content: b.copies})
}

// groupNode собирает группу Mihomo. Ключи идут в постоянном порядке; фильтры
// пишутся только вместе с use.
func (b *mihomoBuild) groupNode(name, typ string, hidden bool, proxies, use []string, filter, exclude string, probe bool) *yaml.Node {
	if len(proxies) == 0 && len(use) == 0 {
		// Копии узлов не записались: группа без участников ушла бы в COMPATIBLE.
		proxies = []string{mihomoReject}
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v *yaml.Node) { n.Content = append(n.Content, yKey(k), v) }
	add("name", yStr(name))
	add("type", yStr(typ))
	if hidden {
		add("hidden", yBool(true))
	}
	if len(proxies) > 0 {
		add("proxies", ySeq(proxies, false))
	}
	if len(use) > 0 {
		add("use", ySeq(use, false))
		if filter != "" {
			add("filter", yStr(filter))
		}
		if exclude != "" {
			add("exclude-filter", yStr(exclude))
		}
	}
	if probe {
		add("url", yStr(b.probe.URL))
		add("interval", yInt(b.probe.IntervalS))
		add("timeout", yInt(b.probe.TimeoutS*1000))
		if typ == "url-test" {
			add("tolerance", yInt(mihomoTolerance))
			add("lazy", yBool(true))
		}
	}
	add("empty-fallback", yStr(mihomoEmptyFallback))
	return n
}

// uses приводит имена источников к именам провайдеров профиля.
func uses(srcs []string) []string {
	out := make([]string, 0, len(srcs))
	for _, id := range srcs {
		out = append(out, "xcp-"+id)
	}
	return out
}

// groupsSection — proxy-groups: ручная группа — select, «авто» — url-test,
// «резерв» — fallback из скрытой url-test по основным узлам и резервного узла
// (без резерва — REJECT). У каждой группы empty-fallback: REJECT. nil — групп нет.
func groupsSection(b *mihomoBuild) *yaml.Node {
	if len(b.shapes) == 0 {
		return nil
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, mg := range b.shapes {
		g := mg.g
		switch g.Mode {
		case ModeManual:
			proxies := append(append([]string(nil), mg.members...), mg.copies...)
			seq.Content = append(seq.Content, b.groupNode(g.Name, "select", false, proxies, uses(mg.use), mg.filter, mg.exclude, false))
		case ModeAuto:
			seq.Content = append(seq.Content, b.groupNode(g.Name, "url-test", false, mg.copies, uses(mg.use), mg.filter, mg.exclude, true))
		case ModeFallback:
			hidden := hiddenName(g.ID)
			seq.Content = append(seq.Content, b.groupNode(hidden, "url-test", true, mg.copies, uses(mg.use), mg.filter, mg.exclude, true))
			reserve := mg.reserve
			if reserve == "" {
				reserve = mihomoReject
			}
			seq.Content = append(seq.Content, b.groupNode(g.Name, "fallback", false, []string{hidden, reserve}, nil, "", "", true))
		}
	}
	return yMap("proxy-groups", seq)
}

// rulesSection — rules профиля.
func rulesSection(lines []string) *yaml.Node {
	return yMap("rules", ySeq(lines, false))
}

// profile собирает профиль панели секциями по порядку. Каждая секция — свой
// *yaml.Node: Phase 147 заменит отдельные секции, не переписывая остальные.
func (b *mihomoBuild) profile() ([]byte, error) {
	lines := []string{}
	for i, r := range b.rules.Items {
		ls, err := b.mihomoLines(r)
		if err != nil {
			var ge *genError
			if errors.As(err, &ge) {
				return nil, genErr(ge.code, "правило "+strconv.Itoa(i+1))
			}
			return nil, err
		}
		lines = append(lines, ls...)
	}
	lines = append(lines, b.mihomoFinalLine())

	root := &yaml.Node{Kind: yaml.MappingNode}
	for _, sec := range []*yaml.Node{
		baseSection(b.ports),
		snifferSection(),
		dnsSection(),
		providersSection(sourcesOf(b.catalog)),
		ruleProvidersSection(b.providers),
		proxiesSection(b),
		groupsSection(b),
		rulesSection(lines),
	} {
		if sec != nil {
			root.Content = append(root.Content, sec.Content...)
		}
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mihomoGroupsAvailable — группы, которые строит панель, доступны на версии
// Mihomo (D-45); неопределённая версия функции не выключает (T-144-05).
func (s *Service) mihomoGroupsAvailable() bool {
	return configlayer.FeatureAvailable(configlayer.KernelMihomo, s.d.KernelVersion(configlayer.KernelMihomo),
		configlayer.FeatureMihomoGroups) != configlayer.Unavailable
}

// generateMihomo разбирает секции и считает файлы: профиль панели и текстовые
// провайдеры списков dat.
func (s *Service) generateMihomo(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	groups, err := parseGroups(in.Sections[GroupsSection])
	if err != nil {
		return nil, err
	}
	rules, err := parseRules(in.Sections[RulesSection])
	if err != nil {
		return nil, err
	}
	probe, err := parseProbe(in.Sections[ProbeSection], s.d.GOARCH)
	if err != nil {
		return nil, err
	}
	if err := validateProbe(probe); err != nil {
		return nil, err
	}
	cat, err := s.d.Sources.Catalog(in, installed)
	if err != nil {
		return nil, err
	}
	b := newMihomoBuild(mihomoBuild{
		groups: groups.Items, rules: rules, probe: probe, catalog: cat.Mihomo,
		ports: in.Routing.Mihomo, groupsOK: s.mihomoGroupsAvailable(),
	})
	if err := b.collectLists(s.d.Lists); err != nil {
		return nil, err
	}
	b.prepare()
	profile, err := b.profile()
	if err != nil {
		return nil, err
	}
	files := []configlayer.GeneratedFile{{
		Kernel:  configlayer.KernelMihomo,
		RelPath: configlayer.PanelMihomoProfile,
		Kind:    configlayer.KindMihomoProfile,
		Content: profile,
	}}
	return append(files, s.providerFiles(b.providers)...), nil
}
