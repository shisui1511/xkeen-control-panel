package geodata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

const (
	// OutputLimit — размер кольцевого буфера вывода по умолчанию.
	OutputLimit = 256 << 10
	// runLineLimit — самая длинная хранимая строка; остаток строки отбрасывается.
	runLineLimit = 8 << 10
	// runDefaultTimeout — сколько живёт запуск `xkeen -ug` (скачивание всех баз).
	runDefaultTimeout = 20 * time.Minute
	// runKillGrace — сколько ждать выхода после SIGTERM, прежде чем убить процесс.
	runKillGrace = 30 * time.Second
	// runDrainGrace — сколько ждать конец потока после выхода xkeen.
	runDrainGrace = 2 * time.Second
)

// ErrRunTimeout — запуск превысил Runner.Timeout.
var ErrRunTimeout = errors.New("geodata: xkeen run timed out")

// Output — потокобезопасный кольцевой буфер строк вывода. Смещения абсолютные
// (считаются от начала запуска): клиент дочитывает с from=N, а переполнение
// сдвигает только начало буфера.
type Output struct {
	mu    sync.Mutex
	limit int
	data  []byte
	start int64 // абсолютное смещение data[0]
}

// NewOutput создаёт буфер; limit <= 0 — OutputLimit.
func NewOutput(limit int) *Output {
	if limit <= 0 {
		limit = OutputLimit
	}
	return &Output{limit: limit}
}

// Append дописывает строку (перевод строки добавляется сам). Строка длиннее
// 8 КиБ режется. При переполнении начало буфера сдвигается до границы строки.
func (o *Output) Append(line string) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) > runLineLimit {
		line = strings.ToValidUTF8(line[:runLineLimit], "")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.data = append(o.data, line...)
	o.data = append(o.data, '\n')
	if over := len(o.data) - o.limit; over > 0 {
		drop := over
		if i := bytes.IndexByte(o.data[over:], '\n'); i >= 0 {
			drop = over + i + 1
		}
		o.data = o.data[drop:]
		o.start += int64(drop)
	}
}

// Read отдаёт текст начиная со смещения from и смещение для следующего чтения.
// from меньше начала буфера — чтение идёт с начала буфера и truncated=true:
// часть вывода уже вытеснена.
func (o *Output) Read(from int64) (text string, next int64, truncated bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	total := o.start + int64(len(o.data))
	if from < o.start {
		from = o.start
		truncated = true
	}
	if from > total {
		from = total
	}
	return string(o.data[from-o.start:]), total, truncated
}

