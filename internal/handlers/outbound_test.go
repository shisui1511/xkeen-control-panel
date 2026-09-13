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

func TestOutboundImport_Comprehensive(t *testing.T) {
	api, xrayDir := newOutboundTestAPI(t)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/outbound/import", nil)
	recGet := httptest.NewRecorder()
	api.OutboundImport(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewBufferString("{bad"))
	recBadJSON := httptest.NewRecorder()
	api.OutboundImport(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. Empty link
	bodyEmpty, _ := json.Marshal(OutboundImportRequest{Link: ""})
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyEmpty))
	recEmpty := httptest.NewRecorder()
	api.OutboundImport(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty link, got %d", recEmpty.Code)
	}

	// 4. Too long link (>16384)
	longLink := "vless://" + strings.Repeat("a", 16385)
	bodyLong, _ := json.Marshal(OutboundImportRequest{Link: longLink})
	reqLong := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyLong))
	recLong := httptest.NewRecorder()
	api.OutboundImport(recLong, reqLong)
	if recLong.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for link >16384, got %d", recLong.Code)
	}

	// 5. Too long vmess link (>8192)
	longVmess := "vmess://" + strings.Repeat("a", 8193)
	bodyVmess, _ := json.Marshal(OutboundImportRequest{Link: longVmess})
	reqVmess := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyVmess))
	recVmess := httptest.NewRecorder()
	api.OutboundImport(recVmess, reqVmess)
	if recVmess.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for vmess >8192, got %d", recVmess.Code)
	}

	// 6. Invalid link format
	bodyInvalid, _ := json.Marshal(OutboundImportRequest{Link: "invalid-scheme://foo"})
	reqInvalid := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyInvalid))
	recInvalid := httptest.NewRecorder()
	api.OutboundImport(recInvalid, reqInvalid)
	if recInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid link, got %d", recInvalid.Code)
	}

	// 7. Successful import when manual file does not exist
	validLink := "vless://550e8400-e29b-41d4-a716-446655440000@host.com:443?security=none#node-init"
	bodyGood, _ := json.Marshal(OutboundImportRequest{Link: validLink, Tag: "custom-tag-1"})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyGood))
	recGood := httptest.NewRecorder()
	api.OutboundImport(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for good import, got %d: %s", recGood.Code, recGood.Body.String())
	}

	manualPath := filepath.Join(xrayDir, "04_outbounds.manual.json")
	data, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("expected manual file to exist, err: %v", err)
	}
	var wrapper struct {
		Outbounds []services.Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("failed to decode manual outbounds: %v", err)
	}
	if len(wrapper.Outbounds) != 1 || wrapper.Outbounds[0].Tag != "custom-tag-1" {
		t.Fatalf("expected 1 outbound with tag custom-tag-1, got: %+v", wrapper.Outbounds)
	}

	// 8. Update existing node with same tag
	updatedLink := "vless://550e8400-e29b-41d4-a716-446655440000@host2.com:443?security=none#node-init"
	bodyUpdate, _ := json.Marshal(OutboundImportRequest{Link: updatedLink, Tag: "custom-tag-1"})
	reqUpdate := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyUpdate))
	recUpdate := httptest.NewRecorder()
	api.OutboundImport(recUpdate, reqUpdate)
	if recUpdate.Code != http.StatusOK {
		t.Errorf("expected 200 for update, got %d", recUpdate.Code)
	}

	// 9. Append new node with different tag
	newLink := "vless://550e8400-e29b-41d4-a716-446655440000@host3.com:443?security=none#node-init"
	bodyAppend, _ := json.Marshal(OutboundImportRequest{Link: newLink, Tag: "custom-tag-2"})
	reqAppend := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyAppend))
	recAppend := httptest.NewRecorder()
	api.OutboundImport(recAppend, reqAppend)
	if recAppend.Code != http.StatusOK {
		t.Errorf("expected 200 for append, got %d", recAppend.Code)
	}

	dataAfter, _ := os.ReadFile(manualPath)
	_ = json.Unmarshal(dataAfter, &wrapper)
	if len(wrapper.Outbounds) != 2 {
		t.Errorf("expected 2 outbounds after append, got %d", len(wrapper.Outbounds))
	}

	// 10. Corrupted manual file returns 500
	_ = os.WriteFile(manualPath, []byte("corrupted json content"), 0644)
	reqCorrupt := httptest.NewRequest(http.MethodPost, "/api/outbound/import", bytes.NewReader(bodyAppend))
	recCorrupt := httptest.NewRecorder()
	api.OutboundImport(recCorrupt, reqCorrupt)
	if recCorrupt.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for corrupted manual file, got %d", recCorrupt.Code)
	}
}

