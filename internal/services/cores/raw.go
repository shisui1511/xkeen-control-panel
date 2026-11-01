package cores

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// RawResult — итог разбора «сырого» блока без записи: проблемы по ключам,
// разобранный остаток и поля формы, найденные в тексте (они принадлежат форме и
// при сохранении отклоняются как raw_dup_key). Синтаксическая ошибка — Error с
// номером строки.
type RawResult struct {
	Rejected []configlayer.KeyIssue `json:"rejected"`
	Value    json.RawMessage        `json:"value,omitempty"`
	Form     json.RawMessage        `json:"form,omitempty"`
	Error    *RawError              `json:"error,omitempty"`
}

// xkeenInboundTag сообщает, что тег входящего XKeen читает по имени: прозрачные
// redirect и tproxy и force-proxy*. Без прежнего значения других защищённых
// входящих разбор не знает.
func xkeenInboundTag(tag string) bool {
	return tag == "redirect" || tag == "tproxy" || configlayer.IsForceProxyTag(tag)
}

// ParseRaw проверяет текст «Дополнительно» секции id ядра kernel, ничего не
// записывая: JSON-объект (Xray; у «Входящих» — объект «тег → поля») или YAML
// (Mihomo). Текст больше 256 КиБ — *Error raw_too_large. Синтаксическая ошибка не
// ошибка вызова: она в RawResult.Error с номером строки. Ключи, занятые панелью,
// закреплённые и повторяющие поля формы, а также TCP external-controller без
// secret (external-controller и external-controller-tls в любой секции main,
// inbounds, other) попадают в Rejected. С соседними секциями черновика текст
// разбирает Service.ParseRaw.
func ParseRaw(kernel, id, text string) (RawResult, error) {
	return parseRaw(kernel, id, text, nil)
}

// parseRaw — ParseRaw с соседними секциями Mihomo siblings, из которых берётся
// secret контроллера.
func parseRaw(kernel, id, text string, siblings []*configlayer.OrderedObject) (RawResult, error) {
	kind, ok := kindOf(kernel, id)
	if !ok {
		return RawResult{}, &Error{Code: codeSectionUnknown}
	}
	res := RawResult{Rejected: []configlayer.KeyIssue{}}
	obj, err := parseAdvanced(kernel, text)
	if err != nil {
		var re *RawError
		if errors.As(err, &re) {
			res.Error = re
			return res, nil
		}
		return RawResult{}, err
	}
	iss := &issueSink{}
	value := mihomoObjectRaw(obj)

	switch kind {
	case kindXrayList:
		for _, key := range obj.Keys() {
			raw, _ := obj.Get(key)
			if !isObject(raw) {
				iss.add(key, configlayer.IssueCoreSectionInvalid)
				continue
			}
			if !xkeenInboundTag(key) {
				continue
			}
			ent := objOf(raw)
			for _, p := range xrayListSpec.pinned {
				if _, has := ooGetPath(ent, p); has && (len(p) > 1 || !slices.Contains(xrayListSpec.rowKeys, p[0])) {
					iss.add(key+"."+strings.Join(p, "."), configlayer.IssueRawPinned)
				}
			}
		}
	case kindMihomoInbounds:
		for _, k := range obj.Keys() {
			v, _ := obj.Get(k)
			switch {
			case mihomoRoutingKeys[k] || mihomoPinnedTop[k]:
				iss.add(k, configlayer.IssueRawReserved)
			case (k == "listeners" || k == "tun") && !isObject(v) && !isEmptyValue(v):
				iss.add(k, configlayer.IssueCoreSectionInvalid)
			}
		}
		// Ключи контроллера и secret полями формы не бывают: они целиком в тексте.
		for _, is := range mihomoControllerIssues(obj, siblings) {
			iss.add(is.Key, is.Code)
		}
	case kindRaw:
		addValueIssues(kernel, id, value, iss, siblings)
	default:
		lay := layoutFor(kernel, id, obj)
		form, rest := splitObj(lay, "", obj)
		if form.Len() > 0 {
			res.Form = mihomoObjectRaw(form)
			collectFormPaths(form, "", func(path string) { iss.add(path, issueRawDupKey) })
		}
		value = mihomoObjectRaw(rest)
		// Закреплённые значения Xray в остаток не попадают, но в тексте они
		// отклоняются так же, как при сохранении.
		addValueIssues(kernel, id, mihomoObjectRaw(obj), iss, siblings)
	}
	res.Rejected = append(res.Rejected, iss.list...)
	res.Value = value
	return res, nil
}

// addValueIssues добавляет проблемы значения секции (зарезервированные и
// закреплённые ключи, controller_no_secret).
func addValueIssues(kernel, id string, value json.RawMessage, iss *issueSink, siblings []*configlayer.OrderedObject) {
	var issues []configlayer.KeyIssue
	if kernel == configlayer.KernelXray {
		issues = XrayValueIssues(id, value, nil)
	} else {
		issues = mihomoValueIssues(id, value, siblings)
	}
	for _, is := range issues {
		iss.add(is.Key, is.Code)
	}
}

// collectFormPaths обходит объект формы и сообщает пути листьев.
func collectFormPaths(o *configlayer.OrderedObject, prefix string, fn func(path string)) {
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		if isObject(v) {
			sub := objOf(v)
			if sub.Len() > 0 {
				collectFormPaths(sub, prefix+k+".", fn)
				continue
			}
		}
		fn(prefix + k)
	}
}
