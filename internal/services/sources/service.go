package sources

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Ошибки сервиса.
var (
	// ErrPreviewExpired — токен предпросмотра неизвестен, использован или устарел.
	ErrPreviewExpired = errors.New("sources: предпросмотр устарел")
	// ErrNodeNotFound — записи «Моих узлов» с таким id нет.
	ErrNodeNotFound = errors.New("sources: узел не найден")
	// ErrSourceNotFound — источника с таким id нет.
	ErrSourceNotFound = errors.New("sources: источник не найден")
	// ErrInvalidInput — недопустимый ввод или повреждённая секция.
	ErrInvalidInput = errors.New("sources: недопустимый ввод")
)

// Deps — зависимости сервиса.
type Deps struct {
	// DataDir — каталог данных панели.
	DataDir string
	// Layer — слой «Конфигурация»: черновик, живые данные, реестр генераторов.
	Layer *configlayer.Layer
	// Installed — какие ядра установлены сейчас.
	Installed func() configlayer.InstalledKernels
	// KernelVersion — сырая версия ядра ("xray" или "mihomo"); пустая строка —
	// версия не определена (функции считаются доступными).
	KernelVersion func(kernel string) string
	// Now — источник времени (nil — time.Now).
	Now func() time.Time
	// Rand — источник случайности для токенов и идентификаторов (nil — crypto/rand).
	Rand io.Reader
	// HWID — общий HWID роутера для запросов к провайдерам (nil — HWID из
	// каталога данных, общий со старым слоем подписок). Вызывается только при
	// запросе подписки: конструктор диска и сети не трогает.
	HWID func() string
	// Device — модель, ОС и версия ОС для заголовков устройства (nil — данные
	// прошивки, определяются лениво при первом запросе).
	Device func() (model, osName, osVersion string)
	// Client — HTTP-клиент запросов к провайдерам (nil — utils.SafeHTTPClient с
	// защитой от SSRF).
	Client func(timeout time.Duration) *http.Client
	// FetchTimeout — таймаут одного запроса к провайдеру (0 — 30 с, на
	// mips/mipsle 60 с).
	FetchTimeout time.Duration
	// TickInterval — период проверки сроков обновления подписок (0 — минута).
	TickInterval time.Duration
	// RetryDelays — задержки повторов после сетевой ошибки (D-30); nil — 5, 15 и
	// 60 минут. Пустой, но не nil набор отключает повторы.
	RetryDelays []time.Duration
	// ActiveKernel — активное ядро для выбора ядра просмотра по умолчанию (D-16):
	// "xray", "mihomo" или любое другое значение («none», «both», пусто — активного
	// нет). nil — активного ядра нет. Должна быть дешёвой: вызывается при каждом
	// построении представления.
	ActiveKernel func() string
	// Running сообщает, запущено ли ядро ("xray" или "mihomo"). nil — не запущено.
	Running func(kernel string) bool
}

// Значения по умолчанию планировщика.
const (
	defaultTickInterval = time.Minute
	// defaultIntervalHours — интервал обновления, когда его не задал ни
	// пользователь, ни провайдер (D-05).
	defaultIntervalHours = 12
)

// defaultRetryDelays — повторы после сетевой ошибки: 5, 15 и 60 минут, затем по
// обычному интервалу (D-30).
var defaultRetryDelays = []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour}