func TestOutboundImportBulk_Comprehensive(t *testing.T) {
	api, xrayDir := newOutboundTestAPI(t)

	// 1. Method Not Allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/outbound/import-bulk", nil)
	recGet := httptest.NewRecorder()
	api.OutboundImportBulk(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", recGet.Code)
	}

	// 2. Invalid JSON
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewBufferString("{bad"))
	recBadJSON := httptest.NewRecorder()
	api.OutboundImportBulk(recBadJSON, reqBadJSON)
	if recBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad JSON, got %d", recBadJSON.Code)
	}

	// 3. Empty items
	bodyEmpty, _ := json.Marshal(OutboundImportBulkRequest{})
	reqEmpty := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyEmpty))
	recEmpty := httptest.NewRecorder()
	api.OutboundImportBulk(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty items, got %d", recEmpty.Code)
	}

	// 4. Too many items (>200)
	tooMany := make([]OutboundImportBulkItem, 201)
	for i := range tooMany {
		tooMany[i] = OutboundImportBulkItem{Link: "vless://uuid@host:443#tag"}
	}
	bodyTooMany, _ := json.Marshal(OutboundImportBulkRequest{Items: tooMany})
	reqTooMany := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyTooMany))
	recTooMany := httptest.NewRecorder()
	api.OutboundImportBulk(recTooMany, reqTooMany)
	if recTooMany.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for >200 items, got %d", recTooMany.Code)
	}

	// 5. Item with empty link
	bodyItemEmpty, _ := json.Marshal(OutboundImportBulkRequest{
		Items: []OutboundImportBulkItem{{Link: ""}},
	})
	reqItemEmpty := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyItemEmpty))
	recItemEmpty := httptest.NewRecorder()
	api.OutboundImportBulk(recItemEmpty, reqItemEmpty)
	if recItemEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty link item, got %d", recItemEmpty.Code)
	}

	// 6. Item with too long link
	bodyItemLong, _ := json.Marshal(OutboundImportBulkRequest{
		Items: []OutboundImportBulkItem{{Link: "vless://" + strings.Repeat("a", 16385)}},
	})
	reqItemLong := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyItemLong))
	recItemLong := httptest.NewRecorder()
	api.OutboundImportBulk(recItemLong, reqItemLong)
	if recItemLong.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for item >16384, got %d", recItemLong.Code)
	}

	// 7. Item with vmess too long
	bodyItemVmess, _ := json.Marshal(OutboundImportBulkRequest{
		Items: []OutboundImportBulkItem{{Link: "vmess://" + strings.Repeat("a", 8193)}},
	})
	reqItemVmess := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyItemVmess))
	recItemVmess := httptest.NewRecorder()
	api.OutboundImportBulk(recItemVmess, reqItemVmess)
	if recItemVmess.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for vmess item >8192, got %d", recItemVmess.Code)
	}

	// 8. Item with invalid format
	bodyItemInvalid, _ := json.Marshal(OutboundImportBulkRequest{
		Items: []OutboundImportBulkItem{{Link: "invalid-link"}},
	})
	reqItemInvalid := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyItemInvalid))
	recItemInvalid := httptest.NewRecorder()
	api.OutboundImportBulk(recItemInvalid, reqItemInvalid)
	if recItemInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid item format, got %d", recItemInvalid.Code)
	}

	// 9. Batch with duplicate tags
	vlessA := "vless://550e8400-e29b-41d4-a716-446655440000@host1.com:443?security=none#common-tag"
	vlessB := "vless://550e8400-e29b-41d4-a716-446655440000@host2.com:443?security=none#common-tag"
	bodyDup, _ := json.Marshal(OutboundImportBulkRequest{
		Items: []OutboundImportBulkItem{
			{Link: vlessA},
			{Link: vlessB},
		},
	})
	reqDup := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyDup))
	recDup := httptest.NewRecorder()
	api.OutboundImportBulk(recDup, reqDup)
	if recDup.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for duplicate tags in batch, got %d", recDup.Code)
	}

	// 10. Successful bulk import
	bodyGood, _ := json.Marshal(OutboundImportBulkRequest{
		Items: []OutboundImportBulkItem{
			{Link: vlessA, Tag: "tag-1"},
			{Link: vlessB, Tag: "tag-2"},
		},
	})
	reqGood := httptest.NewRequest(http.MethodPost, "/api/outbound/import-bulk", bytes.NewReader(bodyGood))
	recGood := httptest.NewRecorder()
	api.OutboundImportBulk(recGood, reqGood)
	if recGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for good bulk import, got %d: %s", recGood.Code, recGood.Body.String())
	}

	manualPath := filepath.Join(xrayDir, "04_outbounds.manual.json")
	data, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("failed to read manual outbounds: %v", err)
	}
	var wrapper struct {
		Outbounds []services.Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("failed to parse manual outbounds: %v", err)
	}
	if len(wrapper.Outbounds) != 2 {
		t.Errorf("expected 2 outbounds, got %d", len(wrapper.Outbounds))
	}
}
