package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func newOutboundTestAPI(t *testing.T) (*API, string) {
	t.Helper()
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		XRayConfigDir: xrayDir,
		AllowedRoots:  []string{tmpDir},
	}
	pathVal := utils.NewPathValidator([]string{tmpDir})
	configSvc := services.NewConfigService(xrayDir, []string{tmpDir})
	subSvc := services.NewSubscriptionService(tmpDir, xrayDir, tmpDir)

	api := &API{
		cfg:             cfg,
		pathVal:         pathVal,
		configSvc:       configSvc,
		subscriptionSvc: subSvc,
		kernelSvc:       services.NewKernelService(t.TempDir()),
		consoleSvc:      services.NewConsoleService("/bin/true"),
	}

	return api, xrayDir
}

func TestOutboundParse_Comprehensive(t *testing.T) {
	api, _ := newOutboundTestAPI(t)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/outbound/parse", nil)
	recGet := httptest.NewRecorder()
	api.OutboundParse(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewBufferString("{invalid"))
	recBadJSON := httptest.NewRecorder()
	api.OutboundParse(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. Empty input
	bodyEmpty, _ := json.Marshal(OutboundParseRequest{})
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(bodyEmpty))
	recEmpty := httptest.NewRecorder()
	api.OutboundParse(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty links/text, got %d", recEmpty.Code)
	}

	// 4. Too many links (>200)
	tooMany := make([]string, 201)
	for i := range tooMany {
		tooMany[i] = "vless://uuid@host:443#tag"
	}
	bodyTooMany, _ := json.Marshal(OutboundParseRequest{Links: tooMany})
	reqTooMany := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(bodyTooMany))
	recTooMany := httptest.NewRecorder()
	api.OutboundParse(recTooMany, reqTooMany)
	if recTooMany.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for >200 links, got %d", recTooMany.Code)
	}

	// 5. wg-quick conf via Text
	wgConf := `[Interface]
PrivateKey = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=
Address = 10.0.0.2/32
[Peer]
PublicKey = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=
Endpoint = 1.2.3.4:51820
`
	bodyWG, _ := json.Marshal(OutboundParseRequest{Text: wgConf})
	reqWG := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(bodyWG))
	recWG := httptest.NewRecorder()
	api.OutboundParse(recWG, reqWG)
	if recWG.Code != http.StatusOK {
		t.Errorf("expected 200 for wg-quick conf text, got %d: %s", recWG.Code, recWG.Body.String())
	}

	// 6. wg-quick conf via Links[0]
	bodyWGLink, _ := json.Marshal(OutboundParseRequest{Links: []string{wgConf}})
	reqWGLink := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(bodyWGLink))
	recWGLink := httptest.NewRecorder()
	api.OutboundParse(recWGLink, reqWGLink)
	if recWGLink.Code != http.StatusOK {
		t.Errorf("expected 200 for wg-quick conf link, got %d: %s", recWGLink.Code, recWGLink.Body.String())
	}

	// 7. Plain text newline separated
	vless1 := "vless://550e8400-e29b-41d4-a716-446655440000@host1.com:443?security=none#node1"
	vless2 := "vless://550e8400-e29b-41d4-a716-446655440000@host2.com:443?security=none#node2"
	bodyText, _ := json.Marshal(OutboundParseRequest{Text: vless1 + "\n\n" + vless2})
	reqText := httptest.NewRequest(http.MethodPost, "/api/outbound/parse", bytes.NewReader(bodyText))
	recText := httptest.NewRecorder()
	api.OutboundParse(recText, reqText)
	if recText.Code != http.StatusOK {
		t.Errorf("expected 200 for text links, got %d: %s", recText.Code, recText.Body.String())
	}
}
