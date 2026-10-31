package cores

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Форма и «Дополнительно» (D-07, XCFG-01): значение секции раскладывается на поля
// формы и остаток, а при сохранении собирается обратно целиком. Поле, которого
// форма не знает, лежит в остатке и не пропадает; порядок ключей прежнего значения
// сохраняется (на нём держится порядок YAML Mihomo и читаемый diff).

// issueRawDupKey — ключ «Дополнительно» повторяет поле формы.
const issueRawDupKey = "raw_dup_key"

// sectionKind — как секция раскладывается на форму и остаток.
type sectionKind int

const (
	// kindObject — объект: поля формы по путям (в том числе вложенным).
	kindObject sectionKind = iota + 1
	// kindXrayList — «Входящие» Xray: массив объектов с тегом.
	kindXrayList
	// kindMihomoInbounds — «Входящие» Mihomo: listeners (массив) и tun (объект).
	kindMihomoInbounds
	// kindRaw — «Прочие ключи»: формы нет, всё значение — остаток.
	kindRaw
)

// kindOf возвращает вид секции; false — ядро или секция неизвестны.
func kindOf(kernel, id string) (sectionKind, bool) {
	switch kernel {
	case configlayer.KernelXray:
		switch id {
		case "log", "dns", "policy":
			return kindObject, true
		case "inbounds":
			return kindXrayList, true
		case "other":
			return kindRaw, true
		}
	case configlayer.KernelMihomo:
		switch id {
		case mihomoMain, mihomoDNS, mihomoSniffer:
			return kindObject, true
		case mihomoInbounds:
			return kindMihomoInbounds, true
		case mihomoOther:
			return kindRaw, true
		}
	}
	return 0, false
}

// FormKeys возвращает пути полей формы секции (копия таблицы XrayFormKeys или
// MihomoFormKeys); для неизвестной секции — nil.
func FormKeys(kernel, id string) []string {
	var table map[string][]string
	switch kernel {
	case configlayer.KernelXray:
		table = XrayFormKeys
	case configlayer.KernelMihomo:
		table = MihomoFormKeys
	}
	keys, ok := table[id]
	if !ok {
		return nil
	}
	return slices.Clone(keys)
}

// RawError — синтаксическая ошибка «сырого» блока: строка и пояснение.
type RawError struct {
	Line   int    `json:"line"`
	Detail string `json:"detail"`
}

// Error описывает ошибку; *RawError возвращает MergeForm при ошибке разбора.
func (e *RawError) Error() string {
	return fmt.Sprintf("строка %d: %s", e.Line, e.Detail)
}

// listSpec — описание списка входящих: ключ идентификатора, ключи строки формы и
// закреплённые пути входящих, которые читает XKeen.
type listSpec struct {
	idKey   string
	rowKeys []string
	pinned  [][]string
}

var (
	xrayListSpec = listSpec{
		idKey:   "tag",
		rowKeys: []string{"tag", "protocol", "listen", "port"},
		pinned:  pinnedInboundPaths,
	}
	mihomoListSpec = listSpec{
		idKey:   "name",
		rowKeys: []string{"name", "type", "listen", "port"},
	}
)

// layout — пути объекта: поля формы и закреплённые значения.
type layout struct {
	leaf, parent             map[string]bool
	pinnedLeaf, pinnedParent map[string]bool
}

func (l layout) addForm(paths []string) {
	addPaths(l.leaf, l.parent, paths)
}

// addPaths заносит пути «a.b.c» в множества листьев и родителей.
func addPaths(leaf, parent map[string]bool, paths []string) {
	for _, p := range paths {
		parts := strings.Split(p, ".")
		leaf[p] = true
		for i := 1; i < len(parts); i++ {
			parent[strings.Join(parts[:i], ".")] = true
		}
	}
}

