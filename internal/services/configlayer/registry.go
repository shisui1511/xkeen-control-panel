package configlayer

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

// Build вызывает генераторы и собирает желаемый набор файлов.
func (r *Registry) Build(src Sections, installed InstalledKernels) ([]GeneratedFile, error) {
	return nil, nil
}
