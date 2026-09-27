package services

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ErrProviderRejectedDevice сообщает, что провайдер вернул только узлы-заглушки:
// он отклонил устройство, а рабочих узлов в ответе нет.
var ErrProviderRejectedDevice = errors.New("provider rejected device: all nodes are stubs")

// Признаки узла-заглушки провайдера. Провайдер, отклонивший устройство
// (например, из-за лимита HWID), вместо рабочих узлов отдаёт фиктивные:
// адрес 0.0.0.0, порт 1 и нулевой UUID. Имя узла не анализируется.
const (
	zeroUUID = "00000000-0000-0000-0000-000000000000"

	stubReasonAddress = "address"
	stubReasonPort    = "port"
	stubReasonUUID    = "uuid"
)

// isStubHost сообщает, что адрес заведомо нерабочий: пустой, unspecified или loopback IPv4.
func isStubHost(host string) bool {
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "127.0.0.1":
		return true
	}
	return false
}

// firstMapItem возвращает первый элемент списка объектов из settings[key].
func firstMapItem(settings map[string]interface{}, key string) map[string]interface{} {
	if settings == nil {
		return nil
	}
	raw, ok := settings[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		if len(v) > 0 {
			m, _ := v[0].(map[string]interface{})
			return m
		}
	case []map[string]interface{}:
		if len(v) > 0 {
			return v[0]
		}
	}
	return nil
}

