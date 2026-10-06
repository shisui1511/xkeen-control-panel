package configlayer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// writeOutcome — итог записи плана. При ошибке откат уже выполнен: Set остаётся
// на диске для разбора, RollbackErr непуст, только если вернуть файлы не удалось
// (журнал тогда сохраняется, и RecoverJournal повторит откат при старте).
type writeOutcome struct {
	Set            *BackupSet
	Written        int
	OrphansRemoved []string
	RollbackErr    error
}

// writePlan безопасно пишет план: 1) набор копий и копии прежних версий всех
// перезаписываемых и удаляемых файлов; 2) журнал в файле состояния до первой
// записи; 3) записи и удаления; 4) при сбое — откат из набора копий и очистка
// журнала. При успехе журнал остаётся: его очищает коммит состояния в той же
// записи файла состояния, что и манифест.
func (p *Pipeline) writePlan(plan Plan, trigger Trigger) (writeOutcome, error) {
	if err := p.checkSymlinks(plan); err != nil {
		return writeOutcome{}, err
	}
	set, err := NewBackupSet(p.d.DataDir, p.d.Now(), string(trigger))
	if err != nil {
		return writeOutcome{}, err
	}
	out := writeOutcome{Set: set}

	// Прежние версии — до любой записи в рабочие каталоги.
	var journal []JournalFile
	for _, fp := range plan.Files {
		reason := ReasonOverwrite
		switch fp.Action {
		case ActionWrite:
		case ActionDelete:
			reason = ReasonDelete
		default:
			continue
		}
		m, err := set.Save(p.d.Roots, fp.Key, fp.Kernel, fp.RelPath, reason)
		if err != nil {
			_ = os.RemoveAll(set.Dir)
			return out, err
		}
		journal = append(journal, JournalFile{Key: fp.Key, Existed: m.Existed, NewHash: fp.NewHash})
		if fp.Action == ActionWrite && fp.RemoveObsolete {
			// Метка .obsolete уйдёт после записи: её байты — в набор до первой записи.
			obs := fp.RelPath + obsoleteSuffix
			if _, err := set.SaveAbs(fp.AbsPath+obsoleteSuffix, fp.Key, fp.Kernel, obs, ReasonObsolete); err != nil {
				_ = os.RemoveAll(set.Dir)
				return out, err
			}
		}
	}
	// Сироты уходят в набор, а не в никуда (D-13): имя панели проверяется ещё раз.
	for _, o := range plan.Orphans {
		if !IsPanelFileName(o.Kernel, o.RelPath) {
			_ = os.RemoveAll(set.Dir)
			return out, fmt.Errorf("%s: %w", o.Key, ErrInvalidPanelName)
		}
		m, err := set.Save(p.d.Roots, o.Key, o.Kernel, o.RelPath, ReasonOrphan)
		if err != nil {
			_ = os.RemoveAll(set.Dir)
			return out, err
		}
		journal = append(journal, JournalFile{Key: o.Key, Existed: m.Existed})
	}
	if err := set.WriteMeta(); err != nil {
		_ = os.RemoveAll(set.Dir)
		return out, err
	}
	startedAt := p.d.Now().UTC()
	err = p.d.Store.Update(func(st *State) error {
		st.Journal = &Journal{BackupDir: set.Dir, Trigger: string(trigger), StartedAt: startedAt, Files: journal}
		return nil
	})
	if err != nil {
		_ = os.RemoveAll(set.Dir)
		return out, err
	}

	fail := func(err error) (writeOutcome, error) {
		out.Written = 0
		out.RollbackErr = p.rollback(set)
		return out, err
	}
	for _, fp := range plan.Files {
		switch fp.Action {
		case ActionWrite:
			if !IsPanelFileName(fp.Kernel, fp.RelPath) {
				return fail(fmt.Errorf("%s: %w", fp.Key, ErrInvalidPanelName))
			}
			if err := os.MkdirAll(filepath.Dir(fp.AbsPath), 0o755); err != nil {
				return fail(fmt.Errorf("%s: %w", fp.Key, err))
			}
			if err := p.d.WriteFile(fp.AbsPath, fp.Content); err != nil {
				return fail(fmt.Errorf("%s: %w", fp.Key, err))
			}
			out.Written++
			// Файл, переименованный в .obsolete, заменён свежей записью: метка лишняя.
			if fp.RemoveObsolete {
				if err := os.Remove(fp.AbsPath + obsoleteSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return fail(fmt.Errorf("%s: %w", fp.Key, err))
				}
			}
		case ActionDelete:
			if !IsPanelFileName(fp.Kernel, fp.RelPath) {
				return fail(fmt.Errorf("%s: %w", fp.Key, ErrInvalidPanelName))
			}
			if err := os.Remove(fp.AbsPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fail(fmt.Errorf("%s: %w", fp.Key, err))
			}
		}
	}
	for _, o := range plan.Orphans {
		if !IsPanelFileName(o.Kernel, o.RelPath) {
			return fail(fmt.Errorf("%s: %w", o.Key, ErrInvalidPanelName))
		}
		if err := os.Remove(o.AbsPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("%s: %w", o.Key, err))
		}
		out.OrphansRemoved = append(out.OrphansRemoved, filepath.Base(o.RelPath))
	}
	return out, nil
}

// checkSymlinks отказывает до первой записи, если файл панели — симлинк, цель
// которого лежит за пределами корней ядер: AtomicReplaceFile следует симлинку и
// перезаписал бы чужой файл (T-144-18). Симлинк внутри корней (config.yaml на
// профиль) допустим.
func (p *Pipeline) checkSymlinks(plan Plan) error {
	var roots []string
	for _, r := range []string{p.d.Roots.Xray, p.d.Roots.Mihomo} {
		if r == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(r); err == nil {
			r = resolved
		}
		roots = append(roots, filepath.Clean(r))
	}
	for _, fp := range plan.Files {
		if fp.Action != ActionWrite {
			continue
		}
		fi, err := os.Lstat(fp.AbsPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("%s: %w", fp.Key, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		// Оборванный симлинк не разрешается: отказ так же, как и за корнем.
		target, err := filepath.EvalSymlinks(fp.AbsPath)
		if err != nil || !withinAnyRoot(target, roots) {
			return fmt.Errorf("%s: %w", fp.Key, ErrSymlinkOutsideRoot)
		}
	}
	return nil
}

func withinAnyRoot(path string, roots []string) bool {
	for _, r := range roots {
		if path == r || strings.HasPrefix(path, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// rollback возвращает все файлы набора в прежнее состояние (в обратном порядке:
// новые файлы удаляются) и очищает журнал. Вызывается после сбоя записи, а
// также планом 144-07 после неудачного перезапуска. Если файл вернуть не
// удалось, журнал остаётся для повторной попытки при старте.
func (p *Pipeline) rollback(set *BackupSet) error {
	var errs []error
	for i := len(set.Meta.Files) - 1; i >= 0; i-- {
		if err := set.Restore(p.d.Roots, set.Meta.Files[i]); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("configlayer: rollback: %w", errors.Join(errs...))
	}
	err := p.d.Store.Update(func(st *State) error {
		st.Journal = nil
		return nil
	})
	if err != nil {
		return fmt.Errorf("configlayer: clear journal: %w", err)
	}
	return nil
}

// ErrSymlinkOutsideRoot — файл панели оказался симлинком за пределы корней ядер.
var ErrSymlinkOutsideRoot = errors.New("файл панели — симлинк за пределами каталогов ядер")
