package routing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Идентификатор генератора в реестре слоя и файлы панели в каталоге Xray.
const (
	xrayGenID = "routing_xray"
	// xrayGroupsFile — исходящие панели: xcp-direct, xcp-block и ручные группы.
	// «.tail» обязателен: без него исходящие встают в начало и первый из них
	// становится исходящим по умолчанию (RESEARCH, Pattern 4).
	xrayGroupsFile = "04_outbounds.xcp-groups.tail.json"
	// xrayRoutingFile — routing панели (заменяет чужой routing целиком).
	xrayRoutingFile = "99_xcp-routing.json"
	// xrayObservatoryFile — burstObservatory; файл есть только при balancer.
	xrayObservatoryFile = "99_xcp-observatory.json"
)

// Теги исходящих панели в Xray.
const (
	// TagDirect — прямой выход с меткой 255: цепочка _out XKeen пропускает её мимо
	// прозрачного перенаправления.
	TagDirect = "xcp-direct"
	// TagBlock — blackhole: соединение закрывается.
	TagBlock = "xcp-block"
	// loopbackTagPrefix — префикс inboundTag loopback ручной группы.
	loopbackTagPrefix = "xcp-lb-"
)

// xrayMark — метка сокета исходящих панели (см. nodes.XrayOutbound).
const xrayMark = 255

// Коды ошибок генератора (начало текста ошибки; слой отдаёт их в Detail
// проблемы generator_failed).
const (
	genErrSelectorPrefix = "xray_selector_prefix"
	// genErrCondConflict — два условия правила ложатся в одно поле правила Xray
	// (например, домен и geosite): пересечение двух списков «ИЛИ» в одном поле не
	// выражается, а объединение расширило бы правило.
	genErrCondConflict = "xray_cond_conflict"
	genErrRawInvalid   = "xray_raw_invalid"
)

// maxGenMemo — предел мемоизации результатов генераторов маршрутизации.
const maxGenMemo = 32

// Префиксы участников группы в Live[selection] и в effectiveSelection.
const (
	memberNode   = "node:"
	memberGroup  = "group:"
	memberDirect = "direct"
	memberReject = "reject"
)

// genError — ошибка генератора с кодом в начале текста.
type genError struct{ code, detail string }

func (e *genError) Error() string {
	if e.detail == "" {
		return e.code
	}
	return e.code + ": " + e.detail
}

func genErr(code, detail string) error { return &genError{code: code, detail: detail} }

// xrayGen — генератор файлов Xray: группы, balancer, routing, observatory.
type xrayGen struct{ s *Service }

// ID — идентификатор генератора в реестре.
func (xrayGen) ID() string { return xrayGenID }

// Generate выдаёт файлы маршрутизации Xray. Xray не установлен или желаемое
// routing.managed=false — файлов нет (D-01): панель маршрутизацией не владеет.
func (g xrayGen) Generate(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	if !installed.Xray || !configlayer.DesiredRouting(in.Sections).Managed {
		return nil, nil
	}
	return g.s.generateXray(in, installed)
}

// memberString превращает участника группы в строку выбора.
func memberString(m Member) string {
	switch m.Kind {
	case KindGroup:
		return memberGroup + m.ID
	case KindDirect:
		return memberDirect
	case KindReject:
		return memberReject
	}
	return ""
}

// effectiveSelection возвращает участника ручной группы, на которого смотрит
// ядро. Выбор sel (из Live[selection]) действует, если такой участник есть в
// группе и avail(участник) истинно; иначе — первый допустимый по порядку:
// сначала участники Members, затем узлы состава Resolve (nodeRefs — их ссылки в
// порядке состава). DIRECT и REJECT берутся по умолчанию, только если стоят
// первыми (D-36). Пусто — допустимых участников нет. Общий для генераторов и
// представления (146-14, 146-15): они передают ссылки узлов своего ядра.
func effectiveSelection(g Group, sel string, avail func(member string) bool, nodeRefs ...string) string {
	cands := make([]string, 0, len(g.Members)+len(nodeRefs))
	for _, m := range g.Members {
		if s := memberString(m); s != "" {
			cands = append(cands, s)
		}
	}
	for _, ref := range nodeRefs {
		cands = append(cands, memberNode+ref)
	}
	if sel != "" {
		for _, c := range cands {
			if c == sel && avail(c) {
				return c
			}
		}
	}
	for i, c := range cands {
		if (c == memberDirect || c == memberReject) && i != 0 {
			continue
		}
		if avail(c) {
			return c
		}
	}
	return ""
}

