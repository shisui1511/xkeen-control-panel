//go:build router

package nodes

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка матрицы возможностей на устройстве. Вердикт — свойства модели:
// Support обязан совпадать с тем, что умеют эмиттеры, и с порогами версий из
// Caps. Ядра здесь не запускаются: их вердикт для тех же узлов даёт
// TestRouterNodeLinks и TestRouterParse*. Эталонных файлов нет.

// rtCapLink разбирает ссылку из файла данных по имени схемы.
func rtCapLink(t *testing.T, scheme string) Node {
	t.Helper()
	n, err := ParseLink(rtFirstLink(t, filepath.Join(routertest.LinksDir(t), scheme+".txt")))
	if err != nil {
		t.Fatalf("ParseLink(%s): %v", scheme, err)
	}
	return n
}

// rtCapInput разбирает входной файл и возвращает его узлы.
func rtCapInput(t *testing.T, name string) []Node {
	t.Helper()
	res, err := ParseBody(rtInput(t, name))
	if err != nil {
		t.Fatalf("ParseBody(%s): %v", name, err)
	}
	return res.Nodes
}

// rtCapAll — все узлы данных проверки: ссылки и входные тела.
func rtCapAll(t *testing.T) []Node {
	t.Helper()
	var out []Node
	links, _ := filepath.Glob(filepath.Join(routertest.LinksDir(t), "*.txt"))
	sort.Strings(links)
	for _, f := range links {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("файл данных не читается: %v", err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			if n, err := ParseLink(line); err == nil {
				out = append(out, n)
			}
		}
	}
	inputs, _ := filepath.Glob(filepath.Join(routertest.InputsDir(t), "*"))
	sort.Strings(inputs)
	for _, f := range inputs {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("входной файл не читается: %v", err)
		}
		if res, err := ParseBody(raw); err == nil {
			out = append(out, res.Nodes...)
		}
	}
	if len(out) == 0 {
		t.Fatal("в данных проверки нет узлов")
	}
	return out
}

func rtCapExpect(t *testing.T, what string, v Verdict, ok bool, code string, params map[string]string) {
	t.Helper()
	if v.OK != ok || v.Code != code {
		t.Errorf("%s: вердикт ok=%v code=%q, ожидалось ok=%v code=%q", what, v.OK, v.Code, ok, code)
		return
	}
	for k, want := range params {
		if v.Params[k] != want {
			t.Errorf("%s: параметр %s=%q, ожидалось %q", what, k, v.Params[k], want)
		}
	}
}

// TestRouterCapabilityTracer: tuic и xhttp не пропадают молча — умеющее ядро
// получает узел, другое — причину с кодом и порогом версии.
func TestRouterCapabilityTracer(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()

	tuic := rtCapLink(t, "tuic")
	rtCapExpect(t, "tuic/xray", Support(tuic, "xray", latest), false, CodeTUICXray, nil)
	rtCapExpect(t, "tuic/mihomo", Support(tuic, "mihomo", latest), true, "", nil)
	if px, err := ClashProxy(tuic, tuic.Name); err != nil || px["type"] != "tuic" {
		t.Errorf("tuic в Clash: type %v, ошибка %v", px["type"], err)
	}

	xh := rtCapLink(t, "vless_xhttp")
	if xh.Transport.Network != "xhttp" {
		t.Fatalf("данные vless_xhttp: транспорт %q", xh.Transport.Network)
	}
	rtCapExpect(t, "xhttp/xray/latest", Support(xh, "xray", latest), true, "", nil)
	rtCapExpect(t, "xhttp/mihomo/latest", Support(xh, "mihomo", latest), true, "", nil)

	old := latest
	old.MihomoXHTTP = Gate{OK: false, Min: "1.19.22"}
	rtCapExpect(t, "xhttp/mihomo/old", Support(xh, "mihomo", old), false, CodeNeedsMihomo, map[string]string{"kernel_min": "1.19.22"})
	rtCapExpect(t, "xhttp/xray/old-mihomo", Support(xh, "xray", old), true, "", nil)

	old = latest
	old.XrayXHTTP = Gate{OK: false, Min: "24.11.21"}
	rtCapExpect(t, "xhttp/xray/old", Support(xh, "xray", old), false, CodeNeedsXray, map[string]string{"kernel_min": "24.11.21"})
	rtCapExpect(t, "xhttp/mihomo/old-xray", Support(xh, "mihomo", old), true, "", nil)

	// reuse-settings (xmux) требует Mihomo не ниже 1.19.23, sessionID* — Xray не ниже 26.6.22.
	plain := xh
	plain.Transport.XHTTP = nil
	withMux := xh
	withMux.Transport.XHTTP = map[string]any{"xmux": map[string]any{"maxConcurrency": "16-32"}, "sessionIDKey": "sid"}
	old = latest
	old.MihomoXHTTPReuse = Gate{OK: false, Min: "1.19.23"}
	rtCapExpect(t, "xhttp-reuse/mihomo", Support(withMux, "mihomo", old), false, CodeNeedsMihomo, map[string]string{"kernel_min": "1.19.23"})
	rtCapExpect(t, "xhttp-reuse/mihomo-no-mux", Support(plain, "mihomo", old), true, "", nil)
	old = latest
	old.XraySessionID = Gate{OK: false, Min: "26.6.22"}
	rtCapExpect(t, "xhttp-session/xray", Support(withMux, "xray", old), false, CodeNeedsXray, map[string]string{"kernel_min": "26.6.22"})
	rtCapExpect(t, "xhttp-session/xray-plain", Support(plain, "xray", old), true, "", nil)
}

