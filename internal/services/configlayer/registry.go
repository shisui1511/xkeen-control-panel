package configlayer

import (
	"fmt"
	"sort"
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

// Generator — единственная точка расширения слоя для фаз 145–147: по секциям
// состояния выдаёт файлы ядер. Вывод обязан быть детерминированным.
type Generator interface {
	ID() string
	Generate(src Sections, installed InstalledKernels) ([]GeneratedFile, error)
}

// Registry — упорядоченный набор генераторов.
type Registry struct {
	generators []Generator
}

// NewRegistry создаёт пустой реестр.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register добавляет генератор в конец списка.
func (r *Registry) Register(g Generator) {
	r.generators = append(r.generators, g)
}

// Build вызывает генераторы по порядку регистрации и собирает желаемый набор
// файлов. Файлы ядер, которых нет в installed, отбрасываются. Один ключ у двух
// файлов — ошибка duplicate_file (перезаписи нет), ошибка генератора — ошибка
// generator_failed с ID генератора. Результат отсортирован по ключу: сборка
// детерминирована, иначе каждое «Применить» переписывало бы файлы.
func (r *Registry) Build(src Sections, installed InstalledKernels) ([]GeneratedFile, error) {
	var out []GeneratedFile
	owner := make(map[string]string)
	var issues []BuildIssue
	for _, g := range r.generators {
		files, err := g.Generate(src, installed)
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
