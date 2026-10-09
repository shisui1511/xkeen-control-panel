//go:build router

package nodes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка форм ответа провайдера на устройстве: Xray-JSON во всех формах,
// sing-box, wg.conf/AWG и диспетчер тела подписки. Вердикт — свойства модели и
// коды ядер устройства (`xray run -test`, `mihomo -t`); эталонных файлов нет.

// rtNodeTag — тег Xray для узла с порядковым номером в проверке.
func rtNodeTag(i int) string {
	return "xcp-ab12cd34-" + strconv.FormatInt(int64(0x1000+i), 16) + "00"
}

// rtEmitAll строит outbound'ы Xray и прокси Clash для всех узлов. Узлы, которые
// ядро выразить не может, должны вернуть *SourceError с кодом из таблицы
// ожидаемых (xrayNo, mihomoNo); любой другой отказ — ошибка проверки.
func rtEmitAll(t *testing.T, nodes []Node, xrayNo, mihomoNo map[string]string) (obs, px []map[string]any) {
	t.Helper()
	for i, n := range nodes {
		name := "Узел " + strconv.Itoa(i+1)
		ob, err := XrayOutbound(n, rtNodeTag(i))
		if err != nil {
			var se *SourceError
			if !errors.As(err, &se) || xrayNo[n.Protocol+"/"+se.Code] == "" && xrayNo["*/"+se.Code] == "" {
				t.Errorf("XrayOutbound узла %d (%s): неожиданный отказ: %v", i+1, n.Protocol, err)
			}
		} else {
			obs = append(obs, ob)
		}
		p, err := ClashProxy(n, name)
		if err != nil {
			var se *SourceError
			if !errors.As(err, &se) || mihomoNo["*/"+se.Code] == "" && mihomoNo[n.Protocol+"/"+se.Code] == "" {
				t.Errorf("ClashProxy узла %d (%s): неожиданный отказ: %v", i+1, n.Protocol, err)
			}
		} else {
			px = append(px, p)
		}
	}
	return obs, px
}

func TestRouterParseXrayForms(t *testing.T) {
	routertest.Current(t)

	// Объект outbounds: служебные outbound не узлы; порт 1080 остаётся json.Number.
	res, err := ParseXray(rtInput(t, "xray_outbounds.json"))
	if err != nil || len(res.Nodes) != 2 || len(res.Skipped) != 0 || res.FullConfig || res.Format != "xray_json" {
		t.Fatalf("outbounds: узлов %d, пропусков %d, full %v, format %s, ошибка %v",
			len(res.Nodes), len(res.Skipped), res.FullConfig, res.Format, err)
	}
	socks := res.Nodes[1]
	srv, _ := listOf(rtDig(socks.Raw, "settings", "servers"))[0].(map[string]any)
	if num, ok := srv["port"].(json.Number); !ok || num.String() != "1080" || socks.Port != 1080 {
		t.Errorf("порт 1080 в Raw: %#v, в IR: %d", srv["port"], socks.Port)
	}
	if res.Nodes[0].Name != "Узел reality" || res.Nodes[0].Reality == nil || res.Nodes[0].Reality.ShortID != "0a1b2c3d" {
		t.Errorf("узел reality не разобран: %+v", res.Nodes[0].Reality)
	}
	nodes := append([]Node{}, res.Nodes...)

	// Полный конфиг.
	res, err = ParseXray(rtInput(t, "xray_full.json"))
	if err != nil || len(res.Nodes) != 1 || !res.FullConfig {
		t.Fatalf("полный конфиг: узлов %d, full %v, ошибка %v", len(res.Nodes), res.FullConfig, err)
	}
	if res.Nodes[0].Transport.Network != "ws" || res.Nodes[0].Transport.Host != "cdn.example" {
		t.Errorf("транспорт ws полного конфига: %+v", res.Nodes[0].Transport)
	}
	nodes = append(nodes, res.Nodes...)

	// Массив полных конфигов: имя узла — remarks.
	res, err = ParseXray(rtInput(t, "xray_array_configs.json"))
	if err != nil || len(res.Nodes) != 2 || !res.FullConfig {
		t.Fatalf("массив конфигов: узлов %d, full %v, ошибка %v", len(res.Nodes), res.FullConfig, err)
	}
	if res.Nodes[0].Name != "Конфиг первый" || res.Nodes[1].Name != "Конфиг второй" {
		t.Errorf("имена из remarks: %q, %q", res.Nodes[0].Name, res.Nodes[1].Name)
	}
	nodes = append(nodes, res.Nodes...)

	// Одиночный outbound.
	res, err = ParseXray(rtInput(t, "xray_xhttp.json"))
	if err != nil || len(res.Nodes) != 1 || res.FullConfig {
		t.Fatalf("одиночный outbound: узлов %d, ошибка %v", len(res.Nodes), err)
	}
	nodes = append(nodes, res.Nodes...)

	// Родные узлы уходят Xray оригиналом, Mihomo — результатом эмиттера.
	obs, px := rtEmitAll(t, nodes, nil, nil)
	if len(obs) != len(nodes) || len(px) != len(nodes) {
		t.Fatalf("эмиссия: outbound %d и прокси %d из %d узлов", len(obs), len(px), len(nodes))
	}
	work := rtParseWork(t)
	redact := rtRedactNodes(nodes...)
	rtXrayKernel(t, "xray_forms", work, redact, obs...)
	rtMihomoKernel(t, "xray_forms", work, redact, px...)
}