// TestRouterCapabilityConsistent: вердикт ядра совпадает с эмиттером — если
// XrayOutbound или ClashProxy отказали, Support отдаёт тот же код и параметры;
// вердикт OK возможен только при успешной эмиссии.
func TestRouterCapabilityConsistent(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()
	for i, n := range rtCapAll(t) {
		_, xerr := XrayOutbound(n, "xcp-probe")
		rtCapSame(t, i, n, "xray", Support(n, "xray", latest), xerr)
		_, merr := ClashProxy(n, "probe")
		rtCapSame(t, i, n, "mihomo", Support(n, "mihomo", latest), merr)
	}
}

func rtCapSame(t *testing.T, i int, n Node, kernel string, v Verdict, err error) {
	t.Helper()
	what := kernel + "/" + n.Protocol
	if err == nil {
		if !v.OK {
			t.Errorf("узел %d (%s): эмиттер принял, а вердикт не OK: %s", i, what, v.Code)
		}
		return
	}
	var se *SourceError
	if v.OK {
		t.Errorf("узел %d (%s): эмиттер отказал (%v), а вердикт OK", i, what, err)
		return
	}
	if !errors.As(err, &se) || v.Code != se.Code {
		t.Errorf("узел %d (%s): код вердикта %q не совпал с ошибкой эмиттера %v", i, what, v.Code, err)
		return
	}
	for k, want := range se.Params {
		if v.Params[k] != want {
			t.Errorf("узел %d (%s): параметр %s=%q, у эмиттера %q", i, what, k, v.Params[k], want)
		}
	}
}

// TestRouterVerdictJSON: ключи вердикта — контракт клиента.
func TestRouterVerdictJSON(t *testing.T) {
	routertest.Current(t)
	keys := func(v Verdict) []string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("вердикт не объект: %v", err)
		}
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	full := keys(Verdict{OK: false, Code: CodeNeedsMihomo, Params: map[string]string{"kernel_min": "1.19.22"}, Notes: []string{CodeNoteUDPDropped}})
	if strings.Join(full, ",") != "code,notes,ok,params" {
		t.Errorf("ключи вердикта с отказом: %v", full)
	}
	if got := keys(Verdict{OK: true}); strings.Join(got, ",") != "ok" {
		t.Errorf("ключи вердикта OK: %v", got)
	}
}

// rtCapCase — строка матрицы: узел и ожидаемые вердикты обоих ядер при CapsLatest.
type rtCapCase struct {
	name       string
	node       func(t *testing.T) Node
	xrayCode   string
	xrayParams map[string]string
	mihomoCode string
}

func rtCapFromLink(scheme string, mutate func(n *Node)) func(t *testing.T) Node {
	return func(t *testing.T) Node {
		n := rtCapLink(t, scheme)
		if mutate != nil {
			mutate(&n)
		}
		return n
	}
}

// rtCapFromInput берёт узел входного файла по порядковому номеру.
func rtCapFromInput(file string, idx int, mutate func(n *Node)) func(t *testing.T) Node {
	return func(t *testing.T) Node {
		nodes := rtCapInput(t, file)
		if idx >= len(nodes) {
			t.Fatalf("в %s нет узла %d", file, idx)
		}
		n := nodes[idx]
		if mutate != nil {
			mutate(&n)
		}
		return n
	}
}

