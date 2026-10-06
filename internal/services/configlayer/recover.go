package configlayer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Возраст, после которого хвосты временных файлов считаются брошенными (A4).
const (
	staleTmpAge    = time.Hour
	staleAtomicAge = 24 * time.Hour
)

// RecoverJournal откатывает прерванную запись (обрыв питания посреди
// применения): если в состоянии остался журнал, файлы возвращаются из его
// набора копий, журнал очищается, выставляется уведомление
// recovered_from_journal. Если набор копий не прочитался, файлы вернуть не из
// чего: уведомление journal_recovery_failed. Возвращает true, если откат выполнялся.
//
// Журнал очищается всегда, даже если набор копий не прочитался или часть
// файлов вернуть не удалось: иначе каждый старт повторял бы откат поверх более
// поздних правок пользователя. Набор остаётся на диске для разбора, ошибка
// возвращается для лога. Вызывается при старте слоя до первой сверки дрейфа.
func RecoverJournal(store *Store, roots Roots) (bool, error) {
	j := store.Snapshot().Journal
	if j == nil {
		return false, nil
	}
	dropJournal := func(recovered bool) error {
		return store.Update(func(st *State) error {
			st.Journal = nil
			if recovered {
				st.Notices.RecoveredFromJournal = true
			} else {
				st.Notices.JournalRecoveryFailed = true
			}
			return nil
		})
	}

	set, err := LoadBackupSet(j.BackupDir)
	if err != nil {
		if dropErr := dropJournal(false); dropErr != nil {
			return false, errors.Join(err, dropErr)
		}
		return false, fmt.Errorf("configlayer: recover journal: %w", err)
	}
	var errs []error
	for i := len(set.Meta.Files) - 1; i >= 0; i-- {
		if err := set.Restore(roots, set.Meta.Files[i]); err != nil {
			errs = append(errs, err)
		}
	}
	if err := dropJournal(true); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return true, fmt.Errorf("configlayer: recover journal: %w", errors.Join(errs...))
	}
	return true, nil
}

// CleanupStale убирает хвосты оборванных применений: каталоги apply-* в
// <dataDir>/tmp старше часа и файлы atomic-* (временные файлы атомарной записи)
// старше суток в корне Xray и каталогах MihomoPanelDirs. Корень Mihomo и любые
// другие файлы не трогаются (A4).
func CleanupStale(dataDir string, roots Roots, now time.Time) error {
	var errs []error
	keep := func(err error) {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	tmpBase := filepath.Join(dataDir, "tmp")
	entries, err := os.ReadDir(tmpBase)
	keep(err)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "apply-") {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > staleTmpAge {
			keep(os.RemoveAll(filepath.Join(tmpBase, e.Name())))
		}
	}

	dirs := make([]string, 0, 1+len(MihomoPanelDirs))
	if roots.Xray != "" {
		dirs = append(dirs, roots.Xray)
	}
	if roots.Mihomo != "" {
		for _, d := range MihomoPanelDirs {
			dirs = append(dirs, filepath.Join(roots.Mihomo, d))
		}
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		keep(err)
		for _, e := range entries {
			if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), "atomic-") {
				continue
			}
			if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > staleAtomicAge {
				keep(os.Remove(filepath.Join(dir, e.Name())))
			}
		}
	}
	return errors.Join(errs...)
}
