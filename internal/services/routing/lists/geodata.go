package lists

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// GeodataEntry — запись .xkeen.xray.geodata[] в xkeen.json: файл и адрес, откуда его
// скачивает XKeen.
type GeodataEntry struct {
	File string `json:"file"`
	URL  string `json:"url"`
}

// GeodataStatus — состояние пользовательских geodata XKeen. Reason непуст, когда
// запись невозможна: no_jq или geofile_update_off. GhProxy — xkeen.gh_proxy (пусто,
// если ключа нет). Invalid — записи geodata с пустым file или url: XKeen на такой
// записи прекращает обработку следующих; Entries содержит только корректные.
type GeodataStatus struct {
	JQ            bool           `json:"jq"`
	GeofileUpdate bool           `json:"geofile_update"`
	GhProxy       string         `json:"gh_proxy"`
	Entries       []GeodataEntry `json:"entries"`
	Invalid       []GeodataEntry `json:"invalid,omitempty"`
	Reason        string         `json:"reason,omitempty"`
}

// CodedError — ошибка с кодом для интерфейса.
type CodedError struct{ Code string }

func (e *CodedError) Error() string { return e.Code }

// Коды ошибок записи geodata.
var (
	ErrNoJQ             = &CodedError{Code: "no_jq"}
	ErrGeofileUpdateOff = &CodedError{Code: "geofile_update_off"}
	ErrSetUnknown       = &CodedError{Code: "set_unknown"}
	ErrAlreadyPresent   = &CodedError{Code: "already_present"}
	ErrCommentsLost     = &CodedError{Code: "comments_lost"}
	ErrInvalidXKeenJSON = &CodedError{Code: "invalid_json"}
	ErrBadStructure     = &CodedError{Code: "invalid_structure"}
	// ErrUpdateFailed — запись выполнена, а запуск xkeen -ug не удался.
	ErrUpdateFailed = &CodedError{Code: "update_failed"}
	// ErrGeodataChanged — xkeen.json изменился после показанного предпросмотра
	// (или между чтением и записью): запись отменена, предпросмотр нужно построить
	// заново.
	ErrGeodataChanged = &CodedError{Code: "geodata_changed"}
	// ErrEntryNotFound — запись geodata для правки (удаление, замена адреса) не найдена:
	// нет записи с таким именем или с таким именем и адресом.
	ErrEntryNotFound = &CodedError{Code: "entry_not_found"}
	// ErrXKeenJSONInvalid — проверка xkeen.json отвергла результат правки: файл не
	// пишется (иначе XKeen не запустит прокси).
	ErrXKeenJSONInvalid = &CodedError{Code: "xkeen_json_invalid"}
)

// ErrorCode возвращает код ошибки пакета или пустую строку.
func ErrorCode(err error) string {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// GeodataStore читает и дополняет .xkeen.xray.geodata[] в xkeen.json. Файлы dat
// скачивает и пишет только XKeen: панель вписывает лишь адреса из встроенного каталога.
type GeodataStore struct {
	// mu сериализует правки xkeen.json: два одновременных запроса не пишут поверх друг друга.
	mu       sync.Mutex
	settings *services.XKeenSettingsService
	lookPath func(string) (string, error)
}

// NewGeodataStore создаёт хранилище. lookPath ищет jq (nil — exec.LookPath и /opt/bin).
// Запуск `xkeen -ug` хранилищу не принадлежит: им владеет менеджер geodata.
func NewGeodataStore(settings *services.XKeenSettingsService, lookPath func(string) (string, error)) *GeodataStore {
	if lookPath == nil {
		lookPath = defaultLookPath
	}
	return &GeodataStore{settings: settings, lookPath: lookPath}
}

func defaultLookPath(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	p := "/opt/bin/" + name
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, nil
	}
	return "", exec.ErrNotFound
}

