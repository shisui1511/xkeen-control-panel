package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Ошибки действий слоя.
var (
	ErrDisabled        = errors.New("слой конфигурации выключен")
	ErrDriftBlocked    = errors.New("есть файлы с расхождением: решите их судьбу перед применением")
	ErrUnknownKey      = errors.New("неизвестный ключ файла")
	ErrDevModeRequired = errors.New("действие доступно только в режиме разработчика")
	ErrFileReleased    = errors.New("файл отпущен: панель его не сверяет")
	ErrStopped         = errors.New("слой остановлен")
	// ErrAlreadyStarted — генератор зарегистрирован после Start: набор генераторов
	// фиксируется до запуска слоя.
	ErrAlreadyStarted = errors.New("слой уже запущен: генераторы регистрируются до Start")
	// ErrChecksFailed — проверка черновика нашла проблемы: применение не начато.
	// Конкретная ошибка — *ChecksError со списком проблем.
	ErrChecksFailed = errors.New("применение заблокировано проверкой черновика")
)

// ChecksError — отказ применения по проверкам до записи; Is совпадает с
// ErrChecksFailed.
type ChecksError struct {
	Issues []BuildIssue
}

// Error называет число проблем.
func (e *ChecksError) Error() string {
	return fmt.Sprintf("%s: проблем %d", ErrChecksFailed.Error(), len(e.Issues))
}

// Is связывает ChecksError с сентинелом ErrChecksFailed.
func (e *ChecksError) Is(target error) bool { return target == ErrChecksFailed }

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
	// checkReq — запросы внеочередной сверки (буфер 1: повторные сливаются).
	checkReq chan struct{}

	mu      sync.Mutex
	stopped bool
	// started выставляется в Start: после него регистрация генераторов закрыта.
	started bool
	// lastFiles — JSON последнего результата сверки; nil — базы нет.
	lastFiles []byte
	// lastDrift — число файлов с дрейфом по последней сверке; переход >0 → 0 будит
	// onDriftResolved.
	lastDrift int
	// lastRoutingDrift — подпись расхождений маршрутизации по последней сверке:
	// смена будит событие routing.
	lastRoutingDrift string
	// onDriftResolved — колбэк «дрейф решён» (сервис источников), до Start.
	onDriftResolved func()
	// failed — уведомления build_failed:<ядро> (только в памяти).
	failed map[string]NoticeView
	// extNotices — уведомления сервиса источников по префиксу идентификатора
	// (SetNotices); dismissed — закрытые из них: id возвращается только после того,
	// как условие исчезло и появилось снова (D-29).
	extNotices map[string][]NoticeView
	dismissed  map[string]bool
	// Кэш версий ядер.
	verAt time.Time
	// verGen растёт при каждом сбросе кэша: результат вычисления, начатого до
	// сброса, в кэш не попадает.
	verGen      uint64
	verKernels  []KernelVersionView
	verFeatures map[Feature]Availability
	// Мемо проверок черновика (Layer.cachedChecks): ключ последнего расчёта и его
	// итог; checksEpoch растёт в InvalidateChecks.
	checksEpoch uint64
	checksKey   checksKey
	checksVal   []BuildIssue
	checksOK    bool
	// afterApply — хуки после успешного применения (AddAfterApply), до Start.
	afterApply []func(ctx context.Context, info AfterApplyInfo)
}

// checksKey — от чего зависит итог проверок черновика: ревизия черновика, хэш
// живых данных, установленные ядра и эпоха (данные вне черновика: dat, каталог
// списков).
type checksKey struct {
	rev       int64
	liveHash  string
	installed InstalledKernels
	epoch     uint64
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
	// Файл состояния читается лениво, при первом обращении включённого слоя:
	// выключенный слой ничего не читает и не пишет (FND-01).
	store := NewStore(opts.DataDir, broker)
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
		Enabled:        opts.Enabled,
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
		checkReq: make(chan struct{}, 1),
		failed:   make(map[string]NoticeView),

		extNotices: make(map[string][]NoticeView),
		dismissed:  make(map[string]bool),
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

// Start запускает слой. Включённый слой сначала загружает состояние, откатывает
// прерванную запись и убирает хвосты, затем берёт базовую сверку; выключенный
// ничего не читает и не пишет. Повторный вызов безопасен.
func (l *Layer) Start() {
	l.startOnce.Do(func() {
		l.mu.Lock()
		l.started = true
		l.mu.Unlock()
		if l.Enabled() {
			if err := l.store.Load(); err != nil {
				log.Printf("[configlayer] состояние слоя не загружено: %v", err)
			}
			l.bootstrap()
			l.checkNow(false)
		}
		// Цикл работает и при выключенном флаге (его включают без перезапуска):
		// выключенный слой сверку пропускает, не читая диск.
		l.spawn(l.loop)
	})
}

// loop — фоновая сверка дрейфа: по тикеру DriftInterval и по запросам
// RequestCheck с дебаунсом DebounceDelay (повторные запросы сливаются).
func (l *Layer) loop() {
	ticker := time.NewTicker(l.opts.DriftInterval)
	defer ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			l.checkNow(true)
		case <-l.checkReq:
			timer := time.NewTimer(l.opts.DebounceDelay)
			select {
			case <-l.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-l.checkReq:
			default:
			}
			l.checkNow(true)
		}
	}
}

// RequestCheck просит внеочередную сверку дрейфа (после правки черновика, записи
// файла в обход слоя и т. п.); запросы в пределах DebounceDelay сливаются.
func (l *Layer) RequestCheck() {
	select {
	case l.checkReq <- struct{}{}:
	default:
	}
}

// Context — контекст жизни слоя: отменяется только Stop. Длинные операции
// (выключение слоя) выполняются под ним, а не под таймаутом: срок задают
// таймауты самих ядер, а истёкший дедлайн не должен выглядеть остановкой панели.
func (l *Layer) Context() context.Context { return l.ctx }

// Stop останавливает слой: отменяет контекст, дожидается всех горутин и
// фоновой записи черновика и закрывает шину. Безопасен при повторном вызове.
func (l *Layer) Stop() {
	l.stopOnce.Do(func() {
		l.mu.Lock()
		l.stopped = true
		l.mu.Unlock()
		l.cancel()
	})
	l.wg.Wait()
	l.FlushState()
	l.broker.Close()
}

