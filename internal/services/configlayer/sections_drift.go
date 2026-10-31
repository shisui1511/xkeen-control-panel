package configlayer

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Дрейф взятых секций Xray и владение секциями в снимке слоя (147-14, SEC-01,
// SEC-02; образец — дрейф маршрутизации из 146, D-07). Пока дрейф не решён,
// панель чужой файл не разбирает и значения из него не переносит (FND-03).

// ErrUnknownSection — идентификатор секции Xray неизвестен.
var ErrUnknownSection = errors.New("неизвестная секция")

// sectionsDrift находит чужие файлы с ключом взятой секции, которые нельзя
// оставить молча: появившиеся после взятия (их нет среди оставленных при взятии,
// например 02_dns.json, возвращённый XKeen) и, кроме «Входящих» (они объединяются
// по тегам), сортирующиеся после файла панели секции (Xray заменяет объект
// поздним файлом целиком). Файлы старого слоя не учитываются: он переписывает их
// сам. Ядро вне области scope или без бинарника не сверяется; нечитаемый
// каталог дрейфом не считается.
func sectionsDrift(roots Roots, st CoresState, installed InstalledKernels, scope string, foreignOwned func(kernel, rel string) bool) []RoutingDriftItem {
	if st.isEmpty() || !kernelInScope(KernelXray, installed, scope) {
		return nil
	}
	files, err := ScanXrayKeyFiles(roots.Xray, foreignOwned)
	if err != nil {
		return nil
	}
	var out []RoutingDriftItem
	for _, id := range XraySectionIDs {
		sec := st.Xray[id]
		if sec == nil {
			continue
		}
		left := make(map[string]bool, len(sec.Left))
		for _, n := range sec.Left {
			left[n] = true
		}
		panelFile := PanelXraySectionFile(id)
		for _, f := range files {
			if f.Legacy || sectionKeyCount(f, id) == 0 {
				continue
			}
			if !left[f.Name] || (id != xraySectionInbounds && f.Name > panelFile) {
				out = append(out, RoutingDriftItem{Kernel: KernelXray, Code: DriftSectionForeign, File: f.Name, Section: id})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		return out[i].File < out[j].File
	})
	return out
}

// sectionDriftBlocks — среди расхождений есть секция, которую желаемое состояние
// (src) держит под управлением панели: такой запуск не идёт. Секция с
// managed=false — это «Отпустить», она дрейфом не блокируется.
func sectionDriftBlocks(src Sections, items []RoutingDriftItem) bool {
	if len(items) == 0 {
		return false
	}
	desired := DesiredCores(src)
	for _, it := range items {
		if desired[CoreSectionName(KernelXray, it.Section)].Managed {
			return true
		}
	}
	return false
}

// ReclaimSection решает дрейф секции id, оставляя секцию у панели (аналог
// ReclaimRouting): каждый «чистый» чужой файл секции переносится в копию и
// добавляется к перенесённым, затем Xray перезапускается; если среди расхождений
// есть смешанный файл — *ForeignMixedError без изменений (его решает только
// «Отпустить»). Нет дрейфа секции — ErrNothingToReclaim. Всё под журналом и
// откатом, как взятие; занятое применение — ErrApplyBusy или ErrKernelBusy.
func (l *Layer) ReclaimSection(ctx context.Context, id string) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	if !knownXraySection(id) {
		return fmt.Errorf("%w: %q", ErrUnknownSection, id)
	}
	release, err := l.pipeline.TryBegin(ctx, true)
	if err != nil {
		return l.mapBusy(err)
	}
	defer release()
	if err := l.pipeline.recoverPendingJournal(); err != nil {
		return err
	}
	st := l.store.Snapshot()
	var items []RoutingDriftItem
	for _, it := range sectionsDrift(l.opts.Roots, st.Cores, l.installed(), KernelXray, l.opts.ForeignOwned) {
		if it.Section == id {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return ErrNothingToReclaim
	}
	files, err := ScanXrayKeyFiles(l.opts.Roots.Xray, l.opts.ForeignOwned)
	if err != nil {
		return err
	}
	byName := make(map[string]XrayKeyFile, len(files))
	for _, f := range files {
		byName[f.Name] = f
	}
	// Сначала проверяются все файлы: смешанный отказывает без единого изменения.
	for _, it := range items {
		f, ok := byName[it.File]
		if !ok || f.Broken || f.Symlink || f.Legacy || sectionKeyCount(f, id) != len(f.Keys) || !safeRoutingName(f.Name) {
			return &ForeignMixedError{File: it.File}
		}
	}
	next := st.Cores.clone()
	taking := make(map[string]bool, len(items))
	names := make([]string, 0, len(items))
	for _, it := range items {
		taking[it.File] = true
		names = append(names, it.File)
	}
	// Файл, который раньше был смешанным, а теперь перенесён, оставленным не числится.
	sec := next.Xray[id]
	left := make([]string, 0, len(sec.Left))
	for _, n := range sec.Left {
		if !taking[n] {
			left = append(left, n)
		}
	}
	sec.Left = left

	ops := &RoutingOps{
		Next:      st.Routing.clone(),
		CoreTake:  map[string][]string{id: names},
		NextCores: &next,
	}
	if err := l.commitDiskPlan(ctx, Plan{Routing: ops}, TriggerRoutingReclaim, ReasonCoresTakeover); err != nil {
		return err
	}
	l.PublishRouting()
	l.checkNow(true)
	return nil
}

// knownXraySection — id входит в список секций Xray.
func knownXraySection(id string) bool {
	for _, known := range XraySectionIDs {
		if known == id {
			return true
		}
	}
	return false
}

// coresSnapshot собирает раздел cores снимка: по каждой секции Xray, которая
// есть в черновике (managed) или у которой есть запись владения, — желаемое,
// взятое, файл панели, перенесённые и оставленные файлы и дрейф. Xray не
// установлен — пустая карта; все срезы не nil.
func (l *Layer) coresSnapshot(st State, drift []RoutingDriftItem) CoresSnapshot {
	out := CoresSnapshot{Xray: map[string]CoreSectionSnap{}, Restart: []RestartNote{}}
	if !l.installed().Xray {
		return out
	}
	desired := DesiredCores(st.Draft)
	for _, id := range XraySectionIDs {
		want := desired[CoreSectionName(KernelXray, id)].Managed
		sec := st.Cores.Xray[id]
		if !want && sec == nil {
			continue
		}
		v := CoreSectionSnap{
			Desired: want,
			Taken:   sec != nil,
			File:    PanelXraySectionFile(id),
			Moved:   []string{},
			Left:    []string{},
			Drift:   []string{},
		}
		if sec != nil {
			for _, m := range sec.Moved {
				v.Moved = append(v.Moved, m.Name)
			}
			v.Left = append(v.Left, sec.Left...)
		}
		for _, it := range drift {
			if it.Section == id {
				v.Drift = append(v.Drift, it.File)
			}
		}
		out.Xray[id] = v
	}
	return out
}
