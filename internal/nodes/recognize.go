package nodes

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Распознаватель ввода поля «Добавить» (SRC-01, D-10…D-14): вставка и файлы
// превращаются в список URL подписок, узлы «Моих узлов» и ошибки строк с кодами
// D-32. Сеть не трогается: запрос подписки по URL делает сервис источников.

// Пределы ввода (T-145-09). Каждый URL подписки стоит до двух сетевых запросов.
const (
	// MaxInputBytes — предел суммарного размера вставки и файлов.
	MaxInputBytes = 1 << 20
	// MaxInputLines — предел суммарного числа строк вставки и файлов.
	MaxInputLines = 5000
	// MaxURLs — предел числа URL подписок в одном вводе.
	MaxURLs = 10
	// MaxFragment — предел длины фрагмента в ошибке строки (в символах).
	MaxFragment = 80
)

// maxFileName — предел длины имени файла в ошибках (в символах).
const maxFileName = 80

// File — файл, выбранный пользователем: имя и текст.
type File struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Input — ввод поля «Добавить»: вставленный текст и выбранные файлы.
type Input struct {
	Text  string `json:"text"`
	Files []File `json:"files,omitempty"`
}

// URLItem — адрес подписки из ввода. Line — номер строки (с 1) в тексте или файле.
type URLItem struct {
	Line int    `json:"line"`
	File string `json:"file,omitempty"`
	URL  string `json:"url"`
}

// LineError — ошибка строки или документа: Line == 0, если номер неизвестен.
// Fragment — не длиннее MaxFragment символов, без userinfo, запроса и секретов.
type LineError struct {
	Line     int          `json:"line"`
	File     string       `json:"file,omitempty"`
	Fragment string       `json:"fragment,omitempty"`
	Err      *SourceError `json:"error"`
}

// Recognition — итог распознавания. NodesOnly — из полного конфига ядра взяты
// только узлы (D-12). Collapsed — сколько узлов-дублей схлопнуто (D-14).
type Recognition struct {
	URLs      []URLItem   `json:"urls"`
	Nodes     []Node      `json:"nodes"`
	Errors    []LineError `json:"errors"`
	NodesOnly bool        `json:"nodes_only"`
	Collapsed int         `json:"collapsed"`
}