// FlushState дожидается фоновой записи черновика и дописывает остаток синхронно, с
// fsync (Store.FlushSync). Stop вызывает её сам; процесс, который завершается
// через os.Exit (самообновление панели), вызывает её до запуска нового.
func (l *Layer) FlushState() {
	if err := l.store.FlushSync(); err != nil {
		log.Printf("[configlayer] состояние слоя не записано при остановке: %v", err)
	}
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
		l.lastDrift = 0
		l.lastRoutingDrift = ""
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
	resolved := publish && l.lastDrift > 0 && ev.DriftCount == 0
	l.lastDrift = ev.DriftCount
	rsig := routingDriftSig(ev.Routing)
	routingChanged := l.lastRoutingDrift != rsig
	l.lastRoutingDrift = rsig
	onResolved := l.onDriftResolved
	l.mu.Unlock()
	if publish && changed {
		l.broker.Publish(Event{Type: EventFiles, Data: ev})
	}
	if publish && routingChanged {
		l.PublishRouting()
	}
	// Дрейф решён (Пересобрать, Принять правку, ручное исправление): будим сервис
	// источников, чтобы отложенная запись состоялась. Горутина слоя, не под checkMu.
	if resolved && onResolved != nil {
		l.spawn(onResolved)
	}
	return ev
}

// evaluate сверяет манифест с диском и добавляет файлы, которые сгенерирует
// текущий черновик (pending).
func (l *Layer) evaluate(st State) FilesEvent {
	// Записи ядра, бинарник которого удалён, не сверяются: конвейер их не трогает,
	// а их «пропавшие» файлы навсегда заблокировали бы «Применить» (WR-06).
	checks := CheckManifest(l.opts.Roots, manifestOfScope(st.Manifest, l.installed(), ""))
	files := make([]FileView, 0, len(checks))
	index := make(map[string]int, len(checks))
	drift := 0
	// config.yaml Mihomo — симлинк на профиль панели: Редактор открывает его под
	// своим путём, и защита должна совпадать с серверной (IsManagedPath, WR-05).
	cfgPath, cfgResolved := l.mihomoConfigAlias()
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
		if cfgPath != "" && c.Kernel == KernelMihomo && fv.Path != cfgPath && resolveFullSymlinks(fv.Path) == cfgResolved {
			fv.AliasPaths = []string{cfgPath}
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
	if generated, err := l.registry.Build(Input{Sections: st.Draft, Live: st.Live, Mode: SourceDraft, Routing: routingFacts(l.opts.Roots, st.Routing)}, l.installed()); err == nil {
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
	// Расхождения маршрутизации входят в число дрейфа: «Применить» блокируется тем же
	// механизмом (кроме запуска с желаемым managed=false, D-06, D-07).
	rd := routingDrift(l.opts.Roots, st.Routing, l.installed(), "", l.opts.ForeignOwned)
	return FilesEvent{Files: files, DriftCount: drift + len(rd), Routing: rd}
}

// routingDriftSig — подпись списка расхождений маршрутизации для сравнения сверок.
func routingDriftSig(items []RoutingDriftItem) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Kernel + "|" + it.Code + "|" + it.File + ";")
	}
	return b.String()
}

// mihomoConfigAlias — путь config.yaml Mihomo и файл, в который он разрешается,
// если config.yaml — симлинк; иначе пустая строка.
func (l *Layer) mihomoConfigAlias() (path, resolved string) {
	abs, err := l.opts.Roots.Abs(KernelMihomo, "config.yaml")
	if err != nil {
		return "", ""
	}
	target, err := filepath.EvalSymlinks(abs)
	if err != nil || target == abs {
		return "", ""
	}
	if st, err := os.Lstat(abs); err != nil || st.Mode()&os.ModeSymlink == 0 {
		return "", ""
	}
	return abs, target
}

// Snapshot — состояние слоя целиком. Выключенный слой диск и ядра не опрашивает.
func (l *Layer) Snapshot() SnapshotView {
	enabled := l.Enabled()
	st := emptyState()
	if enabled {
		st = l.store.Snapshot()
	}
	sv := SnapshotView{
		Enabled:       enabled,
		DevMode:       l.devMode(),
		DraftRevision: st.DraftRevision,
		DraftChanges:  draftChanges(st),
		Files:         []FileView{},
		Kernels:       []KernelVersionView{},
		Features:      map[Feature]Availability{},
		Apply:         l.pipeline.Current(),
		Notices:       l.noticeViews(st),
		Checks:        []BuildIssue{},
	}
	if !sv.Enabled {
		return sv
	}
	sv.Checks = l.cachedChecks(st)
	fe := l.evaluate(st)
	sv.Files, sv.DriftCount = fe.Files, fe.DriftCount
	sv.Routing = l.routingSnapshot(st, fe.Routing)
	sv.Kernels, sv.Features = l.versions()
	return sv
}

// routingSnapshot собирает раздел routing снимка: желаемое из секции черновика,
// по установленным ядрам — взято ли, перенесённые и оставленные файлы, дрейф.
func (l *Layer) routingSnapshot(st State, drift []RoutingDriftItem) RoutingSnapshot {
	out := RoutingSnapshot{Desired: DesiredRouting(st.Draft).Managed}
	installed := l.installed()
	view := func(kernel string, taken bool, moved, left []string, prev string) *RoutingKernelView {
		v := &RoutingKernelView{Taken: taken, Moved: []string{}, Left: []string{}, Drift: []string{}}
		if taken {
			v.Moved = append(v.Moved, moved...)
			v.Left = append(v.Left, left...)
			v.PrevProfile = prev
		}
		for _, it := range drift {
			if it.Kernel == kernel {
				v.Drift = append(v.Drift, it.File)
			}
		}
		return v
	}
	if installed.Xray {
		var moved, left []string
		if x := st.Routing.Xray; x != nil {
			for _, m := range x.Moved {
				moved = append(moved, m.Name)
			}
			left = x.Left
		}
		out.Xray = view(KernelXray, st.Routing.Xray != nil, moved, left, "")
	}
	if installed.Mihomo {
		prev := ""
		if m := st.Routing.Mihomo; m != nil {
			prev = m.PrevProfile
		}
		out.Mihomo = view(KernelMihomo, st.Routing.Mihomo != nil, nil, nil, prev)
	}
	return out
}

