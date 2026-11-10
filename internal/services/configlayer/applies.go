package configlayer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Последние применения файла панели для вкладки «Файлы» (D-06): набор копий
// применения хранит прежние версии файлов, которые это применение перезаписало
// или удалило, поэтому запись набора — «версия до применения». Файл адресуется
// только ключом манифеста, набор — своим идентификатором apply-<число>; путь от
// клиента не принимается (T-144-33).

// ErrInvalidApplyID — идентификатор набора копий не в форме apply-<цифры>.
var ErrInvalidApplyID = errors.New("недопустимый идентификатор применения")

// applyIDRE — форма идентификатора набора копий: только префикс и цифры.
var applyIDRE = regexp.MustCompile(`^apply-[0-9]{1,20}$`)

// ApplyCopyView — версия файла до одного применения.
type ApplyCopyView struct {
	// ID — идентификатор набора копий (имя каталога apply-<наносекунды>).
	ID string `json:"id"`
	// At — время применения.
	At time.Time `json:"at"`
	// Reason — почему файл попал в набор: overwrite, delete, orphan, obsolete, flag_off.
	Reason string `json:"reason"`
	// Size — размер копии в байтах.
	Size int64 `json:"size"`
}

// FileAppliesView — последние применения, в наборах копий которых есть файл.
type FileAppliesView struct {
	Key     string          `json:"key"`
	Path    string          `json:"path"`
	Applies []ApplyCopyView `json:"applies"`
}

// ApplyContentView — содержимое версии файла до применения (не больше 256 КБ).
type ApplyContentView struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

// backupSetsNewestFirst — имена каталогов apply-* в каталоге копий слоя, новые
// первыми. Каталога нет — пустой список.
func backupSetsNewestFirst(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("configlayer: read backup dir: %w", err)
	}
	var sets []string
	for _, e := range entries {
		if e.IsDir() && applyIDRE.MatchString(e.Name()) {
			sets = append(sets, e.Name())
		}
	}
	// Имя — время в наносекундах: сначала сравниваются числа, потом строки.
	sort.Slice(sets, func(i, j int) bool {
		a, errA := strconv.ParseInt(strings.TrimPrefix(sets[i], backupSetPrefix), 10, 64)
		b, errB := strconv.ParseInt(strings.TrimPrefix(sets[j], backupSetPrefix), 10, 64)
		if errA == nil && errB == nil && a != b {
			return a > b
		}
		return sets[i] > sets[j]
	})
	return sets, nil
}

// fileCopyIn ищет в наборе прежнюю версию файла манифеста entry с ключом key:
// запись с тем же ядром и путём (не копия <файл>.obsolete), файл существовал.
func fileCopyIn(set *BackupSet, key string, entry ManifestEntry) (BackupFileMeta, bool) {
	for _, m := range set.Meta.Files {
		if m.Key == key && m.Kernel == entry.Kernel && m.RelPath == entry.RelPath && m.Existed {
			return m, true
		}
	}
	return BackupFileMeta{}, false
}

// manifestEntry — запись манифеста по ключу и путь файла на диске.
func (l *Layer) manifestEntry(key string) (ManifestEntry, string, error) {
	if !l.Enabled() {
		return ManifestEntry{}, "", ErrDisabled
	}
	entry, ok := l.store.Snapshot().Manifest[key]
	if !ok {
		return ManifestEntry{}, "", ErrUnknownKey
	}
	abs, err := l.opts.Roots.Abs(entry.Kernel, entry.RelPath)
	if err != nil {
		return ManifestEntry{}, "", fmt.Errorf("%s: %w", key, err)
	}
	return entry, abs, nil
}

// backupLayerRoot — каталог наборов копий слоя.
func (l *Layer) backupLayerRoot() string {
	return filepath.Join(l.opts.DataDir, backupDirName, backupLayerName)
}

// FileApplies отдаёт до BackupRetention последних применений, в наборах копий
// которых есть прежняя версия файла с ключом манифеста key, новые первыми.
// Ключ вне манифеста — ErrUnknownKey. Только читает каталог копий.
func (l *Layer) FileApplies(key string) (FileAppliesView, error) {
	entry, abs, err := l.manifestEntry(key)
	if err != nil {
		return FileAppliesView{}, err
	}
	view := FileAppliesView{Key: key, Path: abs, Applies: []ApplyCopyView{}}
	root := l.backupLayerRoot()
	sets, err := backupSetsNewestFirst(root)
	if err != nil {
		return FileAppliesView{}, err
	}
	for _, id := range sets {
		if len(view.Applies) >= BackupRetention {
			break
		}
		set, err := LoadBackupSet(filepath.Join(root, id))
		if err != nil {
			continue // набор без meta.json (прерванная запись) пропускается
		}
		m, ok := fileCopyIn(set, key, entry)
		if !ok {
			continue
		}
		info, err := os.Stat(set.copyPath(m.Kernel, m.RelPath))
		if err != nil {
			continue
		}
		view.Applies = append(view.Applies, ApplyCopyView{
			ID:     id,
			At:     set.Meta.CreatedAt,
			Reason: m.Reason,
			Size:   info.Size(),
		})
	}
	return view, nil
}

// FileApplyContent отдаёт содержимое версии файла с ключом key до применения id.
// id проверяется по форме apply-<цифры> (ErrInvalidApplyID); путь копии строится
// только из каталога набора и записи метаданных этого ключа. Набора нет или
// файла в нём нет — ErrUnknownKey. Содержимое режется до 256 КБ по границе
// символа UTF-8.
func (l *Layer) FileApplyContent(key, id string) (ApplyContentView, error) {
	if !applyIDRE.MatchString(id) {
		return ApplyContentView{}, ErrInvalidApplyID
	}
	entry, _, err := l.manifestEntry(key)
	if err != nil {
		return ApplyContentView{}, err
	}
	set, err := LoadBackupSet(filepath.Join(l.backupLayerRoot(), id))
	if err != nil {
		return ApplyContentView{}, fmt.Errorf("%s: %w", id, ErrUnknownKey)
	}
	m, ok := fileCopyIn(set, key, entry)
	if !ok {
		return ApplyContentView{}, fmt.Errorf("%s: %w", id, ErrUnknownKey)
	}
	data, err := readCapped(set.copyPath(m.Kernel, m.RelPath), diffSideLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return ApplyContentView{}, fmt.Errorf("%s: %w", id, ErrUnknownKey)
	}
	if err != nil {
		return ApplyContentView{}, fmt.Errorf("%s: %w", key, err)
	}
	view := ApplyContentView{ID: id}
	view.Content, view.Truncated = truncateUTF8(data, diffSideLimit)
	return view, nil
}
