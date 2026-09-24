package services

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newInspectTracer(t *testing.T, providersYAML string) (*RouteTracerService, string) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "rule-providers:\n" + providersYAML
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewRouteTracerService(nil, nil, home), home
}

func TestListRuleProviders(t *testing.T) {
	tr, home := newInspectTracer(t, `  anchors-a: &d { type: http, behavior: domain, format: text }
  present: { <<: *d, url: "https://user:pw@example.com/list.txt?token=secret" }
  missing: { <<: *d, url: "https://example.com/gone.txt" }
  local: { type: inline, behavior: classical, payload: ["DOMAIN,a.test", "DOMAIN,b.test"] }
`)
	sum := md5.Sum([]byte("https://user:pw@example.com/list.txt?token=secret"))
	if err := os.WriteFile(filepath.Join(home, "rules", hex.EncodeToString(sum[:])), []byte("+.a.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := tr.ListRuleProviders()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]RuleProviderInfo{}
	for _, p := range list {
		byName[p.Name] = p
	}
	if p := byName["present"]; !p.FileExists || p.FileSize == 0 || p.URL != "https://example.com/list.txt" || !strings.HasPrefix(p.Path, "rules/") {
		t.Errorf("present: %+v", p)
	}
	if p := byName["missing"]; p.FileExists {
		t.Errorf("missing file reported as existing: %+v", p)
	}
	if p := byName["local"]; p.Type != "inline" || p.Inline != 2 || !p.FileExists {
		t.Errorf("inline: %+v", p)
	}
	if list[0].Name > list[len(list)-1].Name {
		t.Error("list is not sorted")
	}
}

func TestRuleProviderContent_SearchAndPaging(t *testing.T) {
	tr, _ := newInspectTracer(t, `  big: { type: inline, behavior: domain, payload: ["+.alpha.test", "+.beta.test", "# comment", "+.alpha2.test", "+.gamma.test"] }
`)
	ctx := context.Background()

	page, err := tr.RuleProviderContent(ctx, "big", "", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || page.Matched != 4 || strings.Join(page.Entries, ",") != "+.beta.test,+.alpha2.test" {
		t.Fatalf("paging: %+v", page)
	}

	page, err = tr.RuleProviderContent(ctx, "big", "ALPHA", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || page.Matched != 2 || len(page.Entries) != 2 {
		t.Fatalf("search: %+v", page)
	}

	if _, err := tr.RuleProviderContent(ctx, "nope", "", 0, 10); !errors.Is(err, ErrRuleProviderNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestCheckRuleProviderURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.txt" {
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	tr, _ := newInspectTracer(t, `  ok: { type: http, behavior: domain, format: text, url: "`+srv.URL+`/ok.txt" }
  dead: { type: http, behavior: domain, format: text, url: "`+srv.URL+`/dead.txt" }
`)
	// SafeHTTPClient refuses loopback addresses, so a local server shows up
	// as a transport error — enough to check the plumbing without network.
	res, err := tr.CheckRuleProviderURL(context.Background(), "dead")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || (res.StatusCode == 0 && res.Error == "") {
		t.Fatalf("dead url: %+v", res)
	}
	if _, err := tr.CheckRuleProviderURL(context.Background(), "nope"); !errors.Is(err, ErrRuleProviderNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}
