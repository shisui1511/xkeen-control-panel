package services

import (
	"context"
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

func TestCountProviderNodes_HysteriaV1ShareLinks(t *testing.T) {
	payload := "hysteria://pass1@host1.example.com:443?sni=host1.example.com#node1\n" +
		"hysteria://pass2@host2.example.com:443?sni=host2.example.com#node2\n"
	count := countProviderNodes(payload)
	if count != 2 {
		t.Errorf("expected 2 hysteria:// nodes counted, got %d", count)
	}
}

func TestCountProviderNodes_Hysteria2Regression(t *testing.T) {
	payload := "hy2://pass1@host1.example.com:443#node1\n" +
		"hysteria2://pass2@host2.example.com:443#node2\n"
	count := countProviderNodes(payload)
	if count != 2 {
		t.Errorf("expected 2 hysteria2 nodes counted (regression), got %d", count)
	}
}

func TestCountProviderNodes_MixedHysteriaV1AndV2(t *testing.T) {
	payload := "hysteria://pass1@host1.example.com:443#v1\n" +
		"hysteria2://pass2@host2.example.com:443#v2\n" +
		"hy2://pass3@host3.example.com:443#v2b\n"
	count := countProviderNodes(payload)
	if count != 3 {
		t.Errorf("expected 3 nodes counted (1 hysteria + 2 hysteria2/hy2), got %d", count)
	}
}

func TestCountProviderNodes_ClashYAMLTypeHysteria(t *testing.T) {
	payload := "proxies:\n" +
		"  - name: node1\n" +
		"    type: hysteria\n" +
		"    server: host1.example.com\n" +
		"    port: 443\n" +
		"  - name: node2\n" +
		"    type: hysteria2\n" +
		"    server: host2.example.com\n" +
		"    port: 443\n"
	count := countProviderNodes(payload)
	if count != 2 {
		t.Errorf("expected 2 nodes counted for Clash YAML type: hysteria/hysteria2 (regression), got %d", count)
	}
}

func TestProviderPayload_XrayJSONWithHysteriaSurvivesEndToEnd(t *testing.T) {
	// Реальный формат провайдера: xray full-config array с protocol "hysteria".
	// Проверяем, что providerPayload не отбрасывает эту ноду и что итоговый
	// YAML (используется как Mihomo provider payload) содержит её.
	body := []byte(`[
		{
			"remarks": "Node HYSTERIA",
			"outbounds": [
				{
					"tag": "proxy",
					"protocol": "hysteria",
					"settings": {"address": "hy.example.com", "port": 9443, "version": 2},
					"streamSettings": {
						"network": "hysteria",
						"hysteriaSettings": {"version": 2, "auth": "secret-auth"},
						"security": "tls",
						"tlsSettings": {"serverName": "hy.example.com"}
					}
				},
				{"tag": "direct", "protocol": "freedom", "settings": {}}
			]
		}
	]`)

	payload, format := providerPayload(body)
	if format != "xray-json" {
		t.Fatalf("expected format=xray-json, got %q", format)
	}
	if payload == nil {
		t.Fatal("expected non-nil payload")
	}

	count := countProviderNodes(string(payload))
	if count == 0 {
		t.Errorf("expected countProviderNodes > 0 for hysteria-only provider payload, got 0 (payload would be treated as empty and dropped by ProviderFetch)")
	}
}

func TestProviderCacheIsolation(t *testing.T) {
	tmpDataDir := t.TempDir()
	mihomoDir := t.TempDir()
	xrayDir := t.TempDir()

	svc := NewSubscriptionService(tmpDataDir, xrayDir, mihomoDir)

	sub := &Subscription{
		ID:           "test-sub-1",
		Name:         "ProviderOne",
		URL:          "http://127.0.0.1:9999/sub",
		Enabled:      true,
		EnableMihomo: true,
	}
	if err := svc.Add(sub); err != nil {
		t.Fatalf("Add subscription failed: %v", err)
	}

	payload := []byte("proxies:\n  - name: node1\n    type: vmess\n    server: 1.2.3.4\n    port: 443\n")

	// 1. Cache payload and verify it's saved in {dataDir}/cache/providers/{providerName}.yaml with 0600
	if err := svc.cacheProviderPayload(sub, payload); err != nil {
		t.Fatalf("cacheProviderPayload failed: %v", err)
	}

	cachePath := svc.providerCachePath(sub)
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("cache file not found at %s: %v", cachePath, err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected permissions 0600, got %o", perm)
	}

	// 2. Test legacy fallback reading when {dataDir}/cache/providers is missing
	_ = os.Remove(cachePath)
	legacyPath := svc.legacyProviderCachePath(sub)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatal(err)
	}
	legacyPayload := []byte("proxies:\n  - name: legacyNode\n    type: vmess\n    server: 5.6.7.8\n    port: 443\n")
	if err := os.WriteFile(legacyPath, legacyPayload, 0600); err != nil {
		t.Fatal(err)
	}

	// ProviderFetchWithFallback with unreachable upstream should read legacyPath
	fallbackData, err := svc.ProviderFetchWithFallback(context.Background(), sub.URL, sub)
	if err != nil {
		t.Fatalf("ProviderFetchWithFallback failed on legacy fallback: %v", err)
	}
	if string(fallbackData) != string(legacyPayload) {
		t.Errorf("expected fallback data %q, got %q", string(legacyPayload), string(fallbackData))
	}

	// 3. Test Delete removes both cache path and legacy path
	// Recreate cachePath so both exist
	if err := svc.cacheProviderPayload(sub, payload); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(sub.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("cache file %s was not removed on delete", cachePath)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy cache file %s was not removed on delete", legacyPath)
	}
}

