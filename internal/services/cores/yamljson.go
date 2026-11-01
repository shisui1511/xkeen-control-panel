package cores

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Преобразование YAML ↔ упорядоченный JSON (Mihomo). Значения секций Mihomo
// хранятся в черновике как JSON с сохранённым порядком ключей: порядок значим
// для hosts, nameserver-policy и sub-rules, а обычный map его теряет.
// Переход идёт напрямую по yaml.Node, без промежуточного map.

const (
	// maxRawBytes — предел размера текста профиля или «сырого» блока (A7).
	maxRawBytes = 256 << 10
	// maxProfileBytes — предел размера активного профиля Mihomo при переносе
	// ключей в секции: профиль со встроенными узлами подписки и списками правил
	// заметно больше текста «Дополнительно».
	maxProfileBytes = 8 << 20
	// maxYAMLNodes — предел числа посещённых узлов при раскрытии якорей и
	// ключей слияния: защита от «billion laughs» (T-147-18).
	maxYAMLNodes = 200000
	// maxYAMLDepth — предел вложенности документа.
	maxYAMLDepth = 200
)

// yamlConvError — ошибка преобразования с номером строки узла YAML.
type yamlConvError struct {
	line int
	msg  string
}

func (e *yamlConvError) Error() string {
	if e.line > 0 {
		return fmt.Sprintf("строка %d: %s", e.line, e.msg)
	}
	return e.msg
}

// yamlJSONEntry — пара «ключ, значение» уже преобразованного отображения.
type yamlJSONEntry struct {
	key string
	val []byte
}

// yamlJSONState — счётчик посещённых узлов одного преобразования.
type yamlJSONState struct {
	visited int
}

// YAMLToOrderedJSON преобразует узел YAML в JSON, сохраняя порядок ключей.
// Якоря раскрываются, ключи слияния (<<) подмешивают отсутствующие ключи; число
// посещённых узлов ограничено. Ключ отображения обязан быть скаляром.
func YAMLToOrderedJSON(n *yaml.Node) (json.RawMessage, error) {
	if n == nil {
		return nil, errors.New("пустой узел YAML")
	}
	st := &yamlJSONState{}
	var buf bytes.Buffer
	if err := st.write(&buf, n, 0); err != nil {
		return nil, err
	}
	return json.RawMessage(buf.Bytes()), nil
}

func (st *yamlJSONState) write(buf *bytes.Buffer, n *yaml.Node, depth int) error {
	st.visited++
	if st.visited > maxYAMLNodes {
		return &yamlConvError{line: n.Line, msg: "слишком много узлов YAML (раскрытие якорей)"}
	}
	if depth > maxYAMLDepth {
		return &yamlConvError{line: n.Line, msg: "слишком глубокая вложенность YAML"}
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			buf.WriteString("null")
			return nil
		}
		return st.write(buf, n.Content[0], depth+1)
	case yaml.AliasNode:
		if n.Alias == nil {
			return &yamlConvError{line: n.Line, msg: "якорь не найден"}
		}
		return st.write(buf, n.Alias, depth+1)
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := st.write(buf, c, depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case yaml.MappingNode:
		entries, err := st.entries(n, depth+1)
		if err != nil {
			return err
		}
		buf.WriteByte('{')
		for i, e := range entries {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := yamlJSONString(e.key)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			buf.Write(e.val)
		}
		buf.WriteByte('}')
		return nil
	case yaml.ScalarNode:
		return st.scalar(buf, n)
	}
	return &yamlConvError{line: n.Line, msg: "неизвестный узел YAML"}
}

