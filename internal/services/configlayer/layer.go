package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Ошибки действий слоя.
var (
	ErrDisabled        = errors.New("слой конфигурации выключен")
	ErrDriftBlocked    = errors.New("есть файлы с расхождением: решите их судьбу перед применением")
	ErrUnknownKey      = errors.New("неизвестный ключ файла")
	ErrDevModeRequired = errors.New("действие доступно только в режиме разработчика")
	ErrFileReleased    = errors.New("файл отпущен: панель его не сверяет")
	ErrStopped         = errors.New("слой остановлен")

	errNotImplemented = errors.New("configlayer: not implemented")
)

// Значения по умолчанию и пределы.
const (
	defaultDriftInterval = time.Minute
	defaultDebounceDelay = time.Second
	// versionsCacheTTL — как долго живёт кэш строк версий и карты функций.
	versionsCacheTTL = 30 * time.Second
)

// Виды уведомлений.
const (
	noticeWarning = "warning"
	noticeError   = "error"
)

// Options — зависимости и настройки слоя. Слой создаётся один раз в main.go;
// все функции-зависимости опрашиваются при каждом использовании, поэтому
// переключение флага и dev_mode не требует перезапуска.
type Options struct {
	// DataDir — каталог данных панели (файл состояния, копии, tmp).
	DataDir string
	// Roots — каталоги конфигураций Xray и Mihomo.
	Roots Roots
	// Enabled — включён ли слой (флаг config_layer, D-01).
	Enabled func() bool
	// DevMode — включён ли режим разработчика (диагностика, D-19).
	DevMode func() bool
	// Binaries — пути бинарников ядер; пустой путь — ядро не установлено.
	Binaries func() Binaries
	// XrayEnv — окружение запуска проверки Xray.
	XrayEnv func(dir string) []string
	// ForeignOwned — файл принадлежит старому слою (D-13).
	ForeignOwned func(kernel, rel string) bool
	// KernelVersions — сырые версии ядер и XKeen для строк версий и карты функций.
	KernelVersions func() []KernelVersionInput
	// Applier, ProcessStates, Mihomo, MihomoAPIReady, Lifecycle — зависимости шага
	// перезапуска (144-07); общий замок жизненного цикла приходит из main.go.
	Applier        KernelApplier
	ProcessStates  func() []services.KernelProcessState
	Mihomo         MihomoControl
	MihomoAPIReady func() bool
	Lifecycle      LifecycleLocker
	// Now — источник времени (nil — time.Now).
	Now func() time.Time
	// DriftInterval — период плановой сверки дрейфа (по умолчанию минута).
	DriftInterval time.Duration
	// DebounceDelay — задержка внеочередной сверки по RequestCheck (по умолчанию секунда).
	DebounceDelay time.Duration
}

// Layer — фасад слоя «Конфигурация»: единственный объект, который создаёт
// main.go и которым пользуются обработчики. Связывает хранилище, шину, реестр
// генераторов, конвейер применения и фоновую сверку дрейфа.
type Layer struct {
	opts     Options
	store    *Store
	broker   *Broker
	registry *Registry
	pipeline *Pipeline

	// ctx — контекст жизни слоя: запуски конвейера привязаны к нему, а не к
	// HTTP-запросу, поэтому закрытие вкладки запись не обрывает.
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once

	// checkMu сериализует сверки: результаты публикуются в порядке вычисления.
	checkMu sync.Mutex

	mu      sync.Mutex
	stopped bool
	// lastFiles — JSON последнего результата сверки; nil — базы нет.
	lastFiles []byte
	// failed — уведомления build_failed:<ядро> (только в памяти).
	failed map[string]NoticeView
	// Кэш версий ядер.
	verAt       time.Time
	verKernels  []KernelVersionView
	verFeatures map[Feature]Availability
}