// layoutFor строит пути объектной секции. value — прежнее значение: форма DNS
// Xray не берёт servers, если среди серверов есть не строки («Сложные серверы…»).
func layoutFor(kernel, id string, value *configlayer.OrderedObject) layout {
	l := layout{leaf: map[string]bool{}, parent: map[string]bool{}, pinnedLeaf: map[string]bool{}, pinnedParent: map[string]bool{}}
	var form []string
	for _, p := range FormKeys(kernel, id) {
		if !strings.Contains(p, "[]") {
			form = append(form, p)
		}
	}
	l.addForm(form)
	if kernel == configlayer.KernelXray && id == "dns" && value != nil {
		if raw, ok := value.Get("servers"); ok && !allStrings(raw) {
			delete(l.leaf, "servers")
		}
	}
	if kernel == configlayer.KernelXray && id == "policy" {
		var pinned []string
		for _, c := range xrayStatsCounters {
			pinned = append(pinned, "system."+c)
		}
		addPaths(l.pinnedLeaf, l.pinnedParent, pinned)
	}
	return l
}

// allStrings сообщает, что значение — массив из одних строк.
func allStrings(raw json.RawMessage) bool {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return false
	}
	for _, it := range items {
		t := bytes.TrimSpace(it)
		if len(t) == 0 || t[0] != '"' {
			return false
		}
	}
	return true
}

// --- упорядоченные объекты ---

func newOO() *configlayer.OrderedObject { return configlayer.NewOrderedObject() }

// objOf разбирает значение как объект; не объект или пусто — пустой объект.
func objOf(raw json.RawMessage) *configlayer.OrderedObject {
	if isEmptyValue(raw) {
		return newOO()
	}
	o, err := configlayer.ParseOrderedObject(raw)
	if err != nil {
		return newOO()
	}
	return o
}

// isObject сообщает, что значение — объект.
func isObject(raw json.RawMessage) bool {
	return mihomoIsObject(raw)
}

// ooGetPath читает значение по пути.
func ooGetPath(o *configlayer.OrderedObject, parts []string) (json.RawMessage, bool) {
	v, ok := o.Get(parts[0])
	if !ok {
		return nil, false
	}
	if len(parts) == 1 {
		return v, true
	}
	sub, err := configlayer.ParseOrderedObject(v)
	if err != nil {
		return nil, false
	}
	return ooGetPath(sub, parts[1:])
}

// ooSetPath записывает значение по пути, создавая недостающие объекты.
func ooSetPath(o *configlayer.OrderedObject, parts []string, val json.RawMessage) {
	if len(parts) == 1 {
		o.Set(parts[0], val)
		return
	}
	sub := newOO()
	if v, ok := o.Get(parts[0]); ok {
		if s, err := configlayer.ParseOrderedObject(v); err == nil {
			sub = s
		}
	}
	ooSetPath(sub, parts[1:], val)
	o.Set(parts[0], mihomoObjectRaw(sub))
}

// ooDelPath удаляет значение по пути вместе с родителями, ставшими пустыми.
func ooDelPath(o *configlayer.OrderedObject, parts []string) {
	if _, ok := ooGetPath(o, parts); !ok {
		return
	}
	if len(parts) == 1 {
		o.Delete(parts[0])
		return
	}
	v, _ := o.Get(parts[0])
	sub, err := configlayer.ParseOrderedObject(v)
	if err != nil {
		return
	}
	ooDelPath(sub, parts[1:])
	if sub.Len() == 0 {
		o.Delete(parts[0])
		return
	}
	o.Set(parts[0], mihomoObjectRaw(sub))
}