// TestRouterCapabilityMatrix: протоколы, транспорты, TLS, родные узлы Xray-JSON.
func TestRouterCapabilityMatrix(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()

	cases := []rtCapCase{
		{name: "vless_ws", node: rtCapFromLink("vless_ws", nil)},
		{name: "vless_reality", node: rtCapFromLink("vless_reality", nil)},
		{name: "vless_grpc", node: rtCapFromLink("vless_grpc", nil)},
		{name: "vless_httpupgrade", node: rtCapFromLink("vless_httpupgrade", nil)},
		{name: "vless_tls_alpn", node: rtCapFromLink("vless_tls_alpn", nil)},
		{name: "vmess_ws_tls", node: rtCapFromLink("vmess_ws_tls", nil)},
		{name: "trojan_grpc", node: rtCapFromLink("trojan_grpc", nil)},
		{name: "ss_plain", node: rtCapFromLink("ss_plain", nil)},
		{name: "socks", node: rtCapFromLink("socks", nil)},
		{name: "http_proxy", node: rtCapFromLink("http_proxy", nil)},
		{name: "wireguard", node: rtCapFromLink("wireguard", nil)},
		{name: "ss_plugin", node: rtCapFromLink("ss_plugin", nil), xrayCode: CodeSSPluginXray},
		{name: "hysteria2", node: rtCapFromLink("hysteria2", nil), xrayCode: CodeHysteria2Xray},
		{name: "hy2_alias", node: rtCapFromLink("hy2_alias", nil), xrayCode: CodeHysteria2Xray},
		{name: "tuic", node: rtCapFromLink("tuic", nil), xrayCode: CodeTUICXray},
		{name: "hysteria_v1", node: rtCapFromLink("hysteria_v1", nil), xrayCode: CodeUnsupportedType, mihomoCode: CodeUnsupportedType},
		{name: "vmess_h2", node: rtCapFromLink("vmess_h2", nil), xrayCode: CodeXrayNoHTTPTransport,
			xrayParams: map[string]string{"network": "h2"}},
		{name: "insecure_without_pin", node: rtCapFromLink("trojan_ws", nil), xrayCode: CodeXrayNoInsecure},
		{name: "insecure_with_pin", node: rtCapFromLink("trojan_ws", func(n *Node) {
			n.TLS.Insecure = true
			n.TLS.PinSHA256 = []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
		})},
		{name: "kcp", node: rtCapFromLink("vless_kcp", nil), mihomoCode: CodeKCPMihomo},
		{name: "xhttp_download_settings_link", node: rtCapFromLink("vless_xhttp", func(n *Node) {
			n.Transport.XHTTP = map[string]any{"downloadSettings": map[string]any{"address": "x.example"}}
		}), xrayCode: CodeXHTTPDownloadSettings, mihomoCode: CodeXHTTPDownloadSettings},
		{name: "clash_anytls_passthrough", node: func(t *testing.T) Node {
			for _, n := range rtCapInput(t, "clash_passthrough.yaml") {
				if n.Protocol == "anytls" {
					return n
				}
			}
			t.Fatal("в clash_passthrough.yaml нет узла anytls")
			return Node{}
		}, xrayCode: CodeUnsupportedType, xrayParams: map[string]string{"type": "anytls"}},
		// Родные узлы Xray-JSON: оригинал уходит ядру как есть, поэтому запреты
		// Xray 26.x проверяет пробная эмиссия оригинала.
		{name: "raw_allow_insecure_with_pin", node: rtCapFromInput("xray_forbidden_fields.json", 0, nil), xrayCode: CodeXrayNoInsecure},
		{name: "raw_h2", node: rtCapFromInput("xray_forbidden_fields.json", 1, nil), xrayCode: CodeXrayNoHTTPTransport},
		{name: "raw_http_settings_tcp", node: rtCapFromInput("xray_forbidden_fields.json", 2, nil), xrayCode: CodeXrayNoHTTPTransport},
		{name: "raw_xhttp_headers_host", node: rtCapFromInput("xray_forbidden_fields.json", 3, nil), xrayCode: CodeXrayForbiddenKey,
			xrayParams: map[string]string{"key": "xhttpSettings.headers.Host"}},
		{name: "raw_clean_vless", node: rtCapFromInput("xray_outbounds.json", 0, nil)},
		{name: "raw_pins_array", node: func(t *testing.T) Node {
			h := hex.EncodeToString(rtCapPin("a"))
			return rtCapRawTLS(t, []any{h, h}, true)
		}, xrayCode: CodeXrayForbiddenKey, xrayParams: map[string]string{"key": "tlsSettings.pinnedPeerCertSha256"}},
		{name: "raw_pins_bad_length", node: func(t *testing.T) Node {
			return rtCapRawTLS(t, hex.EncodeToString(rtCapPin("a"))[:62], true)
		}, xrayCode: CodeXrayForbiddenKey, xrayParams: map[string]string{"key": "tlsSettings.pinnedPeerCertSha256"}},
		{name: "raw_xhttp_download_settings", node: rtCapFromInput("xray_xhttp.json", 0, func(n *Node) {
			n.Transport.XHTTP["downloadSettings"] = map[string]any{"address": "x.example"}
		}), mihomoCode: CodeXHTTPDownloadSettings},
		{name: "clash_xhttp_download_settings", node: rtCapFromInput("clash_xhttp.yaml", 0, func(n *Node) {
			n.Transport.XHTTP["download-settings"] = map[string]any{"server": "x.example"}
		}), xrayCode: CodeXHTTPDownloadSettings},
	}
	if len(cases) < 19 {
		t.Fatalf("в матрице %d строк, нужно не меньше 19", len(cases))
	}
	raw := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := c.node(t)
			if n.Origin == OriginXrayJSON {
				raw++
			}
			rtCapExpect(t, "xray", Support(n, "xray", latest), c.xrayCode == "", c.xrayCode, c.xrayParams)
			rtCapExpect(t, "mihomo", Support(n, "mihomo", latest), c.mihomoCode == "", c.mihomoCode, nil)
		})
	}
	if raw < 5 {
		t.Errorf("родных узлов Xray-JSON в матрице %d, нужно не меньше 5", raw)
	}
}

