package configlayer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// defaultValidateTimeout — время на проверку одним ядром для платформы.
func defaultValidateTimeout(goarch string) time.Duration {
	return 60 * time.Second
}

// ValidateTimeout — время на проверку одним ядром (A1: значения не проверены
// на MIPS, поэтому переменная).
var ValidateTimeout = defaultValidateTimeout(runtime.GOARCH)

// ValidateWaitDelay — сколько ждать закрытия вывода после убийства процесса:
// без этого внук ядра, держащий конвейер вывода, подвесил бы проверку.
var ValidateWaitDelay = 2 * time.Second

// Binaries — пути бинарников ядер; пустая строка — ядро не установлено.
type Binaries struct {
	Xray   string
	Mihomo string
}

// Коды итога проверки.
const (
	CodeValidationFailed  = "validation_failed"
	CodeValidationTimeout = "validation_timeout"
	CodeValidationNotRun  = "validation_not_run"
)

// Причины, по которым проверка пропущена.
const (
	NoteKernelNotInstalled = "kernel_not_installed"
	NoteNoChanges          = "no_changes"
	NoteNoConfig           = "no_config"
)

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

func skippedResult(kernel, note string) ValidationResult {
	return ValidationResult{Kernel: kernel, Skipped: true, NoteCode: note}
}

// ValidateXray проверяет план ядром Xray: `<xray> run -test -confdir <tmp>`,
// где tmp — копия *.json корня конфигураций Xray с наложенным планом. tmpBase
// (<data_dir>/tmp) создаётся с правами 0700, каталог проверки удаляется после
// запуска. env строит окружение запуска (nil — окружение панели).
func ValidateXray(ctx context.Context, tmpBase string, roots Roots, bin string, plan Plan, env func(dir string) []string) ValidationResult {
	if bin == "" {
		return skippedResult(KernelXray, NoteKernelNotInstalled)
	}
	if !plan.Changes(KernelXray) {
		return skippedResult(KernelXray, NoteNoChanges)
	}
	tmp, err := makeTmp(tmpBase)
	if err != nil {
		return notRun(KernelXray, err)
	}
	defer os.RemoveAll(tmp)

	if err := mirrorXray(roots.Xray, tmp); err != nil {
		return notRun(KernelXray, err)
	}
	if err := overlayPlan(tmp, plan, KernelXray); err != nil {
		return notRun(KernelXray, err)
	}

	cctx, cancel := context.WithTimeout(ctx, ValidateTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "run", "-test", "-confdir", tmp)
	if env != nil {
		cmd.Env = env(roots.Xray)
	}
	cmd.WaitDelay = ValidateWaitDelay
	out, runErr := cmd.CombinedOutput()
	return finishValidation(KernelXray, cctx, runErr, out)
}

// ValidateMihomo проверяет план ядром Mihomo по копии каталога.
func ValidateMihomo(ctx context.Context, tmpBase string, roots Roots, bin string, plan Plan) ValidationResult {
	if bin == "" {
		return skippedResult(KernelMihomo, NoteKernelNotInstalled)
	}
	if !plan.Changes(KernelMihomo) {
		return skippedResult(KernelMihomo, NoteNoChanges)
	}
	return ValidationResult{Kernel: KernelMihomo, Code: CodeValidationNotRun, Message: "проверка Mihomo не реализована"}
}

// makeTmp создаёт каталог проверки <tmpBase>/apply-* с правами 0700.
func makeTmp(tmpBase string) (string, error) {
	if err := os.MkdirAll(tmpBase, 0o700); err != nil {
		return "", fmt.Errorf("каталог проверки: %w", err)
	}
	tmp, err := os.MkdirTemp(tmpBase, "apply-*")
	if err != nil {
		return "", fmt.Errorf("каталог проверки: %w", err)
	}
	return tmp, nil
}

func notRun(kernel string, err error) ValidationResult {
	return ValidationResult{Kernel: kernel, Code: CodeValidationNotRun, Message: err.Error()}
}

// finishValidation превращает итог запуска ядра в ValidationResult.
func finishValidation(kernel string, cctx context.Context, runErr error, out []byte) ValidationResult {
	if runErr == nil {
		return ValidationResult{Kernel: kernel, OK: true}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return ValidationResult{Kernel: kernel, Code: CodeValidationFailed, Message: string(out)}
	}
	return notRun(kernel, runErr)
}

// mirrorXray копирует *.json верхнего уровня корня Xray во временный каталог.
// Симлинки превращаются в обычные копии, подкаталоги и *.obsolete не копируются.
func mirrorXray(root, tmp string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("каталог Xray: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // оборванный симлинк
			}
			return err
		}
		if err := os.WriteFile(filepath.Join(tmp, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// overlayPlan накладывает план ядра на копию: существующая запись (в том числе
// симлинк) удаляется до записи, поэтому запись никогда не идёт сквозь симлинк в
// рабочий файл.
func overlayPlan(tmp string, plan Plan, kernel string) error {
	for _, fp := range plan.Files {
		if fp.Kernel != kernel {
			continue
		}
		dst := filepath.Join(tmp, filepath.FromSlash(fp.RelPath))
		switch fp.Action {
		case ActionWrite:
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, fp.Content, 0o644); err != nil {
				return err
			}
		case ActionDelete:
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