// nodeRefs — ссылки узлов в порядке состава.
func nodeRefs(list []sources.CatalogNode) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, n.Ref)
	}
	return out
}

// xrayBalancer — balancer «авто» или «резерва».
type xrayBalancer struct {
	tag      string
	selector []string
	fallback string
}

// xrayBuild — расчёт файлов Xray для одного входа. Только чтение и память: диск,
// сеть и время не используются (вывод детерминирован).
type xrayBuild struct {
	groups  []Group
	rules   RulesConfig
	probe   ProbeConfig
	catalog []sources.CatalogNode
	// live — выбор участников ручных групп: ID группы → участник.
	live map[string]string
	// autoOK и fallbackOK — режимы «авто» и «резерв» доступны на версии Xray.
	autoOK, fallbackOK bool
	apiInbound         bool

	byID      map[string]*Group
	nodeByRef map[string]sources.CatalogNode
	res       map[string]Resolved
	balancers map[string]*xrayBalancer
	sel       map[string]string
	visiting  map[string]bool
	// loopRules — правила loopback ручных групп, выбравших «авто»/«резерв».
	loopRules []any
}

// newXrayBuild готовит расчёт: состав каждой группы по списку узлов Xray и
// balancer доступных режимов.
func newXrayBuild(b xrayBuild) *xrayBuild {
	b.byID = make(map[string]*Group, len(b.groups))
	b.nodeByRef = make(map[string]sources.CatalogNode, len(b.catalog))
	b.res = make(map[string]Resolved, len(b.groups))
	b.balancers = map[string]*xrayBalancer{}
	b.sel = map[string]string{}
	b.visiting = map[string]bool{}
	for i := range b.groups {
		b.byID[b.groups[i].ID] = &b.groups[i]
	}
	for _, n := range b.catalog {
		b.nodeByRef[n.Ref] = n
	}
	for _, g := range b.groups {
		b.res[g.ID] = Resolve(g, b.catalog, b.groups)
	}
	for _, g := range b.groups {
		if bal := b.balancerOf(g); bal != nil {
			b.balancers[g.ID] = bal
		}
	}
	return &b
}

// balancerOf строит balancer группы «авто» или «резерв»; nil — группа в Xray не
// материализуется как balancer: режим недоступен на версии ядра (D-42) или нет
// основных узлов (D-13). Резервный узел в основные не входит.
func (b *xrayBuild) balancerOf(g Group) *xrayBalancer {
	switch {
	case g.Mode == ModeAuto && b.autoOK:
	case g.Mode == ModeFallback && b.fallbackOK:
	default:
		return nil
	}
	fallback := TagBlock
	if g.Mode == ModeFallback {
		if r, ok := b.nodeByRef[g.Reserve]; ok && r.XrayTag != "" {
			fallback = r.XrayTag
		}
	}
	seen := map[string]bool{}
	var selector []string
	for _, n := range b.res[g.ID].Nodes {
		if g.Mode == ModeFallback && n.Ref == g.Reserve {
			continue
		}
		if n.XrayTag == "" || seen[n.XrayTag] {
			continue
		}
		seen[n.XrayTag] = true
		selector = append(selector, n.XrayTag)
	}
	if len(selector) == 0 {
		return nil
	}
	sort.Strings(selector)
	return &xrayBalancer{tag: GroupTag(g.ID), selector: selector, fallback: fallback}
}