// rtCapAWG — узел AmneziaWG из входного файла wg.conf.
func rtCapAWG(t *testing.T, file, version string) Node {
	t.Helper()
	nodes := rtCapInput(t, file)
	if len(nodes) != 1 || nodes[0].WG == nil {
		t.Fatalf("%s: ожидался один узел wireguard", file)
	}
	if version == "1.0" {
		// Классический диалект: те же опции, версия задана явно (в Clash —
		// amnezia-wg-option без признаков 1.5+).
		wg := *nodes[0].WG
		wg.AWGVersion = version
		nodes[0].WG = &wg
	}
	if nodes[0].WG.AWGVersion != version {
		t.Fatalf("%s: версия AWG %q, ожидалась %q", file, nodes[0].WG.AWGVersion, version)
	}
	return nodes[0]
}

// TestRouterCapabilityAWG: пороги диалектов AmneziaWG в Mihomo; в Xray AWG нет.
func TestRouterCapabilityAWG(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()

	plain := rtCapLink(t, "wireguard")
	rtCapExpect(t, "wg/xray", Support(plain, "xray", Caps{}), true, "", nil)
	rtCapExpect(t, "wg/mihomo", Support(plain, "mihomo", Caps{}), true, "", nil)
	// wg.conf без опций AmneziaWG (ключи wg-quick не делают его AWG) — обычный wireguard.
	for _, n := range rtCapInput(t, "wg_classic.conf") {
		rtCapExpect(t, "wg_classic/xray", Support(n, "xray", Caps{}), true, "", nil)
		rtCapExpect(t, "wg_classic/mihomo", Support(n, "mihomo", Caps{}), true, "", nil)
	}

	cases := []struct {
		file, version, min string
		gate               func(c *Caps, g Gate)
	}{
		{"wg_awg15.conf", "1.0", "1.18.9", func(c *Caps, g Gate) { c.MihomoAWG = g }},
		{"wg_awg15.conf", "1.5", "1.19.13", func(c *Caps, g Gate) { c.MihomoAWG15 = g }},
		{"wg_awg20.conf", "2.0", "1.19.14", func(c *Caps, g Gate) { c.MihomoAWG20 = g }},
		{"wg_awg31.conf", "3.1", "1.19.30", func(c *Caps, g Gate) { c.MihomoAWG3 = g }},
	}
	for _, c := range cases {
		t.Run(c.version, func(t *testing.T) {
			n := rtCapAWG(t, c.file, c.version)
			rtCapExpect(t, "xray", Support(n, "xray", latest), false, CodeAWGXray, nil)
			rtCapExpect(t, "mihomo/latest", Support(n, "mihomo", latest), true, "", nil)

			caps := latest
			c.gate(&caps, Gate{OK: false, Min: c.min})
			rtCapExpect(t, "mihomo/below", Support(n, "mihomo", caps), false, CodeNeedsMihomo, map[string]string{"kernel_min": c.min})

			// Порог чужого диалекта версию не затрагивает.
			other := latest
			for _, o := range cases {
				if o.version != c.version {
					o.gate(&other, Gate{OK: false, Min: o.min})
				}
			}
			rtCapExpect(t, "mihomo/other-gates-closed", Support(n, "mihomo", other), true, "", nil)
		})
	}
}

func rtCapHasNote(v Verdict, code string) bool {
	for _, n := range v.Notes {
		if n == code {
			return true
		}
	}
	return false
}

