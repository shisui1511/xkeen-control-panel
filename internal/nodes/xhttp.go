package nodes

import (
	"sort"
	"strings"
)

// Соответствие полей xhttp: Xray xhttpSettings (camelCase) ↔ Mihomo xhttp-opts
// (kebab-case). Источники: Mihomo adapter/outbound/vless.go (XHTTPOptions),
// Xray infra/conf/transport_method.go (SplitHTTPConfig).
type xhttpField struct{ xray, mihomo string }

var xhttpFields = []xhttpField{
	{"noGRPCHeader", "no-grpc-header"},
	{"xPaddingBytes", "x-padding-bytes"},
	{"xPaddingObfsMode", "x-padding-obfs-mode"},
	{"xPaddingKey", "x-padding-key"},
	{"xPaddingHeader", "x-padding-header"},
	{"xPaddingPlacement", "x-padding-placement"},
	{"xPaddingMethod", "x-padding-method"},
	{"uplinkHTTPMethod", "uplink-http-method"},
	{"uplinkChunkSize", "uplink-chunk-size"},
	{"uplinkDataPlacement", "uplink-data-placement"},
	{"uplinkDataKey", "uplink-data-key"},
	{"sessionIDPlacement", "session-placement"},
	{"sessionIDKey", "session-key"},
	{"sessionIDTable", "session-table"},
	{"sessionIDLength", "session-length"},
	{"seqPlacement", "seq-placement"},
	{"seqKey", "seq-key"},
	{"scMaxEachPostBytes", "sc-max-each-post-bytes"},
	{"scMinPostsIntervalMs", "sc-min-posts-interval-ms"},
	{"scMaxBufferedPosts", "sc-max-buffered-posts"},
	{"scStreamUpServerSecs", "sc-stream-up-server-secs"},
}

// xmuxFields — поля xmux (Xray) ↔ reuse-settings (Mihomo).
var xmuxFields = []xhttpField{
	{"maxConcurrency", "max-concurrency"},
	{"maxConnections", "max-connections"},
	{"cMaxReuseTimes", "c-max-reuse-times"},
	{"hMaxRequestTimes", "h-max-request-times"},
	{"hMaxReusableSecs", "h-max-reusable-secs"},
	{"hKeepAlivePeriod", "h-keep-alive-period"},
}

// xhttpModes — режимы, которые принимает Xray.
var xhttpModes = map[string]bool{"auto": true, "packet-up": true, "stream-up": true, "stream-one": true}

// xhttpLookup — нормализованное имя (без «-» и «_», в нижнем регистре) → поле.
func xhttpLookup(fields []xhttpField) map[string]xhttpField {
	m := make(map[string]xhttpField, len(fields)*2)
	for _, f := range fields {
		m[normKey(f.xray)] = f
		m[normKey(f.mihomo)] = f
	}
	return m
}

var (
	xhttpByName = xhttpLookup(xhttpFields)
	xmuxByName  = xhttpLookup(xmuxFields)
)

func normKey(k string) string {
	return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(k))
}

// xhttpSettings строит поля xhttp в нужной нотации: kebab == false — Xray
// (xhttpSettings), true — Mihomo (xhttp-opts). Ключи вне таблицы и
// download-settings — ошибки с кодом причины.
func xhttpSettings(t Transport, kebab bool) (map[string]any, error) {
	out := map[string]any{}
	if t.Path != "" {
		out["path"] = t.Path
	}
	host := t.Host

	headers := map[string]any{}
	for k, v := range t.Headers {
		headers[k] = v
	}
	mode := t.Mode

	keys := make([]string, 0, len(t.XHTTP))
	for k := range t.XHTTP {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := t.XHTTP[k]
		switch nk := normKey(k); nk {
		case "downloadsettings":
			return nil, NewError(CodeXHTTPDownloadSettings)
		case "path":
			if s, ok := v.(string); ok && out["path"] == nil && s != "" {
				out["path"] = s
			}
		case "host":
			if s, ok := v.(string); ok && host == "" {
				host = s
			}
		case "mode":
			if s, ok := v.(string); ok && mode == "" {
				mode = s
			}
		case "headers":
			if m, ok := v.(map[string]any); ok {
				for hk, hv := range m {
					if _, dup := headers[hk]; !dup {
						headers[hk] = hv
					}
				}
			}
		case "xmux", "reusesettings":
			x, err := xmuxSettings(v, kebab)
			if err != nil {
				return nil, err
			}
			name := "xmux"
			if kebab {
				name = "reuse-settings"
			}
			out[name] = x
		default:
			f, ok := xhttpByName[nk]
			if !ok {
				return nil, NewError(CodeXHTTPUnknownField, "field", k)
			}
			out[pickName(f, kebab)] = v
		}
	}

	if mode != "" {
		if !xhttpModes[mode] {
			return nil, NewError(CodeXHTTPBadMode, "mode", mode)
		}
		out["mode"] = mode
	}
	// Xray отклоняет headers с ключом host: Host переносится в поле host.
	for k, v := range headers {
		if strings.EqualFold(k, "host") {
			if s, ok := v.(string); ok && host == "" {
				host = s
			}
			delete(headers, k)
		}
	}
	if host != "" {
		out["host"] = host
	}
	if len(headers) > 0 {
		out["headers"] = headers
	}
	return out, nil
}

func xmuxSettings(v any, kebab bool) (map[string]any, error) {
	in, ok := v.(map[string]any)
	if !ok {
		return nil, NewError(CodeXHTTPUnknownField, "field", "xmux")
	}
	out := make(map[string]any, len(in))
	for k, e := range in {
		f, ok := xmuxByName[normKey(k)]
		if !ok {
			return nil, NewError(CodeXHTTPUnknownField, "field", "xmux."+k)
		}
		out[pickName(f, kebab)] = e
	}
	return out, nil
}

func pickName(f xhttpField, kebab bool) string {
	if kebab {
		return f.mihomo
	}
	return f.xray
}