// versions возвращает строки версий и карту функций с кэшем на versionsCacheTTL.
// KernelVersions может запускать бинарники ядер, поэтому вызывается без l.mu:
// иначе всё это время ждали бы spawn, noticeViews, noteBuildResult и checkNow.
func (l *Layer) versions() ([]KernelVersionView, map[Feature]Availability) {
	l.mu.Lock()
	now := l.opts.Now()
	if l.verKernels != nil && now.Sub(l.verAt) < versionsCacheTTL {
		kernels, features := l.verKernels, l.verFeatures
		l.mu.Unlock()
		return kernels, features
	}
	gen := l.verGen
	l.mu.Unlock()

	var inputs []KernelVersionInput
	if l.opts.KernelVersions != nil {
		inputs = l.opts.KernelVersions()
	}
	kernels, features := KernelVersionViews(inputs), FeatureMap(inputs)

	l.mu.Lock()
	// Кэш сбросили, пока считали (установка ядра): результат устарел, отдаём его
	// вызывающему, но не запоминаем.
	if l.verGen == gen {
		l.verKernels, l.verFeatures, l.verAt = kernels, features, now
	}
	l.mu.Unlock()
	return kernels, features
}

// invalidateVersions сбрасывает кэш версий (после установки ядра).
func (l *Layer) invalidateVersions() {
	l.mu.Lock()
	l.verKernels, l.verFeatures = nil, nil
	l.verGen++
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
	if st.Notices.JournalRecoveryFailed {
		out = append(out, NoticeView{ID: "journal_recovery_failed", Kind: noticeError})
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, kernel := range []string{KernelXray, KernelMihomo} {
		if n, ok := l.failed[noticeBuildFailedPrefix+kernel]; ok {
			out = append(out, n)
		}
	}
	return append(out, l.visibleExtLocked()...)
}

// visibleExtLocked — незакрытые внешние уведомления: префиксы по алфавиту, внутри
// префикса по ID. Вызывается под l.mu.
func (l *Layer) visibleExtLocked() []NoticeView {
	prefixes := make([]string, 0, len(l.extNotices))
	for p := range l.extNotices {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	var out []NoticeView
	for _, p := range prefixes {
		for _, n := range l.extNotices[p] {
			if !l.dismissed[n.ID] {
				out = append(out, n)
			}
		}
	}
	return out
}

// SetNotices заменяет уведомления сервиса источников с данным префиксом
// идентификатора (например, "source_"). Закрытое пользователем уведомление не
// возвращается, пока его условие не исчезнет из списка и не появится снова.
// Событие notices публикуется только при изменении видимого списка.
func (l *Layer) SetNotices(prefix string, notices []NoticeView) {
	l.mu.Lock()
	before, _ := json.Marshal(l.visibleExtLocked())
	list := make([]NoticeView, 0, len(notices))
	present := make(map[string]bool, len(notices))
	for _, n := range notices {
		if !strings.HasPrefix(n.ID, prefix) || present[n.ID] {
			continue
		}
		present[n.ID] = true
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	if len(list) == 0 {
		delete(l.extNotices, prefix)
	} else {
		l.extNotices[prefix] = list
	}
	// Условие исчезло — закрытие забывается: при повторном появлении уведомление
	// снова видно.
	for id := range l.dismissed {
		if strings.HasPrefix(id, prefix) && !present[id] {
			delete(l.dismissed, id)
		}
	}
	after, _ := json.Marshal(l.visibleExtLocked())
	l.mu.Unlock()
	if bytes.Equal(before, after) {
		return
	}
	l.publishNotices()
}

// publishNotices рассылает текущие уведомления событием notices (выключенный слой
// диск не читает и событий не шлёт).
func (l *Layer) publishNotices() {
	if !l.Enabled() {
		return
	}
	l.broker.Publish(Event{Type: EventNotices, Data: NoticesEvent{Notices: l.noticeViews(l.store.Snapshot())}})
}

// isExtNoticeID — идентификатор есть среди внешних уведомлений.
func (l *Layer) isExtNoticeID(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, list := range l.extNotices {
		for _, n := range list {
			if n.ID == id {
				return true
			}
		}
	}
	return false
}

// PublishSources сообщает клиентам, что представление источников изменилось
// (событие sources с текущей ревизией черновика). Выключенный слой молчит.
func (l *Layer) PublishSources() {
	if !l.Enabled() {
		return
	}
	l.broker.Publish(Event{Type: EventSources, Data: SourcesEvent{Revision: l.store.Snapshot().DraftRevision}})
}

// PublishRouting сообщает клиентам, что владение маршрутизацией или её дрейф
// изменились (событие routing с текущей ревизией черновика). Выключенный слой
// молчит.
func (l *Layer) PublishRouting() {
	if !l.Enabled() {
		return
	}
	l.broker.Publish(Event{Type: EventRouting, Data: RoutingEvent{Revision: l.store.Snapshot().DraftRevision}})
}

// Ошибки действий с маршрутизацией.
var (
	// ErrNothingToReclaim — у ядра нет дрейфа маршрутизации: возвращать нечего.
	ErrNothingToReclaim = errors.New(IssueNothingToReclaim)
	// ErrForeignMixed — смешанный чужой файл с routing: переносится только
	// «Отпустить маршрутизацию». Конкретная ошибка — *ForeignMixedError с файлом.
	ErrForeignMixed = errors.New(IssueForeignMixed)
	// ErrUnknownKernel — ядро не xray и не mihomo.
	ErrUnknownKernel = errors.New("неизвестное ядро")
)

// ForeignMixedError — «вернуть файл панели» отказал: файл File содержит не только
// routing. Диск не тронут; Is совпадает с ErrForeignMixed.
type ForeignMixedError struct {
	File string
}

// Error называет файл.
func (e *ForeignMixedError) Error() string { return IssueForeignMixed + ": " + e.File }

// Is связывает ForeignMixedError с сентинелом ErrForeignMixed.
func (e *ForeignMixedError) Is(target error) bool { return target == ErrForeignMixed }

// RoutingPreview — предпросмотр взятия для диалога (E10): какие файлы routing Xray
// уйдут в копии, какие останутся, предупреждения D-44 и прежний профиль Mihomo с
// портами по текущему диску. Неустановленное ядро — nil. Диск не меняется.
func (l *Layer) RoutingPreview() (TakeoverPreview, error) {
	if !l.Enabled() {
		return TakeoverPreview{}, ErrDisabled
	}
	installed := l.installed()
	var files []RoutingFile
	if installed.Xray {
		var err error
		if files, _, err = ScanXrayRouting(l.opts.Roots.Xray, l.opts.ForeignOwned); err != nil {
			return TakeoverPreview{}, err
		}
	}
	prevProfile, ports := "", defaultMihomoPorts()
	if installed.Mihomo {
		var err error
		if _, prevProfile, ports, err = ScanMihomoActive(l.opts.Roots.Mihomo); err != nil {
			return TakeoverPreview{}, err
		}
	}
	pv := RoutingPreviewOf(files, prevProfile, ports)
	if !installed.Xray {
		pv.Xray = nil
	}
	if !installed.Mihomo {
		pv.Mihomo = nil
	}
	return pv, nil
}

// ReclaimRouting решает дрейф маршрутизации ядра, оставляя маршрутизацию у панели
// (D-06, D-07). Mihomo: config.yaml перекладывается на профиль панели (обычный файл
// уходит в набор копий), затем Mihomo перечитывает профиль. Xray: каждый «чистый»
// чужой файл routing переносится в копию и добавляется к перенесённым, затем Xray
// перезапускается; если среди расхождений есть смешанный файл — ошибка
// *ForeignMixedError без изменений (его решает только «Отпустить»). Нет дрейфа —
// ErrNothingToReclaim. Всё под журналом и откатом, как взятие; занятое применение —
// ErrApplyBusy или ErrKernelBusy.
func (l *Layer) ReclaimRouting(ctx context.Context, kernel string) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	if kernel != KernelXray && kernel != KernelMihomo {
		return fmt.Errorf("%w: %q", ErrUnknownKernel, kernel)
	}
	release, err := l.pipeline.TryBegin(ctx, true)
	if err != nil {
		return err
	}
	defer release()
	if err := l.pipeline.recoverPendingJournal(); err != nil {
		return err
	}
	st := l.store.Snapshot()
	items := routingDrift(l.opts.Roots, st.Routing, l.installed(), kernel, l.opts.ForeignOwned)
	if len(items) == 0 {
		return ErrNothingToReclaim
	}
	ops := &RoutingOps{Next: st.Routing.clone()}
	switch kernel {
	case KernelXray:
		files, _, err := ScanXrayRouting(l.opts.Roots.Xray, l.opts.ForeignOwned)
		if err != nil {
			return err
		}
		byName := make(map[string]RoutingFile, len(files))
		for _, f := range files {
			byName[f.Name] = f
		}
		// Сначала проверяются все файлы: смешанный отказывает без единого изменения.
		for _, it := range items {
			if f, ok := byName[it.File]; !ok || !f.Pure || !safeRoutingName(f.Name) {
				return &ForeignMixedError{File: it.File}
			}
		}
		taking := make(map[string]bool, len(items))
		for _, it := range items {
			ops.XrayTake = append(ops.XrayTake, it.File)
			taking[it.File] = true
		}
		// Файл, который раньше был смешанным, а теперь перенесён, оставленным не числится.
		if x := ops.Next.Xray; x != nil {
			left := make([]string, 0, len(x.Left))
			for _, n := range x.Left {
				if !taking[n] {
					left = append(left, n)
				}
			}
			x.Left = left
		}
	case KernelMihomo:
		ops.MihomoPoint, ops.keepPrev = true, true
	}
	if err := l.commitDiskPlan(ctx, Plan{Routing: ops}, TriggerRoutingReclaim, ReasonRoutingTakeover); err != nil {
		return err
	}
	l.PublishRouting()
	l.checkNow(true)
	return nil
}

// noticeBuildFailedPrefix — префикс идентификатора уведомления о неудачной
// фоновой сборке файлов ядра.
const noticeBuildFailedPrefix = "build_failed:"

// noticeRoutingAutoTakenPrefix — префикс идентификатора уведомления «панель
// автоматически взяла маршрутизацию установленного позже ядра» (D-02):
// routing_auto_taken:<ядро>.
const noticeRoutingAutoTakenPrefix = "routing_auto_taken:"

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

// EditDraftBatch атомарно правит несколько секций черновика и, если live не
// nil, живые данные слоя под одной проверкой ревизии: конфликт — ErrDraftConflict
// без записи, успех — ревизия +1 и одно событие draft. Значение nil или JSON
// null удаляет секцию.
func (l *Layer) EditDraftBatch(rev int64, sections map[string]json.RawMessage, live func(live Sections) error) (DraftEvent, error) {
	if !l.Enabled() {
		return DraftEvent{}, ErrDisabled
	}
	ev, err := l.store.EditDraftBatch(rev, sections, live)
	if err == nil {
		l.RequestCheck()
	}
	return ev, err
}

// UpdateLive обновляет живые данные слоя (метаданные и статусы источников)
// отдельно от черновика: ревизия не растёт, события конвейера не публикуются.
// fn получает глубокую копию Live; ошибка fn или записи — ничего не меняется.
func (l *Layer) UpdateLive(fn func(live Sections) error) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	return l.store.Update(func(st *State) error {
		if st.Live == nil {
			st.Live = Sections{}
		}
		return fn(st.Live)
	})
}

// UpdateLiveWith — как UpdateLive, но fn видит и черновик с применённым
// состоянием той же записи: сервис может сверить, что конфигурация, по которой он
// считал живые данные, не изменилась, пока шёл долгий запрос. Draft и Applied в
// view — только для чтения (их изменения не сохраняются), Live — копия под запись.
func (l *Layer) UpdateLiveWith(fn func(view StateView) error) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	return l.store.Update(func(st *State) error {
		if st.Live == nil {
			st.Live = Sections{}
		}
		return fn(StateView{DraftRevision: st.DraftRevision, Draft: st.Draft.Clone(), Applied: st.Applied.Clone(), Live: st.Live})
	})
}

