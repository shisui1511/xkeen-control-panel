package configlayer

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Предпросмотр «Применить» (D-08…D-11, CFG-02): окно изменений перед применением
// получает строки изменений по сущностям, объявления перезапуска и файлы ядер
// «было на диске → станет». Файлы собирает та же функция, что и конвейер
// (Pipeline.buildPlan, SourceDraft), поэтому показанное совпадает с тем, что
// запишется. Предпросмотр только читает: замок применения не берёт, диск не меняет.

// Коды блокировки предпросмотра (PreviewBlock.Code): те же причины, по которым
// «Применить» откажет сейчас.
const (
	PreviewBlockedDrift   = "drift_blocked"
	PreviewBlockedChecks  = "checks_failed"
	PreviewBlockedApply   = "apply_busy"
	PreviewBlockedGeodata = "geodata_busy"
)

// PreviewFile — файл ядра, который «Применить» запишет или удалит. Before — что
// лежит на диске сейчас, After — что станет (у удаления пусто). Added и Removed —
// число строк «станет», которых нет в «было», и наоборот (с учётом повторов),
// посчитанное сервером. Missing — файла на диске нет; Truncated — сторона длиннее
// предела diffSideLimit обрезана.
type PreviewFile struct {
	Key            string `json:"key"`
	Path           string `json:"path"`
	Action         string `json:"action"`
	Before         string `json:"before"`
	After          string `json:"after"`
	Added          int    `json:"added"`
	Removed        int    `json:"removed"`
	Missing        bool   `json:"missing"`
	Truncated      bool   `json:"truncated"`
	SecretsChanged int    `json:"secrets_changed"`
}

// PreviewKernel — файлы одного установленного ядра. Active — ядро активно сейчас;
// Deferred — неактивное ядро: файлы пишутся, а действуют при его запуске (D-10).
type PreviewKernel struct {
	Kernel   string        `json:"kernel"`
	Active   bool          `json:"active"`
	Deferred bool          `json:"deferred"`
	Files    []PreviewFile `json:"files"`
}

// PreviewBlock — причина, по которой «Применить» сейчас откажет; Count — число
// расхождений или проблем проверки (для занятого замка — 0).
type PreviewBlock struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// PreviewMoved — чужой файл ядра, который при взятии уходит в копию.
type PreviewMoved struct {
	Kernel string `json:"kernel"`
	Path   string `json:"path"`
}

// ApplyPreview — ответ GET /api/configlayer/apply/preview. Count равно
// len(Changes) и draft_changes снимка той же ревизии (D-12). Kernels — установленные
// ядра, активное первым. Blocked — nil, если «Применить» сейчас не откажет.
type ApplyPreview struct {
	DraftRevision  int64           `json:"draft_revision"`
	Count          int             `json:"count"`
	Changes        []ChangeRow     `json:"changes"`
	Restart        []RestartNote   `json:"restart"`
	Kernels        []PreviewKernel `json:"kernels"`
	Moved          []PreviewMoved  `json:"moved"`
	Blocked        *PreviewBlock   `json:"blocked"`
	SecretsChanged int             `json:"secrets_changed"`
}

