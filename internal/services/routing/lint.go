package routing

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// Предупреждения проверки качества правил (D-40). «Применить» они не блокируют:
// их видит только представление правил.
const (
	// warnExactDup — правило повторяет правило выше: те же условия и цель.
	warnExactDup = "exact_dup"
	// warnOverlap — все значения правила уже перехвачены выше другой целью.
	warnOverlap = "overlap"
	// warnBelowCatchall — выше стоит правило «всё подряд»: до этого правила трафик
	// не дойдёт.
	warnBelowCatchall = "below_catchall"
)

// Значения «всё подряд» в условиях.
const (
	catchAllV4 = "0.0.0.0/0"
	catchAllV6 = "::/0"
)

// lintHit — правило, которое первым перехватило значение: номер (с 1) и цель.
type lintHit struct {
	n      int
	target Target
}

// lintKeyword — подстрока из условия «keyword:» вышестоящего правила.
type lintKeyword struct {
	word string
	hit  lintHit
}

// lintIndex — значения вышестоящих правил из одного условия: для каждого
// значения помнится первое (верхнее) правило. Поиск покрытия значения идёт по
// картам, без перебора всех пар правил.
type lintIndex struct {
	plain    map[string]lintHit
	full     map[string]lintHit
	regexps  map[string]lintHit
	keywords []lintKeyword
	prefixes map[netip.Prefix]lintHit
	lists    map[string]lintHit
}

func newLintIndex() *lintIndex {
	return &lintIndex{
		plain:    map[string]lintHit{},
		full:     map[string]lintHit{},
		regexps:  map[string]lintHit{},
		prefixes: map[netip.Prefix]lintHit{},
		lists:    map[string]lintHit{},
	}
}

// putFirst записывает значение, если его ещё не перехватило правило выше.
func putFirst[K comparable](m map[K]lintHit, k K, h lintHit) {
	if _, ok := m[k]; !ok {
		m[k] = h
	}
}

// lintListKey — ключ списка dat: тип условия, файл и тег.
func lintListKey(c Cond, file, tag string) string {
	return c.Type + "|" + file + "|" + strings.ToLower(tag)
}

