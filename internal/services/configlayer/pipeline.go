package configlayer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
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
	// Kernel ограничивает запуск одним ядром (фоновая сборка после его установки):
	// файлы, записи манифеста и сироты других ядер не участвуют в плане и не
	// трогаются. Пусто — все установленные ядра.
	Kernel string
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

// RestartView — итог шага «Перезапуск» по одному ядру.
type RestartView struct {
	Kernel   string `json:"kernel"`
	Outcome  string `json:"outcome"`
	NoteCode string `json:"note_code,omitempty"`
}

// Коды итога запуска. Код отказа не говорит, что откат удался: за это отвечает
// RolledBack, а неудачный откат и ядро, не поднявшееся после отката, имеют свои коды.
const (
	ResultApplied        = "applied"
	ResultNothingToApply = "nothing_to_apply"
	ResultBuildFailed    = "build_failed"
	// ResultWriteFailed — запись файлов не удалась; прежние файлы возвращены.
	ResultWriteFailed = "write_failed"
	// ResultRestartFailed — ядро не поднялось на новых файлах; прежние файлы
	// возвращены, ядро снова работает на них.
	ResultRestartFailed = "restart_failed"
	// ResultKernelNotRecovered — прежние файлы возвращены, но ядро на них не поднялось.
	ResultKernelNotRecovered = "kernel_not_recovered"
	// ResultRollbackFailed — вернуть прежние файлы не удалось: журнал остался,
	// откат повторится при следующем запуске панели.
	ResultRollbackFailed = "rollback_failed"
	ResultDriftBlocked   = "drift_blocked"
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

	// Шаг перезапуска (144-07). Applier == nil — шаг пропускается.
	Applier        KernelApplier
	ProcessStates  func() []services.KernelProcessState
	Mihomo         MihomoControl
	MihomoAPIReady func() bool
	Lifecycle      LifecycleLocker
}

// Pipeline — конвейер «Применить»: сборка, проверка Xray, проверка Mihomo,
// запись, перезапуск, коммит манифеста. Run вызывается под замками вызывающего
// (TryBegin): applyMu, затем замок жизненного цикла.
type Pipeline struct {
	d PipelineDeps
	// applyMu — «одно применение за раз»; берётся в TryBegin раньше замка жизненного цикла.
	applyMu sync.Mutex
	mu      sync.Mutex
	view    ApplyView
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
			{ID: StepRestart, State: StepPending},
		},
	}
	p.mu.Unlock()
}