// portFromValue разбирает порт из float64, int или строки.
func portFromValue(raw interface{}) (int, bool) {
	switch p := raw.(type) {
	case float64:
		return int(p), true
	case int:
		return p, true
	case int64:
		return int(p), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// endpointFromMap читает address/port из объекта настроек.
func endpointFromMap(m map[string]interface{}) (host string, port int, hasHost bool, hasPort bool) {
	if m == nil {
		return "", 0, false, false
	}
	if raw, ok := m["address"]; ok {
		if s, isStr := raw.(string); isStr {
			host, hasHost = s, true
		}
	}
	if raw, ok := m["port"]; ok {
		if p, okPort := portFromValue(raw); okPort {
			port, hasPort = p, true
		}
	}
	return host, port, hasHost, hasPort
}

// outboundEndpoint читает адрес и порт из известных мест настроек outbound:
// vnext[0], servers[0], плоские settings.address/port и peers[0].endpoint.
// hasHost/hasPort равны true, только если ключ реально присутствует.
func outboundEndpoint(ob *Outbound) (host string, port int, hasHost bool, hasPort bool) {
	if ob == nil || ob.Settings == nil {
		return "", 0, false, false
	}
	for _, key := range []string{"vnext", "servers"} {
		if m := firstMapItem(ob.Settings, key); m != nil {
			if h, p, hh, hp := endpointFromMap(m); hh || hp {
				return h, p, hh, hp
			}
		}
	}
	if h, p, hh, hp := endpointFromMap(ob.Settings); hh || hp {
		return h, p, hh, hp
	}
	if peer := firstMapItem(ob.Settings, "peers"); peer != nil {
		if ep, ok := peer["endpoint"].(string); ok && ep != "" {
			if h, pStr, err := net.SplitHostPort(ep); err == nil {
				p, okPort := portFromValue(pStr)
				return h, p, true, okPort
			}
			return strings.Trim(ep, "[]"), 0, true, false
		}
	}
	return "", 0, false, false
}

// outboundCredentials собирает идентификаторы доступа outbound, в которых
// провайдер мог оставить нулевой UUID.
func outboundCredentials(ob *Outbound) []string {
	if ob == nil || ob.Settings == nil {
		return nil
	}
	var creds []string
	add := func(v interface{}) {
		if s, ok := v.(string); ok && s != "" {
			creds = append(creds, s)
		}
	}
	if vn := firstMapItem(ob.Settings, "vnext"); vn != nil {
		if user := firstMapItem(vn, "users"); user != nil {
			add(user["id"])
		}
	}
	add(ob.Settings["id"])
	if srv := firstMapItem(ob.Settings, "servers"); srv != nil {
		add(srv["uuid"])
		add(srv["password"])
	}
	return creds
}

// outboundStubReason возвращает причину, по которой outbound является
// заглушкой провайдера, либо "" для рабочего узла. Outbound без
// распознаваемого адреса заглушкой не считается: ложных срабатываний нет.
func outboundStubReason(ob *Outbound) string {
	if ob == nil {
		return ""
	}
	host, port, hasHost, hasPort := outboundEndpoint(ob)
	if hasHost && isStubHost(host) {
		return stubReasonAddress
	}
	if hasPort && port <= 1 {
		return stubReasonPort
	}
	for _, cred := range outboundCredentials(ob) {
		if strings.EqualFold(strings.TrimSpace(cred), zeroUUID) {
			return stubReasonUUID
		}
	}
	return ""
}

// countXrayWorkingAndStubs считает рабочие узлы (поддерживаемый Xray протокол,
// не заглушка) и заглушки. Outbounds не мутируются.
func countXrayWorkingAndStubs(outbounds []Outbound) (working int, stubs int) {
	for i := range outbounds {
		if outboundStubReason(&outbounds[i]) != "" {
			stubs++
			continue
		}
		if allowedXrayProtocols[outbounds[i].Protocol] {
			working++
		}
	}
	return working, stubs
}

// isStubClashNode сообщает, что разобранный блок прокси Clash — заглушка
// провайдера. Блок без распознанного адреса и без нулевого UUID заглушкой не
// считается: неразобранное не отбрасывается.
func isStubClashNode(node SubscriptionNode) bool {
	for _, cred := range []string{node.UUID, node.Password} {
		if strings.EqualFold(strings.TrimSpace(cred), zeroUUID) {
			return true
		}
	}
	if node.Server == "" {
		return false
	}
	host, portStr, err := net.SplitHostPort(node.Server)
	if err != nil {
		idx := strings.LastIndex(node.Server, ":")
		if idx < 0 {
			return isStubHost(node.Server)
		}
		host, portStr = node.Server[:idx], node.Server[idx+1:]
	}
	host = strings.Trim(host, "[]")
	if isStubHost(host) {
		return true
	}
	if port, ok := portFromValue(portStr); ok && port <= 1 {
		return true
	}
	return false
}

// isStubShareLink сообщает, что строка share-ссылки — заглушка провайдера.
// Строки, которые не удалось разобрать, заглушками не считаются.
func isStubShareLink(line string) bool {
	if ob := parseShareLink(line); ob != nil {
		return outboundStubReason(ob) != ""
	}
	// Запасной путь: parseShareLink отвергает часть заглушек (например, порт 0).
	u, err := url.Parse(line)
	if err != nil || u.Host == "" {
		return false
	}
	if isStubHost(u.Hostname()) {
		return true
	}
	if u.User != nil && strings.EqualFold(u.User.Username(), zeroUUID) {
		return true
	}
	return false
}

// filterStubsFromProviderPayload убирает узлы-заглушки из payload, который
// панель отдаёт Mihomo. Возвращает отфильтрованный payload, число оставшихся
// узлов и число отброшенных заглушек. Если заглушек нет, payload возвращается
// побайтно неизменным.
func filterStubsFromProviderPayload(payload []byte, format string) (filtered []byte, kept int, stubs int) {
	switch format {
	case providerFormatXrayJSON, providerFormatYAMLFull, providerFormatYAMLProxies:
		return filterStubsFromClashYAML(payload)
	case providerFormatRaw:
		return filterStubsFromShareLinks(payload)
	}
	return payload, countProviderNodes(string(payload)), 0
}

// filterStubsFromClashYAML вырезает из секции proxies блоки-заглушки прямо в
// исходном тексте: остальные строки (включая блоки, которые не удалось
// разобрать, например flow-стиль) остаются побайтно как были.
func filterStubsFromClashYAML(payload []byte) ([]byte, int, int) {
	lines := strings.Split(string(payload), "\n")
	start, end, indent := findTopLevelSection(lines, "proxies")
	if start == -1 {
		return payload, countProviderNodes(string(payload)), 0
	}

	blocks := extractProxyBlocks(lines, start, end, indent)
	drop := make([]bool, len(lines))
	kept, stubs := 0, 0
	for _, b := range blocks {
		blockEnd := trimTrailingEmpty(lines, b.StartLine, b.EndLine)
		if isStubClashNode(ParseClashProxyNode(reindentProxyBlock(lines[b.StartLine:blockEnd], indent))) {
			stubs++
			for i := b.StartLine; i < b.EndLine; i++ {
				drop[i] = true
			}
			continue
		}
		kept++
	}
	if stubs == 0 {
		return payload, kept, 0
	}
	if kept == 0 {
		return []byte("proxies: []\n"), 0, stubs
	}

	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if !drop[i] {
			out = append(out, line)
		}
	}
	return []byte(strings.Join(out, "\n")), kept, stubs
}

// reindentProxyBlock приводит блок прокси к отступу 2, которого ждёт ParseClashProxyNode.
func reindentProxyBlock(blockLines []string, baseIndent int) string {
	if baseIndent == 2 {
		return strings.Join(blockLines, "\n")
	}
	out := make([]string, len(blockLines))
	for i, l := range blockLines {
		t := strings.TrimLeft(l, " \t")
		ni := 2 + (len(l) - len(t) - baseIndent)
		if ni < 0 {
			ni = 0
		}
		out[i] = strings.Repeat(" ", ni) + t
	}
	return strings.Join(out, "\n")
}

func filterStubsFromShareLinks(payload []byte) ([]byte, int, int) {
	text := string(payload)
	wasBase64 := false
	if !providerURISchemeRe.MatchString(text) {
		if decoded := tryDecodeBase64Text(strings.TrimSpace(text)); providerURISchemeRe.MatchString(decoded) {
			text, wasBase64 = decoded, true
		}
	}

	lines := strings.Split(text, "\n")
	keptLines := make([]string, 0, len(lines))
	kept, stubs := 0, 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && providerURISchemeRe.MatchString(trimmed) {
			if isStubShareLink(trimmed) {
				stubs++
				continue
			}
			kept++
		}
		keptLines = append(keptLines, line)
	}
	if stubs == 0 {
		return payload, kept, 0
	}
	out := strings.Join(keptLines, "\n")
	if wasBase64 {
		out = base64.StdEncoding.EncodeToString([]byte(out))
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out), kept, stubs
}