// Status читает записи geodata и условия блока: jq на роутере и geofile_update не false.
func (g *GeodataStore) Status() (GeodataStatus, error) {
	st := GeodataStatus{Entries: []GeodataEntry{}, GeofileUpdate: true}
	_, jqErr := g.lookPath("jq")
	st.JQ = jqErr == nil
	f, err := g.settings.Get(services.XKeenConfigJSON)
	if err != nil {
		return st, err
	}
	parsed, err := parseGeodataFull(f.Content)
	if err != nil {
		return st, err
	}
	st.Entries = parsed.entries
	st.Invalid = parsed.invalid
	st.GhProxy = parsed.ghProxy
	st.GeofileUpdate = parsed.update
	switch {
	case !st.JQ:
		st.Reason = ErrNoJQ.Code
	case !st.GeofileUpdate:
		st.Reason = ErrGeofileUpdateOff.Code
	}
	return st, nil
}

// decodeJSONC разбирает xkeen.json (JSON с комментариями); пустой файл — пустой объект.
func decodeJSONC(content string) (map[string]any, error) {
	stripped := strings.TrimSpace(services.StripJSONComments(content))
	if stripped == "" {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(stripped))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidXKeenJSON, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: лишние данные после объекта", ErrInvalidXKeenJSON)
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: корень не объект", ErrInvalidXKeenJSON)
	}
	return root, nil
}

// geodataParsed — разобранное состояние geodata в xkeen.json.
type geodataParsed struct {
	entries []GeodataEntry
	invalid []GeodataEntry
	update  bool
	ghProxy string
}

// parseGeodataFull разбирает записи geodata, gh_proxy и признак geofile_update (по
// умолчанию true; XKeen отключает обновление только значением false). Запись с пустым
// file или url попадает в invalid, а не в entries.
func parseGeodataFull(content string) (geodataParsed, error) {
	root, err := decodeJSONC(content)
	if err != nil {
		return geodataParsed{entries: []GeodataEntry{}, update: true}, err
	}
	out := geodataParsed{entries: []GeodataEntry{}}
	if xk, ok := root["xkeen"].(map[string]any); ok {
		out.ghProxy, _ = xk["gh_proxy"].(string)
	}
	_, arr, _ := geodataArray(root)
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		file, _ := m["file"].(string)
		url, _ := m["url"].(string)
		if file == "" || url == "" {
			out.invalid = append(out.invalid, GeodataEntry{File: file, URL: url})
			continue
		}
		out.entries = append(out.entries, GeodataEntry{File: file, URL: url})
	}
	out.update = !hasFalseKey(root, "geofile_update")
	return out, nil
}

// parseGeodata возвращает корректные записи geodata и признак geofile_update.
func parseGeodata(content string) ([]GeodataEntry, bool, error) {
	p, err := parseGeodataFull(content)
	return p.entries, p.update, err
}

func hasFalseKey(v any, key string) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == key {
				if b, ok := val.(bool); ok && !b {
					return true
				}
			}
			if hasFalseKey(val, key) {
				return true
			}
		}
	case []any:
		for _, val := range t {
			if hasFalseKey(val, key) {
				return true
			}
		}
	}
	return false
}

// ContentBase — отпечаток содержимого xkeen.json (sha256 в hex). Клиент получает
// его с предпросмотром и возвращает при записи: запись идёт по тому файлу, который
// пользователь видел.
func ContentBase(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// addGeneric добавляет запись в разобранный объект (создавая недостающие уровни).
func addGeneric(root map[string]any, e GeodataEntry) error {
	node := root
	for _, key := range []string{"xkeen", "xray"} {
		next, ok := node[key]
		if !ok {
			m := map[string]any{}
			node[key] = m
			node = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return ErrBadStructure
		}
		node = m
	}
	cur, ok := node["geodata"]
	if !ok {
		node["geodata"] = []any{map[string]any{"file": e.File, "url": e.URL}}
		return nil
	}
	arr, ok := cur.([]any)
	if !ok {
		return ErrBadStructure
	}
	node["geodata"] = append(arr, map[string]any{"file": e.File, "url": e.URL})
	return nil
}

// --- текстовая вставка в JSON с комментариями ---

type jtok struct {
	kind       byte // { } [ ] : , s (строка) v (литерал или число)
	start, end int
}

type jspan struct{ start, end int }

type jitem struct {
	key        string
	start, end int
	val        *jnode
}

type jnode struct {
	kind       byte // { [ или v
	start, end int
	items      []jitem
}

func tokenizeJSONC(src string) ([]jtok, []jspan, error) {
	var toks []jtok
	var comments []jspan
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			j := i
			for j < len(src) && src[j] != '\n' {
				j++
			}
			comments = append(comments, jspan{i, j})
			i = j
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, nil, errors.New("незакрытый комментарий")
			}
			end := i + 2 + j + 2
			comments = append(comments, jspan{i, end})
			i = end
		case c == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(src) {
				return nil, nil, errors.New("незакрытая строка")
			}
			toks = append(toks, jtok{'s', i, j + 1})
			i = j + 1
		case strings.IndexByte("{}[]:,", c) >= 0:
			toks = append(toks, jtok{c, i, i + 1})
			i++
		default:
			j := i
			for j < len(src) && strings.IndexByte(" \t\n\r{}[]:,\"/", src[j]) < 0 {
				j++
			}
			if j == i {
				return nil, nil, fmt.Errorf("неожиданный символ в позиции %d", i)
			}
			toks = append(toks, jtok{'v', i, j})
			i = j
		}
	}
	return toks, comments, nil
}