// setStep меняет шаг и публикует apply_step. Вне запуска (Running=false) шаги не
// меняются и не публикуются: вызов из выключения слоя иначе открыл бы в UI
// «фантомное» применение без apply_done и переписал итог прошлого запуска.
func (p *Pipeline) setStep(id StepID, state StepState, note, msg string) {
	p.mu.Lock()
	if !p.view.Running {
		p.mu.Unlock()
		return
	}
	var sv StepView
	for i := range p.view.Steps {
		if p.view.Steps[i].ID == id {
			p.view.Steps[i] = StepView{ID: id, State: state, NoteCode: note, Message: msg}
			sv = p.view.Steps[i]
		}
	}
	p.mu.Unlock()
	if sv.ID == "" {
		return
	}
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
// первой записи: D-14), запись, перезапуск ядер, коммит манифеста и применённого состояния.
// Файлы пишутся только если оба ядра приняли конфигурацию.
func (p *Pipeline) Run(ctx context.Context, req ApplyRequest) ApplyView {
	p.begin(req)
	// Журнал прошлой неудавшейся записи или отката нельзя перезаписать новым
	// набором: сначала возвращаем файлы по нему (WR-03).
	if err := p.recoverPendingJournal(); err != nil {
		p.setStep(StepBuild, StepFailed, "", err.Error())
		return p.finish(ResultView{Code: ResultRollbackFailed, Message: err.Error()})
	}
	snap := p.d.Store.Snapshot()
	src := snap.Draft
	if req.Source == SourceApplied {
		src = snap.Applied
	}
	bins := p.binaries()
	installed := InstalledKernels{Xray: bins.Xray != "", Mihomo: bins.Mihomo != ""}

	p.setStep(StepBuild, StepRunning, "", "")
	// Дрейф проверяется здесь, под замками применения, а не только в StartApply:
	// фоновые запуски (установка ядра, включение слоя) идут мимо кнопки и иначе
	// молча перезаписали бы ручную правку (D-11). «Пересобрать» дрейф разрешает сам.
	manifest := manifestOfScope(snap.Manifest, installed, req.Kernel)
	if req.Trigger != TriggerRebuild {
		if drift := driftKeys(p.d.Roots, manifest); len(drift) > 0 {
			p.setStep(StepBuild, StepFailed, "", ErrDriftBlocked.Error())
			return p.finish(ResultView{Code: ResultDriftBlocked, Message: ErrDriftBlocked.Error()})
		}
	}
	files, err := p.d.Registry.Build(src, installed)
	if err == nil {
		files = filesOfScope(files, req.Kernel)
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
		plan, err = ComputePlan(p.d.Roots, files, manifest, only, p.d.ForeignOwned)
		// Каталог неустановленного ядра (и ядра вне области запуска) так же не
		// трогается: его сирот не убираем.
		plan.Orphans = orphansOfScope(plan.Orphans, installed, req.Kernel)
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
		p.setStep(StepRestart, StepSkipped, NoteNoChanges, "")
		if err := p.syncApplied(snap, src, req); err != nil {
			return p.finish(ResultView{Code: ResultWriteFailed, Message: err.Error(), RolledBack: true})
		}
		return p.finish(ResultView{OK: true, Code: ResultNothingToApply})
	}

	p.setStep(StepWrite, StepRunning, "", "")
	out, err := p.writePlan(plan, req.Trigger)
	if err != nil {
		p.setStep(StepWrite, StepFailed, "", err.Error())
		if out.RollbackErr != nil {
			return p.finish(ResultView{Code: ResultRollbackFailed, Message: err.Error()})
		}
		return p.finish(ResultView{Code: ResultWriteFailed, Message: err.Error(), RolledBack: true})
	}
	p.setStep(StepWrite, StepDone, "", "")

	// Манифест и применённое состояние фиксируются только после перезапуска (D-16):
	// до этого прежнее состояние восстановимо из набора копий.
	views, err := p.runRestart(ctx, plan, out.Set)
	if err != nil {
		res := ResultView{Code: ResultRestartFailed, Message: err.Error()}
		var re *restartError
		if errors.As(err, &re) {
			res.Kernel, res.RolledBack = re.Kernel, re.RolledBack
			switch {
			case !re.RolledBack:
				res.Code = ResultRollbackFailed
			case !re.Recovered:
				res.Code = ResultKernelNotRecovered
			}
		}
		return p.finish(res)
	}
	if err := p.commitState(plan, src, req); err != nil {
		// Ядра уже перезапущены на новых файлах: вернуть одни файлы мало, ядра
		// нужно поднять на прежних (WR-04).
		return p.finish(p.rollbackAfterCommitFailure(ctx, out.Set, views, err))
	}
	// Ротация копий — после успешного применения; сбой уборки применение не отменяет.
	_ = PruneBackups(p.d.DataDir, BackupRetention)
	return p.finish(ResultView{OK: true, Code: ResultApplied, Written: out.Written, OrphansRemoved: out.OrphansRemoved})
}

// rollbackAfterCommitFailure откатывает файлы после сбоя фиксации состояния и
// перезапускает на прежних файлах ядра, которые к этому моменту уже работали на
// новых; итог отражает, что из этого удалось.
func (p *Pipeline) rollbackAfterCommitFailure(ctx context.Context, set *BackupSet, views []RestartView, cause error) ResultView {
	p.setStep(StepWrite, StepFailed, "", cause.Error())
	if rbErr := p.rollback(set); rbErr != nil {
		return ResultView{Code: ResultRollbackFailed, Message: fmt.Sprintf("%v; откат файлов не удался: %v", cause, rbErr)}
	}
	res := ResultView{Code: ResultWriteFailed, Message: cause.Error(), RolledBack: true}
	for _, v := range views {
		switch v.Outcome {
		case RestartOutcomeRestarted, RestartOutcomeHotReloaded, RestartOutcomeRestartedReload:
		default:
			continue
		}
		if rerr := p.recoverKernel(ctx, v.Kernel); rerr != nil {
			res.Code, res.Kernel = ResultKernelNotRecovered, v.Kernel
			res.Message = fmt.Sprintf("%v; повторный рестарт %s на прежних файлах: %v", cause, v.Kernel, rerr)
		}
	}
	return res
}

// recoverPendingJournal возвращает файлы по журналу, оставшемуся от прерванной
// записи или неудавшегося отката (под замками применения). Журнала нет — ничего
// не делает. Журнал очищается всегда (см. RecoverJournal), поэтому ошибку
// возвращённых не до конца файлов видит вызывающий, а не следующий запуск.
func (p *Pipeline) recoverPendingJournal() error {
	if p.d.Store.Snapshot().Journal == nil {
		return nil
	}
	if _, err := RecoverJournal(p.d.Store, p.d.Roots); err != nil {
		return err
	}
	return nil
}

// kernelInScope — ядро входит в область запуска: установлено и совпадает с
// ограничением only (пусто — без ограничения).
func kernelInScope(kernel string, installed InstalledKernels, only string) bool {
	return installed.Has(kernel) && (only == "" || kernel == only)
}

// manifestOfScope оставляет записи манифеста только ядер из области запуска.
func manifestOfScope(m map[string]ManifestEntry, installed InstalledKernels, only string) map[string]ManifestEntry {
	out := make(map[string]ManifestEntry, len(m))
	for k, e := range m {
		if kernelInScope(e.Kernel, installed, only) {
			out[k] = e
		}
	}
	return out
}

// orphansOfScope оставляет сирот только ядер из области запуска.
func orphansOfScope(orphans []OrphanFile, installed InstalledKernels, only string) []OrphanFile {
	var out []OrphanFile
	for _, o := range orphans {
		if kernelInScope(o.Kernel, installed, only) {
			out = append(out, o)
		}
	}
	return out
}

// filesOfScope оставляет сгенерированные файлы только ядра only (пусто — все).
func filesOfScope(files []GeneratedFile, only string) []GeneratedFile {
	if only == "" {
		return files
	}
	out := make([]GeneratedFile, 0, len(files))
	for _, f := range files {
		if f.Kernel == only {
			out = append(out, f)
		}
	}
	return out
}

// driftKeys — ключи записей манифеста с расхождением (диск не совпадает с тем,
// что записала панель).
func driftKeys(roots Roots, manifest map[string]ManifestEntry) []string {
	var out []string
	for _, c := range CheckManifest(roots, manifest) {
		if c.State.IsDrift() {
			out = append(out, c.Key)
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

// commitState фиксирует манифест и применённое состояние в одной записи файла
// состояния и очищает журнал записи; вызывается после шага перезапуска.
func (p *Pipeline) commitState(plan Plan, src Sections, req ApplyRequest) error {
	now := p.d.Now()
	applyDraft := req.Source == SourceDraft && len(req.Only) == 0
	return p.d.Store.Update(func(st *State) error {
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
		st.Journal = nil
		return nil
	})
}
