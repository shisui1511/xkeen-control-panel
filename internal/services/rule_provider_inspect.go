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
	"strconv"
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
	URL string `json:"url,omitempty"`
	// Proxy is the proxy or group the provider downloads through, as declared
	// in the profile (a node name, no secrets).
	Proxy      string `json:"proxy,omitempty"`
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

// RuleProviderProbeTimeout bounds one probe through the core: below the
// 32767 ms limit of Mihomo's delay API.
const RuleProviderProbeTimeout = 10 * time.Second

// ruleProviderDirectTimeout bounds the direct probe from the panel.
const ruleProviderDirectTimeout = 15 * time.Second

// RuleProviderProbe is the outcome of one probe of a provider URL. Through the
// core only the delay is known (the core does not report the HTTP code);
// the direct probe reports the code.
type RuleProviderProbe struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	DelayMs    int    `json:"delay_ms,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ProxyProbe checks rawURL through the named proxy or group of the running
// core (GET /proxies/{name}/delay) and returns the delay in milliseconds.
type ProxyProbe func(ctx context.Context, proxy, rawURL string) (int, error)

// RuleProviderURLCheck is the result of probing a provider's download URL.
// OK is true when at least one probe passed; StatusCode and Error belong to
// the direct probe. DirectOnly marks a provider that declares a proxy but was
// probed only directly because the core API is unavailable.
type RuleProviderURLCheck struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Proxy      string `json:"proxy,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	OK         bool   `json:"ok"`
	DurationMs int64  `json:"duration_ms"`

	ViaProxy   *RuleProviderProbe `json:"via_proxy,omitempty"`
	Direct     *RuleProviderProbe `json:"direct,omitempty"`
	DirectOnly bool               `json:"direct_only,omitempty"`
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
			Proxy:    def.Proxy,
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
// user can tell a dead link (e.g. 404) from a broken node. With a proxy
// declared and via != nil the probe goes through the core first and, if that
// fails, directly as well (both results are returned). via == nil means the
// core API is unavailable: only the direct probe runs and DirectOnly is set.
// An empty or DIRECT proxy is probed directly without that flag.
func (s *RouteTracerService) CheckRuleProviderURL(ctx context.Context, name string, via ProxyProbe) (*RuleProviderURLCheck, error) {
	store := s.ruleSetStore()
	if store == nil {
		return nil, ErrRuleProviderNotFound
	}
	def, ok := store.providers[name]
	if !ok {
		return nil, ErrRuleProviderNotFound
	}
	res := &RuleProviderURLCheck{Name: name, URL: redactProviderURL(def.URL), Proxy: def.Proxy}
	if def.URL == "" {
		res.Error = "provider has no url"
		return res, nil
	}

	start := time.Now()
	proxy := strings.TrimSpace(def.Proxy)
	switch {
	case proxy == "" || strings.EqualFold(proxy, "DIRECT"):
		res.Direct = directProviderProbe(ctx, def.URL)
	case via == nil:
		res.DirectOnly = true
		res.Direct = directProviderProbe(ctx, def.URL)
	default:
		delay, err := via(ctx, proxy, def.URL)
		if err == nil {
			res.ViaProxy = &RuleProviderProbe{OK: true, DelayMs: delay}
		} else {
			res.ViaProxy = &RuleProviderProbe{Error: proxyProbeErrorText(err)}
			res.Direct = directProviderProbe(ctx, def.URL)
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	res.OK = (res.ViaProxy != nil && res.ViaProxy.OK) || (res.Direct != nil && res.Direct.OK)
	if res.Direct != nil {
		res.StatusCode = res.Direct.StatusCode
		res.Error = res.Direct.Error
	}
	return res, nil
}

// directProviderProbe asks the URL for a single byte straight from the panel
// (only the status matters). The error text never contains the URL.
func directProviderProbe(ctx context.Context, rawURL string) *RuleProviderProbe {
	probe := &RuleProviderProbe{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		probe.Error = probeErrorText(err)
		return probe
	}
	req.Header.Set("Range", "bytes=0-0")
	start := time.Now()
	resp, err := utils.SafeHTTPClient(ruleProviderDirectTimeout).Do(req)
	probe.DelayMs = int(time.Since(start).Milliseconds())
	if err != nil {
		probe.Error = probeErrorText(err)
		return probe
	}
	resp.Body.Close()
	probe.StatusCode = resp.StatusCode
	probe.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	return probe
}

// probeErrorText unwraps *url.Error: its text carries the full URL, including
// the query that may hold a token; the inner error does not.
func probeErrorText(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}

// proxyProbeErrorText turns a failure of the probe through the core into a
// text without URLs, by the status of the core's answer.
func proxyProbeErrorText(err error) string {
	var st *ClashStatusError
	if errors.As(err, &st) {
		switch st.Status {
		case http.StatusNotFound:
			return "прокси нет в запущенном ядре"
		case http.StatusServiceUnavailable:
			return "ядро: 503, проверка через прокси не прошла"
		case http.StatusGatewayTimeout:
			return "ядро: 504, прокси не ответил за отведённое время"
		}
		return "ядро ответило кодом " + strconv.Itoa(st.Status)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ядро не ответило за отведённое время"
	}
	return probeErrorText(err)
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
