package services

import (
	"fmt"
	"regexp"
	"strings"
)

// Этот файл содержит text-based редактор Mihomo config.yaml и парсер метаданных прокси.
//
// Почему text-based, а не yaml.Unmarshal/Marshal:
//  1. Mihomo конфиги пользователей содержат комментарии (#), которые
//     `gopkg.in/yaml.v3` теряет при roundtrip.
//  2. Сохранение порядка ключей в map требует yaml.Node API, который
//     многословен и хрупок для глубоко вложенных структур.
//  3. Нам нужно править только две секции: proxies: и proxy-groups:.
//     Остальной конфиг трогать опасно (DNS, tun, listeners и т.п.).

var proxyNameRe = regexp.MustCompile(`^(\s*)-\s+name:\s*['"]?([^'"#\r\n]+?)['"]?\s*(?:#.*)?$`)

func findTopLevelSection(lines []string, sectionName string) (start, end, baseIndent int) {
	header := sectionName + ":"
	start = -1
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		if trimmed == header || strings.HasPrefix(trimmed, header+" ") || strings.HasPrefix(trimmed, header+"\t") {
			if len(line) == len(strings.TrimLeft(line, " \t")) {
				start = i
				break
			}
		}
	}
	if start == -1 {
		return -1, -1, 0
	}

	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		raw := strings.TrimLeft(lines[i], " \t")
		if len(lines[i]) == len(raw) && !strings.HasPrefix(raw, "- ") {
			end = i
			break
		}
	}

	baseIndent = 2
	for i := start + 1; i < end; i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if strings.HasPrefix(trimmed, "- ") {
			baseIndent = len(lines[i]) - len(trimmed)
			break
		}
	}

	return start, end, baseIndent
}

type proxyBlock struct {
	Name      string
	StartLine int
	EndLine   int
}

func extractProxyBlocks(lines []string, sectionStart, sectionEnd, baseIndent int) []proxyBlock {
	var blocks []proxyBlock
	var current *proxyBlock

	for i := sectionStart + 1; i < sectionEnd; i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)

		if indent == baseIndent && strings.HasPrefix(trimmed, "- ") {
			if current != nil {
				current.EndLine = i
				blocks = append(blocks, *current)
			}
			m := proxyNameRe.FindStringSubmatch(line)
			name := ""
			if len(m) >= 3 {
				name = strings.TrimSpace(m[2])
			}
			current = &proxyBlock{Name: name, StartLine: i}
			continue
		}
	}
	if current != nil {
		current.EndLine = sectionEnd
		blocks = append(blocks, *current)
	}
	return blocks
}

func trimTrailingEmpty(lines []string, start, end int) int {
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return end
}

func ReplaceMihomoProxies(content string, oldNames []string, newBlocks []string) string {
	lines := strings.Split(content, "\n")
	start, end, indent := findTopLevelSection(lines, "proxies")

	if start == -1 {
		appended := "\nproxies:\n" + joinIndentedBlocks(newBlocks, 2)
		if strings.HasSuffix(content, "\n") {
			return content + strings.TrimPrefix(appended, "\n")
		}
		return content + appended
	}

	blocks := extractProxyBlocks(lines, start, end, indent)
	removeSet := make(map[string]bool, len(oldNames))
	for _, n := range oldNames {
		removeSet[n] = true
	}

	var out []string
	out = append(out, lines[:start+1]...)

	for _, b := range blocks {
		if removeSet[b.Name] {
			continue
		}
		blockEnd := trimTrailingEmpty(lines, b.StartLine, b.EndLine)
		out = append(out, lines[b.StartLine:blockEnd]...)
	}

	for _, nb := range newBlocks {
		nbLines := strings.Split(strings.TrimRight(nb, "\n"), "\n")
		out = append(out, nbLines...)
	}

	if end < len(lines) {
		out = append(out, lines[end:]...)
	}

	return strings.Join(out, "\n")
}

func joinIndentedBlocks(blocks []string, indent int) string {
	var sb strings.Builder
	for _, b := range blocks {
		sb.WriteString(strings.TrimRight(b, "\n"))
		sb.WriteString("\n")
	}
	_ = indent
	return sb.String()
}

func findGroupBlock(lines []string, groupName string) (start, end, indent int) {
	gStart, gEnd, gIndent := findTopLevelSection(lines, "proxy-groups")
	if gStart == -1 {
		return -1, -1, 0
	}
	blocks := extractProxyBlocks(lines, gStart, gEnd, gIndent)
	for _, b := range blocks {
		if b.Name == groupName {
			return b.StartLine, b.EndLine, gIndent
		}
	}
	return -1, -1, 0
}

