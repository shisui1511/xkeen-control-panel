package services

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func evalCtx(host string, port int) *ruleEvalContext {
	ip := net.ParseIP(host)
	return &ruleEvalContext{ctx: context.Background(), host: host, port: port, isIP: ip != nil, ip: ip}
}

func TestEvalRule_BothSpellings(t *testing.T) {
	cases := []struct {
		host         string
		typ, payload string
		want         ruleVerdict
	}{
		{"mail.google.com", "DomainSuffix", "google.com", verdictYes},
		{"mail.google.com", "DOMAIN-SUFFIX", "google.com", verdictYes},
		{"notgoogle.com", "DomainSuffix", "google.com", verdictNo},
		{"a.youtube.com", "DomainKeyword", "tube", verdictYes},
		{"x.example.com", "DOMAIN-WILDCARD", "*.example.com", verdictYes},
		{"x.y.example.com", "DomainWildcard", "*.example.com", verdictNo},
		{"api.github.com", "DomainRegex", `^api\.`, verdictYes},
		{"8.8.8.8", "IPCIDR", "8.8.8.0/24", verdictYes},
		{"8.8.8.8", "IP-CIDR", "1.1.1.1/32", verdictNo},
		{"192.168.1.5", "GeoIP", "private", verdictYes},
		{"example.com", "ProcessName", "curl", verdictUnknown},
		{"example.com", "Match", "", verdictYes},
	}
	for _, c := range cases {
		got, _ := evalCtx(c.host, 443).evalRule(c.typ, c.payload, nil)
		if got != c.want {
			t.Errorf("%s %s,%s = %v, want %v", c.host, c.typ, c.payload, got, c.want)
		}
	}
}

func TestEvalRule_PortSpecAndNoResolve(t *testing.T) {
	ec := evalCtx("example.com", 8443)
	for spec, want := range map[string]ruleVerdict{"443": verdictNo, "80/8443": verdictYes, "8000-9000": verdictYes} {
		if got, _ := ec.evalRule("DST-PORT", spec, nil); got != want {
			t.Errorf("DST-PORT %s = %v, want %v", spec, got, want)
		}
	}
	// A domain never matches IP rules marked no-resolve.
	if got, _ := ec.evalRule("IP-CIDR", "0.0.0.0/0", []string{"no-resolve"}); got != verdictNo {
		t.Errorf("no-resolve IP-CIDR on domain = %v", got)
	}
}

func TestEvalRule_Logic(t *testing.T) {
	ec := evalCtx("usher.ttvnw.net", 443)
	api := "((DomainSuffix,gql.twitch.tv) || (DomainSuffix,usher.ttvnw.net))"
	if got, _ := ec.evalRule("OR", api, nil); got != verdictYes {
		t.Errorf("API OR = %v", got)
	}
	cfg := "((DOMAIN-SUFFIX,gql.twitch.tv),(DOMAIN-SUFFIX,usher.ttvnw.net))"
	if got, _ := ec.evalRule("OR", cfg, nil); got != verdictYes {
		t.Errorf("config OR = %v", got)
	}
	if got, _ := ec.evalRule("AND", "((DOMAIN-SUFFIX,ttvnw.net),(DST-PORT,80))", nil); got != verdictNo {
		t.Errorf("AND with false part = %v", got)
	}
	if got, _ := ec.evalRule("NOT", "((DOMAIN,other.com))", nil); got != verdictYes {
		t.Errorf("NOT = %v", got)
	}
	if got, _ := ec.evalRule("AND", "((DST-PORT,443),(NETWORK,UDP))", nil); got != verdictNo {
		t.Errorf("QUIC rule must not match a TCP trace, got %v", got)
	}
	if got, _ := ec.evalRule("AND", "((DOMAIN-SUFFIX,ttvnw.net),(PROCESS-NAME,x))", nil); got != verdictUnknown {
		t.Errorf("AND with unknown part = %v", got)
	}
	nested := "((OR,((DOMAIN,a.com),(DOMAIN,usher.ttvnw.net))),(DST-PORT,443))"
	if got, _ := ec.evalRule("AND", nested, nil); got != verdictYes {
		t.Errorf("nested logic = %v", got)
	}
}

func TestDomainSetEntryMatches(t *testing.T) {
	cases := map[string][]string{
		"+.example.com": {"example.com", "a.example.com"},
		".example.com":  {"a.example.com"},
		"*.example.com": {"a.example.com"},
		"example.com":   {"example.com"},
	}
	for entry, hosts := range cases {
		for _, h := range hosts {
			if !domainSetEntryMatches(entry, h) {
				t.Errorf("%s should match %s", entry, h)
			}
		}
	}
	if domainSetEntryMatches(".example.com", "example.com") || domainSetEntryMatches("example.com", "a.example.com") {
		t.Error("unexpected match")
	}
}