// TestRouterCapabilityLevelB: потери уровня B не меняют вердикт, а добавляют
// примечания note_*.
func TestRouterCapabilityLevelB(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()
	off := false

	type probe struct {
		name   string
		node   Node
		kernel string
		note   string
		want   bool
	}
	ws := rtCapLink(t, "vless_ws")
	ws.UDP = &off
	pe := rtCapLink(t, "vless_ws")
	pe.PacketEncoding = "xudp"
	reality := rtCapLink(t, "vless_reality")
	reality.Reality.SpiderX = "/"
	plainReality := rtCapLink(t, "vless_reality")
	plainReality.TLS.Fingerprint = ""
	withFP := func(fp string) Node {
		n := rtCapLink(t, "vless_tls_alpn")
		n.TLS.Fingerprint = fp
		return n
	}
	clash := rtCapInput(t, "clash_xhttp.yaml")[0]
	clash.UDP = &off
	clash.PacketEncoding = "xudp"

	probes := []probe{
		{"udp/xray", ws, "xray", CodeNoteUDPDropped, true},
		{"udp/mihomo", ws, "mihomo", CodeNoteUDPDropped, false},
		{"packet-encoding/xray", pe, "xray", CodeNotePacketEncDropped, true},
		{"packet-encoding/mihomo", pe, "mihomo", CodeNotePacketEncDropped, false},
		{"spiderx/mihomo", reality, "mihomo", CodeNoteSpiderXDropped, true},
		{"spiderx/xray", reality, "xray", CodeNoteSpiderXDropped, false},
		{"fingerprint-hellochrome133/mihomo", withFP("hellochrome_133"), "mihomo", CodeNoteFingerprintDropped, true},
		{"fingerprint-hellochrome133/xray", withFP("hellochrome_133"), "xray", CodeNoteFingerprintDropped, false},
		{"fingerprint-unknown/xray", withFP("weird"), "xray", CodeNoteFingerprintDropped, true},
		{"fingerprint-chrome120/xray", withFP("chrome120"), "xray", CodeNoteFingerprintDropped, false},
		{"fingerprint-chrome120/mihomo", withFP("chrome120"), "mihomo", CodeNoteFingerprintDropped, false},
		{"reality-without-fingerprint/mihomo", plainReality, "mihomo", CodeNoteFingerprintDropped, false},
		// Родное ядро получает оригинал без потерь: примечаний нет.
		{"native-clash/mihomo-udp", clash, "mihomo", CodeNoteUDPDropped, false},
		{"native-clash/xray-udp", clash, "xray", CodeNoteUDPDropped, true},
		{"native-clash/xray-packet-encoding", clash, "xray", CodeNotePacketEncDropped, true},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			v := Support(p.node, p.kernel, latest)
			if !v.OK {
				t.Fatalf("вердикт не OK (%s): потери уровня B не должны менять вердикт", v.Code)
			}
			if got := rtCapHasNote(v, p.note); got != p.want {
				t.Errorf("примечание %s: есть %v, ожидалось %v (все: %v)", p.note, got, p.want, v.Notes)
			}
		})
	}
}

// rtCapPin — SHA-256 постоянной строки теста: реальных сертификатов в данных нет.
func rtCapPin(seed string) []byte {
	sum := sha256.Sum256([]byte("xcp-router-test-pin/" + seed))
	return sum[:]
}

// rtCapColon — hex с двоеточиями в верхнем регистре (так пишут отпечаток в ссылках).
func rtCapColon(h string) string {
	parts := make([]string, 0, len(h)/2)
	for i := 0; i+1 < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.ToUpper(strings.Join(parts, ":"))
}

// rtCapRawTLS — родной узел Xray-JSON с TLS: первый узел xray_outbounds.json с
// tlsSettings, а если такого нет, первый vless с переписанным на TLS транспортом.
// Закрепление записывается в Raw как есть (hasPins=false — ключа нет).
func rtCapRawTLS(t *testing.T, pins any, hasPins bool) Node {
	t.Helper()
	nodes := rtCapInput(t, "xray_outbounds.json")
	var base *Node
	for i := range nodes {
		ss, _ := nodes[i].Raw["streamSettings"].(map[string]any)
		if _, ok := ss["tlsSettings"]; ok && nodes[i].Origin == OriginXrayJSON {
			base = &nodes[i]
			break
		}
	}
	if base == nil {
		for i := range nodes {
			if nodes[i].Origin == OriginXrayJSON && nodes[i].Raw["protocol"] == "vless" {
				base = &nodes[i]
				break
			}
		}
	}
	if base == nil {
		t.Fatal("в xray_outbounds.json нет родного узла vless")
	}
	n := *base
	n.Raw = deepCopyMap(base.Raw)
	ss, _ := n.Raw["streamSettings"].(map[string]any)
	if ss == nil {
		ss = map[string]any{"network": "tcp"}
		n.Raw["streamSettings"] = ss
	}
	ts, _ := ss["tlsSettings"].(map[string]any)
	if ts == nil {
		delete(ss, "realitySettings")
		ss["security"] = "tls"
		ts = map[string]any{"serverName": orDefault(n.Host, "node.example")}
		ss["tlsSettings"] = ts
	}
	delete(ts, "pinnedPeerCertSha256")
	if hasPins {
		ts["pinnedPeerCertSha256"] = pins
	}
	return n
}

