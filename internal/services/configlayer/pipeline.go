package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
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
	// TriggerSourceRefresh — фоновое обновление источника: сборка из применённого
	// состояния с оверлеем живых данных, черновик не трогается (FND-06, U-04).
	TriggerSourceRefresh Trigger = "source_refresh"
	// TriggerSelect — выбор участника ручной группы: сборка из применённого
	// состояния с оверлеем живых данных, черновик не трогается.
	TriggerSelect Trigger = "select"
	// TriggerDatRefresh — пересборка списков после обновления dat-файлов.
	TriggerDatRefresh Trigger = "dat_refresh"
	// TriggerRoutingReclaim — «вернуть» маршрутизацию: профиль панели Mihomo или
	// чужой файл routing Xray в копию (набор копий вне конвейера «Применить»).
	TriggerRoutingReclaim Trigger = "routing_reclaim"
	// Переключатель access-лога: запуск поверх применённого состояния с подменой
	// секции (SectionsOverlay), чужие правки черновика не применяются.
	TriggerAccessLog Trigger = "access_log"
	// TriggerDNSTakeover — транзакция перехвата DNS (включение, перенос, возврат):
	// запуск поверх применённого состояния с подменой секций и фиксацией живых
	// данных перехвата, черновик не трогается.
	TriggerDNSTakeover Trigger = "dns_takeover"
	// TriggerDNSRestore — откат перехвата DNS к снимку (RunRestore): применённое
	// состояние и живые данные перехвата возвращаются к значениям до транзакции.
	TriggerDNSRestore Trigger = "dns_restore"
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
	// ApplySeq — номер запуска «Применить» из хуков BeforeApply (0 — хуки не
	// вызывались); возвращается хукам AfterApply в AfterApplyInfo.
	ApplySeq int64
	// ExpectRevision — ревизия черновика, которую видел клиент в окне изменений
	// (D-08): сверяется с тем снимком, из которого строится план, и при расхождении
	// запуск отменяется (ResultDraftConflict) до сборки. 0 — не сверять.
	ExpectRevision int64
	// draftSnapshot — подтверждённый снимок состояния запуска «Применить»
	// (StartApplyAt): глубокая копия, которой владеет только этот запуск. Его
	// черновик и ревизия — источник сверки ExpectRevision, сборки плана и фиксации
	// Applied (D-08, D-09); правка черновика после подтверждения в запуск не
	// попадает и остаётся в общем черновике. Манифест, маршрутизация, Applied и
	// живые данные читаются заново под замком применения (после возврата журнала).
	// nil — черновик берётся из текущего состояния.
	draftSnapshot *State
	// Kernel ограничивает запуск одним ядром (фоновая сборка после его установки):
	// файлы, записи манифеста и сироты других ядер не участвуют в плане и не
	// трогаются. Пусто — все установленные ядра.
	Kernel string
	// LiveOverlay — ключи, которые заменяют одноимённые ключи State.Live во входе
	// генераторов этого запуска (кандидат фонового обновления источника). Сам State.Live
	// оверлей не меняет: живые данные фиксируют хуки PipelineDeps.CommitLive.
	LiveOverlay Sections
	// SectionsOverlay — секции, которые заменяют одноимённые в применённом
	// состоянии, из которого идёт запуск (D-13): только при Source == SourceApplied,
	// для SourceDraft поле игнорируется. После успешной проверки, записи и
	// перезапуска (и при пустом плане) значения фиксируются в Applied одной записью
	// с манифестом; в Draft — только через MergeDraft.
	SectionsOverlay Sections
	// MergeDraft — слияние значения подменённой секции в черновик: получает имя
	// секции и её значение в черновике (nil — секции там нет), возвращает новое.
	// Вызывается внутри той же записи состояния; изменение черновика — одна
	// ревизия на запуск. Ошибка отменяет всю запись (файлы возвращаются). nil —
	// черновик не меняется.
	MergeDraft func(name string, draft json.RawMessage) (json.RawMessage, error)
	// Restore — откат уже зафиксированного применения (RunRestore): только при
	// Source == SourceApplied. Файлы собираются из Restore.Applied целиком (не
	// поверх текущего Applied), SectionsOverlay при этом не действует. После
	// успеха (и при пустом плане) одной записью с манифестом Applied становится
	// Restore.Applied, ключи Restore.Live фиксируются в State.Live, черновик не
	// меняется, кроме секций Restore.ResetDraftSections без правки пользователя.
	Restore *RestoreSpec
	// LiveCommit — ключи живых данных, которые подставляются генераторам этого
	// запуска (как LiveOverlay) и после успеха (и при пустом плане) фиксируются в
	// State.Live той же записью, что манифест и Applied; JSON null удаляет ключ.
	// Отказ запуска State.Live не меняет.
	LiveCommit Sections
	// ForceFullRestart — запущенное ядро перезапускается целиком, без горячей
	// перезагрузки Mihomo (правила XKeen пересобираются только полным перезапуском).
	ForceFullRestart bool
	// BeforeWrite вызывается после проверки конфигурации обоими ядрами и до
	// записи файлов, в том числе при пустом плане. Ошибка отменяет запуск без
	// касания файлов (ResultBeforeWriteFailed). nil — хука нет.
	BeforeWrite func(ctx context.Context) error
	// BeforeRollback вызывается один раз в начале отката после того, как
	// BeforeWrite уже отработал: при сбое записи, перезапуска (до повторного подъёма
	// ядра на прежних файлах) и фиксации состояния. Паника хука откат не прерывает.
	// nil — хука нет.
	BeforeRollback func()
	// ReleaseListener — запуск снимает слушатель :53 перехвата DNS (живые данные
	// перехвата выключаются, DNS роутера уже у прошивки). Ручная правка файлов
	// панели такой запуск не блокирует: пишутся только файлы, чьё содержимое
	// меняют живые данные перехвата, остальные файлы с дрейфом не перезаписываются
	// (D-11). Иначе слушатель остался бы в файлах ядра при DNS у прошивки, и следующий
	// запуск ядра упёрся бы в занятый :53. Не действует при TriggerRebuild.
	ReleaseListener bool
}

