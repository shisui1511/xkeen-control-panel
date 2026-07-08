package services

import (
	"testing"
)

func FuzzParseMihomoSubscriptionBlocks(f *testing.F) {
	f.Add(`proxies:
  - name: test1
    type: ss
    server: 1.1.1.1
    port: 8388
`)
	f.Add(`port: 7890
proxies:
  - name: Alpha
    type: vless
    server: a.com
`)
	f.Add(`proxies:
  - name: "🇩🇪 Germany VLESS"
    type: vless
    server: de.example.com
    port: 443
    uuid: uuid-vless
    tls: true
    servername: de.example.com
`)
	f.Fuzz(func(t *testing.T, data string) {
		ParseMihomoSubscriptionBlocks(data)
	})
}

func FuzzParseClashProxyNode(f *testing.F) {
	f.Add(`  - name: node1
    type: ss
    server: 1.2.3.4
    port: 443
`)
	f.Add(`  - name: "🇩🇪 Germany VLESS"
    type: vless
    server: de.example.com
    port: 443
    uuid: uuid-vless
    tls: true
    servername: de.example.com
`)
	f.Fuzz(func(t *testing.T, data string) {
		ParseClashProxyNode(data)
	})
}
