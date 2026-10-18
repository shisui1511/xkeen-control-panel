//go:build router

package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка модели узлов на устройстве. Путь каждой share-ссылки:
//
//	ParseLink → XrayOutbound/XrayFile → `xray run -test` ядра устройства;
//	ParseLink → ClashProxy/ClashFile → `mihomo -t` ядра устройства.
//
// Вердикт — код выхода ядра на целевой архитектуре, эталонных файлов в тесте нет.
// Имя подтеста — имя файла данных без расширения (схема ссылки, не имя узла).
// Невыразимое в ядре обязано вернуть *SourceError с кодом причины: такой исход
// сверяется с таблицами ниже, файл ядру не отдаётся.

const (
	rtXrayBin   = "/opt/sbin/xray"
	rtMihomoBin = "/opt/sbin/mihomo"
)

// rtNotExpressibleXray — схемы, которые Xray выразить не может, и код причины.
var rtNotExpressibleXray = map[string]string{
	"hy2_alias":   CodeHysteria2Xray,
	"hysteria2":   CodeHysteria2Xray,
	"hysteria_v1": CodeUnsupportedType,
	"ss_plugin":   CodeSSPluginXray,
	"trojan_ws":   CodeXrayNoInsecure,
	"tuic":        CodeTUICXray,
	"vmess_h2":    CodeXrayNoHTTPTransport,
}

// rtNotExpressibleMihomo — схемы, которые Mihomo выразить не может, и код причины.
var rtNotExpressibleMihomo = map[string]string{
	"hysteria_v1": CodeUnsupportedType,
	"vless_kcp":   CodeKCPMihomo,
}

// rtLinkMarks — метки известных падений по шагам схем: ключ «схема/шаг».
// Шаги: parse, xray, mihomo.
var rtLinkMarks = map[string]routertest.KnownMark{
	// mKCP header и seed удалены в стабильных Xray обеих целей (26.3.x на arm64, 26.2.x на mipsle)
	"vless_kcp/xray": routertest.Known("*", "xray-mkcp-header-seed-finalmask"),
}

func rtStep(t *testing.T, scheme, step string, ok bool, format string, args ...any) {
	t.Helper()
	routertest.Verdict(t, ok, rtLinkMarks[scheme+"/"+step], format, args...)
}

func rtKernelTimeout() time.Duration {
	switch runtime.GOARCH {
	case "mips", "mipsle":
		return 180 * time.Second
	}
	return 60 * time.Second
}

