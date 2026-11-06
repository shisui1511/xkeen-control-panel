package routing

import (
	"context"
	"regexp"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// dnsDirectLookupLimit — предел совпадений поиска по спискам в DNSDirectMatch
// (T-148-57). Проход по файлам идёт целиком при любом пределе, предел ограничивает
// только память под совпадения: имя редко входит больше чем в десяток тегов.
const dnsDirectLookupLimit = 64

// DirectDNSRules возвращает домены и списки geosite явных правил с целью direct
// (D-21): такие имена резолвятся тем же путём, что их трафик, — напрямую. В
// выборку идут включённые правила без своего правила ядра (Raw), у которых все
// условия — домены или geosite; финальное правило не входит, даже если его цель
// direct, — остальное идёт через группу-цель. Домены — значения условий как в
// модели (без префикса — домен с поддоменами; full:, keyword:, regexp: остаются),
// без повторов в порядке правил; списки — без повторов. Нет секции правил —
// пустые списки без ошибки. Это единый источник для генераторов DNS обоих ядер и
// для пути резолвера прошивки в «Проверить резолв».
func DirectDNSRules(sections configlayer.Sections) (domains []string, refs []lists.ListRef, err error) {
	rules, err := parseRules(sections[RulesSection])
	if err != nil {
		return nil, nil, err
	}
	domains = []string{}
	refs = []lists.ListRef{}
	seenDomain := map[string]bool{}
	seenRef := map[lists.ListRef]bool{}
	for _, r := range rules.Items {
		if !directDNSRule(r) {
			continue
		}
		for _, c := range r.Conds {
			switch c.Type {
			case CondDomain:
				for _, v := range c.Values {
					v = strings.TrimSpace(v)
					if v == "" || seenDomain[v] {
						continue
					}
					seenDomain[v] = true
					domains = append(domains, v)
				}
			case CondGeosite:
				for _, l := range c.Lists {
					if l.Kind == "" {
						l.Kind = lists.KindSite
					}
					if seenRef[l] {
						continue
					}
					seenRef[l] = true
					refs = append(refs, l)
				}
			}
		}
	}
	return domains, refs, nil
}

// directDNSRule сообщает, что правило попадает в выборку D-21: включено, цель
// direct, без Raw, есть условия и все они — домены или geosite.
func directDNSRule(r Rule) bool {
	if !r.Enabled || r.Raw != nil || r.Target.Kind != KindDirect || len(r.Conds) == 0 {
		return false
	}
	for _, c := range r.Conds {
		if c.Type != CondDomain && c.Type != CondGeosite {
			return false
		}
	}
	return true
}

// DNSDirectMatch сообщает, что имя резолвится напрямую по D-21: оно совпадает с
// доменом direct-правила применённой конфигурации или входит в список такого
// правила. Домен без префикса ловит само имя и поддомены (как в правилах Xray),
// full: — только точное имя, keyword: — подстроку, regexp: — регулярное
// выражение. Списки сверяются поиском по индексу dat с малым пределом;
// индекса нет или поиск не удался — совпадение только по доменам. Поиск
// ограничен контекстом вызывающего (у «Проверить резолв» свой таймаут).
func (s *Service) DNSDirectMatch(ctx context.Context, name string) bool {
	target := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if target == "" {
		return false
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return false
	}
	domains, refs, err := DirectDNSRules(st.Applied)
	if err != nil {
		return false
	}
	for _, d := range domains {
		if dnsDomainMatches(d, target) {
			return true
		}
	}
	if len(refs) == 0 || s.d.Lists == nil {
		return false
	}
	view, err := s.d.Lists.Lookup(ctx, target, dnsDirectLookupLimit)
	if err != nil {
		return false
	}
	for _, m := range view.Matches {
		if m.Kind != lists.KindSite {
			continue
		}
		for _, r := range refs {
			if r.File == m.File && strings.EqualFold(r.Tag, m.Tag) {
				return true
			}
		}
	}
	return false
}

// dnsDomainMatches — совпадение имени (нижний регистр, без точки в конце) со
// значением условия «домен» по синтаксису ядра; невалидная регулярка не
// совпадает ни с чем.
func dnsDomainMatches(value, target string) bool {
	v := strings.TrimSpace(value)
	low := strings.ToLower(v)
	switch {
	case strings.HasPrefix(low, "full:"):
		return target == strings.TrimSuffix(low[len("full:"):], ".")
	case strings.HasPrefix(low, "keyword:"):
		word := low[len("keyword:"):]
		return word != "" && strings.Contains(target, word)
	case strings.HasPrefix(low, "regexp:"):
		rx, err := regexp.Compile(v[len("regexp:"):])
		return err == nil && rx.MatchString(target)
	}
	d := strings.TrimSuffix(low, ".")
	return d != "" && (target == d || strings.HasSuffix(target, "."+d))
}
