package configlayer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Trigger — что запустило конвейер.
type Trigger string

// Источники запуска.
const (
	TriggerUser            Trigger = "user"
	TriggerRebuild         Trigger = "rebuild"
	TriggerKernelInstalled Trigger = "kernel_installed"
	TriggerFlagOn          Trigger = "flag_on"
	TriggerFlagOff         Trigger = "flag_off"
)

// SourceKind — из какого состояния собираются файлы.
type SourceKind string

// Источники сборки.
const (
	SourceDraft   SourceKind = "draft"
	SourceApplied SourceKind = "applied"
)

// ApplyRequest — параметры одного запуска.
type ApplyRequest struct {
	Trigger Trigger
	Source  SourceKind
	Only    []string
}

// StepID — шаг конвейера.
type StepID string

// Шаги конвейера.
const (
	StepBuild          StepID = "build"
	StepValidateXray   StepID = "validate_xray"
	StepValidateMihomo StepID = "validate_mihomo"
	StepWrite          StepID = "write"
	StepRestart        StepID = "restart"
)

// StepState — состояние шага.
type StepState string

// Состояния шага.
const (
	StepPending  StepState = "pending"
	StepRunning  StepState = "running"
	StepDone     StepState = "done"
	StepFailed   StepState = "failed"
	StepSkipped  StepState = "skipped"
	StepDeferred StepState = "deferred"
)

// StepView — шаг для UI и шины событий.
type StepView struct {
	ID       StepID    `json:"id"`
	State    StepState `json:"state"`
	NoteCode string    `json:"note_code,omitempty"`
	Message  string    `json:"message,omitempty"`
}

// RestartView — итог перезапуска ядра (заполняет план 144-07).
type RestartView struct {
	Kernel   string `json:"kernel"`
	Outcome  string `json:"outcome"`
	NoteCode string `json:"note_code,omitempty"`
}

// Коды итога запуска.
const (
	ResultApplied          = "applied"
	ResultNothingToApply   = "nothing_to_apply"
	ResultBuildFailed      = "build_failed"
	ResultValidationFailed = "validation_failed"
	ResultValidationTimout = "validation_timeout"
	ResultValidationNotRun = "validation_not_run"
	ResultWriteFailed      = "write_failed_rolled_back"
	ResultRestartFailed    = "restart_failed_rolled_back"
	ResultDriftBlocked     = "drift_blocked"
)

// ResultView — итог запуска.
type ResultView struct {
	OK             bool         `json:"ok"`
	Code           string       `json:"code"`
	Kernel         string       `json:"kernel,omitempty"`
	Message        string       `json:"message,omitempty"`
	HintCode       string       `json:"hint_code,omitempty"`
	Written        int          `json:"written"`
	OrphansRemoved []string     `json:"orphans_removed,omitempty"`
	RolledBack     bool         `json:"rolled_back,omitempty"`
	Issues         []BuildIssue `json:"issues,omitempty"`
}

// ApplyView — состояние запуска для UI.
type ApplyView struct {
	Running bool          `json:"running"`
	Trigger Trigger       `json:"trigger,omitempty"`
	Steps   []StepView    `json:"steps"`
	Restart []RestartView `json:"restart,omitempty"`
	Result  *ResultView   `json:"result,omitempty"`
}

// PipelineDeps — зависимости конвейера.
type PipelineDeps struct {
	Store        *Store
	Broker       *Broker
	Registry     *Registry
	Roots        Roots
	DataDir      string
	Binaries     func() Binaries
	XrayEnv      func(dir string) []string
	ForeignOwned func(kernel, rel string) bool
	WriteFile    func(path string, data []byte) error
	Now          func() time.Time
}

// Pipeline — конвейер «Применить»: сборка, проверка Xray, проверка Mihomo,
// запись, коммит манифеста. Блокировок «одно применение за раз» и замка
// жизненного цикла здесь нет: Run вызывается под замками вызывающего (144-07).
type Pipeline struct {
	d    PipelineDeps
	mu   sync.Mutex
	view ApplyView
}

