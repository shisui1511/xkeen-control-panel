package services

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubscriptionService_UpdateTypeTransition(t *testing.T) {
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	mihomoDir := filepath.Join(tmp, "mihomo")
	_ = os.MkdirAll(xrayDir, 0755)
	_ = os.MkdirAll(mihomoDir, 0755)

	svc := NewSubscriptionService(tmp, xrayDir, mihomoDir)

	sub := Subscription{
		Name:         "Transition Test",
		URL:          "https://example.com/sub",
		Enabled:      true,
		EnableXray:   true,
		EnableMihomo: false,
	}
	svc.Add(&sub)

	id := svc.List()[0].ID

	fragmentPath := filepath.Join(xrayDir, fmt.Sprintf("04_outbounds.%s.json", id))
	_ = os.WriteFile(fragmentPath, []byte(`[]`), 0600)

	updatedSub := sub
	updatedSub.EnableXray = false
	updatedSub.EnableMihomo = true
	err := svc.Update(id, &updatedSub)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if _, err := os.Stat(fragmentPath); !os.IsNotExist(err) {
		t.Error("xray fragment should have been deleted during transition to mihomo")
	}

	configPath := filepath.Join(mihomoDir, "config.yaml")
	providerName := GetMihomoProviderName("", sub.Name, sub.URL, id)
	_ = os.WriteFile(configPath, []byte("proxy-providers:\n  "+providerName+":\n    type: http\n"), 0600)
	providerPath := filepath.Join(mihomoDir, "providers", fmt.Sprintf("%s.yaml", providerName))
	_ = os.MkdirAll(filepath.Join(mihomoDir, "providers"), 0755)
	_ = os.WriteFile(providerPath, []byte(""), 0600)

	updatedSub2 := updatedSub
	updatedSub2.EnableXray = true
	updatedSub2.EnableMihomo = false
	err = svc.Update(id, &updatedSub2)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if _, err := os.Stat(providerPath); !os.IsNotExist(err) {
		t.Error("mihomo provider file should have been deleted during transition to xray")
	}

	data, err := os.ReadFile(configPath)
	if err == nil && strings.Contains(string(data), "proxy-providers:") {
		t.Error("mihomo config should have proxy-providers section cleared during transition to xray")
	}
}

func TestSubscriptionService_UpdateMihomoProviderNameChange(t *testing.T) {
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	mihomoDir := filepath.Join(tmp, "mihomo")
	_ = os.MkdirAll(xrayDir, 0755)
	_ = os.MkdirAll(mihomoDir, 0755)

	svc := NewSubscriptionService(tmp, xrayDir, mihomoDir)

	sub := Subscription{
		Name:         "Old Name",
		URL:          "https://example.com/old-sub",
		Enabled:      true,
		EnableXray:   false,
		EnableMihomo: true,
		MihomoGroups: []string{"Proxy"},
	}
	svc.Add(&sub)

	id := svc.List()[0].ID

	configPath := filepath.Join(mihomoDir, "config.yaml")
	oldProviderName := GetMihomoProviderName("", "Old Name", "https://example.com/old-sub", id)
	_ = os.WriteFile(configPath, []byte("proxy-groups:\n  - name: Proxy\n    use:\n      - "+oldProviderName+"\nproxy-providers:\n  "+oldProviderName+":\n    type: http\n"), 0600)
	
	providerDir := filepath.Join(mihomoDir, "providers")
	_ = os.MkdirAll(providerDir, 0755)
	providerPath := filepath.Join(providerDir, fmt.Sprintf("%s.yaml", oldProviderName))
	_ = os.WriteFile(providerPath, []byte(""), 0600)

	updatedSub := sub
	updatedSub.Name = "New Name"
	updatedSub.URL = "https://example.com/new-sub"
	err := svc.Update(id, &updatedSub)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if _, err := os.Stat(providerPath); !os.IsNotExist(err) {
		t.Error("old mihomo provider file should have been deleted when subscription name/URL changed")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}
	if strings.Contains(string(data), oldProviderName) {
		t.Error("old provider name should have been removed from config.yaml")
	}
}

