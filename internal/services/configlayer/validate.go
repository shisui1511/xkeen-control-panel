package configlayer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// defaultValidateTimeout — время на проверку одним ядром для платформы.
//
// На MIPS (mips, mipsle) проверка с большими geosite/geoip идёт заметно дольше,
// поэтому там 180 с, на остальных платформах 60 с (допущение A1 не проверено на
// железе: значения подкручиваются через ValidateTimeout).
func defaultValidateTimeout(goarch string) time.Duration {
	switch goarch {
	case "mips", "mipsle":
		return 180 * time.Second
	}
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
	return finishValidation(KernelXray, cctx, runErr, out, tmp, roots.Xray)
}

// ValidateMihomo проверяет план ядром Mihomo: `<mihomo> -t -d <tmp> -f
// <tmp>/config.yaml` по копии каталога Mihomo. config.yaml в копии — обычный
// файл с содержимым разрешённого симлинка (если симлинк указывает на файл,
// который пишет план, берутся байты из плана: проверяется то, что ядро прочтёт
// после записи), остальные конфигурации верхнего уровня копируются, geodata
// (*.dat, *.metadb, *.mmdb) и файлы подкаталогов (кроме backups) подключаются
// симлинками, а overlayPlan заменяет в копии файлы плана. Нет config.yaml —
// проверка пропущена (no_config).
func ValidateMihomo(ctx context.Context, tmpBase string, roots Roots, bin string, plan Plan) ValidationResult {
	if bin == "" {
		return skippedResult(KernelMihomo, NoteKernelNotInstalled)
	}
	if !plan.Changes(KernelMihomo) {
		return skippedResult(KernelMihomo, NoteNoChanges)
	}
	cfg, err := filepath.EvalSymlinks(filepath.Join(roots.Mihomo, "config.yaml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return skippedResult(KernelMihomo, NoteNoConfig)
		}
		return notRun(KernelMihomo, err)
	}
	tmp, err := makeTmp(tmpBase)
	if err != nil {
		return notRun(KernelMihomo, err)
	}
	defer os.RemoveAll(tmp)

	if err := mirrorMihomo(roots.Mihomo, cfg, plannedContent(roots.Mihomo, cfg, plan), tmp); err != nil {
		return notRun(KernelMihomo, err)
	}
	if err := overlayPlan(tmp, plan, KernelMihomo); err != nil {
		return notRun(KernelMihomo, err)
	}

	cctx, cancel := context.WithTimeout(ctx, ValidateTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-t", "-d", tmp, "-f", filepath.Join(tmp, "config.yaml"))
	cmd.WaitDelay = ValidateWaitDelay
	out, runErr := cmd.CombinedOutput()
	return finishValidation(KernelMihomo, cctx, runErr, out, tmp, roots.Mihomo)
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

// maxMessageBytes — предел длины сообщения ядра (хвост, где обычно причина).
const maxMessageBytes = 4000

// finishValidation превращает итог запуска ядра в ValidationResult. Сообщение
// очищается: без ANSI, путь временного каталога заменён рабочим корнем, длина
// ограничена.
func finishValidation(kernel string, cctx context.Context, runErr error, out []byte, tmp, workRoot string) ValidationResult {
	if runErr == nil {
		return ValidationResult{Kernel: kernel, OK: true}
	}
	// Таймаут — не «конфиг неверен»: у него свой код, и файлы не пишутся.
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return ValidationResult{
			Kernel:  kernel,
			Code:    CodeValidationTimeout,
			Message: fmt.Sprintf("проверка не уложилась в %d с", int(ValidateTimeout.Seconds())),
		}
	}
	if cctx.Err() != nil {
		return ValidationResult{Kernel: kernel, Code: CodeValidationNotRun, Message: "проверка отменена"}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		msg := sanitizeOutput(string(out), tmp, workRoot)
		return ValidationResult{Kernel: kernel, Code: CodeValidationFailed, Message: msg, HintCode: hintFor(kernel, msg)}
	}
	return notRun(kernel, runErr)
}

// sanitizeOutput готовит вывод ядра к показу в UI.
func sanitizeOutput(out, tmp, workRoot string) string {
	out = utils.StripANSI(out)
	if tmp != "" {
		out = strings.ReplaceAll(out, tmp, workRoot)
	}
	out = condenseOutput(out)
	out = strings.TrimSpace(out)
	if len(out) > maxMessageBytes {
		out = out[len(out)-maxMessageBytes:]
		for len(out) > 0 && !utf8.RuneStart(out[0]) {
			out = out[1:]
		}
	}
	return out
}

// logTimestamp — метка времени в начале строки журнала Xray.
var logTimestamp = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? `)

// condenseOutput убирает из вывода ядра шум, за которым не видно причины отказа:
// строки журнала уровней Info и Debug (перед отказом Xray пишет по строке на
// каждый узел пользователя) и повторы одной и той же строки (предупреждение
// о gRPC выводится для каждого узла). Причина стоит в конце, а блок вывода
// показывается с начала, с ограничением высоты. Если кроме Info и Debug в
// выводе ничего нет, он остаётся целым.
func condenseOutput(out string) string {
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	seen := make(map[string]bool, len(lines))
	for _, l := range lines {
		if strings.Contains(l, " [Info] ") || strings.Contains(l, " [Debug] ") {
			continue
		}
		if key := logTimestamp.ReplaceAllString(l, ""); strings.TrimSpace(key) != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		kept = append(kept, l)
	}
	res := strings.Join(kept, "\n")
	if strings.TrimSpace(res) == "" {
		return out
	}
	return res
}

// hintFor подбирает код подсказки по известным формулировкам ядер.
func hintFor(kernel, out string) string {
	switch kernel {
	case KernelXray:
		switch {
		case strings.Contains(out, "unknown config id"):
			return "xray_unknown_protocol"
		case strings.Contains(out, "invalid character"),
			strings.Contains(out, "unexpected end of JSON"),
			strings.Contains(out, "failed to load config files"):
			return "xray_json_syntax"
		}
	case KernelMihomo:
		if strings.Contains(out, "yaml:") {
			return "mihomo_yaml_syntax"
		}
	}
	return ""
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
	// Сироты после применения исчезнут: в копии их тоже нет.
	for _, o := range plan.Orphans {
		if o.Kernel != kernel {
			continue
		}
		dst := filepath.Join(tmp, filepath.FromSlash(o.RelPath))
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// plannedContent возвращает байты из плана, если разрешённый путь config.yaml
// указывает на файл Mihomo, который план записывает (config.yaml → profiles/…);
// иначе nil. Без этого ядро проверяло бы старый профиль с диска (D-14).
func plannedContent(root, resolvedConfig string, plan Plan) []byte {
	for _, base := range []string{root, resolveDirSymlinks(root)} {
		rel, err := filepath.Rel(filepath.Clean(base), resolvedConfig)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, fp := range plan.Files {
			if fp.Kernel == KernelMihomo && fp.Action == ActionWrite && fp.RelPath == rel {
				return fp.Content
			}
		}
	}
	return nil
}

// mirrorMihomo строит зеркало каталога Mihomo (см. ValidateMihomo). override
// (не nil) — содержимое config.yaml вместо файла на диске.
func mirrorMihomo(root, resolvedConfig string, override []byte, tmp string) error {
	data := override
	if data == nil {
		var err error
		if data, err = os.ReadFile(resolvedConfig); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), data, 0o644); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("каталог Mihomo: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name == "config.yaml" {
			continue
		}
		src := filepath.Join(root, name)
		st, err := os.Stat(src)
		if err != nil {
			continue // оборванный симлинк
		}
		if st.IsDir() {
			// Резервные копии проверке не нужны; профили подключаются как остальные
			// подкаталоги, чтобы overlayPlan подменил в копии нужный файл.
			if name == "backups" {
				continue
			}
			if err := linkDir(src, filepath.Join(tmp, name), 0); err != nil {
				return err
			}
			continue
		}
		dst := filepath.Join(tmp, name)
		switch strings.ToLower(filepath.Ext(name)) {
		case ".yaml", ".yml", ".json":
			b, err := os.ReadFile(src)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dst, b, 0o644); err != nil {
				return err
			}
		case ".dat", ".metadb", ".mmdb":
			if err := os.Symlink(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

// maxMirrorDepth — предел вложенности зеркала подкаталогов (защита от петель).
const maxMirrorDepth = 8

// linkDir создаёт каталог dst с симлинками на файлы каталога src, рекурсивно.
func linkDir(src, dst string, depth int) error {
	if depth > maxMirrorDepth {
		return nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		st, err := os.Stat(s)
		if err != nil {
			continue
		}
		if st.IsDir() {
			if err := linkDir(s, filepath.Join(dst, e.Name()), depth+1); err != nil {
				return err
			}
			continue
		}
		if err := os.Symlink(s, filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
