package configlayer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Disable выключает слой (D-04): managed-файлы манифеста переносятся в набор
// копий (причина flag_off) и удаляются из рабочих каталогов, released остаются на
// месте и в манифесте, перезапускается только запущенное ядро с изменениями.
// Сбой рестарта возвращает файлы и ошибку. Файл состояния и Applied сохраняются:
// повторное включение собирает файлы из них.
//
// Если панель взяла маршрутизацию (State.Routing), выключение сначала возвращает
// её (D-03): перенесённые файлы routing Xray из копий и прежнюю цель config.yaml
// Mihomo, и только затем убирает файлы панели — в том же наборе копий и журнале.
// Возврат делается для всех ядер из State.Routing, а не только установленных:
// файлы должны вернуться и без бинарника. Файл, имя которого пользователь уже
// занял (release_conflict), не перезаписывается — остальное продолжается.
// После выключения каталоги ядер такие же, как до взятия (D-04 из 144).
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

	// Возврат маршрутизации (D-03) считается из фактического владения и входит в
	// тот же план: операции возврата выполняются раньше удаления файлов панели.
	if !st.Routing.isEmpty() {
		ops := computeRoutingOps(RoutingDesired{V: routingSectionVersion}, st.Routing, InstalledKernels{Xray: true, Mihomo: true}, "", routingScans{}, l.opts.Now())
		l.dropReleaseConflicts(ops)
		plan.Routing = ops
	}

	switch {
	case len(plan.Files) > 0 || plan.Routing.hasXrayDisk() || plan.Routing.hasMihomoDisk():
		if err := l.moveManagedToBackup(ctx, plan); err != nil {
			return err
		}
	case plan.Routing != nil:
		// Взятие без операций на диске (config.yaml уже указывал на профиль панели,
		// переносить было нечего): возвращать нечего, достаточно снять владение.
		err := l.store.Update(func(st *State) error {
			st.Routing = plan.Routing.Next.clone()
			return nil
		})
		if err != nil {
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
	l.SetNotices(noticeRoutingAutoTakenPrefix, nil)
	l.invalidateVersions()
	l.checkNow(true)
	return nil
}

// dropReleaseConflicts убирает из операций возврата файлы, чьё имя в каталоге
// Xray уже занято (пользователь вернул файл сам, T-146-19): перезаписи нет, имя
// попадает в лог без содержимого, остальное возвращается.
func (l *Layer) dropReleaseConflicts(ops *RoutingOps) {
	if ops == nil || len(ops.XrayRelease) == 0 {
		return
	}
	keep := ops.XrayRelease[:0:0]
	for _, mf := range ops.XrayRelease {
		abs, err := l.opts.Roots.Abs(KernelXray, mf.Name)
		if err == nil {
			if _, statErr := os.Lstat(abs); statErr == nil {
				log.Printf("[configlayer] выключение слоя: файл %s уже лежит в каталоге Xray, не перезаписывается", utils.SanitizeLogInput(mf.Name))
				continue
			}
		}
		keep = append(keep, mf)
	}
	ops.XrayRelease = keep
}

// moveManagedToBackup копирует файлы плана в набор с триггером "flag_off", удаляет их из
// рабочих каталогов (копирование и удаление, не rename: каталоги могут лежать на
// разных файловых системах), перезапускает затронутые ядра и фиксирует манифест.
// Любая неудача возвращает файлы из набора.
func (l *Layer) moveManagedToBackup(ctx context.Context, plan Plan) error {
	return l.commitDiskPlan(ctx, plan, TriggerFlagOff, ReasonFlagOff)
}

// commitDiskPlan выполняет план слоя вне конвейера «Применить» (выключение флага,
// «вернуть» маршрутизацию): копии файлов плана и операций маршрутизации идут в один
// набор и один журнал; сначала операции маршрутизации (возврат чужих файлов и
// config.yaml раньше удаления файлов панели), затем удаления; затем перезапуск
// затронутых ядер и фиксация манифеста и State.Routing одной записью состояния.
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
		}
		st.Journal = nil
		return nil
	})
	if err != nil {
		rbErr := l.pipeline.rollback(set)
		return errors.Join(err, rbErr)
	}
	_ = PruneBackups(l.opts.DataDir, BackupRetention, l.store.Snapshot().Routing.backupDirs()...)
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
		req := ApplyRequest{Trigger: TriggerFlagOn, Source: SourceApplied}
		view := l.pipeline.Run(l.ctx, req)
		release()
		l.afterRun()
		l.fireAfterApply(req, view)
	})
}
