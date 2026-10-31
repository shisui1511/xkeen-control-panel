package configlayer

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Дамп конфигурации Xray (D-28). Xray сливает log, dns, policy и «прочие» объекты
// из нескольких файлов каталога вглубь, поэтому действующее значение этих секций
// знает только само ядро: `xray run -dump -confdir <каталог>` печатает слитый
// конфиг. Панель не воспроизводит правила слияния ядра, а берёт значения из
// дампа. Дамп только читает каталог и ничего не запускает: сервер не стартует.

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

// XrayDumper — XrayDump с кэшем на подпись каталога: дамп на слабом железе
// занимает секунды, а скан секций просят и конвейер, и страницы. Подпись —
// бинарник и {имя, md5} всех *.json верхнего уровня каталога (включая файлы
// панели): изменился любой файл — дамп считается заново. Кэшируются удачный
// дамп и «дамп недоступен»; отказ ядра и таймаут не кэшируются. Безопасен для
// параллельных вызовов.
type XrayDumper struct {
	mu   sync.Mutex
	sig  string
	dump map[string]json.RawMessage
	err  error
}

// Dump возвращает дамп каталога dir; вызывающий результат не изменяет.
func (d *XrayDumper) Dump(ctx context.Context, bin, dir string, env func(dir string) []string) (map[string]json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sig := xrayDirSignature(bin, dir)
	if sig != "" && sig == d.sig {
		return d.dump, d.err
	}
	dump, err := XrayDump(ctx, bin, dir, env)
	d.sig, d.dump, d.err = "", nil, nil
	if sig != "" && (err == nil || errors.Is(err, ErrXrayDumpUnavailable)) {
		d.sig, d.dump, d.err = sig, dump, err
	}
	return dump, err
}

// xrayDirSignature считает подпись каталога для кэша дампа; пустая строка —
// подпись не посчитана (каталог или файл не читается), кэш не используется.
func xrayDirSignature(bin, dir string) string {
	if bin == "" || dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	h := md5.New()
	h.Write([]byte(bin))
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := readLimited(filepath.Join(dir, name), maxXrayKeyFileBytes)
		if errors.Is(err, fs.ErrNotExist) {
			continue // оборванный симлинк
		}
		if err != nil {
			return ""
		}
		h.Write([]byte{0})
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(HashContent(data)))
	}
	return hex.EncodeToString(h.Sum(nil))
}
