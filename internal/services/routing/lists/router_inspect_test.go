//go:build router

package lists

import (
	"context"
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Роутерный сценарий инспектора тегов: настоящие dat стенда, только чтение.
// Ядро не читается — метка ядра не нужна.

// rtBigTag возвращает первый файл с тегом, где записей не меньше min.
func rtBigTag(t *testing.T, ix *Index, files []FileInfo, min int) (FileInfo, TagInfo, bool) {
	t.Helper()
	for _, f := range files {
		tags, err := ix.Tags(f.Name)
		if err != nil {
			t.Fatalf("%s: Tags: %v", f.Name, err)
		}
		for _, tg := range tags {
			if tg.Count >= min {
				return f, tg, true
			}
		}
	}
	return FileInfo{}, TagInfo{}, false
}

func TestRouterListsInspect(t *testing.T) {
	routertest.Current(t)
	ix, files := rtDatIndex(t)

	t.Run("entries", func(t *testing.T) {
		f, tg, ok := rtBigTag(t, ix, files, 150)
		if !ok {
			t.Skip("на стенде нет тега с 150 и более записями")
		}
		first, err := ix.Entries(f.Name, tg.Tag, "", 0, 100)
		if err != nil {
			t.Fatalf("Entries(%s:%s): %v", f.Name, tg.Tag, err)
		}
		if len(first.Entries) != 100 || !first.More || first.Total != tg.Count {
			t.Fatalf("первое окно: записей %d, more=%v, total=%d (в теге %d)",
				len(first.Entries), first.More, first.Total, tg.Count)
		}
		next, err := ix.Entries(f.Name, tg.Tag, "", 100, 100)
		if err != nil {
			t.Fatalf("Entries offset 100: %v", err)
		}
		if len(next.Entries) == 0 || next.Offset != 100 || next.Entries[0] == first.Entries[0] {
			t.Fatalf("второе окно не продолжает первое: %d записей, offset=%d", len(next.Entries), next.Offset)
		}
		if next.More != (100+len(next.Entries) < next.Total) {
			t.Errorf("more=%v при offset 100, записей %d, total %d", next.More, len(next.Entries), next.Total)
		}

		q := first.Entries[0]
		if len(q) > 4 {
			q = q[1 : len(q)-1]
		}
		hit, err := ix.Entries(f.Name, tg.Tag, strings.ToUpper(q), 0, 100)
		if err != nil {
			t.Fatalf("Entries q=%q: %v", q, err)
		}
		if hit.Total == 0 || hit.Total > tg.Count || len(hit.Entries) == 0 {
			t.Fatalf("q=%q: total=%d записей %d (в теге %d)", q, hit.Total, len(hit.Entries), tg.Count)
		}
		for _, e := range hit.Entries {
			if !strings.Contains(strings.ToLower(e), strings.ToLower(q)) {
				t.Errorf("запись %q не содержит q=%q", e, q)
			}
		}

		if _, err := ix.Entries(f.Name, "NO_SUCH_TAG_XCP", "", 0, 10); !errors.Is(err, ErrListNotFound) {
			t.Errorf("несуществующий тег: %v, ожидалась ErrListNotFound", err)
		}
	})

	rtInspectLookup(t, ix, files)
	rtInspectMihomo(t)
}

var rtPlainDomain = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z]{2,}$`)

// rtLookupHit — тест находит запрос в ожидаемом (файл, тег) и пишет время поиска.
func rtLookupHit(t *testing.T, ix *Index, query, file, tag, kind string) {
	t.Helper()
	start := time.Now()
	view, err := ix.Lookup(context.Background(), query, 1000)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", query, err)
	}
	t.Logf("поиск %q по всем dat стенда: %.2f с, совпадений %d", query, time.Since(start).Seconds(), len(view.Matches))
	for _, m := range view.Matches {
		if m.File == file && strings.EqualFold(m.Tag, tag) {
			if m.Kind != kind {
				t.Errorf("совпадение %s:%s: вид %q, ожидался %q", file, tag, m.Kind, kind)
			}
			return
		}
	}
	t.Errorf("Lookup(%q) не нашёл %s:%s среди %d совпадений", query, file, tag, len(view.Matches))
}

func rtInspectLookup(t *testing.T, ix *Index, files []FileInfo) {
	t.Helper()
	t.Run("domain", func(t *testing.T) {
		for _, f := range files {
			if f.Kind != KindSite {
				continue
			}
			tags, err := ix.Tags(f.Name)
			if err != nil {
				t.Fatalf("%s: Tags: %v", f.Name, err)
			}
			for _, tg := range tags {
				page, err := ix.Entries(f.Name, tg.Tag, "", 0, 20)
				if err != nil {
					t.Fatalf("Entries(%s:%s): %v", f.Name, tg.Tag, err)
				}
				for _, v := range page.Entries {
					if rtPlainDomain.MatchString(v) {
						rtLookupHit(t, ix, v, f.Name, tg.Tag, KindSite)
						rtLookupHit(t, ix, "https://"+strings.ToUpper(v)+"/path?x=1", f.Name, tg.Tag, KindSite)
						return
					}
				}
			}
		}
		t.Skip("в geosite-файлах стенда нет записи с обычным доменом")
	})

	t.Run("ip", func(t *testing.T) {
		for _, f := range files {
			if f.Kind != KindIP {
				continue
			}
			tags, err := ix.Tags(f.Name)
			if err != nil {
				t.Fatalf("%s: Tags: %v", f.Name, err)
			}
			for _, tg := range tags {
				for _, ex := range tg.Examples {
					p, err := netip.ParsePrefix(ex)
					if err != nil {
						continue
					}
					rtLookupHit(t, ix, p.Addr().String(), f.Name, tg.Tag, KindIP)
					return
				}
			}
		}
		t.Skip("на стенде нет geoip-файлов с сетями")
	})

	t.Run("bad_query", func(t *testing.T) {
		for _, q := range []string{"", "   ", "a b", strings.Repeat("a", 300)} {
			if _, err := ix.Lookup(context.Background(), q, 0); !errors.Is(err, ErrLookupQuery) {
				t.Errorf("запрос %q: %v, ожидалась ErrLookupQuery", q, err)
			}
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		view, err := ix.Lookup(ctx, "example.com", 0)
		if err != nil {
			t.Fatalf("Lookup с отменённым ctx: %v", err)
		}
		if view.Matches == nil {
			t.Error("Matches должен быть пустым списком, а не nil")
		}
	})
}

func rtInspectMihomo(t *testing.T) {
	t.Helper()
	t.Run("mihomo_files", func(t *testing.T) {
		files, err := MihomoFiles(DefaultMihomoDir)
		if err != nil {
			t.Fatalf("MihomoFiles: %v", err)
		}
		for _, f := range files {
			if f.Name == "" || f.Size < 0 {
				t.Errorf("некорректный элемент: %+v", f)
			}
		}
		none, err := MihomoFiles(t.TempDir() + "/missing")
		if err != nil || len(none) != 0 {
			t.Errorf("нет каталога: %v, %d файлов; ожидался пустой список без ошибки", err, len(none))
		}
	})
}