// New создаёт слой: открывает файл состояния, шину, реестр и конвейер. Фоновые
// горутины не запускаются до Start.
func New(opts Options) (*Layer, error) {
	if opts.DataDir == "" {
		return nil, errors.New("configlayer: не задан каталог данных")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.DriftInterval <= 0 {
		opts.DriftInterval = defaultDriftInterval
	}
	if opts.DebounceDelay <= 0 {
		opts.DebounceDelay = defaultDebounceDelay
	}
	broker := NewBroker()
	store, err := OpenStore(opts.DataDir, broker)
	if err != nil {
		broker.Close()
		return nil, err
	}
	// Генератор диагностики зарегистрирован всегда, но вне dev_mode файлов не
	// выдаёт (D-19): переключение dev_mode не требует перезапуска.
	registry := NewRegistry()
	registry.Register(NewDiagGenerator(opts.DevMode))
	pipeline := NewPipeline(PipelineDeps{
		Store:          store,
		Broker:         broker,
		Registry:       registry,
		Roots:          opts.Roots,
		DataDir:        opts.DataDir,
		Binaries:       opts.Binaries,
		XrayEnv:        opts.XrayEnv,
		ForeignOwned:   opts.ForeignOwned,
		Now:            opts.Now,
		Applier:        opts.Applier,
		ProcessStates:  opts.ProcessStates,
		Mihomo:         opts.Mihomo,
		MihomoAPIReady: opts.MihomoAPIReady,
		Lifecycle:      opts.Lifecycle,
	})
	ctx, cancel := context.WithCancel(context.Background())
	return &Layer{
		opts:     opts,
		store:    store,
		broker:   broker,
		registry: registry,
		pipeline: pipeline,
		ctx:      ctx,
		cancel:   cancel,
		failed:   make(map[string]NoticeView),
	}, nil
}

// Enabled — включён ли слой (опрашивается при каждом вызове).
func (l *Layer) Enabled() bool {
	return l.opts.Enabled != nil && l.opts.Enabled()
}

func (l *Layer) devMode() bool {
	return l.opts.DevMode != nil && l.opts.DevMode()
}

func (l *Layer) binaries() Binaries {
	if l.opts.Binaries == nil {
		return Binaries{}
	}
	return l.opts.Binaries()
}

func (l *Layer) installed() InstalledKernels {
	b := l.binaries()
	return InstalledKernels{Xray: b.Xray != "", Mihomo: b.Mihomo != ""}
}

// spawn запускает горутину слоя под учётом wg; после Stop горутины не
// запускаются (иначе wg.Add гонялся бы с wg.Wait).
func (l *Layer) spawn(fn func()) bool {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return false
	}
	l.wg.Add(1)
	l.mu.Unlock()
	go func() {
		defer l.wg.Done()
		fn()
	}()
	return true
}

// bootstrap — действия при старте слоя до первой сверки дрейфа: откат
// прерванной записи по журналу и уборка хвостов (144-06). Содержимое файлов не
// логируется.
func (l *Layer) bootstrap() {
	if _, err := RecoverJournal(l.store, l.opts.Roots); err != nil {
		log.Printf("[configlayer] восстановление по журналу: %v", err)
	}
	if err := CleanupStale(l.opts.DataDir, l.opts.Roots, l.opts.Now()); err != nil {
		log.Printf("[configlayer] уборка хвостов: %v", err)
	}
}

// Start запускает слой. Включённый слой сначала откатывает прерванную запись и
// убирает хвосты, затем берёт базовую сверку; выключенный ничего не читает и не
// пишет. Повторный вызов безопасен.
func (l *Layer) Start() {
	l.startOnce.Do(func() {
		if l.Enabled() {
			l.bootstrap()
			l.checkNow(false)
		}
	})
}

// Stop останавливает слой: отменяет контекст, дожидается всех горутин и
// закрывает шину. Безопасен при повторном вызове.
func (l *Layer) Stop() {
	l.stopOnce.Do(func() {
		l.mu.Lock()
		l.stopped = true
		l.mu.Unlock()
		l.cancel()
	})
	l.wg.Wait()
	l.broker.Close()
}

// Subscribe подписывает на события шины (SSE).
func (l *Layer) Subscribe() (<-chan Event, func(), error) {
	return l.broker.Subscribe()
}

// --- сверка и снимок ---

// checkNow сверяет манифест с диском и публикует событие files при изменении
// результата (D-09). publish=false только запоминает базу (старт слоя). Выключенный
// слой диск не читает и сбрасывает базу.
func (l *Layer) checkNow(publish bool) FilesEvent {
	l.checkMu.Lock()
	defer l.checkMu.Unlock()
	if !l.Enabled() {
		l.mu.Lock()
		l.lastFiles = nil
		l.mu.Unlock()
		return FilesEvent{Files: []FileView{}}
	}
	ev := l.evaluate(l.store.Snapshot())
	data, err := json.Marshal(ev)
	if err != nil {
		return ev
	}
	l.mu.Lock()
	changed := !bytes.Equal(l.lastFiles, data)
	l.lastFiles = data
	l.mu.Unlock()
	if publish && changed {
		l.broker.Publish(Event{Type: EventFiles, Data: ev})
	}
	return ev
}