func TestSubscriptionScheduler_FrozenClock(t *testing.T) {
	tmp := t.TempDir()

	var refreshCount int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&refreshCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	pastTime := time.Now().Add(-2 * time.Hour)
	sub1 := &Subscription{
		Name:       "overdue",
		URL:        ts.URL,
		TagPrefix:  "s1-",
		Enabled:    true,
		EnableXray: true,
		Interval:   1,
		LastUpdate: pastTime,
	}
	if err := svc.Add(sub1); err != nil {
		t.Fatalf("Add sub1: %v", err)
	}

	recentTime := time.Now().Add(-30 * time.Minute)
	sub2 := &Subscription{
		Name:       "not-due",
		URL:        ts.URL,
		TagPrefix:  "s2-",
		Enabled:    true,
		EnableXray: true,
		Interval:   1,
		LastUpdate: recentTime,
	}
	if err := svc.Add(sub2); err != nil {
		t.Fatalf("Add sub2: %v", err)
	}

	now := time.Now()
	if !svc.isRefreshDue(sub1, now) {
		t.Error("expected sub1 (overdue) to be due")
	}
	if svc.isRefreshDue(sub2, now) {
		t.Error("expected sub2 (recent) to NOT be due")
	}

	svc.checkAndRefreshDue(now)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&refreshCount) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got := atomic.LoadInt64(&refreshCount)
	if got != 1 {
		t.Errorf("expected 1 refresh call, got %d", got)
	}
}

func TestExponentialBackoff(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	id := "sub_backoff_test"

	svc.recordFailure(id)
	val, ok := svc.retries.Load(id)
	if !ok {
		t.Fatal("expected retry state after first failure")
	}
	rs := val.(*retryState)
	if rs.failCount != 1 {
		t.Errorf("expected failCount=1, got %d", rs.failCount)
	}
	expectedDelay1 := backoffBase
	actualDelay1 := time.Until(rs.nextRetry)
	if actualDelay1 < expectedDelay1-time.Second || actualDelay1 > expectedDelay1+time.Second {
		t.Errorf("expected delay ~%v, got %v", expectedDelay1, actualDelay1)
	}

	svc.recordFailure(id)
	val, _ = svc.retries.Load(id)
	rs = val.(*retryState)
	if rs.failCount != 2 {
		t.Errorf("expected failCount=2, got %d", rs.failCount)
	}
	expectedDelay2 := backoffBase * 2
	actualDelay2 := time.Until(rs.nextRetry)
	if actualDelay2 < expectedDelay2-time.Second || actualDelay2 > expectedDelay2+time.Second {
		t.Errorf("expected delay ~%v, got %v", expectedDelay2, actualDelay2)
	}

	svc.clearFailure(id)
	if _, ok := svc.retries.Load(id); ok {
		t.Error("expected retry state to be cleared after success")
	}
}

func TestBackoffCap(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	id := "sub_cap_test"

	for i := 0; i < 20; i++ {
		svc.recordFailure(id)
	}
	val, ok := svc.retries.Load(id)
	if !ok {
		t.Fatal("expected retry state")
	}
	rs := val.(*retryState)
	actualDelay := time.Until(rs.nextRetry)
	if actualDelay > backoffMax+time.Second {
		t.Errorf("expected delay capped at %v, got %v", backoffMax, actualDelay)
	}
}

