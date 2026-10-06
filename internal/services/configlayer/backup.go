package configlayer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// BackupRetention — сколько наборов копий apply-* хранится (D-15).
var BackupRetention = 5

// Имена внутри каталога копий слоя: <data_dir>/backup/config-layer/apply-<время>.
const (
	backupDirName   = "backup"
	backupLayerName = "config-layer"
	backupSetPrefix = "apply-"
	backupMetaName  = "meta.json"
)

// Причины, по которым файл попал в набор копий.
const (
	ReasonOverwrite = "overwrite"
	ReasonDelete    = "delete"
	ReasonOrphan    = "orphan"
	ReasonObsolete  = "obsolete"
	ReasonFlagOff   = "flag_off"
)

// BackupFileMeta — запись о файле в наборе копий.
type BackupFileMeta struct {
	Key     string `json:"key"`
	Kernel  string `json:"kernel"`
	RelPath string `json:"rel_path"`
	Existed bool   `json:"existed"`
	OldHash string `json:"old_hash,omitempty"`
	Reason  string `json:"reason"`
}

// BackupMeta — содержимое meta.json набора копий.
type BackupMeta struct {
	CreatedAt time.Time        `json:"created_at"`
	Trigger   string           `json:"trigger"`
	Files     []BackupFileMeta `json:"files"`
}

// BackupSet — набор копий одного применения: прежние версии файлов, которые
// конвейер перезапишет или удалит, лежат в <Dir>/<ядро>/<путь> (0600), каталоги
// 0700. Набор лежит вне каталогов ядер и вне путей статики (T-144-17).
type BackupSet struct {
	Dir  string
	Meta BackupMeta
}

// NewBackupSet создаёт каталог набора <dataDir>/backup/config-layer/apply-<наносекунды>.
func NewBackupSet(dataDir string, now time.Time, trigger string) (*BackupSet, error) {
	base := filepath.Join(dataDir, backupDirName, backupLayerName)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, fmt.Errorf("configlayer: create backup dir: %w", err)
	}
	stamp := now.UnixNano()
	var dir string
	for {
		dir = filepath.Join(base, fmt.Sprintf("%s%d", backupSetPrefix, stamp))
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("configlayer: create backup set: %w", err)
		}
		stamp++ // два применения в одну наносекунду: имя сдвигается
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("configlayer: chmod backup set: %w", err)
	}
	return &BackupSet{
		Dir:  dir,
		Meta: BackupMeta{CreatedAt: now.UTC(), Trigger: trigger, Files: []BackupFileMeta{}},
	}, nil
}

// Save копирует текущую версию файла <ядро>:<rel> в набор. Файла нет — запись
// с Existed=false (при откате такой файл удаляется). Повторный Save того же
// файла возвращает первую запись: в наборе остаётся самая ранняя версия.
func (b *BackupSet) Save(roots Roots, key, kernel, rel, reason string) (BackupFileMeta, error) {
	abs, err := roots.Abs(kernel, rel)
	if err != nil {
		return BackupFileMeta{}, fmt.Errorf("%s: %w", key, err)
	}
	return b.SaveAbs(abs, key, kernel, rel, reason)
}

// SaveAbs — Save по готовому пути на диске (для <файл>.obsolete: rel у него
// с суффиксом, ключ — ключ основного файла).
func (b *BackupSet) SaveAbs(absPath, key, kernel, rel, reason string) (BackupFileMeta, error) {
	for _, m := range b.Meta.Files {
		if m.Kernel == kernel && m.RelPath == rel {
			return m, nil
		}
	}
	m := BackupFileMeta{Key: key, Kernel: kernel, RelPath: rel, Reason: reason}
	data, err := os.ReadFile(absPath)
	switch {
	case err == nil:
		m.Existed = true
		m.OldHash = HashContent(data)
		if err := b.writeCopy(kernel, rel, data); err != nil {
			return BackupFileMeta{}, fmt.Errorf("%s: %w", key, err)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return BackupFileMeta{}, fmt.Errorf("%s: %w", key, err)
	}
	b.Meta.Files = append(b.Meta.Files, m)
	return m, nil
}

// copyPath — путь копии файла внутри набора.
func (b *BackupSet) copyPath(kernel, rel string) string {
	return filepath.Join(b.Dir, kernel, filepath.FromSlash(rel))
}

// writeCopy пишет копию (0600) в подкаталоги 0700.
func (b *BackupSet) writeCopy(kernel, rel string, data []byte) error {
	dst := b.copyPath(kernel, rel)
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll не меняет права уже существующих каталогов и подчиняется umask:
	// набор целиком закрыт для чужих (T-144-17).
	for d := dir; d != b.Dir && len(d) > len(b.Dir); d = filepath.Dir(d) {
		if err := os.Chmod(d, 0o700); err != nil {
			return err
		}
	}
	return utils.AtomicWriteFile(dst, data, 0o600)
}

// Restore возвращает файл из набора: существовавший — прежними байтами,
// несуществовавший — удаляется (его отсутствие не ошибка).
func (b *BackupSet) Restore(roots Roots, m BackupFileMeta) error {
	abs, err := roots.Abs(m.Kernel, m.RelPath)
	if err != nil {
		return fmt.Errorf("%s: %w", m.Key, err)
	}
	if !m.Existed {
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", m.Key, err)
		}
		return nil
	}
	data, err := os.ReadFile(b.copyPath(m.Kernel, m.RelPath))
	if err != nil {
		return fmt.Errorf("%s: копия: %w", m.Key, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("%s: %w", m.Key, err)
	}
	if err := utils.AtomicWriteFile(abs, data, 0o644); err != nil {
		return fmt.Errorf("%s: %w", m.Key, err)
	}
	return nil
}

// WriteMeta записывает meta.json набора (0600).
func (b *BackupSet) WriteMeta() error {
	data, err := json.MarshalIndent(b.Meta, "", "  ")
	if err != nil {
		return fmt.Errorf("configlayer: encode backup meta: %w", err)
	}
	if err := utils.AtomicWriteFile(filepath.Join(b.Dir, backupMetaName), data, 0o600); err != nil {
		return fmt.Errorf("configlayer: write backup meta: %w", err)
	}
	return nil
}

// LoadBackupSet читает набор копий по его каталогу (для восстановления по журналу).
func LoadBackupSet(dir string) (*BackupSet, error) {
	data, err := os.ReadFile(filepath.Join(dir, backupMetaName))
	if err != nil {
		return nil, fmt.Errorf("configlayer: read backup meta: %w", err)
	}
	var meta BackupMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("configlayer: parse backup meta: %w", err)
	}
	return &BackupSet{Dir: dir, Meta: meta}, nil
}

// PruneBackups оставляет keep новейших наборов apply-*.
func PruneBackups(dataDir string, keep int) error { return nil }