func TestRouterParseXrayChainRemoved(t *testing.T) {
	routertest.Current(t)
	res, err := ParseXray(rtInput(t, "xray_chain.json"))
	if err != nil || len(res.Nodes) != 1 {
		t.Fatalf("ParseXray: узлов %d, ошибка %v", len(res.Nodes), err)
	}
	n := res.Nodes[0]
	if _, has := n.Raw["proxySettings"]; has {
		t.Errorf("proxySettings остался в Raw")
	}
	so := mapOf(rtDig(n.Raw, "streamSettings", "sockopt"))
	if _, has := so["dialerProxy"]; has {
		t.Errorf("sockopt.dialerProxy остался в Raw")
	}
	if tfo, _ := so["tcpFastOpen"].(bool); !tfo {
		t.Errorf("прочие поля sockopt потеряны: %v", so)
	}
	found := false
	for _, note := range n.Notes {
		found = found || note == CodeNoteChainRemoved
	}
	if !found {
		t.Errorf("нет примечания %s: %v", CodeNoteChainRemoved, n.Notes)
	}
	if !strings.Contains(n.Text, "proxySettings") {
		t.Errorf("исходный текст узла для правки должен хранить оригинал")
	}

	ob, err := XrayOutbound(n, rtNodeTag(1))
	if err != nil {
		t.Fatalf("XrayOutbound: %v", err)
	}
	if strings.Contains(marshalIndent(ob), "hop") {
		t.Errorf("в outbound осталась ссылка на чужой тег")
	}
	rtXrayKernel(t, "xray_chain", rtParseWork(t), rtRedactNodes(n), ob)
}

func TestRouterParseXrayForbiddenKey(t *testing.T) {
	routertest.Current(t)
	res, err := ParseXray(rtInput(t, "xray_forbidden_transport.json"))
	if err != nil {
		t.Fatalf("ParseXray: %v", err)
	}
	if len(res.Nodes) != 1 || len(res.Skipped) != 1 {
		t.Fatalf("узлов %d (ожидался 1), пропусков %d (ожидался 1)", len(res.Nodes), len(res.Skipped))
	}
	sk := res.Skipped[0]
	if sk.Code != CodeXrayForbiddenKey || sk.Name != "Узел transport" || sk.Params["key"] != "transport" || len(sk.Fragment) > 80 {
		t.Errorf("пропуск: %+v", sk)
	}
	if res.Nodes[0].Name != "Узел без transport" {
		t.Errorf("уцелел не тот узел: %q", res.Nodes[0].Name)
	}
}

