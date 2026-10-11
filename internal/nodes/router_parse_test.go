//go:build router

package nodes

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка разборщиков на устройстве. Вход — обезличенные тела ответов из
// testdata/inputs; вердикт — код выхода `xray run -test` и `mihomo -t` ядер
// устройства плюс свойства модели (оригинал уходит родному ядру без потерь,
// круговые конвертации сохраняют важные поля). Эталонных файлов нет.

// rtParseMarks — метки известных падений: ключ «имя проверки/шаг» (xray, mihomo).
var rtParseMarks = map[string]routertest.KnownMark{
	// outbound wireguard в Xray на mipsle: рантайм Go падает уже в `xray run -test`
	"singbox/xray":    routertest.Known("mipsle", "xray-wireguard-outbound-crash-mipsle"),
	"wg_classic/xray": routertest.Known("mipsle", "xray-wireguard-outbound-crash-mipsle"),
}

func rtInput(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(routertest.InputsDir(t), name))
	if err != nil {
		t.Fatalf("входной файл %s не читается: %v", name, err)
	}
	return data
}

// rtParseWork — рабочий каталог проверки на устройстве.
func rtParseWork(t *testing.T) string {
	t.Helper()
	tg := routertest.Current(t)
	if tg.WorkDir == "" {
		return t.TempDir()
	}
	d, err := os.MkdirTemp(tg.WorkDir, "parse-")
	if err != nil {
		t.Fatalf("рабочий каталог: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// rtRedactNodes очищает вывод ядра от значений всех перечисленных узлов.
func rtRedactNodes(nodes ...Node) func(string) string {
	fs := make([]func(string) string, 0, len(nodes))
	for _, n := range nodes {
		fs = append(fs, rtRedactor(n))
	}
	return func(s string) string {
		for _, f := range fs {
			s = f(s)
		}
		return s
	}
}

// rtXrayKernel отдаёт outbound'ы ядру Xray устройства (`xray run -test`).
func rtXrayKernel(t *testing.T, name, work string, redact func(string) string, obs ...map[string]any) {
	t.Helper()
	list := append(append([]map[string]any{}, obs...), map[string]any{"protocol": "freedom", "tag": "direct"})
	file, err := XrayFile(list)
	if err != nil {
		t.Fatalf("XrayFile: %v", err)
	}
	dir := filepath.Join(work, "xray-"+name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outbounds.json"), file, 0o600); err != nil {
		t.Fatal(err)
	}
	code, tail := rtRunKernel(rtXrayBin, redact, "run", "-test", "-confdir", dir)
	routertest.Verdict(t, code == 0, rtParseMarks[name+"/xray"], "xray run -test отверг %s: код %d: %s", name, code, tail)
}

// rtMihomoKernel отдаёт прокси ядру Mihomo устройства (`mihomo -t`).
func rtMihomoKernel(t *testing.T, name, work string, redact func(string) string, proxies ...map[string]any) {
	t.Helper()
	file, err := ClashFile(proxies)
	if err != nil {
		t.Fatalf("ClashFile: %v", err)
	}
	var section struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(file, &section); err != nil || len(section.Proxies) != len(proxies) {
		t.Fatalf("файл Mihomo не разбирается: %v", err)
	}
	names := make([]string, 0, len(proxies))
	for _, p := range proxies {
		names = append(names, strOf(p["name"]))
	}
	cfg := map[string]any{
		"mode":    "rule",
		"proxies": section.Proxies,
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": names},
		},
		"rules": []string{"MATCH,PROXY"},
	}
	cfgYAML, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("сериализация конфига Mihomo: %v", err)
	}
	dir := filepath.Join(work, "mihomo-"+name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	code, tail := rtRunKernel(rtMihomoBin, redact, "-t", "-d", dir, "-f", cfgPath)
	routertest.Verdict(t, code == 0, rtParseMarks[name+"/mihomo"], "mihomo -t отверг %s: код %d: %s", name, code, tail)
}

// rtDig идёт по вложенным картам: пустой результат — пути нет.
func rtDig(m map[string]any, path ...string) any {
	var cur any = m
	for _, k := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

// rtStrings приводит список из эмиттера (в любом виде) к []string.
func rtStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, strOf(e))
		}
		return out
	}
	return nil
}

