package services

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// ErrRuleProviderNotFound is returned for a name absent from the config.
var ErrRuleProviderNotFound = errors.New("rule provider not found")

// maxRuleProviderPage bounds one page of rule-provider content.
const maxRuleProviderPage = 500

// RuleProviderInfo describes a rule-provider as declared in the Mihomo
// config together with the state of its local file.
type RuleProviderInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Behavior string `json:"behavior"`
	Format   string `json:"format"`
	// URL is shown without query string and credentials: rule-set links are
	// usually public, but the query may carry a token.
	URL        string `json:"url,omitempty"`
	Path       string `json:"path,omitempty"`
	FileExists bool   `json:"file_exists"`
	FileSize   int64  `json:"file_size,omitempty"`
	FileMtime  int64  `json:"file_mtime,omitempty"`
	Inline     int    `json:"inline_count,omitempty"`
}

// RuleProviderPage is a filtered page of rule-provider entries.
type RuleProviderPage struct {
	Name    string   `json:"name"`
	Total   int      `json:"total"`
	Matched int      `json:"matched"`
	Offset  int      `json:"offset"`
	Entries []string `json:"entries"`
}

// RuleProviderURLCheck is the result of probing a provider's download URL.
type RuleProviderURLCheck struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	OK         bool   `json:"ok"`
	DurationMs int64  `json:"duration_ms"`
}

// ListRuleProviders returns the rule-providers declared in the Mihomo config
// with the state of their local files.
func (s *RouteTracerService) ListRuleProviders() ([]RuleProviderInfo, error) {
	store := s.ruleSetStore()
	if store == nil {
		return []RuleProviderInfo{}, nil
	}
	list := make([]RuleProviderInfo, 0, len(store.providers))
	for name, def := range store.providers {
		info := RuleProviderInfo{
			Name:     name,
			Type:     strings.ToLower(def.Type),
			Behavior: strings.ToLower(def.Behavior),
			Format:   strings.ToLower(def.Format),
			URL:      redactProviderURL(def.URL),
		}
		if info.Type == "inline" {
			info.Inline = len(def.Payload)
			info.FileExists = true
		} else if path := store.providerPath(def); path != "" {
			if rel, err := filepath.Rel(store.homeDir, path); err == nil {
				info.Path = rel
			}
			if st, err := os.Stat(path); err == nil {
				info.FileExists = true
				info.FileSize = st.Size()
				info.FileMtime = st.ModTime().Unix()
			}
		}
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

// RuleProviderContent returns a page of entries of one provider, optionally
// filtered by a case-insensitive substring.
func (s *RouteTracerService) RuleProviderContent(ctx context.Context, name, query string, offset, limit int) (*RuleProviderPage, error) {
	store := s.ruleSetStore()
	if store == nil {
		return nil, ErrRuleProviderNotFound
	}
	def, ok := store.providers[name]
	if !ok {
		return nil, ErrRuleProviderNotFound
	}
	if limit <= 0 || limit > maxRuleProviderPage {
		limit = maxRuleProviderPage
	}
	if offset < 0 {
		offset = 0
	}
	entries, err := store.entries(ctx, name, def)
	if err != nil {
		return nil, err
	}
	defer entries.Close()

	q := strings.ToLower(strings.TrimSpace(query))
	page := &RuleProviderPage{Name: name, Offset: offset, Entries: []string{}}
	scanner := bufio.NewScanner(entries)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		page.Total++
		if q != "" && !strings.Contains(strings.ToLower(line), q) {
			continue
		}
		if page.Matched >= offset && len(page.Entries) < limit {
			page.Entries = append(page.Entries, line)
		}
		page.Matched++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return page, nil
}

// CheckRuleProviderURL probes the download URL of an HTTP provider so the
// user can tell a dead link (e.g. 404) from a network problem.
func (s *RouteTracerService) CheckRuleProviderURL(ctx context.Context, name string) (*RuleProviderURLCheck, error) {
	store := s.ruleSetStore()
	if store == nil {
		return nil, ErrRuleProviderNotFound
	}
	def, ok := store.providers[name]
	if !ok {
		return nil, ErrRuleProviderNotFound
	}
	res := &RuleProviderURLCheck{Name: name, URL: redactProviderURL(def.URL)}
	if def.URL == "" {
		res.Error = "provider has no url"
		return res, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, def.URL, nil)
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	// Only the status matters; ask for a single byte.
	req.Header.Set("Range", "bytes=0-0")
	start := time.Now()
	resp, err := utils.SafeHTTPClient(15 * time.Second).Do(req)
	res.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	resp.Body.Close()
	res.StatusCode = resp.StatusCode
	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	return res, nil
}

func redactProviderURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