// unionKeys — ключи объектов по порядку: сначала первого, затем новые из
// следующих.
func unionKeys(objs ...*configlayer.OrderedObject) []string {
	var out []string
	seen := map[string]bool{}
	for _, o := range objs {
		for _, k := range o.Keys() {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// --- разложение значения ---

// splitObj делит объект на поля формы и остаток. Закреплённые значения в остаток
// не попадают; родители, опустевшие после выноса полей, убираются.
func splitObj(lay layout, prefix string, value *configlayer.OrderedObject) (form, rest *configlayer.OrderedObject) {
	form, rest = newOO(), newOO()
	for _, k := range value.Keys() {
		v, _ := value.Get(k)
		path := prefix + k
		switch {
		case lay.leaf[path]:
			form.Set(k, v)
		case lay.pinnedLeaf[path]:
		case lay.parent[path] || lay.pinnedParent[path]:
			sub, err := configlayer.ParseOrderedObject(v)
			if err != nil {
				rest.Set(k, v)
				continue
			}
			f, r := splitObj(lay, path+".", sub)
			if f.Len() > 0 {
				form.Set(k, mihomoObjectRaw(f))
			}
			if r.Len() > 0 || sub.Len() == 0 {
				rest.Set(k, mihomoObjectRaw(r))
			}
		default:
			rest.Set(k, v)
		}
	}
	return form, rest
}

// itemID возвращает идентификатор элемента списка (тег или имя).
func itemID(o *configlayer.OrderedObject, idKey string) string {
	raw, ok := o.Get(idKey)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// listItems разбирает массив объектов; nil и пусто — пустой список.
func listItems(raw json.RawMessage) ([]*configlayer.OrderedObject, error) {
	if isEmptyValue(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.New("ожидался массив")
	}
	out := make([]*configlayer.OrderedObject, 0, len(items))
	for _, it := range items {
		o, err := configlayer.ParseOrderedObject(it)
		if err != nil {
			return nil, errors.New("ожидался массив объектов")
		}
		out = append(out, o)
	}
	return out, nil
}

// splitList делит список на строки формы и остаток «идентификатор → поля».
// Закреплённые поля входящих keep в остаток не попадают.
func splitList(items []*configlayer.OrderedObject, spec listSpec, keep []string) (rows []json.RawMessage, adv *configlayer.OrderedObject) {
	adv = newOO()
	for i, it := range items {
		row, rest := newOO(), newOO()
		for _, k := range it.Keys() {
			v, _ := it.Get(k)
			if slices.Contains(spec.rowKeys, k) {
				row.Set(k, v)
			} else {
				rest.Set(k, v)
			}
		}
		id := itemID(it, spec.idKey)
		if id != "" && slices.Contains(keep, id) {
			for _, p := range spec.pinned {
				ooDelPath(rest, p)
			}
		}
		rows = append(rows, mihomoObjectRaw(row))
		if rest.Len() == 0 {
			continue
		}
		key := id
		if _, dup := adv.Get(key); key == "" || dup {
			key = "#" + strconv.Itoa(i)
		}
		adv.Set(key, mihomoObjectRaw(rest))
	}
	return rows, adv
}

// SplitForm раскладывает значение секции на поля формы и текст «Дополнительно».
// form — объект только с путями формы, которые есть в значении (вложенные пути
// строят вложенные объекты), у «Входящих» Xray — массив строк
// {tag, protocol, listen, port}. advanced — всё остальное без закреплённых
// значений: JSON с отступом два пробела у Xray, YAML у Mihomo. Пустое значение —
// пустые форма и остаток.
func SplitForm(kernel, id string, value json.RawMessage) (form json.RawMessage, advanced string, err error) {
	kind, ok := kindOf(kernel, id)
	if !ok {
		return nil, "", &Error{Code: codeSectionUnknown}
	}
	var (
		formObj *configlayer.OrderedObject
		rest    *configlayer.OrderedObject
	)
	switch kind {
	case kindXrayList:
		items, lerr := listItems(value)
		if lerr != nil {
			return nil, "", lerr
		}
		rows, adv := splitList(items, xrayListSpec, XrayKeepTags(value))
		raw, merr := json.Marshal(rows)
		if merr != nil {
			return nil, "", merr
		}
		if len(rows) == 0 {
			raw = json.RawMessage("[]")
		}
		advText, aerr := renderAdvanced(kernel, adv)
		return raw, advText, aerr
	case kindMihomoInbounds:
		obj, oerr := objectOf(value)
		if oerr != nil {
			return nil, "", oerr
		}
		formObj, rest, err = splitMihomoInbounds(obj)
		if err != nil {
			return nil, "", err
		}
	case kindRaw:
		obj, oerr := objectOf(value)
		if oerr != nil {
			return nil, "", oerr
		}
		formObj, rest = newOO(), obj
	default:
		obj, oerr := objectOf(value)
		if oerr != nil {
			return nil, "", oerr
		}
		formObj, rest = splitObj(layoutFor(kernel, id, obj), "", obj)
	}
	advText, aerr := renderAdvanced(kernel, rest)
	if aerr != nil {
		return nil, "", aerr
	}
	return mihomoObjectRaw(formObj), advText, nil
}

// objectOf разбирает значение секции-объекта; пусто — пустой объект.
func objectOf(value json.RawMessage) (*configlayer.OrderedObject, error) {
	if isEmptyValue(value) {
		return newOO(), nil
	}
	o, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, errors.New("ожидался объект")
	}
	return o, nil
}

// splitMihomoInbounds делит «Входящие» Mihomo: listeners — строки формы и
// остаток по именам, tun — поля формы и остаток, прочие ключи — в остаток.
func splitMihomoInbounds(obj *configlayer.OrderedObject) (form, rest *configlayer.OrderedObject, err error) {
	form, rest = newOO(), newOO()
	lay := layoutFor(configlayer.KernelMihomo, mihomoInbounds, nil)
	for _, k := range obj.Keys() {
		v, _ := obj.Get(k)
		switch k {
		case "listeners":
			items, lerr := listItems(v)
			if lerr != nil {
				// Не массив объектов: форма его не показывает, ключ остаётся в остатке.
				rest.Set(k, v)
				continue
			}
			rows, adv := splitList(items, mihomoListSpec, nil)
			raw, merr := json.Marshal(rows)
			if merr != nil {
				return nil, nil, merr
			}
			form.Set(k, raw)
			if adv.Len() > 0 {
				rest.Set(k, mihomoObjectRaw(adv))
			}
		case "tun":
			if !isObject(v) {
				rest.Set(k, v)
				continue
			}
			f, r := splitObj(lay, "tun.", objOf(v))
			if f.Len() > 0 {
				form.Set(k, mihomoObjectRaw(f))
			}
			if r.Len() > 0 {
				rest.Set(k, mihomoObjectRaw(r))
			}
		default:
			rest.Set(k, v)
		}
	}
	return form, rest, nil
}

// renderAdvanced выводит остаток текстом: JSON с отступом (Xray; пусто — "{}")
// или YAML (Mihomo; пусто — "").
func renderAdvanced(kernel string, rest *configlayer.OrderedObject) (string, error) {
	if kernel == configlayer.KernelMihomo {
		if rest.Len() == 0 {
			return "", nil
		}
		return RenderYAML(mihomoObjectRaw(rest))
	}
	if rest.Len() == 0 {
		return "{}", nil
	}
	b, err := rest.MarshalIndent("  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- разбор «Дополнительно» ---

// jsonSyntaxLine возвращает номер строки (с 1) по смещению ошибки синтаксиса.
func jsonSyntaxLine(text string, offset int64) int {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(text)) {
		offset = int64(len(text))
	}
	return 1 + strings.Count(text[:offset], "\n")
}

// parseAdvanced разбирает текст «Дополнительно»: JSON-объект (Xray) или YAML
// (Mihomo). Синтаксическая ошибка — *RawError, текст больше предела — *Error
// raw_too_large.
func parseAdvanced(kernel, text string) (*configlayer.OrderedObject, error) {
	if len(text) > maxRawBytes {
		return nil, &Error{Code: codeRawTooLarge}
	}
	if kernel == configlayer.KernelMihomo {
		raw, line, err := ParseYAMLText(text)
		if err != nil {
			if line <= 0 {
				line = 1
			}
			return nil, &RawError{Line: line, Detail: err.Error()}
		}
		return objOf(raw), nil
	}
	if strings.TrimSpace(text) == "" {
		return newOO(), nil
	}
	var probe json.RawMessage
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			return nil, &RawError{Line: jsonSyntaxLine(text, se.Offset), Detail: err.Error()}
		}
		return nil, &RawError{Line: 1, Detail: err.Error()}
	}
	o, err := configlayer.ParseOrderedObject([]byte(text))
	if err != nil {
		return nil, &RawError{Line: 1, Detail: "ожидался объект"}
	}
	return o, nil
}

// --- сборка значения ---

// issueSink копит проблемы ключей.
type issueSink struct{ list []configlayer.KeyIssue }

func (s *issueSink) add(key, code string) {
	for _, it := range s.list {
		if it.Key == key && it.Code == code {
			return
		}
	}
	s.list = append(s.list, configlayer.KeyIssue{Key: key, Code: code})
}

// mergeObj собирает объект: порядок ключей прежнего значения, поля формы — из
// формы (нет в форме — ключ снят), закреплённые — прежние, остальное — из
// остатка (нет в остатке — ключ снят).
func mergeObj(lay layout, prefix string, prev, form, adv *configlayer.OrderedObject, iss *issueSink) *configlayer.OrderedObject {
	out := newOO()
	for _, k := range unionKeys(prev, form, adv) {
		path := prefix + k
		pv, hasPrev := prev.Get(k)
		fv, hasForm := form.Get(k)
		av, hasAdv := adv.Get(k)
		switch {
		case lay.leaf[path]:
			if hasAdv {
				iss.add(path, issueRawDupKey)
			}
			if hasForm && !isEmptyValue(fv) {
				out.Set(k, fv)
			}
		case lay.pinnedLeaf[path]:
			if hasAdv {
				out.Set(k, av)
			} else if hasPrev {
				out.Set(k, pv)
			}
		case lay.parent[path] || lay.pinnedParent[path]:
			if hasAdv && !isObject(av) {
				iss.add(path, issueRawDupKey)
				continue
			}
			sub := mergeObj(lay, path+".", objOf(pv), objOf(fv), objOf(av), iss)
			if sub.Len() > 0 {
				out.Set(k, mihomoObjectRaw(sub))
			}
		default:
			if hasAdv {
				out.Set(k, av)
			}
		}
	}
	return out
}

// mergeList собирает список из строк формы и остатка. Строка находит прежний
// элемент по идентификатору (без идентификатора — по номеру); закреплённые поля
// входящих keep берутся из прежнего элемента. Записи остатка без строки формы —
// новые элементы (идентификатор — ключ записи), кроме «#<номер>».
func mergeList(prev []*configlayer.OrderedObject, rows []json.RawMessage, adv *configlayer.OrderedObject, spec listSpec, keep []string, iss *issueSink) []json.RawMessage {
	prevByID := map[string]*configlayer.OrderedObject{}
	for _, p := range prev {
		if id := itemID(p, spec.idKey); id != "" {
			if _, dup := prevByID[id]; !dup {
				prevByID[id] = p
			}
		}
	}
	used := map[string]bool{}
	var out []json.RawMessage
	for i, row := range rows {
		ro, err := configlayer.ParseOrderedObject(row)
		if err != nil {
			iss.add("form", configlayer.IssueCoreSectionInvalid)
			continue
		}
		id := itemID(ro, spec.idKey)
		key := id
		var pv *configlayer.OrderedObject
		if id != "" {
			pv = prevByID[id]
		} else {
			key = "#" + strconv.Itoa(i)
			if i < len(prev) && itemID(prev[i], spec.idKey) == "" {
				pv = prev[i]
			}
		}
		used[key] = true
		var ent *configlayer.OrderedObject
		if raw, ok := adv.Get(key); ok {
			if !isObject(raw) {
				iss.add(key, configlayer.IssueCoreSectionInvalid)
				continue
			}
			ent = objOf(raw)
		} else {
			ent = newOO()
		}
		item := mergeItem(pv, ro, ent, spec, id != "" && slices.Contains(keep, id), key, iss)
		out = append(out, mihomoObjectRaw(item))
	}
	for _, key := range adv.Keys() {
		if used[key] || strings.HasPrefix(key, "#") {
			continue
		}
		raw, _ := adv.Get(key)
		if !isObject(raw) {
			iss.add(key, configlayer.IssueCoreSectionInvalid)
			continue
		}
		item := newOO()
		item.Set(spec.idKey, mihomoJSON(key))
		ent := objOf(raw)
		for _, k := range ent.Keys() {
			if k == spec.idKey {
				continue
			}
			v, _ := ent.Get(k)
			item.Set(k, v)
		}
		out = append(out, mihomoObjectRaw(item))
	}
	return out
}

// mergeItem собирает один элемент списка. keepEntry — входящий, который читает
// XKeen: его закреплённые поля берутся из прежнего значения.
func mergeItem(prev, row, ent *configlayer.OrderedObject, spec listSpec, keepEntry bool, key string, iss *issueSink) *configlayer.OrderedObject {
	if prev == nil {
		prev = newOO()
	}
	pinnedTop := map[string]bool{}
	lay := layout{pinnedLeaf: map[string]bool{}, pinnedParent: map[string]bool{}}
	if keepEntry {
		var joined []string
		for _, p := range spec.pinned {
			pinnedTop[p[0]] = true
			joined = append(joined, strings.Join(p, "."))
			if _, ok := ooGetPath(ent, p); ok && (len(p) > 1 || !slices.Contains(spec.rowKeys, p[0])) {
				iss.add(key+"."+strings.Join(p, "."), configlayer.IssueRawPinned)
			}
		}
		addPaths(lay.pinnedLeaf, lay.pinnedParent, joined)
		// Закреплённые поля из остатка не берутся: они всегда прежние.
		clean := newOO()
		for _, k := range ent.Keys() {
			v, _ := ent.Get(k)
			clean.Set(k, v)
		}
		for _, p := range spec.pinned {
			if len(p) > 1 {
				ooDelPath(clean, p)
			}
		}
		ent = clean
	}
	for _, k := range ent.Keys() {
		if slices.Contains(spec.rowKeys, k) {
			iss.add(key+"."+k, issueRawDupKey)
		}
	}
	out := newOO()
	for _, k := range unionKeys(prev, row, ent) {
		switch {
		case slices.Contains(spec.rowKeys, k):
			if v, ok := row.Get(k); ok && !isEmptyValue(v) {
				out.Set(k, v)
			}
		case keepEntry && pinnedTop[k]:
			pv, _ := prev.Get(k)
			ev, _ := ent.Get(k)
			sub := mergeObj(lay, k+".", objOf(pv), newOO(), objOf(ev), iss)
			if sub.Len() > 0 {
				out.Set(k, mihomoObjectRaw(sub))
			}
		default:
			if v, ok := ent.Get(k); ok {
				out.Set(k, v)
			}
		}
	}
	if keepEntry {
		for _, p := range spec.pinned {
			if len(p) != 1 {
				continue
			}
			if v, ok := prev.Get(p[0]); ok {
				out.Set(p[0], v)
			}
		}
	}
	return out
}

// formOf разбирает форму запроса; пустая форма или null — форма прежнего
// значения (клиент форму не прислал).
func formOrPrev(kernel, id string, prev, form json.RawMessage) (json.RawMessage, error) {
	if isEmptyValue(form) {
		f, _, err := SplitForm(kernel, id, prev)
		return f, err
	}
	return form, nil
}

// MergeForm собирает значение секции из прежнего значения, формы и текста
// «Дополнительно». Поле формы, которого нет в форме, снимается; остаток
// заменяет прежний остаток целиком; закреплённые значения и поля формы входящих
// XKeen остаются прежними. Ключ остатка, совпадающий с полем формы, —
// raw_dup_key; зарезервированные и закреплённые ключи, лишние порты и повторы
// отклоняет проверка значения секции. Синтаксическая ошибка текста — *RawError,
// текст больше 256 КиБ — *Error raw_too_large.
func MergeForm(kernel, id string, prev, form json.RawMessage, advanced string) (json.RawMessage, []configlayer.KeyIssue, error) {
	var keep []string
	if kernel == configlayer.KernelXray && id == "inbounds" {
		keep = XrayKeepTags(prev)
	}
	return mergeForm(kernel, id, prev, form, advanced, keep)
}

// mergeForm — MergeForm с явным списком защищённых входящих keep.
func mergeForm(kernel, id string, prev, form json.RawMessage, advanced string, keep []string) (json.RawMessage, []configlayer.KeyIssue, error) {
	kind, ok := kindOf(kernel, id)
	if !ok {
		return nil, nil, &Error{Code: codeSectionUnknown}
	}
	adv, err := parseAdvanced(kernel, advanced)
	if err != nil {
		return nil, nil, err
	}
	iss := &issueSink{}
	var value json.RawMessage

	switch kind {
	case kindRaw:
		value = mihomoObjectRaw(adv)
	case kindXrayList:
		prevItems, _ := listItems(prev)
		form, err = formOrPrev(kernel, id, prev, form)
		if err != nil {
			return nil, nil, &Error{Code: configlayer.IssueCoreSectionInvalid}
		}
		var rows []json.RawMessage
		if json.Unmarshal(form, &rows) != nil {
			return nil, nil, &Error{Code: codeInvalidInput}
		}
		items := mergeList(prevItems, rows, adv, xrayListSpec, keep, iss)
		value = json.RawMessage("[]")
		if len(items) > 0 {
			if value, err = json.Marshal(items); err != nil {
				return nil, nil, err
			}
		}
	case kindMihomoInbounds:
		form, err = formOrPrev(kernel, id, prev, form)
		if err != nil {
			return nil, nil, &Error{Code: configlayer.IssueCoreSectionInvalid}
		}
		value = mergeMihomoInbounds(objOf(prev), objOf(form), adv, iss)
	default:
		prevObj := objOf(prev)
		form, err = formOrPrev(kernel, id, prev, form)
		if err != nil {
			return nil, nil, &Error{Code: configlayer.IssueCoreSectionInvalid}
		}
		value = mihomoObjectRaw(mergeObj(layoutFor(kernel, id, prevObj), "", prevObj, objOf(form), adv, iss))
	}

	if kernel == configlayer.KernelXray {
		for _, is := range XrayValueIssues(id, value, keep) {
			iss.add(is.Key, is.Code)
		}
	} else {
		for _, is := range MihomoValueIssues(id, value) {
			iss.add(is.Key, is.Code)
		}
	}
	return value, iss.list, nil
}

// mergeMihomoInbounds собирает «Входящие» Mihomo: listeners по строкам формы и
// остатку по именам, tun — полями формы и остатком, прочие ключи — из остатка.
func mergeMihomoInbounds(prev, form, adv *configlayer.OrderedObject, iss *issueSink) json.RawMessage {
	out := newOO()
	lay := layoutFor(configlayer.KernelMihomo, mihomoInbounds, nil)
	for _, k := range unionKeys(prev, form, adv) {
		pv, _ := prev.Get(k)
		fv, _ := form.Get(k)
		av, hasAdv := adv.Get(k)
		switch k {
		case "listeners":
			var rows []json.RawMessage
			if !isEmptyValue(fv) {
				if json.Unmarshal(fv, &rows) != nil {
					iss.add("listeners", configlayer.IssueCoreSectionInvalid)
					continue
				}
			}
			if hasAdv && !isObject(av) {
				// Не массив объектов, оставшийся в остатке (форма его не показывала).
				if len(rows) == 0 {
					out.Set(k, av)
					continue
				}
				iss.add("listeners", configlayer.IssueCoreSectionInvalid)
				continue
			}
			prevItems, _ := listItems(pv)
			items := mergeList(prevItems, rows, objOf(av), mihomoListSpec, nil, iss)
			if len(items) > 0 {
				raw, err := json.Marshal(items)
				if err != nil {
					iss.add("listeners", configlayer.IssueCoreSectionInvalid)
					continue
				}
				out.Set(k, raw)
			}
		case "tun":
			if hasAdv && !isObject(av) {
				out.Set(k, av)
				continue
			}
			sub := mergeObj(lay, "tun.", objOf(pv), objOf(fv), objOf(av), iss)
			if sub.Len() > 0 {
				out.Set(k, mihomoObjectRaw(sub))
			}
		default:
			if hasAdv {
				out.Set(k, av)
			}
		}
	}
	return mihomoObjectRaw(out)
}
