package routing

import (
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
)

// Ошибки сервиса. Обработчики сопоставляют их через errors.Is и отдают
// стабильные коды API.
var (
	// ErrInvalidInput — недопустимый ввод; детали (поле, причина) в
	// *InvalidInputError.
	ErrInvalidInput = errors.New("routing: недопустимый ввод")
	// ErrGroupNotFound — группы с таким ID нет.
	ErrGroupNotFound = errors.New("routing: группа не найдена")
	// ErrRuleNotFound — правила с таким ID нет.
	ErrRuleNotFound = errors.New("routing: правило не найдено")
	// ErrGroupCycle — вложенность групп образует цикл; детали в *GroupCycleError.
	ErrGroupCycle = errors.New("routing: вложенность групп образует цикл")
	// ErrNameTaken — группа с таким именем уже есть (без учёта регистра).
	ErrNameTaken = errors.New("routing: имя группы занято")

	errRandUnavailable = errors.New("routing: случайность недоступна")
)

// Коды причин ErrInvalidInput (поле Reason): стабильны, клиент выбирает по ним
// текст ошибки.
const (
	reasonSectionInvalid = "section_invalid"
	reasonNameEmpty      = "name_empty"
	reasonNameTooLong    = "name_too_long"
	reasonNameChars      = "name_chars"
	reasonNameEdges      = "name_edges"
	reasonNameReserved   = "name_reserved"
	reasonModeInvalid    = "mode_invalid"
	reasonFilterTooMany  = "filter_too_many"
	reasonFilterTooLong  = "filter_too_long"
	reasonFilterBacktick = "filter_backtick"
	reasonRegexInvalid   = "regex_invalid"
	reasonRegexBacktick  = "regex_backtick"
	reasonSourceMissing  = "source_missing"
	reasonPickInvalid    = "pick_invalid"
	reasonTooMany        = "too_many"
	reasonMembersManual  = "members_manual_only"
	reasonMemberKind     = "member_kind_invalid"
	reasonMemberSelf     = "member_self"
	reasonMemberMissing  = "member_group_missing"
	reasonReserveNeeded  = "reserve_required"
	reasonReserveInvalid = "reserve_invalid"
	reasonReserveMode    = "reserve_fallback_only"
	reasonCondsRequired  = "conds_required"
	reasonCondsTooMany   = "conds_too_many"
	reasonRawWithConds   = "raw_with_conds"
	reasonCondType       = "cond_type_invalid"
	reasonCondConflict   = "cond_conflict"
	reasonValuesRequired = "values_required"
	reasonValuesForbid   = "values_forbidden"
	reasonValuesTooMany  = "values_too_many"
	reasonListsRequired  = "lists_required"
	reasonListsForbid    = "lists_forbidden"
	reasonListInvalid    = "list_invalid"
	reasonDomainInvalid  = "domain_invalid"
	reasonRegexpInvalid  = "regexp_invalid"
	reasonIPInvalid      = "ip_invalid"
	reasonPortInvalid    = "port_invalid"
	reasonNetInvalid     = "network_invalid"
	reasonTargetInvalid  = "target_invalid"
	reasonRawInvalid     = "raw_invalid"
	reasonProbeInvalid   = "probe_invalid"
	reasonOutOfRange     = "out_of_range"
)

// Пределы модели.
const (
	maxGroups        = 200
	maxRules         = 1000
	maxConds         = 8
	maxCondValues    = 5000
	maxNameRunes     = 64
	maxFilterValues  = 50
	maxFilterRunes   = 64
	maxRegexRunes    = 256
	maxMembers       = 500
	maxPicks         = 2000
	maxSourceIDs     = 200
	maxRawRuleBytes  = 4096
	maxMihomoRawLine = 1024
	maxDomainLen     = 253
	maxProbeURLLen   = 512
	minProbeInterval = 10
	maxProbeInterval = 3600
)

// InvalidInputError — недопустимый ввод: Field указывает поле (для условий —
// conds.<номер>.values), Reason — стабильный код причины.
type InvalidInputError struct {
	Field  string
	Reason string
}

func (e *InvalidInputError) Error() string {
	return ErrInvalidInput.Error() + ": " + e.Field + ": " + e.Reason
}

// Is связывает ошибку с ErrInvalidInput.
func (e *InvalidInputError) Is(target error) bool { return target == ErrInvalidInput }

func invalid(field, reason string) error { return &InvalidInputError{Field: field, Reason: reason} }

// GroupCycleError — цикл вложенности; Group — имя группы, через которую он идёт.
type GroupCycleError struct{ Group string }

func (e *GroupCycleError) Error() string { return ErrGroupCycle.Error() + ": " + e.Group }

// Is связывает ошибку с ErrGroupCycle.
func (e *GroupCycleError) Is(target error) bool { return target == ErrGroupCycle }

// reservedNames — имена, зарезервированные Mihomo: группа с таким именем
// сломала бы строки правил (D-14).
var reservedNames = []string{"DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE", "GLOBAL"}

// validateName проверяет имя группы по D-14 (уникальность — validateGroup).
func validateName(name string) error {
	if name == "" {
		return invalid("name", reasonNameEmpty)
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return invalid("name", reasonNameTooLong)
	}
	if strings.TrimSpace(name) != name {
		return invalid("name", reasonNameEdges)
	}
	for _, r := range name {
		if r == ',' || r == '(' || r == ')' || unicode.IsControl(r) {
			return invalid("name", reasonNameChars)
		}
	}
	for _, res := range reservedNames {
		if strings.EqualFold(name, res) {
			return invalid("name", reasonNameReserved)
		}
	}
	return nil
}