// rtEqField сравнивает поле двух карт по текстовому виду (число и строка с
// одним значением равны: 0 из YAML и "0" из JSON — одно и то же поле).
func rtEqField(t *testing.T, what string, a, b any) {
	t.Helper()
	if strOf(a) != strOf(b) && !reflect.DeepEqual(a, b) {
		t.Errorf("%s: %v ≠ %v", what, a, b)
	}
}

func rtMustCode(t *testing.T, err error, want string) {
	t.Helper()
	var se *SourceError
	if !errors.As(err, &se) || se.Code != want {
		t.Fatalf("ожидалась ошибка с кодом %s, получено: %v", want, err)
	}
}

// TestRouterParseClashXHTTP — tracer: Clash YAML с vless xhttp → IR и Raw →
// Mihomo получает оригинал, Xray — полный xhttpSettings; круговые конвертации
// сохраняют alpn, flow, xhttp, udp, packet-encoding, encryption (SRC-05, SRC-06).
func TestRouterParseClashXHTTP(t *testing.T) {
	routertest.Current(t)
	data := rtInput(t, "clash_xhttp.yaml")
	res, err := ParseClash(data)
	if err != nil || len(res.Nodes) != 1 || len(res.Skipped) != 0 {
		t.Fatalf("ParseClash: узлов %d, пропусков %d, ошибка %v", len(res.Nodes), len(res.Skipped), err)
	}
	n := res.Nodes[0]
	if n.Origin != OriginClash || n.Format != "yaml" || n.Raw == nil || n.Protocol != "vless" {
		t.Fatalf("узел: origin %s, format %s, protocol %s, Raw %v", n.Origin, n.Format, n.Protocol, n.Raw != nil)
	}
	if n.Transport.Network != "xhttp" || n.Transport.Path != "/xh" || n.Transport.Host != "cdn.example" || n.Transport.Mode != "stream-up" {
		t.Errorf("транспорт xhttp не разобран: %+v", n.Transport)
	}
	if _, ok := n.Transport.XHTTP["x-padding-bytes"]; !ok {
		t.Errorf("в Transport.XHTTP нет x-padding-bytes: %v", n.Transport.XHTTP)
	}
	if _, ok := mapOf(n.Transport.XHTTP["reuse-settings"])["max-concurrency"]; !ok {
		t.Errorf("в Transport.XHTTP нет reuse-settings.max-concurrency: %v", n.Transport.XHTTP)
	}
	if n.TLS == nil || !reflect.DeepEqual(n.TLS.ALPN, []string{"h2", "http/1.1"}) {
		t.Errorf("TLS.ALPN: %+v", n.TLS)
	}
	if n.UDP == nil || !*n.UDP || n.PacketEncoding != "xudp" || n.Encryption != "none" {
		t.Errorf("udp/packet-encoding/encryption не перенесены: udp %v, pe %q, enc %q", n.UDP, n.PacketEncoding, n.Encryption)
	}
	if n.Text == "" || !strings.Contains(n.Text, "xhttp-opts") {
		t.Errorf("Text узла не содержит YAML прокси")
	}

	// Mihomo получает оригинал: меняется только name.
	px, err := ClashProxy(n, n.Name)
	if err != nil {
		t.Fatalf("ClashProxy: %v", err)
	}
	if !reflect.DeepEqual(px, n.Raw) {
		t.Errorf("ClashProxy родного узла отличается от Raw")
	}
	// Xray получает эмиттер: xhttpSettings полностью.
	ob, err := XrayOutbound(n, "xcp-ab12cd34-00000001")
	if err != nil {
		t.Fatalf("XrayOutbound: %v", err)
	}
	xs, _ := rtDig(ob, "streamSettings", "xhttpSettings").(map[string]any)
	for k, want := range map[string]string{"path": "/xh", "host": "cdn.example", "mode": "stream-up", "xPaddingBytes": "100-1000"} {
		if strOf(xs[k]) != want {
			t.Errorf("xhttpSettings.%s = %v, ожидалось %s", k, xs[k], want)
		}
	}
	if b, _ := xs["noGRPCHeader"].(bool); !b {
		t.Errorf("xhttpSettings.noGRPCHeader: %v", xs["noGRPCHeader"])
	}
	if strOf(rtDig(xs, "xmux", "maxConcurrency")) != "16-32" || rtDig(xs, "xmux", "maxConnections") == nil {
		t.Errorf("xhttpSettings.xmux: %v", xs["xmux"])
	}
	if got := rtStrings(rtDig(ob, "streamSettings", "tlsSettings", "alpn")); !reflect.DeepEqual(got, []string{"h2", "http/1.1"}) {
		t.Errorf("tlsSettings.alpn: %v", got)
	}

	// Clash → IR → Xray → IR → Clash.
	file, err := XrayFile([]map[string]any{ob})
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseXray(file)
	if err != nil || len(back.Nodes) != 1 {
		t.Fatalf("ParseXray результата эмиттера: %v", err)
	}
	xn := back.Nodes[0]
	cp, err := ClashProxy(xn, xn.Name)
	if err != nil {
		t.Fatalf("ClashProxy после круга: %v", err)
	}
	if _, has := cp["cipher"]; has {
		t.Errorf("круг Clash→Xray→Clash: у vless появился cipher")
	}
	if got := rtStrings(cp["alpn"]); !reflect.DeepEqual(got, []string{"h2", "http/1.1"}) {
		t.Errorf("круг Clash→Xray→Clash: alpn %v", got)
	}
	if strOf(cp["flow"]) != strOf(n.Raw["flow"]) || strOf(cp["network"]) != "xhttp" {
		t.Errorf("круг Clash→Xray→Clash: flow %q, network %q", cp["flow"], cp["network"])
	}
	if u, _ := cp["udp"].(bool); !u {
		t.Errorf("круг Clash→Xray→Clash: udp %v", cp["udp"])
	}
	src := mapOf(n.Raw["xhttp-opts"])
	dst := mapOf(cp["xhttp-opts"])
	for _, k := range []string{"path", "host", "mode", "x-padding-bytes", "no-grpc-header"} {
		rtEqField(t, "круг Clash→Xray→Clash: xhttp-opts."+k, src[k], dst[k])
	}
	for _, k := range []string{"max-concurrency", "max-connections"} {
		rtEqField(t, "круг Clash→Xray→Clash: reuse-settings."+k,
			rtDig(src, "reuse-settings", k), rtDig(dst, "reuse-settings", k))
	}

	// Clash → IR → Clash (эмиттер, не Raw): udp true и false, packet-encoding, alpn, flow, encryption.
	for _, udp := range []bool{true, false} {
		m := n
		m.UDP = &udp
		m.Flow = "xtls-rprx-vision"
		em, err := EmitClash(m)
		if err != nil {
			t.Fatalf("EmitClash: %v", err)
		}
		if u, _ := em["udp"].(bool); u != udp {
			t.Errorf("круг Clash→IR→Clash: udp %v → %v", udp, em["udp"])
		}
		if em["packet-encoding"] != "xudp" || em["encryption"] != "none" || em["flow"] != "xtls-rprx-vision" {
			t.Errorf("круг Clash→IR→Clash: packet-encoding %v, encryption %v, flow %v", em["packet-encoding"], em["encryption"], em["flow"])
		}
		if got := rtStrings(em["alpn"]); !reflect.DeepEqual(got, []string{"h2", "http/1.1"}) {
			t.Errorf("круг Clash→IR→Clash: alpn %v", got)
		}
	}

	// Вердикт ядер: Mihomo принимает оригинал, Xray — результат эмиттера.
	work := rtParseWork(t)
	redact := rtRedactNodes(n)
	rtMihomoKernel(t, "clash_xhttp", work, redact, px)
	rtXrayKernel(t, "clash_xhttp", work, redact, ob)
}