// Service — сервис источников. Создаётся один раз в cmd/xcp/main.go.
type Service struct {
	d Deps

	// pmu защищает кэш токенов предпросмотра.
	pmu      sync.Mutex
	previews map[string]*previewEntry
	// order — токены от старейшего к новейшему (вытеснение при переполнении).
	order []string

	// mmu защищает мемоизацию разбора записей и результатов генераторов.
	mmu    sync.Mutex
	parsed map[string]parsedEntry
	gens   map[string]memoFile
	// genOrder — ключи gens от старейшего к новейшему.
	genOrder []string
	// parses — сколько раз реально разбирался исходный текст записи.
	parses atomic.Int64

	// blobs — снимки наборов узлов подписок по хэшу и диагностика ответов.
	blobs blobStore

	// vmu защищает кэш разобранных снимков представления (view.go).
	vmu       sync.Mutex
	viewNodes map[string][]nodes.Node
	viewOrder []string

	xray   xrayGen
	mihomo mihomoGen

	// ctx — контекст жизни сервиса: отменяется Stop. Фоновые обновления и запросы
	// к провайдерам привязаны к нему, а не к HTTP-запросу.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	// lmu защищает stopped: после Stop горутины не запускаются (иначе wg.Add
	// гонялся бы с wg.Wait).
	lmu       sync.Mutex
	stopped   bool
	startOnce sync.Once
	stopOnce  sync.Once
	// startupDone — стартовые действия (уборка, уведомления, отложенные записи)
	// выполнены: они ждут включения слоя.
	startupDone atomic.Bool

	// cmu защищает calls и backoffs: одновременные обновления одного источника
	// сливаются.
	cmu   sync.Mutex
	calls map[string]*refreshCall
	// backoffs — откат планировщика после внутреннего сбоя обновления (scheduler.go).
	backoffs map[string]backoff
	// refreshMu — один источник за раз: обновления, повторы отложенных записей и
	// сборка после установки ядра идут по очереди (T-145-25).
	refreshMu sync.Mutex
	// nmu сериализует сборку уведомлений.
	nmu sync.Mutex

	// emu защищает draftExtras.
	emu sync.Mutex
	// draftExtras — хук секций, которые «Добавить» кладёт в ту же правку черновика
	// (группы по умолчанию при первом источнике); nil — хука нет.
	draftExtras DraftExtras
}

// DraftExtras — хук «Добавить»: по состоянию слоя до правки и идентификаторам
// источников, которые эта правка добавляет («my» — если «Мои узлы» появляются в
// черновике впервые, и id новых подписок), возвращает секции, которые войдут в ту
// же правку черновика (одна ревизия). Секции src_* недопустимы; nil-значение
// удаляет секцию. Ошибка хука — ошибка «Добавить» без правки черновика.
type DraftExtras func(st configlayer.StateView, added []string) (map[string]json.RawMessage, error)

// SetDraftExtras задаёт хук секций для «Добавить» (nil снимает его). Вызывается
// при сборке в cmd/xcp/main.go до Start; гонок с «Добавить» нет, хук читается под
// мьютексом.
func (s *Service) SetDraftExtras(fn DraftExtras) {
	s.emu.Lock()
	s.draftExtras = fn
	s.emu.Unlock()
}

// extrasHook — текущий хук секций (nil — не задан).
func (s *Service) extrasHook() DraftExtras {
	s.emu.Lock()
	defer s.emu.Unlock()
	return s.draftExtras
}