// dedupeStrings убирает повторы, сохраняя порядок.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// cleanFilter обрезает пробелы, выкидывает пустые значения и повторы.
func cleanFilter(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return dedupeStrings(out)
}

// validateFilters проверяет и нормализует Include и Exclude: до 50 значений по 64
// символа; при regex — выражения Go (RE2, без катастрофического отката) до 256
// символов; обратный апостроф запрещён (разделитель нескольких filter Mihomo).
func validateFilters(g *Group) error {
	for _, f := range []struct {
		field string
		list  *[]string
	}{{"include", &g.Include}, {"exclude", &g.Exclude}} {
		*f.list = cleanFilter(*f.list)
		if len(*f.list) > maxFilterValues {
			return invalid(f.field, reasonFilterTooMany)
		}
		limit, backtick := maxFilterRunes, reasonFilterBacktick
		if g.Regex {
			limit, backtick = maxRegexRunes, reasonRegexBacktick
		}
		for _, v := range *f.list {
			if utf8.RuneCountInString(v) > limit {
				return invalid(f.field, reasonFilterTooLong)
			}
			if strings.ContainsRune(v, '`') {
				return invalid(f.field, backtick)
			}
			if g.Regex {
				if _, err := regexp.Compile(v); err != nil {
					return invalid(f.field, reasonRegexInvalid)
				}
			}
		}
	}
	return nil
}

// validateGroup проверяет и нормализует группу. others — остальные группы
// секции (без правимой), sourceIDs — источники черновика.
func validateGroup(g *Group, others []Group, sourceIDs map[string]bool) error {
	normalizeGroup(g)
	if err := validateName(g.Name); err != nil {
		return err
	}
	if !validMode(g.Mode) {
		return invalid("mode", reasonModeInvalid)
	}
	for _, o := range others {
		if strings.EqualFold(o.Name, g.Name) {
			return ErrNameTaken
		}
	}
	if err := validateFilters(g); err != nil {
		return err
	}

	if len(g.Sources.IDs) > maxSourceIDs {
		return invalid("sources.ids", reasonTooMany)
	}
	g.Sources.IDs = dedupeStrings(g.Sources.IDs)
	for _, id := range g.Sources.IDs {
		if !sourceIDs[id] {
			return invalid("sources.ids", reasonSourceMissing)
		}
	}

	if len(g.Picks) > maxPicks {
		return invalid("picks", reasonTooMany)
	}
	g.Picks = dedupeStrings(g.Picks)
	for _, ref := range g.Picks {
		if !refRE.MatchString(ref) {
			return invalid("picks", reasonPickInvalid)
		}
	}

	if g.Mode != ModeManual && len(g.Members) > 0 {
		return invalid("members", reasonMembersManual)
	}
	if len(g.Members) > maxMembers {
		return invalid("members", reasonTooMany)
	}
	known := make(map[string]bool, len(others))
	for _, o := range others {
		known[o.ID] = true
	}
	members := make([]Member, 0, len(g.Members))
	seenMember := make(map[Member]bool, len(g.Members))
	for _, m := range g.Members {
		switch m.Kind {
		case KindDirect, KindReject:
			m.ID = ""
		case KindGroup:
			if g.ID != "" && m.ID == g.ID {
				return invalid("members", reasonMemberSelf)
			}
			if !known[m.ID] {
				return invalid("members", reasonMemberMissing)
			}
		default:
			return invalid("members", reasonMemberKind)
		}
		if !seenMember[m] {
			seenMember[m] = true
			members = append(members, m)
		}
	}
	g.Members = members

	switch {
	case g.Mode == ModeFallback && g.Reserve == "":
		return invalid("reserve", reasonReserveNeeded)
	case g.Mode == ModeFallback && !refRE.MatchString(g.Reserve):
		return invalid("reserve", reasonReserveInvalid)
	case g.Mode != ModeFallback && g.Reserve != "":
		return invalid("reserve", reasonReserveMode)
	}
	return nil
}

