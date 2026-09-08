package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterTransport(t *testing.T) {
	svc := &SubscriptionService{}
	sub := &Subscription{FilterTransport: "ws"}
	outbounds := []Outbound{
		{Tag: "ws-proxy", Protocol: "vmess", StreamSettings: map[string]interface{}{"network": "ws"}},
		{Tag: "tcp-proxy", Protocol: "vmess", StreamSettings: map[string]interface{}{"network": "tcp"}},
		{Tag: "no-stream", Protocol: "trojan"},
	}
	result := svc.applyFilters(outbounds, sub)
	if len(result) != 1 {
		t.Errorf("expected 1 outbound after FilterTransport=ws, got %d", len(result))
	}
	if len(result) > 0 && result[0].Tag != "ws-proxy" {
		t.Errorf("expected 'ws-proxy', got %q", result[0].Tag)
	}
}

func TestTagDeduplication(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	sub := &Subscription{ID: "test", Name: "test"}

	outbounds := []Outbound{
		{Tag: "server", Protocol: "vmess"},
		{Tag: "server", Protocol: "vmess"},
		{Tag: "server", Protocol: "vmess"},
	}

	path := svc.getFragmentPath(sub)
	if _, err := svc.writeFragment(path, outbounds, sub); err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatal(err)
	}
	result := wrapper.Outbounds

	if len(result) != 3 {
		t.Fatalf("expected 3 outbounds, got %d", len(result))
	}
	if result[0].Tag != "server" {
		t.Errorf("expected first tag=server, got %q", result[0].Tag)
	}
	if result[1].Tag != "server-1" {
		t.Errorf("expected second tag=server-1, got %q", result[1].Tag)
	}
	if result[2].Tag != "server-2" {
		t.Errorf("expected third tag=server-2, got %q", result[2].Tag)
	}
}

