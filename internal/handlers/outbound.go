package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
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

	var req OutboundParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, http.StatusBadRequest, err.Error())
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