func UpdateMihomoGroupProxies(content, groupName string, addNames, removeNames []string) string {
	lines := strings.Split(content, "\n")
	gStart, gEnd, gIndent := findGroupBlock(lines, groupName)
	if gStart == -1 {
		return content
	}

	subIndent := gIndent + 2
	subStart, subEnd := -1, -1
	for i := gStart + 1; i < gEnd; i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		ind := len(line) - len(trimmed)
		if ind == subIndent && strings.HasPrefix(trimmed, "proxies:") {
			subStart = i
			subEnd = gEnd
			for j := i + 1; j < gEnd; j++ {
				l := lines[j]
				t := strings.TrimLeft(l, " \t")
				if strings.TrimSpace(l) == "" {
					continue
				}
				if len(l)-len(t) <= subIndent {
					subEnd = j
					break
				}
			}
			break
		}
	}

	existing := []string{}
	if subStart != -1 {
		itemIndent := subIndent + 2
		for i := subStart + 1; i < subEnd; i++ {
			l := lines[i]
			t := strings.TrimLeft(l, " \t")
			if len(l)-len(t) >= itemIndent && strings.HasPrefix(t, "- ") {
				name := strings.TrimSpace(strings.TrimPrefix(t, "- "))
				name = strings.Trim(name, `"'`)
				existing = append(existing, name)
			}
		}
	}

	removeSet := map[string]bool{}
	for _, n := range removeNames {
		removeSet[n] = true
	}

	filtered := existing[:0]
	for _, n := range existing {
		if !removeSet[n] {
			filtered = append(filtered, n)
		}
	}

	existingSet := map[string]bool{}
	for _, n := range filtered {
		existingSet[n] = true
	}
	for _, n := range addNames {
		if !existingSet[n] {
			filtered = append(filtered, n)
			existingSet[n] = true
		}
	}

	subPad := strings.Repeat(" ", subIndent)
	itemPad := strings.Repeat(" ", subIndent+2)
	newSubLines := []string{subPad + "proxies:"}
	for _, n := range filtered {
		newSubLines = append(newSubLines, fmt.Sprintf("%s- %s", itemPad, yamlSafeScalar(n)))
	}

	var out []string
	if subStart != -1 {
		out = append(out, lines[:subStart]...)
		out = append(out, newSubLines...)
		out = append(out, lines[subEnd:]...)
	} else {
		out = append(out, lines[:gStart+1]...)
		out = append(out, newSubLines...)
		out = append(out, lines[gStart+1:]...)
	}

	return strings.Join(out, "\n")
}

var yamlNeedsQuotingRe = regexp.MustCompile(`[\s:#\[\]{}&,*>!%` + "`" + `"'|@?]`)

var yamlSpecialKeywords = map[string]bool{
	"null": true, "~": true, "true": true, "false": true,
	"yes": true, "no": true, "on": true, "off": true,
}

func yamlSafeScalar(v string) string {
	if v == "" {
		return "''"
	}
	low := strings.ToLower(strings.TrimSpace(v))
	if yamlSpecialKeywords[low] || yamlNeedsQuotingRe.MatchString(v) {
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	if isNumericLike(v) {
		return "'" + v + "'"
	}
	return v
}

func ParseMihomoSubscriptionBlocks(content string) (blocks []string, names []string) {
	normalized := strings.TrimSpace(content)
	if !strings.HasPrefix(normalized, "proxies:") && !strings.Contains(normalized, "\nproxies:") {
		normalized = "proxies:\n" + normalized
	}

	lines := strings.Split(normalized, "\n")
	start, end, indent := findTopLevelSection(lines, "proxies")
	if start == -1 {
		return nil, nil
	}

	proxyBlocks := extractProxyBlocks(lines, start, end, indent)
	for _, b := range proxyBlocks {
		if b.Name == "" {
			continue
		}
		blockEnd := trimTrailingEmpty(lines, b.StartLine, b.EndLine)
		rawLines := lines[b.StartLine:blockEnd]

		if indent != 2 {
			reindented := make([]string, len(rawLines))
			for i, l := range rawLines {
				t := strings.TrimLeft(l, " \t")
				cur := len(l) - len(t)
				rel := cur - indent
				ni := 2 + rel
				if ni < 0 {
					ni = 0
				}
				reindented[i] = strings.Repeat(" ", ni) + t
			}
			rawLines = reindented
		}

		blocks = append(blocks, strings.Join(rawLines, "\n"))
		names = append(names, b.Name)
	}
	return blocks, names
}

func isNumericLike(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= '0' && r <= '9' {
			continue
		}
		if (r == '-' || r == '+') && i == 0 {
			continue
		}
		if r == '.' && i > 0 {
			continue
		}
		return false
	}
	return true
}

