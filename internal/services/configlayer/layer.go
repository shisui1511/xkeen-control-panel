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
)

// Ошибки действий слоя.
var (
	ErrDisabled        = errors.New("слой конфигурации выключен")
	ErrDriftBlocked    = errors.New("есть файлы с расхождением: решите их судьбу перед применением")
	ErrUnknownKey      = errors.New("неизвестный ключ файла")
	ErrDevModeRequired = errors.New("действие доступно только в режиме разработчика")
	ErrFileReleased    = errors.New("файл отпущен: панель его не сверяет")
	ErrStopped         = errors.New("слой остановлен")
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
	// checkReq — запросы внеочередной сверки (буфер 1: повторные сливаются).
	checkReq chan struct{}

	mu      sync.Mutex
	stopped bool
	// lastFiles — JSON последнего результата сверки; nil — базы нет.
	lastFiles []byte
	// failed — уведомления build_failed:<ядро> (только в памяти).
	failed map[string]NoticeView
	// Кэш версий ядер.
	verAt time.Time
	// verGen растёт при каждом сбросе кэша: результат вычисления, начатого до
	// сброса, в кэш не попадает.
	verGen      uint64
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
	// Свежая сверка под замком применения: пока есть нерешённый дрейф, диск не
	// трогается (D-11). «Пересобрать» по названным файлам этим не блокируется.
	if l.checkNow(true).DriftCount > 0 {
		release()
		return ErrDriftBlocked
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
// коммита, уведомления (запуск мог восстановить файлы по журналу) и внеочередная
// сверка.
func (l *Layer) afterRun() {
	st := l.store.Snapshot()
	l.broker.Publish(Event{Type: EventDraft, Data: draftEventOf(st)})
	l.broker.Publish(Event{Type: EventNotices, Data: NoticesEvent{Notices: l.noticeViews(st)}})
	l.checkNow(true)
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
		l.pipeline.Run(l.ctx, ApplyRequest{Trigger: TriggerRebuild, Source: SourceApplied, Only: only})
		release()
		l.afterRun()
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

// DismissNotice закрывает уведомление: schema_reset и recovered_from_journal
// сохраняются в файле состояния, build_failed:<ядро> живёт только в памяти.
// Неизвестный идентификатор — ErrUnknownNotice. Возвращает оставшиеся
// уведомления и публикует событие notices.
func (l *Layer) DismissNotice(id string) ([]NoticeView, error) {
	switch {
	case id == "schema_reset" || id == "recovered_from_journal":
		if err := l.store.DismissNotice(id); err != nil {
			return nil, err
		}
	case isBuildFailedID(id):
		l.mu.Lock()
		delete(l.failed, id)
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
	l.spawn(func() {
		release, err := l.pipeline.TryBegin(l.ctx, false)
		if err != nil {
			log.Printf("[configlayer] сборка после установки ядра %s не запущена: %v", kernel, err)
			return
		}
		defer release()
		view := l.pipeline.Run(l.ctx, ApplyRequest{Trigger: TriggerKernelInstalled, Source: SourceApplied, Kernel: kernel})
		release()
		l.invalidateVersions()
		l.noteBuildResult(kernel, view)
		l.afterRun()
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
		err := fn()
		l.store.Invalidate()
		return err
	}
	fnErr, reloadErr := l.store.ReplaceExternally(fn)
	if reloadErr != nil {
		log.Printf("[configlayer] не удалось перечитать состояние слоя после восстановления: %v", reloadErr)
	}
	l.broker.Publish(Event{Type: EventSnapshot, Data: l.Snapshot()})
	l.RequestCheck()
	return fnErr
}
