package nodes

import (
	"sort"
	"strings"
)

// Коды ошибок ввода и ответа провайдера (D-32). Список совпадает с
// frontend/src/lib/sourceErrorCodes.json.
const (
	CodeHTMLResponse           = "html_response"
	CodeHTTPAuth               = "http_auth"
	CodeAllStubs               = "all_stubs"
	CodeEmptyResponse          = "empty_response"
	CodeExpired                = "expired"
	CodeDNSFailed              = "dns_failed"
	CodeTimeout                = "timeout"
	CodeTLSError               = "tls_error"
	CodeSSRFBlocked            = "ssrf_blocked"
	CodeUnreachableDirect      = "unreachable_direct"
	CodeServerError            = "server_error"
	CodeUnknownScheme          = "unknown_scheme"
	CodeBadBase64              = "bad_base64"
	CodeYAMLError              = "yaml_error"
	CodeJSONError              = "json_error"
	CodeFullKernelConfig       = "full_kernel_config"
	CodeWGMissingKey           = "wg_missing_key"
	CodeInvalidURL             = "invalid_url"
	CodeTooLarge               = "too_large"
	CodeTooManyURLs            = "too_many_urls"
	CodeUnrecognized           = "unrecognized"
	CodeEditSingleNode         = "edit_single_node"
	CodeTUICXray               = "tuic_xray"
	CodeNeedsMihomo            = "needs_mihomo"
	CodeNeedsXray              = "needs_xray"
	CodeAWGXray                = "awg_xray"
	CodeHysteria2Xray          = "hysteria2_xray"
	CodeSSPluginXray           = "ss_plugin_xray"
	CodeXrayNoHTTPTransport    = "xray_no_http_transport"
	CodeXrayNoInsecure         = "xray_no_insecure"
	CodeXrayForbiddenKey       = "xray_forbidden_key"
	CodeXHTTPUnknownField      = "xhttp_unknown_field"
	CodeXHTTPDownloadSettings  = "xhttp_download_settings"
	CodeXHTTPBadMode           = "xhttp_bad_mode"
	CodeUnsupportedType        = "unsupported_type"
	CodeKCPMihomo              = "kcp_mihomo"
	CodeStub                   = "stub"
	CodeNoteUDPDropped         = "note_udp_dropped"
	CodeNoteSpiderXDropped     = "note_spiderx_dropped"
	CodeNotePacketEncDropped   = "note_packet_encoding_dropped"
	CodeNoteChainRemoved       = "note_chain_removed"
	CodeNoteFingerprintDropped = "note_fingerprint_dropped"
)

// SourceError — ошибка ввода или ответа провайдера; уходит клиенту как есть.
type SourceError struct {
	Code       string            `json:"code"`
	Params     map[string]string `json:"params,omitempty"`
	HTTPStatus int               `json:"http_status,omitempty"`
	Line       int               `json:"line,omitempty"`
	Fragment   string            `json:"fragment,omitempty"`
	Detail     string            `json:"detail,omitempty"`
}

// Error возвращает код и параметры. Detail и Fragment в текст не попадают:
// они могут нести данные ввода.
func (e *SourceError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Params) == 0 {
		return e.Code
	}
	keys := make([]string, 0, len(e.Params))
	for k := range e.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(e.Code)
	b.WriteString(" (")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(e.Params[k])
	}
	b.WriteByte(')')
	return b.String()
}

// NewError собирает ошибку с параметрами из пар ключ-значение.
func NewError(code string, kv ...string) *SourceError {
	e := &SourceError{Code: code}
	for i := 0; i+1 < len(kv); i += 2 {
		if e.Params == nil {
			e.Params = make(map[string]string, len(kv)/2)
		}
		e.Params[kv[i]] = kv[i+1]
	}
	return e
}

// AllCodes — отсортированный список всех кодов (ошибки и примечания).
func AllCodes() []string {
	out := []string{
		CodeHTMLResponse, CodeHTTPAuth, CodeAllStubs, CodeEmptyResponse, CodeExpired,
		CodeDNSFailed, CodeTimeout, CodeTLSError, CodeSSRFBlocked, CodeUnreachableDirect,
		CodeServerError, CodeUnknownScheme, CodeBadBase64, CodeYAMLError, CodeJSONError,
		CodeFullKernelConfig, CodeWGMissingKey, CodeInvalidURL, CodeTooLarge, CodeTooManyURLs,
		CodeUnrecognized, CodeEditSingleNode, CodeTUICXray, CodeNeedsMihomo, CodeNeedsXray,
		CodeAWGXray, CodeHysteria2Xray, CodeSSPluginXray, CodeXrayNoHTTPTransport,
		CodeXrayNoInsecure, CodeXrayForbiddenKey, CodeXHTTPUnknownField,
		CodeXHTTPDownloadSettings, CodeXHTTPBadMode, CodeUnsupportedType, CodeKCPMihomo,
		CodeStub, CodeNoteUDPDropped, CodeNoteSpiderXDropped, CodeNotePacketEncDropped,
		CodeNoteChainRemoved, CodeNoteFingerprintDropped,
	}
	sort.Strings(out)
	return out
}
