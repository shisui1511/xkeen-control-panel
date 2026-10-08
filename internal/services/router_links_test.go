//go:build router

package services

import (
	"context"
	"encoding/json"
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

// Проверка share-ссылок на устройстве. Путь каждой ссылки:
//
//	POST /api/outbound/parse работающего xcp → каталог Xray с этим outbound → `xray run -test`;
//	тот же outbound → тот же код панели, что делает провайдер подписки из Xray-JSON
//	(outboundsToNodes + convertSubscriptionNodesToClashYAML) → `mihomo -t`.
//
// Вердикт — код выхода ядра на целевой архитектуре, эталонных конфигов в тесте нет.
// Имя подтеста — имя файла данных без расширения (это схема ссылки, не имя узла).

// mihomoGeneratorSkips — схемы, протокол которых генератор Mihomo панели не выпускает
// (ветка «Неподдерживаемый протокол» в convertSubscriptionNodesToClashYAML). Для них
// шаг Mihomo не выполняется; список не расширяется ради зелёного прогона: новая схема
// без прокси у генератора — находка (todo + метка), а не запись сюда.
var mihomoGeneratorSkips = map[string]string{
	"socks":  "socks",
	"socks5": "socks",
	"tuic":   "tuic",
}

// rtLinkMarks — метки известных падений по шагам схем: ключ «схема/шаг».
// Шаги: parse, xray, mihomo.
var rtLinkMarks = map[string]routertest.KnownMark{
	// parse: панель не разбирает ссылку.
	"http_proxy/parse": routertest.Known("*", "share-link-http-scheme-unparsed"),
	"ss_legacy/parse":  routertest.Known("*", "share-link-ss-legacy-base64-unparsed"),
	"vmess_grpc/parse": routertest.Known("*", "share-link-vmess-numeric-port-unparsed"),

	// xray: ядро отвергает outbound, который выпустила панель.
	"hy2_alias/xray":   routertest.Known("*", "xray-hysteria2-outbound-rejected"),
	"hysteria2/xray":   routertest.Known("*", "xray-hysteria2-outbound-rejected"),
	"hysteria_v1/xray": routertest.Known("*", "xray-hysteria2-outbound-rejected"),
	"tuic/xray":        routertest.Known("*", "xray-tuic-outbound-rejected"),
	"vmess_h2/xray":    routertest.Known("*", "xray-vmess-h2-transport-removed"),
	"wireguard/xray":   routertest.Known("mipsle", "xray-wireguard-outbound-crash-mipsle"),

	// mihomo: ядро отвергает YAML, который выпустил генератор панели.
	"ss_plain/mihomo":     routertest.Known("*", "mihomo-yaml-shadowsocks-type-name"),
	"ss_plugin/mihomo":    routertest.Known("*", "mihomo-yaml-shadowsocks-type-name"),
	"ss_sip002/mihomo":    routertest.Known("*", "mihomo-yaml-shadowsocks-type-name"),
	"vmess_ws_tls/mihomo": routertest.Known("*", "mihomo-yaml-vmess-alterid-key"),
	"vmess_h2/mihomo":     routertest.Known("*", "mihomo-yaml-vmess-alterid-key"),
	"hysteria2/mihomo":    routertest.Known("*", "mihomo-yaml-hysteria2-obfs-map"),
	"hysteria_v1/mihomo":  routertest.Known("*", "mihomo-yaml-hysteria2-obfs-map"),
}

type rtParseResponse struct {
	Success bool               `json:"success"`
	Error   string             `json:"error"`
	Data    []ParseLinksResult `json:"data"`
}

// rtStep фиксирует итог шага с учётом метки известного падения схемы.
func rtStep(t *testing.T, scheme, step string, ok bool, format string, args ...any) {
	t.Helper()
	routertest.Verdict(t, ok, rtLinkMarks[scheme+"/"+step], format, args...)
}

// rtKernelTimeout — время на проверку одним ядром (на MIPS большие geo-файлы грузятся дольше).
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

// rtSafeKeys — ключи outbound, значения которых не секретны (имена протоколов,
// транспортов и шифров): их в выводе ядра оставляем, иначе текст ошибки нечитаем.
var rtSafeKeys = map[string]bool{
	"protocol": true, "network": true, "security": true, "method": true,
	"flow": true, "fingerprint": true, "encryption": true, "type": true,
	"headerType": true, "congestion": true,
}

// rtSecrets собирает строковые значения outbound (адреса, ключи, имена): их нельзя
// выводить в отчёт, поэтому вывод ядра очищается от них перед печатью.
func rtSecrets(v any, key string, out *[]string) {
	switch x := v.(type) {
	case string:
		if len(x) >= 4 && !rtSafeKeys[key] {
			*out = append(*out, x)
		}
	case map[string]any:
		for k, e := range x {
			rtSecrets(e, k, out)
		}
	case []any:
		for _, e := range x {
			rtSecrets(e, key, out)
		}
	}
}

// rtRedactor возвращает функцию очистки вывода ядра от значений outbound.
func rtRedactor(ob *Outbound) func(string) string {
	return rtRedactorValue(ob)
}

// rtRedactorValue — то же для произвольного значения (узел из конфига ядра).
func rtRedactorValue(v any) func(string) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return func(string) string { return "(вывод скрыт)" }
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return func(string) string { return "(вывод скрыт)" }
	}
	var secrets []string
	rtSecrets(generic, "", &secrets)
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
		// Самое важное в выводе ядра — причина отказа: она идёт после маркера, всё до него
		// (баннер, пути, предупреждения) для вердикта не нужно.
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

