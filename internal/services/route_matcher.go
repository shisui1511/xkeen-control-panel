package services

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ruleVerdict is the outcome of testing one routing rule against a target.
// Rules the panel cannot evaluate faithfully (process names, source
// addresses, unreadable rule sets…) are reported as unknown instead of being
// silently skipped, so the trace never claims a match it cannot prove.
type ruleVerdict int

const (
	verdictNo ruleVerdict = iota
	verdictYes
	verdictUnknown
)

// Reasons attached to unknown verdicts; the UI translates them.
const (
	unknownUnsupportedType = "unsupported_type"
	unknownRuleSet         = "rule_set_unavailable"
	unknownGeoData         = "geodata_unavailable"
	unknownResolve         = "resolve_failed"
)

// normalizeRuleType maps both config spelling ("DOMAIN-SUFFIX") and Clash
// API spelling ("DomainSuffix") to one canonical form ("DOMAINSUFFIX").
func normalizeRuleType(t string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", "_", "").Replace(strings.TrimSpace(t)))
}

// ruleProviderDef is a rule-provider as declared in config.yaml.
type ruleProviderDef struct {
	Type     string   `yaml:"type"`
	Behavior string   `yaml:"behavior"`
	Format   string   `yaml:"format"`
	Path     string   `yaml:"path"`
	URL      string   `yaml:"url"`
	Payload  []string `yaml:"payload"`
}

// GeoTagLookup returns the GeoSite and GeoIP tags (lower case) that contain
// the target. A nil map means that database is not available.
type GeoTagLookup func(ctx context.Context, target string) (geosite, geoip map[string]bool, err error)

// ruleEvalContext carries the target and lazily computed facts about it.
type ruleEvalContext struct {
	ctx   context.Context
	host  string
	port  int
	isIP  bool
	ip    net.IP
	sets  *ruleSetStore
	geo   GeoTagLookup
	depth int

	resolved    bool
	resolvedIPs []net.IP

	geoLoaded  bool
	geoErr     error
	geositeSet map[string]bool
	geoipSet   map[string]bool
}

// targetIPs returns the IPs IP-based rules are checked against: the target
// itself, or the resolved addresses of a domain, as Mihomo does unless the
// rule carries no-resolve.
func (c *ruleEvalContext) targetIPs(noResolve bool) ([]net.IP, bool) {
	if c.isIP {
		return []net.IP{c.ip}, true
	}
	if noResolve {
		return nil, true
	}
	if !c.resolved {
		c.resolved = true
		ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
		defer cancel()
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", c.host)
		if err == nil {
			c.resolvedIPs = ips
		}
	}
	return c.resolvedIPs, len(c.resolvedIPs) > 0
}

func (c *ruleEvalContext) geoTags() (map[string]bool, map[string]bool, error) {
	if !c.geoLoaded {
		c.geoLoaded = true
		if c.geo == nil {
			c.geoErr = fmt.Errorf("geodata lookup unavailable")
		} else {
			target := c.host
			if c.isIP {
				target = c.ip.String()
			}
			c.geositeSet, c.geoipSet, c.geoErr = c.geo(c.ctx, target)
		}
	}
	return c.geositeSet, c.geoipSet, c.geoErr
}