// New создаёт сервис. Слой и источник «установленных ядер» обязательны.
func New(d Deps) (*Service, error) {
	if d.Layer == nil {
		return nil, errors.New("sources: не задан слой конфигурации")
	}
	if d.Installed == nil {
		return nil, errors.New("sources: не задан источник установленных ядер")
	}
	if d.KernelVersion == nil {
		d.KernelVersion = func(string) string { return "" }
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Rand == nil {
		d.Rand = rand.Reader
	}
	if d.HWID == nil {
		dataDir := d.DataDir
		d.HWID = func() string { return services.LoadOrGenerateHWID(dataDir) }
	}
	if d.Device == nil {
		d.Device = services.NewDeviceInfo().Get
	}
	if d.Client == nil {
		d.Client = utils.SafeHTTPClient
	}
	if d.FetchTimeout <= 0 {
		d.FetchTimeout = defaultFetchTimeout
		if runtime.GOARCH == "mips" || runtime.GOARCH == "mipsle" || runtime.GOARCH == "mips64" || runtime.GOARCH == "mips64le" {
			d.FetchTimeout = slowFetchTimeout
		}
	}
	if d.ActiveKernel == nil {
		d.ActiveKernel = func() string { return "" }
	}
	if d.Running == nil {
		d.Running = func(string) bool { return false }
	}
	if d.TickInterval <= 0 {
		d.TickInterval = defaultTickInterval
	}
	if d.RetryDelays == nil {
		d.RetryDelays = defaultRetryDelays
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Конструктор диска и сети не трогает: каталог снимков создаёт первая запись.
	s := &Service{
		d:         d,
		ctx:       ctx,
		cancel:    cancel,
		calls:     map[string]*refreshCall{},
		backoffs:  map[string]backoff{},
		previews:  map[string]*previewEntry{},
		parsed:    map[string]parsedEntry{},
		gens:      map[string]memoFile{},
		viewNodes: map[string][]nodes.Node{},
		blobs: blobStore{
			dir:     filepath.Join(d.DataDir, "config_layer", "snapshots"),
			diagDir: filepath.Join(d.DataDir, "config_layer", "diag"),
			reg:     newBlobRegistry(),
		},
	}
	s.xray = xrayGen{s: s}
	s.mihomo = mihomoGen{s: s}
	return s, nil
}

// spawn запускает фоновую горутину сервиса под wg; после Stop не запускает.
func (s *Service) spawn(fn func()) bool {
	s.lmu.Lock()
	if s.stopped {
		s.lmu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.lmu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// OnKernelInstalled — хук установки ядра (D-01): панель сама запрашивает
// подписки под это ядро и в фоне собирает его файлы. Выключенный слой — возврат до
// горутины: ни запросов к провайдерам, ни чтения состояния, ни каталогов на диске.
// Запущенное ядро решает конвейер (Xray перезапускается, файл Mihomo пишется без
// перезагрузки); остановленное не запускается.
func (s *Service) OnKernelInstalled(kernel string) {
	if kernel != kernelXray && kernel != kernelMihomo {
		return
	}
	if !s.d.Layer.Enabled() {
		return
	}
	s.spawn(func() {
		st, err := s.d.Layer.ReadState()
		if err != nil {
			return
		}
		for _, id := range subscriptionIDs(st, false) {
			if s.ctx.Err() != nil {
				return
			}
			if _, err := s.refreshCurrent(s.ctx, id, []string{kernel}); err != nil && !errors.Is(err, ErrSourceDisabled) {
				log.Printf("[sources] сборка источника %s после установки ядра %s не выполнена: %v", id, kernel, err)
			}
		}
	})
}

// OnDriftResolved — колбэк слоя «дрейф решён» (Layer.OnDriftResolved): отложенные
// записи подписок проходят сами, без нового запроса к провайдеру (D-27).
func (s *Service) OnDriftResolved() {
	s.spawn(func() { s.retryDeferred(s.ctx) })
}

// Generators — генераторы файлов источников для реестра слоя
// (Layer.RegisterGenerator до Start): sources_xray и sources_mihomo.
func (s *Service) Generators() []configlayer.Generator {
	return []configlayer.Generator{s.xray, s.mihomo}
}

// randMu сериализует чтение источника случайности: io.Reader не обязан быть
// безопасным для параллельных вызовов.
var randMu sync.Mutex

func (s *Service) newID() (string, error) {
	randMu.Lock()
	defer randMu.Unlock()
	return newID(s.d.Rand)
}

// uniqueID — id, которого нет среди занятых.
func (s *Service) uniqueID(taken map[string]bool) (string, error) {
	for range 16 {
		id, err := s.newID()
		if err != nil {
			return "", err
		}
		if !taken[id] {
			return id, nil
		}
	}
	return "", errors.New("sources: не удалось подобрать уникальный идентификатор")
}

// parsedEntry — результат разбора исходного текста записи.
type parsedEntry struct {
	node nodes.Node
	err  *nodes.SourceError
}

// maxParsed — предел мемоизации разбора записей.
const maxParsed = 4096

// parseRaw разбирает исходный текст записи «Моих узлов»: ровно один узел, иначе
// ошибка. Результат мемоизируется по sha256 текста (сборка идёт при каждой сверке
// слоя, разбор больших текстов на MIPS не бесплатен).
func (s *Service) parseRaw(raw string) (nodes.Node, *nodes.SourceError) {
	key := hash(raw)
	s.mmu.Lock()
	if p, ok := s.parsed[key]; ok {
		s.mmu.Unlock()
		return p.node, p.err
	}
	s.mmu.Unlock()

	s.parses.Add(1)
	n, serr := parseSingle(raw)

	s.mmu.Lock()
	if len(s.parsed) >= maxParsed {
		s.parsed = map[string]parsedEntry{}
	}
	s.parsed[key] = parsedEntry{node: n, err: serr}
	s.mmu.Unlock()
	return n, serr
}

// parseSingle распознаёт текст и требует ровно один узел (D-13).
func parseSingle(text string) (nodes.Node, *nodes.SourceError) {
	rec := nodes.Recognize(nodes.Input{Text: text})
	if len(rec.Nodes) != 1 || len(rec.URLs) != 0 {
		// Ошибки строк в ответ не подмешиваются: они могут нести фрагмент ввода.
		return nodes.Node{}, nodes.NewError(nodes.CodeEditSingleNode)
	}
	return rec.Nodes[0], nil
}
