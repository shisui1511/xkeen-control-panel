package configlayer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"unicode/utf8"
)

// diffSideLimit — предел одной стороны сравнения: файлы панели малы, а
// огромный файл в ответе API ни к чему.
const diffSideLimit = 256 * 1024

// DiffView — две стороны сравнения файла: ожидаемое (генерация из применённого
// состояния) и фактическое с диска. Строковый diff строит клиент.
type DiffView struct {
	Key       string `json:"key"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Missing   bool   `json:"missing"`
	Truncated bool   `json:"truncated"`
}

// Diff отдаёт ожидаемое содержимое файла манифеста (генерация из применённого
// состояния, а не из черновика) и фактическое с диска. Для drift_renamed
// фактическим считается содержимое <имя>.obsolete; пропавший файл — Missing.
// Каждая сторона режется до 256 КБ по границе символа UTF-8 с признаком
// Truncated. Принимается только ключ манифеста; отпущенный файл не сверяется
// (ErrFileReleased).
func (l *Layer) Diff(key string) (DiffView, error) {
	if !l.Enabled() {
		return DiffView{}, ErrDisabled
	}
	st := l.store.Snapshot()
	entry, ok := st.Manifest[key]
	if !ok {
		return DiffView{}, ErrUnknownKey
	}
	if entry.Status == StatusReleased {
		return DiffView{}, ErrFileReleased
	}
	abs, err := l.opts.Roots.Abs(entry.Kernel, entry.RelPath)
	if err != nil {
		return DiffView{}, fmt.Errorf("%s: %w", key, err)
	}
	generated, err := l.registry.Build(st.Applied, l.installed())
	if err != nil {
		return DiffView{}, fmt.Errorf("сборка ожидаемого содержимого: %w", err)
	}
	view := DiffView{Key: key}
	var expected []byte
	for _, g := range generated {
		if g.Key() == key {
			expected = g.Content
			break
		}
	}
	var expTrunc bool
	view.Expected, expTrunc = truncateUTF8(expected, diffSideLimit)

	actual, err := readCapped(abs, diffSideLimit)
	if errors.Is(err, fs.ErrNotExist) {
		actual, err = readCapped(abs+obsoleteSuffix, diffSideLimit)
		if errors.Is(err, fs.ErrNotExist) {
			view.Missing = true
			view.Truncated = expTrunc
			return view, nil
		}
	}
	if err != nil {
		return DiffView{}, fmt.Errorf("%s: %w", key, err)
	}
	var actTrunc bool
	view.Actual, actTrunc = truncateUTF8(actual, diffSideLimit)
	view.Truncated = expTrunc || actTrunc
	return view, nil
}

// readCapped читает не больше max+1 байт: лишний байт нужен, чтобы заметить
// превышение предела, не загружая огромный файл целиком.
func readCapped(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(max)+1))
}

// truncateUTF8 обрезает b до max байт по границе символа UTF-8; второй результат
// сообщает, была ли обрезка.
func truncateUTF8(b []byte, max int) (string, bool) {
	if len(b) <= max {
		return string(b), false
	}
	cut := max
	for i := 0; i < utf8.UTFMax && cut > 0 && !utf8.RuneStart(b[cut]); i++ {
		cut--
	}
	return string(b[:cut]), true
}
