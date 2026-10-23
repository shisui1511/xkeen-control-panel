package configlayer

import (
	"fmt"
	"sort"
	"sync"
)

// Expectation — что должно быть видно в Mihomo после перезагрузки провайдера:
// имя провайдера и число узлов или правил. Сверка идёт в конвейере (144-05).
type Expectation struct {
	// ProviderType — "proxies" или "rules".
	ProviderType string `json:"provider_type"`
	ProviderName string `json:"provider_name"`
	Count        int    `json:"count"`
}

// GeneratedFile — файл, который генератор хочет видеть в каталоге ядра.
type GeneratedFile struct {
	Kernel  string
	RelPath string
	Kind    FileKind
	Content []byte
	Expect  *Expectation
	// Inert — файл не влияет на запущенное ядро, пока на него не сослались
	// (провайдеры Mihomo до Phase 146): пишется, проверяется и сверяется, но не
	// перезапускает ядро.
	Inert bool
}

// Key — ключ записи манифеста этого файла.
func (f GeneratedFile) Key() string {
	return ManifestKey(f.Kernel, f.RelPath)
}

// InstalledKernels — какие ядра установлены на роутере.
type InstalledKernels struct {
	Xray   bool
	Mihomo bool
}

// Has сообщает, установлено ли ядро с таким именем.
func (k InstalledKernels) Has(kernel string) bool {
	switch kernel {
	case KernelXray:
		return k.Xray
	case KernelMihomo:
		return k.Mihomo
	}
	return false
}

// Input — входные данные генератора. Sections — секции черновика (Mode
// SourceDraft) или применённого состояния (Mode SourceApplied). Live — живые
// данные слоя (State.Live), которые генератор только читает и не меняет.
type Input struct {
	Sections Sections
	Live     Sections
	Mode     SourceKind
	// Routing — факты о маршрутизации (146-06): api-входящий в configs/, порты
	// прежнего профиля Mihomo, какие ядра уже взяты. Только для чтения.
	Routing RoutingFacts
}

// Generator — единственная точка расширения слоя для фаз 145–147: по входным
// данным выдаёт файлы ядер. Вывод обязан быть детерминированным.
type Generator interface {
	ID() string
	Generate(in Input, installed InstalledKernels) ([]GeneratedFile, error)
}

// Checker — проверка входных данных до записи: возвращает блокирующие проблемы
// (пустая группа, висячая ссылка). Непустой итог не даёт применить черновик и
// отвергает фоновый запуск конвейера. Проверка обязана терпеть любой вход, сама
// диск не читает и работает быстро: Snapshot считает её по ревизии черновика.
type Checker interface {
	ID() string
	Check(in Input, installed InstalledKernels) []BuildIssue
}

// Registry — упорядоченный набор генераторов и проверок.
type Registry struct {
	mu         sync.RWMutex
	generators []Generator
	checkers   []Checker
}

// NewRegistry создаёт пустой реестр.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register добавляет генератор в конец списка.
func (r *Registry) Register(g Generator) {
	r.mu.Lock()
	r.generators = append(r.generators, g)
	r.mu.Unlock()
}

// RegisterChecker добавляет проверку в конец списка.
func (r *Registry) RegisterChecker(c Checker) {
	r.mu.Lock()
	r.checkers = append(r.checkers, c)
	r.mu.Unlock()
}

// HasChecker сообщает, зарегистрирована ли проверка с таким ID.
func (r *Registry) HasChecker(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.checkers {
		if c.ID() == id {
			return true
		}
	}
	return false
}

// Check вызывает проверки по порядку регистрации и собирает все проблемы разом.
// Паника проверки не роняет слой: она становится проблемой checker_failed с ID
// проверки в Key. Итог отсортирован по (Key, Code) и никогда не nil.
func (r *Registry) Check(in Input, installed InstalledKernels) []BuildIssue {
	r.mu.RLock()
	checkers := append([]Checker(nil), r.checkers...)
	r.mu.RUnlock()
	out := []BuildIssue{}
	for _, c := range checkers {
		out = append(out, runChecker(c, in, installed)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// runChecker вызывает одну проверку, превращая панику в проблему checker_failed.
func runChecker(c Checker, in Input, installed InstalledKernels) (issues []BuildIssue) {
	defer func() {
		if rec := recover(); rec != nil {
			issues = []BuildIssue{{Key: c.ID(), Code: IssueCheckerFailed, Detail: fmt.Sprintf("%s: %v", c.ID(), rec)}}
		}
	}()
	return c.Check(in, installed)
}

// Has сообщает, зарегистрирован ли генератор с таким ID.
func (r *Registry) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, g := range r.generators {
		if g.ID() == id {
			return true
		}
	}
	return false
}

// Build вызывает генераторы по порядку регистрации и собирает желаемый набор
// файлов. Файлы ядер, которых нет в installed, отбрасываются. Один ключ у двух
// файлов — ошибка duplicate_file (перезаписи нет), ошибка генератора — ошибка
// generator_failed с ID генератора. Результат отсортирован по ключу: сборка
// детерминирована, иначе каждое «Применить» переписывало бы файлы.
func (r *Registry) Build(in Input, installed InstalledKernels) ([]GeneratedFile, error) {
	var out []GeneratedFile
	owner := make(map[string]string)
	var issues []BuildIssue
	r.mu.RLock()
	generators := append([]Generator(nil), r.generators...)
	r.mu.RUnlock()
	for _, g := range generators {
		files, err := g.Generate(in, installed)
		if err != nil {
			issues = append(issues, BuildIssue{Key: g.ID(), Code: IssueGeneratorFailed, Detail: fmt.Sprintf("%s: %v", g.ID(), err)})
			continue
		}
		for _, f := range files {
			if !installed.Has(f.Kernel) {
				continue
			}
			key := f.Key()
			if prev, dup := owner[key]; dup {
				issues = append(issues, BuildIssue{Key: key, Code: IssueDuplicateFile, Detail: prev + ", " + g.ID()})
				continue
			}
			owner[key] = g.ID()
			out = append(out, f)
		}
	}
	if len(issues) > 0 {
		return nil, &BuildError{Issues: issues}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}
