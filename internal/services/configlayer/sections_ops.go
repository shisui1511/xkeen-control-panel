package configlayer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Взятие и возврат секций Xray внутри «Применить» (SEC-01, XCFG-03; D-01, D-02,
// D-05, D-25; образец — takeover.go из 146). Желаемое — секции xray_* черновика
// (managed), фактическое — State.Cores. Желаемое managed ∧ нет записи в
// State.Cores — взятие, обратное — возврат. Операции дополняют те же RoutingOps,
// что и маршрутизация: файлы секций идут тем же путём, что «чистые» файлы
// routing (резервный набор, журнал, общий откат, зеркало проверки ядром).

// coreTransitions сообщает, есть ли в черновике секции Xray, которые нужно
// взять или вернуть.
func coreTransitions(desired map[string]CoreSection, actual CoresState) (taking, releasing bool) {
	for _, id := range XraySectionIDs {
		managed := desired[CoreSectionName(KernelXray, id)].Managed
		_, held := actual.Xray[id]
		switch {
		case managed && !held:
			taking = true
		case !managed && held:
			releasing = true
		}
	}
	return taking, releasing
}

// computeCoreOps считает операции над секциями Xray по желаемому (секции
// черновика) и фактическому (State.Cores). Взятие: «чистые» файлы секции из
// скана уходят в резервный набор, смешанные остаются на месте и попадают в Left.
// Возврат: файлы из Moved возвращаются в configs/, запись удаляется. issues —
// проблемы взятия, найденные по сканам до записи (coreTakeIssues); пустой скан
// секции в сравнение не входит. Ядро вне области запуска вызывающий отсекает до
// вызова.
func computeCoreOps(desired map[string]CoreSection, actual CoresState, scans map[string]XraySectionScan, now time.Time) (take map[string][]string, release map[string][]MovedFile, next CoresState, issues []BuildIssue) {
	next = actual.clone()
	now = now.UTC()
	for _, id := range XraySectionIDs {
		managed := desired[CoreSectionName(KernelXray, id)].Managed
		cur := actual.Xray[id]
		switch {
		case managed && cur == nil:
			scan := scans[id]
			if next.Xray == nil {
				next.Xray = map[string]*XraySectionState{}
			}
			next.Xray[id] = &XraySectionState{
				TakenAt: now,
				File:    PanelXraySectionFile(id),
				Moved:   []MovedFile{},
				Left:    append([]string{}, scan.Mixed...),
			}
			if len(scan.Pure) > 0 {
				if take == nil {
					take = map[string][]string{}
				}
				take[id] = append([]string(nil), scan.Pure...)
			}
		case !managed && cur != nil:
			if len(cur.Moved) > 0 {
				if release == nil {
					release = map[string][]MovedFile{}
				}
				release[id] = append([]MovedFile(nil), cur.Moved...)
			}
			delete(next.Xray, id)
		}
	}
	return take, release, next, coreTakeIssues(desired, actual, scans, nil)
}

// mihomoActiveReader читает активный профиль Mihomo: имя и md5 содержимого.
type mihomoActiveReader func() (name, hash string, err error)

// coreTakeIssues собирает проблемы взятия секций до записи (T-147-10, T-147-11,
// T-147-12); код каждой — константа из sections.go, Key — имя секции черновика,
// Params {"file": имя}. Проверки идут только для берущихся секций (managed без
// записи в actual) и только там, где есть скан. Для Xray: (1) from — набор
// {имя, md5} должен совпасть с текущими Sources скана секции; (2) в переносимых
// «чистых» файлах есть комментарии JSONC, а comments_ack не выставлен (D-02);
// (3) «Входящие» при смешанном файле с inbounds (D-25); (4) секция перекрыта
// (sectionShadowFile, D-28). Для Mihomo при перекладке config.yaml на
// профиль панели (mihomo != nil): md5 текущего активного профиля должен совпасть
// с from секции.
//
// Правило (4) для log, dns, policy и «Прочих ключей» (D-28): Xray сливает эти
// объекты из файлов вглубь, поэтому секция не берётся (код IssueSectionShadowed),
// если у её ключа есть смешанный файл (его остаток продолжил бы сливаться со
// значением панели), файл после файла панели или значение секции неизвестно
// (дамп ядра не получен). Для «Входящих» правило прежнее — по файлу после файла
// панели; смешанный файл блокирует их отдельно (IssueInboundsMixed, D-25).
func coreTakeIssues(desired map[string]CoreSection, actual CoresState, scans map[string]XraySectionScan, mihomo mihomoActiveReader) []BuildIssue {
	var out []BuildIssue
	for _, id := range XraySectionIDs {
		name := CoreSectionName(KernelXray, id)
		cs := desired[name]
		if !cs.Managed || actual.Xray[id] != nil {
			continue
		}
		scan, ok := scans[id]
		if !ok {
			continue
		}
		issue := func(code, file string) BuildIssue {
			return BuildIssue{Key: name, Code: code, Params: map[string]string{"file": file}}
		}
		if file, differs := firstSourceMismatch(cs.From, scan.Sources); differs {
			out = append(out, issue(IssueSectionTakeoverFailed, file))
		}
		if !cs.CommentsAck {
			if file := firstCommon(scan.Pure, scan.Comments); file != "" {
				out = append(out, issue(IssueCommentsNotAck, file))
			}
		}
		if id == xraySectionInbounds && scan.InboundsMixed && len(scan.Mixed) > 0 {
			out = append(out, issue(IssueInboundsMixed, scan.Mixed[0]))
		}
		if file := sectionShadowFile(id, scan); file != "" {
			out = append(out, issue(IssueSectionShadowed, file))
		}
	}
	if mihomo == nil {
		return out
	}
	var (
		read    bool
		profile string
		hash    string
		readErr error
	)
	for _, id := range MihomoSectionIDs {
		name := CoreSectionName(KernelMihomo, id)
		cs := desired[name]
		if !cs.Managed || len(cs.From) == 0 {
			continue
		}
		if !read {
			profile, hash, readErr = mihomo()
			read = true
		}
		switch {
		case readErr != nil:
			out = append(out, BuildIssue{
				Key: name, Code: IssueSectionTakeoverFailed, Detail: readErr.Error(),
				Params: map[string]string{"file": "config.yaml"},
			})
		case hash != cs.From[0].Hash:
			out = append(out, BuildIssue{
				Key: name, Code: IssueSectionTakeoverFailed,
				Params: map[string]string{"file": profile},
			})
		}
	}
	return out
}

// sectionShadowFile возвращает файл, из-за которого секцию Xray нельзя взять по
// правилу D-28 (см. coreTakeIssues); пустая строка — препятствий нет.
func sectionShadowFile(id string, scan XraySectionScan) string {
	if id == xraySectionInbounds {
		if len(scan.Shadowing) > 0 {
			return scan.Shadowing[0]
		}
		return ""
	}
	switch {
	case len(scan.Mixed) > 0:
		return scan.Mixed[0]
	case len(scan.Shadowing) > 0:
		return scan.Shadowing[0]
	case scan.EffectiveUnknown && len(scan.Files) > 0:
		return scan.Files[0]
	}
	return ""
}

// firstSourceMismatch сравнивает from секции с текущими источниками скана:
// расхождение хэша, пропавший или новый файл — имя первого (по порядку) файла
// расхождения.
func firstSourceMismatch(from, current []SourceFile) (file string, differs bool) {
	want := make(map[string]string, len(from))
	for _, f := range from {
		want[f.Name] = f.Hash
	}
	have := make(map[string]string, len(current))
	for _, f := range current {
		have[f.Name] = f.Hash
	}
	for _, f := range from {
		if h, ok := have[f.Name]; !ok || h != f.Hash {
			return f.Name, true
		}
	}
	for _, f := range current {
		if _, ok := want[f.Name]; !ok {
			return f.Name, true
		}
	}
	return "", false
}

// firstCommon возвращает первое имя из a, которое есть и в b.
func firstCommon(a, b []string) string {
	in := make(map[string]bool, len(b))
	for _, n := range b {
		in[n] = true
	}
	for _, n := range a {
		if in[n] {
			return n
		}
	}
	return ""
}

// planCores дополняет операции маршрутизации операциями над секциями Xray:
// условия запуска те же, что у planRouting (без req.Only; SourceDraft,
// TriggerKernelInstalled или TriggerFlagOn). Секции Xray вне области запуска
// или при неустановленном ядре не трогаются. Проблемы взятия возвращаются
// BuildError до записи. Если секции не меняются, возвращается routing как есть
// (в том числе nil); иначе операции сливаются в routing, а при nil создаётся
// новый RoutingOps с прежним владением маршрутизацией.
func (p *Pipeline) planCores(ctx context.Context, src Sections, actual CoresState, routing *RoutingOps, installed InstalledKernels, req ApplyRequest) (*RoutingOps, error) {
	if len(req.Only) > 0 {
		return routing, nil
	}
	if req.Source != SourceDraft && req.Trigger != TriggerKernelInstalled && req.Trigger != TriggerFlagOn {
		return routing, nil
	}
	desired := DesiredCores(src)

	var issues []BuildIssue
	if routing != nil && routing.MihomoPoint {
		root := p.d.Roots.Mihomo
		issues = append(issues, coreTakeIssues(desired, CoresState{}, nil, func() (string, string, error) {
			name, data, err := ReadMihomoActive(root)
			if err != nil {
				return "", "", err
			}
			return name, HashContent(data), nil
		})...)
	}

	var (
		take    map[string][]string
		release map[string][]MovedFile
		next    CoresState
		changed bool
	)
	taking, releasing := coreTransitions(desired, actual)
	if kernelInScope(KernelXray, installed, req.Kernel) && (taking || releasing) {
		var scans map[string]XraySectionScan
		if taking {
			files, err := ScanXrayKeyFiles(p.d.Roots.Xray, p.d.ForeignOwned)
			if err != nil {
				return nil, &BuildError{Issues: []BuildIssue{{Key: RoutingSection, Code: IssueRoutingScanFailed, Detail: err.Error()}}}
			}
			// Значение log, dns, policy и «Прочих ключей» — из дампа ядра (D-28);
			// дамп не нужен, пока берутся одни «Входящие». Не получен — значение
			// неизвестно, такие секции не берутся.
			var dump map[string]json.RawMessage
			if takesDumpSection(desired, actual) {
				dump, _ = p.dumper.Dump(ctx, p.binaries().Xray, p.d.Roots.Xray, p.d.XrayEnv)
			}
			scans = ScanXraySections(files, dump)
		}
		var xrayIssues []BuildIssue
		take, release, next, xrayIssues = computeCoreOps(desired, actual, scans, p.d.Now())
		issues = append(xrayIssues, issues...)
		changed = true
	}
	if len(issues) > 0 {
		return nil, &BuildError{Issues: issues}
	}
	if !changed {
		return routing, nil
	}

	ops := routing
	if ops == nil {
		ops = &RoutingOps{Next: p.d.Store.Snapshot().Routing.clone()}
	}
	ops.CoreTake, ops.CoreRelease, ops.NextCores = take, release, &next
	if len(take) > 0 {
		ops.coreHashes = map[string]string{}
		for id := range take {
			for _, f := range desired[CoreSectionName(KernelXray, id)].From {
				ops.coreHashes[f.Name] = f.Hash
			}
		}
	}
	if len(release) > 0 {
		if conflicts := releaseConflicts(p.d.Roots, ops); len(conflicts) > 0 {
			return nil, &BuildError{Issues: conflicts}
		}
	}
	return ops, nil
}

// takesDumpSection — среди берущихся секций есть та, чьё значение даёт дамп ядра
// (log, dns, policy, «Прочие ключи»).
func takesDumpSection(desired map[string]CoreSection, actual CoresState) bool {
	for _, id := range XraySectionIDs {
		if id == xraySectionInbounds {
			continue
		}
		if desired[CoreSectionName(KernelXray, id)].Managed && actual.Xray[id] == nil {
			return true
		}
	}
	return false
}

// sectionOwner — ключ верхнего уровня принадлежит секции id.
func sectionOwner(id string) func(key string) bool {
	return func(key string) bool { return XraySectionOfKey(key) == id }
}

// prepareCoreTake готовит перенос «чистых» файлов секций до записи журнала:
// копия каждого файла уходит в набор (причина ReasonCoresTakeover), на диске
// ничего не меняется. md5 копии сверяется с from черновика (T-147-10): не
// совпал или файл исчез — ошибка с кодом IssueSectionTakeoverFailed. Перенесённые
// файлы дописываются в NextCores.Xray[id].Moved (запись с тем же именем
// заменяется), имена — в журнал, откат и восстановление после падения идут по
// набору.
func (p *Pipeline) prepareCoreTake(set *BackupSet, ops *RoutingOps, jr *JournalRouting) ([]JournalFile, error) {
	var files []JournalFile
	for _, id := range XraySectionIDs {
		names := ops.CoreTake[id]
		if len(names) == 0 {
			continue
		}
		if ops.NextCores == nil || ops.NextCores.Xray[id] == nil {
			return nil, fmt.Errorf("%s: %s: нет записи владения секцией", IssueSectionTakeoverFailed, id)
		}
		moved := make([]MovedFile, 0, len(names))
		for _, name := range names {
			if !safeRoutingName(name) {
				return nil, fmt.Errorf("%s: %w", name, ErrInvalidPanelPath)
			}
			abs, err := p.d.Roots.Abs(KernelXray, name)
			if err != nil {
				return nil, err
			}
			if err := verifyPureFile(abs, sectionOwner(id)); err != nil {
				return nil, fmt.Errorf("%s: %s: %w", IssueSectionTakeoverFailed, name, err)
			}
			key := ManifestKey(KernelXray, name)
			m, err := set.Save(p.d.Roots, key, KernelXray, name, ReasonCoresTakeover)
			if err != nil {
				return nil, err
			}
			if !m.Existed {
				return nil, fmt.Errorf("%s: %s: файл исчез", IssueSectionTakeoverFailed, name)
			}
			if want, ok := ops.coreHashes[name]; ok && m.OldHash != want {
				return nil, fmt.Errorf("%s: %s: файл изменился после проверки", IssueSectionTakeoverFailed, name)
			}
			moved = append(moved, MovedFile{Name: name, Hash: m.OldHash, BackupDir: set.Dir})
			jr.XrayTaken = append(jr.XrayTaken, name)
			files = append(files, JournalFile{Key: key, Existed: true})
		}
		st := ops.NextCores.Xray[id]
		replaced := make(map[string]bool, len(moved))
		for _, m := range moved {
			replaced[m.Name] = true
		}
		merged := make([]MovedFile, 0, len(st.Moved)+len(moved))
		for _, m := range st.Moved {
			if !replaced[m.Name] {
				merged = append(merged, m)
			}
		}
		st.Moved = append(merged, moved...)
	}
	return files, nil
}
