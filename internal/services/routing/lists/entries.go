package lists

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// entriesMaxLimit — сколько записей инспектор отдаёт за один запрос.
const entriesMaxLimit = 100

// EntriesPage — окно записей одного тега для инспектора.
//
//   - Total — число записей тега после фильтра q (не размер окна);
//   - Offset — смещение окна;
//   - More — за окном есть ещё записи.
type EntriesPage struct {
	Tag     string   `json:"tag"`
	Total   int      `json:"total"`
	Offset  int      `json:"offset"`
	Entries []string `json:"entries"`
	More    bool     `json:"more"`
}

// Entries читает записи тега постранично: файл идёт потоковым проходом, в память
// попадает только окно [offset, offset+limit). limit 1..100 (0 или больше — 100).
// q — подстрока без учёта регистра (пусто — без фильтра). Имя файла — любое из пары
// симлинков, тег сравнивается без учёта регистра. Нет файла или тега — ErrListNotFound.
// Каталог dat только читается: файл закрывается по выходе из вызова.
func (ix *Index) Entries(file, tag, q string, offset, limit int) (EntriesPage, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > entriesMaxLimit {
		limit = entriesMaxLimit
	}
	needle := strings.ToLower(strings.TrimSpace(q))

	real, fi, err := ix.resolve(file)
	if err != nil {
		return EntriesPage{}, err
	}
	kind := ix.kindOf(real, fi.Size(), fi.ModTime())

	ix.scanMu.Lock()
	defer ix.scanMu.Unlock()
	f, err := os.Open(ix.path(real))
	if err != nil {
		return EntriesPage{}, fmt.Errorf("%w: %s", ErrListNotFound, file)
	}
	defer f.Close()

	page := EntriesPage{Offset: offset, Entries: []string{}}
	found := false
	err = walkEntries(f, func(code string, e *entryBody) error {
		if !strings.EqualFold(code, tag) {
			return nil
		}
		found = true
		page.Tag = code
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
			v, ok := formatElem(kind, elem)
			if !ok {
				continue
			}
			if needle != "" && !strings.Contains(strings.ToLower(v), needle) {
				continue
			}
			if page.Total >= offset && len(page.Entries) < limit {
				page.Entries = append(page.Entries, v)
			}
			page.Total++
		}
	})
	if err != nil {
		return EntriesPage{}, fmt.Errorf("%w: %s: %v", ErrCorruptDat, real, err)
	}
	if !found {
		return EntriesPage{}, fmt.Errorf("%w: %s:%s", ErrListNotFound, real, tag)
	}
	page.More = offset+len(page.Entries) < page.Total
	return page, nil
}