// TestRouterParseClashRoundTripXray — Xray → IR → Clash → IR → Xray сохраняет
// alpn, flow, xhttp (path, host, mode, extra-поля); vless не получает cipher.
func TestRouterParseClashRoundTripXray(t *testing.T) {
	routertest.Current(t)
	res, err := ParseXray(rtInput(t, "xray_xhttp.json"))
	if err != nil || len(res.Nodes) != 1 {
		t.Fatalf("ParseXray: узлов %d, ошибка %v", len(res.Nodes), err)
	}
	n := res.Nodes[0]
	if n.Origin != OriginXrayJSON || n.Format != "json" || n.Protocol != "vless" || n.Transport.Network != "xhttp" {
		t.Fatalf("узел: origin %s, format %s, protocol %s, network %s", n.Origin, n.Format, n.Protocol, n.Transport.Network)
	}
	// Родной узел Xray уходит оригиналом (с тегом и меткой).
	ob, err := XrayOutbound(n, "xcp-ab12cd34-00000002")
	if err != nil {
		t.Fatalf("XrayOutbound: %v", err)
	}
	if !reflect.DeepEqual(rtDig(ob, "streamSettings", "xhttpSettings"), rtDig(n.Raw, "streamSettings", "xhttpSettings")) {
		t.Errorf("родной xhttpSettings изменён при передаче Xray")
	}

	cp, err := ClashProxy(n, "Узел")
	if err != nil {
		t.Fatalf("ClashProxy: %v", err)
	}
	if _, has := cp["cipher"]; has {
		t.Errorf("vless получил cipher")
	}
	file, err := ClashFile([]map[string]any{cp})
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseClash(file)
	if err != nil || len(back.Nodes) != 1 {
		t.Fatalf("ParseClash результата эмиттера: %v", err)
	}
	ob2, err := XrayOutbound(back.Nodes[0], "xcp-ab12cd34-00000002")
	if err != nil {
		t.Fatalf("XrayOutbound после круга: %v", err)
	}
	if !reflect.DeepEqual(rtStrings(rtDig(ob2, "streamSettings", "tlsSettings", "alpn")), rtStrings(rtDig(n.Raw, "streamSettings", "tlsSettings", "alpn"))) {
		t.Errorf("круг Xray→Clash→Xray: alpn изменился")
	}
	vn, _ := listOf(rtDig(ob2, "settings", "vnext"))[0].(map[string]any)
	user, _ := listOf(vn["users"])[0].(map[string]any)
	if strOf(user["flow"]) != "" {
		t.Errorf("круг Xray→Clash→Xray: flow %v", user["flow"])
	}
	src := mapOf(rtDig(n.Raw, "streamSettings", "xhttpSettings"))
	dst := mapOf(rtDig(ob2, "streamSettings", "xhttpSettings"))
	for _, k := range []string{"path", "host", "mode", "xPaddingBytes", "noGRPCHeader"} {
		rtEqField(t, "круг Xray→Clash→Xray: xhttpSettings."+k, src[k], dst[k])
	}
	for _, k := range []string{"maxConcurrency", "maxConnections"} {
		rtEqField(t, "круг Xray→Clash→Xray: xmux."+k, rtDig(src, "xmux", k), rtDig(dst, "xmux", k))
	}

	work := rtParseWork(t)
	redact := rtRedactNodes(n)
	rtXrayKernel(t, "xray_xhttp", work, redact, ob)
	rtMihomoKernel(t, "xray_xhttp", work, redact, cp)
}