// TestRouterParseXrayForbiddenFieldsKept — родные узлы с полями, запрещёнными в
// Xray 26.x, не теряются: узел остаётся в Nodes с нетронутым Raw, IR заполнен из
// Raw, Xray отказывает кодом причины, Mihomo получает результат эмиттера.
func TestRouterParseXrayForbiddenFieldsKept(t *testing.T) {
	routertest.Current(t)
	data := rtInput(t, "xray_forbidden_fields.json")
	res, err := ParseXray(data)
	if err != nil || len(res.Nodes) != 4 || len(res.Skipped) != 0 {
		t.Fatalf("ParseXray: узлов %d, пропусков %d, ошибка %v", len(res.Nodes), len(res.Skipped), err)
	}
	doc, serr := decodeJSON(data)
	if serr != nil {
		t.Fatal(serr)
	}
	src := listOf(mapOf(doc)["outbounds"])
	for i, n := range res.Nodes {
		if !reflect.DeepEqual(n.Raw, src[i]) {
			t.Errorf("узел %d: Raw отличается от исходного объекта", i+1)
		}
	}
	a, b, c, d := res.Nodes[0], res.Nodes[1], res.Nodes[2], res.Nodes[3]
	if a.TLS == nil || !a.TLS.Insecure || len(a.TLS.PinSHA256) != 1 {
		t.Errorf("allowInsecure/pinnedPeerCertSha256 не в IR: %+v", a.TLS)
	}
	if b.Transport.Network != "h2" {
		t.Errorf("network h2: %q", b.Transport.Network)
	}
	if c.Transport.Network != "http" {
		t.Errorf("httpSettings без network: %q", c.Transport.Network)
	}
	if d.Transport.Headers["Host"] != "cdn.example" {
		t.Errorf("xhttpSettings.headers.Host не в IR: %v", d.Transport.Headers)
	}
	want := []string{CodeXrayNoInsecure, CodeXrayNoHTTPTransport, CodeXrayNoHTTPTransport, CodeXrayForbiddenKey}
	for i, n := range res.Nodes {
		_, err := XrayOutbound(n, rtNodeTag(i))
		var se *SourceError
		if !errors.As(err, &se) || se.Code != want[i] {
			t.Errorf("узел %d: XrayOutbound вернул %v, ожидался код %s", i+1, err, want[i])
		}
	}
	// Mihomo получает результат эмиттера: причина «не для Xray» узел не прячет.
	_, px := rtEmitAll(t, res.Nodes, map[string]string{
		"*/" + CodeXrayNoInsecure: "x", "*/" + CodeXrayNoHTTPTransport: "x", "*/" + CodeXrayForbiddenKey: "x",
	}, nil)
	if len(px) != 4 {
		t.Fatalf("прокси Mihomo: %d из 4", len(px))
	}
	rtMihomoKernel(t, "xray_forbidden_fields", rtParseWork(t), rtRedactNodes(res.Nodes...), px...)
}