var (
	// base64LineRE — строка, похожая на блок base64 (подписка, свёрнутая в одну строку).
	base64LineRE = regexp.MustCompile(`^[A-Za-z0-9+/_=-]{16,}$`)
	// wgHeaderRE — заголовок секции INI.
	wgHeaderRE = regexp.MustCompile(`^\[[^\]\[]+\]$`)
	// iniKeyRE — строка «ключ = значение» INI.
	iniKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*\s*=\s*[^=\s]`)
	// yamlListRE — первая строка YAML-списка прокси.
	yamlListRE = regexp.MustCompile(`^-\s+["']?(?:name|type|server)["']?\s*:`)
	// yamlProxyKeyRE — первая строка YAML с одним прокси-картой.
	yamlProxyKeyRE = regexp.MustCompile(`^["']?(?:name|type)["']?\s*:`)
	yamlTypeRE     = regexp.MustCompile(`(?m)^type\s*:`)
	yamlServerRE   = regexp.MustCompile(`(?m)^server\s*:`)
	// yamlClashKeyRE — верхние ключи Clash-конфига: документ разбирает ParseClash.
	yamlClashKeyRE = regexp.MustCompile(`(?m)^(?:proxy-groups|proxy-providers|rules|mixed-port|allow-lan)\s*:`)
	schemeRE       = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
)

// Recognize разбирает ввод целиком. Нераспознанное не блокирует остальное:
// каждая строка или документ даёт URL, узел или ошибку с кодом и номером строки.
func Recognize(in Input) Recognition {
	res := Recognition{URLs: []URLItem{}, Nodes: []Node{}, Errors: []LineError{}}

	type document struct{ file, content string }
	docs := []document{{"", in.Text}}
	for _, f := range in.Files {
		docs = append(docs, document{cleanFileName(f.Name), f.Content})
	}
	total, lines := 0, 0
	for _, d := range docs {
		total += len(d.content)
		if d.content != "" {
			lines += strings.Count(d.content, "\n") + 1
		}
	}
	// Проверка до разбора: превышение не обрабатывается частично (T-145-09).
	if total > MaxInputBytes {
		res.Errors = append(res.Errors, LineError{
			Err: NewError(CodeTooLarge, "unit", "bytes", "limit", itoa(MaxInputBytes)),
		})
		return res
	}
	if lines > MaxInputLines {
		res.Errors = append(res.Errors, LineError{
			Err: NewError(CodeTooLarge, "unit", "lines", "limit", itoa(MaxInputLines)),
		})
		return res
	}

	r := &recognizer{res: &res}
	for _, d := range docs {
		r.document(d.file, d.content, 0)
	}
	res.Nodes, res.Collapsed = dedupeNodes(r.nodes)
	return res
}

// recognizer накапливает результат по документам ввода.
type recognizer struct {
	res   *Recognition
	nodes []Node
}

// document разбирает один документ: JSON, YAML, либо построчно (ссылки, URL,
// блоки wg.conf, base64).
func (r *recognizer) document(file, content string, depth int) {
	text := strings.TrimPrefix(content, "\xef\xbb\xbf")
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	switch {
	case trimmed[0] == '{' || (trimmed[0] == '[' && !hasPrefixFold(trimmed, "[interface]")):
		r.jsonDoc(file, text)
	case yamlKind(trimmed) != yamlKindNone:
		r.yamlDoc(file, text, trimmed)
	default:
		if r.wrappedBase64(file, trimmed, depth) {
			return
		}
		r.lineDoc(file, text, depth)
	}
}

// jsonDoc разбирает документ Xray-JSON или sing-box.
func (r *recognizer) jsonDoc(file, text string) {
	v, serr := decodeJSON([]byte(text))
	if serr != nil {
		r.addSourceError(file, serr.Line, "", serr)
		return
	}
	var (
		res ParseResult
		err error
	)
	m := mapOf(v)
	switch {
	case m != nil && m["outbounds"] == nil && m["protocol"] == nil && m["type"] != nil:
		// Один outbound sing-box (исходный текст узла из «Моих узлов»).
		res, err = parseSingboxValue([]any{m})
	case looksSingbox(v):
		res, err = parseSingboxValue(v)
	default:
		res, err = parseXrayValue(v)
	}
	r.take(file, res, err, 0, 1)
}

const (
	yamlKindNone = iota
	yamlKindList
	yamlKindMap
	yamlKindSingle
)

// yamlKind определяет, что документ — YAML Clash: словарь с proxies (или
// другими ключами конфига), список прокси или один прокси-словарь.
func yamlKind(trimmed string) int {
	if proxiesKeyRE.MatchString(trimmed) || yamlClashKeyRE.MatchString(trimmed) {
		return yamlKindMap
	}
	first := firstMeaningfulLine(trimmed)
	switch {
	case yamlListRE.MatchString(first):
		return yamlKindList
	case yamlProxyKeyRE.MatchString(first) && yamlTypeRE.MatchString(trimmed) && yamlServerRE.MatchString(trimmed):
		return yamlKindSingle
	}
	return yamlKindNone
}

// firstMeaningfulLine — первая строка без пустых и комментариев.
func firstMeaningfulLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			return l
		}
	}
	return ""
}

// yamlDoc разбирает YAML Clash. Номера строк ошибок сохраняются: одиночный
// прокси оборачивается в список без сдвига строк.
func (r *recognizer) yamlDoc(file, text, trimmed string) {
	data := text
	if yamlKind(trimmed) == yamlKindSingle {
		lines := strings.Split(text, "\n")
		seen := false
		for i, l := range lines {
			t := strings.TrimSpace(l)
			if !seen && t != "" && !strings.HasPrefix(t, "#") {
				lines[i] = "- " + l
				seen = true
				continue
			}
			if seen && l != "" {
				lines[i] = "  " + l
			}
		}
		data = strings.Join(lines, "\n")
	}
	res, err := ParseClash([]byte(data))
	if len(res.Nodes) > 0 && yamlHasConfigKeys(text) {
		res.FullConfig = true
	}
	r.take(file, res, err, 0, 1)
}

// yamlHasConfigKeys — в верхнем уровне YAML есть rules или proxy-groups: документ
// — полный конфиг Clash, берутся только узлы (D-12).
func yamlHasConfigKeys(text string) bool {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(text), &top); err != nil {
		return false
	}
	_, rules := top["rules"]
	_, groups := top["proxy-groups"]
	return rules || groups
}