// evaluate сверяет манифест с диском и добавляет файлы, которые сгенерирует
// текущий черновик (pending).
func (l *Layer) evaluate(st State) FilesEvent {
	checks := CheckManifest(l.opts.Roots, st.Manifest)
	files := make([]FileView, 0, len(checks))
	index := make(map[string]int, len(checks))
	drift := 0
	for _, c := range checks {
		fv := FileView{Key: c.Key, Kernel: c.Kernel, Path: c.AbsPath, Owner: "panel", State: c.State, ObsoleteName: c.ObsoleteName}
		if c.State == StateReleased {
			fv.Owner = "manual"
		}
		if fv.Path == "" {
			if abs, err := l.opts.Roots.Abs(c.Kernel, c.RelPath); err == nil {
				fv.Path = abs
			}
		}
		if c.State.IsDrift() {
			drift++
		}
		index[c.Key] = len(files)
		files = append(files, fv)
	}

	// Pending: файл, которого нет в манифесте, или чей хэш у черновика отличается
	// от записи манифеста при совпадающем диске. Ошибка сборки здесь не важна:
	// «Применить» покажет её сам.
	if generated, err := l.registry.Build(st.Draft, l.installed()); err == nil {
		for _, g := range generated {
			key := g.Key()
			i, known := index[key]
			switch {
			case !known:
				abs, err := l.opts.Roots.Abs(g.Kernel, g.RelPath)
				if err != nil {
					continue
				}
				index[key] = len(files)
				files = append(files, FileView{Key: key, Kernel: g.Kernel, Path: abs, Owner: "panel", State: StatePending})
			case files[i].State == StateOK && st.Manifest[key].Hash != HashContent(g.Content):
				files[i].State = StatePending
			}
		}
	}
	sortFileViews(files)
	return FilesEvent{Files: files, DriftCount: drift}
}

// Snapshot — состояние слоя целиком. Выключенный слой диск и ядра не опрашивает.
func (l *Layer) Snapshot() SnapshotView {
	st := l.store.Snapshot()
	sv := SnapshotView{
		Enabled:       l.Enabled(),
		DevMode:       l.devMode(),
		DraftRevision: st.DraftRevision,
		DraftChanges:  draftChanges(st),
		Files:         []FileView{},
		Kernels:       []KernelVersionView{},
		Features:      map[Feature]Availability{},
		Apply:         l.pipeline.Current(),
		Notices:       l.noticeViews(st),
	}
	if !sv.Enabled {
		return sv
	}
	fe := l.evaluate(st)
	sv.Files, sv.DriftCount = fe.Files, fe.DriftCount
	sv.Kernels, sv.Features = l.versions()
	return sv
}

// versions возвращает строки версий и карту функций с кэшем на versionsCacheTTL.
func (l *Layer) versions() ([]KernelVersionView, map[Feature]Availability) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.opts.Now()
	if l.verKernels != nil && now.Sub(l.verAt) < versionsCacheTTL {
		return l.verKernels, l.verFeatures
	}
	var inputs []KernelVersionInput
	if l.opts.KernelVersions != nil {
		inputs = l.opts.KernelVersions()
	}
	l.verKernels = KernelVersionViews(inputs)
	l.verFeatures = FeatureMap(inputs)
	l.verAt = now
	return l.verKernels, l.verFeatures
}

// invalidateVersions сбрасывает кэш версий (после установки ядра).
func (l *Layer) invalidateVersions() {
	l.mu.Lock()
	l.verKernels, l.verFeatures = nil, nil
	l.mu.Unlock()
}