// avail сообщает, что участник группы существует и пригоден в Xray.
func (b *xrayBuild) avail(member string) bool {
	switch {
	case member == memberDirect || member == memberReject:
		return true
	case strings.HasPrefix(member, memberNode):
		_, ok := b.nodeByRef[strings.TrimPrefix(member, memberNode)]
		return ok
	case strings.HasPrefix(member, memberGroup):
		g := b.byID[strings.TrimPrefix(member, memberGroup)]
		if g == nil {
			return false
		}
		if g.Mode == ModeManual {
			return b.selection(*g) != ""
		}
		return b.balancers[g.ID] != nil
	}
	return false
}

// selection возвращает выбранного участника ручной группы (результат
// effectiveSelection с выбором из Live[selection]); пусто — допустимых нет.
func (b *xrayBuild) selection(g Group) string {
	if v, ok := b.sel[g.ID]; ok {
		return v
	}
	if b.visiting[g.ID] {
		// Цикл вложенности: его блокирует проверка, генератор не зацикливается.
		return ""
	}
	b.visiting[g.ID] = true
	v := effectiveSelection(g, b.live[g.ID], b.avail, nodeRefs(b.res[g.ID].Nodes)...)
	delete(b.visiting, g.ID)
	b.sel[g.ID] = v
	return v
}

// freedomOutbound — прямой выход с меткой сокета 255.
func freedomOutbound(tag string) map[string]any {
	return map[string]any{
		"tag":      tag,
		"protocol": "freedom",
		"streamSettings": map[string]any{
			"sockopt": map[string]any{"mark": xrayMark},
		},
	}
}

// blackholeOutbound — обрыв соединения.
func blackholeOutbound(tag string) map[string]any {
	return map[string]any{"tag": tag, "protocol": "blackhole"}
}

// groupOutbound строит исходящий ручной группы под её тегом: копия выбранного
// узла, freedom для DIRECT, blackhole для REJECT и пустой группы, для
// вложенной ручной группы — её выбранный участник, для «авто»/«резерва» —
// loopback и правило на balancer (D-08).
func (b *xrayBuild) groupOutbound(top Group) map[string]any {
	tag := GroupTag(top.ID)
	cur := top
	for range len(b.groups) + 1 {
		sel := b.selection(cur)
		switch {
		case sel == memberDirect:
			return freedomOutbound(tag)
		case strings.HasPrefix(sel, memberNode):
			n, ok := b.nodeByRef[strings.TrimPrefix(sel, memberNode)]
			if !ok {
				return blackholeOutbound(tag)
			}
			ob, err := nodes.XrayOutbound(n.Node, tag)
			if err != nil {
				// Узел каталога прошёл пробную эмиссию; сюда не доходим. Закрытый
				// выход надёжнее, чем исходящий по умолчанию.
				log.Printf("[routing] узел %s не записан для Xray в группу %s", n.XrayTag, tag)
				return blackholeOutbound(tag)
			}
			return ob
		case strings.HasPrefix(sel, memberGroup):
			h := b.byID[strings.TrimPrefix(sel, memberGroup)]
			if h == nil {
				return blackholeOutbound(tag)
			}
			if h.Mode == ModeManual {
				cur = *h
				continue
			}
			bal := b.balancers[h.ID]
			if bal == nil {
				return blackholeOutbound(tag)
			}
			lb := loopbackTagPrefix + top.ID
			b.loopRules = append(b.loopRules, map[string]any{
				"inboundTag":  []string{lb},
				"balancerTag": bal.tag,
			})
			return map[string]any{
				"tag":      tag,
				"protocol": "loopback",
				"settings": map[string]any{"inboundTag": lb},
			}
		}
		// REJECT и пустая группа.
		return blackholeOutbound(tag)
	}
	return blackholeOutbound(tag)
}