// PreviewApply собирает предпросмотр «Применить» по текущему черновику. Дрейф,
// проблемы проверки и занятый замок приходят полем Blocked, а не ошибкой: окно
// показывает причину внутри себя. Ошибка — только выключенный слой (ErrDisabled)
// или сбой чтения диска и сборки, не связанный с проверками.
func (l *Layer) PreviewApply() (ApplyPreview, error) {
	if !l.Enabled() {
		return ApplyPreview{}, ErrDisabled
	}
	st := l.store.Snapshot()
	installed := l.installed()
	out := ApplyPreview{
		DraftRevision: st.DraftRevision,
		Changes:       []ChangeRow{},
		Restart:       l.restartNotes(st),
		Kernels:       []PreviewKernel{},
		Moved:         []PreviewMoved{},
	}
	// Тот же счёт, что у draft_changes: чистый черновик строк не даёт.
	if draftDirty(st) {
		for _, row := range l.store.changeRows(st) {
			row.Label = maskLabel(row.Label)
			out.Changes = append(out.Changes, row)
		}
	}
	out.Count = len(out.Changes)

	// Порядок причин — как у StartApply: проверки черновика, занятый замок, дрейф.
	if issues := l.cachedChecks(st); len(issues) > 0 {
		out.Blocked = &PreviewBlock{Code: PreviewBlockedChecks, Count: len(issues)}
	} else if l.BusyHolder() != "" {
		out.Blocked = &PreviewBlock{Code: PreviewBlockedGeodata}
	} else if l.pipeline.Current().Running {
		out.Blocked = &PreviewBlock{Code: PreviewBlockedApply}
	}

	kernels := l.previewKernels(installed)
	req := ApplyRequest{Trigger: TriggerUser, Source: SourceDraft}
	plan, _, err := l.pipeline.buildPlan(st, &req, installed)
	var de *driftBlockedError
	var be *BuildError
	switch {
	case errors.As(err, &de):
		if out.Blocked == nil {
			out.Blocked = &PreviewBlock{Code: PreviewBlockedDrift, Count: de.count}
		}
		out.Kernels = kernels
		return out, nil
	case errors.As(err, &be):
		if out.Blocked == nil || out.Blocked.Code != PreviewBlockedChecks {
			out.Blocked = &PreviewBlock{Code: PreviewBlockedChecks, Count: len(be.Issues)}
		}
		out.Kernels = kernels
		return out, nil
	case err != nil:
		return ApplyPreview{}, err
	}

	byKernel := make(map[string]int, len(kernels))
	for i, k := range kernels {
		byKernel[k.Kernel] = i
	}
	add := func(kernel string, pf PreviewFile) {
		if i, ok := byKernel[kernel]; ok {
			kernels[i].Files = append(kernels[i].Files, pf)
			out.SecretsChanged += pf.SecretsChanged
		}
	}
	for _, fp := range plan.Files {
		if fp.Action != ActionWrite && fp.Action != ActionDelete {
			continue
		}
		var after []byte
		if fp.Action == ActionWrite {
			after = fp.Content
		}
		pf, changed, err := previewFileOf(fp.Key, fp.AbsPath, fp.Action, after)
		if err != nil {
			return ApplyPreview{}, err
		}
		if changed {
			add(fp.Kernel, pf)
		}
	}
	// Сироты удаляются тем же применением.
	for _, o := range plan.Orphans {
		pf, changed, err := previewFileOf(o.Key, o.AbsPath, ActionDelete, nil)
		if err != nil {
			return ApplyPreview{}, err
		}
		if changed {
			add(o.Kernel, pf)
		}
	}
	out.Kernels = kernels
	out.Moved = l.previewMoved(plan.Routing)
	return out, nil
}

// previewKernels — установленные ядра для предпросмотра: активное первым, у
// остальных Deferred (D-10). Список файлов не nil.
func (l *Layer) previewKernels(installed InstalledKernels) []PreviewKernel {
	active := l.activeKernel()
	out := []PreviewKernel{}
	for _, k := range []string{KernelXray, KernelMihomo} {
		if !installed.Has(k) {
			continue
		}
		pk := PreviewKernel{Kernel: k, Active: k == active, Deferred: k != active, Files: []PreviewFile{}}
		if pk.Active {
			out = append([]PreviewKernel{pk}, out...)
		} else {
			out = append(out, pk)
		}
	}
	return out
}

// activeKernel — активное ядро по тому же решению, что принимает шаг перезапуска
// конвейера (KernelApplier.Preview): работающее ядро, иначе name_client XKeen.
// Запущены оба (конфликт) или ядро не определено — пустая строка. Без Applier —
// работающий процесс ядра.
func (l *Layer) activeKernel() string {
	if l.opts.Applier != nil {
		switch k := l.opts.Applier.Preview(services.ApplyTargetActive).ActiveKernel; k {
		case KernelXray, KernelMihomo:
			return k
		}
		return ""
	}
	if l.opts.ProcessStates == nil {
		return ""
	}
	for _, st := range l.opts.ProcessStates() {
		if st.Status == "running" && (st.Name == KernelXray || st.Name == KernelMihomo) {
			return st.Name
		}
	}
	return ""
}

