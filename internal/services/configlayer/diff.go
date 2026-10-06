package configlayer

// DiffView — две стороны сравнения файла: ожидаемое (генерация из применённого
// состояния) и фактическое с диска. Строковый diff строит клиент.
type DiffView struct {
	Key       string `json:"key"`
	Expected  string `json:"expected"`
	Actual    string `json:"actual"`
	Missing   bool   `json:"missing"`
	Truncated bool   `json:"truncated"`
}

// Diff — ожидаемое и фактическое содержимое файла.
func (l *Layer) Diff(key string) (DiffView, error) { return DiffView{}, errNotImplemented }

// truncateUTF8 обрезает b до max байт по границе символа UTF-8.
func truncateUTF8(b []byte, max int) (string, bool) { return "", false }
