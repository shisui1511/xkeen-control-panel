package configlayer

import (
	"regexp"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
)

// Маскирование секретов предпросмотра «Применить» (D-11, T-149-07): окно изменений
// должно быть безопасно для снимка экрана, поэтому сервер заменяет значения
// учётных данных маской до отправки. Файлы ядер бывают JSON, JSONC и YAML (в том
// числе flow-стиль в одну строку), поэтому разбор построчный, без json.Unmarshal:
// в каждой строке перебираются все пары «ключ — значение». Страховка — замена
// любых подстрок вида UUID. LAN-адреса и прочие значения не трогаются.

// SecretMask — маска значения учётных данных; SecretMaskChanged — маска значения,
// которое отличается от стороны «было» (или которого там нет): изменение секрета
// видно, а сам секрет — нет.
const (
	SecretMask        = "••••••••"
	SecretMaskChanged = SecretMask + "*"
)

// maskExtraKeys — ключи сверх общего набора учётных данных узлов
// (nodes.IsCredentialKey), которые есть в файлах ядер: приватный ключ WireGuard
// Xray, публичный ключ и список short-id Reality, ключ защиты заголовка, seed mKCP.
// Имена приведены к виду normMaskKey.
var maskExtraKeys = map[string]bool{
	"secretkey":           true,
	"publickey":           true,
	"shortids":            true,
	"headerprotectionkey": true,
	"seed":                true,
}

var maskKeyReplacer = strings.NewReplacer("-", "", "_", "")

// normMaskKey — имя ключа без учёта регистра, «-» и «_».
func normMaskKey(name string) string {
	return strings.ToLower(maskKeyReplacer.Replace(name))
}

// isSecretKey — значение ключа name маскируется.
func isSecretKey(name string) bool {
	return nodes.IsCredentialKey(name) || maskExtraKeys[normMaskKey(name)]
}

// maskPairRe находит ключ и разделитель пары: ключ в двойных или одинарных кавычках
// или голый, затем «:» или «=». Значение разбирается кодом (masker.pairs): так
// вложенные пары flow-map и JSON в одну строку тоже находятся.
var maskPairRe = regexp.MustCompile(`(?:"((?:[^"\\\n]|\\.)*)"|'([^'\n]*)'|([A-Za-z_][A-Za-z0-9_-]*))[ \t]*[:=][ \t]*`)

// maskItemRe — элемент массива или flow-map под секретным ключом: строка в кавычках
// или голое значение.
var maskItemRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'[^']*'|[^\s,\[\]{}"':]+`)

// maskBlockScalarRe — индикатор блочного скаляра YAML («|», «>-», «|+2»): значение
// идёт следующими строками.
var maskBlockScalarRe = regexp.MustCompile(`^[|>][-+0-9]*$`)

// maskYAMLPairRe — строка внутри блока — пара YAML «ключ: значение» (разделитель
// только «:» с пробелом или концом строки: «=» бывает внутри base64-значения).
var maskYAMLPairRe = regexp.MustCompile(`^(?:"(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z_][A-Za-z0-9_-]*)[ \t]*:(?:[ \t]|$)`)

// maskUUIDRe — подстрока вида UUID (страховочная замена).
var maskUUIDRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// uuidMaskKey — ключ учёта подстрок UUID, найденных страховочной заменой.
const uuidMaskKey = "\x00uuid"

// maskPair маскирует обе стороны файла. Значение секрета «станет» сравнивается со
// значениями того же ключа (нормализованного) в «было»: совпало с тем же по
// порядку вхождением или есть среди значений ключа — SecretMask, иначе
// SecretMaskChanged. Второе условие нужно, чтобы новый узел в середине файла не
// помечал изменёнными секреты всех узлов после него. Третий результат — число
// изменившихся секретов.
func maskPair(before, after string) (string, string, int) {
	mb := newMasker(nil)
	b := mb.text(before)
	ma := newMasker(mb.vals)
	a := ma.text(after)
	return b, a, ma.changed
}

// maskLabel — страховочная замена подстрок вида UUID в подписи строки изменений.
func maskLabel(s string) string {
	return maskUUIDRe.ReplaceAllString(s, SecretMask)
}

// masker маскирует одну сторону файла построчно и запоминает значения секретов
// по ключам.
type masker struct {
	// ref — значения секретов стороны «было» по ключу (nil — это сама сторона «было»).
	ref    map[string][]string
	refSet map[string]map[string]bool
	// vals — значения секретов этой стороны по ключу, по порядку вхождения.
	vals    map[string][]string
	changed int
	// openKey — массив или объект под секретным ключом не закрылся на своей строке:
	// следующие строки маскируются поэлементно до закрывающей скобки.
	openKey   string
	openDepth int
	// blockKey — секретный ключ без значения на своей строке (YAML-блок, значение на
	// следующих строках): строки с отступом больше blockIndent маскируются целиком.
	blockKey    string
	blockIndent int
}

func newMasker(ref map[string][]string) *masker {
	m := &masker{ref: ref, vals: map[string][]string{}}
	if ref != nil {
		m.refSet = make(map[string]map[string]bool, len(ref))
		for k, vs := range ref {
			set := make(map[string]bool, len(vs))
			for _, v := range vs {
				set[v] = true
			}
			m.refSet[k] = set
		}
	}
	return m
}

// secret запоминает значение ключа и возвращает его маску.
func (m *masker) secret(key, raw string) string {
	idx := len(m.vals[key])
	m.vals[key] = append(m.vals[key], raw)
	if m.ref == nil {
		return SecretMask
	}
	if prev := m.ref[key]; (idx < len(prev) && prev[idx] == raw) || m.refSet[key][raw] {
		return SecretMask
	}
	m.changed++
	return SecretMaskChanged
}

// text маскирует текст файла построчно.
func (m *masker) text(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = m.line(ln)
	}
	return strings.Join(lines, "\n")
}

