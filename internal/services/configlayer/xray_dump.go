package configlayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Дамп конфигурации Xray (D-28). `xray run -dump -confdir <каталог>` печатает
// конфиг, слитый ядром. Дамп НОРМАЛИЗОВАН ядром (добавлены значения по умолчанию,
// сервера dns превращены в объекты и т. п.), поэтому значением секции для показа,
// переноса в файл панели и сверки с файлом он не служит: действующее значение
// панель считает сама (mergeXrayValue). Дамп — арбитр правил слияния в роутерных
// сценариях и диагностика (Layer.XrayDump). Дамп только читает каталог и ничего
// не запускает: сервер не стартует.

var (
	// ErrXrayDumpUnavailable — дамп получить нельзя: бинарника Xray нет или ядро
	// не знает флага -dump. Значение секций при этом неизвестно.
	ErrXrayDumpUnavailable = errors.New("дамп Xray недоступен")
	// ErrXrayDumpFailed — ядро отказалось сливать каталог (неверный файл, таймаут)
	// или вывод не разобрался. Значение секций при этом неизвестно.
	ErrXrayDumpFailed = errors.New("дамп Xray не получен")
)

// XrayDump сливает каталог dir ядром bin (`<bin> run -dump -confdir <dir>`) и
// возвращает значения ключей верхнего уровня слитого конфига. Результат
// непустой картой (возможно, без ключей) — nil возвращается только вместе с
// ошибкой. env строит окружение запуска (nil — окружение панели). Запуск не идёт
// во время операции XKeen над ядром (services.AcquireKernelHelper), время
// ограничено ValidateTimeout. Ошибка — ErrXrayDumpUnavailable (нет бинарника,
// нет флага -dump) или ErrXrayDumpFailed. Значения нигде не логируются.
func XrayDump(ctx context.Context, bin, dir string, env func(dir string) []string) (map[string]json.RawMessage, error) {
	if bin == "" {
		return nil, fmt.Errorf("%w: ядро не установлено", ErrXrayDumpUnavailable)
	}
	if dir == "" {
		return nil, fmt.Errorf("%w: каталог Xray не задан", ErrXrayDumpUnavailable)
	}
	defer services.AcquireKernelHelper(ctx)()
	cctx, cancel := context.WithTimeout(ctx, ValidateTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "run", "-dump", "-confdir", dir)
	if env != nil {
		cmd.Env = env(dir)
	}
	cmd.WaitDelay = ValidateWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: не уложился в %d с", ErrXrayDumpFailed, int(ValidateTimeout.Seconds()))
		}
		if cctx.Err() != nil {
			return nil, fmt.Errorf("%w: отменён", ErrXrayDumpFailed)
		}
		msg := sanitizeOutput(stderr.String(), "", dir)
		if strings.Contains(msg, "flag provided but not defined") {
			return nil, fmt.Errorf("%w: ядро без флага -dump", ErrXrayDumpUnavailable)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("%w: %s", ErrXrayDumpFailed, msg)
		}
		return nil, fmt.Errorf("%w: %v", ErrXrayDumpUnavailable, err)
	}
	out := stdout.Bytes()
	start := bytes.IndexByte(out, '{')
	if start < 0 {
		return nil, fmt.Errorf("%w: в выводе нет конфига", ErrXrayDumpFailed)
	}
	merged := map[string]json.RawMessage{}
	if err := json.NewDecoder(bytes.NewReader(out[start:])).Decode(&merged); err != nil {
		return nil, fmt.Errorf("%w: вывод не разобрался как JSON", ErrXrayDumpFailed)
	}
	return merged, nil
}
