package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseXrayAccessLine(t *testing.T) {
	cases := []struct {
		line string
		want XrayAccessEntry
	}{
		{
			"2026/09/24 03:59:04.123456 from 172.16.0.136:52344 accepted tcp:www.google.com:443 [tproxy-in >> vless-reality] email: user@x",
			XrayAccessEntry{Time: "2026/09/24 03:59:04", SourceIP: "172.16.0.136", Status: "accepted", Network: "tcp", Destination: "www.google.com", Port: "443", Inbound: "tproxy-in", Outbound: "vless-reality", Email: "user@x"},
		},
		{
			"2026/09/24 03:59:05 from tcp:172.16.0.5:5555 accepted udp:8.8.8.8:53 [dns-in -> dns-out]",
			XrayAccessEntry{Time: "2026/09/24 03:59:05", SourceIP: "172.16.0.5", Status: "accepted", Network: "udp", Destination: "8.8.8.8", Port: "53", Inbound: "dns-in", Outbound: "dns-out"},
		},
		{
			"2026/09/24 03:59:06 from [fd00::5]:4444 accepted tcp:[2a00:1450::200e]:443 [redirect >> direct]",
			XrayAccessEntry{Time: "2026/09/24 03:59:06", SourceIP: "fd00::5", Status: "accepted", Network: "tcp", Destination: "2a00:1450::200e", Port: "443", Inbound: "redirect", Outbound: "direct"},
		},
		{
			"2026/09/24 03:59:07 from 172.16.0.9:1000 rejected  proxy/vless/encoding: invalid request user id",
			XrayAccessEntry{Time: "2026/09/24 03:59:07", SourceIP: "172.16.0.9", Status: "rejected", Reason: "proxy/vless/encoding: invalid request user id"},
		},
		{
			"2026/09/24 03:59:08 from 172.16.0.9:1001 accepted tcp:example.com:80 [block]",
			XrayAccessEntry{Time: "2026/09/24 03:59:08", SourceIP: "172.16.0.9", Status: "accepted", Network: "tcp", Destination: "example.com", Port: "80", Outbound: "block"},
		},
	}
	for _, c := range cases {
		got, ok := parseXrayAccessLine(c.line)
		if !ok || got != c.want {
			t.Errorf("parse %q:\n got  %+v\n want %+v", c.line, got, c.want)
		}
	}
	if _, ok := parseXrayAccessLine("garbage line"); ok {
		t.Error("garbage parsed")
	}
}

func newAccessFixture(t *testing.T, logJSON string) (*XrayAccessLogService, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01_log.json"), []byte(logJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(`{"routing":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewXrayAccessLogService(dir), dir
}

func TestXrayAccessLog_ToggleKeepsComments(t *testing.T) {
	svc, dir := newAccessFixture(t, "{\n  // keep me\n  \"log\": {\n    \"access\": \"none\",\n    \"loglevel\": \"warning\"\n  }\n}\n")
	svc.accessPath = filepath.Join(t.TempDir(), "xray", "access.log")
	st, err := svc.Status()
	if err != nil || st.Enabled || !strings.HasSuffix(st.ConfigFile, "01_log.json") {
		t.Fatalf("status: %+v %v", st, err)
	}
	if _, err := svc.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if !strings.Contains(string(data), "// keep me") || !strings.Contains(string(data), `"access": "`+svc.accessPath+`"`) {
		t.Fatalf("enable: %s", data)
	}
	st, _ = svc.Status()
	if !st.Enabled || st.AccessPath != svc.accessPath {
		t.Fatalf("status after enable: %+v", st)
	}
	if _, err := svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if st, _ = svc.Status(); st.Enabled {
		t.Fatal("still enabled")
	}
}

func TestXrayAccessLog_InsertsMissingAccessKey(t *testing.T) {
	svc, dir := newAccessFixture(t, `{"log": {"loglevel": "warning"}}`)
	if _, err := svc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "01_log.json"))
	if !strings.Contains(string(data), `"access": "none"`) {
		t.Fatalf("access key not inserted: %s", data)
	}
}

func TestXrayAccessLog_Report(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "access.log")
	svc, _ := newAccessFixture(t, `{"log": {"access": "`+logFile+`"}}`)
	svc.Resolve = func(ip string) string {
		if ip == "172.16.0.136" {
			return "LEGION"
		}
		return ""
	}
	lines := []string{
		"2026/09/24 04:00:01 from 172.16.0.136:1 accepted tcp:youtube.com:443 [tproxy >> proxy]",
		"2026/09/24 04:00:02 from 172.16.0.136:2 accepted tcp:youtube.com:443 [tproxy >> proxy]",
		"2026/09/24 04:00:03 from 172.16.0.63:3 accepted tcp:ya.ru:443 [tproxy >> direct]",
		"not an access line",
		"2026/09/24 04:00:04 from 172.16.0.136:4 accepted tcp:github.com:443 [tproxy >> direct]",
	}
	if err := os.WriteFile(logFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := svc.Report(XrayAccessFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Parsed != 4 || len(r.Entries) != 4 || r.Entries[0].Destination != "github.com" || r.Entries[0].Device != "LEGION" {
		t.Fatalf("report: %+v", r)
	}
	if len(r.Devices) != 2 || r.Devices[0].IP != "172.16.0.136" || r.Devices[0].Connections != 3 || r.Devices[0].TopDestinations[0] != "youtube.com" {
		t.Fatalf("devices: %+v", r.Devices)
	}

	r, _ = svc.Report(XrayAccessFilter{SourceIP: "172.16.0.136", Outbound: "direct"})
	if len(r.Entries) != 1 || r.Entries[0].Destination != "github.com" {
		t.Fatalf("filter: %+v", r.Entries)
	}
	r, _ = svc.Report(XrayAccessFilter{Destination: "YA."})
	if len(r.Entries) != 1 || r.Entries[0].SourceIP != "172.16.0.63" {
		t.Fatalf("destination filter: %+v", r.Entries)
	}
}

func TestTailLines_SkipsPartialFirstLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.log")
	if err := os.WriteFile(path, []byte("aaaaaaaaaa\nbbbb\ncccc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := tailLines(path, 12)
	// The seek lands inside the first line: its fragment "a" is dropped.
	if err != nil || strings.Join(lines, ",") != "bbbb,cccc" {
		t.Fatalf("tail: %q %v", lines, err)
	}
}