// RestoreSpec — снимок, к которому возвращается применённое состояние.
type RestoreSpec struct {
	// Applied — применённое состояние снимка целиком: секций, которых в нём нет,
	// после отката в Applied не будет.
	Applied Sections
	// Live — ключи живых данных снимка: значение заменяет текущее, JSON null
	// удаляет ключ. Ключи вне карты не меняются.
	Live Sections
	// ResetDraftSections — секции черновика, которые возвращаются к снимку, если
	// пользователь их не правил (черновик совпадает с текущим Applied); правка
	// пользователя остаётся (D-27).
	ResetDraftSections []string
}

// overlayActive — у запуска есть подмена секций, которая действует: только
// поверх применённого состояния и не при откате к снимку.
func (r ApplyRequest) overlayActive() bool {
	return r.Source == SourceApplied && r.Restore == nil && len(r.SectionsOverlay) > 0
}

// restoreActive — запуск возвращает применённое состояние к снимку.
func (r ApplyRequest) restoreActive() bool {
	return r.Source == SourceApplied && r.Restore != nil
}

// runLive — живые данные, из которых собираются файлы запуска: State.Live с
// оверлеем кандидата, ключами снимка отката и ключами LiveCommit. Ключ со
// значением JSON null во входе генераторов отсутствует, как и после фиксации.
func (r ApplyRequest) runLive(live Sections) Sections {
	out := liveWithOverlay(live, r.LiveOverlay)
	if r.restoreActive() {
		overlayLive(out, r.Restore.Live)
	}
	overlayLive(out, r.LiveCommit)
	return out
}

// commitLiveKeys фиксирует ключи LiveCommit запуска в st.Live (внутри Store.Update):
// значение заменяет текущее, JSON null удаляет ключ.
func commitLiveKeys(st *State, keys Sections) error {
	if len(keys) == 0 {
		return nil
	}
	if st.Live == nil {
		st.Live = Sections{}
	}
	names := sortedSectionNames(keys)
	for _, k := range names {
		old, had := st.Live[k]
		if _, err := settleValue(st.Live, k, cloneRaw(keys[k]), old, had); err != nil {
			return err
		}
	}
	return nil
}

// sortedSectionNames — отсортированные имена ключей карты секций.
func sortedSectionNames(s Sections) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// overlayLive кладёт ключи src в dst: JSON null (и пустое значение) удаляет ключ.
func overlayLive(dst, src Sections) {
	for k, v := range src {
		if isNullRaw(v) {
			delete(dst, k)
			continue
		}
		dst[k] = cloneRaw(v)
	}
}