// lintPrefix разбирает адрес или сеть в префикс.
func lintPrefix(v string) (netip.Prefix, bool) {
	norm, ok := normIPValue(v)
	if !ok {
		return netip.Prefix{}, false
	}
	if p, err := netip.ParsePrefix(norm); err == nil {
		return p, true
	}
	a, err := netip.ParseAddr(norm)
	if err != nil {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(a, a.BitLen()), true
}

// add заносит значения условия правила с номером n и целью target.
func (x *lintIndex) add(c Cond, h lintHit) {
	switch c.Type {
	case CondDomain:
		for _, v := range c.Values {
			low := strings.ToLower(strings.TrimSpace(v))
			switch {
			case strings.HasPrefix(low, "full:"):
				putFirst(x.full, low[len("full:"):], h)
			case strings.HasPrefix(low, "keyword:"):
				x.keywords = append(x.keywords, lintKeyword{word: low[len("keyword:"):], hit: h})
			case strings.HasPrefix(low, "regexp:"):
				putFirst(x.regexps, strings.TrimSpace(v)[len("regexp:"):], h)
			default:
				putFirst(x.plain, low, h)
			}
		}
	case CondIP:
		for _, v := range c.Values {
			if p, ok := lintPrefix(v); ok {
				putFirst(x.prefixes, p, h)
			}
		}
	case CondGeosite, CondGeoip:
		for _, l := range c.Lists {
			putFirst(x.lists, lintListKey(c, l.File, l.Tag), h)
		}
	}
}

// nearer выбирает верхнее из двух правил (меньший номер).
func nearer(a, b lintHit, haveA bool) (lintHit, bool) {
	if !haveA || b.n < a.n {
		return b, true
	}
	return a, haveA
}

// coverDomain ищет верхнее правило, которое перехватывает значение условия
// «домен»: домен без префикса покрывает себя и поддомены, full: — только
// точное совпадение, keyword: — все домены с подстрокой.
func (x *lintIndex) coverDomain(v string) (lintHit, bool) {
	low := strings.ToLower(strings.TrimSpace(v))
	var (
		best lintHit
		have bool
	)
	switch {
	case strings.HasPrefix(low, "regexp:"):
		if h, ok := x.regexps[strings.TrimSpace(v)[len("regexp:"):]]; ok {
			best, have = nearer(best, h, have)
		}
		return best, have
	case strings.HasPrefix(low, "keyword:"):
		word := low[len("keyword:"):]
		for _, k := range x.keywords {
			if strings.Contains(word, k.word) {
				best, have = nearer(best, k.hit, have)
			}
		}
		return best, have
	}
	host, isFull := low, false
	if rest, ok := strings.CutPrefix(low, "full:"); ok {
		host, isFull = rest, true
	}
	if isFull {
		if h, ok := x.full[host]; ok {
			best, have = nearer(best, h, have)
		}
	}
	for d := host; d != ""; {
		if h, ok := x.plain[d]; ok {
			best, have = nearer(best, h, have)
		}
		_, rest, found := strings.Cut(d, ".")
		if !found {
			break
		}
		d = rest
	}
	for _, k := range x.keywords {
		if strings.Contains(host, k.word) {
			best, have = nearer(best, k.hit, have)
		}
	}
	return best, have
}

// coverIP ищет верхнее правило, чья сеть включает значение.
func (x *lintIndex) coverIP(v string) (lintHit, bool) {
	p, ok := lintPrefix(v)
	if !ok {
		return lintHit{}, false
	}
	var (
		best lintHit
		have bool
	)
	for bits := p.Bits(); bits >= 0; bits-- {
		wider, err := p.Addr().Prefix(bits)
		if err != nil {
			continue
		}
		if h, ok := x.prefixes[wider]; ok {
			best, have = nearer(best, h, have)
		}
	}
	return best, have
}

// overlap сообщает, что все значения условия перехвачены выше правилами с другой
// целью, и возвращает номер ближайшего из перехвативших.
func (x *lintIndex) overlap(c Cond, target Target) (int, bool) {
	nearest, count := 0, 0
	hit := func(h lintHit, ok bool) bool {
		if !ok || h.target == target {
			return false
		}
		count++
		if h.n > nearest {
			nearest = h.n
		}
		return true
	}
	switch c.Type {
	case CondDomain:
		for _, v := range c.Values {
			h, ok := x.coverDomain(v)
			if !hit(h, ok) {
				return 0, false
			}
		}
	case CondIP:
		for _, v := range c.Values {
			h, ok := x.coverIP(v)
			if !hit(h, ok) {
				return 0, false
			}
		}
	case CondGeosite, CondGeoip:
		for _, l := range c.Lists {
			h, ok := x.lists[lintListKey(c, l.File, l.Tag)]
			if !hit(h, ok) {
				return 0, false
			}
		}
	default:
		return 0, false
	}
	return nearest, count > 0
}

// lintCatchAll сообщает, что правило ловит весь трафик: все условия — сеть с tcp
// и udp либо ip 0.0.0.0/0 и ::/0. Правило с нестандартными условиями и своё
// правило ядра сюда не относятся.
func lintCatchAll(r Rule) bool {
	if r.Raw != nil || len(r.Conds) == 0 {
		return false
	}
	for _, c := range r.Conds {
		set := map[string]bool{}
		for _, v := range c.Values {
			if c.Type == CondIP {
				if norm, ok := normIPValue(v); ok {
					v = norm
				}
			} else {
				v = strings.ToLower(strings.TrimSpace(v))
			}
			set[v] = true
		}
		switch c.Type {
		case CondNetwork:
			if !set["tcp"] || !set["udp"] {
				return false
			}
		case CondIP:
			if !set[catchAllV4] || !set[catchAllV6] {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// lintKey — нормализованные условия и цель правила для сравнения на точный
// дубль: порядок условий и значений не важен.
func lintKey(r Rule) string {
	parts := make([]string, 0, len(r.Conds))
	for _, c := range r.Conds {
		vals := make([]string, 0, len(c.Values)+len(c.Lists))
		for _, v := range c.Values {
			if c.Type == CondIP || c.Type == CondSrcIP {
				if norm, ok := normIPValue(v); ok {
					v = norm
				}
			} else {
				v = strings.ToLower(strings.TrimSpace(v))
			}
			vals = append(vals, v)
		}
		for _, l := range c.Lists {
			vals = append(vals, "list:"+l.File+":"+strings.ToLower(l.Tag))
		}
		sort.Strings(vals)
		parts = append(parts, c.Type+"="+strings.Join(vals, ","))
	}
	sort.Strings(parts)
	return strings.Join(parts, "&") + "->" + r.Target.Kind + ":" + r.Target.ID
}

// lintWarning собирает предупреждение с номером перекрывающего правила.
func lintWarning(code string, n int, ruleID string) IssueView {
	return IssueView{Code: code, Params: map[string]string{"n": strconv.Itoa(n)}, Key: "rule:" + ruleID}
}

// lintRules проверяет качество правил (D-40) по включённым правилам сверху вниз,
// не раскрывая содержимое geosite и geoip. Результат — предупреждения по ID
// правила; номера считаются от начала списка, как в представлении. Правила из
// нескольких условий и «свои правила ядра» в сравнении на перекрытие не
// участвуют.
//
//   - below_catchall {n}: выше стоит правило «всё подряд» под номером n;
//   - exact_dup {n}: условия и цель совпадают с правилом n выше;
//   - overlap {n}: все значения правила уже перехвачены выше другой целью, ближайшее
//     из перехвативших правил — n.
func lintRules(items []Rule) map[string][]IssueView {
	out := map[string][]IssueView{}
	catchAll := 0
	seen := map[string]int{}
	index := map[string]*lintIndex{
		CondDomain: newLintIndex(), CondIP: newLintIndex(), CondGeosite: newLintIndex(), CondGeoip: newLintIndex(),
	}
	for i, r := range items {
		if !r.Enabled {
			continue
		}
		n := i + 1
		if catchAll > 0 {
			out[r.ID] = append(out[r.ID], lintWarning(warnBelowCatchall, catchAll, r.ID))
			continue
		}
		if r.Raw != nil || len(r.Conds) == 0 {
			continue
		}
		key := lintKey(r)
		if first, dup := seen[key]; dup {
			out[r.ID] = append(out[r.ID], lintWarning(warnExactDup, first, r.ID))
		} else {
			seen[key] = n
			if len(r.Conds) == 1 {
				if x := index[r.Conds[0].Type]; x != nil {
					if over, ok := x.overlap(r.Conds[0], r.Target); ok {
						out[r.ID] = append(out[r.ID], lintWarning(warnOverlap, over, r.ID))
					}
				}
			}
		}
		if len(r.Conds) == 1 {
			if x := index[r.Conds[0].Type]; x != nil {
				x.add(r.Conds[0], lintHit{n: n, target: r.Target})
			}
		}
		if lintCatchAll(r) {
			catchAll = n
		}
	}
	return out
}
