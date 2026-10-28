package lists

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrLookupQuery — запрос поиска пуст или не похож на домен либо IP (код lookup_query).
var ErrLookupQuery = errors.New("lookup_query")

const (
	lookupDefaultLimit = 50
	lookupMaxQueryLen  = 253
)

// LookupMatch — тег, в который попал запрос: файл каталога, тег и вид файла
// (KindSite или KindIP).
type LookupMatch struct {
	File string `json:"file"`
	Tag  string `json:"tag"`
	Kind string `json:"kind"`
}

// LookupView — результат поиска «куда относится». More — сколько совпадений
// не поместилось в limit.
type LookupView struct {
	Query   string        `json:"query"`
	Matches []LookupMatch `json:"matches"`
	More    int           `json:"more"`
}

// normalizeLookupQuery приводит запрос к домену (нижний регистр) либо IP: схема,
// путь, порт и квадратные скобки отбрасываются. Пусто или длиннее 253 — ErrLookupQuery.
func normalizeLookupQuery(v string) (string, netip.Addr, error) {
	s := strings.TrimSpace(v)
	if s == "" || len(s) > lookupMaxQueryLen*4 {
		return "", netip.Addr{}, ErrLookupQuery
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = u.Host
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	s = strings.ToLower(s)
	if s == "" || len(s) > lookupMaxQueryLen || strings.ContainsAny(s, " \t\r\n\x00") {
		return "", netip.Addr{}, ErrLookupQuery
	}
	if a, err := netip.ParseAddr(s); err == nil {
		a = a.WithZone("").Unmap()
		return a.String(), a, nil
	}
	return s, netip.Addr{}, nil
}

// domainMatches — совпадение домена запроса с записью geosite по её типу
// (как в ядре Xray): Plain — подстрока, Regex — регулярное выражение, Root — сам
// домен или поддомен, Full — точное равенство. re — кэш скомпилированных
// регулярок одного поиска; nil в кэше — невалидная, она пропускается.
func domainMatches(typ uint64, value, target string, re map[string]*regexp.Regexp) bool {
	value = strings.TrimSpace(value)
	if value == "" || target == "" {
		return false
	}
	switch typ {
	case domainFull:
		return target == strings.ToLower(value)
	case domainPlain:
		return strings.Contains(target, strings.ToLower(value))
	case domainRegex:
		rx, cached := re[value]
		if !cached {
			c, err := regexp.Compile(value)
			if err == nil {
				rx = c
			}
			re[value] = rx
		}
		return rx != nil && rx.MatchString(target)
	case domainRoot:
		v := strings.ToLower(value)
		return target == v || strings.HasSuffix(target, "."+v)
	}
	return false
}

// Lookup ищет домен или IP по всем dat каталога: geosite-файлы сверяются по типу
// записи, geoip-файлы — по вхождению адреса в сеть. Домен проверяется только в
// geosite, IP — только в geoip (имена не резолвятся). Файлы только читаются потоком.
// Отмена ctx прекращает проход, возвращается уже найденное. limit 0 — 50.
func (ix *Index) Lookup(ctx context.Context, query string, limit int) (LookupView, error) {
	target, addr, err := normalizeLookupQuery(query)
	if err != nil {
		return LookupView{}, err
	}
	if limit <= 0 {
		limit = lookupDefaultLimit
	}
	files, err := ix.Files()
	if err != nil {
		return LookupView{}, err
	}
	view := LookupView{Query: target, Matches: []LookupMatch{}}
	isIP := addr.IsValid()
	reCache := map[string]*regexp.Regexp{}

	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		if (f.Kind == KindIP) != isIP {
			continue
		}
		hits, err := ix.lookupFile(ctx, f, target, addr, reCache)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			// Повреждённый или пропавший файл не обрывает поиск по остальным.
			continue
		}
		for _, tag := range hits {
			if len(view.Matches) < limit {
				view.Matches = append(view.Matches, LookupMatch{File: f.Name, Tag: tag, Kind: f.Kind})
			} else {
				view.More++
			}
		}
	}
	return view, nil
}

// lookupFile — один потоковый проход по файлу; возвращает теги с совпадением
// (каждый тег один раз). При отмене ctx возвращает найденное до отмены.
func (ix *Index) lookupFile(ctx context.Context, f FileInfo, target string, addr netip.Addr, reCache map[string]*regexp.Regexp) ([]string, error) {
	ix.scanMu.Lock()
	defer ix.scanMu.Unlock()
	fh, err := os.Open(ix.path(f.Name))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrListNotFound, f.Name)
	}
	defer fh.Close()

	var tags []string
	seen := map[string]struct{}{}
	err = walkEntries(fh, func(code string, e *entryBody) error {
		if ctx.Err() != nil {
			return errStopWalk
		}
		key := strings.ToUpper(code)
		if _, dup := seen[key]; dup {
			return nil
		}
		for {
			num, typ, err := e.tag()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if num != 2 || typ != protowire.BytesType {
				if err := e.skip(typ); err != nil {
					return err
				}
				continue
			}
			elem, err := e.bytes(maxElemBytes)
			if err != nil {
				return err
			}
			if elemMatches(f.Kind, elem, target, addr, reCache) {
				seen[key] = struct{}{}
				tags = append(tags, code)
				return nil
			}
		}
	})
	if err != nil {
		return tags, fmt.Errorf("%w: %s: %v", ErrCorruptDat, f.Name, err)
	}
	return tags, nil
}

func elemMatches(kind string, elem []byte, target string, addr netip.Addr, reCache map[string]*regexp.Regexp) bool {
	if kind == KindIP {
		p, ok := parseCIDR(elem)
		if !ok || !addr.IsValid() {
			return false
		}
		return p.Contains(addr)
	}
	typ, v, ok := parseDomain(elem)
	return ok && domainMatches(typ, v, target, reCache)
}
