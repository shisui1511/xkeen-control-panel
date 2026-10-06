package configlayer

import (
	"context"
	"runtime"
	"time"
)

// defaultValidateTimeout — время на проверку одним ядром для платформы.
func defaultValidateTimeout(goarch string) time.Duration {
	return 60 * time.Second
}

// ValidateTimeout — время на проверку одним ядром (A1: значения не проверены
// на MIPS, поэтому переменная).
var ValidateTimeout = defaultValidateTimeout(runtime.GOARCH)

// ValidateWaitDelay — сколько ждать закрытия вывода после убийства процесса.
var ValidateWaitDelay = 2 * time.Second

// Binaries — пути бинарников ядер; пустая строка — ядро не установлено.
type Binaries struct {
	Xray   string
	Mihomo string
}

// ValidationResult — итог проверки одним ядром.
type ValidationResult struct {
	Kernel   string
	OK       bool
	Skipped  bool
	NoteCode string
	Code     string
	Message  string
	HintCode string
}

// ValidateXray проверяет план ядром Xray во временном каталоге.
func ValidateXray(ctx context.Context, tmpBase string, roots Roots, bin string, plan Plan, env func(dir string) []string) ValidationResult {
	return ValidationResult{Kernel: KernelXray}
}

// ValidateMihomo проверяет план ядром Mihomo по копии каталога.
func ValidateMihomo(ctx context.Context, tmpBase string, roots Roots, bin string, plan Plan) ValidationResult {
	return ValidationResult{Kernel: KernelMihomo}
}
