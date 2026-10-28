package lists

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Виды точечных правок xkeen.json, которые умеет GeodataStore.
const (
	// OpAdd — вписать запись geodata (File, URL).
	OpAdd = "add"
	// OpRemove — убрать записи geodata с именем File; при непустом MatchURL — только
	// с тем же именем И тем же адресом (фоновая уборка временной записи не трогает
	// запись с тем же именем и другим адресом).
	OpRemove = "remove"
	// OpSetURL — заменить адрес у записи с именем File (при непустом MatchURL — только
	// у записи с этим адресом) на URL.
	OpSetURL = "set_url"
	// OpGhProxy — записать xkeen.gh_proxy = Value.
	OpGhProxy = "gh_proxy"
	// OpEnableUpdate — заменить false у ключа geofile_update на true.
	OpEnableUpdate = "enable_update"
)

// GeodataOp — одна правка xkeen.json. В запись geodata попадают только file и url
// (никаких скрытых отметок происхождения записи).
type GeodataOp struct {
	Kind     string
	File     string
	URL      string
	MatchURL string
	Value    string
}

// GeodataPreviewText — результат предпросмотра правки. Base — отпечаток Before:
// клиент возвращает его при записи, и запись идёт по тому файлу, который он видел.
type GeodataPreviewText struct {
	Before       string
	After        string
	CommentsKept bool
	Base         string
}

var errBadOp = errors.New("geodata: недопустимая правка")

// Preview строит новое содержимое xkeen.json для правки. Файл не меняется. Правка
// текстовая — комментарии и порядок ключей сохраняются; если результат текстовой
// правки не даёт того же разобранного JSON, файл собирается заново (CommentsKept=false
// при наличии комментариев в исходном тексте).
func (g *GeodataStore) Preview(op GeodataOp) (GeodataPreviewText, error) {
	if err := g.requireConditions(op.Kind); err != nil {
		return GeodataPreviewText{}, err
	}
	return g.build(op)
}

// Apply пишет правку в xkeen.json через XKeenSettingsService.Save (с резервной
// копией). Чтение, построение и запись идут под замком хранилища; перед записью файл
// перечитывается и сравнивается с прочитанным для предпросмотра. base — отпечаток
// файла из показанного предпросмотра; пусто — привязки к показанному нет. Файл
// изменился с момента предпросмотра или во время записи — ErrGeodataChanged, файл не
// пишется, чужая правка не теряется. acceptCommentLoss подтверждает потерю
// комментариев, если текстовая правка невозможна.
func (g *GeodataStore) Apply(op GeodataOp, base string, acceptCommentLoss bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.applyLocked(op, base, acceptCommentLoss)
}

func (g *GeodataStore) applyLocked(op GeodataOp, base string, acceptCommentLoss bool) error {
	if err := g.requireConditions(op.Kind); err != nil {
		return err
	}
	p, err := g.build(op)
	if err != nil {
		return err
	}
	if base != "" && p.Base != base {
		return ErrGeodataChanged
	}
	if !p.CommentsKept && !acceptCommentLoss {
		return ErrCommentsLost
	}
	cur, err := g.settings.Get(services.XKeenConfigJSON)
	if err != nil {
		return err
	}
	if cur.Content != p.Before {
		return ErrGeodataChanged
	}
	if p.After == p.Before {
		return nil
	}
	if _, err := g.settings.Save(services.XKeenConfigJSON, p.After); err != nil {
		if errors.Is(err, services.ErrXKeenSettingsInvalid) {
			return fmt.Errorf("%w: %v", ErrXKeenJSONInvalid, err)
		}
		return err
	}
	return nil
}

// requireConditions проверяет условия окружения. Вставка записи и замена адреса
// работают только через sed XKeen по jq и при geofile_update не false; удаление,
// gh_proxy и включение обновления чинят окружение или безопасны без jq.
func (g *GeodataStore) requireConditions(kind string) error {
	if kind != OpAdd && kind != OpSetURL {
		return nil
	}
	st, err := g.Status()
	if err != nil {
		return err
	}
	if !st.JQ {
		return ErrNoJQ
	}
	if !st.GeofileUpdate {
		return ErrGeofileUpdateOff
	}
	return nil
}

