package services

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// DefaultXrayAccessLog is where XKeen puts the Xray access log.
const DefaultXrayAccessLog = "/opt/var/log/xray/access.log"

// xrayAccessTailBytes bounds how much of the access log is parsed per
// request: recent activity is what matters and the file can grow large.
const xrayAccessTailBytes = 1 << 20

// ErrXrayLogConfigNotFound means no config file in the Xray directory has a
// top-level "log" section.
var ErrXrayLogConfigNotFound = errors.New("xray log section not found")

// XrayAccessLogStatus describes the access log configuration.
type XrayAccessLogStatus struct {
	ConfigFile string `json:"config_file,omitempty"`
	AccessPath string `json:"access_path"`
	Enabled    bool   `json:"enabled"`
	FileSize   int64  `json:"file_size"`
}

// XrayAccessEntry is one parsed access log line.
type XrayAccessEntry struct {
	Time        string `json:"time"`
	SourceIP    string `json:"source_ip"`
	Device      string `json:"device,omitempty"`
	Status      string `json:"status"` // accepted | rejected
	Network     string `json:"network,omitempty"`
	Destination string `json:"destination,omitempty"`
	Port        string `json:"port,omitempty"`
	Inbound     string `json:"inbound,omitempty"`
	Outbound    string `json:"outbound,omitempty"`
	Email       string `json:"email,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// XrayDeviceSummary aggregates access log entries of one source IP.
type XrayDeviceSummary struct {
	IP              string   `json:"ip"`
	Device          string   `json:"device,omitempty"`
	Connections     int      `json:"connections"`
	Rejected        int      `json:"rejected"`
	LastSeen        string   `json:"last_seen"`
	TopDestinations []string `json:"top_destinations"`
}

// XrayAccessFilter narrows the parsed entries.
type XrayAccessFilter struct {
	SourceIP    string
	Destination string
	Outbound    string
	Limit       int
}

// XrayAccessReport is the response for the access log view.
type XrayAccessReport struct {
	Entries []XrayAccessEntry   `json:"entries"`
	Devices []XrayDeviceSummary `json:"devices"`
	Parsed  int                 `json:"parsed"`
}

// XrayAccessLogService reads and toggles the Xray access log.
type XrayAccessLogService struct {
	configDir string
	// accessPath is where the log goes when it is switched on.
	accessPath string
	// Resolve maps a LAN IP to a device name (router clients).
	Resolve func(ip string) string
}

func NewXrayAccessLogService(configDir string) *XrayAccessLogService {
	return &XrayAccessLogService{configDir: configDir, accessPath: DefaultXrayAccessLog}
}

// findLogConfig returns the config file holding the top-level "log" section
// and its parsed access value.
func (s *XrayAccessLogService) findLogConfig() (string, string, bool, error) {
	files, _ := filepath.Glob(filepath.Join(s.configDir, "*.json"))
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var root map[string]json.RawMessage
		if json.Unmarshal([]byte(StripJSONComments(string(data))), &root) != nil {
			continue
		}
		raw, ok := root["log"]
		if !ok {
			continue
		}
		var log struct {
			Access *string `json:"access"`
		}
		_ = json.Unmarshal(raw, &log)
		if log.Access == nil {
			return f, "", false, nil
		}
		return f, *log.Access, true, nil
	}
	return "", "", false, ErrXrayLogConfigNotFound
}

// Status reports where the access log goes and whether it is enabled.
func (s *XrayAccessLogService) Status() (*XrayAccessLogStatus, error) {
	file, access, _, err := s.findLogConfig()
	if err != nil {
		return nil, err
	}
	st := &XrayAccessLogStatus{ConfigFile: file, AccessPath: access}
	st.Enabled = access != "" && !strings.EqualFold(access, "none")
	if st.Enabled {
		if fi, err := os.Stat(access); err == nil {
			st.FileSize = fi.Size()
		}
	}
	return st, nil
}

var accessValueRe = regexp.MustCompile(`("access"\s*:\s*)"[^"]*"`)
var logSectionRe = regexp.MustCompile(`("log"\s*:\s*\{)`)

// SetEnabled switches the access log on (to s.accessPath) or off
// ("none") by editing only the "access" value, so comments and the rest of
// the file are kept. Xray must be restarted for the change to apply.
func (s *XrayAccessLogService) SetEnabled(enabled bool) (*XrayAccessLogStatus, error) {
	file, _, hasAccess, err := s.findLogConfig()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	value := "none"
	if enabled {
		value = s.accessPath
		if err := os.MkdirAll(filepath.Dir(value), 0o755); err != nil {
			return nil, err
		}
	}
	quoted, _ := json.Marshal(value)
	content := string(data)
	if hasAccess {
		content = accessValueRe.ReplaceAllString(content, "${1}"+strings.ReplaceAll(string(quoted), "$", "$$"))
	} else {
		loc := logSectionRe.FindStringIndex(content)
		if loc == nil {
			return nil, ErrXrayLogConfigNotFound
		}
		content = content[:loc[1]] + "\n    \"access\": " + string(quoted) + "," + content[loc[1]:]
	}
	if !json.Valid([]byte(StripJSONComments(content))) {
		return nil, fmt.Errorf("edited %s is not valid JSON", filepath.Base(file))
	}
	if err := utils.AtomicReplaceFile(file, []byte(content)); err != nil {
		return nil, err
	}
	return s.Status()
}