func TestRouterParseSingbox(t *testing.T) {
	routertest.Current(t)
	res, err := ParseSingbox(rtInput(t, "singbox.json"))
	if err != nil || len(res.Nodes) != 8 || len(res.Skipped) != 0 || res.Format != "singbox" {
		t.Fatalf("ParseSingbox: узлов %d, пропусков %d, format %s, ошибка %v", len(res.Nodes), len(res.Skipped), res.Format, err)
	}
	for _, n := range res.Nodes {
		if n.Origin != OriginSingbox || n.Format != "json" || n.Raw != nil {
			t.Errorf("узел %q: origin %s, format %s, Raw %v", n.Name, n.Origin, n.Format, n.Raw != nil)
		}
	}
	by := map[string]Node{}
	for _, n := range res.Nodes {
		by[n.Name] = n
	}
	r := by["Узел reality"]
	if r.PacketEncoding != "xudp" || r.Reality == nil || r.Reality.PublicKey == "" || r.TLS == nil || r.TLS.Fingerprint != "chrome" {
		t.Errorf("vless reality: pe %q, reality %+v, tls %+v", r.PacketEncoding, r.Reality, r.TLS)
	}
	if w := by["Узел ws"]; w.Transport.Network != "ws" || w.Transport.Host != "cdn.example" || w.Transport.Path != "/ws" {
		t.Errorf("vmess ws: %+v", w.Transport)
	}
	if g := by["Узел grpc"]; g.Transport.Network != "grpc" || g.Transport.ServiceName != "trsvc" {
		t.Errorf("trojan grpc: %+v", g.Transport)
	}
	if s := by["Узел ss"]; s.Plugin != "obfs-local" || s.PluginOpts["obfs"] != "http" {
		t.Errorf("shadowsocks plugin: %q %v", s.Plugin, s.PluginOpts)
	}
	if h := by["Узел hy2"]; h.Hy2 == nil || h.Hy2.Obfs != "salamander" || h.Hy2.Up != "50 Mbps" {
		t.Errorf("hysteria2: %+v", h.Hy2)
	}
	if u := by["Узел tuic"]; u.TUIC == nil || u.TUIC.CongestionControl != "bbr" {
		t.Errorf("tuic: %+v", u.TUIC)
	}
	if w := by["Узел wg"]; w.WG == nil || w.WG.MTU != 1280 || len(w.WG.Address) != 1 {
		t.Errorf("wireguard: %+v", w.WG)
	}
	if x := by["Узел xhttp"]; x.Transport.Network != "xhttp" || x.Transport.Path != "/xh" || x.Transport.Host != "cdn.example" || x.Transport.Mode != "packet-up" {
		t.Errorf("защитная ветка xhttp: %+v", x.Transport)
	}

	// Оба ядра получают эмиттер; невыразимое — код причины.
	xrayNo := map[string]string{
		"shadowsocks/" + CodeSSPluginXray: "x", "hysteria2/" + CodeHysteria2Xray: "x", "tuic/" + CodeTUICXray: "x",
	}
	obs, px := rtEmitAll(t, res.Nodes, xrayNo, nil)
	if len(obs) != 5 || len(px) != 8 {
		t.Errorf("эмиссия: outbound Xray %d (ожидалось 5), прокси Mihomo %d (ожидалось 8)", len(obs), len(px))
	}
	work := rtParseWork(t)
	redact := rtRedactNodes(res.Nodes...)
	rtXrayKernel(t, "singbox", work, redact, obs...)
	rtMihomoKernel(t, "singbox", work, redact, px...)
}