// StateView — копия секций слоя для сервисов-потребителей.
type StateView struct {
	DraftRevision int64
	Draft         Sections
	Applied       Sections
	Live          Sections
}

// ReadState возвращает глубокие копии черновика, применённого состояния и
// живых данных: изменение результата состояние слоя не меняет.
func (l *Layer) ReadState() (StateView, error) {
	if !l.Enabled() {
		return StateView{}, ErrDisabled
	}
	st := l.store.Snapshot()
	return StateView{
		DraftRevision: st.DraftRevision,
		Draft:         st.Draft,
		Applied:       st.Applied,
		Live:          st.Live,
	}, nil
}

// RegisterGenerator добавляет генератор в реестр. Допустима только до Start;
// генератор с уже занятым ID отклоняется.
func (l *Layer) RegisterGenerator(g Generator) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return ErrAlreadyStarted
	}
	if l.registry.Has(g.ID()) {
		return fmt.Errorf("configlayer: генератор %q уже зарегистрирован", g.ID())
	}
	l.registry.Register(g)
	return nil
}

// AddLiveCommitter добавляет хук фиксации живых данных конвейера (см.
// PipelineDeps.CommitLive) в конец списка: хуки вызываются по порядку
// регистрации в той же записи состояния, что и манифест. Допустим только до
// Start; nil отклоняется.
func (l *Layer) AddLiveCommitter(fn func(st *State, req ApplyRequest, used Input) error) error {
	if fn == nil {
		return errors.New("configlayer: хук фиксации живых данных не задан")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return ErrAlreadyStarted
	}
	l.pipeline.addCommitLive(fn)
	return nil
}