// TestRouterCapabilityRawPins: вердикт матрицы для каждой формы
// tlsSettings.pinnedPeerCertSha256 родного узла Xray-JSON совпадает с кодом
// выхода `xray run -test` на устройстве. Отвергнутая форма даёт
// xray_forbidden_key и в файл Xray не попадает.
func TestRouterCapabilityRawPins(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()
	h1 := hex.EncodeToString(rtCapPin("a"))
	h2 := hex.EncodeToString(rtCapPin("b"))
	colon := rtCapColon
	forms := []struct {
		name string
		pins any
	}{
		{"single_lower", h1},
		{"two_comma", h1 + "," + h2},
		{"colons_upper", colon(h1)},
		{"spaces_and_empty_parts", " " + h1 + " , ," + h2 + ","},
		{"empty_string", ""},
		{"array_two", []any{h1, h2}},
		{"short_62", h1[:62]},
		{"long_66", h1 + "ab"},
		{"not_hex_64", strings.Repeat("zz", 32)},
		{"number", json.Number("12345")},
	}
	for i, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			n := rtCapRawTLS(t, f.pins, true)
			v := Support(n, "xray", latest)

			ob := deepCopyMap(n.Raw)
			ob["tag"] = rtNodeTag(i)
			setSockoptMark(ob)
			file, err := XrayFile([]map[string]any{ob, {"protocol": "freedom", "tag": "direct"}})
			if err != nil {
				t.Fatalf("XrayFile: %v", err)
			}
			dir := filepath.Join(rtParseWork(t), "xray-rawpins-"+f.name)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "outbounds.json"), file, 0o600); err != nil {
				t.Fatal(err)
			}
			code, tail := rtRunKernel(rtXrayBin, rtRedactNodes(n), "run", "-test", "-confdir", dir)

			if v.OK != (code == 0) {
				t.Fatalf("форма %s: матрица ok=%v (code %q), xray run -test вернул %d: %s", f.name, v.OK, v.Code, code, tail)
			}
			if !v.OK {
				rtCapExpect(t, f.name, v, false, CodeXrayForbiddenKey, map[string]string{"key": "tlsSettings.pinnedPeerCertSha256"})
				if _, err := XrayOutbound(n, rtNodeTag(i)); err == nil {
					t.Errorf("форма %s: XrayOutbound пропустил отвергнутую форму в файл", f.name)
				}
			}
		})
	}
}

// rtCapSingbox разбирает trojan из JSON sing-box с заданным блоком tls.
func rtCapSingbox(t *testing.T, tls map[string]any) Node {
	t.Helper()
	body, err := json.Marshal(map[string]any{"outbounds": []any{map[string]any{
		"type": "trojan", "tag": "pin-probe", "server": "node.example", "server_port": 443,
		"password": "probe-pass", "tls": tls,
	}}})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	res, err := ParseBody(body)
	if err != nil || len(res.Nodes) != 1 {
		t.Fatalf("ParseBody sing-box: узлов %d, ошибка %v", len(res.Nodes), err)
	}
	return res.Nodes[0]
}

// rtCapRefused: эмиттеры отказывают узлу с кодом pin_not_expressible, а вердикты
// обоих ядер совпадают с этим отказом: в файл ядра ничего не уходит.
func rtCapRefused(t *testing.T, n Node, code string) {
	t.Helper()
	latest := CapsLatest()
	rtCapExpect(t, "xray", Support(n, "xray", latest), false, code, nil)
	rtCapExpect(t, "mihomo", Support(n, "mihomo", latest), false, code, nil)
	_, xerr := XrayOutbound(n, rtNodeTag(0))
	rtCapSame(t, 0, n, "xray", Support(n, "xray", latest), xerr)
	_, merr := ClashProxy(n, "pin-probe")
	rtCapSame(t, 0, n, "mihomo", Support(n, "mihomo", latest), merr)
	if xerr == nil || merr == nil {
		t.Errorf("узел без выражаемого закрепления при отключённой проверке ушёл в ядро: xray=%v mihomo=%v", xerr, merr)
	}
}

