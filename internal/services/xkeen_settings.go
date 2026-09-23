package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// DefaultXKeenConfigDir is where XKeen keeps its own settings on Entware.
const DefaultXKeenConfigDir = "/opt/etc/xkeen"

// XKeen settings file kinds exposed to the UI.
const (
	XKeenPortProxying = "port_proxying"
	XKeenPortExclude  = "port_exclude"
	XKeenIPExclude    = "ip_exclude"
	XKeenConfigJSON   = "xkeen_json"
)

var xkeenSettingsFiles = map[string]string{
	XKeenPortProxying: "port_proxying.lst",
	XKeenPortExclude:  "port_exclude.lst",
	XKeenIPExclude:    "ip_exclude.lst",
	XKeenConfigJSON:   "xkeen.json",
}

// xkeenSettingsOrder fixes the order in which files are listed in the UI.
var xkeenSettingsOrder = []string{XKeenPortProxying, XKeenPortExclude, XKeenIPExclude, XKeenConfigJSON}

// maxXKeenSettingsSize bounds a single settings file (ip_exclude can hold
// thousands of subnets, but anything beyond this is a mistake).
const maxXKeenSettingsSize = 512 * 1024

// xkeenBackupsKept is how many previous versions of each file are retained.
const xkeenBackupsKept = 5

// ErrUnknownXKeenSetting is returned for an unsupported settings kind.
var ErrUnknownXKeenSetting = errors.New("unknown xkeen settings kind")