// AfterApplyInfo — итог успешного применения для хуков AddAfterApply: что
// запустило конвейер, из какого состояния собраны файлы, итог и перезапуски.
type AfterApplyInfo struct {
	Trigger Trigger
	Source  SourceKind
	Result  ResultView
	Restart []RestartView
}

// AddAfterApply добавляет хук, который слой вызывает асинхронно после каждого
// успешного применения (пользовательского и фонового), уже после снятия замка
// применения: хук может сам вызвать RunLive. Неуспешное применение хуки не
// вызывает. Допустим только до Start; nil отклоняется.
func (l *Layer) AddAfterApply(fn func(ctx context.Context, info AfterApplyInfo)) error {
	if fn == nil {
		return errors.New("configlayer: хук после применения не задан")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return ErrAlreadyStarted
	}
	l.afterApply = append(l.afterApply, fn)
	return nil
}

// fireAfterApply вызывает хуки после успешного применения, каждый в своей
// горутине слоя. Вызывается после снятия замка применения и afterRun.
func (l *Layer) fireAfterApply(req ApplyRequest, view ApplyView) {
	if view.Result == nil || !view.Result.OK {
		return
	}
	l.mu.Lock()
	hooks := append([]func(context.Context, AfterApplyInfo){}, l.afterApply...)
	l.mu.Unlock()
	if len(hooks) == 0 {
		return
	}
	info := AfterApplyInfo{
		Trigger: req.Trigger,
		Source:  req.Source,
		Result:  *view.Result,
		Restart: append([]RestartView(nil), view.Restart...),
	}
	for _, fn := range hooks {
		l.spawn(func() { fn(l.ctx, info) })
	}
}

// RegisterChecker добавляет проверку до записи в реестр. Допустима только до
// Start; проверка с уже занятым ID отклоняется.
func (l *Layer) RegisterChecker(c Checker) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return ErrAlreadyStarted
	}
	if l.registry.HasChecker(c.ID()) {
		return fmt.Errorf("configlayer: проверка %q уже зарегистрирована", c.ID())
	}
	l.registry.RegisterChecker(c)
	return nil
}

// checksFor проверяет черновик состояния: те же входные данные, из которых Run
// собирает файлы для SourceDraft. Пустой срез — проблем нет. Диск проверка не
// читает: это обязанность самих проверок.
func (l *Layer) checksFor(st State) []BuildIssue {
	return l.registry.Check(Input{Sections: st.Draft, Live: st.Live, Mode: SourceDraft, Routing: routingFacts(l.opts.Roots, st.Routing)}, l.installed())
}

// cachedChecks — checksFor с мемо по ревизии черновика, хэшу живых данных,
// установленным ядрам и эпохе проверок: Snapshot и ежеминутная сверка не
// пересчитывают проверки, пока ничего не изменилось. Возвращает копию.
func (l *Layer) cachedChecks(st State) []BuildIssue {
	live, err := json.Marshal(st.Live)
	if err != nil {
		return l.checksFor(st)
	}
	installed := l.installed()
	l.mu.Lock()
	key := checksKey{rev: st.DraftRevision, liveHash: HashContent(live), installed: installed, epoch: l.checksEpoch}
	if l.checksOK && l.checksKey == key {
		out := append([]BuildIssue{}, l.checksVal...)
		l.mu.Unlock()
		return out
	}
	l.mu.Unlock()
	issues := l.checksFor(st)
	l.mu.Lock()
	// Эпоха сменилась, пока считали: итог мог устареть, в мемо он не попадает.
	if l.checksEpoch == key.epoch {
		l.checksKey, l.checksVal, l.checksOK = key, append([]BuildIssue{}, issues...), true
	}
	l.mu.Unlock()
	return issues
}

// InvalidateChecks сбрасывает мемо проверок и просит внеочередную сверку:
// сервисы зовут его, когда изменились данные вне черновика (dat, каталог
// списков), от которых зависят проверки.
func (l *Layer) InvalidateChecks() {
	l.mu.Lock()
	l.checksEpoch++
	l.mu.Unlock()
	l.RequestCheck()
}

// OnDriftResolved регистрирует колбэк «дрейф решён»: вызывается, когда число
// файлов с дрейфом по сверке становится 0 (Пересобрать, Принять правку, ручное
// исправление файла). Допустим только до Start.
func (l *Layer) OnDriftResolved(fn func()) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return ErrAlreadyStarted
	}
	l.onDriftResolved = fn
	return nil
}