func TestSubscriptionProxyCount(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	sub := Subscription{
		Name:    "Proxy Count Test",
		URL:     "https://example.com/sub",
		Enabled: true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	id := svc.List()[0].ID

	// Before refresh (no fragment file)
	got := svc.Get(id)
	if got.ProxyCount != 0 {
		t.Errorf("expected ProxyCount 0, got %d", got.ProxyCount)
	}

	// Create fragment file manually to simulate refresh
	path := svc.getFragmentPath(got)
	outbounds := []Outbound{
		{Tag: "proxy1", Protocol: "vmess"},
		{Tag: "proxy2", Protocol: "vless"},
	}
	if _, err := svc.writeFragment(path, outbounds, got); err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	// Read again
	got = svc.Get(id)
	if got.ProxyCount != 2 {
		t.Errorf("expected ProxyCount 2, got %d", got.ProxyCount)
	}

	subs := svc.List()
	if subs[0].ProxyCount != 2 {
		t.Errorf("expected ProxyCount 2 in List(), got %d", subs[0].ProxyCount)
	}
}

func TestRoutingFragmentAutoMode(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "xray")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	sub := &Subscription{ID: "sub1", TagPrefix: "pfx"}
	svc := &SubscriptionService{dataDir: tmp, configDir: configDir}

	routingPath := svc.getRoutingFragmentPath(sub)
	if err := svc.writeRoutingFragment(routingPath, sub, []string{"pfx-node1", "pfx-node2"}); err != nil {
		t.Fatalf("writeRoutingFragment: %v", err)
	}

	data, err := os.ReadFile(routingPath)
	if err != nil {
		t.Fatalf("routing fragment not written: %v", err)
	}

	var frag struct {
		Routing struct {
			Balancers []struct {
				Tag      string   `json:"tag"`
				Selector []string `json:"selector"`
			} `json:"balancers"`
			Rules []struct {
				BalancerTag string   `json:"balancerTag"`
				Domain      []string `json:"domain"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &frag); err != nil {
		t.Fatalf("invalid routing JSON: %v", err)
	}

	if len(frag.Routing.Balancers) != 1 {
		t.Fatalf("expected 1 balancer, got %d", len(frag.Routing.Balancers))
	}
	if frag.Routing.Balancers[0].Selector[0] != "pfx-" {
		t.Errorf("selector: %v", frag.Routing.Balancers[0].Selector)
	}
	if len(frag.Routing.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(frag.Routing.Rules))
	}
	if frag.Routing.Rules[0].BalancerTag != "sub1-balancer" {
		t.Errorf("balancerTag: %v", frag.Routing.Rules[0].BalancerTag)
	}

	hasDomain := false
	for _, d := range frag.Routing.Rules[0].Domain {
		if d == "geosite:geolocation-!cn" {
			hasDomain = true
		}
	}
	if !hasDomain {
		t.Errorf("missing geosite:geolocation-!cn in rule domains: %v", frag.Routing.Rules[0].Domain)
	}

	// Without TagPrefix — should use direct outboundTag
	sub2 := &Subscription{ID: "sub2", TagPrefix: ""}
	path2 := svc.getRoutingFragmentPath(sub2)
	if err := svc.writeRoutingFragment(path2, sub2, []string{"node1"}); err != nil {
		t.Fatalf("writeRoutingFragment no-prefix: %v", err)
	}
	data2, _ := os.ReadFile(path2)
	var frag2 struct {
		Routing struct {
			Rules []struct {
				OutboundTag string `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	json.Unmarshal(data2, &frag2)
	if len(frag2.Routing.Rules) == 0 || frag2.Routing.Rules[0].OutboundTag != "node1" {
		t.Errorf("outboundTag: %v", frag2.Routing.Rules)
	}
}

func TestXrayConfigurationHardening(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	sub := &Subscription{ID: "test", Name: "test"}

	outbounds := []Outbound{
		{Tag: "vless-node", Protocol: "vless"},
		{Tag: "hy2-node", Protocol: "hysteria2"},
		{Tag: "tuic-node", Protocol: "tuic"},
		{Tag: "vmess-node", Protocol: "vmess"},
	}

	path := svc.getFragmentPath(sub)
	nodes, err := svc.writeFragment(path, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	if len(nodes) != 4 {
		t.Errorf("expected 4 returned nodes, got %d", len(nodes))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatal(err)
	}

	if len(wrapper.Outbounds) != 2 {
		t.Errorf("expected only 2 outbounds written to file, got %d", len(wrapper.Outbounds))
	}

	for _, ob := range wrapper.Outbounds {
		if ob.Protocol == "hysteria2" || ob.Protocol == "tuic" {
			t.Errorf("unsupported protocol %s was written to file", ob.Protocol)
		}
	}
}

func TestSubscriptionService_ClashYAMLToXrayOutbounds(t *testing.T) {
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	_ = os.MkdirAll(xrayDir, 0755)

	yamlContent := `proxies:
  - name: "🇩🇪 Germany VLESS"
    type: vless
    server: de.example.com
    port: 443
    uuid: uuid-vless
    tls: true
    servername: de.example.com
  - name: "🇳🇱 Netherlands VMess"
    type: vmess
    server: nl.example.com
    port: 8080
    uuid: uuid-vmess
    alterId: 0
    network: ws
    ws-opts:
      path: /vmessws
  - name: "🇸🇬 Singapore Shadowsocks"
    type: ss
    server: sg.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-pass
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer srv.Close()

	svc := NewSubscriptionService(tmp, xrayDir, tmp)
	svc.httpClient = srv.Client()

	sub := Subscription{
		ID:           "clash-to-xray-test",
		Name:         "Clash to Xray Test",
		URL:          srv.URL,
		EnableXray:   true,
		EnableMihomo: false,
		Enabled:      true,
	}
	_ = svc.Add(&sub)

	err := svc.Refresh("clash-to-xray-test")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	fragmentPath := svc.getFragmentPath(&sub)
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("fragment file not found: %v", err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("failed to unmarshal generated outbounds: %v", err)
	}

	if len(wrapper.Outbounds) != 3 {
		t.Fatalf("expected 3 outbounds, got %d", len(wrapper.Outbounds))
	}

	o0 := wrapper.Outbounds[0]
	if o0.Tag != "🇩🇪 Germany VLESS" || o0.Protocol != "vless" {
		t.Errorf("o0 invalid: Tag=%q, Protocol=%q", o0.Tag, o0.Protocol)
	}
	if o0.StreamSettings["security"] != "tls" {
		t.Errorf("o0 security expected tls, got %v", o0.StreamSettings["security"])
	}

	o1 := wrapper.Outbounds[1]
	if o1.Tag != "🇳🇱 Netherlands VMess" || o1.Protocol != "vmess" {
		t.Errorf("o1 invalid: Tag=%q, Protocol=%q", o1.Tag, o1.Protocol)
	}
	if o1.StreamSettings["network"] != "ws" {
		t.Errorf("o1 network expected ws, got %v", o1.StreamSettings["network"])
	}
	wsSettings := o1.StreamSettings["wsSettings"].(map[string]interface{})
	if wsSettings["path"] != "/vmessws" {
		t.Errorf("o1 ws path expected /vmessws, got %v", wsSettings["path"])
	}

	o2 := wrapper.Outbounds[2]
	if o2.Tag != "🇸🇬 Singapore Shadowsocks" || o2.Protocol != "shadowsocks" {
		t.Errorf("o2 invalid: Tag=%q, Protocol=%q", o2.Tag, o2.Protocol)
	}
}

func TestConvertSubscriptionNodesToClashYAML(t *testing.T) {
	svc := &SubscriptionService{}
	nodes := []SubscriptionNode{
		{
			Tag:        "vless-node",
			Protocol:   "vless",
			Server:     "vless.example.com:443",
			UUID:       "vless-uuid",
			Security:   "reality",
			PublicKey:  "pubkey",
			ShortID:    "shortid",
			ServerName: "dest.com",
			Transport:  "grpc",
			WSPath:     "service-name",
		},
		{
			Tag:        "vmess-node",
			Protocol:   "vmess",
			Server:     "vmess.example.com:8080",
			UUID:       "vmess-uuid",
			AlterID:    2,
			Security:   "tls",
			Insecure:   true,
			Transport:  "ws",
			WSPath:     "/path",
			ServerName: "sni.com",
		},
		{
			Tag:        "trojan-node",
			Protocol:   "trojan",
			Server:     "trojan.example.com:443",
			Password:   "trojan-pass",
			ServerName: "trojan-sni",
			Insecure:   true,
		},
		{
			Tag:      "ss-node",
			Protocol: "ss",
			Server:   "ss.example.com:8388",
			Cipher:   "aes-128-gcm",
			Password: "ss-password",
		},
		{
			Tag:          "hy2-node",
			Protocol:     "hysteria2",
			Server:       "hy2.example.com:443",
			Password:     "hy2-pass",
			ServerName:   "hy2-sni",
			Insecure:     true,
			ObfsType:     "simple",
			ObfsPassword: "obfs-password",
		},
		{
			Tag:      "ipv6-node",
			Protocol: "trojan",
			Server:   "[2001:db8::1]:8080",
			Password: "trojan-pass",
		},
	}

	yamlContent, names := svc.convertSubscriptionNodesToClashYAML(nodes)

	expectedNames := []string{"vless-node", "vmess-node", "trojan-node", "ss-node", "hy2-node", "ipv6-node"}
	if len(names) != len(expectedNames) {
		t.Fatalf("expected names length %d, got %d", len(expectedNames), len(names))
	}
	for i, name := range names {
		if name != expectedNames[i] {
			t.Errorf("expected name %q at %d, got %q", expectedNames[i], i, name)
		}
	}

	blocks, parsedNames := ParseMihomoSubscriptionBlocks(yamlContent)
	if len(blocks) != 6 {
		t.Fatalf("expected 6 parsed blocks, got %d. YAML:\n%s", len(blocks), yamlContent)
	}

	n0 := ParseClashProxyNode(blocks[0])
	if n0.Tag != "vless-node" || n0.Protocol != "vless" || n0.UUID != "vless-uuid" || n0.Security != "reality" || n0.PublicKey != "pubkey" || n0.ShortID != "shortid" || n0.ServerName != "dest.com" || n0.Transport != "grpc" || n0.WSPath != "service-name" {
		t.Errorf("vless-node parsed incorrectly: %+v", n0)
	}

	n1 := ParseClashProxyNode(blocks[1])
	if n1.Tag != "vmess-node" || n1.Protocol != "vmess" || n1.UUID != "vmess-uuid" || n1.AlterID != 2 || n1.Security != "tls" || !n1.Insecure || n1.Transport != "ws" || n1.WSPath != "/path" || n1.ServerName != "sni.com" {
		t.Errorf("vmess-node parsed incorrectly: %+v", n1)
	}

	n2 := ParseClashProxyNode(blocks[2])
	if n2.Tag != "trojan-node" || n2.Protocol != "trojan" || n2.Password != "trojan-pass" || n2.Security != "tls" || n2.ServerName != "trojan-sni" || !n2.Insecure {
		t.Errorf("trojan-node parsed incorrectly: %+v", n2)
	}

	n3 := ParseClashProxyNode(blocks[3])
	if n3.Tag != "ss-node" || n3.Protocol != "shadowsocks" || n3.Cipher != "aes-128-gcm" || n3.Password != "ss-password" {
		t.Errorf("ss-node parsed incorrectly: %+v", n3)
	}

	n4 := ParseClashProxyNode(blocks[4])
	if n4.Tag != "hy2-node" || n4.Protocol != "hysteria2" || n4.Password != "hy2-pass" || n4.ServerName != "hy2-sni" || !n4.Insecure || n4.ObfsType != "simple" || n4.ObfsPassword != "obfs-password" {
		t.Errorf("hy2-node parsed incorrectly: %+v", n4)
	}

	n5 := ParseClashProxyNode(blocks[5])
	if n5.Tag != "ipv6-node" || n5.Protocol != "trojan" || n5.Password != "trojan-pass" {
		t.Errorf("ipv6-node parsed incorrectly: %+v", n5)
	}

	if len(parsedNames) != 6 {
		t.Fatalf("expected 6 parsed names, got %d", len(parsedNames))
	}
}

func TestApplyClashFilters(t *testing.T) {
	svc := &SubscriptionService{}
	blocks := []string{
		"- name: us-vless\n  type: vless\n  network: tcp",
		"- name: de-vmess\n  type: vmess\n  network: ws",
		"- name: de-ss\n  type: ss\n  network: tcp",
	}
	names := []string{"us-vless", "de-vmess", "de-ss"}

	sub1 := &Subscription{FilterName: "de-"}
	fb1, fn1 := svc.applyClashFilters(blocks, names, sub1)
	if len(fb1) != 2 || fn1[0] != "de-vmess" || fn1[1] != "de-ss" {
		t.Errorf("FilterName failed: got %v", fn1)
	}

	sub2 := &Subscription{FilterType: "vless"}
	fb2, fn2 := svc.applyClashFilters(blocks, names, sub2)
	if len(fb2) != 1 || fn2[0] != "us-vless" {
		t.Errorf("FilterType failed: got %v", fn2)
	}

	sub3 := &Subscription{FilterTransport: "ws"}
	fb3, fn3 := svc.applyClashFilters(blocks, names, sub3)
	if len(fb3) != 1 || fn3[0] != "de-vmess" {
		t.Errorf("FilterTransport failed: got %v", fn3)
	}
}

func TestConvertSubscriptionNodesToClashYAML_Wireguard(t *testing.T) {
	svc := &SubscriptionService{}
	jc := 4
	jmin := 40
	jmax := 70
	s1 := 15
	s2 := 40
	s3 := 20
	s4 := 30
	cpa := 12
	rt := true
	dc := false
	rekey := 120

	nodes := []SubscriptionNode{
		{
			Tag:            "pure-wg",
			Protocol:       "wireguard",
			Server:         "198.51.100.1:51820",
			SecretKey:      "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE=",
			PublicKey:      "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI=",
			PreSharedKey:   "Y2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2NjY2M=",
			LocalAddresses: []string{"10.0.0.2/32"},
			DNS:            []string{"1.1.1.1", "8.8.8.8"},
			MTU:            1420,
			Reserved:       []int{1, 2, 3},
		},
		{
			Tag:            "awg-31",
			Protocol:       "wireguard",
			Server:         "198.51.100.2:51820",
			SecretKey:      "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE=",
			PublicKey:      "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI=",
			LocalAddresses: []string{"10.0.0.3/32"},
			MTU:            1360,
			AWG: &AWGOptions{
				Jc:                     &jc,
				Jmin:                   &jmin,
				Jmax:                   &jmax,
				S1:                     &s1,
				S2:                     &s2,
				S3:                     &s3,
				S4:                     &s4,
				H1:                     "1000000001",
				H2:                     "1000000002",
				H3:                     "1000000003",
				H4:                     "1000000004",
				I1:                     "0A1B2C",
				I2:                     "3D4E5F",
				I3:                     "112233",
				I4:                     "445566",
				I5:                     "778899",
				Version:                "3.1",
				HeaderProtectionKey:    "secret-hpk-key",
				ContentPaddingAddition: &cpa,
				RandomTrailers:         &rt,
				DisableCookies:         &dc,
				RekeyAfterTime:         &rekey,
			},
		},
	}

	yamlContent, names := svc.convertSubscriptionNodesToClashYAML(nodes)
	if len(names) != 2 || names[0] != "pure-wg" || names[1] != "awg-31" {
		t.Fatalf("unexpected names: %v", names)
	}

	// 1. Check pure-wg does NOT contain amnezia-wg-option
	blocks, _ := ParseMihomoSubscriptionBlocks(yamlContent)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d. YAML:\n%s", len(blocks), yamlContent)
	}

	node0 := ParseClashProxyNode(blocks[0])
	if node0.Tag != "pure-wg" || node0.Protocol != "wireguard" {
		t.Errorf("unexpected node0: %+v", node0)
	}
	if node0.AWG != nil {
		t.Errorf("pure-wg must not have AWG, got %+v", node0.AWG)
	}
	if node0.Dialect != "plain" {
		t.Errorf("expected plain dialect, got %s", node0.Dialect)
	}

	// 2. Check awg-31 contains all 13 fields and matches round-trip
	node1 := ParseClashProxyNode(blocks[1])
	if node1.Tag != "awg-31" || node1.Protocol != "wireguard" {
		t.Errorf("unexpected node1: %+v", node1)
	}
	if node1.AWG == nil {
		t.Fatalf("node1 must have AWG populated")
	}
	if *node1.AWG.Jc != 4 || *node1.AWG.S1 != 15 || *node1.AWG.S3 != 20 {
		t.Errorf("mismatched AWG numeric fields: %+v", node1.AWG)
	}
	if node1.AWG.H1 != "1000000001" || node1.AWG.I1 != "0A1B2C" || node1.AWG.HeaderProtectionKey != "secret-hpk-key" {
		t.Errorf("mismatched AWG string fields: %+v", node1.AWG)
	}
	if node1.Dialect != "3.1" {
		t.Errorf("expected 3.1 dialect, got %s", node1.Dialect)
	}
}

func TestRoundTrip_AllImportPaths_AWG(t *testing.T) {
	svc := &SubscriptionService{}
	sub := &Subscription{TagPrefix: "rt"}

	keyA := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE="
	keyB := "YmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmJmYmI="

	// Path 1: wg-quick .conf
	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.0.0.2/32
Jc = 4
S1 = 15
H1 = 1000000001
I1 = 0a1b2c
Version = 3.1
HeaderProtectionKey = hpk-secret

[Peer]
PublicKey = %s
Endpoint = 198.51.100.1:51820
AllowedIPs = 0.0.0.0/0
`, keyA, keyB)

	outbounds1, _, err1 := parseSubscriptionBody([]byte(conf), "text/plain", sub)
	if err1 != nil || len(outbounds1) != 1 {
		t.Fatalf("failed to parse wg-quick conf: %v", err1)
	}
	nodes1 := svc.outboundsToNodes(outbounds1, sub)
	yaml1, _ := svc.convertSubscriptionNodesToClashYAML(nodes1)
	blocks1, _ := ParseMihomoSubscriptionBlocks(yaml1)
	if len(blocks1) != 1 {
		t.Fatalf("path 1 failed to convert to YAML block: %s", yaml1)
	}
	rtNode1 := ParseClashProxyNode(blocks1[0])
	if rtNode1.AWG == nil || *rtNode1.AWG.Jc != 4 || rtNode1.AWG.I1 != "0A1B2C" || rtNode1.Dialect != "3.1" {
		t.Errorf("path 1 round-trip mismatch: %+v, AWG: %+v", rtNode1, rtNode1.AWG)
	}

	// Path 2: awg:// share link
	awgLink := fmt.Sprintf("awg://%s@198.51.100.2:51820?publickey=%s&ip=10.0.0.3&jc=4&s1=15&h1=1000000001&i1=0a1b2c&version=3.1&headerprotectionkey=hpk-secret#AWGLink", keyA, keyB)
	outbounds2, _, err2 := parseSubscriptionBody([]byte(awgLink), "text/plain", sub)
	if err2 != nil || len(outbounds2) != 1 {
		t.Fatalf("failed to parse awg link: %v", err2)
	}
	nodes2 := svc.outboundsToNodes(outbounds2, sub)
	yaml2, _ := svc.convertSubscriptionNodesToClashYAML(nodes2)
	blocks2, _ := ParseMihomoSubscriptionBlocks(yaml2)
	if len(blocks2) != 1 {
		t.Fatalf("path 2 failed to convert to YAML block: %s", yaml2)
	}
	rtNode2 := ParseClashProxyNode(blocks2[0])
	if rtNode2.AWG == nil || *rtNode2.AWG.Jc != 4 || rtNode2.AWG.I1 != "0A1B2C" || rtNode2.Dialect != "3.1" {
		t.Errorf("path 2 round-trip mismatch: %+v, AWG: %+v", rtNode2, rtNode2.AWG)
	}

	// Path 3: sing-box JSON
	singboxJSON := fmt.Sprintf(`{
  "outbounds": [
    {
      "type": "wireguard",
      "tag": "sb-awg",
      "server": "198.51.100.3",
      "server_port": 51820,
      "private_key": "%s",
      "peer_public_key": "%s",
      "local_address": ["10.0.0.4/32"],
      "jc": 4,
      "s1": 15,
      "h1": "1000000001",
      "i1": "0a1b2c",
      "version": "3.1",
      "header_protection_key": "hpk-secret"
    }
  ]
}`, keyA, keyB)
	outbounds3, err3 := parseSingBoxJSON([]byte(singboxJSON))
	if err3 != nil || len(outbounds3) != 1 {
		t.Fatalf("failed to parse sing-box JSON: %v", err3)
	}
	nodes3 := svc.outboundsToNodes(outbounds3, sub)
	yaml3, _ := svc.convertSubscriptionNodesToClashYAML(nodes3)
	blocks3, _ := ParseMihomoSubscriptionBlocks(yaml3)
	if len(blocks3) != 1 {
		t.Fatalf("path 3 failed to convert to YAML block: %s", yaml3)
	}
	rtNode3 := ParseClashProxyNode(blocks3[0])
	if rtNode3.AWG == nil || *rtNode3.AWG.Jc != 4 || rtNode3.AWG.I1 != "0A1B2C" || rtNode3.Dialect != "3.1" {
		t.Errorf("path 3 round-trip mismatch: %+v, AWG: %+v", rtNode3, rtNode3.AWG)
	}

	// Path 4: Clash YAML with amnezia-wg-option
	clashInput := fmt.Sprintf(`proxies:
  - name: clash-awg
    type: wireguard
    server: 198.51.100.4
    port: 51820
    private-key: %s
    public-key: %s
    ip: 10.0.0.5/32
    amnezia-wg-option:
      jc: 4
      s1: 15
      h1: 1000000001
      i1: 0a1b2c
      version: 3.1
      header-protection-key: hpk-secret
`, keyA, keyB)
	blocks4, _ := ParseMihomoSubscriptionBlocks(clashInput)
	if len(blocks4) != 1 {
		t.Fatalf("failed to parse clash input blocks")
	}
	node4 := ParseClashProxyNode(blocks4[0])
	yaml4, _ := svc.convertSubscriptionNodesToClashYAML([]SubscriptionNode{node4})
	blocks4Out, _ := ParseMihomoSubscriptionBlocks(yaml4)
	if len(blocks4Out) != 1 {
		t.Fatalf("path 4 failed to re-convert to YAML block: %s", yaml4)
	}
	rtNode4 := ParseClashProxyNode(blocks4Out[0])
	if rtNode4.AWG == nil || *rtNode4.AWG.Jc != 4 || rtNode4.AWG.I1 != "0A1B2C" || rtNode4.Dialect != "3.1" {
		t.Errorf("path 4 round-trip mismatch: %+v, AWG: %+v", rtNode4, rtNode4.AWG)
	}
}

func TestConvertSubscriptionNodesToClashYAML_SkipsInvalidWireGuardWithoutKeys(t *testing.T) {
	svc := &SubscriptionService{}
	nodes := []SubscriptionNode{
		{
			Tag:       "wg-missing-both-keys",
			Protocol:  "wireguard",
			Server:    "1.2.3.4:51820",
			SecretKey: "",
			PublicKey: "",
		},
		{
			Tag:       "wg-missing-secret",
			Protocol:  "wireguard",
			Server:    "1.2.3.4:51820",
			SecretKey: "",
			PublicKey: "pubkey123=",
		},
		{
			Tag:       "wg-missing-public",
			Protocol:  "wireguard",
			Server:    "1.2.3.4:51820",
			SecretKey: "privkey123=",
			PublicKey: "",
		},
		{
			Tag:       "wg-valid",
			Protocol:  "wireguard",
			Server:    "1.2.3.4:51820",
			SecretKey: "privkey123=",
			PublicKey: "pubkey123=",
		},
	}

	_, names := svc.convertSubscriptionNodesToClashYAML(nodes)
	if len(names) != 1 {
		t.Fatalf("expected exactly 1 valid node emitted, got %d: %v", len(names), names)
	}
	if names[0] != "wg-valid" {
		t.Errorf("expected 'wg-valid', got %q", names[0])
	}
}

func TestConvertSubscriptionNodesToClashYAML_SafeEscapingDNSAndRawOptions(t *testing.T) {
	svc := &SubscriptionService{}
	nodes := []SubscriptionNode{
		{
			Tag:       "wg-safe-yaml",
			Protocol:  "wireguard",
			Server:    "1.2.3.4:51820",
			SecretKey: "privkey123=",
			PublicKey: "pubkey123=",
			DNS:       []string{"1.1.1.1", "2606:4700:4700::1111#cloudflare"},
			AWG: &AWGOptions{
				RawOptions: map[string]interface{}{
					"custom-str": "value: with colon # and comment",
					"custom-int": 42,
				},
			},
		},
	}

	yaml, _ := svc.convertSubscriptionNodesToClashYAML(nodes)
	if !strings.Contains(yaml, `dns: ['1.1.1.1', '2606:4700:4700::1111#cloudflare']`) {
		t.Errorf("expected escaped DNS in YAML, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, `custom-str: 'value: with colon # and comment'`) {
		t.Errorf("expected escaped RawOptions string in YAML, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, `custom-int: 42`) {
		t.Errorf("expected RawOptions int in YAML, got:\n%s", yaml)
	}
}


