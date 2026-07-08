package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