// entries собирает пары отображения в порядке документа; ключ слияния подмешивает
// только отсутствующие ключи (явные ключи отображения сильнее, раньше стоящее
// отображение списка слияния сильнее позднего).
func (st *yamlJSONState) entries(n *yaml.Node, depth int) ([]yamlJSONEntry, error) {
	if depth > maxYAMLDepth {
		return nil, &yamlConvError{line: n.Line, msg: "слишком глубокая вложенность YAML"}
	}
	explicit := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Kind == yaml.ScalarNode && k.ShortTag() != "!!merge" {
			explicit[k.Value] = true
		}
	}
	var out []yamlJSONEntry
	pos := map[string]int{}
	put := func(key string, val []byte) {
		if i, ok := pos[key]; ok {
			out[i].val = val
			return
		}
		pos[key] = len(out)
		out = append(out, yamlJSONEntry{key: key, val: val})
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind == yaml.AliasNode && k.Alias != nil {
			k = k.Alias
		}
		if k.Kind != yaml.ScalarNode {
			return nil, &yamlConvError{line: k.Line, msg: "ключ отображения должен быть скаляром"}
		}
		if k.ShortTag() == "!!merge" {
			merged, err := st.mergeSources(v, depth+1)
			if err != nil {
				return nil, err
			}
			for _, e := range merged {
				if explicit[e.key] {
					continue
				}
				if _, seen := pos[e.key]; seen {
					continue
				}
				put(e.key, e.val)
			}
			continue
		}
		var vb bytes.Buffer
		if err := st.write(&vb, v, depth+1); err != nil {
			return nil, err
		}
		put(k.Value, vb.Bytes())
	}
	return out, nil
}

// mergeSources раскрывает значение ключа слияния: отображение, алиас на него или
// список таких отображений. Пары идут в порядке приоритета (раньше — сильнее).
func (st *yamlJSONState) mergeSources(v *yaml.Node, depth int) ([]yamlJSONEntry, error) {
	st.visited++
	if st.visited > maxYAMLNodes {
		return nil, &yamlConvError{line: v.Line, msg: "слишком много узлов YAML (раскрытие якорей)"}
	}
	switch v.Kind {
	case yaml.AliasNode:
		if v.Alias == nil {
			return nil, &yamlConvError{line: v.Line, msg: "якорь не найден"}
		}
		return st.mergeSources(v.Alias, depth+1)
	case yaml.MappingNode:
		return st.entries(v, depth+1)
	case yaml.SequenceNode:
		var out []yamlJSONEntry
		seen := map[string]bool{}
		for _, c := range v.Content {
			part, err := st.mergeSources(c, depth+1)
			if err != nil {
				return nil, err
			}
			for _, e := range part {
				if seen[e.key] {
					continue
				}
				seen[e.key] = true
				out = append(out, e)
			}
		}
		return out, nil
	}
	return nil, &yamlConvError{line: v.Line, msg: "ключ слияния ожидает отображение"}
}

// scalar пишет скаляр по тегу: null, bool, целое и дробное числа — числами,
// остальное — строкой.
func (st *yamlJSONState) scalar(buf *bytes.Buffer, n *yaml.Node) error {
	switch n.ShortTag() {
	case "!!null":
		buf.WriteString("null")
		return nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err == nil {
			buf.WriteString(strconv.FormatBool(b))
			return nil
		}
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			buf.WriteString(strconv.FormatInt(i, 10))
			return nil
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			buf.WriteString(strconv.FormatUint(u, 10))
			return nil
		}
	case "!!float":
		var f float64
		if err := n.Decode(&f); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			buf.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
			return nil
		}
	}
	sb, err := yamlJSONString(n.Value)
	if err != nil {
		return err
	}
	buf.Write(sb)
	return nil
}

// yamlJSONString кодирует строку без HTML-экранирования.
func yamlJSONString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// OrderedJSONToYAML строит узел YAML из JSON с сохранением порядка ключей.
// Строки получают тег !!str (кодировщик сам берёт кавычки там, где строка
// читалась бы как число или bool), числа остаются числами. Исключение —
// строка «<<»: она всегда в двойных кавычках (yamlStrNode).
func OrderedJSONToYAML(raw json.RawMessage) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	n, err := yamlNodeFromJSON(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("данные после значения JSON")
	}
	return n, nil
}