type jparser struct {
	src  string
	toks []jtok
	pos  int
}

func (p *jparser) peek() (jtok, bool) {
	if p.pos >= len(p.toks) {
		return jtok{}, false
	}
	return p.toks[p.pos], true
}

func (p *jparser) value() (*jnode, error) {
	t, ok := p.peek()
	if !ok {
		return nil, errors.New("неожиданный конец")
	}
	p.pos++
	switch t.kind {
	case 's', 'v':
		return &jnode{kind: 'v', start: t.start, end: t.end}, nil
	case '{':
		n := &jnode{kind: '{', start: t.start}
		if c, ok := p.peek(); ok && c.kind == '}' {
			p.pos++
			n.end = c.end
			return n, nil
		}
		for {
			k, ok := p.peek()
			if !ok || k.kind != 's' {
				return nil, errors.New("ожидался ключ")
			}
			p.pos++
			var key string
			if err := json.Unmarshal([]byte(p.src[k.start:k.end]), &key); err != nil {
				return nil, err
			}
			if c, ok := p.peek(); !ok || c.kind != ':' {
				return nil, errors.New("ожидалось «:»")
			}
			p.pos++
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, jitem{key: key, start: k.start, end: v.end, val: v})
			c, ok := p.peek()
			if !ok {
				return nil, errors.New("неожиданный конец")
			}
			p.pos++
			if c.kind == '}' {
				n.end = c.end
				return n, nil
			}
			if c.kind != ',' {
				return nil, errors.New("ожидалась «,» или «}»")
			}
		}
	case '[':
		n := &jnode{kind: '[', start: t.start}
		if c, ok := p.peek(); ok && c.kind == ']' {
			p.pos++
			n.end = c.end
			return n, nil
		}
		for {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, jitem{start: v.start, end: v.end, val: v})
			c, ok := p.peek()
			if !ok {
				return nil, errors.New("неожиданный конец")
			}
			p.pos++
			if c.kind == ']' {
				n.end = c.end
				return n, nil
			}
			if c.kind != ',' {
				return nil, errors.New("ожидалась «,» или «]»")
			}
		}
	}
	return nil, fmt.Errorf("неожиданный токен %q", string(t.kind))
}

func (n *jnode) find(key string) *jitem {
	for i := len(n.items) - 1; i >= 0; i-- {
		if n.items[i].key == key {
			return &n.items[i]
		}
	}
	return nil
}

type insertion struct {
	pos  int
	text string
}

func leadingIndent(src string, pos int) string {
	ls := strings.LastIndexByte(src[:pos], '\n') + 1
	i := ls
	for i < pos && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return src[ls:i]
}

// lineEndAfter — позиция ближайшего перевода строки после pos вне блочного комментария.
func lineEndAfter(src string, pos int, comments []jspan) int {
	for {
		nl := strings.IndexByte(src[pos:], '\n')
		if nl < 0 {
			return -1
		}
		p := pos + nl
		inside := false
		for _, c := range comments {
			if p > c.start && p < c.end && strings.HasPrefix(src[c.start:], "/*") {
				pos = c.end
				inside = true
				break
			}
		}
		if !inside {
			return p
		}
	}
}