func TestMihomoSubscriptionType(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	yamlContent := `proxies:
  - name: TestProxy
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: testpass
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(yamlContent))
	}))
	defer ts.Close()

	sub := Subscription{
		Name:         "Mihomo Sub",
		URL:          ts.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	id := svc.List()[0].ID
	if err := svc.Refresh(id); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got := svc.List()[0]

	providerName := GetMihomoProviderName("", sub.Name, sub.URL, id)
	configPath := filepath.Join(tmp, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config.yaml at %s: %v", configPath, err)
	}
	if !strings.Contains(string(data), "proxy-providers:") {
		t.Error("expected 'proxy-providers:' in config.yaml after refresh")
	}
	if !strings.Contains(string(data), providerName) {
		t.Errorf("expected provider name %q in config.yaml after refresh", providerName)
	}

	providerPath := filepath.Join(tmp, "providers", providerName+".yaml")
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("expected provider file at %s: %v", providerPath, err)
	}
	if !strings.Contains(string(providerData), "TestProxy") {
		t.Error("expected 'TestProxy' in provider file after refresh")
	}

	if got.ProxyCount != 1 {
		t.Errorf("expected ProxyCount=1, got %d", got.ProxyCount)
	}
	if len(got.ProxyNames) != 1 || got.ProxyNames[0] != "TestProxy" {
		t.Errorf("expected ProxyNames=[TestProxy], got %v", got.ProxyNames)
	}
}

func TestSubscriptionTrafficAndRules(t *testing.T) {
	upload, download, total, expire := parseSubscriptionUserinfo("upload=1073741824; download=5368709120; total=107374182400; expire=1700000000")
	if upload != 1073741824 || download != 5368709120 || total != 107374182400 || expire != 1700000000 {
		t.Errorf("parseSubscriptionUserinfo failed: upload=%d, download=%d, total=%d, expire=%d", upload, download, total, expire)
	}

	yaml := `
port: 7890
socks-port: 7891
rules:
  - DOMAIN-SUFFIX,google.com,PROXY
  - DOMAIN-KEYWORD,google,PROXY
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`
	rulesCount := countMihomoRules(yaml)
	if rulesCount != 4 {
		t.Errorf("expected 4 rules, got %d", rulesCount)
	}

	yaml2 := `
rules:
  - DOMAIN,example.com,DIRECT
proxies:
  - name: proxy
    type: socks5
`
	rulesCount2 := countMihomoRules(yaml2)
	if rulesCount2 != 1 {
		t.Errorf("expected 1 rule in yaml2, got %d", rulesCount2)
	}

	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=100; download=200; total=1000")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("proxies:\n  - name: p1\n    type: ss\nrules:\n  - MATCH,p1"))
	}))
	defer ts.Close()

	sub := Subscription{
		Name:         "Traffic Sub",
		URL:          ts.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatal(err)
	}

	id := svc.List()[0].ID
	if err := svc.Refresh(id); err != nil {
		t.Fatal(err)
	}

	got := svc.Get(id)
	if got.Upload != 100 || got.Download != 200 || got.Total != 1000 {
		t.Errorf("expected traffic values 100, 200, 1000; got %d, %d, %d", got.Upload, got.Download, got.Total)
	}
	if got.RuleCount != 1 {
		t.Errorf("expected RuleCount=1, got %d", got.RuleCount)
	}
}

func TestSubscriptionDiagnostics(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = http.DefaultClient

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("profile-title", "base64:VGVzdCBPbmU=")
		w.Header().Set("support-url", "https://t.me/support")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("vmess://eyJhZGQiOiIxLjIuMy40IiwicG9ydCI6IjQ0MyIsImlkIjoiNTUwZTg0MDAtZTI5Yi00MWQ0LWE3MTYtNDQ2NjU1NDQwMDAwIiwicHMiOiJ2bWVzcy1ub2RlIiwibmV0IjoidGNwIiwidHlwZSI6Im5vbmUifQ==\ninvalid-line\n"))
	}))
	defer ts.Close()

	sub := Subscription{
		ID:         "diag_test",
		Name:       "Diag Test",
		URL:        ts.URL,
		Enabled:    true,
		EnableXray: true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	err := svc.Refresh("diag_test")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	body, headers, err := svc.GetRaw("diag_test")
	if err != nil {
		t.Fatalf("GetRaw failed: %v", err)
	}
	if !strings.Contains(body, "vmess://") {
		t.Errorf("expected body to contain vmess, got %q", body)
	}
	if headers["Support-Url"][0] != "https://t.me/support" {
		t.Errorf("expected support-url header, got %v", headers["Support-Url"])
	}

	report, err := svc.GetParseReport("diag_test")
	if err != nil {
		t.Fatalf("GetParseReport failed: %v", err)
	}
	if report.ParsedCount != 1 {
		t.Errorf("expected 1 parsed node, got %d", report.ParsedCount)
	}
	if report.SkippedCount != 1 {
		t.Errorf("expected 1 skipped line, got %d", report.SkippedCount)
	}
	if len(report.Skipped) != 1 {
		t.Fatalf("expected 1 skip reason, got %d", len(report.Skipped))
	}
	if report.Skipped[0].Line != 2 {
		t.Errorf("expected line 2 skipped, got %d", report.Skipped[0].Line)
	}
	if report.Skipped[0].Snippet != "invalid-line" {
		t.Errorf("expected snippet 'invalid-line', got %q", report.Skipped[0].Snippet)
	}

	if err := svc.Delete("diag_test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, _, err = svc.GetRaw("diag_test")
	if err == nil {
		t.Error("expected error getting raw after delete, got nil")
	}
}

func TestSubscriptionService_MihomoProxyProvider(t *testing.T) {
	tmp := t.TempDir()

	yamlContent := `proxies:
  - name: node1
    type: ss
    server: 1.2.3.4
    port: 443
    cipher: chacha20-ietf-poly1305
    password: test
  - name: node2
    type: vmess
    server: 5.6.7.8
    port: 443
    uuid: uuid
    alterId: 0
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer srv.Close()

	logFile := filepath.Join(tmp, "xkeen_calls.log")
	scriptContent := fmt.Sprintf("#!/bin/sh\necho \"$1\" >> %q\n", logFile)
	mockXkeenPath := filepath.Join(tmp, "mock-xkeen")
	if err := os.WriteFile(mockXkeenPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	mihomoDir := filepath.Join(tmp, "mihomo")
	if err := os.MkdirAll(mihomoDir, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(mihomoDir, "config.yaml")
	initialConfig := `port: 9090
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0600); err != nil {
		t.Fatal(err)
	}

	svc := NewSubscriptionService(tmp, tmp, mihomoDir)
	svc.httpClient = srv.Client()

	consoleSvc := NewConsoleService(mockXkeenPath)
	svc.SetConsoleService(consoleSvc)

	sub := Subscription{
		ID:           "mihomo-sub",
		Name:         "Mihomo Sub Test",
		URL:          srv.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
		Interval:     1,
		MihomoGroups: []string{"PROXY"},
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("failed to add subscription: %v", err)
	}

	if err := svc.Refresh("mihomo-sub"); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configStr := string(configBytes)

	if !strings.Contains(configStr, "proxy-providers:") {
		t.Error("config.yaml should contain proxy-providers:")
	}
	providerName := GetMihomoProviderName("", sub.Name, sub.URL, sub.ID)
	if !strings.Contains(configStr, providerName+":") {
		t.Errorf("config.yaml should contain provider name %q", providerName)
	}
	if !strings.Contains(configStr, "use:\n      - "+providerName) {
		t.Errorf("config.yaml group should use provider, got:\n%s", configStr)
	}

	providerFilePath := filepath.Join(mihomoDir, "providers", providerName+".yaml")
	if _, err := os.Stat(providerFilePath); err != nil {
		t.Errorf("provider file should be written at %s: %v", providerFilePath, err)
	}

	callsBytes, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should be called")
	}

	if err := os.WriteFile(logFile, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.Refresh("mihomo-sub"); err != nil {
		t.Fatal(err)
	}
	callsBytes, _ = os.ReadFile(logFile)
	if strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should NOT be called when no changes")
	}

	if err := svc.Delete("mihomo-sub"); err != nil {
		t.Fatalf("failed to delete subscription: %v", err)
	}

	configBytes, _ = os.ReadFile(configPath)
	configStr = string(configBytes)
	if strings.Contains(configStr, "mihomo-sub") {
		t.Error("provider and references should be removed from config.yaml after deletion")
	}
	if _, err := os.Stat(providerFilePath); !os.IsNotExist(err) {
		t.Error("provider file should be deleted")
	}
}

func TestRefreshXray_DoesNotRestartXray(t *testing.T) {
	tmp := t.TempDir()

	vmessJSON1 := `{"ps":"xray-node","add":"1.1.1.1","port":"443","id":"uuid","net":"tcp"}`
	vmessJSON2 := `{"ps":"xray-node-new","add":"2.2.2.2","port":"443","id":"uuid","net":"tcp"}`
	b64_1 := base64.StdEncoding.EncodeToString([]byte("vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON1))))
	b64_2 := base64.StdEncoding.EncodeToString([]byte("vmess://" + base64.StdEncoding.EncodeToString([]byte(vmessJSON2))))

	var responseContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseContent))
	}))
	defer srv.Close()

	logFile := filepath.Join(tmp, "xkeen_calls.log")
	scriptContent := fmt.Sprintf("#!/bin/sh\necho \"$1\" >> %q\n", logFile)
	mockXkeenPath := filepath.Join(tmp, "mock-xkeen")
	if err := os.WriteFile(mockXkeenPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	xrayConfigDir := filepath.Join(tmp, "xray")
	_ = os.MkdirAll(xrayConfigDir, 0755)

	svc := NewSubscriptionService(tmp, xrayConfigDir, tmp)
	svc.httpClient = srv.Client()

	consoleSvc := NewConsoleService(mockXkeenPath)
	svc.SetConsoleService(consoleSvc)

	sub := Subscription{
		ID:           "xray-sub",
		Name:         "Xray Sub Test",
		URL:          srv.URL,
		EnableXray:   true,
		EnableMihomo: false,
		Enabled:      true,
		Interval:     1,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatal(err)
	}

	responseContent = b64_1
	if err := svc.Refresh("xray-sub"); err != nil {
		t.Fatalf("first Refresh failed: %v", err)
	}

	callsBytes, _ := os.ReadFile(logFile)
	if !strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should be called on first refresh")
	}

	_ = os.WriteFile(logFile, []byte(""), 0600)
	if err := svc.Refresh("xray-sub"); err != nil {
		t.Fatalf("second Refresh failed: %v", err)
	}
	callsBytes, _ = os.ReadFile(logFile)
	if strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should NOT be called if configuration has not changed")
	}

	_ = os.WriteFile(logFile, []byte(""), 0600)
	responseContent = b64_2
	if err := svc.Refresh("xray-sub"); err != nil {
		t.Fatalf("third Refresh failed: %v", err)
	}
	callsBytes, _ = os.ReadFile(logFile)
	if !strings.Contains(string(callsBytes), "-restart") {
		t.Error("xkeen -restart should be called when config changed")
	}
}

func TestRefreshMihomo_ConcurrentRace(t *testing.T) {
	yamlContent := `proxies:
  - {name: node1, type: ss, server: 1.2.3.4, port: 443, cipher: chacha20-ietf-poly1305, password: test}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)
	svc.httpClient = srv.Client()

	sub := Subscription{
		ID:           "race-sub",
		URL:          srv.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
	}
	if err := svc.Add(&sub); err != nil {
		t.Fatalf("Add subscription failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subCopy := svc.Get("race-sub")
			if subCopy != nil {
				body, headers, err := svc.downloadRaw(subCopy.URL, subCopy)
				if err == nil {
					_ = svc.refreshMihomo(subCopy, body, headers)
				}
			}
		}()
	}
	wg.Wait()
}

func TestSubscriptionService_MihomoAPIProviderReload(t *testing.T) {
	var calledPath string
	var calledMethod string
	var authHeader string

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		calledMethod = r.Method
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer apiServer.Close()

	tmp := t.TempDir()
	mihomoDir := filepath.Join(tmp, "mihomo")
	_ = os.MkdirAll(mihomoDir, 0755)
	configPath := filepath.Join(mihomoDir, "config.yaml")

	initialConfig := `port: 9090
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT
`
	_ = os.WriteFile(configPath, []byte(initialConfig), 0600)

	yamlContent := `proxies:
  - name: node1
    type: ss
    server: 1.2.3.4
    port: 443
    cipher: chacha20-ietf-poly1305
    password: test
`
	subServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(yamlContent))
	}))
	defer subServer.Close()

	svc := NewSubscriptionService(tmp, tmp, mihomoDir)
	svc.httpClient = subServer.Client()
	svc.SetMihomoAPI(apiServer.URL, "test-secret-token")

	sub := Subscription{
		ID:           "mihomo-reload-test",
		Name:         "Mihomo reload test",
		URL:          subServer.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
		Interval:     1,
		MihomoGroups: []string{"PROXY"},
	}
	_ = svc.Add(&sub)

	_ = svc.Refresh("mihomo-reload-test")

	calledPath = ""
	calledMethod = ""
	authHeader = ""

	err := svc.Refresh("mihomo-reload-test")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	providerName := GetMihomoProviderName("", sub.Name, sub.URL, sub.ID)
	expectedPath := "/providers/proxies/" + providerName

	if calledMethod != http.MethodPut {
		t.Errorf("expected Method PUT, got %q", calledMethod)
	}
	if calledPath != expectedPath {
		t.Errorf("expected Path %q, got %q", expectedPath, calledPath)
	}
	if authHeader != "Bearer test-secret-token" {
		t.Errorf("expected Authorization header 'Bearer test-secret-token', got %q", authHeader)
	}
}

