package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Профиль из шаблона (D-01…D-03): шаблон каталога превращается в рабочий профиль
// текстовой правкой. Комментарии, пустые строки, якоря и слияния шаблона
// сохраняются, эмодзи не кодируются: yaml.v3 используется только для разбора.

// ProfileTemplateFiles — шаблоны, из которых панель создаёт профиль (D-03).
// Шаблоны с готовыми узлами или своими адресами сюда не входят.
var ProfileTemplateFiles = []string{
	"rule-based.yaml",
	"global-proxy.yaml",
	"zkeen.yaml",
	"smart-antifilter.yaml",
	"split-services.yaml",
	"low-memory.yaml",
	"gaming-low-latency.yaml",
	"full-tunnel.yaml",
}

// IsProfileTemplateFile — файл шаблона разрешён для создания профиля.
func IsProfileTemplateFile(name string) bool {
	for _, f := range ProfileTemplateFiles {
		if f == name {
			return true
		}
	}
	return false
}

// ProfilePorts — порты прозрачного проксирования Mihomo. Ноль — порта нет.
type ProfilePorts struct {
	Redir, TProxy int
}

// TemplateAdded — что панель дописала шаблону и какие порты оказались в профиле.
type TemplateAdded struct {
	// Keys — добавленные ключи верхнего уровня в порядке записи.
	Keys []string
	// Redir и TProxy — порты профиля (значения шаблона либо добавленные).
	Redir, TProxy int
}

const (
	// templateDefaultRedir и templateDefaultTProxy — порты, когда их нет ни в
	// шаблоне, ни у активного профиля.
	templateDefaultRedir  = 5000
	templateDefaultTProxy = 5001
	// templateRoutingMark — метка, по которой XKeen отличает трафик ядра.
	templateRoutingMark = 255
	// templateProviderDir — каталог файлов источников в корне Mihomo.
	templateProviderDir = "proxy_providers"
)

// templateSourceIDRE — допустимый идентификатор источника: «my» или 8 hex. Значение
// попадает в текст профиля, поэтому проверяется здесь, а не только у вызывающего.
var templateSourceIDRE = regexp.MustCompile(`^(?:my|[0-9a-f]{8})$`)