func newTestRuleSets(t *testing.T, providers map[string]ruleProviderDef) (*ruleSetStore, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &ruleSetStore{providers: providers, homeDir: home, cacheDir: filepath.Join(t.TempDir(), "cache"), mu: &sync.Mutex{}}, home
}

func TestEvalRuleSet_Formats(t *testing.T) {
	store, home := newTestRuleSets(t, map[string]ruleProviderDef{
		"inline":  {Type: "inline", Behavior: "classical", Payload: []string{"DOMAIN-SUFFIX,inline.test"}},
		"text":    {Type: "http", Behavior: "domain", Format: "text", URL: "https://example.com/list.txt"},
		"yaml":    {Type: "file", Behavior: "ipcidr", Format: "yaml", Path: "./rules/ips.yaml"},
		"escape":  {Type: "file", Behavior: "domain", Format: "text", Path: "../../etc/hosts"},
		"missing": {Type: "file", Behavior: "domain", Format: "text", Path: "./rules/none.txt"},
	})
	sum := md5.Sum([]byte("https://example.com/list.txt"))
	if err := os.WriteFile(filepath.Join(home, "rules", hex.EncodeToString(sum[:])), []byte("# c\n+.youtube.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "rules", "ips.yaml"), []byte("payload:\n  - 91.108.4.0/22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	check := func(host, set string, want ruleVerdict) {
		t.Helper()
		ec := evalCtx(host, 443)
		ec.sets = store
		if got, _ := ec.evalRule("RuleSet", set, nil); got != want {
			t.Errorf("RULE-SET %s for %s = %v, want %v", set, host, got, want)
		}
	}
	check("a.inline.test", "inline", verdictYes)
	check("www.youtube.com", "text", verdictYes)
	check("example.org", "text", verdictNo)
	check("91.108.5.1", "yaml", verdictYes)
	check("example.org", "escape", verdictUnknown)
	check("example.org", "missing", verdictUnknown)
	check("example.org", "undeclared", verdictUnknown)

	// A provider the core reports as empty cannot match, even without a file.
	store.empty = map[string]bool{"missing": true}
	check("example.org", "missing", verdictNo)
}

func TestEvalRuleSet_MRSConversionIsCached(t *testing.T) {
	store, home := newTestRuleSets(t, map[string]ruleProviderDef{
		"ads": {Type: "http", Behavior: "domain", Format: "mrs", URL: "https://example.com/ads.mrs"},
	})
	sum := md5.Sum([]byte("https://example.com/ads.mrs"))
	if err := os.WriteFile(filepath.Join(home, "rules", hex.EncodeToString(sum[:])), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake mihomo: "convert-ruleset <behavior> mrs <in> <out>" writes a list
	// and counts its invocations.
	counter := filepath.Join(t.TempDir(), "calls")
	bin := filepath.Join(t.TempDir(), "mihomo")
	script := "#!/bin/sh\necho x >> " + counter + "\nprintf '+.ads.test\\n' > \"$5\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	store.mihomoBin = bin

	for i := 0; i < 3; i++ {
		ec := evalCtx("tracker.ads.test", 443)
		ec.sets = store
		if got, _ := ec.evalRule("RULE-SET", "ads", nil); got != verdictYes {
			t.Fatalf("run %d: got %v", i, got)
		}
	}
	calls, _ := os.ReadFile(counter)
	if n := strings.Count(string(calls), "x"); n != 1 {
		t.Fatalf("expected one conversion, got %d", n)
	}
}

func TestMatchKernelRules_Undetermined(t *testing.T) {
	rules := []MihomoKernelRule{
		{Type: "RuleSet", Payload: "adlist@domain", Proxy: "REJECT"},
		{Type: "DomainSuffix", Payload: "youtube.com", Proxy: "YouTube"},
		{Type: "Match", Proxy: "DIRECT"},
	}
	rules[1].Extra.Disabled = false

	res := matchKernelRules(evalCtx("www.youtube.com", 443), rules)
	if res == nil || res.TargetGroup != "YouTube" || res.RuleIndex != 2 {
		t.Fatalf("unexpected result %+v", res)
	}
	if !res.Undetermined || res.UndeterminedRule != "RuleSet,adlist@domain" || res.UndeterminedGroup != "REJECT" || res.UndeterminedReason != unknownRuleSet {
		t.Fatalf("expected undetermined rule-set warning, got %+v", res)
	}

	// Disabled rules are skipped.
	rules[1].Extra.Disabled = true
	res = matchKernelRules(evalCtx("www.youtube.com", 443), rules)
	if res.TargetGroup != "DIRECT" {
		t.Fatalf("disabled rule matched: %+v", res)
	}
}