// groupCycles находит группы, лежащие на цикле вложенности (участники-группы
// ручных групп). Результат — ID в порядке следования групп в секции.
func groupCycles(groups []Group) []string {
	next := make(map[string][]string, len(groups))
	for _, g := range groups {
		if g.Mode != ModeManual {
			continue
		}
		for _, m := range g.Members {
			if m.Kind == KindGroup {
				next[g.ID] = append(next[g.ID], m.ID)
			}
		}
	}
	const (
		white = iota
		gray
		black
	)
	color := make(map[string]int, len(groups))
	onCycle := make(map[string]bool)
	var stack []string
	var visit func(id string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, to := range next[id] {
			switch color[to] {
			case white:
				visit(to)
			case gray:
				for i := len(stack) - 1; i >= 0; i-- {
					onCycle[stack[i]] = true
					if stack[i] == to {
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	for _, g := range groups {
		if color[g.ID] == white {
			visit(g.ID)
		}
	}
	var out []string
	for _, g := range groups {
		if onCycle[g.ID] {
			out = append(out, g.ID)
		}
	}
	return out
}

// --- правила ---

// domainRE — домен в ASCII (после punycode): метки из букв, цифр, дефиса и
// подчёркивания.
var domainRE = regexp.MustCompile(`^[a-z0-9_](?:[a-z0-9_-]*[a-z0-9_])?(?:\.[a-z0-9_](?:[a-z0-9_-]*[a-z0-9_])?)*$`)

// listFileRE — имя dat-файла списка (как в каталоге XKeen).
var listFileRE = regexp.MustCompile(`^[A-Za-z0-9._-]+\.dat$`)

// listTagRE — тег списка в dat.
var listTagRE = regexp.MustCompile(`^[A-Za-z0-9!@._-]{1,128}$`)

// portRE — порт или диапазон портов.
var portRE = regexp.MustCompile(`^(\d{1,5})(?:-(\d{1,5}))?$`)

// normDomain приводит домен к нижнему регистру и punycode и проверяет его.
func normDomain(v string) (string, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || utf8.RuneCountInString(v) > maxDomainLen {
		return "", false
	}
	ascii, err := idna.Punycode.ToASCII(v)
	if err != nil || len(ascii) > maxDomainLen || !domainRE.MatchString(ascii) {
		return "", false
	}
	return ascii, true
}

// plainToken сообщает, что строка годится как подстрока в правило: без пробелов,
// управляющих символов, запятых, скобок, кавычек и обратного апострофа.
func plainToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(",()`\"'", r) {
			return false
		}
	}
	return true
}

// normDomainValue проверяет значение условия «домен» (D-18): по умолчанию домен
// с поддоменами; префиксы full:, keyword:, regexp:. Возвращает каноничное
// значение или код причины.
func normDomainValue(v string) (norm, reason string) {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	switch {
	case strings.HasPrefix(low, "full:"):
		d, ok := normDomain(v[len("full:"):])
		if !ok {
			return "", reasonDomainInvalid
		}
		return "full:" + d, ""
	case strings.HasPrefix(low, "keyword:"):
		k := strings.ToLower(strings.TrimSpace(v[len("keyword:"):]))
		if !plainToken(k) || utf8.RuneCountInString(k) > maxDomainLen {
			return "", reasonDomainInvalid
		}
		return "keyword:" + k, ""
	case strings.HasPrefix(low, "regexp:"):
		expr := v[len("regexp:"):]
		if expr == "" || utf8.RuneCountInString(expr) > maxRegexRunes || strings.ContainsAny(expr, ",`") {
			return "", reasonRegexpInvalid
		}
		for _, r := range expr {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return "", reasonRegexpInvalid
			}
		}
		if _, err := regexp.Compile(expr); err != nil {
			return "", reasonRegexpInvalid
		}
		return "regexp:" + expr, ""
	}
	d, ok := normDomain(v)
	if !ok {
		return "", reasonDomainInvalid
	}
	return d, ""
}

// normIPValue проверяет IP-адрес или сеть и возвращает каноничную запись.
func normIPValue(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if p, err := netip.ParsePrefix(v); err == nil {
		return p.Masked().String(), true
	}
	if a, err := netip.ParseAddr(v); err == nil && a.Zone() == "" {
		return a.String(), true
	}
	return "", false
}

// normPortValue проверяет порт (1–65535) или диапазон a-b (a ≤ b).
func normPortValue(v string) (string, bool) {
	m := portRE.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", false
	}
	lo, _ := strconv.Atoi(m[1])
	if lo < 1 || lo > 65535 {
		return "", false
	}
	if m[2] == "" {
		return strconv.Itoa(lo), true
	}
	hi, _ := strconv.Atoi(m[2])
	if hi < lo || hi > 65535 {
		return "", false
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi), true
}

// condField — имя поля условия для ошибки.
func condField(i int, part string) string { return "conds." + strconv.Itoa(i) + "." + part }

// validateCond проверяет и нормализует условие номер i.
func validateCond(i int, c *Cond) error {
	valuesField := condField(i, "values")
	listsField := condField(i, "lists")
	if len(c.Values) > maxCondValues || len(c.Lists) > maxCondValues {
		return invalid(valuesField, reasonValuesTooMany)
	}
	switch c.Type {
	case CondGeosite, CondGeoip:
		if len(c.Values) > 0 {
			return invalid(valuesField, reasonValuesForbid)
		}
		if len(c.Lists) == 0 {
			return invalid(listsField, reasonListsRequired)
		}
		want := lists.KindSite
		if c.Type == CondGeoip {
			want = lists.KindIP
		}
		for j := range c.Lists {
			l := &c.Lists[j]
			if !listFileRE.MatchString(l.File) || !listTagRE.MatchString(l.Tag) {
				return invalid(listsField, reasonListInvalid)
			}
			if l.Kind == "" {
				l.Kind = want
			}
			if l.Kind != want {
				return invalid(listsField, reasonListInvalid)
			}
		}
		return nil
	case CondBittorrent:
		if len(c.Values) > 0 {
			return invalid(valuesField, reasonValuesForbid)
		}
		if len(c.Lists) > 0 {
			return invalid(listsField, reasonListsForbid)
		}
		return nil
	case CondDomain, CondIP, CondSrcIP, CondPort, CondNetwork:
	default:
		return invalid(condField(i, "type"), reasonCondType)
	}
	if len(c.Lists) > 0 {
		return invalid(listsField, reasonListsForbid)
	}
	values := make([]string, 0, len(c.Values))
	for _, raw := range c.Values {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var (
			norm   string
			reason string
		)
		switch c.Type {
		case CondDomain:
			norm, reason = normDomainValue(raw)
		case CondIP, CondSrcIP:
			var ok bool
			if norm, ok = normIPValue(raw); !ok {
				reason = reasonIPInvalid
			}
		case CondPort:
			var ok bool
			if norm, ok = normPortValue(raw); !ok {
				reason = reasonPortInvalid
			}
		case CondNetwork:
			norm = strings.ToLower(strings.TrimSpace(raw))
			if norm != "tcp" && norm != "udp" {
				reason = reasonNetInvalid
			}
		}
		if reason != "" {
			return invalid(valuesField, reason)
		}
		values = append(values, norm)
	}
	values = dedupeStrings(values)
	if len(values) == 0 {
		return invalid(valuesField, reasonValuesRequired)
	}
	if c.Type == CondNetwork {
		// Порядок каноничен: tcp, затем udp.
		sort.Strings(values)
	}
	c.Values = values
	return nil
}

// validateTarget проверяет цель: группа с существующим ID, DIRECT или REJECT.
func validateTarget(field string, t *Target, groups []Group) error {
	switch t.Kind {
	case KindDirect, KindReject:
		t.ID = ""
		return nil
	case KindGroup:
		for _, g := range groups {
			if g.ID == t.ID {
				return nil
			}
		}
	}
	return invalid(field, reasonTargetInvalid)
}

// xrayRuleKeys — поля объекта правила Xray (RuleObject), которые допустимы в
// «своём правиле»; цель (outboundTag, balancerTag) назначает панель.
var xrayRuleKeys = map[string]bool{
	"type": true, "domain": true, "domains": true, "ip": true, "port": true,
	"sourcePort": true, "localPort": true, "localIP": true, "network": true,
	"source": true, "sourceIP": true, "user": true, "vlessRoute": true,
	"inboundTag": true, "protocol": true, "attrs": true, "process": true,
	"ruleTag": true,
}

// xrayTargetKeys — поля цели: удаляются из своего правила.
var xrayTargetKeys = []string{"outboundTag", "balancerTag"}

// mihomoRuleTypes — типы правил Mihomo, допустимые в «своём правиле». Нет MATCH и
// SUB-RULE (цель и вложенность задаёт панель) и GEOSITE/GEOIP (списки идут
// только через dat, D-19).
var mihomoRuleTypes = map[string]bool{
	"DOMAIN": true, "DOMAIN-SUFFIX": true, "DOMAIN-KEYWORD": true, "DOMAIN-REGEX": true,
	"DOMAIN-WILDCARD": true, "IP-CIDR": true, "IP-CIDR6": true, "IP-SUFFIX": true,
	"IP-ASN": true, "SRC-IP-CIDR": true, "SRC-IP-SUFFIX": true, "SRC-IP-ASN": true,
	"DST-PORT": true, "SRC-PORT": true, "IN-PORT": true, "IN-TYPE": true, "IN-USER": true,
	"IN-NAME": true, "PROCESS-NAME": true, "PROCESS-PATH": true, "PROCESS-NAME-REGEX": true,
	"PROCESS-PATH-REGEX": true, "UID": true, "NETWORK": true, "DSCP": true,
	"AND": true, "OR": true, "NOT": true, "RULE-SET": true,
}

// mihomoForbidden находит запрещённые типы в позиции типа правила: в начале
// строки или после открывающей скобки логического правила.
var mihomoForbidden = regexp.MustCompile(`(?i)(?:^|\()\s*(?:MATCH|SUB-RULE|GEOSITE|GEOIP|SRC-GEOIP)\s*[,)]`)

// checkRawXray проверяет «своё правило» Xray: один JSON-объект, только поля
// RuleObject, без цели; возвращает каноничный текст без outboundTag и balancerTag.
func checkRawXray(text string) (string, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return "", invalid("raw", reasonRawInvalid)
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", invalid("raw", reasonRawInvalid)
	}
	for _, k := range xrayTargetKeys {
		delete(obj, k)
	}
	matching := 0
	for k, v := range obj {
		if !xrayRuleKeys[k] {
			return "", invalid("raw", reasonRawInvalid)
		}
		switch k {
		case "type":
			var t string
			if json.Unmarshal(v, &t) != nil || t != "field" {
				return "", invalid("raw", reasonRawInvalid)
			}
		case "ruleTag":
		default:
			matching++
		}
		if k == "domain" || k == "domains" || k == "ip" {
			var list []string
			if err := json.Unmarshal(v, &list); err != nil {
				var one string
				if json.Unmarshal(v, &one) != nil {
					return "", invalid("raw", reasonRawInvalid)
				}
				list = []string{one}
			}
			for _, s := range list {
				low := strings.ToLower(strings.TrimSpace(s))
				if strings.HasPrefix(low, "geosite:") || strings.HasPrefix(low, "geoip:") {
					return "", invalid("raw", reasonRawInvalid)
				}
			}
		}
	}
	// Правило без условий поймало бы весь трафик.
	if matching == 0 {
		return "", invalid("raw", reasonRawInvalid)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return "", invalid("raw", reasonRawInvalid)
	}
	return string(out), nil
}