type clashProxy map[string]string

func (c clashProxy) get(key string) string { return c[key] }

func parseClashProxyBlock(blockStr string) clashProxy {
	result := make(clashProxy)
	lines := strings.Split(blockStr, "\n")

	baseIndent := -1
	for _, l := range lines {
		raw := strings.TrimLeft(l, " \t")
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if strings.HasPrefix(raw, "- ") {
			baseIndent = len(l) - len(raw)
			break
		}
	}
	if baseIndent < 0 {
		baseIndent = 0
	}

	topLevel := baseIndent
	fieldLevel := baseIndent + 2
	nestedLevel := baseIndent + 4
	deepLevel := baseIndent + 6

	var section string
	var subSection string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		raw := strings.TrimLeft(line, " \t")
		indent := len(line) - len(raw)

		content := trimmed
		if strings.HasPrefix(content, "- ") {
			content = content[2:]
			indent = topLevel
		}

		if indent <= fieldLevel {
			if indent <= topLevel {
				section = ""
			}
			subSection = ""
		} else if indent == nestedLevel {
			subSection = ""
		}

		if colonIdx := strings.Index(content, ": "); colonIdx >= 0 {
			key := content[:colonIdx]
			value := strings.TrimSpace(content[colonIdx+2:])
			if ci := strings.Index(value, " #"); ci >= 0 {
				value = strings.TrimSpace(value[:ci])
			}
			if len(value) >= 2 &&
				((value[0] == '"' && value[len(value)-1] == '"') ||
					(value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}

			switch {
			case indent == topLevel || indent == fieldLevel:
				result[key] = value
			case indent == nestedLevel && section != "":
				result[section+"."+key] = value
			case indent == deepLevel && section != "" && subSection != "":
				result[section+"."+subSection+"."+key] = value
			}
		} else if strings.HasSuffix(content, ":") {
			key := strings.TrimSuffix(content, ":")
			switch indent {
			case fieldLevel:
				section = key
			case nestedLevel:
				if section != "" {
					subSection = key
				}
			}
		} else if strings.HasPrefix(content, "- ") {
			value := strings.TrimPrefix(content, "- ")
			if indent == nestedLevel && section != "" {
				existing := result[section+"._list"]
				if existing == "" {
					result[section+"._list"] = value
				} else {
					result[section+"._list"] = existing + "," + value
				}
			}
		}
	}

	return result
}

// ParseClashProxyNode разбирает YAML блок прокси Clash и генерирует SubscriptionNode.
func ParseClashProxyNode(blockStr string) SubscriptionNode {
	p := parseClashProxyBlock(blockStr)
	name := p.get("name")
	proxyType := strings.ToLower(p.get("type"))
	server := p.get("server")
	portStr := p.get("port")

	node := parseRemark(name)
	node.Tag = name
	node.Protocol = proxyType
	if server != "" && portStr != "" {
		node.Server = server + ":" + portStr
	}

	node.Transport = "tcp"
	if nw := strings.ToLower(p.get("network")); nw != "" {
		node.Transport = nw
	} else if ws := p.get("ws-opts.path"); ws != "" {
		node.Transport = "ws"
	}

	node.Security = "none"
	if tls := strings.ToLower(p.get("tls")); tls == "true" {
		node.Security = "tls"
	}
	if p.get("reality-opts.public-key") != "" {
		node.Security = "reality"
	}

	return node
}

var providerIDRe = regexp.MustCompile(`^(\s*)['"]?([^'"#:\r\n]+?)['"]?:\s*(?:#.*)?$`)

type providerBlock struct {
	ID        string
	StartLine int
	EndLine   int
}

func extractProviderBlocks(lines []string, sectionStart, sectionEnd, baseIndent int) []providerBlock {
	var blocks []providerBlock
	var current *providerBlock

	for i := sectionStart + 1; i < sectionEnd; i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)

		if indent == baseIndent {
			m := providerIDRe.FindStringSubmatch(line)
			if len(m) >= 3 {
				if current != nil {
					current.EndLine = i
					blocks = append(blocks, *current)
				}
				id := strings.TrimSpace(m[2])
				current = &providerBlock{ID: id, StartLine: i}
				continue
			}
		}
	}
	if current != nil {
		current.EndLine = sectionEnd
		blocks = append(blocks, *current)
	}
	return blocks
}