// rtRunKernel запускает проверку ядром с таймаутом и возвращает код выхода и очищенный вывод.
func rtRunKernel(bin string, redact func(string) string, args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), rtKernelTimeout())
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, bin, args...)
	return code, redact(stdout + " " + stderr)
}

func TestRouterShareLinks(t *testing.T) {
	dir := routertest.LinksDir(t)
	tg := routertest.Current(t)
	panel := routertest.NewPanel(t)

	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("в каталоге ссылок нет файлов данных")
	}
	sort.Strings(files)

	for _, file := range files {
		scheme := strings.TrimSuffix(filepath.Base(file), ".txt")
		t.Run(scheme, func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("файл данных не читается: %v", err)
			}
			link := ""
			for _, line := range strings.Split(string(raw), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					link = line
					break
				}
			}
			if link == "" {
				t.Fatal("в файле данных нет ссылки")
			}

			// 1. Разбор работающей панелью.
			var resp rtParseResponse
			status, err := panel.PostJSON("/api/outbound/parse", map[string]any{"links": []string{link}}, &resp)
			if err != nil || status != 200 {
				t.Fatalf("POST /api/outbound/parse: статус %d, ошибка %v", status, err)
			}
			parsed := len(resp.Data) == 1 && resp.Data[0].Outbound != nil && resp.Data[0].Error == ""
			rtStep(t, scheme, "parse", parsed, "панель не разобрала ссылку схемы %s (результатов: %d)", scheme, len(resp.Data))
			if !parsed {
				return
			}
			ob := resp.Data[0].Outbound
			redact := rtRedactor(ob)
			t.Logf("схема %s: протокол %s", scheme, ob.Protocol)

			work := t.TempDir()
			if base := tg.WorkDir; base != "" {
				d, err := os.MkdirTemp(base, "link-")
				if err != nil {
					t.Fatalf("рабочий каталог: %v", err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(d) })
				work = d
			}

			// 2. Xray: каталог с этим outbound.
			xrayDir := filepath.Join(work, "xray")
			if err := os.MkdirAll(xrayDir, 0o700); err != nil {
				t.Fatal(err)
			}
			cfg := map[string]any{"outbounds": []any{ob, map[string]any{"protocol": "freedom", "tag": "direct"}}}
			cfgJSON, err := json.Marshal(cfg)
			if err != nil {
				t.Fatalf("сериализация конфига Xray: %v", err)
			}
			if err := os.WriteFile(filepath.Join(xrayDir, "outbounds.json"), cfgJSON, 0o600); err != nil {
				t.Fatal(err)
			}
			code, tail := rtRunKernel(rtXrayBinary, redact, "run", "-test", "-confdir", xrayDir)
			rtStep(t, scheme, "xray", code == 0, "xray run -test отверг схему %s (протокол %s): код %d: %s", scheme, ob.Protocol, code, tail)
			t.Logf("xray -test: код %d", code)

			// 3. Mihomo: тот же путь, что у провайдера подписки из Xray-JSON.
			svc := &SubscriptionService{}
			nodes := svc.outboundsToNodes([]Outbound{*ob}, &Subscription{})
			proxiesYAML, names := svc.convertSubscriptionNodesToClashYAML(nodes)
			if reason, skip := mihomoGeneratorSkips[scheme]; skip {
				if len(names) != 0 {
					t.Errorf("список mihomoGeneratorSkips устарел — убрать схему %s: генератор выпустил прокси", scheme)
					return
				}
				t.Logf("MIHOMO-SKIP scheme=%s: генератор не выпускает протокол %s", scheme, reason)
				return
			}
			if len(names) == 0 {
				rtStep(t, scheme, "mihomo", false, "генератор Mihomo потерял узел схемы %s (протокол %s)", scheme, ob.Protocol)
				return
			}
			var section struct {
				Proxies []map[string]any `yaml:"proxies"`
			}
			if err := yaml.Unmarshal([]byte(proxiesYAML), &section); err != nil {
				rtStep(t, scheme, "mihomo", false, "генератор Mihomo выпустил неразборчивый YAML для схемы %s", scheme)
				return
			}
			mihomoCfg := map[string]any{
				"mode":    "rule",
				"proxies": section.Proxies,
				"proxy-groups": []any{
					map[string]any{"name": "PROXY", "type": "select", "proxies": names},
				},
				"rules": []string{"MATCH,PROXY"},
			}
			cfgYAML, err := yaml.Marshal(mihomoCfg)
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
			code, tail = rtRunKernel(rtMihomoBin, redact, "-t", "-d", mihomoDir, "-f", cfgPath)
			rtStep(t, scheme, "mihomo", code == 0, "mihomo -t отверг схему %s (протокол %s): код %d: %s", scheme, ob.Protocol, code, tail)
			t.Logf("mihomo -t: код %d", code)
		})
	}
}