// checkRawMihomo проверяет «своё правило» Mihomo: одна строка без цели, тип из
// списка, скобки сбалансированы, без MATCH, SUB-RULE, GEOSITE и GEOIP, после
// значения нет полей, кроме no-resolve и src (mihomoRawShape).
func checkRawMihomo(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > maxMihomoRawLine {
		return "", invalid("raw", reasonRawInvalid)
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", invalid("raw", reasonRawInvalid)
		}
	}
	head, _, _ := strings.Cut(text, ",")
	if !mihomoRuleTypes[strings.ToUpper(strings.TrimSpace(head))] || mihomoForbidden.MatchString(text) {
		return "", invalid("raw", reasonRawInvalid)
	}
	depth := 0
	for _, r := range text {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return "", invalid("raw", reasonRawInvalid)
			}
		}
	}
	if depth != 0 {
		return "", invalid("raw", reasonRawInvalid)
	}
	if !mihomoRawShape(strings.ToUpper(strings.TrimSpace(head)), text) {
		return "", invalid("raw", reasonRawInvalid)
	}
	return text, nil
}

// mihomoRawParams — параметры простого правила Mihomo, которые панель переносит
// за цель (rawMihomoLine).
var mihomoRawParams = map[string]bool{"no-resolve": true, "src": true}

