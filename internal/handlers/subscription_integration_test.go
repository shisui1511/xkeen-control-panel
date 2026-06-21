package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestSubscriptionE2E(t *testing.T) {
	// 1. Создаем mock HTTP-сервер для эмуляции провайдера подписки
	mockLinks := "vless://550e8400-e29b-41d4-a716-446655440000@us.example.com:443?security=none#US-New York (ChatGPT)\nvmess://eyJhZGQiOiJubC5leGFtcGxlLmNvbSIsInBvcnQiOiI4MDgwIiwiaWQiOiI1NTBlODQwMC1lMjliLTQxZDQtYTcxNi00NDY2NTU0NDAwMDAiLCJwcyI6Ik5MIC0gQW1zdGVyZGFtIChHYW1pbmcpIiwibmV0Ijoid3MiLCJwYXRoIjoiL3dzIn0="
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=100; download=200; total=1000")
		w.Header().Set("profile-title", "base64:TXkgVlBO") // "My VPN"
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockLinks))
	}))
	defer ts.Close()

	// 2. Инициализация временных директорий и сервисов
	tmp := t.TempDir()
	xrayDir := filepath.Join(tmp, "xray")
	mihomoDir := filepath.Join(tmp, "mihomo")
	err := os.MkdirAll(xrayDir, 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(mihomoDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	subSvc := services.NewSubscriptionService(tmp, xrayDir, mihomoDir)
	subSvc.SetHTTPClient(ts.Client()) // используем http-клиент mock-сервера

	cfg := &config.Config{
		DataDir:         tmp,
		XRayConfigDir:   xrayDir,
		MihomoConfigDir: mihomoDir,
	}

	api := &API{
		cfg:             cfg,
		subscriptionSvc: subSvc,
	}

	// 3. Добавление новой подписки через API (POST /api/subscriptions/add)
	addPayload := map[string]interface{}{
		"name":        "Integration Sub",
		"url":         ts.URL,
		"enable_xray": true,
		"enabled":     true,
		"interval":    12,
		"tag_prefix":  "test-pfx",
	}
	payloadBytes, _ := json.Marshal(addPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/add", bytes.NewReader(payloadBytes))
	rr := httptest.NewRecorder()

	api.SubscriptionAdd(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SubscriptionAdd failed: status %d, body %s", rr.Code, rr.Body.String())
	}

	var addedSub services.Subscription
	err = json.Unmarshal(rr.Body.Bytes(), &addedSub)
	if err != nil {
		t.Fatalf("failed to decode added subscription: %v", err)
	}

	id := addedSub.ID
	if id == "" {
		t.Fatal("expected non-empty ID for added subscription")
	}

	// 4. Принудительное обновление подписки через API (POST /api/subscriptions/refresh?id=...)
	req = httptest.NewRequest(http.MethodPost, "/api/subscriptions/refresh?id="+id, nil)
	rr = httptest.NewRecorder()

	api.SubscriptionRefresh(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SubscriptionRefresh failed: status %d, body %s", rr.Code, rr.Body.String())
	}

	var refreshedSub services.Subscription
	err = json.Unmarshal(rr.Body.Bytes(), &refreshedSub)
	if err != nil {
		t.Fatalf("failed to decode refreshed subscription: %v", err)
	}

	if refreshedSub.LastCount != 2 {
		t.Errorf("expected 2 proxies, got %d", refreshedSub.LastCount)
	}
	if refreshedSub.DetectedFormat != "share-links" {
		t.Errorf("expected format 'share-links', got %q", refreshedSub.DetectedFormat)
	}
	if refreshedSub.ProfileTitle != "My VPN" {
		t.Errorf("expected profile title 'My VPN', got %q", refreshedSub.ProfileTitle)
	}

	// 5. Проверка генерации файлов конфигурации Xray
	fragmentFile := filepath.Join(xrayDir, "04_outbounds."+id+".json")
	if _, err := os.Stat(fragmentFile); os.IsNotExist(err) {
		t.Fatalf("fragment file %s was not created", fragmentFile)
	}

	fragBytes, err := os.ReadFile(fragmentFile)
	if err != nil {
		t.Fatalf("failed to read fragment file: %v", err)
	}

	var xrayWrapper struct {
		Outbounds []services.Outbound `json:"outbounds"`
	}
	err = json.Unmarshal(fragBytes, &xrayWrapper)
	if err != nil {
		t.Fatalf("failed to unmarshal xray fragment: %v", err)
	}

	if len(xrayWrapper.Outbounds) != 2 {
		t.Fatalf("expected 2 outbounds in xray config fragment, got %d", len(xrayWrapper.Outbounds))
	}

	// Проверяем работу tagPrefix
	if !strings.HasPrefix(xrayWrapper.Outbounds[0].Tag, "test-pfx-") {
		t.Errorf("expected tag prefix 'test-pfx-' in tag %q", xrayWrapper.Outbounds[0].Tag)
	}

	// 6. Получение списка нод (GET /api/subscriptions/nodes?id=...)
	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions/nodes?id="+id, nil)
	rr = httptest.NewRecorder()

	api.SubscriptionNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SubscriptionNodes failed: status %d, body %s", rr.Code, rr.Body.String())
	}

	var nodes []services.SubscriptionNode
	err = json.Unmarshal(rr.Body.Bytes(), &nodes)
	if err != nil {
		t.Fatalf("failed to decode nodes: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	// Проверяем метаданные первой ноды (US-New York (ChatGPT))
	if nodes[0].Name != "New York" || nodes[0].Country != "US" || nodes[0].Flag != "🇺🇸" || nodes[0].UseCase != "ChatGPT" {
		t.Errorf("invalid metadata for node 0: %+v", nodes[0])
	}

	// 7. Получение отчета о парсинге (GET /api/subscriptions/parse-report?id=...)
	req = httptest.NewRequest(http.MethodGet, "/api/subscriptions/parse-report?id="+id, nil)
	rr = httptest.NewRecorder()

	api.SubscriptionParseReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SubscriptionParseReport failed: status %d, body %s", rr.Code, rr.Body.String())
	}

	var report services.ParseReport
	err = json.Unmarshal(rr.Body.Bytes(), &report)
	if err != nil {
		t.Fatalf("failed to decode parse report: %v", err)
	}

	if report.ParsedCount != 2 || report.SkippedCount != 0 {
		t.Errorf("invalid parse report: parsed=%d, skipped=%d", report.ParsedCount, report.SkippedCount)
	}

	// 8. Удаление подписки (POST /api/subscriptions/delete?id=...)
	req = httptest.NewRequest(http.MethodPost, "/api/subscriptions/delete?id="+id, nil)
	rr = httptest.NewRecorder()

	api.SubscriptionDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SubscriptionDelete failed: status %d, body %s", rr.Code, rr.Body.String())
	}

	// Проверяем, что файл конфигурации Xray удален
	if _, err := os.Stat(fragmentFile); !os.IsNotExist(err) {
		t.Fatalf("fragment file %s was not deleted after subscription deletion", fragmentFile)
	}
}
