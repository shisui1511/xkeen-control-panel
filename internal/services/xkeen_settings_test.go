package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func issueCodes(issues []XKeenSettingsIssue) []string {
	codes := make([]string, 0, len(issues))
	for _, is := range issues {
		codes = append(codes, is.Severity+":"+is.Code)
	}
	return codes
}

func TestValidateXKeenPorts(t *testing.T) {
	content := "#80\n443\n596:599, 8443\n\n50000-50100 # discord\n0\n70000\nabc\n1:2:3\n"
	entries, issues := ValidateXKeenPorts(content)
	if entries != 4 {
		t.Errorf("expected 4 valid entries, got %d", entries)
	}
	if len(issues) != 4 {
		t.Fatalf("expected 4 issues, got %v", issueCodes(issues))
	}
	wantLines := []int{6, 7, 8, 9}
	for i, is := range issues {
		if is.Code != "invalid_port" || is.Severity != "error" || is.Line != wantLines[i] {
			t.Errorf("issue %d: %+v", i, is)
		}
	}
}

func TestValidateXKeenIPs(t *testing.T) {
	content := "#77.88.8.8\n1.1.1.1\n2a02:6b8::feed:0ff\n8.8.8.0/24\n192.168.1.0/24\n8.8.8.8/24\n300.1.1.1\nfe80::1%eth0\n"
	entries, issues := ValidateXKeenIPs(content)
	if entries != 5 {
		t.Errorf("expected 5 valid entries, got %d", entries)
	}
	got := strings.Join(issueCodes(issues), ",")
	want := "warning:local_ip,warning:host_bits_set,error:invalid_ip,error:invalid_ip"
	if got != want {
		t.Errorf("issues = %s, want %s", got, want)
	}
}

func TestValidateXKeenJSON(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"empty object", "{\n}\n", ""},
		{"blank", "", ""},
		{"comments", "{\n  // comment\n  \"xkeen\": { /* x */ \"killswitch\": \"on\" }\n}", ""},
		{"url in string", `{"xkeen": {"policy": [{"name": "http://a//b"}]}}`, ""},
		{"syntax", "{\n  \"xkeen\": {,}\n}", "error:invalid_json"},
		{"policy not array", `{"xkeen": {"policy": {"name": "x"}}}`, "error:policy_not_array"},
		{"policy no name", `{"xkeen": {"policy": [{"port": "443"}]}}`, "error:policy_without_name"},
		{"killswitch", `{"xkeen": {"killswitch": "yes"}}`, "warning:killswitch_value"},
		{"gomemlimit", `{"xkeen": {"mihomo": {"gomemlimit_percent": 95, "gomemlimit_mb": 32}}}`, "warning:gomemlimit_percent_range,warning:gomemlimit_mb_range"},
	}
	for _, c := range cases {
		_, issues := ValidateXKeenJSON(c.content)
		if got := strings.Join(issueCodes(issues), ","); got != c.want {
			t.Errorf("%s: issues = %q, want %q", c.name, got, c.want)
		}
	}

	_, issues := ValidateXKeenJSON("{\n  \"a\": 1,\n  \"b\": ,\n}")
	if len(issues) != 1 || issues[0].Line != 3 {
		t.Errorf("expected syntax error on line 3, got %+v", issues)
	}
}

func newTestXKeenSettings(t *testing.T) (*XKeenSettingsService, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "xkeen")
	data := filepath.Join(root, "xcp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return NewXKeenSettingsService(dir, data, []string{dir}), dir
}

func TestXKeenSettingsService_SaveAndList(t *testing.T) {
	svc, dir := newTestXKeenSettings(t)

	if _, err := svc.Get("../../etc/passwd"); !errors.Is(err, ErrUnknownXKeenSetting) {
		t.Fatalf("expected unknown kind error, got %v", err)
	}

	files, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0].Kind != XKeenPortProxying || files[0].Exists {
		t.Fatalf("unexpected list: %+v", files)
	}

	if _, err := svc.Save(XKeenPortProxying, "80\n99999\n"); !errors.Is(err, ErrXKeenSettingsInvalid) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "port_proxying.lst")); !os.IsNotExist(err) {
		t.Fatal("invalid content must not be written")
	}

	f, err := svc.Save(XKeenPortProxying, "# ports\r\n80\r\n443")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "port_proxying.lst"))
	if string(data) != "# ports\n80\n443\n" || f.Entries != 2 {
		t.Fatalf("unexpected saved content %q entries %d", data, f.Entries)
	}

	// Excluded ports are ignored by XKeen while proxied ports are set.
	f, err = svc.Save(XKeenPortExclude, "22\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(issueCodes(f.Issues), ","); got != "warning:ports_exclude_overridden" {
		t.Errorf("expected conflict warning, got %s", got)
	}
}

func TestXKeenSettingsService_BackupRotation(t *testing.T) {
	svc, _ := newTestXKeenSettings(t)
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	step := 0
	svc.now = func() time.Time { step++; return base.Add(time.Duration(step) * time.Second) }

	for i := 1; i <= 8; i++ {
		if _, err := svc.Save(XKeenIPExclude, strings.Repeat("1.1.1.1\n", i)); err != nil {
			t.Fatal(err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(svc.backupDir, "ip_exclude.lst.*"))
	if len(matches) != xkeenBackupsKept {
		t.Fatalf("expected %d backups, got %d", xkeenBackupsKept, len(matches))
	}

	// Saving identical content does not create a backup.
	if _, err := svc.Save(XKeenIPExclude, strings.Repeat("1.1.1.1\n", 8)); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(svc.backupDir, "ip_exclude.lst.*"))
	if len(after) != xkeenBackupsKept || after[len(after)-1] != matches[len(matches)-1] {
		t.Fatal("unchanged save must not rotate backups")
	}
}