// mihomoRawRegexTypes — типы с регулярным выражением в значении: параметров у них
// нет, а запятая в значении ломает разбор строки в Mihomo.
var mihomoRawRegexTypes = map[string]bool{"DOMAIN-REGEX": true, "PROCESS-NAME-REGEX": true, "PROCESS-PATH-REGEX": true}

// mihomoRawShape проверяет, что после значения нет ничего, кроме допустимых
// параметров. Цель правила Mihomo — поле сразу за значением, поэтому лишнее
// поле (DST-PORT,22,DIRECT) заняло бы место цели панели и увело трафик мимо
// группы. Простое правило: тип, значение и только no-resolve/src (у regexp-типов
// параметров нет). Логическое: после закрывающей скобки ничего.
func mihomoRawShape(typ, text string) bool {
	if logicTypes[typ] {
		_, rest, _ := strings.Cut(text, ",")
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "(") {
			return false
		}
		depth := 0
		for i, r := range rest {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return strings.TrimSpace(rest[i+1:]) == ""
				}
			}
		}
		return false
	}
	parts := strings.Split(text, ",")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return false
	}
	extra := parts[2:]
	if len(extra) > 0 && mihomoRawRegexTypes[typ] {
		return false
	}
	for _, p := range extra {
		if !mihomoRawParams[strings.ToLower(strings.TrimSpace(p))] {
			return false
		}
	}
	return true
}

// validateRaw проверяет «своё правило ядра» и нормализует его текст.
func validateRaw(raw *RawRule) error {
	if len(raw.Text) > maxRawRuleBytes {
		return invalid("raw", reasonRawInvalid)
	}
	var (
		text string
		err  error
	)
	switch raw.Kernel {
	case configlayer.KernelXray:
		text, err = checkRawXray(raw.Text)
	case configlayer.KernelMihomo:
		text, err = checkRawMihomo(raw.Text)
	default:
		return invalid("raw", reasonRawInvalid)
	}
	if err != nil {
		return err
	}
	raw.Text = text
	return nil
}

// xrayCondFields — поле правила routing Xray для каждого типа условия. Два
// условия одного поля не выражаются в одном правиле Xray (xray_cond_conflict).
// Таблицу читает scripts/check-routing-api.js: по строке на тип, вид `Тип: "поле",`.
var xrayCondFields = map[string]string{
	CondDomain:     "domain",
	CondGeosite:    "domain",
	CondIP:         "ip",
	CondGeoip:      "ip",
	CondPort:       "port",
	CondNetwork:    "network",
	CondSrcIP:      "source",
	CondBittorrent: "protocol",
}

// xrayCondField возвращает поле правила Xray для типа условия; неизвестный тип —
// сам тип (он отвергается раньше, validateCond). То же сопоставление, что в
// xrayRule (rules_xray.go) и COND_XRAY_FIELD во фронтенде (routingView.ts), их
// сверяет scripts/check-routing-api.js.
func xrayCondField(t string) string {
	if f, ok := xrayCondFields[t]; ok {
		return f
	}
	return t
}

// condConflict возвращает индекс первого условия, чьё поле Xray уже занято более
// ранним условием: домены и geosite — одно поле domain, IP и geoip — одно поле ip.
func condConflict(conds []Cond) (int, bool) {
	taken := make(map[string]bool, len(conds))
	for i, c := range conds {
		f := xrayCondField(c.Type)
		if taken[f] {
			return i, true
		}
		taken[f] = true
	}
	return 0, false
}

// validateRule проверяет и нормализует правило: 1–8 условий или своё правило
// ядра, цель — группа, DIRECT или REJECT. Не больше одного условия на поле Xray
// (cond_conflict, D-15): модель одна для обоих ядер, ядро, установленное позже,
// берётся автоматически, и его сборка не должна падать.
func validateRule(r *Rule, groups []Group) error {
	normalizeRule(r)
	switch {
	case r.Raw != nil && len(r.Conds) > 0:
		return invalid("raw", reasonRawWithConds)
	case r.Raw != nil:
		if err := validateRaw(r.Raw); err != nil {
			return err
		}
	case len(r.Conds) == 0:
		return invalid("conds", reasonCondsRequired)
	case len(r.Conds) > maxConds:
		return invalid("conds", reasonCondsTooMany)
	}
	for i := range r.Conds {
		if err := validateCond(i, &r.Conds[i]); err != nil {
			return err
		}
	}
	if i, bad := condConflict(r.Conds); bad {
		return invalid(condField(i, "type"), reasonCondConflict)
	}
	return validateTarget("target", &r.Target, groups)
}

