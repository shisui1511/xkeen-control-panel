package nodes

// maxNodes — предел узлов в одном разборе (T-145-04): больше — too_large.
const maxNodes = 5000

// Skipped — прокси из ответа или вставки, который не стал узлом. Ничего не
// исчезает молча: каждый пропуск несёт код причины.
//
// Fragment — не длиннее 80 символов и без секретов: берётся только из имени
// узла или типа (маскирование текста — задача вызывающего).
type Skipped struct {
	Line     int
	Name     string
	Code     string
	Params   map[string]string
	Fragment string
}

// ParseResult — итог разбора тела ответа.
//
// Format: "clash" | "xray_json" | "singbox" | "base64" | "links" | "wg".
// FullConfig — тело оказалось полным конфигом ядра (D-12).
type ParseResult struct {
	Nodes      []Node
	Skipped    []Skipped
	Format     string
	FullConfig bool
}

// skippedFrom строит запись пропуска из ошибки разбора.
func skippedFrom(err *SourceError, line int, name, fragment string) Skipped {
	s := Skipped{Line: line, Name: fragmentOf(name), Code: err.Code, Fragment: fragmentOf(fragment)}
	if len(err.Params) > 0 {
		s.Params = make(map[string]string, len(err.Params))
		for k, v := range err.Params {
			s.Params[k] = v
		}
	}
	return s
}