// noticeViews собирает уведомления: из файла состояния и build_failed из памяти.
func (l *Layer) noticeViews(st State) []NoticeView {
	out := []NoticeView{}
	if st.Notices.SchemaReset {
		out = append(out, NoticeView{ID: "schema_reset", Kind: noticeWarning})
	}
	if st.Notices.RecoveredFromJournal {
		out = append(out, NoticeView{ID: "recovered_from_journal", Kind: noticeWarning})
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, kernel := range []string{KernelXray, KernelMihomo} {
		if n, ok := l.failed[noticeBuildFailedPrefix+kernel]; ok {
			out = append(out, n)
		}
	}
	return out
}

// noticeBuildFailedPrefix — префикс идентификатора уведомления о неудачной
// фоновой сборке файлов ядра.
const noticeBuildFailedPrefix = "build_failed:"

// --- черновик ---

// EditDraft правит секцию черновика с проверкой ревизии (D-05).
func (l *Layer) EditDraft(rev int64, section string, value json.RawMessage) (DraftEvent, error) {
	if !l.Enabled() {
		return DraftEvent{}, ErrDisabled
	}
	ev, err := l.store.EditDraft(rev, section, value)
	if err == nil {
		l.RequestCheck()
	}
	return ev, err
}

// ResetDraft сбрасывает черновик к применённому состоянию.
func (l *Layer) ResetDraft(rev int64) (DraftEvent, error) {
	if !l.Enabled() {
		return DraftEvent{}, ErrDisabled
	}
	ev, err := l.store.ResetDraft(rev)
	if err == nil {
		l.RequestCheck()
	}
	return ev, err
}

// Diag выполняет диагностическое действие над секцией diag черновика (D-19).
// Только в режиме разработчика; ревизия проверяется как у обычной правки.
func (l *Layer) Diag(rev int64, action string) (DraftEvent, error) {
	if !l.Enabled() {
		return DraftEvent{}, ErrDisabled
	}
	if !l.devMode() {
		return DraftEvent{}, ErrDevModeRequired
	}
	value, err := DiagSectionFor(action)
	if err != nil {
		return DraftEvent{}, err
	}
	return l.EditDraft(rev, DiagSection, value)
}

// --- применение ---

// StartApply запускает применение черновика («Применить» по кнопке). Возвращается
// сразу после запуска: конвейер привязан к жизни слоя, а не к HTTP-запросу, и не
// обрывается при закрытии вкладки.
func (l *Layer) StartApply(user bool) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	release, err := l.pipeline.TryBegin(l.ctx, user)
	if err != nil {
		return err
	}
	if !l.spawn(func() {
		defer release()
		l.pipeline.Run(l.ctx, ApplyRequest{Trigger: TriggerUser, Source: SourceDraft})
		release()
		l.afterRun()
	}) {
		release()
		return ErrStopped
	}
	return nil
}

// afterRun — действия после запуска конвейера: число изменений черновика после
// коммита и внеочередная сверка.
func (l *Layer) afterRun() {
	l.broker.Publish(Event{Type: EventDraft, Data: draftEventOf(l.store.Snapshot())})
	l.checkNow(true)
}

// --- пока не реализовано (следующие задачи плана) ---

// Rebuild пересобирает названные файлы из применённого состояния.
func (l *Layer) Rebuild(keys []string, all bool) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	return errNotImplemented
}

// Release отпускает файл.
func (l *Layer) Release(key string) (FilesEvent, error) {
	if !l.Enabled() {
		return FilesEvent{}, ErrDisabled
	}
	return FilesEvent{}, errNotImplemented
}

// DismissNotice закрывает уведомление.
func (l *Layer) DismissNotice(id string) ([]NoticeView, error) { return nil, errNotImplemented }

// RequestCheck просит внеочередную сверку дрейфа.
func (l *Layer) RequestCheck() {}

// OnKernelInstalled собирает файлы для только что установленного ядра.
func (l *Layer) OnKernelInstalled(kernel string) {}

// IsManagedPath — путь принадлежит файлу, которым владеет панель.
func (l *Layer) IsManagedPath(absPath string) bool { return false }

// ReloadFromDisk перечитывает файл состояния после восстановления снимка.
func (l *Layer) ReloadFromDisk() error { return errNotImplemented }

// Enable запускает сборку файлов после включения слоя.
func (l *Layer) Enable() {}

// Disable выключает слой: файлы панели уходят в набор копий.
func (l *Layer) Disable(ctx context.Context) error { return errNotImplemented }

// Diff — ожидаемое и фактическое содержимое файла.
func (l *Layer) Diff(key string) (DiffView, error) { return DiffView{}, errNotImplemented }

// DiffView — две стороны сравнения файла.
type DiffView struct {
	Key       string `json:"key"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Missing   bool   `json:"missing"`
	Truncated bool   `json:"truncated"`
}