var (
	rtIPv4Re = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	rtIPv6Re = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{0,4}\b`)
)

// rtRedactor очищает вывод ядра от значений узла (адреса, ключи, имена): отчёт
// прогона не должен содержать данных ссылки.
func rtRedactor(n Node) func(string) string {
	var secrets []string
	add := func(s string) {
		if len(s) >= 4 {
			secrets = append(secrets, s)
		}
	}
	add(n.Name)
	add(n.Host)
	add(n.UUID)
	add(n.Password)
	add(n.Transport.Path)
	add(n.Transport.Host)
	add(n.Transport.ServiceName)
	if n.TLS != nil {
		add(n.TLS.SNI)
	}
	if n.Reality != nil {
		add(n.Reality.PublicKey)
		add(n.Reality.ShortID)
	}
	if n.WG != nil {
		add(n.WG.PrivateKey)
		add(n.WG.PeerPublicKey)
		add(n.WG.PreSharedKey)
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return func(s string) string {
		for _, sec := range secrets {
			s = strings.ReplaceAll(s, sec, "***")
		}
		s = rtIPv4Re.ReplaceAllStringFunc(s, func(ip string) string {
			if ip == "127.0.0.1" || ip == "0.0.0.0" {
				return ip
			}
			return "x.x.x.x"
		})
		s = rtIPv6Re.ReplaceAllString(s, "x:x")
		s = strings.Join(strings.Fields(s), " ")
		for _, marker := range []string{"panic:", "fatal error:", "Failed to start:", "level=error"} {
			if i := strings.Index(s, marker); i >= 0 {
				s = s[i:]
				break
			}
		}
		if r := []rune(s); len(r) > 600 {
			s = string(r[:400]) + " … " + string(r[len(r)-200:])
		}
		return s
	}
}

func rtRunKernel(bin string, redact func(string) string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), rtKernelTimeout())
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, bin, args...)
	return code, redact(stdout + " " + stderr)
}

// rtFirstLink читает первую непустую строку файла данных.
func rtFirstLink(t *testing.T, file string) string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("файл данных не читается: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	t.Fatal("в файле данных нет ссылки")
	return ""
}

// rtExpectCode проверяет, что ошибка эмиттера — *SourceError с ожидаемым кодом.
func rtExpectCode(t *testing.T, kernel string, err error, want string) {
	t.Helper()
	var se *SourceError
	if !errors.As(err, &se) {
		t.Errorf("%s: ожидался *SourceError с кодом %s, получено: %v", kernel, want, err)
		return
	}
	if se.Code != want {
		t.Errorf("%s: код ошибки %q, ожидался %q", kernel, se.Code, want)
	}
}

func TestRouterNodeLinks(t *testing.T) {
	dir := routertest.LinksDir(t)
	tg := routertest.Current(t)

	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("в каталоге ссылок нет файлов данных")
	}
	sort.Strings(files)

	const src = "ab12cd34"
	for _, file := range files {
		scheme := strings.TrimSuffix(filepath.Base(file), ".txt")
		t.Run(scheme, func(t *testing.T) {
			link := rtFirstLink(t, file)
			n, err := ParseLink(link)
			rtStep(t, scheme, "parse", err == nil, "ParseLink не разобрал ссылку схемы %s: %v", scheme, err)
			if err != nil {
				return
			}
			redact := rtRedactor(n)

			// Ключ и тег не зависят от имени узла.
			renamed := n
			renamed.Name = "переименован"
			if renamed.Key(src) != n.Key(src) || XrayTag(src, renamed.Key(src)) != XrayTag(src, n.Key(src)) {
				t.Errorf("переименование узла изменило ключ или тег Xray")
			}

			work := t.TempDir()
			if base := tg.WorkDir; base != "" {
				d, err := os.MkdirTemp(base, "node-")
				if err != nil {
					t.Fatalf("рабочий каталог: %v", err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(d) })
				work = d
			}

			rtCheckXray(t, scheme, n, src, work, redact)
			rtCheckMihomo(t, scheme, n, work, redact)
		})
	}
}

func rtCheckXray(t *testing.T, scheme string, n Node, src, work string, redact func(string) string) {
	t.Helper()
	tag := XrayTag(src, n.Key(src))
	ob, err := XrayOutbound(n, tag)
	if want, skip := rtNotExpressibleXray[scheme]; skip {
		rtExpectCode(t, "xray", err, want)
		t.Logf("XRAY-SKIP scheme=%s: невыразимо в Xray, код %s", scheme, want)
		return
	}
	if err != nil {
		t.Errorf("XrayOutbound отказал схеме %s: %v", scheme, err)
		return
	}

	file, err := XrayFile([]map[string]any{ob, {"protocol": "freedom", "tag": "direct"}})
	if err != nil {
		t.Fatalf("XrayFile: %v", err)
	}
	again, _ := XrayFile([]map[string]any{mustXray(t, n, tag), {"protocol": "freedom", "tag": "direct"}})
	if !bytes.Equal(file, again) {
		t.Errorf("XrayFile недетерминирован для схемы %s", scheme)
	}
	for _, banned := range []string{`"allowInsecure"`, `"transport"`, `"httpSettings"`} {
		if bytes.Contains(file, []byte(banned)) {
			t.Errorf("файл Xray схемы %s содержит запрещённый ключ %s", scheme, banned)
		}
	}
	var parsed struct {
		Outbounds []struct {
			Tag            string `json:"tag"`
			StreamSettings struct {
				Sockopt struct {
					Mark int `json:"mark"`
				} `json:"sockopt"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(file, &parsed); err != nil || len(parsed.Outbounds) == 0 {
		t.Fatalf("файл Xray не разбирается: %v", err)
	}
	if parsed.Outbounds[0].Tag != tag || parsed.Outbounds[0].StreamSettings.Sockopt.Mark != 255 {
		t.Errorf("outbound схемы %s: тег или sockopt.mark 255 не выставлены", scheme)
	}

	xrayDir := filepath.Join(work, "xray")
	if err := os.MkdirAll(xrayDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xrayDir, "outbounds.json"), file, 0o600); err != nil {
		t.Fatal(err)
	}
	code, tail := rtRunKernel(rtXrayBin, redact, "run", "-test", "-confdir", xrayDir)
	rtStep(t, scheme, "xray", code == 0, "xray run -test отверг схему %s (протокол %s): код %d: %s", scheme, n.Protocol, code, tail)
	t.Logf("xray -test: код %d", code)
}