// isNullRaw — значение пустое или JSON null.
func isNullRaw(v json.RawMessage) bool {
	return len(bytes.TrimSpace(v)) == 0 || bytes.Equal(bytes.TrimSpace(v), []byte("null"))
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
	// Trigger — что запустило конвейер: по нему клиент отличает фоновое обновление
	// источника (source_refresh) от «Применить».
	Trigger Trigger `json:"trigger,omitempty"`
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
	// ResultInterrupted — запуск прерван остановкой панели; журнал остался, файлы
	// вернёт RecoverJournal при следующем старте.
	ResultInterrupted = "interrupted"
	// ResultRollbackFailed — вернуть прежние файлы не удалось: журнал остался,
	// откат повторится при следующем запуске панели.
	ResultRollbackFailed = "rollback_failed"
	// ResultJournalRecoveryFailed — журнал прошлой прерванной записи вернуть до
	// конца не удалось. Журнал уже снят (RecoverJournal снимает его всегда), поэтому
	// повторного возврата не будет: файлы нужно проверить вручную.
	ResultJournalRecoveryFailed = "journal_recovery_failed"
	// ResultStateWriteFailed — применять было нечего, но файл состояния не записался;
	// файлы на диске не менялись, возвращать нечего.
	ResultStateWriteFailed = "state_write_failed"
	ResultDriftBlocked     = "drift_blocked"
	// ResultDraftConflict — черновик изменился после того, как клиент показал окно
	// изменений: применение отменено до сборки, файлы не тронуты.
	ResultDraftConflict = "draft_conflict"
	// ResultBeforeWriteFailed — хук ApplyRequest.BeforeWrite вернул ошибку после
	// проверки ядрами: запуск отменён, файлы не тронуты.
	ResultBeforeWriteFailed = "before_write_failed"
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
	// Enabled — включён ли слой. Run перепроверяет его под замками применения:
	// фоновый запуск мог ждать замок, пока Disable снимал флаг. nil — всегда включён.
	Enabled func() bool

	// Шаг перезапуска (144-07). Applier == nil — шаг пропускается.
	Applier        KernelApplier
	ProcessStates  func() []services.KernelProcessState
	Mihomo         MihomoControl
	MihomoAPIReady func() bool
	Lifecycle      LifecycleLocker

	// CommitLive — хуки фиксации живых данных. Вызываются по порядку регистрации
	// внутри той же записи файла состояния, что и манифест, после успешной записи
	// файлов и перезапуска, а также когда план пуст; used — вход, из которого
	// собраны файлы этого запуска. Ошибка хука отменяет запись состояния (файлы
	// возвращаются), следующие хуки не вызываются. При отказе сборки, проверки,
	// записи, перезапуска и при дрейфе хуки не вызываются. Хук получает req: запуск,
	// ограниченный одним ядром (req.Kernel), собрал файлы только этого ядра.
	// Пусто — хуков нет.
	CommitLive []LiveCommitFunc
}

// LiveCommitFunc — хук фиксации живых данных: вызывается внутри записи состояния.
type LiveCommitFunc func(st *State, req ApplyRequest, used Input) error

// Pipeline — конвейер «Применить»: сборка, проверка Xray, проверка Mihomo,
// запись, перезапуск, коммит манифеста. Run вызывается под замками вызывающего
// (TryBegin): applyMu, затем замок жизненного цикла.
type Pipeline struct {
	d PipelineDeps
	// applyMu — «одно применение за раз»; берётся в TryBegin раньше замка жизненного цикла.
	applyMu sync.Mutex
	// tail — итог запуска опубликован (finish), а замок применения держится до конца
	// хвоста запуска; сбрасывает тот, кто взял замок следующим (TryBegin).
	tail atomic.Bool
	mu   sync.Mutex
	view ApplyView
	// commitLive — хуки фиксации живых данных (PipelineDeps.CommitLive) в порядке
	// регистрации, под mu: слой добавляет их после создания конвейера.
	commitLive []LiveCommitFunc
	// beforeRollback — хук ApplyRequest.BeforeRollback текущего запуска, под mu:
	// взводится в begin, снимается первым вызовом fireBeforeRollback и в finish.
	beforeRollback func()
}

// fireBeforeRollback вызывает хук BeforeRollback текущего запуска не больше одного
// раза; паника хука журналируется и откат не прерывает.
func (p *Pipeline) fireBeforeRollback() {
	p.mu.Lock()
	fn := p.beforeRollback
	p.beforeRollback = nil
	p.mu.Unlock()
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("configlayer: хук до отката: паника: %v", r)
		}
	}()
	fn()
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
	return &Pipeline{d: d, commitLive: append([]LiveCommitFunc(nil), d.CommitLive...)}
}

// addCommitLive добавляет хук фиксации живых данных в конец списка (до Start слоя).
func (p *Pipeline) addCommitLive(fn LiveCommitFunc) {
	p.mu.Lock()
	p.commitLive = append(p.commitLive, fn)
	p.mu.Unlock()
}

