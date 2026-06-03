package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
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

// OutboundImportRequest represents the payload for importing a single outbound node.
type OutboundImportRequest struct {
	Link string `json:"link"`
	Tag  string `json:"tag,omitempty"`
}

// OutboundImport handles POST /api/outbound/import.
// Parses a single share link, updates or adds it to 04_outbounds.manual.json,
// and restarts Xray service if it is running.
func (a *API) OutboundImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxConfigBytes)
	var req OutboundImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "http: request body too large" {
			JSONError(w, http.StatusRequestEntityTooLarge, "request body too large (max 1 MB)")
			return
		}
		JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	link := strings.TrimSpace(req.Link)
	if link == "" {
		a.errorResponse(w, a.t(r, "subscr.import_error_empty"), http.StatusBadRequest)
		return
	}

	if len(link) > 16384 {
		a.errorResponse(w, a.t(r, "subscr.import_error_too_long"), http.StatusBadRequest)
		return
	}

	if strings.HasPrefix(strings.ToLower(link), "vmess://") && len(link) > 8192 {
		a.errorResponse(w, a.t(r, "subscr.import_error_too_long"), http.StatusBadRequest)
		return
	}

	results := a.subscriptionSvc.ParseLinks([]string{link})
	if len(results) == 0 || results[0].Error != "" || results[0].Outbound == nil {
		a.errorResponse(w, a.t(r, "subscr.import_error_invalid"), http.StatusBadRequest)
		return
	}

	ob := results[0].Outbound
	if req.Tag != "" {
		ob.Tag = req.Tag
	}

	manualPath := filepath.Join(a.cfg.XRayConfigDir, "04_outbounds.manual.json")

	var wrapper struct {
		Outbounds []services.Outbound `json:"outbounds"`
	}

	if a.configSvc.Exists(manualPath) {
		data, err := a.configSvc.Read(manualPath)
		if err != nil {
			a.errorResponse(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			if len(strings.TrimSpace(string(data))) > 0 {
				a.errorResponse(w, "Failed to parse manual outbounds file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			wrapper.Outbounds = []services.Outbound{}
		}
	} else {
		wrapper.Outbounds = []services.Outbound{}
	}

	// Update existing by tag or append
	found := false
	for i, existing := range wrapper.Outbounds {
		if existing.Tag == ob.Tag {
			wrapper.Outbounds[i] = *ob
			found = true
			break
		}
	}
	if !found {
		wrapper.Outbounds = append(wrapper.Outbounds, *ob)
	}

	jsonData, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := a.configSvc.Save(manualPath, jsonData); err != nil {
		a.errorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// D-03: Restart xray if running
	if a.kernelSvc != nil && a.consoleSvc != nil {
		if k := a.kernelSvc.Get("xray"); k != nil && k.ProcessStatus == "running" {
			if _, err := a.consoleSvc.Execute("-restart"); err != nil {
				log.Printf("OutboundImport: xkeen -restart after outbound import: %v", err)
			}
		}
	}

	JSONSuccess(w, nil)
}