// Hooks — всё, чем сервис подключается к слою: генераторы, проверка и хуки.
// Пустые поля пропускаются.
type Hooks struct {
	Generators      []Generator
	Checker         Checker
	LiveCommitter   LiveCommitFunc
	AfterApply      func(ctx context.Context, info AfterApplyInfo)
	OnDriftResolved func()
}

// Register подключает сервис целиком или не подключает вовсе: все условия
// (слой не запущен, ID генераторов и проверки свободны и не повторяются)
// проверяются под одним замком до первой записи. Ошибка оставляет слой как был:
// у сервиса, которого не подключили, в слое ничего не остаётся. Допустим только
// до Start.
func (l *Layer) Register(h Hooks) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return ErrAlreadyStarted
	}
	seen := make(map[string]bool, len(h.Generators))
	for _, g := range h.Generators {
		if g == nil {
			return errors.New("configlayer: генератор не задан")
		}
		if seen[g.ID()] || l.registry.Has(g.ID()) {
			return fmt.Errorf("configlayer: генератор %q уже зарегистрирован", g.ID())
		}
		seen[g.ID()] = true
	}
	if h.Checker != nil && l.registry.HasChecker(h.Checker.ID()) {
		return fmt.Errorf("configlayer: проверка %q уже зарегистрирована", h.Checker.ID())
	}
	for _, g := range h.Generators {
		l.registry.Register(g)
	}
	if h.Checker != nil {
		l.registry.RegisterChecker(h.Checker)
	}
	if h.LiveCommitter != nil {
		l.pipeline.addCommitLive(h.LiveCommitter)
	}
	if h.AfterApply != nil {
		l.afterApply = append(l.afterApply, h.AfterApply)
	}
	if h.OnDriftResolved != nil {
		l.onDriftResolved = h.OnDriftResolved
	}
	return nil
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
	// Проверки до записи: пустая группа или висячая ссылка не дают начать
	// применение (GRP-05, SRC-07). Фоновые запуски проверяет сам конвейер.
	if issues := l.cachedChecks(l.store.Snapshot()); len(issues) > 0 {
		return &ChecksError{Issues: issues}
	}
	release, err := l.pipeline.TryBegin(l.ctx, user)
	if err != nil {
		return err
	}
	// Свежая сверка под замком применения: пока есть нерешённый дрейф, диск не
	// трогается (D-11). «Пересобрать» по названным файлам этим не блокируется.
	// Расхождение маршрутизации не блокирует применение с желаемым managed=false:
	// это и есть решение «Отпустить маршрутизацию» (D-06, D-07).
	fe := l.checkNow(true)
	if fe.DriftCount-len(fe.Routing) > 0 || (len(fe.Routing) > 0 && DesiredRouting(l.store.Snapshot().Draft).Managed) {
		release()
		return ErrDriftBlocked
	}
	if !l.spawn(func() {
		defer release()
		req := ApplyRequest{Trigger: TriggerUser, Source: SourceDraft}
		view := l.runPipeline(req)
		release()
		l.afterRun()
		l.fireAfterApply(req, view)
	}) {
		release()
		return ErrStopped
	}
	return nil
}

// runPipeline запускает конвейер и следит за владением маршрутизацией: если запуск
// взял или вернул маршрутизацию, клиенты получают событие routing; ядро, которое
// установка взяла автоматически (D-02), получает уведомление routing_auto_taken.
func (l *Layer) runPipeline(req ApplyRequest) ApplyView {
	before := l.store.Snapshot().Routing
	view := l.pipeline.Run(l.ctx, req)
	after := l.store.Snapshot().Routing
	auto := ""
	if req.Trigger == TriggerKernelInstalled && view.Result != nil && view.Result.OK && routingKernelTaken(after, req.Kernel) && !routingKernelTaken(before, req.Kernel) {
		auto = req.Kernel
	}
	l.updateAutoTakenNotices(auto, after)
	if !routingStateEqual(before, after) {
		l.PublishRouting()
	}
	return view
}

// routingKernelTaken — панель владеет маршрутизацией ядра.
func routingKernelTaken(r RoutingState, kernel string) bool {
	switch kernel {
	case KernelXray:
		return r.Xray != nil
	case KernelMihomo:
		return r.Mihomo != nil
	}
	return false
}

// routingStateEqual сравнивает владение маршрутизацией до и после запуска.
func routingStateEqual(a, b RoutingState) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// updateAutoTakenNotices обновляет уведомления routing_auto_taken:<ядро>: добавляет
// auto (если не пусто, закрытое ранее показывается заново) и убирает уведомления
// ядер, которыми панель больше не владеет.
func (l *Layer) updateAutoTakenNotices(auto string, now RoutingState) {
	l.mu.Lock()
	cur := append([]NoticeView(nil), l.extNotices[noticeRoutingAutoTakenPrefix]...)
	if auto != "" {
		delete(l.dismissed, noticeRoutingAutoTakenPrefix+auto)
	}
	l.mu.Unlock()
	list := make([]NoticeView, 0, len(cur)+1)
	for _, n := range cur {
		if n.Kernel != auto && routingKernelTaken(now, n.Kernel) {
			list = append(list, n)
		}
	}
	if auto != "" {
		list = append(list, NoticeView{ID: noticeRoutingAutoTakenPrefix + auto, Kind: noticeWarning, Kernel: auto})
	}
	l.SetNotices(noticeRoutingAutoTakenPrefix, list)
}

// afterRun — действия после запуска конвейера: число изменений черновика после
// коммита, уведомления (запуск мог восстановить файлы по журналу) и внеочередная
// сверка.
func (l *Layer) afterRun() {
	st := l.store.Snapshot()
	l.broker.Publish(Event{Type: EventDraft, Data: draftEventOf(st)})
	l.broker.Publish(Event{Type: EventNotices, Data: NoticesEvent{Notices: l.noticeViews(st)}})
	l.checkNow(true)
}

