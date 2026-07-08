package services

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// mockConn реализует net.Conn для тестирования SubscriptionHealthService.
type mockConn struct {
	net.Conn
}

func (m *mockConn) Close() error {
	return nil
}

func TestMockSubscriptionFormats(t *testing.T) {
	// 1. Инициализация mock HTTP-сервера, отдающего 6 форматов подписок.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=2147483648; download=4294967296; total=10737418240; expire=1893456000")
		w.Header().Set("profile-title", "base64:VGVzdCBTdWJzY3JpcHRpb24=") // "Test Subscription"

		switch r.URL.Path {
		case "/remnawave":
			w.Header().Set("Content-Type", "application/json")
			data, _ := os.ReadFile("testdata/remnawave.json")
			w.Write(data)
		case "/marzban":
			w.Header().Set("Content-Type", "text/plain")
			data, _ := os.ReadFile("testdata/marzban.txt")
			w.Write(data)
		case "/3xui":
			w.Header().Set("Content-Type", "text/plain")
			data, _ := os.ReadFile("testdata/3xui.txt")
			w.Write(data)
		case "/xui":
			w.Header().Set("Content-Type", "text/plain")
			data, _ := os.ReadFile("testdata/xui.txt")
			w.Write(data)
		case "/singbox":
			w.Header().Set("Content-Type", "application/json")
			data, _ := os.ReadFile("testdata/singbox_generic.json")
			w.Write(data)
		case "/clash":
			w.Header().Set("Content-Type", "application/yaml")
			yamlContent := `proxies:
  - name: "Clash-Proxy-1"
    type: ss
    server: clash.example.com
    port: 8388
    cipher: aes-256-gcm
    password: test
rules:
  - MATCH,Clash-Proxy-1
`
			w.Write([]byte(yamlContent))
		case "/share-links":
			w.Header().Set("Content-Type", "text/plain")
			links := "vless://550e8400-e29b-41d4-a716-446655440000@1.2.3.4:443?security=none#Plain-VLESS\nss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@1.2.3.4:8388#Plain-SS"
			w.Write([]byte(links))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// 2. Тестирование каждого формата
	tests := []struct {
		name           string
		path           string
		subType        string
		expectedFormat string
		expectedCount  int
	}{
		{"Remnawave Xray-JSON", "/remnawave", "xray", "xray-json", 1},
		{"Marzban base64", "/marzban", "xray", "base64", 2},
		{"3X-UI base64", "/3xui", "xray", "base64", 4},
		{"X-UI plain", "/xui", "xray", "share-links", 4},
		{"Sing-box JSON", "/singbox", "xray", "sing-box", 2},
		{"Clash YAML", "/clash", "mihomo", "clash-meta", 1},
		{"Plain share-links", "/share-links", "xray", "share-links", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subTmp := t.TempDir()
			svc := NewSubscriptionService(subTmp, subTmp, subTmp)
			svc.httpClient = ts.Client()

			enableXray := tt.subType == "xray"
			enableMihomo := tt.subType == "mihomo"

			sub := Subscription{
				Name:         tt.name,
				URL:          ts.URL + tt.path,
				EnableXray:   enableXray,
				EnableMihomo: enableMihomo,
				Enabled:      true,
			}
			err := svc.Add(&sub)
			if err != nil {
				t.Fatalf("Add failed: %v", err)
			}

			id := sub.ID
			err = svc.Refresh(id)
			if err != nil {
				t.Fatalf("Refresh failed: %v", err)
			}

			got := svc.Get(id)

			if got.DetectedFormat != tt.expectedFormat {
				t.Errorf("expected format %q, got %q", tt.expectedFormat, got.DetectedFormat)
			}
			if got.LastCount != tt.expectedCount {
				t.Errorf("expected proxy count %d, got %d", tt.expectedCount, got.LastCount)
			}

			// Проверяем заполнение метаданных лимитов из заголовков
			if got.Upload != 2147483648 || got.Download != 4294967296 || got.Total != 10737418240 {
				t.Errorf("userinfo headers not parsed correctly: upload=%d, download=%d, total=%d", got.Upload, got.Download, got.Total)
			}
			if got.ProfileTitle != "Test Subscription" {
				t.Errorf("expected ProfileTitle='Test Subscription', got %q", got.ProfileTitle)
			}
		})
	}
}