// take переносит результат разборщика в итог. Пропуски несут свои коды; ошибка
// самого разбора добавляется, только если пропусков нет (иначе она их дублирует).
func (r *recognizer) take(file string, res ParseResult, err error, offset, defLine int) {
	r.nodes = append(r.nodes, res.Nodes...)
	if res.FullConfig {
		r.res.NodesOnly = true
	}
	for _, sk := range res.Skipped {
		line := sk.Line
		if line > 0 {
			line += offset
		}
		se := &SourceError{Code: sk.Code}
		if len(sk.Params) > 0 {
			se.Params = make(map[string]string, len(sk.Params))
			for k, v := range sk.Params {
				se.Params[k] = v
			}
		}
		r.addSourceError(file, line, MaskText(sk.Fragment, nil), se)
	}
	switch {
	case err != nil && len(res.Skipped) == 0:
		se := asSourceError(err)
		line := se.Line
		if line > 0 {
			line += offset
		} else {
			line = defLine
		}
		r.addSourceError(file, line, "", se)
	case err == nil && len(res.Nodes) == 0 && len(res.Skipped) == 0:
		// Документ распознан, но узлов в нём нет: не молчим.
		r.addSourceError(file, defLine, "", NewError(CodeEmptyResponse))
	}
}

// lineDoc разбирает документ построчно: комментарии и пустые строки пропускаются,
// блок [Interface] целиком уходит в ParseWG, прочие строки — ссылки, URL, base64.
func (r *recognizer) lineDoc(file, text string, depth int) {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "" || line[0] == '#':
			i++
		case strings.EqualFold(line, "[interface]"):
			end := wgBlockEnd(lines, i)
			res, err := ParseWG(strings.Join(lines[i:end], "\n"))
			r.take(file, res, err, i, i+1)
			i = end
		default:
			r.lineItem(file, line, i+1, depth)
			i++
		}
	}
}

// wgBlockEnd возвращает индекс (не включая) конца блока wg.conf, начатого строкой
// start: блок идёт, пока строки — заголовки секций, пары «ключ = значение»,
// комментарии и пустые строки; пустые и комментарии в хвосте блоку не принадлежат.
func wgBlockEnd(lines []string, start int) int {
	end := start + 1
	for k := start + 1; k < len(lines); k++ {
		l := strings.TrimSpace(lines[k])
		switch {
		case l == "" || l[0] == '#' || l[0] == ';':
			continue
		case strings.EqualFold(l, "[interface]"):
			return end
		case wgHeaderRE.MatchString(l), iniKeyRE.MatchString(l):
			end = k + 1
		default:
			return end
		}
	}
	return end
}

// lineItem разбирает одну строку: URL подписки, share-ссылку или base64.
func (r *recognizer) lineItem(file, line string, lineNo, depth int) {
	lower := strings.ToLower(line)
	isHTTP := strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
	// Внутри декодированного base64 http(s) — прокси-ссылка списка, а не подписка.
	if isHTTP && depth == 0 {
		r.urlItem(file, line, lineNo)
		return
	}
	if scheme, _, ok := strings.Cut(line, "://"); ok {
		scheme = strings.ToLower(scheme)
		if !schemeRE.MatchString(scheme) {
			r.addSourceError(file, lineNo, fragmentOfLine(line), NewError(CodeUnrecognized))
			return
		}
		n, err := ParseLink(line)
		if err != nil {
			r.addSourceError(file, lineNo, fragmentOfLine(line), asSourceError(err))
			return
		}
		r.nodes = append(r.nodes, n)
		return
	}
	if depth == 0 && base64LineRE.MatchString(line) {
		r.base64Block(file, line, lineNo, depth)
		return
	}
	r.addSourceError(file, lineNo, fragmentOfLine(line), NewError(CodeUnrecognized))
}

// urlItem принимает http(s) URL с непустым хостом (T-145-10); проверка приватных
// адресов — задача SafeHTTPClient сервиса источников.
func (r *recognizer) urlItem(file, line string, lineNo int) {
	u, err := url.Parse(line)
	if err != nil || u.Hostname() == "" {
		r.addSourceError(file, lineNo, fragmentOfLine(line), NewError(CodeInvalidURL))
		return
	}
	if len(r.res.URLs) >= MaxURLs {
		r.addSourceError(file, lineNo, fragmentOfLine(line),
			NewError(CodeTooManyURLs, "limit", itoa(MaxURLs)))
		return
	}
	r.res.URLs = append(r.res.URLs, URLItem{Line: lineNo, File: file, URL: line})
}