func mustXray(t *testing.T, n Node, tag string) map[string]any {
	t.Helper()
	ob, err := XrayOutbound(n, tag)
	if err != nil {
		t.Fatalf("XrayOutbound: %v", err)
	}
	return ob
}

func rtCheckMihomo(t *testing.T, scheme string, n Node, work string, redact func(string) string) {
	t.Helper()
	px, err := ClashProxy(n, "PROXY-1")
	if want, skip := rtNotExpressibleMihomo[scheme]; skip {
		rtExpectCode(t, "mihomo", err, want)
		t.Logf("MIHOMO-SKIP scheme=%s: невыразимо в Mihomo, код %s", scheme, want)
		return
	}
	if err != nil {
		t.Errorf("ClashProxy отказал схеме %s: %v", scheme, err)
		return
	}
	if n.Protocol == "vless" {
		if _, has := px["cipher"]; has {
			t.Errorf("прокси vless схемы %s содержит cipher", scheme)
		}
	}
	if n.Protocol == "tuic" && px["type"] != "tuic" {
		t.Errorf("узел tuic выпущен как type %v", px["type"])
	}
	if n.TLS != nil && n.TLS.Enabled || n.Reality != nil {
		switch n.Protocol {
		case "vless", "vmess", "trojan":
			if fp, _ := px["client-fingerprint"].(string); fp == "" {
				t.Errorf("прокси схемы %s с TLS без client-fingerprint", scheme)
			}
		}
	}

	file, err := ClashFile([]map[string]any{px})
	if err != nil {
		t.Fatalf("ClashFile: %v", err)
	}
	px2, _ := ClashProxy(n, "PROXY-1")
	again, _ := ClashFile([]map[string]any{px2})
	if !bytes.Equal(file, again) {
		t.Errorf("ClashFile недетерминирован для схемы %s", scheme)
	}

	var section struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(file, &section); err != nil || len(section.Proxies) != 1 {
		t.Fatalf("файл Mihomo не разбирается: %v", err)
	}
	cfg := map[string]any{
		"mode":    "rule",
		"proxies": section.Proxies,
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []string{"PROXY-1"}},
		},
		"rules": []string{"MATCH,PROXY"},
	}
	cfgYAML, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("сериализация конфига Mihomo: %v", err)
	}
	mihomoDir := filepath.Join(work, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(mihomoDir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	code, tail := rtRunKernel(rtMihomoBin, redact, "-t", "-d", mihomoDir, "-f", cfgPath)
	rtStep(t, scheme, "mihomo", code == 0, "mihomo -t отверг схему %s (протокол %s): код %d: %s", scheme, n.Protocol, code, tail)
	t.Logf("mihomo -t: код %d", code)
}

// TestRouterNodeKeysUnique — узлы одного источника с одной идентичностью, но
// разным транспортом не схлопываются, а полные дубли схлопываются; порядок
// входа на ключи не влияет.
func TestRouterNodeKeysUnique(t *testing.T) {
	dir := routertest.LinksDir(t)
	base, err := ParseLink(rtFirstLink(t, filepath.Join(dir, "vless_ws.txt")))
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	second := base
	second.Name = "другой фронт"
	second.Transport.Path = base.Transport.Path + "-2"
	dup := base
	dup.Name = "копия"

	const src = "ab12cd34"
	out, collapsed := Uniq(src, []Node{base, second, dup})
	if len(out) != 2 || collapsed != 1 {
		t.Fatalf("Uniq: узлов %d, схлопнуто %d (ожидалось 2 и 1)", len(out), collapsed)
	}
	suffix := regexp.MustCompile(`~[0-9a-f]{6}$`)
	suffixed := 0
	for _, k := range out {
		if suffix.MatchString(k.Key) {
			suffixed++
		}
	}
	if suffixed != 1 {
		t.Errorf("суффикс ~6hex у %d ключей из 2, ожидался ровно один", suffixed)
	}
	rev, _ := Uniq(src, []Node{dup, second, base})
	keys := func(list []Keyed) string {
		var b strings.Builder
		for _, k := range list {
			b.WriteString(k.Key + "|" + k.Node.Transport.Path + "\n")
		}
		return b.String()
	}
	if keys(out) != keys(rev) {
		t.Errorf("перестановка входа изменила ключи узлов")
	}
}
