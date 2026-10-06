package configlayer

import (
	"context"
	"encoding/json"
	"errors"
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

// Options — зависимости и настройки слоя.
type Options struct {
	DataDir        string
	Roots          Roots
	Enabled        func() bool
	DevMode        func() bool
	Binaries       func() Binaries
	XrayEnv        func(dir string) []string
	ForeignOwned   func(kernel, rel string) bool
	KernelVersions func() []KernelVersionInput
	Applier        KernelApplier
	ProcessStates  func() []services.KernelProcessState
	Mihomo         MihomoControl
	MihomoAPIReady func() bool
	Lifecycle      LifecycleLocker
	Now            func() time.Time
	DriftInterval  time.Duration
	DebounceDelay  time.Duration
}

// Layer — фасад слоя «Конфигурация».
type Layer struct {
	opts     Options
	registry *Registry
}

// New создаёт слой.
func New(opts Options) (*Layer, error) {
	return &Layer{opts: opts, registry: NewRegistry()}, nil
}

// Start запускает фоновую сверку.
func (l *Layer) Start() {}

// Stop останавливает слой.
func (l *Layer) Stop() {}

// Enabled — включён ли слой.
func (l *Layer) Enabled() bool { return false }

// Subscribe подписывает на события шины.
func (l *Layer) Subscribe() (<-chan Event, func(), error) { return nil, nil, errNotImplemented }

// Snapshot — состояние слоя.
func (l *Layer) Snapshot() SnapshotView { return SnapshotView{} }

// EditDraft правит секцию черновика.
func (l *Layer) EditDraft(rev int64, section string, value json.RawMessage) (DraftEvent, error) {
	return DraftEvent{}, errNotImplemented
}

// ResetDraft сбрасывает черновик к применённому состоянию.
func (l *Layer) ResetDraft(rev int64) (DraftEvent, error) { return DraftEvent{}, errNotImplemented }

// StartApply запускает применение черновика.
func (l *Layer) StartApply(user bool) error { return errNotImplemented }

// Rebuild пересобирает названные файлы из применённого состояния.
func (l *Layer) Rebuild(keys []string, all bool) error { return errNotImplemented }

// Release отпускает файл.
func (l *Layer) Release(key string) (FilesEvent, error) { return FilesEvent{}, errNotImplemented }

// DismissNotice закрывает уведомление.
func (l *Layer) DismissNotice(id string) ([]NoticeView, error) { return nil, errNotImplemented }

// Diag выполняет диагностическое действие над черновиком.
func (l *Layer) Diag(rev int64, action string) (DraftEvent, error) {
	return DraftEvent{}, errNotImplemented
}

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