// Xray access log line, e.g.
//
//	2026/09/24 03:59:04.123456 from 172.16.0.136:52344 accepted tcp:www.google.com:443 [tproxy-in >> vless-reality] email: user@x
//	2026/09/24 03:59:04 from tcp:172.16.0.5:5555 accepted udp:8.8.8.8:53 [dns-in -> dns-out]
//	2026/09/24 03:59:04 from 172.16.0.5:5555 rejected  proxy/vless/encoding: invalid request user id
var (
	accessLineRe = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})(?:\.\d+)?\s+from\s+(?:(?:tcp|udp):)?(\[[0-9a-fA-F:.]+\]|[0-9.]+|[0-9a-fA-F:]+?):(\d+)\s+(accepted|rejected)\s+(.*)$`)
	accessDestRe = regexp.MustCompile(`^(?:(tcp|udp):)?(\[[0-9a-fA-F:.]+\]|[^\s\[]+?)(?::(\d+))?(?:\s+\[([^\]]*)\])?(?:\s+email:\s*(\S+))?\s*$`)
)

func parseXrayAccessLine(line string) (XrayAccessEntry, bool) {
	m := accessLineRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return XrayAccessEntry{}, false
	}
	e := XrayAccessEntry{
		Time:     m[1],
		SourceIP: strings.Trim(m[2], "[]"),
		Status:   m[4],
	}
	rest := strings.TrimSpace(m[5])
	if e.Status == "rejected" {
		e.Reason = rest
		return e, true
	}
	d := accessDestRe.FindStringSubmatch(rest)
	if d == nil {
		e.Destination = rest
		return e, true
	}
	e.Network, e.Destination, e.Port, e.Email = d[1], strings.Trim(d[2], "[]"), d[3], d[5]
	route := d[4]
	for _, sep := range []string{">>", "->"} {
		if in, out, ok := strings.Cut(route, sep); ok {
			e.Inbound, e.Outbound = strings.TrimSpace(in), strings.TrimSpace(out)
			return e, true
		}
	}
	e.Outbound = strings.TrimSpace(route)
	return e, true
}

// Report parses the tail of the access log, applies the filter and builds
// a per-device summary. Entries are returned newest first.
func (s *XrayAccessLogService) Report(f XrayAccessFilter) (*XrayAccessReport, error) {
	st, err := s.Status()
	if err != nil {
		return nil, err
	}
	report := &XrayAccessReport{Entries: []XrayAccessEntry{}, Devices: []XrayDeviceSummary{}}
	if !st.Enabled {
		return report, nil
	}
	lines, err := tailLines(st.AccessPath, xrayAccessTailBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return report, nil
		}
		return nil, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	dest := strings.ToLower(strings.TrimSpace(f.Destination))
	names := map[string]string{}
	name := func(ip string) string {
		if n, ok := names[ip]; ok {
			return n
		}
		n := ""
		if s.Resolve != nil {
			n = s.Resolve(ip)
		}
		names[ip] = n
		return n
	}

	type agg struct {
		sum   XrayDeviceSummary
		dests map[string]int
	}
	devices := map[string]*agg{}
	var matched []XrayAccessEntry
	for _, line := range lines {
		e, ok := parseXrayAccessLine(line)
		if !ok {
			continue
		}
		report.Parsed++
		e.Device = name(e.SourceIP)

		a := devices[e.SourceIP]
		if a == nil {
			a = &agg{sum: XrayDeviceSummary{IP: e.SourceIP, Device: e.Device}, dests: map[string]int{}}
			devices[e.SourceIP] = a
		}
		a.sum.Connections++
		if e.Status == "rejected" {
			a.sum.Rejected++
		}
		a.sum.LastSeen = e.Time
		if e.Destination != "" {
			a.dests[e.Destination]++
		}

		if f.SourceIP != "" && e.SourceIP != f.SourceIP {
			continue
		}
		if f.Outbound != "" && !strings.EqualFold(e.Outbound, f.Outbound) {
			continue
		}
		if dest != "" && !strings.Contains(strings.ToLower(e.Destination), dest) {
			continue
		}
		matched = append(matched, e)
	}

	for i := len(matched) - 1; i >= 0 && len(report.Entries) < limit; i-- {
		report.Entries = append(report.Entries, matched[i])
	}
	for _, a := range devices {
		a.sum.TopDestinations = topKeys(a.dests, 5)
		report.Devices = append(report.Devices, a.sum)
	}
	sort.Slice(report.Devices, func(i, j int) bool {
		return report.Devices[i].Connections > report.Devices[j].Connections
	})
	return report, nil
}

func topKeys(counts map[string]int, n int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}

// tailLines returns the complete lines within the last maxBytes of a file.
func tailLines(path string, maxBytes int64) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	partial := false
	if st.Size() > maxBytes {
		if _, err := f.Seek(-maxBytes, io.SeekEnd); err != nil {
			return nil, err
		}
		partial = true
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if partial {
			// The seek lands mid-line; skip the fragment.
			partial = false
			continue
		}
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}