// ReplaceMihomoProxyProvider добавляет или обновляет блок провайдера в секции proxy-providers:.
// Если block пустой, провайдер удаляется.
func ReplaceMihomoProxyProvider(content string, providerID string, block string) string {
	lines := strings.Split(content, "\n")
	start, end, indent := findTopLevelSection(lines, "proxy-providers")

	if start == -1 {
		if block == "" {
			return content
		}
		appended := "\nproxy-providers:\n" + block
		if strings.HasSuffix(content, "\n") {
			return content + strings.TrimPrefix(appended, "\n")
		}
		return content + appended
	}

	for i := start + 1; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		raw := strings.TrimLeft(line, " \t")
		ind := len(line) - len(raw)
		if ind > 0 {
			indent = ind
			break
		}
	}

	blocks := extractProviderBlocks(lines, start, end, indent)

	var out []string
	out = append(out, lines[:start+1]...)

	replaced := false
	for _, b := range blocks {
		if b.ID == providerID {
			if block != "" {
				blockLines := strings.Split(strings.TrimRight(block, "\n"), "\n")
				out = append(out, blockLines...)
			}
			replaced = true
			continue
		}
		blockEnd := trimTrailingEmpty(lines, b.StartLine, b.EndLine)
		out = append(out, lines[b.StartLine:blockEnd]...)
	}

	if !replaced && block != "" {
		blockLines := strings.Split(strings.TrimRight(block, "\n"), "\n")
		out = append(out, blockLines...)
	}

	if end < len(lines) {
		out = append(out, lines[end:]...)
	}

	return strings.Join(out, "\n")
}

// UpdateMihomoGroupProviders добавляет или удаляет providerID из секции use: указанной группы прокси.
func UpdateMihomoGroupProviders(content, groupName string, providerID string, remove bool) string {
	lines := strings.Split(content, "\n")
	gStart, gEnd, gIndent := findGroupBlock(lines, groupName)
	if gStart == -1 {
		return content
	}

	subIndent := gIndent + 2
	subStart, subEnd := -1, -1
	for i := gStart + 1; i < gEnd; i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		ind := len(line) - len(trimmed)
		if ind == subIndent && strings.HasPrefix(trimmed, "use:") {
			subStart = i
			subEnd = gEnd
			for j := i + 1; j < gEnd; j++ {
				l := lines[j]
				t := strings.TrimLeft(l, " \t")
				if strings.TrimSpace(l) == "" {
					continue
				}
				if len(l)-len(t) <= subIndent {
					subEnd = j
					break
				}
			}
			break
		}
	}

	existing := []string{}
	if subStart != -1 {
		itemIndent := subIndent + 2
		for i := subStart + 1; i < subEnd; i++ {
			l := lines[i]
			t := strings.TrimLeft(l, " \t")
			if len(l)-len(t) >= itemIndent && strings.HasPrefix(t, "- ") {
				name := strings.TrimSpace(strings.TrimPrefix(t, "- "))
				name = strings.Trim(name, `"'`)
				existing = append(existing, name)
			}
		}
	}

	filtered := existing[:0]
	for _, n := range existing {
		if n != providerID {
			filtered = append(filtered, n)
		}
	}

	if !remove {
		found := false
		for _, n := range filtered {
			if n == providerID {
				found = true
				break
			}
		}
		if !found {
			filtered = append(filtered, providerID)
		}
	}

	if len(filtered) == 0 {
		var out []string
		if subStart != -1 {
			out = append(out, lines[:subStart]...)
			out = append(out, lines[subEnd:]...)
			return strings.Join(out, "\n")
		}
		return content
	}

	subPad := strings.Repeat(" ", subIndent)
	itemPad := strings.Repeat(" ", subIndent+2)
	newSubLines := []string{subPad + "use:"}
	for _, n := range filtered {
		newSubLines = append(newSubLines, fmt.Sprintf("%s- %s", itemPad, yamlSafeScalar(n)))
	}

	var out []string
	if subStart != -1 {
		out = append(out, lines[:subStart]...)
		out = append(out, newSubLines...)
		out = append(out, lines[subEnd:]...)
	} else {
		out = append(out, lines[:gStart+1]...)
		out = append(out, newSubLines...)
		out = append(out, lines[gStart+1:]...)
	}

	return strings.Join(out, "\n")
}