func TestRouterParseWG(t *testing.T) {
	routertest.Current(t)
	work := rtParseWork(t)

	classic, err := ParseWG(string(rtInput(t, "wg_classic.conf")))
	if err != nil || len(classic.Nodes) != 1 || classic.Format != "wg" {
		t.Fatalf("классический wg.conf: узлов %d, ошибка %v", len(classic.Nodes), err)
	}
	c := classic.Nodes[0]
	if c.Origin != OriginWGConf || c.Format != "wg" || c.Host != "node10.example" || c.Port != 51820 {
		t.Errorf("узел wg: %s %s %s:%d", c.Origin, c.Format, c.Host, c.Port)
	}
	if c.WG == nil || len(c.WG.AWG) != 0 || c.WG.AWGVersion != "" {
		t.Errorf("PostUp/PostDown сделали обычный wg.conf AmneziaWG: %+v", c.WG)
	}
	if c.WG.MTU != 1280 || c.WG.PreSharedKey == "" || len(c.WG.AllowedIPs) != 2 || len(c.WG.Address) != 1 {
		t.Errorf("поля wg: %+v", c.WG)
	}
	if !strings.Contains(c.Text, "PostUp") {
		t.Errorf("Text узла не хранит исходный блок")
	}
	ob, err := XrayOutbound(c, rtNodeTag(1))
	if err != nil {
		t.Fatalf("XrayOutbound обычного wg: %v", err)
	}
	px, err := ClashProxy(c, "WG")
	if err != nil {
		t.Fatalf("ClashProxy обычного wg: %v", err)
	}
	rtXrayKernel(t, "wg_classic", work, rtRedactNodes(c), ob)
	rtMihomoKernel(t, "wg_classic", work, rtRedactNodes(c), px)

	// AmneziaWG: версия по признакам, Xray отказывает кодом, Mihomo принимает.
	for file, ver := range map[string]string{"wg_awg15.conf": "1.5", "wg_awg20.conf": "2.0", "wg_awg31.conf": "3.1"} {
		res, err := ParseWG(string(rtInput(t, file)))
		if err != nil || len(res.Nodes) != 1 {
			t.Errorf("%s: узлов %d, ошибка %v", file, len(res.Nodes), err)
			continue
		}
		n := res.Nodes[0]
		if n.WG == nil {
			t.Errorf("%s: нет данных wg", file)
			continue
		}
		if len(n.WG.AWG) == 0 || n.WG.AWGVersion != ver {
			t.Errorf("%s: AWG %v, версия %q (ожидалась %s)", file, n.WG.AWG, n.WG.AWGVersion, ver)
			continue
		}
		if _, err := XrayOutbound(n, rtNodeTag(2)); err == nil {
			t.Errorf("%s: AmneziaWG ушёл в Xray", file)
		} else {
			rtMustCode(t, err, CodeAWGXray)
		}
		p, err := ClashProxy(n, "AWG")
		if err != nil {
			t.Errorf("%s: ClashProxy: %v", file, err)
			continue
		}
		rtMihomoKernel(t, strings.TrimSuffix(file, ".conf"), work, rtRedactNodes(n), p)
	}

	// Несколько блоков [Interface] подряд.
	multi, err := ParseWG(string(rtInput(t, "wg_multi.conf")))
	if err != nil || len(multi.Nodes) != 2 || multi.Nodes[0].Host == multi.Nodes[1].Host {
		t.Errorf("wg_multi: узлов %d, ошибка %v", len(multi.Nodes), err)
	}

	// Регистр ключей не важен.
	var lowered []string
	for _, line := range strings.Split(string(rtInput(t, "wg_classic.conf")), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			line = strings.ToLower(k) + "=" + v
		}
		lowered = append(lowered, line)
	}
	if lr, err := ParseWG(strings.Join(lowered, "\n")); err != nil || len(lr.Nodes) != 1 {
		t.Errorf("ключи в нижнем регистре: узлов %d, ошибка %v", len(lr.Nodes), err)
	}

	// Нет обязательного ключа.
	for file, want := range map[string][2]string{
		"wg_missing_private.conf":  {"PrivateKey", "Interface"},
		"wg_missing_endpoint.conf": {"Endpoint", "Peer"},
	} {
		_, err := ParseWG(string(rtInput(t, file)))
		var se *SourceError
		if !errors.As(err, &se) || se.Code != CodeWGMissingKey || se.Params["key"] != want[0] || se.Params["section"] != want[1] {
			t.Errorf("%s: ожидался wg_missing_key %v, получено: %v", file, want, err)
		}
	}
}