// LiveRun — параметры фонового запуска конвейера в обход черновика (RunLive).
type LiveRun struct {
	// Trigger — что запустило конвейер (TriggerSourceRefresh, TriggerSelect,
	// TriggerDatRefresh); попадает в шаги apply_step и в хуки.
	Trigger Trigger
	// Overlay — ключи живых данных, которые заменяют одноимённые во входе
	// генераторов этого запуска (кандидат); сами живые данные фиксируют хуки
	// AddLiveCommitter.
	Overlay Sections
	// Kernel ограничивает запуск одним ядром; пусто — все установленные.
	Kernel string
}

// RunLive — запуск конвейера из применённого состояния с оверлеем живых данных
// (FND-06, U-04): сборка, проверки до записи, проверка обоих ядер, запись,
// перезапуск только запущенного, коммит. Черновик и его ревизия не меняются; живые
// данные фиксируют хуки AddLiveCommitter в той же записи, что и манифест. Идущее
// «Применить» ждёт (параллельного применения нет). Нерешённый дрейф —
// ErrDriftBlocked: файлы и ядро не тронуты. Вызов синхронный: горутину запускает
// вызывающий сервис со своим wg. После успешного запуска слой зовёт хуки
// AddAfterApply уже после снятия замка применения.
func (l *Layer) RunLive(ctx context.Context, run LiveRun) (ApplyView, error) {
	if !l.Enabled() {
		return ApplyView{}, ErrDisabled
	}
	if run.Trigger == "" {
		return ApplyView{}, errors.New("configlayer: не задан триггер запуска")
	}
	l.mu.Lock()
	stopped := l.stopped
	l.mu.Unlock()
	if stopped {
		return ApplyView{}, ErrStopped
	}
	release, err := l.pipeline.TryBegin(ctx, false)
	if err != nil {
		if l.ctx.Err() != nil {
			return ApplyView{}, ErrStopped
		}
		return ApplyView{}, err
	}
	defer release()
	req := ApplyRequest{Trigger: run.Trigger, Source: SourceApplied, LiveOverlay: run.Overlay, Kernel: run.Kernel}
	view := l.runPipeline(req)
	release()
	l.afterRun()
	l.fireAfterApply(req, view)
	if view.Result != nil && view.Result.Code == ResultDriftBlocked {
		return view, ErrDriftBlocked
	}
	return view, nil
}

// RunSourceRefresh — фоновое обновление источника: RunLive с триггером
// TriggerSourceRefresh (его зовёт сервис источников).
func (l *Layer) RunSourceRefresh(ctx context.Context, overlay Sections) (ApplyView, error) {
	return l.RunLive(ctx, LiveRun{Trigger: TriggerSourceRefresh, Overlay: overlay})
}

// Rebuild — «Пересобрать»: полный конвейер, ограниченный названными файлами.
// Источник — применённое состояние, а не черновик (D-10, D-11): ручная правка
// заменяется тем, что панель применяла в прошлый раз. all=true берёт все файлы с
// дрейфом (ключи keys игнорируются). Пустой набор или ключ вне манифеста —
// ErrUnknownKey. Возврат сразу после запуска; итог приходит событием apply_done.
func (l *Layer) Rebuild(keys []string, all bool) error {
	if !l.Enabled() {
		return ErrDisabled
	}
	st := l.store.Snapshot()
	var only []string
	// Файлы неустановленного ядра пересобирать нечем: ключи такого ядра — неизвестные.
	manifest := manifestOfScope(st.Manifest, l.installed(), "")
	if all {
		for _, c := range CheckManifest(l.opts.Roots, manifest) {
			if c.State.IsDrift() {
				only = append(only, c.Key)
			}
		}
	} else {
		seen := make(map[string]bool, len(keys))
		for _, k := range keys {
			if _, ok := manifest[k]; !ok {
				return ErrUnknownKey
			}
			if !seen[k] {
				seen[k] = true
				only = append(only, k)
			}
		}
		sort.Strings(only)
	}
	if len(only) == 0 {
		return ErrUnknownKey
	}
	release, err := l.pipeline.TryBegin(l.ctx, true)
	if err != nil {
		return err
	}
	if !l.spawn(func() {
		defer release()
		req := ApplyRequest{Trigger: TriggerRebuild, Source: SourceApplied, Only: only}
		view := l.runPipeline(req)
		release()
		l.afterRun()
		l.fireAfterApply(req, view)
	}) {
		release()
		return ErrStopped
	}
	return nil
}

// Release — «Принять правку»: запись манифеста становится released (D-10). Файл
// не перезаписывается, не сверяется и не считается сиротой, пока пользователь не
// нажмёт «Пересобрать» по его ключу. Файл не переименовывается.
func (l *Layer) Release(key string) (FilesEvent, error) {
	if !l.Enabled() {
		return FilesEvent{}, ErrDisabled
	}
	if _, ok := l.store.Snapshot().Manifest[key]; !ok {
		return FilesEvent{}, ErrUnknownKey
	}
	release, err := l.pipeline.TryBegin(l.ctx, true)
	if err != nil {
		return FilesEvent{}, err
	}
	defer release()
	err = l.store.Update(func(st *State) error {
		entry, ok := st.Manifest[key]
		if !ok {
			return ErrUnknownKey
		}
		entry.Status = StatusReleased
		st.Manifest[key] = entry
		return nil
	})
	if err != nil {
		return FilesEvent{}, err
	}
	return l.checkNow(true), nil
}

// --- уведомления ---

// DismissNotice закрывает уведомление: schema_reset, recovered_from_journal и
// journal_recovery_failed сохраняются в файле состояния, build_failed:<ядро> и
// уведомления сервиса источников (SetNotices) живут только в памяти.
// Неизвестный идентификатор — ErrUnknownNotice. Возвращает оставшиеся
// уведомления и публикует событие notices.
func (l *Layer) DismissNotice(id string) ([]NoticeView, error) {
	switch {
	case id == "schema_reset" || id == "recovered_from_journal" || id == "journal_recovery_failed":
		if err := l.store.DismissNotice(id); err != nil {
			return nil, err
		}
	case isBuildFailedID(id):
		l.mu.Lock()
		delete(l.failed, id)
		l.mu.Unlock()
	case l.isExtNoticeID(id):
		l.mu.Lock()
		l.dismissed[id] = true
		l.mu.Unlock()
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownNotice, id)
	}
	views := l.noticeViews(l.store.Snapshot())
	l.broker.Publish(Event{Type: EventNotices, Data: NoticesEvent{Notices: views}})
	return views, nil
}