// TestRouterParseClashAnchors — merge-ключ раскрыт, short-id без кавычек остаётся
// строкой, оба узла принимаются ядрами.
func TestRouterParseClashAnchors(t *testing.T) {
	routertest.Current(t)
	res, err := ParseClash(rtInput(t, "clash_anchors.yaml"))
	if err != nil || len(res.Nodes) != 2 || len(res.Skipped) != 0 {
		t.Fatalf("ParseClash: узлов %d, пропусков %d, ошибка %v", len(res.Nodes), len(res.Skipped), err)
	}
	a, b := res.Nodes[0], res.Nodes[1]
	if _, has := a.Raw["<<"]; has || a.Raw["type"] != "vless" || a.Raw["servername"] != "cdn.example" {
		t.Errorf("merge-ключ не раскрыт в Raw: %v", a.Raw)
	}
	if a.Transport.Network != "ws" || a.Transport.Path != "/w" || a.Transport.Host != "cdn.example" {
		t.Errorf("транспорт ws из якоря: %+v", a.Transport)
	}
	if a.UDP == nil || *a.UDP {
		t.Errorf("udp: false из якоря потерян: %v", a.UDP)
	}
	if b.Reality == nil || b.Reality.ShortID != "12345678" {
		t.Errorf("short-id: %+v", b.Reality)
	}
	if sid, ok := rtDig(b.Raw, "reality-opts", "short-id").(string); !ok || sid != "12345678" {
		t.Errorf("short-id в Raw не строка: %#v", rtDig(b.Raw, "reality-opts", "short-id"))
	}
	if b.UUID != "33333333-3333-4333-8333-333333333333" {
		t.Errorf("uuid: %q", b.UUID)
	}

	work := rtParseWork(t)
	redact := rtRedactNodes(a, b)
	pa, err := ClashProxy(a, "A")
	if err != nil {
		t.Fatal(err)
	}
	pb, err := ClashProxy(b, "B")
	if err != nil {
		t.Fatal(err)
	}
	rtMihomoKernel(t, "clash_anchors", work, redact, pa, pb)
	oa, err := XrayOutbound(a, "xcp-ab12cd34-0000000a")
	if err != nil {
		t.Fatal(err)
	}
	ob, err := XrayOutbound(b, "xcp-ab12cd34-0000000b")
	if err != nil {
		t.Fatal(err)
	}
	rtXrayKernel(t, "clash_anchors", work, redact, oa, ob)
}

