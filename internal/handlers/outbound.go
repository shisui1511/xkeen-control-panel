package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// OutboundParseRequest accepts a list of share links or a single multi-line string.
type OutboundParseRequest struct {
	Links []string `json:"links,omitempty"`
	Text  string   `json:"text,omitempty"` // newline-separated links (alternative input)
}

// OutboundParse handles POST /api/outbound/parse.
// Parses share links (vless://, vmess://, trojan://, ss://, hy2://, tuic://, socks://, socks5://)
// and returns structured Xray outbound objects.
func (a *API) OutboundParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(nil, r.Body, maxConfigBytes)
	var req OutboundParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			JSONError(w, http.StatusRequestEntityTooLarge, "request body too large (max 1 MB)")
			return
		}
		a.errorResponse(w, a.t(r, "error.invalid_request"), http.StatusBadRequest)
		return
	}

	// If text is provided and it looks like a wg-quick .conf file, parse it directly.
	if req.Text != "" && services.LooksLikeWgQuickConf(req.Text) {
		results := a.subscriptionSvc.ParseOutboundText(req.Text)
		JSONSuccess(w, results)
		return
	}
	if len(req.Links) == 1 && services.LooksLikeWgQuickConf(req.Links[0]) {
		results := a.subscriptionSvc.ParseOutboundText(req.Links[0])
		JSONSuccess(w, results)
		return
	}

	links := req.Links
	// Support plain text input: split by newlines
	if len(links) == 0 && req.Text != "" {
		for _, line := range strings.Split(req.Text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				links = append(links, line)
			}
		}
	}

	if len(links) == 0 {
		JSONError(w, http.StatusBadRequest, "links or text is required")
		return
	}

	if len(links) > 200 {
		JSONError(w, http.StatusBadRequest, "too many links (max 200 per request)")
		return
	}

	results := a.subscriptionSvc.ParseLinks(links)
	JSONSuccess(w, results)
}