func TestRouterParseBody(t *testing.T) {
	routertest.Current(t)

	for file, format := range map[string]string{
		"xray_outbounds.json": "xray_json",
		"xray_full.json":      "xray_json",
		"singbox.json":        "singbox",
		"clash_xhttp.yaml":    "clash",
		"wg_classic.conf":     "wg",
	} {
		res, err := ParseBody(rtInput(t, file))
		if err != nil || res.Format != format || len(res.Nodes) == 0 {
			t.Errorf("%s: format %q (ожидался %s), узлов %d, ошибка %v", file, res.Format, format, len(res.Nodes), err)
		}
	}
	// BOM перед YAML не мешает.
	if res, err := ParseBody(append([]byte("\xef\xbb\xbf"), rtInput(t, "clash_xhttp.yaml")...)); err != nil || res.Format != "clash" {
		t.Errorf("BOM перед YAML: format %q, ошибка %v", res.Format, err)
	}

	// Ссылки: все схемы из данных; вторая строка — неизвестная схема.
	files, _ := filepath.Glob(filepath.Join(routertest.LinksDir(t), "*.txt"))
	sort.Strings(files)
	var links []string
	good := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(raw), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				links = append(links, l)
				if _, err := ParseLink(l); err == nil {
					good++
				}
			}
		}
	}
	list := links[0] + "\nfoo://x\n# комментарий\n\n" + strings.Join(links[1:], "\n") + "\n"
	res, err := ParseBody([]byte(list))
	if err != nil || res.Format != "links" || len(res.Nodes) != good {
		t.Fatalf("список ссылок: format %q, узлов %d (ожидалось %d), ошибка %v", res.Format, len(res.Nodes), good, err)
	}
	var foo *Skipped
	for i := range res.Skipped {
		if res.Skipped[i].Code == CodeUnknownScheme {
			foo = &res.Skipped[i]
		}
	}
	if foo == nil || foo.Line != 2 || foo.Params["scheme"] != "foo" {
		t.Errorf("пропуск foo://x: %+v", foo)
	}

	// Те же ссылки в base64: Std, URL, Raw и с переносами строк.
	plain := strings.Join(links, "\n")
	wrapped := func(s string) string {
		var b strings.Builder
		for len(s) > 76 {
			b.WriteString(s[:76] + "\n")
			s = s[76:]
		}
		return b.String() + s
	}
	std := base64.StdEncoding.EncodeToString([]byte(plain))
	for name, body := range map[string]string{
		"Std":     std,
		"URL":     base64.URLEncoding.EncodeToString([]byte(plain)),
		"Raw":     base64.RawStdEncoding.EncodeToString([]byte(plain)),
		"RawURL":  base64.RawURLEncoding.EncodeToString([]byte(plain)),
		"Wrapped": wrapped(std),
	} {
		res, err := ParseBody([]byte(body))
		if err != nil || res.Format != "base64" || len(res.Nodes) != good {
			t.Errorf("base64 %s: format %q, узлов %d (ожидалось %d), ошибка %v", name, res.Format, len(res.Nodes), good, err)
		}
	}

	// Не подписка: код ошибки, а не молчание.
	for name, c := range map[string]struct {
		body string
		code string
		line int
	}{
		"empty":      {"", CodeEmptyResponse, 0},
		"spaces":     {" \n\t\n", CodeEmptyResponse, 0},
		"html":       {"<!DOCTYPE html>\n<html><body>Not found</body></html>", CodeHTMLResponse, 0},
		"html-lower": {"  <html lang=\"en\"><head></head></html>", CodeHTMLResponse, 0},
		"json":       {"{\n  \"outbounds\": [\n    {\"protocol\": }\n  ]\n}", CodeJSONError, 3},
		"text":       {"Subscription not found for this device", CodeUnrecognized, 0},
		"bad-base64": {"QUJD=QUJDRA==QUJDRA==", CodeBadBase64, 0},
	} {
		_, err := ParseBody([]byte(c.body))
		var se *SourceError
		if !errors.As(err, &se) || se.Code != c.code {
			t.Errorf("%s: ожидался код %s, получено: %v", name, c.code, err)
			continue
		}
		if c.line > 0 && (se.Line != c.line || se.Params["line"] != strconv.Itoa(c.line)) {
			t.Errorf("%s: номер строки %d / %q, ожидался %d", name, se.Line, se.Params["line"], c.line)
		}
	}
	// Слишком много узлов.
	big := strings.Repeat("ss://YWVzLTEyOC1nY206cA@node1.example:8388#n\n", maxNodes+1)
	if _, err := ParseBody([]byte(big)); err == nil {
		t.Errorf("%d ссылок разобраны без too_large", maxNodes+1)
	} else {
		rtMustCode(t, err, CodeTooLarge)
	}
}
