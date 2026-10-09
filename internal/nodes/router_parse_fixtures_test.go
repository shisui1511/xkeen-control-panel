//go:build router

package nodes

import (
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Обезличенные ответы провайдеров (testdata/inputs/real_*): тело через ParseBody
// в узлы, узлы — в оба ядра. Вердикт — код выхода `xray run -test` и `mihomo -t`
// ядер устройства; число узлов закреплено по самому файлу, эталонных выходов нет.
// Файлы синтетические: собраны по форме реальных ответов (полные конфиги Xray с
// vless Reality tcp и grpc и hysteria версии 2; Clash с теми же узлами), все
// адреса — в зоне .example; сверку обезличивания делает scripts/check-fixtures-anonymized.js.
func TestRouterParseFixtures(t *testing.T) {
	routertest.Current(t)

	cases := []struct {
		file     string
		format   string
		nodes    int
		full     bool
		kernel   bool
		xrayNo   map[string]string
		mihomoNo map[string]string
	}{
		{file: "real_xray.json", format: "xray_json", nodes: 21, full: true, kernel: true},
		{file: "real_clash.yaml", format: "clash", nodes: 10, kernel: true,
			xrayNo: map[string]string{"*/" + CodeHysteria2Xray: "x"}},
		// Схемы ссылок по одной проверены ядрами в TestRouterNodeLinks.
		{file: "real_links.b64", format: "base64", nodes: 24,
			xrayNo: map[string]string{
				"*/" + CodeHysteria2Xray: "x", "*/" + CodeUnsupportedType: "x", "*/" + CodeSSPluginXray: "x",
				"*/" + CodeXrayNoInsecure: "x", "*/" + CodeTUICXray: "x", "*/" + CodeXrayNoHTTPTransport: "x",
			},
			mihomoNo: map[string]string{"*/" + CodeUnsupportedType: "x", "*/" + CodeKCPMihomo: "x"}},
	}
	work := rtParseWork(t)
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			res, err := ParseBody(rtInput(t, c.file))
			if err != nil {
				t.Fatalf("ParseBody: %v", err)
			}
			if res.Format != c.format || len(res.Nodes) != c.nodes || len(res.Skipped) != 0 || res.FullConfig != c.full {
				t.Fatalf("format %q (ожидался %s), узлов %d (ожидалось %d), пропусков %d, full %v",
					res.Format, c.format, len(res.Nodes), c.nodes, len(res.Skipped), res.FullConfig)
			}
			obs, px := rtEmitAll(t, res.Nodes, c.xrayNo, c.mihomoNo)
			if c.xrayNo == nil && len(obs) != c.nodes || c.mihomoNo == nil && len(px) != c.nodes {
				t.Errorf("outbound Xray %d, прокси Mihomo %d из %d узлов", len(obs), len(px), c.nodes)
			}
			if !c.kernel {
				return
			}
			redact := rtRedactNodes(res.Nodes...)
			rtXrayKernel(t, c.file, work, redact, obs...)
			rtMihomoKernel(t, c.file, work, redact, px...)
		})
	}
}