// previewMoved — чужие файлы, которые при взятии уходят в копии: «чистые» файлы
// routing и файлы взятых секций Xray, обычный config.yaml Mihomo при перекладке на
// профиль панели. Результат не nil, пути без повторов и по порядку.
func (l *Layer) previewMoved(ops *RoutingOps) []PreviewMoved {
	out := []PreviewMoved{}
	if ops == nil {
		return out
	}
	seen := map[string]bool{}
	push := func(kernel, rel string) {
		abs, err := l.opts.Roots.Abs(kernel, rel)
		if err != nil || seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, PreviewMoved{Kernel: kernel, Path: abs})
	}
	for _, name := range ops.XrayTake {
		push(KernelXray, name)
	}
	for _, names := range ops.CoreTake {
		for _, name := range names {
			push(KernelXray, name)
		}
	}
	if ops.MihomoPoint {
		if abs, err := l.opts.Roots.Abs(KernelMihomo, mihomoConfigFile); err == nil {
			if fi, err := os.Lstat(abs); err == nil && fi.Mode().IsRegular() {
				push(KernelMihomo, mihomoConfigFile)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kernel != out[j].Kernel {
			return out[i].Kernel < out[j].Kernel
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// diffCountLimit — предел чтения стороны «было» для сверки и счёта строк, когда
// она длиннее diffSideLimit: показ усекается до diffSideLimit, а счётчики и
// «не меняется» считаются по файлу целиком, если он не больше этого предела.
const diffCountLimit = 4 * 1024 * 1024

// previewFileOf — сторона «было» с диска (показ не больше diffSideLimit байт) и
// «станет» для одного файла плана. Второй результат ложь — файл не меняется
// (содержимое совпадает или удалять нечего). Обе стороны проходят maskPair до
// счёта строк и усечения: сырые значения секретов в ответ не попадают (D-11).
func previewFileOf(key, abs string, action PlanAction, after []byte) (PreviewFile, bool, error) {
	pf := PreviewFile{Key: key, Path: abs, Action: string(action)}
	before, err := readCapped(abs, diffSideLimit)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		pf.Missing = true
		before = nil
	case err != nil:
		return PreviewFile{}, false, fmt.Errorf("%s: %w", key, err)
	}
	if action == ActionDelete && pf.Missing {
		return pf, false, nil
	}
	// Сторона «было» длиннее предела показа: для сверки с «станет» и счёта строк
	// файл читается дальше (до diffCountLimit), иначе усечённая сторона всегда
	// «отличается» и даёт заведомо неверные счётчики.
	limit := diffSideLimit
	longBefore := len(before) > diffSideLimit
	if longBefore {
		if full, ferr := readCapped(abs, diffCountLimit); ferr == nil {
			before, limit = full, diffCountLimit
		}
	}
	if action == ActionWrite && !pf.Missing && bytes.Equal(before, after) {
		return pf, false, nil
	}
	// Недочитанный до конца хвост «было» обрывается на границе строки: обрывок
	// строки с секретом не должен попасть мимо маскирования.
	beforeText, _ := cutLines(string(before), limit)
	maskedBefore, maskedAfter, changed := maskPair(beforeText, string(after))
	pf.SecretsChanged = changed
	pf.Added, pf.Removed = lineDelta(maskedBefore, maskedAfter)
	var bt, at bool
	pf.Before, bt = cutLines(maskedBefore, diffSideLimit)
	pf.After, at = cutLines(maskedAfter, diffSideLimit)
	pf.Truncated = longBefore || bt || at
	return pf, true, nil
}

// cutLines обрезает s до max байт по последней целой строке (без строки длиннее
// предела — по границе символа UTF-8); второй результат — была ли обрезка.
func cutLines(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	if i := strings.LastIndexByte(s[:max], '\n'); i >= 0 {
		return s[:i+1], true
	}
	return truncateUTF8([]byte(s), max)
}

// lineDelta — число строк after, которых нет в before, и строк before, которых нет
// в after, как у мультимножеств (повторы учитываются). Линейно по размеру сторон.
func lineDelta(before, after string) (added, removed int) {
	counts := map[string]int{}
	for _, s := range splitLines(before) {
		counts[s]++
	}
	for _, s := range splitLines(after) {
		if counts[s] > 0 {
			counts[s]--
		} else {
			added++
		}
	}
	for _, n := range counts {
		removed += n
	}
	return added, removed
}

// splitLines делит текст на строки; пустой текст — ни одной строки, завершающий
// перевод строки пустой строки не даёт.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