// line маскирует одну строку с учётом незакрытого массива и YAML-блока под
// секретным ключом с прошлых строк, затем заменяет оставшиеся подстроки UUID.
func (m *masker) line(s string) string {
	body := strings.TrimLeft(s, " \t")
	indent := len(s) - len(body)
	if m.blockKey != "" {
		if strings.TrimSpace(body) == "" {
			return s
		}
		if indent > m.blockIndent {
			return m.uuids(s[:indent] + m.blockLine(body))
		}
		m.blockKey = ""
	}
	head, rest := "", s
	if m.openKey != "" {
		end, depth, closed := bracketEnd(s, m.openDepth)
		if !closed {
			m.openDepth = depth
			return m.uuids(m.items(m.openKey, s))
		}
		head = m.items(m.openKey, s[:end]) + s[end:end+1]
		rest = s[end+1:]
		m.openKey = ""
	}
	return m.uuids(head + m.pairs(rest, indent, false))
}

// uuids заменяет подстроки вида UUID маской (с признаком изменения).
func (m *masker) uuids(s string) string {
	return maskUUIDRe.ReplaceAllStringFunc(s, func(u string) string {
		return m.secret(uuidMaskKey, strings.ToLower(u))
	})
}

// blockLine маскирует строку YAML-блока под секретным ключом: элемент списка или
// скаляр — целиком, вложенные пары — все значения.
func (m *masker) blockLine(body string) string {
	prefix := ""
	if strings.HasPrefix(body, "- ") {
		prefix, body = body[:2], body[2:]
	}
	if maskYAMLPairRe.MatchString(body) {
		return prefix + m.pairs(body, 0, true)
	}
	return prefix + m.value(m.blockKey, body)
}

// value маскирует скаляр целиком, сохраняя кавычки и хвостовые пробелы.
func (m *masker) value(key, s string) string {
	v := strings.TrimRight(s, " \t\r")
	if v == "" {
		return s
	}
	tail := s[len(v):]
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return string(v[0]) + m.secret(key, v[1:len(v)-1]) + string(v[0]) + tail
	}
	return m.secret(key, v) + tail
}

// items маскирует каждый элемент массива или flow-map под секретным ключом.
func (m *masker) items(key, s string) string {
	return maskItemRe.ReplaceAllStringFunc(s, func(tok string) string {
		if q := tok[0]; (q == '"' || q == '\'') && len(tok) >= 2 {
			return string(q) + m.secret(key, tok[1:len(tok)-1]) + string(q)
		}
		return m.secret(key, tok)
	})
}