// isBuildFailedID — идентификатор вида build_failed:<ядро> известного ядра.
func isBuildFailedID(id string) bool {
	kernel, ok := strings.CutPrefix(id, noticeBuildFailedPrefix)
	return ok && (kernel == KernelXray || kernel == KernelMihomo)
}

// noteBuildResult отражает итог фоновой сборки в уведомлениях: неудача ставит
// build_failed:<ядро>, успех снимает прежнее. Событие notices — только при изменении.
func (l *Layer) noteBuildResult(kernel string, view ApplyView) {
	id := noticeBuildFailedPrefix + kernel
	l.mu.Lock()
	_, had := l.failed[id]
	failed := view.Result != nil && !view.Result.OK
	switch {
	case failed:
		reason := view.Result.Message
		if reason == "" {
			reason = view.Result.Code
		}
		l.failed[id] = NoticeView{ID: id, Kind: noticeError, Kernel: kernel, Reason: reason}
	default:
		delete(l.failed, id)
	}
	l.mu.Unlock()
	if failed || had {
		l.broker.Publish(Event{Type: EventNotices, Data: NoticesEvent{Notices: l.noticeViews(l.store.Snapshot())}})
	}
}

// NoteBuildResult отражает итог фонового запуска RunLive в уведомлении
// build_failed:<ядро> так же, как это делает сборка после установки ядра:
// неудача ставит уведомление, успех снимает прежнее. Какие итоги считать
// неудачей, решает вызывающий сервис. Неизвестное ядро игнорируется.
func (l *Layer) NoteBuildResult(kernel string, view ApplyView) {
	if kernel != KernelXray && kernel != KernelMihomo {
		return
	}
	l.noteBuildResult(kernel, view)
}

// OnKernelInstalled — хук установки ядра (D-18): в фоне собирает для него файлы
// из применённого состояния. Запуск ограничен этим ядром: файлы, сироты и
// ручные правки другого ядра не трогаются, а нерешённый дрейф самого ядра
// блокирует сборку (D-11). Ядро не запускается: решение о перезапуске принимает
// тот же Preview конвейера, остановленное и неактивное ядро не трогается.
// Неудача — уведомление build_failed:<ядро>.
func (l *Layer) OnKernelInstalled(kernel string) {
	if (kernel != KernelXray && kernel != KernelMihomo) || !l.Enabled() {
		return
	}
	l.invalidateVersions()
	// Версия и состав списков ядра меняют итог проверок черновика.
	l.InvalidateChecks()
	l.spawn(func() {
		release, err := l.pipeline.TryBegin(l.ctx, false)
		if err != nil {
			log.Printf("[configlayer] сборка после установки ядра %s не запущена: %v", utils.SanitizeLogInput(kernel), err)
			return
		}
		defer release()
		req := ApplyRequest{Trigger: TriggerKernelInstalled, Source: SourceApplied, Kernel: kernel}
		view := l.runPipeline(req)
		release()
		l.invalidateVersions()
		l.InvalidateChecks()
		l.noteBuildResult(kernel, view)
		l.afterRun()
		l.fireAfterApply(req, view)
	})
}

// --- защита путей Редактора и перечитывание ---

// IsManagedPath — абсолютный путь указывает на файл, которым владеет панель
// (запись манифеста managed). Пути сравниваются после полного разворачивания
// симлинков: путь через симлинк-каталог на корень ядра и симлинк на панельный
// файл (config.yaml → profiles/xcp-*.yaml) распознаются; отпущенные файлы и
// выключенный слой дают false. Нужен Редактору (D-12): панельные файлы там только
// на чтение.
func (l *Layer) IsManagedPath(absPath string) bool {
	if !l.Enabled() || !filepath.IsAbs(absPath) {
		return false
	}
	target := resolveFullSymlinks(absPath)
	// Как в evaluate и конвейере: записи ядра без бинарника не учитываются,
	// иначе файл защищён, а интерфейс его не показывает (WR-05).
	manifest := manifestOfScope(l.store.Snapshot().Manifest, l.installed(), "")
	for _, e := range manifest {
		if e.Status != StatusManaged {
			continue
		}
		abs, err := l.opts.Roots.Abs(e.Kernel, e.RelPath)
		if err != nil {
			continue
		}
		if resolveFullSymlinks(abs) == target {
			return true
		}
	}
	return false
}

// resolveFullSymlinks разворачивает симлинки пути целиком, включая сам файл;
// файла нет — как resolveDirSymlinks.
func resolveFullSymlinks(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return resolveDirSymlinks(p)
}

// resolveDirSymlinks нормализует путь и разворачивает симлинки в его каталоге
// (сам файл может отсутствовать).
func resolveDirSymlinks(p string) string {
	dir, base := filepath.Split(filepath.Clean(p))
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Join(dir, base)
}

// RestoreExternally выполняет fn — подмену каталога данных панели (восстановление
// снимка), — удерживая хранилище: правки черновика и запись конвейера ждут,
// поэтому восстановленный файл состояния не будет затёрт состоянием из памяти.
// После fn состояние перечитывается, рассылается snapshot и запрашивается сверка
// дрейфа. Выключенный слой диск не читает: только забывает кэш в памяти.
//
// applyMu не берётся: вызывающий уже держит замок жизненного цикла (порядок
// applyMu → lifecycleMu нарушать нельзя).
func (l *Layer) RestoreExternally(fn func() error) error {
	if !l.Enabled() {
		// фоновая запись черновика, начатая до выключения, не ляжет поверх снимка
		l.store.Flush()
		err := fn()
		l.store.Invalidate()
		return err
	}
	fnErr, reloadErr := l.store.ReplaceExternally(fn)
	// Восстановленный файл состояния мог дать ту же ревизию с другим черновиком.
	l.mu.Lock()
	l.checksEpoch++
	l.mu.Unlock()
	if reloadErr != nil {
		log.Printf("[configlayer] не удалось перечитать состояние слоя после восстановления: %v", reloadErr)
	}
	l.broker.Publish(Event{Type: EventSnapshot, Data: l.Snapshot()})
	l.RequestCheck()
	return fnErr
}