// XKeenSettingsIssue describes one problem found during validation.
// Severity is "error" (XKeen would drop or reject it) or "warning".
type XKeenSettingsIssue struct {
	Line     int    `json:"line,omitempty"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Value    string `json:"value,omitempty"`
}

// XKeenSettingsFile is one settings file with its validation summary.
type XKeenSettingsFile struct {
	Kind    string               `json:"kind"`
	Path    string               `json:"path"`
	Exists  bool                 `json:"exists"`
	Content string               `json:"content"`
	Entries int                  `json:"entries"`
	Issues  []XKeenSettingsIssue `json:"issues"`
}

// XKeenSettingsService reads, validates and writes XKeen's own settings:
// proxied/excluded ports, excluded IPs and xkeen.json.
type XKeenSettingsService struct {
	dir       string
	backupDir string
	validator *utils.PathValidator
	now       func() time.Time
}

func NewXKeenSettingsService(dir, dataDir string, allowedRoots []string) *XKeenSettingsService {
	if dir == "" {
		dir = DefaultXKeenConfigDir
	}
	return &XKeenSettingsService{
		dir:       dir,
		backupDir: filepath.Join(dataDir, "backup", "xkeen"),
		validator: utils.NewPathValidator(allowedRoots),
		now:       time.Now,
	}
}

func (s *XKeenSettingsService) pathFor(kind string) (string, error) {
	name, ok := xkeenSettingsFiles[kind]
	if !ok {
		return "", ErrUnknownXKeenSetting
	}
	return s.validator.Validate(filepath.Join(s.dir, name))
}

// List returns every settings file with content and validation issues.
// Cross-file checks (proxied and excluded ports both set) are included.
func (s *XKeenSettingsService) List() ([]XKeenSettingsFile, error) {
	files := make([]XKeenSettingsFile, 0, len(xkeenSettingsOrder))
	for _, kind := range xkeenSettingsOrder {
		f, err := s.Get(kind)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// Get reads one settings file. A missing file is reported with Exists=false.
func (s *XKeenSettingsService) Get(kind string) (XKeenSettingsFile, error) {
	path, err := s.pathFor(kind)
	if err != nil {
		return XKeenSettingsFile{}, err
	}
	f := XKeenSettingsFile{Kind: kind, Path: path}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		f.Exists = true
		f.Content = string(data)
	case errors.Is(err, os.ErrNotExist):
	default:
		return XKeenSettingsFile{}, err
	}
	f.Entries, f.Issues = s.Validate(kind, f.Content)
	return f, nil
}

// Validate checks content for the given kind. It returns the number of
// effective entries and the issues found, including the conflict between
// proxied and excluded ports which XKeen resolves by ignoring exclusions.
func (s *XKeenSettingsService) Validate(kind, content string) (int, []XKeenSettingsIssue) {
	var entries int
	var issues []XKeenSettingsIssue
	switch kind {
	case XKeenPortProxying, XKeenPortExclude:
		entries, issues = ValidateXKeenPorts(content)
		if entries > 0 {
			other := XKeenPortExclude
			code := "ports_exclude_ignored"
			if kind == XKeenPortExclude {
				other = XKeenPortProxying
				code = "ports_exclude_overridden"
			}
			if otherPath, err := s.pathFor(other); err == nil {
				if data, err := os.ReadFile(otherPath); err == nil {
					if n, _ := ValidateXKeenPorts(string(data)); n > 0 {
						issues = append(issues, XKeenSettingsIssue{Severity: "warning", Code: code})
					}
				}
			}
		}
	case XKeenIPExclude:
		entries, issues = ValidateXKeenIPs(content)
	case XKeenConfigJSON:
		entries, issues = ValidateXKeenJSON(content)
	}
	if issues == nil {
		issues = []XKeenSettingsIssue{}
	}
	return entries, issues
}

// Save validates and atomically writes a settings file, keeping a backup of
// the previous version. Content with errors is rejected and nothing is written.
func (s *XKeenSettingsService) Save(kind, content string) (XKeenSettingsFile, error) {
	path, err := s.pathFor(kind)
	if err != nil {
		return XKeenSettingsFile{}, err
	}
	if len(content) > maxXKeenSettingsSize {
		return XKeenSettingsFile{}, fmt.Errorf("content exceeds %d bytes", maxXKeenSettingsSize)
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	entries, issues := s.Validate(kind, content)
	for _, is := range issues {
		if is.Severity == "error" {
			return XKeenSettingsFile{Kind: kind, Path: path, Content: content, Entries: entries, Issues: issues}, ErrXKeenSettingsInvalid
		}
	}

	if prev, err := os.ReadFile(path); err == nil {
		if string(prev) == content {
			return XKeenSettingsFile{Kind: kind, Path: path, Exists: true, Content: content, Entries: entries, Issues: issues}, nil
		}
		s.backup(kind, prev)
	}

	perm := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	if err := utils.AtomicWriteFile(path, []byte(content), perm); err != nil {
		return XKeenSettingsFile{}, err
	}
	return XKeenSettingsFile{Kind: kind, Path: path, Exists: true, Content: content, Entries: entries, Issues: issues}, nil
}

// ErrXKeenSettingsInvalid is returned by Save when validation found errors.
var ErrXKeenSettingsInvalid = errors.New("xkeen settings contain errors")

func (s *XKeenSettingsService) backup(kind string, data []byte) {
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return
	}
	name := xkeenSettingsFiles[kind]
	stamp := s.now().Format("20060102-150405")
	_ = os.WriteFile(filepath.Join(s.backupDir, name+"."+stamp), data, 0o600)

	matches, _ := filepath.Glob(filepath.Join(s.backupDir, name+".*"))
	if len(matches) <= xkeenBackupsKept {
		return
	}
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-xkeenBackupsKept] {
		_ = os.Remove(old)
	}
}

// xkeenListLines yields the meaningful part of each line the way XKeen's
// init script reads them: CR and "#" comments stripped, blanks skipped.
func xkeenListLines(content string, fn func(lineNo int, value string)) {
	for i, raw := range strings.Split(content, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			fn(i+1, line)
		}
	}
}

// ValidateXKeenPorts checks a port list: single ports or ranges written as
// "a:b" or "a-b", separated by newlines or commas. XKeen silently drops
// invalid values, so every one of them is reported as an error.
func ValidateXKeenPorts(content string) (int, []XKeenSettingsIssue) {
	var issues []XKeenSettingsIssue
	entries := 0
	xkeenListLines(content, func(lineNo int, line string) {
		for _, token := range strings.Split(line, ",") {
			token = strings.Join(strings.Fields(token), "")
			if token == "" {
				continue
			}
			if !validPortToken(token) {
				issues = append(issues, XKeenSettingsIssue{Line: lineNo, Severity: "error", Code: "invalid_port", Value: token})
				continue
			}
			entries++
		}
	})
	return entries, issues
}

func validPortToken(token string) bool {
	token = strings.ReplaceAll(token, "-", ":")
	parts := strings.Split(token, ":")
	if len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.TrimLeft(p, "0123456789") != "" {
			return false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

// ValidateXKeenIPs checks the excluded address list: one IPv4/IPv6 address
// or CIDR subnet per line. Local and private networks are flagged because
// they never reach the proxy anyway and only bloat the ipset.
func ValidateXKeenIPs(content string) (int, []XKeenSettingsIssue) {
	var issues []XKeenSettingsIssue
	entries := 0
	xkeenListLines(content, func(lineNo int, line string) {
		prefix, err := parseAddrOrPrefix(line)
		if err != nil {
			issues = append(issues, XKeenSettingsIssue{Line: lineNo, Severity: "error", Code: "invalid_ip", Value: line})
			return
		}
		entries++
		addr := prefix.Addr()
		if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
			issues = append(issues, XKeenSettingsIssue{Line: lineNo, Severity: "warning", Code: "local_ip", Value: line})
		}
		if prefix.Masked() != prefix {
			issues = append(issues, XKeenSettingsIssue{Line: lineNo, Severity: "warning", Code: "host_bits_set", Value: line})
		}
	})
	return entries, issues
}

func parseAddrOrPrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	if addr.Zone() != "" {
		return netip.Prefix{}, errors.New("zoned address")
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// ValidateXKeenJSON checks xkeen.json (JSON with comments) against the
// structure XKeen's init script expects. XKeen refuses to start the proxy on
// syntax errors or a malformed policy list, so both are errors.
func ValidateXKeenJSON(content string) (int, []XKeenSettingsIssue) {
	stripped := strings.TrimSpace(StripJSONComments(content))
	if stripped == "" {
		return 0, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripped), &root); err != nil {
		issue := XKeenSettingsIssue{Severity: "error", Code: "invalid_json", Value: err.Error()}
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			issue.Line = 1 + strings.Count(stripped[:min(int(syntaxErr.Offset), len(stripped))], "\n")
		}
		return 0, []XKeenSettingsIssue{issue}
	}

	var issues []XKeenSettingsIssue
	raw, ok := root["xkeen"]
	if !ok || string(raw) == "null" {
		return len(root), nil
	}
	var xk struct {
		Policy     json.RawMessage `json:"policy"`
		Killswitch *string         `json:"killswitch"`
		Mihomo     *struct {
			GomemlimitPercent *float64 `json:"gomemlimit_percent"`
			GomemlimitMB      *float64 `json:"gomemlimit_mb"`
		} `json:"mihomo"`
	}
	if err := json.Unmarshal(raw, &xk); err != nil {
		return len(root), []XKeenSettingsIssue{{Severity: "error", Code: "invalid_structure", Value: err.Error()}}
	}
	if len(xk.Policy) > 0 && string(xk.Policy) != "null" {
		var policies []map[string]json.RawMessage
		if err := json.Unmarshal(xk.Policy, &policies); err != nil {
			issues = append(issues, XKeenSettingsIssue{Severity: "error", Code: "policy_not_array"})
		} else {
			for i, p := range policies {
				var name string
				if err := json.Unmarshal(p["name"], &name); err != nil || strings.TrimSpace(name) == "" {
					issues = append(issues, XKeenSettingsIssue{Severity: "error", Code: "policy_without_name", Value: strconv.Itoa(i + 1)})
				}
			}
		}
	}
	if xk.Killswitch != nil && *xk.Killswitch != "on" && *xk.Killswitch != "off" {
		issues = append(issues, XKeenSettingsIssue{Severity: "warning", Code: "killswitch_value", Value: *xk.Killswitch})
	}
	if xk.Mihomo != nil {
		if p := xk.Mihomo.GomemlimitPercent; p != nil && (*p < 1 || *p > 90) {
			issues = append(issues, XKeenSettingsIssue{Severity: "warning", Code: "gomemlimit_percent_range", Value: strconv.FormatFloat(*p, 'f', -1, 64)})
		}
		if mb := xk.Mihomo.GomemlimitMB; mb != nil && *mb < 64 {
			issues = append(issues, XKeenSettingsIssue{Severity: "warning", Code: "gomemlimit_mb_range", Value: strconv.FormatFloat(*mb, 'f', -1, 64)})
		}
	}
	return len(root), issues
}

// StripJSONComments removes // and /* */ comments outside of string literals,
// keeping newlines so that line numbers of the remaining JSON are preserved.
func StripJSONComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(s) {
			switch s[i+1] {
			case '/':
				for i < len(s) && s[i] != '\n' {
					i++
				}
				if i < len(s) {
					b.WriteByte('\n')
				}
				continue
			case '*':
				i += 2
				for i < len(s) && !(s[i] == '*' && i+1 < len(s) && s[i+1] == '/') {
					if s[i] == '\n' {
						b.WriteByte('\n')
					}
					i++
				}
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// IsXKeenSettingsKind reports whether kind names a supported settings file.
func IsXKeenSettingsKind(kind string) bool {
	_, ok := xkeenSettingsFiles[kind]
	return ok
}