// rtCapKernels отдаёт узел обоим ядрам устройства и возвращает outbound Xray и
// прокси Mihomo, которые ядра приняли.
func rtCapKernels(t *testing.T, name string, n Node) (map[string]any, map[string]any) {
	t.Helper()
	ob, err := XrayOutbound(n, rtNodeTag(0))
	if err != nil {
		t.Fatalf("XrayOutbound: %v", err)
	}
	px, err := ClashProxy(n, "pin-probe")
	if err != nil {
		t.Fatalf("ClashProxy: %v", err)
	}
	work := rtParseWork(t)
	rtXrayKernel(t, name, work, rtRedactNodes(n), ob)
	rtMihomoKernel(t, name, work, rtRedactNodes(n), px)
	return ob, px
}

// TestRouterCapabilityPins: закрепление сертификата переносится в оба ядра без
// потерь, а закрепление, которое ядро не выражает, не теряется молча: при
// отключённой проверке сертификата узел отвергается с причиной, без неё идёт с
// примечанием note_pin_dropped. Вердикт ядер — xray run -test и mihomo -t.
func TestRouterCapabilityPins(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()
	certRaw := rtCapPin("cert")
	certHex := hex.EncodeToString(certRaw)
	certB64 := base64.StdEncoding.EncodeToString(certRaw)
	keyB64 := base64.StdEncoding.EncodeToString(rtCapPin("spki"))

	t.Run("pubkey_insecure", func(t *testing.T) {
		rtCapRefused(t, rtCapSingbox(t, map[string]any{
			"enabled": true, "server_name": "node.example", "insecure": true,
			"certificate_public_key_sha256": []any{keyB64},
		}), CodePinNotExpressible)
	})

	t.Run("pubkey_secure", func(t *testing.T) {
		n := rtCapSingbox(t, map[string]any{
			"enabled": true, "server_name": "node.example",
			"certificate_public_key_sha256": []any{keyB64},
		})
		if len(n.TLS.PinPubKeySHA256) != 1 || len(n.TLS.PinSHA256) != 0 {
			t.Fatalf("закрепление открытого ключа в IR: pubkey=%v cert=%v", n.TLS.PinPubKeySHA256, n.TLS.PinSHA256)
		}
		for _, k := range []string{"xray", "mihomo"} {
			v := Support(n, k, latest)
			if !v.OK || !rtCapHasNote(v, CodeNotePinDropped) {
				t.Errorf("%s: вердикт %+v, ожидался OK с примечанием %s", k, v, CodeNotePinDropped)
			}
		}
		ob, px := rtCapKernels(t, "pins-pubkey-secure", n)
		if _, has := px["skip-cert-verify"]; has {
			t.Errorf("прокси Mihomo содержит skip-cert-verify при включённой проверке")
		}
		if _, has := px["fingerprint"]; has {
			t.Errorf("прокси Mihomo содержит fingerprint, хотя закрепление открытого ключа не выражается")
		}
		if rtDig(ob, "streamSettings", "tlsSettings", "pinnedPeerCertSha256") != nil {
			t.Errorf("в файле Xray есть pinnedPeerCertSha256 для закрепления открытого ключа")
		}
	})

	t.Run("cert_sha256_insecure", func(t *testing.T) {
		n := rtCapSingbox(t, map[string]any{
			"enabled": true, "server_name": "node.example", "insecure": true,
			"certificate_sha256": []any{certB64},
		})
		if len(n.TLS.PinSHA256) != 1 || n.TLS.PinSHA256[0] != certHex {
			t.Fatalf("certificate_sha256 не переведён в hex: %v", n.TLS.PinSHA256)
		}
		for _, k := range []string{"xray", "mihomo"} {
			v := Support(n, k, latest)
			if !v.OK || rtCapHasNote(v, CodeNotePinDropped) {
				t.Errorf("%s: вердикт %+v, ожидался OK без примечания %s", k, v, CodeNotePinDropped)
			}
		}
		ob, px := rtCapKernels(t, "pins-cert-insecure", n)
		if got := rtDig(ob, "streamSettings", "tlsSettings", "pinnedPeerCertSha256"); got != certHex {
			t.Errorf("pinnedPeerCertSha256 в файле Xray не равен hex закрепления: %v", got)
		}
		if px["fingerprint"] != certHex {
			t.Errorf("fingerprint прокси Mihomo не равен hex закрепления: %v", px["fingerprint"])
		}
	})

	t.Run("link_colons_upper", func(t *testing.T) {
		n := rtCapLink(t, "trojan_ws")
		n.TLS.Insecure = true
		n.TLS.PinSHA256 = []string{rtCapColon(certHex)}
		for _, k := range []string{"xray", "mihomo"} {
			if v := Support(n, k, latest); !v.OK || rtCapHasNote(v, CodeNotePinDropped) {
				t.Errorf("%s: вердикт %+v, ожидался OK без примечания", k, v)
			}
		}
		ob, px := rtCapKernels(t, "pins-link-colons", n)
		if got := rtDig(ob, "streamSettings", "tlsSettings", "pinnedPeerCertSha256"); got != certHex {
			t.Errorf("pinnedPeerCertSha256 в файле Xray: %v, ожидался hex в нижнем регистре без двоеточий", got)
		}
		if px["fingerprint"] != certHex {
			t.Errorf("fingerprint прокси Mihomo: %v, ожидался тот же hex", px["fingerprint"])
		}
	})

	t.Run("link_short_pin_insecure", func(t *testing.T) {
		n := rtCapLink(t, "trojan_ws")
		n.TLS.Insecure = true
		n.TLS.PinSHA256 = []string{certHex[:62]}
		rtCapRefused(t, n, CodePinNotExpressible)
	})

	t.Run("two_pins_mihomo_note", func(t *testing.T) {
		n := rtCapLink(t, "trojan_ws")
		n.TLS.Insecure = false
		n.TLS.PinSHA256 = []string{certHex, hex.EncodeToString(rtCapPin("other"))}
		if v := Support(n, "xray", latest); !v.OK || rtCapHasNote(v, CodeNotePinDropped) {
			t.Errorf("xray: вердикт %+v, ожидался OK без примечания (пишутся оба хеша)", v)
		}
		if v := Support(n, "mihomo", latest); !v.OK || !rtCapHasNote(v, CodeNotePinDropped) {
			t.Errorf("mihomo: вердикт %+v, ожидался OK с примечанием (пишется первый хеш)", v)
		}
		rtCapKernels(t, "pins-two", n)
	})

	t.Run("hysteria2_link", func(t *testing.T) {
		n := rtCapLink(t, "hysteria2")
		px, err := ClashProxy(n, "pin-probe")
		if err != nil {
			t.Fatalf("ClashProxy: %v", err)
		}
		fp, _ := px["fingerprint"].(string)
		if b, err := hex.DecodeString(fp); err != nil || len(b) != 32 || fp != strings.ToLower(fp) {
			t.Errorf("fingerprint прокси Mihomo не 64 hex в нижнем регистре: %q", fp)
		}
		rtMihomoKernel(t, "pins-hysteria2", rtParseWork(t), rtRedactNodes(n), px)
	})

	t.Run("http_tls_insecure", func(t *testing.T) {
		n := rtCapLink(t, "http_proxy")
		n.TLS = &TLS{Enabled: true, SNI: "node.example", Insecure: true, PinSHA256: []string{certHex}}
		px, err := ClashProxy(n, "pin-probe")
		if err != nil {
			t.Fatalf("ClashProxy: %v", err)
		}
		if px["fingerprint"] != certHex {
			t.Errorf("fingerprint http-прокси Mihomo: %v, ожидался hex закрепления", px["fingerprint"])
		}
		rtMihomoKernel(t, "pins-http-tls", rtParseWork(t), rtRedactNodes(n), px)
		n.TLS.PinSHA256 = nil
		n.TLS.PinPubKeySHA256 = []string{keyB64}
		if _, err := ClashProxy(n, "pin-probe"); err == nil {
			t.Errorf("http-прокси с отключённой проверкой и закреплением открытого ключа ушёл в Mihomo")
		}
	})
}