// evalRule tests a single rule. params are the trailing rule options such
// as "no-resolve".
func (c *ruleEvalContext) evalRule(ruleType, payload string, params []string) (ruleVerdict, string) {
	noResolve := false
	for _, p := range params {
		if strings.EqualFold(strings.TrimSpace(p), "no-resolve") {
			noResolve = true
		}
	}
	host := strings.ToLower(c.host)

	switch normalizeRuleType(ruleType) {
	case "MATCH":
		return verdictYes, ""
	case "DOMAIN":
		return boolVerdict(!c.isIP && host == strings.ToLower(payload)), ""
	case "DOMAINSUFFIX":
		suffix := strings.ToLower(strings.TrimPrefix(payload, "."))
		return boolVerdict(!c.isIP && (host == suffix || strings.HasSuffix(host, "."+suffix))), ""
	case "DOMAINKEYWORD":
		return boolVerdict(!c.isIP && strings.Contains(host, strings.ToLower(payload))), ""
	case "DOMAINWILDCARD":
		return boolVerdict(!c.isIP && wildcardMatch(strings.ToLower(payload), host)), ""
	case "DOMAINREGEX":
		re, err := regexp.Compile(payload)
		if err != nil {
			return verdictUnknown, unknownUnsupportedType
		}
		return boolVerdict(!c.isIP && re.MatchString(c.host)), ""
	case "IPCIDR", "IPCIDR6":
		_, ipNet, err := net.ParseCIDR(withDefaultMask(payload))
		if err != nil {
			return verdictNo, ""
		}
		ips, ok := c.targetIPs(noResolve)
		if !ok && !noResolve {
			return verdictUnknown, unknownResolve
		}
		for _, ip := range ips {
			if ipNet.Contains(ip) {
				return verdictYes, ""
			}
		}
		return verdictNo, ""
	case "DSTPORT":
		return boolVerdict(portInSpec(c.port, payload)), ""
	case "NETWORK":
		// The trace models an ordinary TCP connection.
		return boolVerdict(strings.EqualFold(strings.TrimSpace(payload), "tcp")), ""
	case "GEOIP":
		return c.evalGeoIP(payload, noResolve)
	case "GEOSITE":
		if c.isIP {
			return verdictNo, ""
		}
		sites, _, err := c.geoTags()
		if err != nil || sites == nil {
			return verdictUnknown, unknownGeoData
		}
		return boolVerdict(sites[strings.ToLower(payload)]), ""
	case "RULESET":
		return c.evalRuleSet(payload)
	case "AND", "OR", "NOT":
		return c.evalLogic(normalizeRuleType(ruleType), payload)
	}
	return verdictUnknown, unknownUnsupportedType
}

func (c *ruleEvalContext) evalGeoIP(payload string, noResolve bool) (ruleVerdict, string) {
	code := strings.ToLower(strings.TrimSpace(payload))
	if code == "private" || code == "lan" {
		ips, ok := c.targetIPs(noResolve)
		if !ok && !noResolve {
			return verdictUnknown, unknownResolve
		}
		for _, ip := range ips {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				return verdictYes, ""
			}
		}
		return verdictNo, ""
	}
	if !c.isIP && noResolve {
		return verdictNo, ""
	}
	_, ips, err := c.geoTags()
	if err != nil || ips == nil {
		return verdictUnknown, unknownGeoData
	}
	return boolVerdict(ips[code]), ""
}

func (c *ruleEvalContext) evalRuleSet(name string) (ruleVerdict, string) {
	if c.sets == nil || c.depth > 0 {
		return verdictUnknown, unknownRuleSet
	}
	if c.sets.empty[name] {
		return verdictNo, ""
	}
	def, ok := c.sets.providers[name]
	if !ok {
		return verdictUnknown, unknownRuleSet
	}
	entries, err := c.sets.entries(c.ctx, name, def)
	if err != nil {
		log.Printf("RouteTracer: rule set %s unavailable: %v", name, err)
		return verdictUnknown, unknownRuleSet
	}
	defer entries.Close()

	behavior := strings.ToLower(def.Behavior)
	unknown := false
	scanner := bufio.NewScanner(entries)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch behavior {
		case "domain":
			if !c.isIP && domainSetEntryMatches(line, strings.ToLower(c.host)) {
				return verdictYes, ""
			}
		case "ipcidr":
			if _, ipNet, err := net.ParseCIDR(withDefaultMask(line)); err == nil {
				if ips, ok := c.targetIPs(false); ok {
					for _, ip := range ips {
						if ipNet.Contains(ip) {
							return verdictYes, ""
						}
					}
				}
			}
		case "classical":
			t, p, params := splitRuleLine(line)
			sub := *c
			sub.depth = c.depth + 1
			v, _ := sub.evalRule(t, p, params)
			c.resolved, c.resolvedIPs = sub.resolved, sub.resolvedIPs
			switch v {
			case verdictYes:
				return verdictYes, ""
			case verdictUnknown:
				unknown = true
			}
		default:
			return verdictUnknown, unknownRuleSet
		}
	}
	if err := scanner.Err(); err != nil || unknown {
		return verdictUnknown, unknownRuleSet
	}
	return verdictNo, ""
}