// NewPipeline создаёт конвейер; WriteFile и Now имеют значения по умолчанию
// (utils.AtomicReplaceFile и time.Now).
func NewPipeline(d PipelineDeps) *Pipeline {
	if d.WriteFile == nil {
		d.WriteFile = utils.AtomicReplaceFile
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Pipeline{d: d}
}

func (v ApplyView) clone() ApplyView {
	out := v
	out.Steps = append([]StepView(nil), v.Steps...)
	out.Restart = append([]RestartView(nil), v.Restart...)
	if v.Result != nil {
		r := *v.Result
		r.OrphansRemoved = append([]string(nil), v.Result.OrphansRemoved...)
		r.Issues = append([]BuildIssue(nil), v.Result.Issues...)
		out.Result = &r
	}
	return out
}

// Current возвращает копию состояния последнего запуска.
func (p *Pipeline) Current() ApplyView {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.view.clone()
}

func (p *Pipeline) begin(req ApplyRequest) {
	p.mu.Lock()
	p.view = ApplyView{
		Running: true,
		Trigger: req.Trigger,
		Steps: []StepView{
			{ID: StepBuild, State: StepPending},
			{ID: StepValidateXray, State: StepPending},
			{ID: StepValidateMihomo, State: StepPending},
			{ID: StepWrite, State: StepPending},
		},
	}
	p.mu.Unlock()
}

// setStep меняет шаг и публикует apply_step.
func (p *Pipeline) setStep(id StepID, state StepState, note, msg string) {
	p.mu.Lock()
	var sv StepView
	for i := range p.view.Steps {
		if p.view.Steps[i].ID == id {
			p.view.Steps[i] = StepView{ID: id, State: state, NoteCode: note, Message: msg}
			sv = p.view.Steps[i]
		}
	}
	p.mu.Unlock()
	p.d.Broker.Publish(Event{Type: EventApplyStep, Data: sv})
}

// finish закрывает запуск итогом и публикует apply_done.
func (p *Pipeline) finish(res ResultView) ApplyView {
	p.mu.Lock()
	p.view.Running = false
	p.view.Result = &res
	out := p.view.clone()
	p.mu.Unlock()
	p.d.Broker.Publish(Event{Type: EventApplyDone, Data: out})
	return out
}

func (p *Pipeline) binaries() Binaries {
	if p.d.Binaries == nil {
		return Binaries{}
	}
	return p.d.Binaries()
}

// Run выполняет один запуск: сборка, проверка Xray, проверка Mihomo (обе до
// первой записи: D-14), запись, коммит манифеста и применённого состояния.
// Файлы пишутся только если оба ядра приняли конфигурацию.
func (p *Pipeline) Run(ctx context.Context, req ApplyRequest) ApplyView {
	p.begin(req)
	snap := p.d.Store.Snapshot()
	src := snap.Draft
	if req.Source == SourceApplied {
		src = snap.Applied
	}
	bins := p.binaries()
	installed := InstalledKernels{Xray: bins.Xray != "", Mihomo: bins.Mihomo != ""}

	p.setStep(StepBuild, StepRunning, "", "")
	files, err := p.d.Registry.Build(src, installed)
	if err == nil {
		err = CheckGenerated(files, p.d.ForeignOwned)
	}
	var plan Plan
	if err == nil {
		only := make(map[string]bool, len(req.Only))
		for _, k := range req.Only {
			only[k] = true
		}
		// Записи манифеста неустановленного ядра не участвуют в плане: иначе при
		// пустой генерации его файлы ушли бы в удаление. Они остаются нетронутыми.
		plan, err = ComputePlan(p.d.Roots, files, manifestOfInstalled(snap.Manifest, installed), only, p.d.ForeignOwned)
		// Сирот убирает 144-06; здесь они в план не входят.
		plan.Orphans = nil
	}
	if err != nil {
		p.setStep(StepBuild, StepFailed, "", err.Error())
		res := ResultView{Code: ResultBuildFailed, Message: err.Error()}
		var be *BuildError
		if errors.As(err, &be) {
			res.Issues = be.Issues
		}
		return p.finish(res)
	}
	p.setStep(StepBuild, StepDone, "", "")

	tmpBase := filepath.Join(p.d.DataDir, "tmp")
	p.setStep(StepValidateXray, StepRunning, "", "")
	xr := ValidateXray(ctx, tmpBase, p.d.Roots, bins.Xray, plan, p.d.XrayEnv)
	if res, failed := p.afterValidation(StepValidateXray, xr); failed {
		return p.finish(res)
	}
	p.setStep(StepValidateMihomo, StepRunning, "", "")
	mr := ValidateMihomo(ctx, tmpBase, p.d.Roots, bins.Mihomo, plan)
	if res, failed := p.afterValidation(StepValidateMihomo, mr); failed {
		return p.finish(res)
	}

	if plan.Empty() {
		p.setStep(StepWrite, StepSkipped, NoteNoChanges, "")
		if err := p.syncApplied(snap, src, req); err != nil {
			return p.finish(ResultView{Code: ResultWriteFailed, Message: err.Error(), RolledBack: true})
		}
		return p.finish(ResultView{OK: true, Code: ResultNothingToApply})
	}

	p.setStep(StepWrite, StepRunning, "", "")
	written, err := p.commit(plan, snap, src, req)
	if err != nil {
		p.setStep(StepWrite, StepFailed, "", err.Error())
		return p.finish(ResultView{Code: ResultWriteFailed, Message: err.Error(), RolledBack: true})
	}
	p.setStep(StepWrite, StepDone, "", "")
	return p.finish(ResultView{OK: true, Code: ResultApplied, Written: written})
}

// manifestOfInstalled оставляет записи манифеста только установленных ядер.
func manifestOfInstalled(m map[string]ManifestEntry, installed InstalledKernels) map[string]ManifestEntry {
	out := make(map[string]ManifestEntry, len(m))
	for k, e := range m {
		if installed.Has(e.Kernel) {
			out[k] = e
		}
	}
	return out
}

// syncApplied фиксирует применённое состояние из снимка источника, когда файлов
// писать не нужно (пустой план). Для пересборки по ключам и из applied ничего
// не меняется; без расхождений файл состояния не переписывается.
func (p *Pipeline) syncApplied(snap State, src Sections, req ApplyRequest) error {
	if req.Source != SourceDraft || len(req.Only) > 0 {
		return nil
	}
	if draftChanges(State{Draft: src, Applied: snap.Applied}) == 0 {
		return nil
	}
	return p.d.Store.Update(func(st *State) error {
		st.Applied = src.Clone()
		return nil
	})
}

// afterValidation отражает итог проверки в шаге; failed=true — применение
// отменено, ResultView готов.
func (p *Pipeline) afterValidation(id StepID, r ValidationResult) (ResultView, bool) {
	switch {
	case r.Skipped:
		p.setStep(id, StepSkipped, r.NoteCode, "")
	case r.OK:
		p.setStep(id, StepDone, "", "")
	default:
		p.setStep(id, StepFailed, "", r.Message)
		return ResultView{Code: r.Code, Kernel: r.Kernel, Message: r.Message, HintCode: r.HintCode}, true
	}
	return ResultView{}, false
}

// preImage — состояние файла до записи, для отката при сбое.
type preImage struct {
	abs     string
	existed bool
	data    []byte
}

// commit пишет файлы плана и фиксирует манифест и применённое состояние. При
// сбое записи или коммита возвращённые на диск прежние содержимое и отсутствие
// файлов восстанавливаются (резервные копии с журналом добавляет 144-06).
func (p *Pipeline) commit(plan Plan, snap State, src Sections, req ApplyRequest) (int, error) {
	var done []preImage
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			pi := done[i]
			if pi.existed {
				_ = utils.AtomicReplaceFile(pi.abs, pi.data)
			} else {
				_ = os.Remove(pi.abs)
			}
		}
	}
	written := 0
	for _, fp := range plan.Files {
		if fp.Action != ActionWrite && fp.Action != ActionDelete {
			continue
		}
		pi := preImage{abs: fp.AbsPath}
		data, err := os.ReadFile(fp.AbsPath)
		switch {
		case err == nil:
			pi.existed, pi.data = true, data
		case !errors.Is(err, os.ErrNotExist):
			rollback()
			return 0, fmt.Errorf("%s: %w", fp.Key, err)
		}
		if fp.Action == ActionDelete {
			if err := os.Remove(fp.AbsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollback()
				return 0, fmt.Errorf("%s: %w", fp.Key, err)
			}
			done = append(done, pi)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fp.AbsPath), 0o755); err != nil {
			rollback()
			return 0, fmt.Errorf("%s: %w", fp.Key, err)
		}
		done = append(done, pi)
		if err := p.d.WriteFile(fp.AbsPath, fp.Content); err != nil {
			rollback()
			return 0, fmt.Errorf("%s: %w", fp.Key, err)
		}
		written++
	}

	now := p.d.Now()
	applyDraft := req.Source == SourceDraft && len(req.Only) == 0
	err := p.d.Store.Update(func(st *State) error {
		for _, fp := range plan.Files {
			switch fp.Action {
			case ActionWrite:
				st.Manifest[fp.Key] = ManifestEntry{
					Kernel: fp.Kernel, RelPath: fp.RelPath, Kind: fp.Kind,
					Hash: fp.NewHash, Status: StatusManaged, WrittenAt: now,
				}
			case ActionDelete:
				delete(st.Manifest, fp.Key)
			}
		}
		if applyDraft {
			st.Applied = src.Clone()
		}
		return nil
	})
	if err != nil {
		rollback()
		return 0, err
	}
	// Файл, переименованный в .obsolete, заменён свежей записью: метка лишняя.
	for _, fp := range plan.Files {
		if fp.Action == ActionWrite && fp.RemoveObsolete {
			_ = os.Remove(fp.AbsPath + obsoleteSuffix)
		}
	}
	return written, nil
}
