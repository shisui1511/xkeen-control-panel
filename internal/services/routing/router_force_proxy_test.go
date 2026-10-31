//go:build router

package routing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
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

// rtfpMihomoProfile — прежний профиль Mihomo с входящим force-proxy рядом с
// обычным redir (фикстура секций 147-13 с добавленным listener).
var rtfpMihomoProfile = strings.Replace(rtmsFixture,
	"    port: 7100\n",
	"    port: 7100\n  - name: force-proxy\n    type: redir\n    port: 7101\n", 1)

// rtfpHasIssue — среди проблем есть код force_proxy_group_missing с ключом секции.
func rtfpHasIssue(issues []configlayer.BuildIssue, section string) bool {
	for _, is := range issues {
		if is.Code == issueForceProxyGroupMissing && is.Key == section {
			return true
		}
	}
	return false
}

// rtfpXrayRules генерирует файлы Xray из черновика и возвращает правила routing.
func rtfpXrayRules(t *testing.T, e *rtEnv) []map[string]any {
	t.Helper()
	files, err := e.svc.generateXray(inputOf(e.state(t)), e.installed)
	if err != nil {
		t.Fatalf("generateXray: %v", err)
	}
	for _, f := range files {
		if f.RelPath != xrayRoutingFile {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(f.Content, &doc); err != nil {
			t.Fatalf("%s не JSON: %v", xrayRoutingFile, err)
		}
		return rtxRules(t, rtxRouting(t, doc))
	}
	t.Fatalf("генератор не выдал %s", xrayRoutingFile)
	return nil
}

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

	t.Run("mihomo_in_name_first", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		proxyG, ok := rtGroupByName(e.groups(t), defaultProxyGroupName)
		if !ok {
			t.Fatal("нет группы «Прокси» по умолчанию")
		}
		rtmsWriteProfile(t, e, rtfpMihomoProfile)
		e.takeAndApply(t)

		rules := rtmStrings(e.mihomoProfile(t)["rules"])
		want := "IN-NAME,force-proxy," + proxyG.Name
		if len(rules) == 0 || rules[0] != want {
			t.Errorf("первая строка rules: %v, ожидалась %q", rules, want)
		}
		for i, r := range rules[1:] {
			if strings.HasPrefix(r, "IN-NAME,force-proxy,") {
				t.Errorf("строка force-proxy повторена на позиции %d: %q", i+1, r)
			}
		}
	})

	t.Run("xray_missing_group", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		inbounds := json.RawMessage(`[{"tag":"redirect","listen":"127.0.0.1","port":61219,"protocol":"dokodemo-door",` +
			`"settings":{"network":"tcp","followRedirect":true}},` +
			`{"tag":"force-proxy-redirect","listen":"127.0.0.1","port":61221,"protocol":"dokodemo-door",` +
			`"settings":{"network":"tcp","followRedirect":true}}]`)
		put := func(group string) {
			t.Helper()
			raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{Managed: true, Value: inbounds, ForceProxyGroup: group})
			if err != nil {
				t.Fatal(err)
			}
			e.putSection(t, configlayer.SectionXrayInbounds, raw)
		}
		hasForce := func() bool {
			for _, r := range rtfpXrayRules(t, e) {
				if rtxEqualStrings(rtxStrings(r["inboundTag"]), []string{"force-proxy-redirect"}) {
					return true
				}
			}
			return false
		}

		put("deleted-group-id")
		if !rtfpHasIssue(e.check(t), configlayer.SectionXrayInbounds) {
			t.Errorf("проверка не дала %s для секции %s: %+v", issueForceProxyGroupMissing, configlayer.SectionXrayInbounds, e.check(t))
		}
		if hasForce() {
			t.Error("генератор поставил правило force-proxy при пропавшей группе")
		}

		// Группа по умолчанию («Прокси») на месте: проблемы нет, правило стоит.
		put("")
		if rtfpHasIssue(e.check(t), configlayer.SectionXrayInbounds) {
			t.Errorf("проблема %s при группе по умолчанию: %+v", issueForceProxyGroupMissing, e.check(t))
		}
		if !hasForce() {
			t.Error("генератор не поставил правило force-proxy при группе по умолчанию")
		}
	})

	t.Run("mihomo_missing_group", func(t *testing.T) {
		e := rtNewEnv(t, rtWithKernels(rtMihomoOnly))
		e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
		rtmsWriteProfile(t, e, rtfpMihomoProfile)
		if _, err := e.svc.Take(e.rev(t)); err != nil {
			t.Fatalf("Take: %v", err)
		}
		cs := rtmsSection(t, e.state(t).Draft, "inbounds")
		cs.ForceProxyGroup = "deleted-group-id"
		raw, err := configlayer.EncodeCoreSection(cs)
		if err != nil {
			t.Fatal(err)
		}
		e.putSection(t, configlayer.SectionMihomoInbounds, raw)
		if !rtfpHasIssue(e.check(t), configlayer.SectionMihomoInbounds) {
			t.Errorf("проверка не дала %s для секции %s: %+v", issueForceProxyGroupMissing, configlayer.SectionMihomoInbounds, e.check(t))
		}
	})
}