// evalLogic evaluates AND/OR/NOT. The payload is either the config form
// "((DOMAIN,a),(NETWORK,UDP))" or the Clash API form
// "((Domain,a) && (Network,udp))".
func (c *ruleEvalContext) evalLogic(op, payload string) (ruleVerdict, string) {
	subs := splitLogicPayload(payload)
	if len(subs) == 0 {
		return verdictUnknown, unknownUnsupportedType
	}
	results := make([]ruleVerdict, 0, len(subs))
	reason := ""
	for _, s := range subs {
		t, p, params := splitRuleLine(s)
		v, r := c.evalRule(t, p, params)
		if v == verdictUnknown && reason == "" {
			reason = r
		}
		results = append(results, v)
	}
	switch op {
	case "NOT":
		switch results[0] {
		case verdictYes:
			return verdictNo, ""
		case verdictNo:
			return verdictYes, ""
		}
		return verdictUnknown, reason
	case "AND":
		unknown := false
		for _, v := range results {
			if v == verdictNo {
				return verdictNo, ""
			}
			if v == verdictUnknown {
				unknown = true
			}
		}
		if unknown {
			return verdictUnknown, reason
		}
		return verdictYes, ""
	default: // OR
		unknown := false
		for _, v := range results {
			if v == verdictYes {
				return verdictYes, ""
			}
			if v == verdictUnknown {
				unknown = true
			}
		}
		if unknown {
			return verdictUnknown, reason
		}
		return verdictNo, ""
	}
}

func boolVerdict(b bool) ruleVerdict {
	if b {
		return verdictYes
	}
	return verdictNo
}

func withDefaultMask(cidr string) string {
	cidr = strings.TrimSpace(cidr)
	if strings.Contains(cidr, "/") {
		return cidr
	}
	if strings.Contains(cidr, ":") {
		return cidr + "/128"
	}
	return cidr + "/32"
}

// portInSpec supports "443", "80/443", "8000-9000" and combinations.
func portInSpec(port int, spec string) bool {
	for _, part := range strings.FieldsFunc(spec, func(r rune) bool { return r == '/' || r == ',' }) {
		part = strings.TrimSpace(part)
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			a, err1 := strconv.Atoi(strings.TrimSpace(lo))
			b, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 == nil && err2 == nil && port >= a && port <= b {
				return true
			}
			continue
		}
		if p, err := strconv.Atoi(part); err == nil && p == port {
			return true
		}
	}
	return false
}

// domainSetEntryMatches implements Mihomo's domain-behavior syntax:
// "+.a.com" (a.com and subdomains), ".a.com" (subdomains only),
// "*.a.com" (exactly one label), "a.com" (exact).
func domainSetEntryMatches(entry, host string) bool {
	entry = strings.ToLower(strings.Trim(entry, `"' `))
	switch {
	case strings.HasPrefix(entry, "+."):
		base := entry[2:]
		return host == base || strings.HasSuffix(host, "."+base)
	case strings.HasPrefix(entry, "."):
		return strings.HasSuffix(host, entry)
	case strings.Contains(entry, "*"):
		return wildcardMatch(entry, host)
	default:
		return host == entry
	}
}

// wildcardMatch matches "*" against a single domain label.
func wildcardMatch(pattern, host string) bool {
	pl := strings.Split(pattern, ".")
	hl := strings.Split(host, ".")
	if len(pl) != len(hl) {
		return false
	}
	for i := range pl {
		if pl[i] != "*" && pl[i] != hl[i] {
			return false
		}
	}
	return true
}

// splitRuleLine splits "TYPE,payload[,target][,params…]" keeping a
// parenthesised logic payload intact. For rule-set entries and logic
// sub-rules there is no target, so everything after the payload is params.
func splitRuleLine(line string) (ruleType, payload string, rest []string) {
	parts := splitTopLevel(strings.TrimSpace(line), ',')
	if len(parts) == 0 {
		return "", "", nil
	}
	ruleType = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		payload = strings.TrimSpace(parts[1])
	}
	for _, p := range parts[2:] {
		rest = append(rest, strings.TrimSpace(p))
	}
	return ruleType, payload, rest
}