// build собирает файлы: исходящие панели, routing и, если есть balancer,
// observatory.
func (b *xrayBuild) build() ([]configlayer.GeneratedFile, error) {
	if err := b.checkSelectors(); err != nil {
		return nil, err
	}

	outbounds := []map[string]any{freedomOutbound(TagDirect), blackholeOutbound(TagBlock)}
	for _, g := range b.groups {
		if g.Mode == ModeManual {
			outbounds = append(outbounds, b.groupOutbound(g))
		}
	}
	groupsJSON, err := xrayJSON(map[string]any{"outbounds": outbounds})
	if err != nil {
		return nil, err
	}

	rules := make([]any, 0, len(b.rules.Items)+len(b.loopRules)+2)
	if b.apiInbound {
		// D-43: правило api первым, входящий api остаётся в чужом файле.
		rules = append(rules, map[string]any{"inboundTag": []string{"api"}, "outboundTag": "api"})
	}
	rules = append(rules, b.loopRules...)
	for i, r := range b.rules.Items {
		rule, ok, err := b.xrayRule(r)
		if err != nil {
			var ge *genError
			if errors.As(err, &ge) {
				return nil, genErr(ge.code, "правило "+strconv.Itoa(i+1))
			}
			return nil, err
		}
		if ok {
			rules = append(rules, rule)
		}
	}
	// Явное «Остальное» (D-17): не зависит от порядка исходящих.
	field, tag := b.xrayTarget(b.rules.Final)
	rules = append(rules, map[string]any{"network": "tcp,udp", field: tag})

	routing := map[string]any{"domainStrategy": "AsIs", "rules": rules}
	var balancers []any
	var subjects []string
	subjectSeen := map[string]bool{}
	for _, g := range b.groups {
		bal := b.balancers[g.ID]
		if bal == nil {
			continue
		}
		balancers = append(balancers, map[string]any{
			"tag":         bal.tag,
			"selector":    bal.selector,
			"strategy":    map[string]any{"type": "leastPing"},
			"fallbackTag": bal.fallback,
		})
		for _, t := range bal.selector {
			if !subjectSeen[t] {
				subjectSeen[t] = true
				subjects = append(subjects, t)
			}
		}
	}
	if len(balancers) > 0 {
		routing["balancers"] = balancers
	}
	routingJSON, err := xrayJSON(map[string]any{"routing": routing})
	if err != nil {
		return nil, err
	}

	files := []configlayer.GeneratedFile{
		{Kernel: configlayer.KernelXray, RelPath: xrayGroupsFile, Kind: configlayer.KindXrayJSON, Content: groupsJSON},
		{Kernel: configlayer.KernelXray, RelPath: xrayRoutingFile, Kind: configlayer.KindXrayJSON, Content: routingJSON},
	}
	if len(balancers) > 0 {
		sort.Strings(subjects)
		obsJSON, err := xrayJSON(map[string]any{"burstObservatory": map[string]any{
			"subjectSelector": subjects,
			"pingConfig": map[string]any{
				"destination":  b.probe.URL,
				"connectivity": "",
				"interval":     strconv.Itoa(b.probe.IntervalS) + "s",
				"timeout":      strconv.Itoa(b.probe.TimeoutS) + "s",
				"sampling":     ProbeSampling,
			},
		}})
		if err != nil {
			return nil, err
		}
		files = append(files, configlayer.GeneratedFile{
			Kernel: configlayer.KernelXray, RelPath: xrayObservatoryFile, Kind: configlayer.KindXrayJSON, Content: obsJSON,
		})
	}
	return files, nil
}

