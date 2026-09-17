package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouteTracer_NormalizeTarget(t *testing.T) {
	tests := []struct {
		input       string
		defaultPort int
		wantHost    string
		wantPort    int
		wantIsIP    bool
		wantErr     bool
	}{
		{"https://example.com/path", 0, "example.com", 443, false, false},
		{"http://1.1.1.1:8080/test", 0, "1.1.1.1", 8080, true, false},
		{"rutracker.org", 80, "rutracker.org", 80, false, false},
		{"[2001:db8::1]:8443", 0, "2001:db8::1", 8443, true, false},
		{"", 0, "", 0, false, true},
		{"invalid target with spaces", 0, "", 0, false, true},
	}

	for _, tt := range tests {
		host, port, isIP, _, err := normalizeTarget(tt.input, tt.defaultPort)
		if (err != nil) != tt.wantErr {
			t.Errorf("normalizeTarget(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if host != tt.wantHost {
				t.Errorf("normalizeTarget(%q) host = %s, want %s", tt.input, host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("normalizeTarget(%q) port = %d, want %d", tt.input, port, tt.wantPort)
			}
			if isIP != tt.wantIsIP {
				t.Errorf("normalizeTarget(%q) isIP = %v, want %v", tt.input, isIP, tt.wantIsIP)
			}
		}
	}
}

func TestRouteTracer_MatchUserRules(t *testing.T) {
	rules := []UserRule{
		{
			ID:      "1",
			Type:    "domain_suffix",
			Value:   "rutracker.org",
			Target:  "proxy",
			Group:   "MyGroup",
			Enabled: true,
		},
		{
			ID:      "2",
			Type:    "ip_cidr",
			Value:   "192.168.1.0/24",
			Target:  "direct",
			Enabled: true,
		},
		{
			ID:      "3",
			Type:    "port",
			Value:   "22",
			Target:  "direct",
			Enabled: true,
		},
	}

	// 1. Suffix match
	host, port, isIP, ip, _ := normalizeTarget("sub.rutracker.org:443", 0)
	res := matchUserRules(rules, host, port, isIP, ip, "PROXY")
	if res == nil || res.TargetGroup != "MyGroup" || res.Source != "user_rule" {
		t.Fatalf("expected match for sub.rutracker.org with MyGroup, got %+v", res)
	}

	// 2. IP CIDR match
	hostIP, portIP, isIP2, ip2, _ := normalizeTarget("192.168.1.55", 0)
	resIP := matchUserRules(rules, hostIP, portIP, isIP2, ip2, "PROXY")
	if resIP == nil || resIP.TargetAction != "DIRECT" {
		t.Fatalf("expected match for 192.168.1.55 DIRECT, got %+v", resIP)
	}

	// 3. Port match
	hostPort, portVal, isIP3, ip3, _ := normalizeTarget("remote.server:22", 0)
	resPort := matchUserRules(rules, hostPort, portVal, isIP3, ip3, "PROXY")
	if resPort == nil || resPort.RuleType != "port" {
		t.Fatalf("expected port match for :22, got %+v", resPort)
	}

	// 4. No match
	hostNo, portNo, isIP4, ip4, _ := normalizeTarget("google.com", 0)
	resNo := matchUserRules(rules, hostNo, portNo, isIP4, ip4, "PROXY")
	if resNo != nil {
		t.Fatalf("expected no match for google.com, got %+v", resNo)
	}
}

func TestRouteTracer_FullTrace(t *testing.T) {
	// Setup mock server for Mihomo controller
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rules":
			resp := map[string]interface{}{
				"rules": []map[string]string{
					{"type": "DOMAIN-SUFFIX", "payload": "google.com", "proxy": "GOOGLE-GROUP"},
					{"type": "MATCH", "payload": "", "proxy": "DEFAULT-GROUP"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/proxies/GOOGLE-GROUP":
			resp := map[string]interface{}{
				"name": "GOOGLE-GROUP",
				"type": "Selector",
				"now":  "node-nl-01",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/proxies/node-nl-01":
			resp := map[string]interface{}{
				"name": "node-nl-01",
				"type": "Vless",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/proxies/DEFAULT-GROUP":
			resp := map[string]interface{}{
				"name": "DEFAULT-GROUP",
				"type": "URLTest",
				"now":  "node-de-02",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/proxies/node-de-02":
			resp := map[string]interface{}{
				"name": "node-de-02",
				"type": "Shadowsocks",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	hostPort := strings.TrimPrefix(server.URL, "http://")
	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "config.yaml")
	cfgContent := fmt.Sprintf("external-controller: %s\nsecret: \"test-token\"\n", hostPort)
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	userRulesSvc := NewUserRulesService(tmpDir)
	_ = userRulesSvc.Save([]UserRule{
		{
			ID:      "1",
			Type:    "domain_suffix",
			Value:   "custom-proxy.org",
			Target:  "proxy",
			Group:   "GOOGLE-GROUP",
			Enabled: true,
		},
	})

	mihomoSvc := NewMihomoService("", "", tmpDir)
	tracer := NewRouteTracerService(userRulesSvc, mihomoSvc, tmpDir)

	ctx := context.Background()

	// 1. Trace matching user rule
	resUser, err := tracer.TraceRoute(ctx, "sub.custom-proxy.org", 443)
	if err != nil {
		t.Fatalf("TraceRoute user rule failed: %v", err)
	}
	if resUser.Source != "user_rule" || resUser.TargetGroup != "GOOGLE-GROUP" {
		t.Errorf("unexpected user rule trace: %+v", resUser)
	}
	if resUser.SelectedProxy != "node-nl-01" || resUser.ProxyType != "Vless" {
		t.Errorf("unexpected proxy resolution: %s (%s)", resUser.SelectedProxy, resUser.ProxyType)
	}
	if resUser.TraceTimeMs <= 0 {
		t.Errorf("expected TraceTimeMs > 0, got %f", resUser.TraceTimeMs)
	}

	// 2. Trace matching kernel rule (google.com)
	resKernel, err := tracer.TraceRoute(ctx, "mail.google.com", 443)
	if err != nil {
		t.Fatalf("TraceRoute kernel rule failed: %v", err)
	}
	if resKernel.Source != "kernel_rule" || resKernel.TargetGroup != "GOOGLE-GROUP" {
		t.Errorf("unexpected kernel rule trace: %+v", resKernel)
	}

	// 3. Trace matching fallback MATCH (unknown-domain.xyz)
	resFallback, err := tracer.TraceRoute(ctx, "unknown-domain.xyz", 80)
	if err != nil {
		t.Fatalf("TraceRoute fallback failed: %v", err)
	}
	if resFallback.TargetGroup != "DEFAULT-GROUP" {
		t.Errorf("unexpected fallback trace: %+v", resFallback)
	}
	if resFallback.SelectedProxy != "node-de-02" || resFallback.ProxyType != "Shadowsocks" {
		t.Errorf("unexpected fallback proxy: %s (%s)", resFallback.SelectedProxy, resFallback.ProxyType)
	}
}

func TestRouteTracer_NestedProxyGroupResolution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/proxies/CHAIN-TOP":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "CHAIN-TOP",
				"type": "Selector",
				"now":  "CHAIN-MID",
			})
		case "/proxies/CHAIN-MID":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "CHAIN-MID",
				"type": "URLTest",
				"now":  "TERMINAL-NODE",
			})
		case "/proxies/TERMINAL-NODE":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name": "TERMINAL-NODE",
				"type": "Hysteria2",
				"now":  "",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	hostPort := strings.TrimPrefix(server.URL, "http://")
	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "config.yaml")
	cfgContent := fmt.Sprintf("external-controller: %s\nsecret: \"test-token\"\n", hostPort)
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	userRulesSvc := NewUserRulesService(tmpDir)
	mihomoSvc := NewMihomoService("", "", tmpDir)
	tracer := NewRouteTracerService(userRulesSvc, mihomoSvc, tmpDir)

	proxy, pType := tracer.lookupSelectedProxy(context.Background(), "CHAIN-TOP")
	if proxy != "TERMINAL-NODE" {
		t.Errorf("expected TERMINAL-NODE, got %s", proxy)
	}
	if pType != "Hysteria2" {
		t.Errorf("expected Hysteria2, got %s", pType)
	}
}