// TestRouterParseClashPassthrough — тип Clash, которого модель не знает, проходит
// насквозь; прокси без адреса попадает в Skipped, встроенное действие — нет.
func TestRouterParseClashPassthrough(t *testing.T) {
	routertest.Current(t)
	res, err := ParseClash(rtInput(t, "clash_passthrough.yaml"))
	if err != nil {
		t.Fatalf("ParseClash: %v", err)
	}
	if len(res.Nodes) != 2 || len(res.Skipped) != 1 {
		t.Fatalf("узлов %d (ожидалось 2), пропусков %d (ожидался 1)", len(res.Nodes), len(res.Skipped))
	}
	if res.Skipped[0].Code != CodeUnrecognized || res.Skipped[0].Line == 0 {
		t.Errorf("пропуск без адреса: %+v", res.Skipped[0])
	}
	var proxies []map[string]any
	for _, n := range res.Nodes {
		if n.Protocol != strOf(n.Raw["type"]) || n.Host == "" || n.Port == 0 {
			t.Errorf("тип %q не прошёл насквозь: protocol %q, %s:%d", n.Raw["type"], n.Protocol, n.Host, n.Port)
		}
		px, err := ClashProxy(n, n.Name)
		if err != nil {
			t.Errorf("ClashProxy узла типа %s: %v", n.Protocol, err)
			continue
		}
		proxies = append(proxies, px)
		// В Xray такой узел не выражается: причина — код, а не молчание.
		if _, xerr := XrayOutbound(n, "xcp-ab12cd34-0000000c"); xerr == nil {
			t.Errorf("тип %s ушёл в Xray", n.Protocol)
		} else {
			rtMustCode(t, xerr, CodeUnsupportedType)
		}
	}
	rtMihomoKernel(t, "clash_passthrough", rtParseWork(t), rtRedactNodes(res.Nodes...), proxies...)
}

// TestRouterParseClashErrors — ошибки YAML несут номер строки, алиас-бомба
// отклоняется (T-145-04), список прокси сверх предела — too_large.
func TestRouterParseClashErrors(t *testing.T) {
	routertest.Current(t)

	_, err := ParseClash([]byte("proxies:\n  - name: a\n    type: vless\n   server: [broken\n"))
	rtMustCode(t, err, CodeYAMLError)
	var se *SourceError
	if errors.As(err, &se) && (se.Line < 1 || se.Params["line"] == "") {
		t.Errorf("yaml_error без номера строки: %+v", se)
	}

	// Алиасы, раскрывающиеся экспоненциально.
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 10; i++ {
		prev := "*a" + string(rune('0'+i-1))
		b.WriteString("a" + string(rune('0'+i)) + ": &a" + string(rune('0'+i)) + " [")
		b.WriteString(strings.TrimSuffix(strings.Repeat(prev+", ", 9), ", "))
		b.WriteString("]\n")
	}
	b.WriteString("proxies: *a9\n")
	_, err = ParseClash([]byte(b.String()))
	rtMustCode(t, err, CodeYAMLError)

	var many strings.Builder
	many.WriteString("proxies:\n")
	for i := 0; i < maxNodes+1; i++ {
		many.WriteString("  - {name: n, type: ss, server: node1.example, port: 1, cipher: aes-128-gcm, password: p}\n")
	}
	_, err = ParseClash([]byte(many.String()))
	rtMustCode(t, err, CodeTooLarge)

	_, err = ParseClash([]byte("   \n"))
	rtMustCode(t, err, CodeEmptyResponse)
}