func TestHTMLStubDetection_Mihomo(t *testing.T) {
	tmpDataDir := t.TempDir()
	mihomoDir := t.TempDir()
	xrayDir := t.TempDir()

	htmlBody := `<!DOCTYPE html>
<html>
<head><title>Cloudflare Protection / Login Required</title></head>
<body><h1>Please verify your access or renewal</h1></body>
</html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlBody))
	}))
	defer ts.Close()

	svc := NewSubscriptionService(tmpDataDir, xrayDir, mihomoDir)
	svc.httpClient = ts.Client()

	sub := &Subscription{
		ID:           "html-sub",
		Name:         "HTML Provider",
		URL:          ts.URL,
		Enabled:      true,
		EnableMihomo: true,
	}
	if err := svc.Add(sub); err != nil {
		t.Fatal(err)
	}

	// 1. Without cache: should return error and set LastError
	_, err := svc.ProviderFetchWithFallback(context.Background(), sub.URL, sub)
	if err == nil {
		t.Fatal("expected ProviderFetchWithFallback to fail on HTML stub with no cache")
	}
	live := svc.Get(sub.ID)
	if live == nil || !strings.Contains(live.LastError, "HTML landing page") {
		t.Errorf("expected LastError to mention HTML landing page, got %q", live.LastError)
	}

	// 2. With valid cache: should serve cached payload and retain LastError
	cachedPayload := []byte("proxies:\n  - name: cachedNode1\n    type: vless\n    server: 1.1.1.1\n    port: 443\n")
	if err := svc.cacheProviderPayload(sub, cachedPayload); err != nil {
		t.Fatal(err)
	}

	data, err := svc.ProviderFetchWithFallback(context.Background(), sub.URL, sub)
	if err != nil {
		t.Fatalf("expected fallback to return cache, got error: %v", err)
	}
	if string(data) != string(cachedPayload) {
		t.Errorf("expected cached payload %q, got %q", string(cachedPayload), string(data))
	}
	liveAfter := svc.Get(sub.ID)
	if liveAfter.LastCount != 1 {
		t.Errorf("expected LastCount to be 1 from cache, got %d", liveAfter.LastCount)
	}
	if !strings.Contains(liveAfter.LastError, "HTML landing page") {
		t.Errorf("expected LastError to be preserved, got %q", liveAfter.LastError)
	}
}

func TestZeroNodesDetection(t *testing.T) {
	tmpDataDir := t.TempDir()
	mihomoDir := t.TempDir()
	xrayDir := t.TempDir()

	emptyYAML := "proxies: []\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(emptyYAML))
	}))
	defer ts.Close()

	svc := NewSubscriptionService(tmpDataDir, xrayDir, mihomoDir)
	svc.httpClient = ts.Client()

	sub := &Subscription{
		ID:           "zero-nodes-sub",
		Name:         "Zero Nodes",
		URL:          ts.URL,
		Enabled:      true,
		EnableMihomo: true,
	}
	if err := svc.Add(sub); err != nil {
		t.Fatal(err)
	}

	// With cache: should serve cache and set LastError
	validCache := []byte("proxies:\n  - name: nodeA\n    type: ss\n    server: 2.2.2.2\n    port: 8388\n")
	if err := svc.cacheProviderPayload(sub, validCache); err != nil {
		t.Fatal(err)
	}

	data, err := svc.ProviderFetchWithFallback(context.Background(), sub.URL, sub)
	if err != nil {
		t.Fatalf("expected fallback to return cache, got error: %v", err)
	}
	if string(data) != string(validCache) {
		t.Errorf("expected valid cache returned, got %q", string(data))
	}
	live := svc.Get(sub.ID)
	if !strings.Contains(live.LastError, "0 valid nodes") {
		t.Errorf("expected LastError to mention 0 valid nodes, got %q", live.LastError)
	}
}

func TestSingleflightDeduplication(t *testing.T) {
	tmpDataDir := t.TempDir()
	mihomoDir := t.TempDir()
	xrayDir := t.TempDir()

	var reqCount int32
	validYAML := "proxies:\n  - name: sfNode\n    type: vmess\n    server: 3.3.3.3\n    port: 443\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		time.Sleep(50 * time.Millisecond) // simulate latency
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(validYAML))
	}))
	defer ts.Close()

	svc := NewSubscriptionService(tmpDataDir, xrayDir, mihomoDir)
	svc.httpClient = ts.Client()

	sub := &Subscription{
		ID:           "sf-sub",
		Name:         "SF Sub",
		URL:          ts.URL,
		Enabled:      true,
		EnableMihomo: true,
	}
	if err := svc.Add(sub); err != nil {
		t.Fatal(err)
	}

	const concurrent = 10
	var wg sync.WaitGroup
	wg.Add(concurrent)
	errs := make([]error, concurrent)

	for i := 0; i < concurrent; i++ {
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.ProviderFetch(context.Background(), sub.URL, sub)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d failed: %v", i, err)
		}
	}

	count := atomic.LoadInt32(&reqCount)
	if count > 2 {
		t.Errorf("expected singleflight to deduplicate requests (expected ~1, max 2), got %d upstream hits", count)
	}
}

func TestLastErrorClearedOnSuccess(t *testing.T) {
	tmpDataDir := t.TempDir()
	mihomoDir := t.TempDir()
	xrayDir := t.TempDir()

	var shouldFail bool = true
	validYAML := "proxies:\n  - name: successNode\n    type: vmess\n    server: 4.4.4.4\n    port: 443\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFail {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(validYAML))
	}))
	defer ts.Close()

	svc := NewSubscriptionService(tmpDataDir, xrayDir, mihomoDir)
	svc.httpClient = ts.Client()

	sub := &Subscription{
		ID:           "clear-err-sub",
		Name:         "Clear Err Sub",
		URL:          ts.URL,
		Enabled:      true,
		EnableMihomo: true,
	}
	if err := svc.Add(sub); err != nil {
		t.Fatal(err)
	}

	// 1. First fetch fails
	_, err := svc.ProviderFetchWithFallback(context.Background(), sub.URL, sub)
	if err == nil {
		t.Fatal("expected error on first fetch")
	}
	live := svc.Get(sub.ID)
	if live.LastError == "" {
		t.Fatal("expected LastError to be populated")
	}

	// 2. Next fetch succeeds
	shouldFail = false
	payload, err := svc.ProviderFetchWithFallback(context.Background(), sub.URL, sub)
	if err != nil {
		t.Fatalf("expected success on second fetch: %v", err)
	}
	if string(payload) != validYAML {
		t.Errorf("expected payload %q, got %q", validYAML, string(payload))
	}

	liveAfter := svc.Get(sub.ID)
	if liveAfter.LastError != "" {
		t.Errorf("expected LastError to be cleared to empty string, got %q", liveAfter.LastError)
	}
	if liveAfter.LastCount != 1 {
		t.Errorf("expected LastCount 1, got %d", liveAfter.LastCount)
	}
}