// splitTopLevel splits s by sep ignoring separators nested in parentheses.
func splitTopLevel(s string, sep rune) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// splitLogicPayload returns the sub-rules of a logic payload in either form.
func splitLogicPayload(payload string) []string {
	p := strings.TrimSpace(payload)
	p = strings.TrimPrefix(p, "!")
	if strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")") {
		p = p[1 : len(p)-1]
	}
	var subs []string
	depth, start := 0, -1
	for i, r := range p {
		switch r {
		case '(':
			if depth == 0 {
				start = i
			}
			depth++
		case ')':
			depth--
			if depth == 0 && start >= 0 {
				subs = append(subs, p[start+1:i])
				start = -1
			}
		}
	}
	return subs
}

// ruleSetStore reads rule-provider contents from disk. Binary MRS sets are
// converted once with "mihomo convert-ruleset" and cached by path, size and
// mtime, since converting a large list costs seconds of router CPU.
type ruleSetStore struct {
	providers map[string]ruleProviderDef
	homeDir   string
	cacheDir  string
	mihomoBin string

	// empty names providers the running core reports with zero rules
	// (e.g. never downloaded): they cannot match anything.
	empty map[string]bool

	// mu serialises MRS conversions; shared by all traces of a service.
	mu *sync.Mutex
}

// loadRuleProviders parses the rule-providers section of a Mihomo config,
// YAML anchors and merge keys included.
func loadRuleProviders(configPath string) (map[string]ruleProviderDef, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		RuleProviders map[string]ruleProviderDef `yaml:"rule-providers"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return parsed.RuleProviders, nil
}

func (s *ruleSetStore) providerPath(def ruleProviderDef) string {
	if def.Path != "" {
		if filepath.IsAbs(def.Path) {
			return filepath.Clean(def.Path)
		}
		return filepath.Join(s.homeDir, def.Path)
	}
	if def.URL != "" {
		sum := md5.Sum([]byte(def.URL))
		return filepath.Join(s.homeDir, "rules", hex.EncodeToString(sum[:]))
	}
	return ""
}

// entries returns a reader of one rule per line.
func (s *ruleSetStore) entries(ctx context.Context, name string, def ruleProviderDef) (io.ReadCloser, error) {
	if strings.EqualFold(def.Type, "inline") {
		return io.NopCloser(strings.NewReader(strings.Join(def.Payload, "\n"))), nil
	}
	path := s.providerPath(def)
	if path == "" || !strings.HasPrefix(path, filepath.Clean(s.homeDir)+string(filepath.Separator)) {
		return nil, fmt.Errorf("rule set %s: path outside mihomo directory", name)
	}
	switch strings.ToLower(def.Format) {
	case "mrs":
		return s.convertedMRS(ctx, path, strings.ToLower(def.Behavior))
	case "yaml", "":
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Payload []string `yaml:"payload"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		return io.NopCloser(strings.NewReader(strings.Join(doc.Payload, "\n"))), nil
	default: // text
		return os.Open(path)
	}
}

func (s *ruleSetStore) convertedMRS(ctx context.Context, path, behavior string) (io.ReadCloser, error) {
	if s.mihomoBin == "" || s.cacheDir == "" {
		return nil, fmt.Errorf("mrs conversion unavailable")
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := sha1.Sum([]byte(path))
	prefix := hex.EncodeToString(key[:8])
	cached := filepath.Join(s.cacheDir, fmt.Sprintf("%s-%d-%d.txt", prefix, st.Size(), st.ModTime().Unix()))

	s.mu.Lock()
	defer s.mu.Unlock()

	if f, err := os.Open(cached); err == nil {
		return f, nil
	}
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return nil, err
	}
	tmp := cached + ".tmp"
	// Detached from the request: an aborted trace must not waste the
	// conversion, the next trace reuses the cache.
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.mihomoBin, "convert-ruleset", behavior, "mrs", path, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("convert-ruleset: %v: %s", err, strings.TrimSpace(string(out)))
	}
	// Drop stale conversions of the same provider file.
	if old, _ := filepath.Glob(filepath.Join(s.cacheDir, prefix+"-*.txt")); len(old) > 0 {
		for _, o := range old {
			_ = os.Remove(o)
		}
	}
	if err := os.Rename(tmp, cached); err != nil {
		return nil, err
	}
	return os.Open(cached)
}
