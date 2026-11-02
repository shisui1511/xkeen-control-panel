//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Список провайдеров правил при остановленном Mihomo (D-21): панель читает
// config.yaml активного профиля и файлы на диске, к API ядра не обращается.
// Часть файловая: настоящий RouteTracerService над временным каталогом без
// ядра. Часть живая: GET /api/rule-providers/info на панели устройства при
// любом активном ядре — маршрут GateData, 409 быть не должно. Метка ядра не
// нужна: исход от ядра не зависит.

const rtRpProfile = `mode: rule
rule-providers:
  rt-http-file:
    type: http
    behavior: domain
    format: text
    url: "https://rules.example.invalid/a.txt?token=probe"
    path: ./rules/a.txt
    proxy: grp-a
  rt-http-nofile:
    type: http
    behavior: ipcidr
    format: mrs
    url: "https://rules.example.invalid/b.mrs"
    path: ./rules/b.mrs
  rt-file-nofile:
    type: file
    behavior: domain
    format: text
    path: ./rules/c.txt
  rt-file-empty:
    type: file
    behavior: domain
    format: text
    path: ./rules/d.txt
  rt-inline:
    type: inline
    behavior: domain
    payload:
      - "+.example.com"
      - "full.example.org"
rules:
  - MATCH,DIRECT
`

func TestRouterRuleProvidersStopped(t *testing.T) {
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "rp-stopped-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(rtRpProfile), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "+.example.com\nfull.example.org\n"
	fileA := filepath.Join(dir, "rules", "a.txt")
	if err := os.WriteFile(fileA, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(fileA, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "d.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Без ядра и без сервиса Mihomo: список строится только по файлам.
	svc := services.NewRouteTracerService(nil, nil, dir)
	list, err := svc.ListRuleProviders()
	if err != nil {
		t.Fatalf("ListRuleProviders: %v", err)
	}
	byName := map[string]services.RuleProviderInfo{}
	for _, it := range list {
		byName[it.Name] = it
	}
	if len(byName) != 5 {
		t.Fatalf("провайдеров %d, нужно 5", len(byName))
	}

	a := byName["rt-http-file"]
	if a.Type != "http" || a.Proxy != "grp-a" || !a.FileExists ||
		a.FileSize != int64(len(body)) || a.FileMtime != mtime.Unix() {
		t.Errorf("rt-http-file: тип %q, proxy %q, файл %v, размер %d, дата %d", a.Type, a.Proxy, a.FileExists, a.FileSize, a.FileMtime)
	}
	if a.URL != "https://rules.example.invalid/a.txt" || strings.Contains(a.URL, "token") {
		t.Errorf("rt-http-file: адрес показан не очищенным: %q", a.URL)
	}
	if a.Path != filepath.Join("rules", "a.txt") {
		t.Errorf("rt-http-file: путь %q", a.Path)
	}
	if b := byName["rt-http-nofile"]; b.FileExists || b.FileSize != 0 || b.Proxy != "" || b.Format != "mrs" {
		t.Errorf("rt-http-nofile: %+v", b)
	}
	if c := byName["rt-file-nofile"]; c.FileExists || c.URL != "" || c.Type != "file" {
		t.Errorf("rt-file-nofile: %+v", c)
	}
	if d := byName["rt-file-empty"]; !d.FileExists || d.FileSize != 0 {
		t.Errorf("rt-file-empty: файл %v, размер %d", d.FileExists, d.FileSize)
	}
	if in := byName["rt-inline"]; in.Type != "inline" || in.Inline != 2 || !in.FileExists {
		t.Errorf("rt-inline: %+v", in)
	}

	// Живая панель: маршрут GateData отвечает 200 при любом активном ядре.
	e := rtNewSrc(t)
	r := e.call(http.MethodGet, "/api/rule-providers/info", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET /api/rule-providers/info при ядре %q: %d %s", tg.Core, r.Status, r.Code)
	}
	var live []map[string]any
	if err := json.Unmarshal(r.Data, &live); err != nil {
		t.Fatalf("data ответа не список: %v", err)
	}
	for _, it := range live {
		if u, _ := it["url"].(string); strings.Contains(u, "?") {
			t.Errorf("адрес провайдера %v показан с query", it["name"])
		}
	}
}