func yamlNodeFromJSON(dec *json.Decoder, depth int) (*yaml.Node, error) {
	if depth > maxYAMLDepth {
		return nil, errors.New("слишком глубокая вложенность JSON")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := kt.(string)
				if !ok {
					return nil, errors.New("ключ объекта: ожидалась строка")
				}
				v, err := yamlNodeFromJSON(dec, depth+1)
				if err != nil {
					return nil, err
				}
				m.Content = append(m.Content, yamlStrNode(key), v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case '[':
			s := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			for dec.More() {
				v, err := yamlNodeFromJSON(dec, depth+1)
				if err != nil {
					return nil, err
				}
				s.Content = append(s.Content, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return s, nil
		}
		return nil, errors.New("неожиданный разделитель JSON")
	case string:
		return yamlStrNode(t), nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case json.Number:
		text := t.String()
		if !strings.ContainsAny(text, ".eE") {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: text}, nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		// Запись «1e5» без точки YAML 1.1 читает строкой: дробное число
		// пишется всегда с точкой.
		val := strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(val, ".") {
			val += ".0"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: val}, nil
	}
	return nil, errors.New("неожиданный токен JSON")
}

// yamlMergeKey — ключ слияния YAML 1.1. Без кавычек yaml.v3 и Mihomo читают
// его как слияние, а не как обычный ключ.
const yamlMergeKey = "<<"

func yamlStrNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	// Простым скаляром «<<» стал бы ключом слияния и подмешал вложенное
	// отображение в корень профиля мимо controller_no_secret (T-147-19);
	// кодировщик кавычки сам не ставит, поэтому стиль задан явно. Правило
	// действует и для значений-строк «<<».
	if s == yamlMergeKey {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// yamlErrLine ищет номер строки в тексте ошибки разбора yaml.v3
// («line 3: …» или «yaml: line 3: …»).
var yamlErrLine = regexp.MustCompile(`line (\d+)`)

// parseYAMLRoot разбирает текст YAML в узел корня. Пустой документ (в том числе
// из одних комментариев или явный null) — nil без ошибки. Возвращает номер
// строки ошибки (0 — неизвестен).
func parseYAMLRoot(text []byte) (*yaml.Node, int, error) {
	return parseYAMLRootLimit(text, maxRawBytes)
}

// parseYAMLRootLimit — parseYAMLRoot с явным пределом размера текста.
func parseYAMLRootLimit(text []byte, limit int) (*yaml.Node, int, error) {
	if len(text) > limit {
		return nil, 0, fmt.Errorf("текст больше %d КиБ", limit>>10)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(text, &doc); err != nil {
		line := 0
		if m := yamlErrLine.FindStringSubmatch(err.Error()); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		return nil, line, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, 0, nil
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && root.ShortTag() == "!!null" {
		return nil, 0, nil
	}
	if root.Kind == yaml.AliasNode && root.Alias != nil {
		root = root.Alias
	}
	return root, 0, nil
}

// ParseYAMLText разбирает текст YAML в упорядоченный JSON-объект. Пустой
// документ — {}. Корень, не являющийся отображением, и текст больше maxRawBytes
// — ошибка; line — строка ошибки (0, если неизвестна).
func ParseYAMLText(text string) (value json.RawMessage, line int, err error) {
	root, line, err := parseYAMLRoot([]byte(text))
	if err != nil {
		return nil, line, err
	}
	if root == nil {
		return json.RawMessage("{}"), 0, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, root.Line, errors.New("корень документа должен быть отображением")
	}
	raw, err := YAMLToOrderedJSON(root)
	if err != nil {
		var ce *yamlConvError
		if errors.As(err, &ce) {
			return nil, ce.line, err
		}
		return nil, 0, err
	}
	return raw, 0, nil
}

// RenderYAML выводит YAML (отступ 2) из JSON с сохранением порядка ключей.
func RenderYAML(raw json.RawMessage) (string, error) {
	n, err := OrderedJSONToYAML(raw)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
