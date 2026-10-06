package configlayer

import (
	"errors"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// KernelApplier — единый источник решения «перезапускать ли ядро» и сам
// перезапуск (*services.KernelApplier).
type KernelApplier interface {
	Preview(targets ...string) services.ApplyResult
	ApplyLocked(targets ...string) services.ApplyResult
}

// MihomoControl — управление Mihomo через Clash API (*services.MihomoService).
type MihomoControl interface {
	ReloadConfig(configPath string) error
}

// LifecycleLocker — замок жизненного цикла ядра (*sync.Mutex подходит).
type LifecycleLocker interface {
	TryLock() bool
	Lock()
	Unlock()
}

// Тайминги подтверждения перезапуска по процессу ядра (A2).
var (
	RestartConfirmTimeout = 90 * time.Second
	RestartPollInterval   = 500 * time.Millisecond
	RestartStableWindow   = 5 * time.Second
)

// Ошибки TryBegin.
var (
	ErrApplyBusy  = errors.New("применение уже идёт")
	ErrKernelBusy = errors.New("идёт другая операция с ядром")
)