func TestSmokeFixturesDirectParsing(t *testing.T) {
	// Тестируем прямой разбор фикстурных файлов без HTTP.
	tests := []struct {
		file           string
		expectedFormat string
		expectedCount  int
	}{
		{"testdata/remnawave.json", "xray-json", 1},
		{"testdata/marzban.txt", "base64", 2},
		{"testdata/3xui.txt", "base64", 4},
		{"testdata/xui.txt", "share-links", 4},
		{"testdata/singbox_generic.json", "sing-box", 2},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("ReadFile failed: %v", err)
			}

			sub := &Subscription{}
			outbounds, skipped, err := parseSubscriptionBody(data, "", sub)
			if err != nil {
				t.Fatalf("parseSubscriptionBody failed: %v", err)
			}

			if sub.DetectedFormat != tt.expectedFormat {
				t.Errorf("expected format %q, got %q", tt.expectedFormat, sub.DetectedFormat)
			}

			totalCount := len(outbounds)
			if tt.expectedFormat == "base64" || tt.expectedFormat == "share-links" {
				totalCount = sub.LastCount
			}
			if totalCount != tt.expectedCount {
				t.Errorf("expected count %d, got %d", tt.expectedCount, totalCount)
			}
			_ = skipped
		})
	}
}

func TestPerNodeModelNameRetention(t *testing.T) {
	tmp := t.TempDir()
	svc := NewSubscriptionService(tmp, tmp, tmp)

	// Добавляем подписку с vless-ссылкой, содержащей сложные метаданные в remark.
	remark := "DE-Frankfurt [NEW] (Youtube, Instagram) 10Gb/s"
	link := "vless://550e8400-e29b-41d4-a716-446655440000@de.example.com:443?security=reality&pbk=pubkey123&sid=shortid456&sni=sni.example.com&fp=chrome&flow=xtls-rprx-vision#" + remark

	outbounds := []Outbound{*parseShareLink(link)}
	sub := &Subscription{ID: "test_sub", Name: "Test Sub"}

	fragmentPath := svc.getFragmentPath(sub)
	nodes, err := svc.writeFragment(fragmentPath, outbounds, sub)
	if err != nil {
		t.Fatalf("writeFragment failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	// Проверяем, что Name не равен исходному сырому remark, а очищен
	if node.Name != "Frankfurt" {
		t.Errorf("expected Name to be 'Frankfurt', got %q", node.Name)
	}
	if node.Country != "DE" {
		t.Errorf("expected Country to be 'DE', got %q", node.Country)
	}
	if node.Flag != "🇩🇪" {
		t.Errorf("expected Flag to be '🇩🇪', got %q", node.Flag)
	}
	if node.UseCase != "Youtube, Instagram" {
		t.Errorf("expected UseCase to be 'Youtube, Instagram', got %q", node.UseCase)
	}
	if node.Speed != "10Gb/s" {
		t.Errorf("expected Speed to be '10Gb/s', got %q", node.Speed)
	}
	if !node.IsNew {
		t.Errorf("expected IsNew to be true")
	}

	// Читаем сгенерированный фрагмент и проверяем, что тег в outbound равен remark для совместимости с Xray маршрутизацией
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("failed to read fragment: %v", err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("failed to unmarshal outbounds: %v", err)
	}

	if wrapper.Outbounds[0].Tag != remark {
		t.Errorf("expected outbound Tag to retain original remark %q, got %q", remark, wrapper.Outbounds[0].Tag)
	}
}

func TestSubscriptionHealthService_MockDial(t *testing.T) {
	tmp := t.TempDir()
	subSvc := NewSubscriptionService(tmp, tmp, tmp)

	healthSvc := NewSubscriptionHealthService(tmp, subSvc)
	// Устанавливаем детерминированный mock dialer с эмуляцией latency
	healthSvc.dialFunc = func(network, address string, timeout time.Duration) (net.Conn, error) {
		time.Sleep(30 * time.Millisecond) // deterministic delay
		return &mockConn{}, nil
	}

	sub := &Subscription{
		ID:   "health_test",
		Name: "Health Test",
		Nodes: []SubscriptionNode{
			{Tag: "node1", Server: "1.2.3.4:443"},
			{Tag: "node2", Server: "5.6.7.8:443"},
		},
	}
	subSvc.Add(sub)

	// Выполняем проверку здоровья
	healthSvc.ForceCheck("health_test")

	results := healthSvc.GetHealth("health_test")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	h1, ok := results["node1"]
	if !ok {
		t.Fatal("node1 result not found")
	}
	if !h1.Alive {
		t.Error("expected node1 to be alive")
	}
	// LatencyMs должно быть больше или равно 30мс
	if h1.LatencyMs < 30 {
		t.Errorf("expected latency >= 30ms, got %d", h1.LatencyMs)
	}

	// Тестируем одиночную проверку ForceCheckNode
	h2, exists := healthSvc.ForceCheckNode("health_test", "node2")
	if !exists {
		t.Fatal("node2 check failed")
	}
	if !h2.Alive || h2.LatencyMs < 30 {
		t.Errorf("node2 health details invalid: alive=%v, latency=%d", h2.Alive, h2.LatencyMs)
	}
}
