package sources

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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

// Deps — зависимости сервиса. Поля для сети, планировщика и вида добавляют
// планы 145-08…145-10.
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
}

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

	xray   xrayGen
	mihomo mihomoGen
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
	s := &Service{
		d:        d,
		previews: map[string]*previewEntry{},
		parsed:   map[string]parsedEntry{},
		gens:     map[string]memoFile{},
	}
	s.xray = xrayGen{s: s}
	s.mihomo = mihomoGen{s: s}
	return s, nil
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