// TestRouterCapabilityPinnedInsecure: узел с «insecure» и закреплённым
// сертификатом уходит в Xray без allowInsecure (ключ удалён в Xray 26.x), с
// pinnedPeerCertSha256; вердикт ядра устройства — код выхода `xray run -test`.
func TestRouterCapabilityPinnedInsecure(t *testing.T) {
	routertest.Current(t)
	n := rtCapLink(t, "trojan_ws")
	n.TLS.Insecure = true
	n.TLS.PinSHA256 = []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	ob, err := XrayOutbound(n, rtNodeTag(0))
	if err != nil {
		t.Fatalf("XrayOutbound отказал узлу с закреплённым сертификатом: %v", err)
	}
	file, err := XrayFile([]map[string]any{ob})
	if err != nil {
		t.Fatalf("XrayFile: %v", err)
	}
	for _, banned := range []string{`"allowInsecure"`} {
		if strings.Contains(string(file), banned) {
			t.Errorf("файл Xray содержит запрещённый ключ %s", banned)
		}
	}
	if !strings.Contains(string(file), `"pinnedPeerCertSha256"`) {
		t.Errorf("в файле Xray нет pinnedPeerCertSha256")
	}
	rtXrayKernel(t, "pinned-insecure", rtParseWork(t), rtRedactNodes(n), ob)
}