// build читает xkeen.json и строит результат правки без проверки окружения.
func (g *GeodataStore) build(op GeodataOp) (GeodataPreviewText, error) {
	f, err := g.settings.Get(services.XKeenConfigJSON)
	if err != nil {
		return GeodataPreviewText{}, err
	}
	before := f.Content
	res := GeodataPreviewText{Before: before, Base: ContentBase(before)}
	expected, err := decodeJSONC(before)
	if err != nil {
		return res, err
	}

	var edit func(src string) (string, error)
	creates := false
	switch op.Kind {
	case OpRemove:
		if !removeGeneric(expected, op.File, op.MatchURL) {
			return res, ErrEntryNotFound
		}
		edit = func(src string) (string, error) { return removeGeodata(src, op.File, op.MatchURL) }
	default:
		return res, errBadOp
	}
	return finishEdit(res, expected, creates, edit)
}

// finishEdit применяет текстовую правку и сверяет её с ожидаемым объектом; при
// расхождении собирает файл заново из ожидаемого объекта.
func finishEdit(res GeodataPreviewText, expected map[string]any, creates bool, edit func(string) (string, error)) (GeodataPreviewText, error) {
	before := res.Before
	src := before
	if creates {
		switch {
		case strings.TrimSpace(src) == "":
			src = "{\n}\n"
		case strings.TrimSpace(services.StripJSONComments(src)) == "":
			src = strings.TrimRight(src, "\n") + "\n{\n}\n"
		}
	}
	if text, err := edit(src); err == nil {
		if got, derr := decodeJSONC(text); derr == nil && reflect.DeepEqual(got, expected) {
			if text != before && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			res.After = text
			res.CommentsKept = true
			return res, nil
		}
	}
	raw, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return res, err
	}
	res.After = string(raw) + "\n"
	res.CommentsKept = services.StripJSONComments(before) == before
	return res, nil
}

// geodataArray возвращает массив xkeen.xray.geodata разобранного объекта; ok=false —
// пути нет или на пути не объект и не массив.
func geodataArray(root map[string]any) (xray map[string]any, arr []any, ok bool) {
	xk, ok := root["xkeen"].(map[string]any)
	if !ok {
		return nil, nil, false
	}
	xray, ok = xk["xray"].(map[string]any)
	if !ok {
		return nil, nil, false
	}
	arr, ok = xray["geodata"].([]any)
	return xray, arr, ok
}

// geodataMatches — элемент geodata подходит под правку: то же имя файла и, если
// задан matchURL, тот же адрес. Не объект не подходит никогда.
func geodataMatches(el any, file, matchURL string) bool {
	m, ok := el.(map[string]any)
	if !ok {
		return false
	}
	f, _ := m["file"].(string)
	if f != file {
		return false
	}
	if matchURL != "" {
		if u, _ := m["url"].(string); u != matchURL {
			return false
		}
	}
	return true
}

// removeGeneric убирает подходящие элементы из разобранного объекта; false — таких нет.
func removeGeneric(root map[string]any, file, matchURL string) bool {
	xray, arr, ok := geodataArray(root)
	if !ok {
		return false
	}
	kept := make([]any, 0, len(arr))
	for _, el := range arr {
		if !geodataMatches(el, file, matchURL) {
			kept = append(kept, el)
		}
	}
	if len(kept) == len(arr) {
		return false
	}
	xray["geodata"] = kept
	return true
}

// --- текстовая правка массива geodata ---

// splice — замена фрагмента src[start:end] текстом (start==end — вставка).
type splice struct {
	start, end int
	text       string
}

// geodataNode разбирает src и находит узел массива xkeen.xray.geodata. Пути нет —
// (nil, ...) без ошибки; на пути не объект или не массив — ErrBadStructure.
func geodataNode(src string) (arr *jnode, toks []jtok, comments []jspan, err error) {
	toks, comments, err = tokenizeJSONC(src)
	if err != nil {
		return nil, nil, nil, err
	}
	p := &jparser{src: src, toks: toks}
	root, err := p.value()
	if err != nil {
		return nil, nil, nil, err
	}
	if p.pos != len(toks) || root.kind != '{' {
		return nil, nil, nil, ErrBadStructure
	}
	node := root
	for i, key := range []string{"xkeen", "xray", "geodata"} {
		item := node.find(key)
		if item == nil {
			return nil, toks, comments, nil
		}
		want := byte('{')
		if i == 2 {
			want = '['
		}
		if item.val.kind != want {
			return nil, nil, nil, ErrBadStructure
		}
		node = item.val
	}
	return node, toks, comments, nil
}

