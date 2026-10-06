package configlayer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
)

// Disable выключает слой (D-04): managed-файлы манифеста переносятся в набор
// копий (причина flag_off) и удаляются из рабочих каталогов, released остаются на
// месте и в манифесте, перезапускается только запущенное ядро с изменениями.
// Сбой рестарта возвращает файлы и ошибку. Файл состояния и Applied сохраняются:
// повторное включение собирает файлы из них.
//
// Disable берёт те же замки, что и применение: занятое применение — ErrApplyBusy
// или ErrKernelBusy, диск не трогается (Pitfall 12). Ключ config_layer в
// config.json снимает commitFlag (nil — ничего не делать): он вызывается после
// успешного переноса файлов, но до освобождения замков, поэтому между переносом
// и снятием флага не успевает ни применение, ни фоновая сборка (D-04). Ошибка
// commitFlag возвращается как есть; файлы к этому моменту уже перенесены, и
// решение, вернуть ли их (Enable), остаётся за вызывающим.
func (l *Layer) Disable(ctx context.Context, commitFlag func() error) error {
	release, err := l.pipeline.TryBegin(ctx, true)
	if err != nil {
		return err
	}
	defer release()

	// Журнал прошлой неудавшейся записи не перезаписывается набором выключения.
	if err := l.pipeline.recoverPendingJournal(); err != nil {
		return err
	}
	st := l.store.Snapshot()
	keys := make([]string, 0, len(st.Manifest))
	for key, e := range st.Manifest {
		if e.Status == StatusManaged {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	// Всё проверяется до первой операции с диском: имя панели и путь внутри корня.
	var plan Plan
	for _, key := range keys {
		e := st.Manifest[key]
		abs, err := l.opts.Roots.Abs(e.Kernel, e.RelPath)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if !IsPanelFileName(e.Kernel, e.RelPath) {
			return fmt.Errorf("%s: %w", key, ErrInvalidPanelName)
		}
		plan.Files = append(plan.Files, FilePlan{
			Key: key, Kernel: e.Kernel, RelPath: e.RelPath, AbsPath: abs, Action: ActionDelete, Kind: e.Kind,
		})
	}

	if len(plan.Files) > 0 {
		if err := l.moveManagedToBackup(ctx, plan); err != nil {
			return err
		}
	}
	if commitFlag != nil {
		if err := commitFlag(); err != nil {
			return err
		}
	}
	l.mu.Lock()
	hadFailed := len(l.failed) > 0
	l.failed = make(map[string]NoticeView)
	l.mu.Unlock()
	if hadFailed {
		l.broker.Publish(Event{Type: EventNotices, Data: NoticesEvent{Notices: l.noticeViews(l.store.Snapshot())}})
	}
	l.invalidateVersions()
	l.checkNow(true)
	return nil
}

// moveManagedToBackup копирует файлы плана в набор с триггером "flag_off", удаляет их из
// рабочих каталогов (копирование и удаление, не rename: каталоги могут лежать на
// разных файловых системах), перезапускает затронутые ядра и фиксирует манифест.
// Любая неудача возвращает файлы из набора.
func (l *Layer) moveManagedToBackup(ctx context.Context, plan Plan) error {
	set, err := NewBackupSet(l.opts.DataDir, l.opts.Now(), string(TriggerFlagOff))
	if err != nil {
		return err
	}
	journal := make([]JournalFile, 0, len(plan.Files))
	for _, fp := range plan.Files {
		m, err := set.Save(l.opts.Roots, fp.Key, fp.Kernel, fp.RelPath, ReasonFlagOff)
		if err != nil {
			_ = os.RemoveAll(set.Dir)
			return err
		}
		journal = append(journal, JournalFile{Key: fp.Key, Existed: m.Existed})
	}
	if err := set.WriteMeta(); err != nil {
		_ = os.RemoveAll(set.Dir)
		return err
	}
	startedAt := l.opts.Now().UTC()
	// Журнал — до удаления: прерванное выключение откатит RecoverJournal.
	err = l.store.Update(func(st *State) error {
		st.Journal = &Journal{BackupDir: set.Dir, Trigger: string(TriggerFlagOff), StartedAt: startedAt, Files: journal}
		return nil
	})
	if err != nil {
		_ = os.RemoveAll(set.Dir)
		return err
	}

	for _, fp := range plan.Files {
		if err := os.Remove(fp.AbsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			rbErr := l.pipeline.rollback(set)
			return errors.Join(fmt.Errorf("%s: %w", fp.Key, err), rbErr)
		}
	}

	// Перезапускается только запущенное ядро с изменениями (решение — Preview).
	// При неудаче restartKernels сам возвращает файлы из набора и поднимает ядро на
	// прежних файлах (D-17).
	if _, err := l.pipeline.restartKernels(ctx, plan, set); err != nil {
		var re *restartError
		if !errors.As(err, &re) {
			err = errors.Join(err, l.pipeline.rollback(set))
		}
		return err
	}

	err = l.store.Update(func(st *State) error {
		for _, fp := range plan.Files {
			if e, ok := st.Manifest[fp.Key]; ok && e.Status == StatusManaged {
				delete(st.Manifest, fp.Key)
			}
		}
		st.Journal = nil
		return nil
	})
	if err != nil {
		rbErr := l.pipeline.rollback(set)
		return errors.Join(err, rbErr)
	}
	_ = PruneBackups(l.opts.DataDir, BackupRetention)
	return nil
}

// Enable запускает сборку файлов после включения слоя (D-04): в фоне, из
// применённого состояния, черновик не трогается. Ключ config_layer в config.json
// обработчик сохраняет раньше вызова Enable. Ошибка запуска только логируется:
// результат запуска виден в событиях apply_*.
func (l *Layer) Enable() {
	if !l.Enabled() {
		return
	}
	l.spawn(func() {
		release, err := l.pipeline.TryBegin(l.ctx, false)
		if err != nil {
			log.Printf("[configlayer] сборка после включения слоя не запущена: %v", err)
			return
		}
		defer release()
		l.bootstrap()
		l.pipeline.Run(l.ctx, ApplyRequest{Trigger: TriggerFlagOn, Source: SourceApplied})
		release()
		l.afterRun()
	})
}