// base64Block декодирует блок base64. Подпиской он считается, только если после
// декодирования в нём есть «://»; иначе строка — unrecognized. Ошибки и URL
// декодированного списка привязаны к строке блока.
func (r *recognizer) base64Block(file, block string, lineNo, depth int) {
	raw, err := decodeBase64Any(block)
	if err != nil {
		r.addSourceError(file, lineNo, MaskSecret(block), NewError(CodeBadBase64))
		return
	}
	decoded := string(raw)
	if !strings.Contains(decoded, "://") {
		r.addSourceError(file, lineNo, MaskSecret(block), NewError(CodeUnrecognized))
		return
	}
	errFrom, urlFrom := len(r.res.Errors), len(r.res.URLs)
	r.lineDoc(file, decoded, depth+1)
	for i := errFrom; i < len(r.res.Errors); i++ {
		r.res.Errors[i].Line = lineNo
		r.res.Errors[i].Err.Line = lineNo
	}
	for i := urlFrom; i < len(r.res.URLs); i++ {
		r.res.URLs[i].Line = lineNo
	}
}

// wrappedBase64 — документ из нескольких строк, целиком состоящий из base64 с
// переносами (подписка, свёрнутая по ширине): склеивается и декодируется как один
// блок. Если не декодируется или без «://» — документ идёт построчным путём.
func (r *recognizer) wrappedBase64(file, trimmed string, depth int) bool {
	if depth != 0 || !strings.ContainsAny(trimmed, "\r\n") || !base64BodyRE.MatchString(trimmed) {
		return false
	}
	joined := strings.Join(strings.Fields(trimmed), "")
	if len(joined) < 16 {
		return false
	}
	raw, err := decodeBase64Any(joined)
	if err != nil || !strings.Contains(string(raw), "://") {
		return false
	}
	r.base64Block(file, joined, 1, depth)
	return true
}

// addSourceError добавляет ошибку строки. SourceError копируется: Detail не
// переносится, Fragment и Line выставляются из строки.
func (r *recognizer) addSourceError(file string, line int, fragment string, se *SourceError) {
	fragment = truncFragment(fragment)
	e := &SourceError{Code: se.Code, HTTPStatus: se.HTTPStatus, Line: line, Fragment: fragment}
	if len(se.Params) > 0 {
		e.Params = make(map[string]string, len(se.Params))
		for k, v := range se.Params {
			e.Params[k] = v
		}
	}
	r.res.Errors = append(r.res.Errors, LineError{Line: line, File: file, Fragment: fragment, Err: e})
}

// dedupeNodes схлопывает дубли по идентичности узла и отпечатку транспорта
// (как Uniq): остаются первые экземпляры в порядке ввода.
func dedupeNodes(list []Node) (out []Node, collapsed int) {
	out = make([]Node, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, n := range list {
		k := n.Identity() + "|" + transportFingerprint(n)
		if seen[k] {
			collapsed++
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	return out, collapsed
}

// fragmentOfLine — фрагмент строки для ошибки. Для строк со схемой — схема и хост
// без userinfo, пути и запроса; для base64-блоков внутри адреса (vmess, ss) хост
// не показывается. Остальное проходит MaskText.
func fragmentOfLine(line string) string {
	if scheme, rest, ok := strings.Cut(line, "://"); ok {
		scheme = strings.ToLower(scheme)
		if schemeRE.MatchString(scheme) {
			auth := rest
			if i := strings.IndexAny(auth, "/?#"); i >= 0 {
				auth = auth[:i]
			}
			if i := strings.LastIndex(auth, "@"); i >= 0 {
				auth = auth[i+1:]
			} else if scheme == "vmess" || scheme == "ss" || scheme == "ssr" {
				auth = maskEllipsis
			}
			return truncFragment(scheme + "://" + auth)
		}
	}
	return truncFragment(MaskText(line, nil))
}

// truncFragment ограничивает фрагмент MaxFragment символами по границе руны.
func truncFragment(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= MaxFragment {
		return s
	}
	return string([]rune(s)[:MaxFragment-1]) + maskEllipsis
}

// cleanFileName оставляет имя файла без каталогов и управляющих символов.
func cleanFileName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxFileName {
		name = string([]rune(name)[:maxFileName-1]) + maskEllipsis
	}
	return name
}

// asSourceError приводит ошибку к *SourceError; чужая ошибка — unrecognized.
func asSourceError(err error) *SourceError {
	var se *SourceError
	if errors.As(err, &se) && se != nil {
		return se
	}
	return NewError(CodeUnrecognized)
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func itoa(n int) string { return strconv.Itoa(n) }