// validateProbe проверяет параметры проверки узлов: адрес http или https,
// интервал 10–3600 с, таймаут не меньше секунды и меньше интервала.
func validateProbe(p ProbeConfig) error {
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || len(p.URL) > maxProbeURLLen {
		return invalid("url", reasonProbeInvalid)
	}
	for _, r := range p.URL {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalid("url", reasonProbeInvalid)
		}
	}
	if p.IntervalS < minProbeInterval || p.IntervalS > maxProbeInterval {
		return invalid("interval_s", reasonProbeInvalid)
	}
	if p.TimeoutS < 1 || p.TimeoutS >= p.IntervalS {
		return invalid("timeout_s", reasonProbeInvalid)
	}
	return nil
}

// --- проверка до записи ---

// Коды проблем проверки.
const (
	issueGroupEmpty     = "group_empty"
	issueGroupCycle     = "group_cycle"
	issueTargetMissing  = "target_missing"
	issueSectionInvalid = "section_invalid"
	// issueCondConflict — включённое правило в черновике с двумя условиями одного
	// поля Xray: генератор Xray такого не выразит (xray_cond_conflict).
	issueCondConflict = "cond_conflict"
	// issueListNotFound — правило ссылается на список, которого нет на роутере
	// (файл dat удалён или тег пропал после обновления баз): D-21.
	issueListNotFound = "list_not_found"
	// issueListTooLarge — список больше предела размера для Mihomo (D-19).
	issueListTooLarge = "list_too_large"
)

// Коды предупреждений проверки: «Применить» они не блокируют.
const (
	// warnGroupEmptyKernel — группа пуста только для одного ядра (D-13).
	warnGroupEmptyKernel = "group_empty_kernel"
	// warnModeUnavailable — режим группы недоступен по версии ядра.
	warnModeUnavailable = "mode_unavailable"
	// warnPickStale — выбранного узла нет ни в одном установленном ядре.
	warnPickStale = "pick_stale"
	// warnListHeavy — список тяжёлый (больше lists.WarnEntries записей): Mihomo
	// будет грузить его заметно дольше (D-19).
	warnListHeavy = "list_heavy"
)

// checkerID — идентификатор проверки в реестре слоя.
const checkerID = "routing_check"

// IssueView — проблема или предупреждение проверки: код и параметры текста.
// Key — ключ места ("group:<id>", "rule:<id>", "final", "section:<имя>"); по нему
// проблема привязывается к строке в интерфейсе.
type IssueView struct {
	Code   string            `json:"code"`
	Params map[string]string `json:"params"`
	Key    string            `json:"key,omitempty"`
}

// catalogFunc — источник каталога узлов (sources.Service.Catalog).
type catalogFunc func(configlayer.Input, configlayer.InstalledKernels) (sources.Catalog, error)

// versionFunc — сырая версия ядра ("xray" или "mihomo"); пусто — не определена
// (Deps.KernelVersion).
type versionFunc func(kernel string) string

// groupModeFeature — функция, от версии ядра которой зависит режим группы;
// пустая — режим порога не имеет. Группы Mihomo зависят от mihomo_groups при
// любом режиме (empty-fallback задан у каждой группы панели).
func groupModeFeature(kernel string, mode GroupMode) configlayer.Feature {
	if kernel == configlayer.KernelMihomo {
		return configlayer.FeatureMihomoGroups
	}
	switch mode {
	case ModeAuto:
		return configlayer.FeatureXrayAutoGroup
	case ModeFallback:
		return configlayer.FeatureXrayFallbackGroup
	}
	return ""
}

// groupEmptiness — пустота групп для одного набора узлов (одного ядра или
// объединённого каталога) по правилу D-36: то же правило, что у генераторов
// (xrayBuild.selection для Xray, выпуск ручной группы mihomoBuild.isEmitted для
// Mihomo). Ручная группа пуста, если у неё нет действующего участника по
// effectiveSelection с пустым выбором: вложенная группа считается, если она не
// пуста; DIRECT и REJECT — только на первом месте; узел — из состава Resolve.
// Группа «авто» пуста, если в составе нет узлов, «резерв» — если нет основных
// узлов (без резервного). Один расчёт для всех потребителей пустоты (analyze, последствия удаления, «Сейчас»). Версия ядра
// пустоту не меняет: режим, недоступный по версии, — отдельное предупреждение.
type groupEmptiness struct {
	groups   []Group
	nodes    []sources.CatalogNode
	byID     map[string]*Group
	memo     map[string]bool
	visiting map[string]bool
}

// newGroupEmptiness готовит расчёт пустоты групп для набора узлов.
func newGroupEmptiness(groups []Group, nodes []sources.CatalogNode) *groupEmptiness {
	e := &groupEmptiness{
		groups: groups, nodes: nodes,
		byID: make(map[string]*Group, len(groups)),
		memo: map[string]bool{}, visiting: map[string]bool{},
	}
	for i := range groups {
		e.byID[groups[i].ID] = &groups[i]
	}
	return e
}

// empty сообщает, что у группы нет действующего участника для этого набора узлов.
// Группы нет в списке и цикл вложенности — пусто (цикл отдельно блокирует
// group_cycle).
func (e *groupEmptiness) empty(id string) bool {
	g := e.byID[id]
	if g == nil || e.visiting[id] {
		return true
	}
	if v, ok := e.memo[id]; ok {
		return v
	}
	e.visiting[id] = true
	r := Resolve(*g, e.nodes, e.groups)
	var empty bool
	if g.Mode == ModeManual {
		avail := func(member string) bool {
			if gid, ok := strings.CutPrefix(member, memberGroup); ok {
				return !e.empty(gid)
			}
			// DIRECT и REJECT допустимы всегда, ссылки узлов приходят из состава.
			return true
		}
		empty = effectiveSelection(*g, "", avail, nodeRefs(r.Nodes)...) == ""
	} else {
		// Резервный узел «резерва» в основные не входит (balancerOf, mains): без
		// других узлов группу оба ядра заблокируют.
		main := r.Nodes
		if g.Mode == ModeFallback && g.Reserve != "" {
			main = withoutRef(main, g.Reserve)
		}
		empty = len(main) == 0
	}
	delete(e.visiting, id)
	e.memo[id] = empty
	return empty
}