// Lines возвращает строки, которые сейчас в буфере.
func (o *Output) Lines() []string {
	o.mu.Lock()
	text := string(o.data)
	o.mu.Unlock()
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// Save записывает текущее содержимое буфера в файл (атомарно).
func (o *Output) Save(path string) error {
	o.mu.Lock()
	data := append([]byte(nil), o.data...)
	o.mu.Unlock()
	return utils.AtomicWriteFile(path, data, 0o600)
}

// Runner запускает xkeen с живым выводом. Аргументы задаёт только вызывающий код
// (фиксированный список без пользовательских значений); оболочка не участвует.
type Runner struct {
	Bin     string
	Timeout time.Duration
}

var (
	runEscRe      = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|[()][A-Za-z0-9]|[@-Z\\-_])`)
	runProgressRe = regexp.MustCompile(`^[#\s]*(?:\d+(?:\.\d+)?%)?$`)
	runMeterRe    = regexp.MustCompile(`^\s*%\s+Total\s+%\s+Received`)
)

// runCleanLine приводит сырую строку к виду для журнала: без ESC-последовательностей,
// управляющих символов и некорректного UTF-8. Пустая строка — строка не нужна.
func runCleanLine(raw string) string {
	s := utils.StripANSI(raw)
	s = runEscRe.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.ToValidUTF8(s, "")
	s = strings.TrimRight(s, " ")
	if s == "" || runProgressRe.MatchString(s) || runMeterRe.MatchString(s) {
		return ""
	}
	return s
}

// runPump читает общий пайп stdout/stderr, режет поток по \n и \r и кладёт чистые
// строки в out. Когда sealed взведён, поток только дочитывается в никуда: читающий
// конец не закрывается, пока его держит последний потомок (перезапущенное ядро
// унаследовало fd и без читателя получило бы SIGPIPE).
func runPump(r *os.File, out *Output, sealed *atomic.Bool, done chan<- struct{}) {
	defer close(done)
	defer r.Close()
	buf := make([]byte, 4096)
	line := make([]byte, 0, 256)
	skip := false // строка уже длиннее лимита: остаток до разделителя выбрасывается
	flush := func() {
		if len(line) > 0 && out != nil && !sealed.Load() {
			if s := runCleanLine(string(line)); s != "" {
				out.Append(s)
			}
		}
		line = line[:0]
		skip = false
	}
	for {
		if sealed.Load() {
			_, _ = io.Copy(io.Discard, r)
			return
		}
		n, err := r.Read(buf)
		for _, c := range buf[:n] {
			switch {
			case c == '\n' || c == '\r':
				flush()
			case skip:
			case len(line) >= runLineLimit:
				skip = true
			default:
				line = append(line, c)
			}
		}
		if err != nil {
			flush()
			return
		}
	}
}

// Run запускает Bin с args. Вывод построчно попадает в out. Код выхода возвращается
// как есть (ненулевой код — не ошибка Run: итог по файлам даёт разбор текста).
// Ошибка — запуск не удался, истёк Timeout (ErrRunTimeout) или отменён ctx.
//
// Остановка по таймауту или отмене: SIGTERM только процессу xkeen, через 30 секунд
// SIGKILL только ему. Группу процессов не трогаем — в ней могут быть перезапущенные
// ядра.
func (r *Runner) Run(ctx context.Context, args []string, out *Output) (int, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = runDefaultTimeout
	}

	// os/exec без контекста: контекстный вариант сам убивает процесс при отмене,
	// а сигнал должен идти по правилам выше.
	cmd := exec.Command(r.Bin, args...)
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return -1, err
	}
	defer devnull.Close()
	cmd.Stdin = devnull

	pr, pw, err := os.Pipe()
	if err != nil {
		return -1, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	err = cmd.Start()
	// Пишущий конец в родителе закрывается сразу: иначе EOF не наступит никогда.
	_ = pw.Close()
	if err != nil {
		_ = pr.Close()
		return -1, err
	}

	var sealed atomic.Bool
	pumpDone := make(chan struct{})
	go runPump(pr, out, &sealed, pumpDone)

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var waitErr, runErr error
	select {
	case waitErr = <-waitCh:
	case <-timer.C:
		runErr = ErrRunTimeout
		waitErr = runTerminate(cmd, waitCh)
	case <-ctx.Done():
		runErr = ctx.Err()
		waitErr = runTerminate(cmd, waitCh)
	}

	// Дождаться конца потока недолго; дальше читатель работает в фоне вхолостую.
	grace := time.NewTimer(runDrainGrace)
	defer grace.Stop()
	select {
	case <-pumpDone:
	case <-grace.C:
		sealed.Store(true)
	}

	code := 0
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &exitErr):
		code = exitErr.ExitCode()
	default:
		code = -1
		if runErr == nil {
			runErr = waitErr
		}
	}
	if runErr != nil && code == 0 {
		code = -1
	}
	return code, runErr
}

// runTerminate просит процесс завершиться (SIGTERM), через runKillGrace убивает его
// (SIGKILL) и возвращает результат Wait. Сигналы получает только сам процесс.
func runTerminate(cmd *exec.Cmd, waitCh <-chan error) error {
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	t := time.NewTimer(runKillGrace)
	defer t.Stop()
	select {
	case err := <-waitCh:
		return err
	case <-t.C:
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return <-waitCh
}