// checkSelectors проверяет, что тег узла в selector не является началом другого
// тега: selector Xray работает по префиксу, и тег «a» захватил бы «a-2».
// Теги с суффиксом из nodeIDs именно такие. Проверка идёт по тегам всего
// каталога и по тегам панели.
func (b *xrayBuild) checkSelectors() error {
	if len(b.balancers) == 0 {
		return nil
	}
	all := make([]string, 0, len(b.catalog)+len(b.groups)+2)
	for _, n := range b.catalog {
		if n.XrayTag != "" {
			all = append(all, n.XrayTag)
		}
	}
	for _, g := range b.groups {
		all = append(all, GroupTag(g.ID))
	}
	all = append(all, TagDirect, TagBlock)
	sort.Strings(all)
	all = slices.Compact(all)
	for _, g := range b.groups {
		bal := b.balancers[g.ID]
		if bal == nil {
			continue
		}
		for _, t := range bal.selector {
			i := sort.SearchStrings(all, t)
			if i+1 < len(all) && all[i] == t && strings.HasPrefix(all[i+1], t) {
				return genErr(genErrSelectorPrefix, t)
			}
		}
	}
	return nil
}

// xrayJSON сериализует файл Xray: ключи карт сортируются, отступы два пробела,
// в конце перевод строки (как nodes.XrayFile).
func xrayJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// xrayModeAvailable — функция доступна на установленной версии Xray;
// неопределённая версия функции не выключает (T-144-05).
func (s *Service) xrayModeAvailable(f configlayer.Feature) bool {
	return configlayer.FeatureAvailable(configlayer.KernelXray, s.d.KernelVersion(configlayer.KernelXray), f) != configlayer.Unavailable
}

// liveSelection читает Live[selection]: ID группы → участник. Порча живых данных
// выбор не отменяет, группы просто выбирают по умолчанию.
func liveSelection(live configlayer.Sections) map[string]string {
	out := map[string]string{}
	raw, ok := live[LiveKeySelection]
	if !ok || isEmptyRaw(raw) {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]string{}
	}
	return out
}

// hashParts — sha256 от частей, разделённых нулевым байтом.
func hashParts(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Service) genGet(key string) ([]configlayer.GeneratedFile, bool) {
	s.gmu.Lock()
	defer s.gmu.Unlock()
	f, ok := s.gens[key]
	return f, ok
}

func (s *Service) genPut(key string, files []configlayer.GeneratedFile) {
	s.gmu.Lock()
	defer s.gmu.Unlock()
	if _, exists := s.gens[key]; !exists {
		s.genOrder = append(s.genOrder, key)
		for len(s.genOrder) > maxGenMemo {
			delete(s.gens, s.genOrder[0])
			s.genOrder = s.genOrder[1:]
		}
	}
	s.gens[key] = files
}

// generateXray разбирает секции, считает файлы и мемоизирует результат по
// хэшу входа: секции, выбор участников, узлы каталога Xray, факты слоя и
// доступность режимов.
func (s *Service) generateXray(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
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

	autoOK := s.xrayModeAvailable(configlayer.FeatureXrayAutoGroup)
	fallbackOK := s.xrayModeAvailable(configlayer.FeatureXrayFallbackGroup)

	parts := []string{
		xrayGenID, string(in.Mode), s.d.GOARCH,
		strconv.FormatBool(autoOK), strconv.FormatBool(fallbackOK),
		strconv.FormatBool(in.Routing.XrayAPIInbound),
		string(in.Sections[GroupsSection]), string(in.Sections[RulesSection]), string(in.Sections[ProbeSection]),
		string(in.Live[LiveKeySelection]),
	}
	for _, n := range cat.Xray {
		parts = append(parts, n.Ref, n.XrayTag, n.Node.Name, n.Node.Text)
	}
	key := hashParts(parts...)
	if files, ok := s.genGet(key); ok {
		return slices.Clone(files), nil
	}

	b := newXrayBuild(xrayBuild{
		groups: groups.Items, rules: rules, probe: probe, catalog: cat.Xray,
		live: liveSelection(in.Live), autoOK: autoOK, fallbackOK: fallbackOK,
		apiInbound: in.Routing.XrayAPIInbound,
	})
	files, err := b.build()
	if err != nil {
		return nil, err
	}
	s.genPut(key, files)
	return slices.Clone(files), nil
}