// emptyGroups — ID каждой группы → пуста ли она для набора узлов (groupEmptiness).
func emptyGroups(groups []Group, nodes []sources.CatalogNode) map[string]bool {
	e := newGroupEmptiness(groups, nodes)
	out := make(map[string]bool, len(groups))
	for _, g := range groups {
		out[g.ID] = e.empty(g.ID)
	}
	return out
}

// analyze считает проблемы и предупреждения маршрутизации по входным данным:
// испорченные секции, группы, пустые для всех установленных ядер (пустота — по
// правилу D-36, emptyGroups), циклы
// вложенности, цели правил на несуществующие группы и включённые правила с двумя
// условиями одного поля Xray (cond_conflict) (issues, блокируют «Применить»), а также группы, пустые только для части ядер, режимы, недоступные
// по версии ядра, и устаревшие отдельные узлы (warnings, не блокируют). Состав
// считает Resolve на каталоге источников. Это единственный расчёт проблем:
// проверка до записи и представление вызывают его. Сеть не трогается, dat читает
// только индекс списков (мемоизирован по имени, размеру и времени файла);
// ошибка каталога (его сообщают генераторы источников) пропускает только
// проверки, которым нужен состав. Правила со списками, которых нет на роутере
// (list_not_found) или которые больше предела Mihomo (list_too_large), при
// желаемом routing.managed блокируют «Применить», иначе — предупреждения;
// тяжёлый список (list_heavy) — всегда предупреждение. Индекс ix nil — списки
// не проверяются.
func analyze(in configlayer.Input, installed configlayer.InstalledKernels, catalog catalogFunc, version versionFunc, ix *lists.Index) (issues, warnings []IssueView) {
	issues, warnings = []IssueView{}, []IssueView{}
	sectionBad := func(name string) {
		issues = append(issues, IssueView{Code: issueSectionInvalid, Params: map[string]string{"section": name}, Key: "section:" + name})
	}

	groups, gerr := parseGroups(in.Sections[GroupsSection])
	if gerr != nil {
		sectionBad(GroupsSection)
	}
	rules, rerr := parseRules(in.Sections[RulesSection])
	if rerr != nil {
		sectionBad(RulesSection)
	}
	if probe, perr := parseProbe(in.Sections[ProbeSection], ""); perr != nil || validateProbe(probe) != nil {
		sectionBad(ProbeSection)
	}

	if gerr == nil {
		var cat sources.Catalog
		haveCat := false
		if installed.Xray || installed.Mihomo {
			if c, err := catalog(in, installed); err == nil {
				cat, haveCat = c, true
			}
		}
		kernels := []struct {
			name  string
			on    bool
			nodes []sources.CatalogNode
		}{
			{configlayer.KernelXray, installed.Xray, cat.Xray},
			{configlayer.KernelMihomo, installed.Mihomo, cat.Mihomo},
		}
		// Пустота групп по каждому установленному ядру считается один раз: тем же
		// правилом D-36, что у генераторов.
		emptyBy := map[string]map[string]bool{}
		if haveCat {
			for _, k := range kernels {
				if k.on {
					emptyBy[k.name] = emptyGroups(groups.Items, k.nodes)
				}
			}
		}
		for _, g := range groups.Items {
			key := "group:" + g.ID
			params := func(extra ...string) map[string]string {
				m := map[string]string{"group": g.Name}
				for i := 0; i+1 < len(extra); i += 2 {
					m[extra[i]] = extra[i+1]
				}
				return m
			}
			if haveCat {
				var emptyOn []string
				active := 0
				staleIn := map[string]int{}
				for _, k := range kernels {
					if !k.on {
						continue
					}
					active++
					if emptyBy[k.name][g.ID] {
						emptyOn = append(emptyOn, k.name)
					}
					for _, ref := range Resolve(g, k.nodes, groups.Items).Stale {
						staleIn[ref]++
					}
				}
				switch {
				case active > 0 && len(emptyOn) == active:
					issues = append(issues, IssueView{Code: issueGroupEmpty, Params: params(), Key: key})
				default:
					for _, k := range emptyOn {
						warnings = append(warnings, IssueView{Code: warnGroupEmptyKernel, Params: params("kernel", k), Key: key})
					}
				}
				// Узел устарел, если его нет ни в одном установленном ядре: узел,
				// который ядро просто не умеет (tuic в Xray), устаревшим не считается.
				for _, ref := range g.Picks {
					if active > 0 && staleIn[ref] == active {
						warnings = append(warnings, IssueView{Code: warnPickStale, Params: params(), Key: key})
						break
					}
				}
			}
			for _, k := range kernels {
				f := groupModeFeature(k.name, g.Mode)
				if !k.on || f == "" {
					continue
				}
				if configlayer.FeatureAvailable(k.name, version(k.name), f) == configlayer.Unavailable {
					warnings = append(warnings, IssueView{
						Code:   warnModeUnavailable,
						Params: params("kernel", k.name, "min", configlayer.FeatureMinVersion(f)),
						Key:    key,
					})
				}
			}
		}
		for _, id := range groupCycles(groups.Items) {
			for _, g := range groups.Items {
				if g.ID == id {
					issues = append(issues, IssueView{Code: issueGroupCycle, Params: map[string]string{"group": g.Name}, Key: "group:" + g.ID})
				}
			}
		}
	}

	if rerr == nil {
		exists := make(map[string]bool, len(groups.Items))
		for _, g := range groups.Items {
			exists[g.ID] = true
		}
		missing := func(t Target) bool { return t.Kind == KindGroup && (gerr != nil || !exists[t.ID]) }
		for i, r := range rules.Items {
			if missing(r.Target) {
				issues = append(issues, IssueView{Code: issueTargetMissing, Params: map[string]string{"rule": strconv.Itoa(i + 1)}, Key: "rule:" + r.ID})
			}
			// Правило, записанное в обход сервиса, с двумя условиями одного поля
			// Xray: помечается до «Применить», а не сбоем генератора.
			if _, bad := condConflict(r.Conds); bad && r.Enabled && r.Raw == nil {
				issues = append(issues, IssueView{Code: issueCondConflict, Params: map[string]string{"rule": strconv.Itoa(i + 1)}, Key: "rule:" + r.ID})
			}
		}
		if missing(rules.Final) {
			issues = append(issues, IssueView{Code: issueTargetMissing, Params: map[string]string{"rule": "final"}, Key: "final"})
		}
		problems, heavy := listProblems(rules.Items, installed, ix, version)
		if configlayer.DesiredRouting(in.Sections).Managed {
			issues = append(issues, problems...)
		} else {
			warnings = append(warnings, problems...)
		}
		warnings = append(warnings, heavy...)
	}
	return issues, warnings
}