func TestUniversalRefreshMihomo(t *testing.T) {
	rawOutboundLink := "vless://uuid-vless@vless.example.com:443?security=reality&pbk=pubkey&sid=shortid&sni=dest.com&type=grpc&serviceName=service-name#vless-node\n"
	base64Body := base64.StdEncoding.EncodeToString([]byte(rawOutboundLink))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(base64Body))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	mihomoDir := filepath.Join(tmp, "mihomo")
	_ = os.MkdirAll(mihomoDir, 0755)
	configPath := filepath.Join(mihomoDir, "config.yaml")

	initialConfig := `port: 9090
proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - DIRECT
`
	_ = os.WriteFile(configPath, []byte(initialConfig), 0600)

	svc := NewSubscriptionService(tmp, tmp, mihomoDir)
	svc.httpClient = srv.Client()

	sub := Subscription{
		ID:           "universal-sub",
		Name:         "Universal Sub Test",
		URL:          srv.URL,
		EnableMihomo: true,
		EnableXray:   false,
		Enabled:      true,
		Interval:     1,
		MihomoGroups: []string{"PROXY"},
	}
	_ = svc.Add(&sub)

	t.Logf("Base64 body: %s", base64Body)
	decodedBytes, _ := base64.StdEncoding.DecodeString(base64Body)
	t.Logf("Decoded body: %q", string(decodedBytes))
	parsedOb := parseShareLink(strings.TrimSpace(string(decodedBytes)))
	t.Logf("parseShareLink result: %+v", parsedOb)

	err := svc.Refresh("universal-sub")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configStr := string(configBytes)

	providerName := GetMihomoProviderName("", sub.Name, sub.URL, sub.ID)
	if !strings.Contains(configStr, "type: file") {
		t.Errorf("expected type: file in config.yaml, got:\n%s", configStr)
	}
	if strings.Contains(configStr, "            url:") || strings.Contains(configStr, "            interval:") {
		t.Errorf("provider in config.yaml should not contain HTTP properties, got:\n%s", configStr)
	}

	providerFilePath := filepath.Join(mihomoDir, "providers", providerName+".yaml")
	providerBytes, err := os.ReadFile(providerFilePath)
	if err != nil {
		t.Fatalf("provider file not found at %s: %v", providerFilePath, err)
	}
	providerStr := string(providerBytes)

	blocks, names := ParseMihomoSubscriptionBlocks(providerStr)
	if len(blocks) != 1 || names[0] != "vless-node" {
		t.Errorf("provider file has invalid content, got:\n%s", providerStr)
	}

	node := ParseClashProxyNode(blocks[0])
	if node.Tag != "vless-node" || node.Protocol != "vless" || node.Server != "vless.example.com:443" || node.Security != "reality" {
		t.Errorf("converted node parsed incorrectly: %+v", node)
	}
}
