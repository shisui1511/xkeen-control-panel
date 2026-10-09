package nodes

import (
	"bytes"
	"errors"
	"regexp"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// maxNodes — предел узлов в одном разборе (T-145-04): больше — too_large.
const maxNodes = 5000

// maxBodyBytes — предел размера тела ответа или вставки (T-145-04).
const maxBodyBytes = 16 << 20

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

var (
	proxiesKeyRE = regexp.MustCompile(`(?m)^proxies\s*:`)
	// Тело в base64: строки без пробелов из алфавитов Std и URL, возможно с переносами.
	base64BodyRE = regexp.MustCompile(`^[A-Za-z0-9+/_=-]+(?:[\r\n]+[A-Za-z0-9+/_=-]+)*$`)
)

// ParseBody разбирает тело ответа провайдера или вставку: Xray-JSON, JSON
// sing-box, Clash YAML, wg.conf, base64 списка ссылок или список ссылок.
// Ошибка (*SourceError) — только когда не распознано ничего; нераспознанные
// строки при наличии узлов остаются в Skipped с кодом и номером строки.
func ParseBody(data []byte) (ParseResult, error) {
	return parseBody(data, 0)
}

func parseBody(data []byte, depth int) (ParseResult, error) {
	if len(data) > maxBodyBytes {
		return ParseResult{}, NewError(CodeTooLarge)
	}
	body := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ParseResult{}, NewError(CodeEmptyResponse)
	}
	if looksHTML(trimmed) {
		return ParseResult{}, NewError(CodeHTMLResponse)
	}
	text := string(trimmed)

	var (
		res ParseResult
		err error
	)
	switch {
	case services.LooksLikeWgQuickConf(text):
		res, err = ParseWG(text)
	case trimmed[0] == '{' || trimmed[0] == '[':
		var v any
		var serr *SourceError
		if v, serr = decodeJSON(trimmed); serr != nil {
			return ParseResult{}, serr
		}
		if looksSingbox(v) {
			res, err = parseSingboxValue(v)
		} else {
			res, err = parseXrayValue(v)
		}
	case proxiesKeyRE.MatchString(text):
		res, err = ParseClash(trimmed)
	case depth == 0 && len(text) >= 16 && base64BodyRE.MatchString(text):
		raw, derr := decodeBase64Any(strings.Join(strings.Fields(text), ""))
		if derr != nil {
			return ParseResult{}, NewError(CodeBadBase64)
		}
		res, err = parseBody(raw, depth+1)
		res.Format = "base64"
	default:
		res, err = parseLinks(string(body))
	}
	if err != nil {
		return res, err
	}
	if len(res.Nodes) == 0 {
		switch {
		case len(res.Skipped) == 0:
			return res, NewError(CodeEmptyResponse)
		case res.Format == "links" || res.Format == "base64":
			// Ни одной ссылки не распознано: это не подписка.
			allBad := true
			for _, sk := range res.Skipped {
				if sk.Code != CodeUnrecognized {
					allBad = false
					break
				}
			}
			if allBad {
				return res, NewError(CodeUnrecognized)
			}
		}
	}
	return res, nil
}

func looksHTML(b []byte) bool {
	head := b
	if len(head) > 64 {
		head = head[:64]
	}
	head = bytes.ToLower(head)
	return bytes.HasPrefix(head, []byte("<!doctype")) || bytes.HasPrefix(head, []byte("<html"))
}

// looksSingbox — JSON sing-box: у outbound есть type и нет protocol.
func looksSingbox(v any) bool {
	var list []any
	switch x := v.(type) {
	case map[string]any:
		list = listOf(x["outbounds"])
	case []any:
		list = x
	}
	for _, e := range list {
		if m := mapOf(e); m != nil {
			if _, has := m["protocol"]; !has && m["type"] != nil {
				return true
			}
		}
	}
	return false
}

// parseLinks разбирает список ссылок построчно: пустые строки и комментарии # пропускаются.
func parseLinks(text string) (ParseResult, error) {
	res := ParseResult{Format: "links"}
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n, err := ParseLink(line)
		if err != nil {
			var se *SourceError
			if !errors.As(err, &se) {
				se = NewError(CodeUnrecognized)
			}
			_, name := splitFragment(line)
			fragment := ""
			if scheme, _, ok := strings.Cut(line, "://"); ok && validScheme(strings.ToLower(scheme)) {
				fragment = strings.ToLower(scheme) + "://"
			}
			res.Skipped = append(res.Skipped, skippedFrom(se, i+1, name, fragment))
			continue
		}
		res.Nodes = append(res.Nodes, n)
		if len(res.Nodes) > maxNodes {
			return res, NewError(CodeTooLarge)
		}
	}
	return res, nil
}
