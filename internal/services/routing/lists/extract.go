package lists

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// Пределы размера списка для Mihomo (D-19).
const (
	// WarnEntries — больше записей: список помечается «тяжёлым».
	WarnEntries = 50000
	// MaxEntries — больше уникальных записей: выгрузка отклоняется.
	MaxEntries = 300000
	// MaxBytes — больше текста: выгрузка отклоняется.
	MaxBytes = 10 << 20
)

// ErrListTooLarge — категория больше предела (MaxEntries записей или MaxBytes текста).
var ErrListTooLarge = errors.New("list_too_large")

// Типы Domain в dat.
const (
	domainPlain   = 0
	domainRegex   = 1
	domainRoot    = 2
	domainFull    = 3
	prefixKeyword = "DOMAIN-KEYWORD,"
	prefixRegex   = "DOMAIN-REGEX,"
)

// Category — выгрузка одной категории в строки текстового провайдера Mihomo.
// Срезы общие с кэшем индекса: менять их нельзя.
//
//   - Domains — для провайдера behavior: domain («+.x» для RootDomain, «x» для Full);
//   - Classical — для behavior: classical («DOMAIN-KEYWORD,x», «DOMAIN-REGEX,x»);
//   - CIDRs — для behavior: ipcidr.
//
// Entries — число строк во всех трёх списках после удаления дублей и невалидных
// значений; именно оно ожидается в ruleCount провайдера.
type Category struct {
	Kind      string
	Domains   []string
	Classical []string
	CIDRs     []string
	Entries   int
	Hash      string
	Heavy     bool

	bytes int
}

// Extract выгружает категорию потоково: читается и разбирается только тело записи тега.
// Результат мемоизирован по (имя, размер, mtime, тег).
func (ix *Index) Extract(file, tag string) (Category, error) {
	real, fi, err := ix.resolve(file)
	if err != nil {
		return Category{}, err
	}
	st := stampOf(fi)
	key := fmt.Sprintf("%s\x00%d\x00%d\x00%s", real, st.size, st.mod, strings.ToUpper(tag))
	if c, ok := ix.cachedCategory(key); ok {
		return c, nil
	}
	kind := ix.kindOf(real, fi.Size(), fi.ModTime())

	ix.scanMu.Lock()
	defer ix.scanMu.Unlock()
	if c, ok := ix.cachedCategory(key); ok {
		return c, nil
	}
	f, err := os.Open(ix.path(real))
	if err != nil {
		return Category{}, fmt.Errorf("%w: %s", ErrListNotFound, file)
	}
	defer f.Close()
	cat, err := extractFrom(f, kind, tag)
	if err != nil {
		if errors.Is(err, ErrListNotFound) {
			return Category{}, fmt.Errorf("%w: %s:%s", ErrListNotFound, real, tag)
		}
		return cat, err
	}
	ix.storeCategory(key, cat)
	return cat, nil
}

func (ix *Index) cachedCategory(key string) (Category, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	c, ok := ix.cats[key]
	return c, ok
}

func (ix *Index) storeCategory(key string, c Category) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if _, ok := ix.cats[key]; ok {
		return
	}
	for len(ix.catOrder) > 0 && (len(ix.catOrder) >= maxCachedCategories || ix.catBytes+c.bytes > maxCacheBytes) {
		old := ix.catOrder[0]
		ix.catOrder = ix.catOrder[1:]
		ix.catBytes -= ix.cats[old].bytes
		delete(ix.cats, old)
	}
	ix.cats[key] = c
	ix.catOrder = append(ix.catOrder, key)
	ix.catBytes += c.bytes
}

// validValue — значение пригодно для строки текстового провайдера: непустое, без
// «/», пробелов и запятых (запись со «/» провайдер домена отбрасывает).
func validValue(v string) bool {
	if v == "" {
		return false
	}
	return !strings.ContainsAny(v, "/, \t\r\n")
}

func extractFrom(r io.Reader, kind, tag string) (Category, error) {
	cat := Category{Kind: kind}
	var (
		domains   = map[string]struct{}{}
		classical = map[string]struct{}{}
		cidrs     = map[string]struct{}{}
		total     int
		bytes     int
		found     bool
	)
	add := func(set map[string]struct{}, v string) error {
		if _, dup := set[v]; dup {
			return nil
		}
		set[v] = struct{}{}
		total++
		bytes += len(v) + 1
		if total > MaxEntries || bytes > MaxBytes {
			return ErrListTooLarge
		}
		return nil
	}
	err := walkEntries(r, func(code string, e *entryBody) error {
		if !strings.EqualFold(code, tag) {
			return nil
		}
		found = true
		for {
			num, typ, err := e.tag()
			if errors.Is(err, io.EOF) {
				return errStopWalk
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
			if kind == KindIP {
				if p, ok := parseCIDR(elem); ok {
					if err := add(cidrs, p.String()); err != nil {
						return err
					}
				}
				continue
			}
			dt, v, ok := parseDomain(elem)
			if !ok || !validValue(v) {
				continue
			}
			switch dt {
			case domainRoot:
				err = add(domains, "+."+v)
			case domainFull:
				err = add(domains, v)
			case domainPlain:
				err = add(classical, prefixKeyword+v)
			case domainRegex:
				err = add(classical, prefixRegex+v)
			}
			if err != nil {
				return err
			}
		}
	})
	if err != nil {
		if errors.Is(err, ErrListTooLarge) {
			cat.Entries = total
			cat.Heavy = true
		}
		return cat, err
	}
	if !found {
		return cat, ErrListNotFound
	}
	cat.Domains = sortedKeys(domains)
	cat.Classical = sortedKeys(classical)
	cat.CIDRs = sortedKeys(cidrs)
	cat.Entries = len(cat.Domains) + len(cat.Classical) + len(cat.CIDRs)
	cat.Heavy = cat.Entries > WarnEntries
	cat.bytes = bytes
	h := sha256.New()
	for _, part := range [][]string{cat.Domains, cat.Classical, cat.CIDRs} {
		_, _ = h.Write([]byte(strings.Join(part, "\n")))
		_, _ = h.Write([]byte{0})
	}
	cat.Hash = hex.EncodeToString(h.Sum(nil))
	return cat, nil
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Examples возвращает до n первых значений категории, не выгружая её целиком.
func (ix *Index) Examples(file, tag string, n int) ([]string, error) {
	real, fi, err := ix.resolve(file)
	if err != nil {
		return nil, err
	}
	out := []string{}
	if n <= 0 {
		return out, nil
	}
	kind := ix.kindOf(real, fi.Size(), fi.ModTime())

	ix.scanMu.Lock()
	defer ix.scanMu.Unlock()
	f, err := os.Open(ix.path(real))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrListNotFound, file)
	}
	defer f.Close()
	found := false
	err = walkEntries(f, func(code string, e *entryBody) error {
		if !strings.EqualFold(code, tag) {
			return nil
		}
		found = true
		for len(out) < n {
			num, typ, err := e.tag()
			if errors.Is(err, io.EOF) {
				break
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
			if s, ok := formatElem(kind, elem); ok {
				out = append(out, s)
			}
		}
		return errStopWalk
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: %s:%s", ErrListNotFound, real, tag)
	}
	return out, nil
}
