package routing

import (
	"net/netip"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Коды ошибок генератора Mihomo (начало текста ошибки).
const (
	genErrMihomoRule = "mihomo_rule_invalid"
	// genErrMihomoRegexParens — регулярное выражение со скобками в правиле из
	// нескольких условий: скобки сломали бы разбор логического правила Mihomo,
	// а пропуск правила направил бы трафик иначе, чем задумано.
	genErrMihomoRegexParens = "mihomo_regex_parens"
)

// mihomoMaxPortRanges — сколько значений в одном правиле DST-PORT (предел
// Mihomo — 28 диапазонов).
const mihomoMaxPortRanges = 28

// Цели правил Mihomo без группы.
const (
	mihomoDirect = "DIRECT"
	mihomoReject = "REJECT"
)

// mihomoLeaf — простое правило Mihomo: тип, значение и признак no-resolve.
type mihomoLeaf struct {
	typ, val  string
	noResolve bool
}

// top — строка правила верхнего уровня: параметры идут после цели.
func (l mihomoLeaf) top(target string) string {
	s := l.typ + "," + l.val + "," + target
	if l.noResolve {
		s += ",no-resolve"
	}
	return s
}

// inner — правило внутри логического: параметры сразу за значением.
func (l mihomoLeaf) inner() string {
	s := l.typ + "," + l.val
	if l.noResolve {
		s += ",no-resolve"
	}
	return s
}

// mihomoApplicable сообщает, что правило пишется в профиль Mihomo: оно включено,
// а правила «только Xray» (своё правило Xray, bittorrent) в Mihomo отсутствуют.
func mihomoApplicable(r Rule) bool {
	if !r.Enabled {
		return false
	}
	if r.Raw != nil {
		return r.Raw.Kernel == configlayer.KernelMihomo
	}
	for _, c := range r.Conds {
		if c.Type == CondBittorrent {
			return false
		}
	}
	return true
}

// mihomoTarget переводит цель правила в имя для строки правила: имя
// материализованной группы, DIRECT или REJECT. Группа, которой нет, недоступная
// на версии ядра (D-45) или пустая для Mihomo (D-13), ведёт в REJECT: трафик не
// уходит мимо VPN.
func (b *mihomoBuild) mihomoTarget(t Target) string {
	switch t.Kind {
	case KindDirect:
		return mihomoDirect
	case KindGroup:
		if g := b.byID[t.ID]; g != nil && b.isEmitted(g.ID) {
			return g.Name
		}
	}
	return mihomoReject
}

// mihomoFinalLine — последняя строка rules: «Остальное» (D-17), MATCH с целью.
func (b *mihomoBuild) mihomoFinalLine() string {
	return "MATCH," + b.mihomoTarget(b.rules.Final)
}

// mihomoLines переводит правило модели в строки rules профиля Mihomo (без
// MATCH). Пусто и nil — правило пропускается: выключено, написано для Xray или
// не может сработать (список без записей). Одно условие с несколькими
// значениями — строка на значение; два и более условий — одна строка AND, где
// значения условия объединены через OR (D-15…D-18, D-23).
func (b *mihomoBuild) mihomoLines(r Rule) ([]string, error) {
	if !mihomoApplicable(r) {
		return nil, nil
	}
	target := b.mihomoTarget(r.Target)
	if r.Raw != nil {
		return []string{rawMihomoLine(r.Raw.Text, target)}, nil
	}
	if len(r.Conds) == 0 {
		// Правило без условий поймало бы весь трафик; проверка модели такого не
		// допускает.
		return nil, genErr(genErrMihomoRule, "")
	}
	alts := make([][]mihomoLeaf, 0, len(r.Conds))
	for _, c := range r.Conds {
		a, err := b.condLeaves(c)
		if err != nil {
			return nil, err
		}
		alts = append(alts, a)
	}
	if len(alts) == 1 {
		lines := make([]string, 0, len(alts[0]))
		for _, l := range alts[0] {
			lines = append(lines, l.top(target))
		}
		return lines, nil
	}
	elems := make([]string, 0, len(alts))
	for _, a := range alts {
		if len(a) == 0 {
			// Условие без совпадений: «И» с ним не сработает никогда.
			return nil, nil
		}
		inner := make([]string, 0, len(a))
		for _, l := range a {
			if l.typ == "DOMAIN-REGEX" && strings.ContainsAny(l.val, "()") {
				return nil, genErr(genErrMihomoRegexParens, "")
			}
			inner = append(inner, "("+l.inner()+")")
		}
		if len(inner) == 1 {
			elems = append(elems, inner[0])
			continue
		}
		elems = append(elems, "(OR,"+strings.Join(inner, ",")+")")
	}
	return []string{"AND,(" + strings.Join(elems, ",") + ")," + target}, nil
}

// condLeaves переводит условие в варианты («ИЛИ»): по листу на значение. Пустой
// итог — условие ничему не соответствует (список без записей).
func (b *mihomoBuild) condLeaves(c Cond) ([]mihomoLeaf, error) {
	var out []mihomoLeaf
	switch c.Type {
	case CondDomain:
		for _, v := range c.Values {
			out = append(out, domainLeaf(v))
		}
	case CondGeosite:
		for _, ref := range c.Lists {
			l := b.lists[listKey(ref.File, ref.Tag)]
			if l == nil {
				return nil, genErr(genErrListNotFound, ref.File+":"+ref.Tag)
			}
			for _, part := range []string{listPartDomain, listPartClassical} {
				if name := l.name(part); name != "" {
					out = append(out, mihomoLeaf{typ: "RULE-SET", val: name})
				}
			}
		}
	case CondIP:
		for _, v := range c.Values {
			out = append(out, mihomoLeaf{typ: "IP-CIDR", val: cidrValue(v), noResolve: true})
		}
	case CondGeoip:
		for _, ref := range c.Lists {
			l := b.lists[listKey(ref.File, ref.Tag)]
			if l == nil {
				return nil, genErr(genErrListNotFound, ref.File+":"+ref.Tag)
			}
			if name := l.name(listPartIP); name != "" {
				out = append(out, mihomoLeaf{typ: "RULE-SET", val: name, noResolve: true})
			}
		}
	case CondPort:
		for i := 0; i < len(c.Values); i += mihomoMaxPortRanges {
			end := min(i+mihomoMaxPortRanges, len(c.Values))
			out = append(out, mihomoLeaf{typ: "DST-PORT", val: strings.Join(c.Values[i:end], "/")})
		}
	case CondNetwork:
		for _, v := range c.Values {
			out = append(out, mihomoLeaf{typ: "NETWORK", val: strings.ToUpper(v)})
		}
	case CondSrcIP:
		for _, v := range c.Values {
			out = append(out, mihomoLeaf{typ: "SRC-IP-CIDR", val: cidrValue(v)})
		}
	default:
		return nil, genErr(genErrMihomoRule, c.Type)
	}
	return out, nil
}

// domainLeaf переводит значение условия «домен»: без префикса — домен с
// поддоменами (D-18), full:, keyword: и regexp: — точный домен, подстрока и
// регулярное выражение.
func domainLeaf(v string) mihomoLeaf {
	switch {
	case strings.HasPrefix(v, "full:"):
		return mihomoLeaf{typ: "DOMAIN", val: strings.TrimPrefix(v, "full:")}
	case strings.HasPrefix(v, "keyword:"):
		return mihomoLeaf{typ: "DOMAIN-KEYWORD", val: strings.TrimPrefix(v, "keyword:")}
	case strings.HasPrefix(v, "regexp:"):
		return mihomoLeaf{typ: "DOMAIN-REGEX", val: strings.TrimPrefix(v, "regexp:")}
	}
	return mihomoLeaf{typ: "DOMAIN-SUFFIX", val: v}
}

// cidrValue дополняет одиночный адрес маской (/32 или /128): правила IP-CIDR
// Mihomo принимают только сеть.
func cidrValue(v string) string {
	if strings.Contains(v, "/") {
		return v
	}
	if a, err := netip.ParseAddr(v); err == nil {
		return v + "/" + strconv.Itoa(a.BitLen())
	}
	return v
}

// logicTypes — логические правила Mihomo: цель идёт после всей скобочной части.
var logicTypes = map[string]bool{"AND": true, "OR": true, "NOT": true}

// rawMihomoLine приписывает цель к своему правилу Mihomo. Параметры простого
// правила (no-resolve, src) в Mihomo идут после цели, поэтому они переносятся.
func rawMihomoLine(text, target string) string {
	parts := strings.Split(text, ",")
	if logicTypes[strings.ToUpper(strings.TrimSpace(parts[0]))] {
		return text + "," + target
	}
	end := len(parts)
	for end > 2 {
		p := strings.ToLower(strings.TrimSpace(parts[end-1]))
		if p != "no-resolve" && p != "src" {
			break
		}
		end--
	}
	line := strings.Join(parts[:end], ",") + "," + target
	if end < len(parts) {
		line += "," + strings.Join(parts[end:], ",")
	}
	return line
}