// appendItem планирует вставку нового элемента (или члена) в конец массива/объекта.
// build получает отступ элемента и признак многострочной записи.
func (n *jnode) appendItem(src string, comments []jspan, build func(indent, unit string, multi bool) string) ([]insertion, error) {
	closePos := n.end - 1
	if len(n.items) == 0 {
		inner := src[n.start+1 : closePos]
		if strings.Contains(inner, "\n") {
			base := leadingIndent(src, n.start)
			unit := indentUnit(base)
			indent := base + unit
			return []insertion{{n.start + 1, "\n" + indent + build(indent, unit, true)}}, nil
		}
		return []insertion{{n.start + 1, build("", "", false)}}, nil
	}
	last := n.items[len(n.items)-1]
	if strings.Contains(src[last.end:closePos], "\n") {
		indent := leadingIndent(src, last.start)
		unit := indentUnit(indent)
		p := lineEndAfter(src, last.end, comments)
		if p < 0 || p > closePos {
			return nil, errors.New("не найдено место вставки")
		}
		return []insertion{{last.end, ","}, {p, "\n" + indent + build(indent, unit, true)}}, nil
	}
	return []insertion{{last.end, ", " + build("", "", false)}}, nil
}

func indentUnit(indent string) string {
	if strings.Contains(indent, "\t") {
		return "\t"
	}
	return "  "
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func entryText(e GeodataEntry) string {
	return "{" + quote("file") + ": " + quote(e.File) + ", " + quote("url") + ": " + quote(e.URL) + "}"
}

// memberText строит член «ключ: значение» для цепочки недостающих ключей keys (последний — массив geodata).
func memberText(keys []string, e GeodataEntry, indent, unit string, multi bool) string {
	head := quote(keys[0]) + ": "
	if len(keys) == 1 {
		if multi {
			return head + "[\n" + indent + unit + entryText(e) + "\n" + indent + "]"
		}
		return head + "[" + entryText(e) + "]"
	}
	if multi {
		inner := memberText(keys[1:], e, indent+unit, unit, true)
		return head + "{\n" + indent + unit + inner + "\n" + indent + "}"
	}
	return head + "{" + memberText(keys[1:], e, "", "", false) + "}"
}

// applyInsertions вставляет тексты с конца к началу. Вставки в одну позицию попадают
// в результат в порядке списка (например, запятая у прежнего последнего элемента и
// перевод строки с новым, когда после элемента нет комментария).
func applyInsertions(src string, ins []insertion) string {
	sorted := make([]insertion, len(ins))
	for i, in := range ins {
		sorted[len(ins)-1-i] = in
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].pos > sorted[j].pos })
	for _, in := range sorted {
		src = src[:in.pos] + in.text + src[in.pos:]
	}
	return src
}

// insertGeodata вставляет запись в текст xkeen.json, не трогая остальное.
func insertGeodata(src string, e GeodataEntry) (string, error) {
	toks, comments, err := tokenizeJSONC(src)
	if err != nil {
		return "", err
	}
	p := &jparser{src: src, toks: toks}
	root, err := p.value()
	if err != nil {
		return "", err
	}
	if p.pos != len(toks) || root.kind != '{' {
		return "", ErrBadStructure
	}
	path := []string{"xkeen", "xray", "geodata"}
	node := root
	for i, key := range path {
		item := node.find(key)
		if item == nil {
			ins, err := node.appendItem(src, comments, func(indent, unit string, multi bool) string {
				return memberText(path[i:], e, indent, unit, multi)
			})
			if err != nil {
				return "", err
			}
			return applyInsertions(src, ins), nil
		}
		if i == len(path)-1 {
			if item.val.kind != '[' {
				return "", ErrBadStructure
			}
			ins, err := item.val.appendItem(src, comments, func(string, string, bool) string { return entryText(e) })
			if err != nil {
				return "", err
			}
			return applyInsertions(src, ins), nil
		}
		if item.val.kind != '{' {
			return "", ErrBadStructure
		}
		node = item.val
	}
	return "", ErrBadStructure
}
