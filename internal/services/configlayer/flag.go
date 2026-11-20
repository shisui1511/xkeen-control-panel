package configlayer

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// commitDiskPlan выполняет план слоя вне конвейера «Применить» («вернуть»
// маршрутизацию и секции): копии файлов плана и операций маршрутизации идут в один
// набор и один журнал; сначала операции маршрутизации (возврат чужих файлов и
// config.yaml раньше удаления файлов панели), затем удаления; затем перезапуск
// затронутых ядер и фиксация манифеста, State.Routing и State.Cores (если план
// несёт NextCores) одной записью состояния.
// Неудача любого шага возвращает всё из набора. Вызывается под замками применения.
func (l *Layer) commitDiskPlan(ctx context.Context, plan Plan, trigger Trigger, reason string) error {
	set, err := NewBackupSet(l.opts.DataDir, l.opts.Now(), string(trigger))
	if err != nil {
		return err
	}
	journal := make([]JournalFile, 0, len(plan.Files))
	for _, fp := range plan.Files {
		m, err := set.Save(l.opts.Roots, fp.Key, fp.Kernel, fp.RelPath, reason)
		if err != nil {
			_ = os.RemoveAll(set.Dir)
			return err
		}
		journal = append(journal, JournalFile{Key: fp.Key, Existed: m.Existed})
	}
	// Копии переносимых файлов и config.yaml — в тот же набор до журнала.
	routingJournal, routingFiles, err := l.pipeline.prepareRouting(set, plan.Routing)
	if err != nil {
		_ = os.RemoveAll(set.Dir)
		return err
	}
	journal = append(journal, routingFiles...)
	if err := set.WriteMeta(); err != nil {
		_ = os.RemoveAll(set.Dir)
		return err
	}
	startedAt := l.opts.Now().UTC()
	// Журнал — до удаления: прерванная операция откатится через RecoverJournal.
	err = l.store.Update(func(st *State) error {
		st.Journal = &Journal{BackupDir: set.Dir, Trigger: string(trigger), StartedAt: startedAt, Files: journal, Routing: routingJournal}
		return nil
	})
	if err != nil {
		_ = os.RemoveAll(set.Dir)
		return err
	}

	// Возврат чужого routing и прежнего профиля — раньше, чем уйдут файлы панели:
	// иначе Xray остался бы без routing, а config.yaml повис бы на удалённом профиле.
	if err := l.pipeline.applyRoutingOps(plan.Routing); err != nil {
		rbErr := l.pipeline.rollback(set)
		return errors.Join(err, rbErr)
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
	if l.pipeline.d.Applier != nil {
		if _, err := l.pipeline.restartKernels(ctx, plan, set); err != nil {
			// Остановка панели: журнал остаётся, файлы вернёт RecoverJournal (как в Run).
			if errors.Is(err, errInterrupted) {
				return err
			}
			var re *restartError
			if !errors.As(err, &re) {
				err = errors.Join(err, l.pipeline.rollback(set))
			}
			return err
		}
	}

	err = l.store.Update(func(st *State) error {
		for _, fp := range plan.Files {
			if e, ok := st.Manifest[fp.Key]; ok && e.Status == StatusManaged {
				delete(st.Manifest, fp.Key)
			}
		}
		if plan.Routing != nil {
			st.Routing = plan.Routing.Next.clone()
			// Владение секциями (147-14): «Вернуть файл панели» записывает новую
			// копию в Moved — как commitState.
			if plan.Routing.NextCores != nil {
				st.Cores = plan.Routing.NextCores.clone()
			}
		}
		st.Journal = nil
		return nil
	})
	if err != nil {
		rbErr := l.pipeline.rollback(set)
		return errors.Join(err, rbErr)
	}
	_ = PruneBackups(l.opts.DataDir, BackupRetention, protectedBackupDirs(l.store.Snapshot())...)
	return nil
}

// protectedBackupDirs — наборы копий, которые ротация не удаляет: на них
// ссылаются владение маршрутизацией и секциями.
func protectedBackupDirs(st State) []string {
	return append(st.Routing.backupDirs(), st.Cores.backupDirs()...)
}