// RenderMihomoTemplate готовит профиль из шаблона: блок proxy-providers заменяется
// ссылками на файлы источников (type: file, проверка здоровья выключена), а
// отсутствующие external-controller-unix, routing-mark, redir-port и tproxy-port
// дописываются. Порты берутся из шаблона, иначе у активного профиля (active), иначе
// 5000 и 5001. Диск не читается.
func RenderMihomoTemplate(content []byte, sourceIDs []string, active ProfilePorts) ([]byte, TemplateAdded, error) {
	var added TemplateAdded
	for _, id := range sourceIDs {
		if !templateSourceIDRE.MatchString(id) {
			return nil, added, fmt.Errorf("недопустимый идентификатор источника %q", id)
		}
	}
	var top map[string]any
	if err := yaml.Unmarshal(content, &top); err != nil {
		return nil, added, fmt.Errorf("шаблон не разобран: %w", err)
	}
	if top == nil {
		return nil, added, fmt.Errorf("шаблон пуст")
	}

	text := string(content)
	cr := strings.Contains(text, "\r\n")
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	lines = setProxyProviders(lines, sourceIDs)

	// Скаляры верхнего уровня, которых нет в шаблоне.
	redirPort, hasRedir := templatePort(top, "redir-port", "redir")
	tproxyPort, hasTProxy := templatePort(top, "tproxy-port", "tproxy")
	var extra []string
	if _, ok := top["external-controller-unix"]; !ok {
		added.Keys = append(added.Keys, "external-controller-unix")
		extra = append(extra, "external-controller-unix: "+DefaultMihomoSocketPath)
	}
	if _, ok := top["routing-mark"]; !ok {
		added.Keys = append(added.Keys, "routing-mark")
		extra = append(extra, "routing-mark: "+strconv.Itoa(templateRoutingMark))
	}
	if !hasRedir {
		redirPort = firstPort(active.Redir, templateDefaultRedir)
		added.Keys = append(added.Keys, "redir-port")
		extra = append(extra, "redir-port: "+strconv.Itoa(redirPort))
	}
	if !hasTProxy {
		tproxyPort = firstPort(active.TProxy, templateDefaultTProxy)
		added.Keys = append(added.Keys, "tproxy-port")
		extra = append(extra, "tproxy-port: "+strconv.Itoa(tproxyPort))
	}
	added.Redir, added.TProxy = redirPort, tproxyPort
	if len(extra) > 0 {
		lines = insertScalars(lines, extra)
	}

	out := strings.Join(lines, "\n")
	if cr {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	var check map[string]any
	if err := yaml.Unmarshal([]byte(out), &check); err != nil {
		return nil, TemplateAdded{}, fmt.Errorf("профиль из шаблона не разобран: %w", err)
	}
	return []byte(out), added, nil
}

// firstPort — порт, если задан, иначе запасной.
func firstPort(port, fallback int) int {
	if port > 0 {
		return port
	}
	return fallback
}

// templatePort ищет порт прозрачного входа: скаляр верхнего уровня key либо слушатель
// listeners указанного типа. Второй результат — порт задан.
func templatePort(top map[string]any, key, listenerType string) (int, bool) {
	if v, ok := top[key]; ok {
		if n := yamlInt(v); n > 0 {
			return n, true
		}
	}
	list, _ := top["listeners"].([]any)
	for _, item := range list {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if t, _ := m["type"].(string); t == listenerType {
			if n := yamlInt(m["port"]); n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}

// yamlInt приводит разобранное число к int; не число — 0.
func yamlInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return 0
}

// isBlankOrComment — строка пустая или целиком комментарий.
func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// providersBlock — текст блока proxy-providers для источников.
func providersBlock(ids []string) []string {
	if len(ids) == 0 {
		return []string{"proxy-providers: {}"}
	}
	out := []string{"proxy-providers:"}
	for _, id := range ids {
		name := "xcp-" + id
		out = append(out,
			"  "+name+":",
			"    type: file",
			"    path: ./"+templateProviderDir+"/"+name+".yaml",
			"    health-check:",
			"      enable: false",
		)
	}
	return out
}

// setProxyProviders заменяет блок proxy-providers блоком из файлов источников.
// Комментарии и пустые строки в хвосте прежнего блока остаются. Нет блока —
// вставка перед proxy-groups (вместе с комментариями, которые к ним относятся),
// иначе в конец.
func setProxyProviders(lines []string, ids []string) []string {
	block := providersBlock(ids)
	start, end, _ := findTopLevelSection(lines, "proxy-providers")
	if start >= 0 {
		last := start
		for i := start + 1; i < end; i++ {
			if !isBlankOrComment(lines[i]) {
				last = i
			}
		}
		out := make([]string, 0, len(lines)+len(block))
		out = append(out, lines[:start]...)
		out = append(out, block...)
		return append(out, lines[last+1:]...)
	}
	if gs, _, _ := findTopLevelSection(lines, "proxy-groups"); gs >= 0 {
		at := gs
		for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
			at--
		}
		ins := append(append([]string{}, block...), "")
		out := make([]string, 0, len(lines)+len(ins))
		out = append(out, lines[:at]...)
		out = append(out, ins...)
		return append(out, lines[at:]...)
	}
	// В конец: перед хвостовой пустой строкой файла.
	at := len(lines)
	if at > 0 && lines[at-1] == "" {
		at--
	}
	ins := append([]string{}, block...)
	out := make([]string, 0, len(lines)+len(ins))
	out = append(out, lines[:at]...)
	out = append(out, ins...)
	return append(out, lines[at:]...)
}

// topKeyRe — строка ключа верхнего уровня: без отступа, не комментарий и не элемент списка.
var topKeyRe = regexp.MustCompile(`^[^\s#\-][^:]*:(.*)$`)

// insertScalars вставляет скаляры верхнего уровня после первого блока скаляров
// (до первого ключа со вложением). Пустые строки и комментарии, относящиеся к этому
// ключу, остаются после вставки. Ключа со вложением нет — вставка в конец.
func insertScalars(lines, extra []string) []string {
	nested := -1
	for i, line := range lines {
		m := topKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := strings.TrimSpace(m[1])
		if v == "" || strings.HasPrefix(v, "#") {
			nested = i
			break
		}
	}
	at := len(lines)
	if nested >= 0 {
		at = nested
		for at > 0 && isBlankOrComment(lines[at-1]) {
			at--
		}
		if at == 0 {
			// Нет ни одного скаляра выше: вставка после шапки из комментариев.
			at = nested
			for at > 0 && strings.HasPrefix(strings.TrimSpace(lines[at-1]), "#") {
				at--
			}
		}
	} else if at > 0 && lines[at-1] == "" {
		at--
	}
	out := make([]string, 0, len(lines)+len(extra))
	out = append(out, lines[:at]...)
	out = append(out, extra...)
	return append(out, lines[at:]...)
}
