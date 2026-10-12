package nodes

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Общие помощники разборщиков: приведение значений из деревьев JSON и YAML
// (json.Number, int, float64, bool, строки) к нужным типам.

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

// strOf приводит скаляр к строке; не скаляр даёт пустую строку.
func strOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// intOf приводит число (или строку с числом) к int; не число даёт 0.
func intOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case uint64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
		if f, err := x.Float64(); err == nil {
			return int(f)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return i
		}
	}
	return 0
}

// boolOf читает флаг; ok == false, если значения нет или оно не флаг.
func boolOf(v any) (val, ok bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		if x == "" {
			return false, false
		}
		return truthy(x), true
	}
	return false, false
}

func boolPtrOf(v any) *bool {
	if b, ok := boolOf(v); ok {
		return &b
	}
	return nil
}

// strListOf читает список строк; одиночная строка разбивается по запятым.
func strListOf(v any) []string {
	switch x := v.(type) {
	case []any:
		var out []string
		for _, e := range x {
			if s := strOf(e); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), x...)
	case string:
		return splitList(x)
	}
	return nil
}

// firstStr — первый элемент списка (или сам скаляр) как строка.
func firstStr(v any) string {
	if l, ok := v.([]any); ok {
		if len(l) == 0 {
			return ""
		}
		return strOf(l[0])
	}
	return strOf(v)
}

// headersOf разделяет карту заголовков на Host и остальное; значения-списки
// берутся по первому элементу.
func headersOf(v any) (host string, rest map[string]string) {
	m := mapOf(v)
	if len(m) == 0 {
		return "", nil
	}
	for k, val := range m {
		s := firstStr(val)
		if strings.EqualFold(k, "host") {
			if host == "" {
				host = s
			}
			continue
		}
		if rest == nil {
			rest = map[string]string{}
		}
		rest[k] = s
	}
	return host, rest
}

// pickStr — первое непустое строковое значение из перечисленных ключей карты.
func pickStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strOf(m[k]); s != "" {
			return s
		}
	}
	return ""
}

// fragmentOf обрезает фрагмент для Skipped до 80 символов.
func fragmentOf(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= 80 {
		return s
	}
	return string([]rune(s)[:80])
}

// marshalIndent — JSON с отступом в два пробела без экранирования HTML.
func marshalIndent(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

// lineAt — номер строки (с 1) по смещению в тексте.
func lineAt(data []byte, offset int64) int {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	return bytes.Count(data[:offset], []byte{'\n'}) + 1
}

// jsonError строит json_error с номером строки.
func jsonError(data []byte, offset int64) *SourceError {
	line := lineAt(data, offset)
	e := NewError(CodeJSONError, "line", strconv.Itoa(line))
	e.Line = line
	return e
}

// decodeJSON разбирает один JSON-документ; числа остаются json.Number.
// Пустой ввод — empty_response, ошибка синтаксиса — json_error с номером строки.
func decodeJSON(data []byte) (any, *SourceError) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) && len(bytes.TrimSpace(data)) == 0 {
			return nil, NewError(CodeEmptyResponse)
		}
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			return nil, jsonError(data, syn.Offset)
		}
		return nil, jsonError(data, int64(len(data)))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, jsonError(data, dec.InputOffset())
	}
	return v, nil
}

// hasKeyDeep ищет ключ на любом уровне вложенности.
func hasKeyDeep(v any, key string, depth int) bool {
	if depth > 200 {
		return false
	}
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k == key || hasKeyDeep(e, key, depth+1) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasKeyDeep(e, key, depth+1) {
				return true
			}
		}
	}
	return false
}

// credentialKeys — ключи структурных записей (Xray-JSON, Clash, sing-box), значения
// которых — учётные данные; имена приведены к нижнему регистру без «-» и «_».
var credentialKeys = map[string]bool{
	"id": true, "uuid": true, "pass": true, "passwd": true, "password": true, "pwd": true,
	"auth": true, "authstr": true, "authpayload": true, "token": true, "secret": true,
	"psk": true, "presharedkey": true, "privatekey": true, "obfspassword": true,
	"shortid": true, "key": true,
}

// credentialValues собирает значения учётных данных из сырой записи, включая
// вложенные карты и списки (vnext, users, peers). Записи, которая не стала узлом,
// эти значения нужны только для маскирования в диагностике.
func credentialValues(v any) []string {
	var out []string
	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 8 {
			return
		}
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				name := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(k))
				if s, ok := val.(string); ok && credentialKeys[name] && s != "" {
					out = append(out, s)
					continue
				}
				walk(val, depth+1)
			}
		case []any:
			for _, e := range x {
				walk(e, depth+1)
			}
		}
	}
	walk(v, 0)
	return out
}
