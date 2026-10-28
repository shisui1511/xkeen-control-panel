//go:build router

package lists

import (
	"errors"
	"strings"
	"testing"

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
}