// pairs перебирает все пары «ключ — значение» строки и маскирует значения
// секретных ключей (force — все ключи секретные: строки внутри блока под секретным
// ключом). indent — отступ строки для YAML-блока.
func (m *masker) pairs(s string, indent int, force bool) string {
	var b strings.Builder
	pos, depth := 0, 0
	for pos < len(s) {
		loc := maskPairRe.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			break
		}
		depth = max(depth+flowDepth(s[pos:pos+loc[0]]), 0)
		var key string
		switch {
		case loc[2] >= 0:
			key = s[pos+loc[2] : pos+loc[3]]
		case loc[4] >= 0:
			key = s[pos+loc[4] : pos+loc[5]]
		default:
			key = s[pos+loc[6] : pos+loc[7]]
		}
		vs := pos + loc[1]
		b.WriteString(s[pos:vs])
		pos = vs
		secret := force || isSecretKey(key)
		nk := normMaskKey(key)
		rest := strings.TrimRight(s[vs:], " \t\r")
		if rest == "" || strings.HasPrefix(rest, "#") || maskBlockScalarRe.MatchString(rest) {
			// Значение на следующих строках (YAML-блок, блочный скаляр «|» или «>»,
			// перенос строки в JSON). Комментарий «//» сюда не относится: значение,
			// начатое с «//», маскируется как обычное.
			if secret && !force && depth == 0 {
				m.blockKey, m.blockIndent = nk, indent
			}
			break
		}
		switch c := s[vs]; c {
		case '{', '[':
			if !secret {
				// Вложенные пары разбираются дальше по строке.
				continue
			}
			end, d, closed := bracketEnd(s[vs+1:], 1)
			b.WriteByte(c)
			if !closed {
				b.WriteString(m.items(nk, s[vs+1:]))
				m.openKey, m.openDepth = nk, d
				return b.String()
			}
			b.WriteString(m.items(nk, s[vs+1:vs+1+end]))
			b.WriteByte(s[vs+1+end])
			pos = vs + 2 + end
		case '"', '\'':
			if end := quotedEnd(s, vs); end > 0 {
				if secret {
					b.WriteByte(c)
					b.WriteString(m.secret(nk, s[vs+1:end-1]))
					b.WriteByte(c)
				} else {
					b.WriteString(s[vs:end])
				}
				pos = end
				continue
			}
			// Незакрытая кавычка (строка обрезана): значение до конца строки.
			pos = m.bare(&b, s, vs, len(s), nk, secret, depth)
		default:
			pos = m.bare(&b, s, vs, bareEnd(s, vs, depth), nk, secret, depth)
		}
	}
	b.WriteString(s[pos:])
	return b.String()
}

// bare пишет голое значение s[vs:end] (секретное — маской) и возвращает end.
// Вне скобок хвостовая запятая JSON остаётся видимой.
func (m *masker) bare(b *strings.Builder, s string, vs, end int, key string, secret bool, depth int) int {
	raw := s[vs:end]
	val := strings.TrimRight(raw, " \t\r")
	if depth == 0 {
		val = strings.TrimRight(strings.TrimSuffix(val, ","), " \t")
	}
	if secret && val != "" {
		b.WriteString(m.secret(key, val))
		b.WriteString(raw[len(val):])
	} else {
		b.WriteString(raw)
	}
	return end
}

// bareEnd — конец голого значения: внутри скобок — до «,», «}» или «]», вне скобок
// (YAML-блок, JSON с отступами) — до конца строки.
func bareEnd(s string, vs, depth int) int {
	if depth == 0 {
		return len(s)
	}
	if i := strings.IndexAny(s[vs:], ",}]"); i >= 0 {
		return vs + i
	}
	return len(s)
}

// quotedEnd — индекс после закрывающей кавычки строки, начатой в s[i]; -1 —
// строка не закрыта. В двойных кавычках учитываются экранирования «\», в
// одинарных — удвоенная одинарная кавычка (YAML).
func quotedEnd(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch {
		case q == '"' && s[j] == '\\':
			j++
		case s[j] == q:
			if q == '\'' && j+1 < len(s) && s[j+1] == '\'' {
				j++
				continue
			}
			return j + 1
		}
	}
	return -1
}

// bracketEnd ищет скобку, закрывающую уровень depth (строки в кавычках
// пропускаются): индекс закрывающей скобки и true, либо длину строки, оставшуюся
// глубину и false.
func bracketEnd(s string, depth int) (int, int, bool) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			end := quotedEnd(s, i)
			if end < 0 {
				return len(s), depth, false
			}
			i = end - 1
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i, 0, true
			}
		}
	}
	return len(s), depth, false
}

// flowDepth — изменение глубины скобок flow-стиля в отрезке строки (строки в
// кавычках пропускаются).
func flowDepth(s string) int {
	d := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			end := quotedEnd(s, i)
			if end < 0 {
				return d
			}
			i = end - 1
		case '{', '[':
			d++
		case '}', ']':
			d--
		}
	}
	return d
}
