//go:build router

package routing

import (
	"os"
	"path/filepath"
	"testing"
)

// Входящие force-proxy* XKeen (D-12): при взятой панелью маршрутизации правило
// «эти входящие → группа» стоит первым в обоих ядрах. Настоящий слой, настоящие
// `xray run -test` и `mihomo -t` устройства в конвейере «Применить», файлы
// читаются с диска. Ядро стенда не трогается (Applier не задан), ядро тест не
// читает и не переключает — метка ядра не нужна.

// rtfpInbounds — чужой файл входящих XKeen: прозрачные redirect и tproxy и
// входящий force-proxy-redirect.
const rtfpInbounds = `{
  "inbounds": [
    {
      "tag": "redirect",
      "listen": "127.0.0.1",
      "port": 61219,
      "protocol": "dokodemo-door",
      "settings": {"network": "tcp", "followRedirect": true}
    },
    {
      "tag": "tproxy",
      "listen": "127.0.0.1",
      "port": 61220,
      "protocol": "dokodemo-door",
      "settings": {"network": "udp", "followRedirect": true},
      "streamSettings": {"sockopt": {"tproxy": "tproxy"}}
    },
    {
      "tag": "force-proxy-redirect",
      "listen": "127.0.0.1",
      "port": 61221,
      "protocol": "dokodemo-door",
      "settings": {"network": "tcp", "followRedirect": true}
    }
  ]
}
`

func TestRouterForceProxyRuleFirst(t *testing.T) {
	t.Run("xray_foreign_tag_rule_first", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		proxyG, ok := rtGroupByName(e.groups(t), defaultProxyGroupName)
		if !ok {
			t.Fatal("нет группы «Прокси» по умолчанию")
		}
		if err := os.WriteFile(filepath.Join(e.xrayDir(), "03_inbounds.json"), []byte(rtfpInbounds), 0o600); err != nil {
			t.Fatal(err)
		}
		e.takeAndApply(t)

		rules := rtxRules(t, rtxRouting(t, e.xrayDoc(t, xrayRoutingFile)))
		if len(rules) == 0 {
			t.Fatal("в routing панели нет правил")
		}
		first := rules[0]
		if !rtxEqualStrings(rtxStrings(first["inboundTag"]), []string{"force-proxy-redirect"}) || first["outboundTag"] != GroupTag(proxyG.ID) {
			t.Errorf("первое правило routing: %v, ожидалось inboundTag [force-proxy-redirect] → %s", first, GroupTag(proxyG.ID))
		}
		for i, r := range rules[1:] {
			if rtxEqualStrings(rtxStrings(r["inboundTag"]), []string{"force-proxy-redirect"}) {
				t.Errorf("правило force-proxy повторено на позиции %d: %v", i+1, r)
			}
		}
		rtxaRun(t, "run", "-test", "-confdir", e.xrayDir())
	})
}