// itemMatches разбирает текст элемента массива (вместе с комментариями внутри него)
// и проверяет, подходит ли он под правку.
func itemMatches(src string, it jitem, file, matchURL string) bool {
	if it.val.kind != '{' {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(services.StripJSONComments(src[it.start:it.end])), &m); err != nil {
		return false
	}
	return geodataMatches(m, file, matchURL)
}

// removeGeodata убирает из текста xkeen.json все элементы geodata с именем file (при
// непустом matchURL — ещё и с этим адресом), не трогая остальное. Ни одного — ErrEntryNotFound.
func removeGeodata(src, file, matchURL string) (string, error) {
	removed := 0
	for {
		next, ok, err := removeGeodataOnce(src, file, matchURL)
		if err != nil {
			return "", err
		}
		if !ok {
			break
		}
		src = next
		removed++
	}
	if removed == 0 {
		return "", ErrEntryNotFound
	}
	return src, nil
}

// removeGeodataOnce убирает последний подходящий элемент массива.
func removeGeodataOnce(src, file, matchURL string) (string, bool, error) {
	arr, toks, comments, err := geodataNode(src)
	if err != nil {
		return "", false, err
	}
	if arr == nil {
		return src, false, nil
	}
	idx := -1
	for i := len(arr.items) - 1; i >= 0; i-- {
		if itemMatches(src, arr.items[i], file, matchURL) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return src, false, nil
	}
	return applySplices(src, arrayItemRemoval(src, toks, comments, arr, idx)), true, nil
}

// arrayItemRemoval планирует удаление элемента idx массива вместе с запятой. Строка
// элемента удаляется целиком (с концевым комментарием), если на ней кроме элемента нет
// других токенов; иначе удаляются только элемент и запятая, соседние комментарии остаются.
func arrayItemRemoval(src string, toks []jtok, comments []jspan, arr *jnode, idx int) []splice {
	item := arr.items[idx]
	last := idx == len(arr.items)-1

	var comma jtok
	var hasComma bool
	if last {
		if idx > 0 {
			comma, hasComma = commaBefore(toks, item.start)
		}
	} else {
		comma, hasComma = commaAfter(toks, item.end)
	}

	endPos := item.end
	if !last && hasComma {
		endPos = comma.end
	}
	ls := strings.LastIndexByte(src[:item.start], '\n') + 1
	eol := lineEndAfter(src, endPos, comments)
	alone := strings.TrimSpace(src[ls:item.start]) == ""
	if alone {
		if t, ok := nextToken(toks, endPos); ok && (eol < 0 || t.start < eol) {
			alone = false
		}
	}

	if alone {
		end := len(src)
		if eol >= 0 {
			end = eol + 1
		}
		out := []splice{{ls, end, ""}}
		if last && hasComma {
			out = append(out, splice{comma.start, comma.end, ""})
		}
		return out
	}
	switch {
	case len(arr.items) == 1:
		return []splice{{item.start, item.end, ""}}
	case last:
		return []splice{{comma.start, comma.end, ""}, {item.start, item.end, ""}}
	default:
		end := comma.end
		for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
			end++
		}
		return []splice{{item.start, end, ""}}
	}
}

func commaAfter(toks []jtok, pos int) (jtok, bool) {
	for _, t := range toks {
		if t.start >= pos && t.kind == ',' {
			return t, true
		}
	}
	return jtok{}, false
}

func commaBefore(toks []jtok, pos int) (jtok, bool) {
	for i := len(toks) - 1; i >= 0; i-- {
		if toks[i].end <= pos && toks[i].kind == ',' {
			return toks[i], true
		}
	}
	return jtok{}, false
}

func nextToken(toks []jtok, pos int) (jtok, bool) {
	for _, t := range toks {
		if t.start >= pos {
			return t, true
		}
	}
	return jtok{}, false
}

// applySplices применяет непересекающиеся замены, начиная с последней.
func applySplices(src string, sp []splice) string {
	sorted := append([]splice(nil), sp...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start > sorted[j].start })
	for _, s := range sorted {
		src = src[:s.start] + s.text + src[s.end:]
	}
	return src
}