// liveCommitter возвращает один хук, который вызывает все зарегистрированные по
// порядку и останавливается на первой ошибке; nil — хуков нет.
func (p *Pipeline) liveCommitter() LiveCommitFunc {
	p.mu.Lock()
	hooks := append([]LiveCommitFunc(nil), p.commitLive...)
	p.mu.Unlock()
	if len(hooks) == 0 {
		return nil
	}
	return func(st *State, req ApplyRequest, used Input) error {
		for _, h := range hooks {
			if err := h(st, req, used); err != nil {
				return err
			}
		}
		return nil
	}
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
	p.beforeRollback = req.BeforeRollback
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
			p.view.Steps[i] = StepView{ID: id, State: state, NoteCode: note, Message: msg, Trigger: p.view.Trigger}
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
	p.beforeRollback = nil
	out := p.view.clone()
	p.mu.Unlock()
	p.tail.Store(true)
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
	// Решение «слой включён» принимается под замками, которые держит и Disable:
	// запуск, дождавшийся замка после выключения, файлов не пишет (D-04, FND-01).
	if p.d.Enabled != nil && !p.d.Enabled() {
		p.setStep(StepBuild, StepSkipped, NoteNoChanges, "")
		return p.finish(ResultView{OK: true, Code: ResultNothingToApply})
	}
	// Журнал прошлой неудавшейся записи или отката нельзя перезаписать новым
	// набором: сначала возвращаем файлы по нему (WR-03).
	if err := p.recoverPendingJournal(); err != nil {
		p.setStep(StepBuild, StepFailed, "", err.Error())
		return p.finish(ResultView{Code: ResultJournalRecoveryFailed, Message: err.Error()})
	}
	snap := p.d.Store.Snapshot()
	// Подтверждённый запуск собирает и фиксирует черновик своего снимка, а не
	// свежее чтение: правка после подтверждения не применится незаметно (D-08,
	// D-09) и останется в черновике (Applied получает только src, Draft не трогается).
	if req.draftSnapshot != nil {
		snap.Draft, snap.DraftRevision = req.draftSnapshot.Draft, req.draftSnapshot.DraftRevision
	}
	// Ревизия, которую показало окно изменений, сверяется с тем же снимком, из
	// которого строится план (D-08).
	if req.ExpectRevision > 0 && snap.DraftRevision != req.ExpectRevision {
		p.setStep(StepBuild, StepFailed, "", ErrDraftConflict.Error())
		return p.finish(ResultView{Code: ResultDraftConflict, Message: ErrDraftConflict.Error()})
	}
	bins := p.binaries()
	installed := InstalledKernels{Xray: bins.Xray != "", Mihomo: bins.Mihomo != ""}

	p.setStep(StepBuild, StepRunning, "", "")
	// План собирает та же функция, что и предпросмотр «Применить» (D-09): показанное
	// в окне изменений совпадает с тем, что запишется.
	plan, used, err := p.buildPlan(snap, &req, installed)
	src := used.Sections
	if errors.Is(err, ErrDriftBlocked) {
		p.setStep(StepBuild, StepFailed, "", ErrDriftBlocked.Error())
		return p.finish(ResultView{Code: ResultDriftBlocked, Message: ErrDriftBlocked.Error()})
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

	// Хук «до записи» (порядок D-01: проверка ядрами → действие вызывающего →
	// запись): отказ проверки до него не доходит, его ошибка файлов не трогает.
	if req.BeforeWrite != nil {
		if err := req.BeforeWrite(ctx); err != nil {
			p.setStep(StepWrite, StepFailed, "", err.Error())
			return p.finish(ResultView{Code: ResultBeforeWriteFailed, Message: err.Error()})
		}
	}

	if plan.Empty() {
		p.setStep(StepWrite, StepSkipped, NoteNoChanges, "")
		p.setStep(StepRestart, StepSkipped, NoteNoChanges, "")
		if err := p.syncApplied(snap, src, req, used); err != nil {
			// Файлы не менялись, но BeforeWrite уже отработал: его действие
			// возвращает хук отката.
			p.fireBeforeRollback()
			return p.finish(ResultView{Code: ResultStateWriteFailed, Message: err.Error()})
		}
		return p.finish(ResultView{OK: true, Code: ResultNothingToApply})
	}

	p.setStep(StepWrite, StepRunning, "", "")
	out, err := p.writePlan(plan, req.Trigger)
	if err != nil {
		// Ядро не перезапускалось, файлы writePlan уже вернул; действие BeforeWrite
		// возвращает хук отката.
		p.fireBeforeRollback()
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
		if errors.Is(err, errInterrupted) {
			return p.finish(ResultView{Code: ResultInterrupted, Message: err.Error()})
		}
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
	if err := p.commitState(plan, src, req, used); err != nil {
		// Ядра уже перезапущены на новых файлах: вернуть одни файлы мало, ядра
		// нужно поднять на прежних (WR-04).
		return p.finish(p.rollbackAfterCommitFailure(ctx, out.Set, views, err))
	}
	// Ротация копий — после успешного применения; сбой уборки применение не отменяет.
	kept := p.d.Store.Snapshot()
	_ = PruneBackups(p.d.DataDir, BackupRetention, append(kept.Routing.backupDirs(), kept.Cores.backupDirs()...)...)
	return p.finish(ResultView{OK: true, Code: ResultApplied, Written: out.Written, OrphansRemoved: out.OrphansRemoved})
}

// driftBlockedError — сборка плана остановлена дрейфом: Is совпадает с
// ErrDriftBlocked, count — число расхождений, по которым запуск заблокирован.
type driftBlockedError struct {
	count int
}

func (e *driftBlockedError) Error() string { return ErrDriftBlocked.Error() }

// Is связывает driftBlockedError с сентинелом ErrDriftBlocked.
func (e *driftBlockedError) Is(target error) bool { return target == ErrDriftBlocked }

// runSections — секции, из которых собираются файлы запуска: черновик или
// применённое состояние (с подменой секций или снимком отката).
func runSections(snap State, req ApplyRequest) Sections {
	if req.Source != SourceApplied {
		return snap.Draft
	}
	switch {
	case req.restoreActive():
		// Откат к снимку: файлы собираются из прежнего Applied целиком, секции,
		// которых в нём не было, во вход не попадают.
		return req.Restore.Applied.Clone()
	case req.overlayActive():
		return liveWithOverlay(snap.Applied, req.SectionsOverlay)
	}
	return snap.Applied
}

// buildPlan собирает план запуска из снимка состояния: проверка дрейфа, сборка
// реестром генераторов, проверки до записи, расчёт плана, операции взятия и возврата,
// признак полного перезапуска. Диск не меняется и шаги не публикуются: этим путём
// идут и Run, и предпросмотр «Применить» (Layer.PreviewApply), поэтому окно
// изменений показывает ровно то, что запишется (D-09). Снятие слушателя при дрейфе
// сужает req.Only до файлов перехвата. Возвращает план и вход генераторов
// (used.Sections — секции запуска). Дрейф — *driftBlockedError (Is ErrDriftBlocked),
// проблемы сборки и проверок — *BuildError.
func (p *Pipeline) buildPlan(snap State, req *ApplyRequest, installed InstalledKernels) (Plan, Input, error) {
	src := runSections(snap, *req)
	// Дрейф проверяется здесь, под замками применения, а не только в StartApply:
	// фоновые запуски (установка ядра, включение слоя) идут мимо кнопки и иначе
	// молча перезаписали бы ручную правку (D-11). «Пересобрать» дрейф разрешает сам.
	manifest := manifestOfScope(snap.Manifest, installed, req.Kernel)
	listenerOnly := false
	if req.Trigger != TriggerRebuild {
		drift := driftKeys
		if req.Trigger == TriggerKernelInstalled {
			// Переустановленное ядро с пропавшими файлами: перезаписывать нечего,
			// ручной правки там нет, поэтому сборка их восстанавливает (D-18).
			drift = driftKeysExceptMissing
		}
		blocked := len(drift(p.d.Roots, manifest))
		// Дрейф маршрутизации (D-06, D-07) блокирует запуск, который держит
		// маршрутизацию этого ядра; запуск, отпускающий ядро, — это и есть решение
		// «Отпустить», дрейфом этого ядра он не блокируется (по ядрам, D-06 из 147.1).
		if blocked == 0 {
			if rd := routingDrift(p.d.Roots, snap.Routing, installed, req.Kernel, p.d.ForeignOwned); routingDriftBlocks(src, rd) {
				blocked = len(rd)
			}
		}
		// Чужой файл с ключом взятой секции блокирует запуск, который эту секцию
		// держит (managed); запуск с managed=false — «Отпустить» — не блокируется.
		if blocked == 0 {
			if sd := sectionsDrift(p.d.Roots, snap.Cores, installed, req.Kernel, p.d.ForeignOwned); sectionDriftBlocks(src, sd) {
				blocked = len(sd)
			}
		}
		switch {
		case blocked > 0 && req.ReleaseListener:
			// Снятие слушателя :53 не ждёт, пока пользователь разрешит дрейф: план
			// сужается до файлов, затронутых живыми данными перехвата.
			listenerOnly = true
		case blocked > 0:
			return Plan{}, Input{Sections: src}, &driftBlockedError{count: blocked}
		}
	}
	used := Input{Sections: src, Live: req.runLive(snap.Live), Mode: req.Source, Routing: routingFacts(p.d.Roots, snap.Routing)}
	files, err := p.d.Registry.Build(used, installed)
	if err == nil {
		files = filesOfScope(files, req.Kernel)
		err = CheckGenerated(files, p.d.ForeignOwned)
	}
	if err == nil {
		// Проверки до записи (пустая группа, висячая ссылка): при любом триггере,
		// в том числе фоновом, иначе обновление источника обошло бы блокировку
		// «Применить». Непустой итог — отказ сборки без записи на диск.
		if issues := p.d.Registry.Check(used, installed); len(issues) > 0 {
			err = &BuildError{Issues: issues}
		}
	}
	var plan Plan
	if err == nil {
		only := make(map[string]bool, len(req.Only))
		for _, k := range req.Only {
			only[k] = true
		}
		skipPlan := false
		if listenerOnly {
			// Только файлы, затронутые живыми данными перехвата; их нет — писать нечего.
			var keys []string
			if keys, err = p.listenerFileKeys(used, snap, installed, *req, files); err == nil {
				for _, k := range keys {
					only[k] = true
				}
				skipPlan = len(keys) == 0
				req.Only = keys
			}
		}
		if err == nil && !skipPlan {
			// Записи манифеста неустановленного ядра не участвуют в плане: иначе при
			// пустой генерации его файлы ушли бы в удаление. Они остаются нетронутыми.
			plan, err = ComputePlan(p.d.Roots, files, manifest, only, p.d.ForeignOwned)
			// Каталог неустановленного ядра (и ядра вне области запуска) так же не
			// трогается: его сирот не убираем.
			plan.Orphans = orphansOfScope(plan.Orphans, installed, req.Kernel)
		}
	}
	if err == nil && !listenerOnly {
		// Взятие и возврат маршрутизации — часть этого же плана: проверка ядрами
		// идёт на итоговой раскладке, запись и откат общие.
		plan.Routing, err = p.planRouting(src, snap.Routing, installed, *req)
	}
	if err == nil && !listenerOnly {
		// Секции Xray (взятие и возврат файлов, D-01) идут теми же операциями.
		plan.Routing, err = p.planCores(src, snap.Cores, plan.Routing, installed, *req)
	}
	if err == nil && req.Source == SourceDraft && len(req.Only) == 0 {
		// Смена порта прозрачного входящего или tun — полный перезапуск вместо
		// горячей перезагрузки Mihomo (D-16): правила XKeen пересобираются только им.
		// Тот же расчёт, что и у объявления в снимке; на переходе владения
		// сравнивается значение по ту сторону перехода (G-147-3).
		base := restartBase(p.d.Roots, snap, src, installed, p.d.ForeignOwned)
		plan.FullRestart = fullRestartOf(RestartReasons(snap.Applied, src, base))
	}
	if err == nil && req.ForceFullRestart {
		if plan.FullRestart == nil {
			plan.FullRestart = map[string]string{}
		}
		plan.FullRestart[KernelMihomo] = RestartReasonFirmwareDNS
	}
	return plan, used, err
}

// listenerFileKeys — ключи файлов, чьё содержимое в этом запуске меняют живые данные
// перехвата: файлы, собранные из тех же секций с прежними живыми данными, отличаются
// от файлов запуска (или есть только с одной стороны). Файлы, которых слушатель не
// касается, сюда не попадают, поэтому ручная правка в них не перезаписывается.
func (p *Pipeline) listenerFileKeys(used Input, snap State, installed InstalledKernels, req ApplyRequest, files []GeneratedFile) ([]string, error) {
	before := used
	before.Live = snap.Live
	prev, err := p.d.Registry.Build(before, installed)
	if err != nil {
		return nil, err
	}
	prev = filesOfScope(prev, req.Kernel)
	old := make(map[string][]byte, len(prev))
	for _, f := range prev {
		old[f.Key()] = f.Content
	}
	seen := make(map[string]bool, len(files))
	var keys []string
	for _, f := range files {
		key := f.Key()
		seen[key] = true
		if prevContent, ok := old[key]; !ok || !bytes.Equal(prevContent, f.Content) {
			keys = append(keys, key)
		}
	}
	for key := range old {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// rollbackAfterCommitFailure откатывает файлы после сбоя фиксации состояния и
// перезапускает на прежних файлах ядра, которые к этому моменту уже работали на
// новых; итог отражает, что из этого удалось.
func (p *Pipeline) rollbackAfterCommitFailure(ctx context.Context, set *BackupSet, views []RestartView, cause error) ResultView {
	// Хук до отката — первым, раньше отката файлов и повторного подъёма ядер.
	p.fireBeforeRollback()
	p.setStep(StepWrite, StepFailed, "", cause.Error())
	rbErr := p.rollback(set)
	if !filesRestored(rbErr) {
		return ResultView{Code: ResultRollbackFailed, Message: fmt.Sprintf("%v; откат файлов не удался: %v", cause, rbErr)}
	}
	res := ResultView{Code: ResultWriteFailed, Message: cause.Error(), RolledBack: true}
	if rbErr != nil {
		// Файлы вернулись, не снялся только журнал (тот же отказ записи состояния):
		// ядра всё равно поднимаются на прежних файлах, журнал вернёт RecoverJournal.
		res.Message = fmt.Sprintf("%v; журнал отката не снят: %v", cause, rbErr)
	}
	for _, v := range views {
		switch v.Outcome {
		case RestartOutcomeRestarted, RestartOutcomeHotReloaded, RestartOutcomeRestartedReload:
		default:
			continue
		}
		if rerr := p.recoverKernel(ctx, v.Kernel); rerr != nil {
			res.Code, res.Kernel = ResultKernelNotRecovered, v.Kernel
			res.Message = fmt.Sprintf("%v; повторный рестарт %s на прежних файлах: %v", res.Message, v.Kernel, rerr)
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

// driftKeysExceptMissing — как driftKeys, но без записей, чей файл просто пропал
// (drift_missing): их сборка создаёт заново, ручной правки они не хранят.
func driftKeysExceptMissing(roots Roots, manifest map[string]ManifestEntry) []string {
	var out []string
	for _, c := range CheckManifest(roots, manifest) {
		if c.State.IsDrift() && c.State != StateDriftMissing {
			out = append(out, c.Key)
		}
	}
	return out
}

// syncApplied фиксирует применённое состояние из снимка источника и живые данные,
// когда файлов писать не нужно (пустой план). Для пересборки по ключам и из applied
// применённое состояние не меняется; без расхождений и без хука файл состояния не
// переписывается. Хук и применённое состояние идут одной записью.
func (p *Pipeline) syncApplied(snap State, src Sections, req ApplyRequest, used Input) error {
	syncDraft := req.Source == SourceDraft && len(req.Only) == 0 &&
		draftDirty(State{Draft: src, Applied: snap.Applied})
	hook := p.liveCommitter()
	overlay := req.overlayActive()
	restore := req.restoreActive()
	liveCommit := len(req.LiveCommit) > 0
	if !syncDraft && !overlay && !restore && !liveCommit && hook == nil {
		return nil
	}
	return p.d.Store.Update(func(st *State) error {
		if syncDraft {
			st.Applied = src.Clone()
		}
		// Пустой план: файл тот же, но панельное поле секции всё равно меняется
		// (D-15): значение фиксируется в Applied и Draft, последний путь не теряется.
		if overlay {
			if err := applySectionsOverlay(st, req); err != nil {
				return err
			}
		}
		// Откат к снимку с тем же файлом: Applied и живые данные снимка всё равно
		// фиксируются, иначе панельные поля снимка потерялись бы.
		if restore {
			if err := applyRestore(st, req.Restore); err != nil {
				return err
			}
		}
		if err := commitLiveKeys(st, req.LiveCommit); err != nil {
			return err
		}
		if hook != nil {
			return hook(st, req, used)
		}
		return nil
	})
}

// liveWithOverlay возвращает копию живых данных, в которой ключи оверлея заменяют
// одноимённые; без оверлея — копию как есть. Состояние слоя не меняется.
func liveWithOverlay(live, overlay Sections) Sections {
	out := live.Clone()
	for k, v := range overlay {
		out[k] = cloneRaw(v)
	}
	return out
}

// applySectionsOverlay фиксирует подменённые секции запуска (D-13) в st: значение
// пишется в Applied, а в Draft сливается только функцией req.MergeDraft — другие
// ключи секции и другие секции черновика не меняются. Если черновик изменился,
// ревизия растёт один раз на запуск. Ошибка MergeDraft возвращается вызывающему:
// Store.Update тогда не записывает состояние. Вызывается внутри Store.Update.
func applySectionsOverlay(st *State, req ApplyRequest) error {
	if st.Applied == nil {
		st.Applied = Sections{}
	}
	if st.Draft == nil {
		st.Draft = Sections{}
	}
	names := make([]string, 0, len(req.SectionsOverlay))
	for name := range req.SectionsOverlay {
		names = append(names, name)
	}
	sort.Strings(names)
	draftChanged := false
	for _, name := range names {
		value := req.SectionsOverlay[name]
		oldApplied, hadApplied := st.Applied[name]
		if _, err := settleValue(st.Applied, name, cloneRaw(value), oldApplied, hadApplied); err != nil {
			return err
		}
		if req.MergeDraft == nil {
			continue
		}
		oldDraft, hadDraft := st.Draft[name]
		next, err := req.MergeDraft(name, cloneRaw(oldDraft))
		if err != nil {
			return fmt.Errorf("configlayer: слияние секции %q в черновик: %w", name, err)
		}
		changed, err := settleValue(st.Draft, name, next, oldDraft, hadDraft)
		if err != nil {
			return err
		}
		draftChanged = draftChanged || changed
	}
	if draftChanged {
		st.DraftRevision++
	}
	return nil
}

// applyRestore фиксирует откат к снимку в st (вызывается внутри Store.Update):
// Applied становится снимком целиком, ключи живых данных снимка заменяют текущие
// (JSON null удаляет ключ). Черновик не меняется (D-27), кроме секций из
// ResetDraftSections, которые пользователь не правил: их черновик совпадает с
// текущим Applied до замены и получает значение снимка (секции нет в снимке —
// удаляется и из черновика). Если черновик изменился, ревизия растёт один раз.
func applyRestore(st *State, spec *RestoreSpec) error {
	if st.Draft == nil {
		st.Draft = Sections{}
	}
	if st.Live == nil {
		st.Live = Sections{}
	}
	next := spec.Applied.Clone()
	names := append([]string(nil), spec.ResetDraftSections...)
	sort.Strings(names)
	draftChanged := false
	for i, name := range names {
		if i > 0 && names[i-1] == name {
			continue
		}
		draft, hadDraft := st.Draft[name]
		applied, hadApplied := st.Applied[name]
		untouched := hadDraft == hadApplied && (!hadDraft || SectionEqual(draft, applied))
		if !untouched {
			continue
		}
		changed, err := settleValue(st.Draft, name, cloneRaw(next[name]), draft, hadDraft)
		if err != nil {
			return err
		}
		draftChanged = draftChanged || changed
	}
	st.Applied = next
	if err := commitLiveKeys(st, spec.Live); err != nil {
		return err
	}
	if draftChanged {
		st.DraftRevision++
	}
	return nil
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

// commitState фиксирует манифест, применённое состояние и (хуком) живые данные в
// одной записи файла состояния и очищает журнал записи; вызывается после шага
// перезапуска.
func (p *Pipeline) commitState(plan Plan, src Sections, req ApplyRequest, used Input) error {
	now := p.d.Now()
	hook := p.liveCommitter()
	applyDraft := req.Source == SourceDraft && len(req.Only) == 0
	overlay := req.overlayActive()
	restore := req.restoreActive()
	return p.d.Store.Update(func(st *State) error {
		for _, fp := range plan.Files {
			switch fp.Action {
			case ActionWrite:
				st.Manifest[fp.Key] = ManifestEntry{
					Kernel: fp.Kernel, RelPath: fp.RelPath, Kind: fp.Kind,
					Hash: fp.NewHash, Status: StatusManaged, WrittenAt: now, Inert: fp.Inert,
				}
			case ActionDelete:
				delete(st.Manifest, fp.Key)
			}
		}
		if applyDraft {
			st.Applied = src.Clone()
		}
		if overlay {
			if err := applySectionsOverlay(st, req); err != nil {
				return err
			}
		}
		if restore {
			if err := applyRestore(st, req.Restore); err != nil {
				return err
			}
		}
		// Живые данные запуска — той же записью, что манифест и Applied.
		if err := commitLiveKeys(st, req.LiveCommit); err != nil {
			return err
		}
		if plan.Routing != nil {
			st.Routing = plan.Routing.Next.clone()
			if plan.Routing.NextCores != nil {
				st.Cores = plan.Routing.NextCores.clone()
			}
		}
		// Живые данные фиксируются в той же записи, что и манифест (RESEARCH
		// Pitfall 3): отказ хука отменяет и манифест.
		if hook != nil {
			if err := hook(st, req, used); err != nil {
				return err
			}
		}
		st.Journal = nil
		return nil
	})
}