// listProblems проверяет списки dat, на которые ссылаются включённые правила
// (D-19, D-21): нет файла или тега — list_not_found {file, tag, rule}; для
// Mihomo (установлен, группы доступны по версии) список больше предела —
// list_too_large {file, tag}; тяжёлый — list_heavy {file, tag, entries}
// (отдельным списком: это предупреждение при любом желаемом состоянии). Ошибка
// чтения индекса даёт list_not_found для той же пары. Свои правила ядра списков
// не содержат. Один список в правиле — одна запись, номер правила считается с
// единицы по всему списку правил.
func listProblems(items []Rule, installed configlayer.InstalledKernels, ix *lists.Index, version versionFunc) (problems, heavy []IssueView) {
	if ix == nil {
		return nil, nil
	}
	mihomo := installed.Mihomo &&
		configlayer.FeatureAvailable(configlayer.KernelMihomo, version(configlayer.KernelMihomo), configlayer.FeatureMihomoGroups) != configlayer.Unavailable
	for i, r := range items {
		if !r.Enabled || r.Raw != nil {
			continue
		}
		key := "rule:" + r.ID
		seen := map[string]bool{}
		for _, c := range r.Conds {
			if c.Type != CondGeosite && c.Type != CondGeoip {
				continue
			}
			for _, ref := range c.Lists {
				k := listKey(ref.File, ref.Tag)
				if seen[k] {
					continue
				}
				seen[k] = true
				notFound := IssueView{Code: issueListNotFound, Key: key,
					Params: map[string]string{"file": ref.File, "tag": ref.Tag, "rule": strconv.Itoa(i + 1)}}
				if _, _, ok, err := ix.Has(ref.File, ref.Tag); err != nil || !ok {
					problems = append(problems, notFound)
					continue
				}
				if !mihomo || !mihomoApplicable(r) {
					continue
				}
				cat, err := ix.Extract(ref.File, ref.Tag)
				switch {
				case errors.Is(err, lists.ErrListTooLarge):
					problems = append(problems, IssueView{Code: issueListTooLarge, Key: key,
						Params: map[string]string{"file": ref.File, "tag": ref.Tag}})
				case err != nil:
					problems = append(problems, notFound)
				case cat.Heavy:
					heavy = append(heavy, IssueView{Code: warnListHeavy, Key: key,
						Params: map[string]string{"file": ref.File, "tag": ref.Tag, "entries": strconv.Itoa(cat.Entries)}})
				}
			}
		}
	}
	return problems, heavy
}

// routingChecker — проверка до записи (configlayer.Checker). При желаемом
// routing.managed=false ничего не блокирует: группы не материализуются,
// «Применить только узлы» разрешено (D-01).
type routingChecker struct{ s *Service }

func (routingChecker) ID() string { return checkerID }

// Check отдаёт блокирующие проблемы модели. Диск не читает: каталог источников
// мемоизирован в sources.
func (c routingChecker) Check(in configlayer.Input, installed configlayer.InstalledKernels) []configlayer.BuildIssue {
	if !configlayer.DesiredRouting(in.Sections).Managed {
		return nil
	}
	issues, _ := analyze(in, installed, c.s.d.Sources.Catalog, c.s.d.KernelVersion, c.s.d.Lists)
	out := make([]configlayer.BuildIssue, 0, len(issues))
	for _, is := range issues {
		out = append(out, configlayer.BuildIssue{Key: is.Key, Code: is.Code, Params: is.Params})
	}
	return out
}

// Checker возвращает проверку routing_check для Layer.RegisterChecker.
func (s *Service) Checker() configlayer.Checker { return routingChecker{s: s} }
