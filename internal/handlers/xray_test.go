package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
	logpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/log/command"
	routerpb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/router/command"
	statspb "github.com/shisui1511/xkeen-control-panel/internal/xrayapi/gen/xray/app/stats/command"
	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi/testutil"
)

func TestXrayRealityKeygen(t *testing.T) {
	api := &API{}

	req := httptest.NewRequest(http.MethodGet, "/api/xray/reality/keygen", nil)
	rr := httptest.NewRecorder()

	api.XrayRealityKeygen(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			PrivateKey string `json:"private_key"`
			PublicKey  string `json:"public_key"`
			ShortID    string `json:"short_id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success true")
	}
	if resp.Data.PrivateKey == "" || resp.Data.PublicKey == "" || resp.Data.ShortID == "" {
		t.Errorf("expected non-empty keypair in response: %+v", resp.Data)
	}
}

func TestXrayStats(t *testing.T) {
	// 1. Nil service -> 503
	api := &API{}
	req := httptest.NewRequest(http.MethodGet, "/api/xray/stats", nil)
	rr := httptest.NewRecorder()
	api.XrayStats(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when service is nil, got %d", rr.Code)
	}

	// 2. Non-GET method -> 405
	reqPost := httptest.NewRequest(http.MethodPost, "/api/xray/stats", nil)
	rrPost := httptest.NewRecorder()
	api.XrayStats(rrPost, reqPost)
	if rrPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on POST, got %d", rrPost.Code)
	}

	// 3. Kernel not xray -> 503
	svcKernel := services.NewXrayGRPCService("127.0.0.1:10085")
	svcKernel.SetActiveKernelFunc(func() string { return "mihomo" })
	api.SetXrayGRPCService(svcKernel)
	rrKernel := httptest.NewRecorder()
	api.XrayStats(rrKernel, req)
	if rrKernel.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when kernel is mihomo, got %d", rrKernel.Code)
	}

	// 4. Success path with mock server
	mock := testutil.NewMockStatsServer()
	mock.SetStat("outbound>>>vless-us>>>traffic>>>uplink", 1024)
	mock.SetStat("outbound>>>vless-us>>>traffic>>>downlink", 2048)

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	go func() { _ = s.Serve(lis) }()
	defer func() {
		s.Stop()
		_ = lis.Close()
	}()

	svc := services.NewXrayGRPCService("passthrough://bufnet")
	svc.SetActiveKernelFunc(func() string { return "xray" })
	svc.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(target,
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	})
	api.SetXrayGRPCService(svc)

	rrSuccess := httptest.NewRecorder()
	api.XrayStats(rrSuccess, req)

	if rrSuccess.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrSuccess.Code, rrSuccess.Body.String())
	}

	var statsResp struct {
		Success bool `json:"success"`
		Data    map[string]struct {
			Uplink   int64 `json:"uplink"`
			Downlink int64 `json:"downlink"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rrSuccess.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}
	if !statsResp.Success {
		t.Errorf("expected success true")
	}
	vless, ok := statsResp.Data["vless-us"]
	if !ok {
		t.Fatalf("expected vless-us in stats")
	}
	if vless.Uplink != 1024 || vless.Downlink != 2048 {
		t.Errorf("expected 1024/2048, got %d/%d", vless.Uplink, vless.Downlink)
	}
}

func TestXrayGRPCMonitoring(t *testing.T) {
	tmpDir := t.TempDir()
	pv := utils.NewPathValidator([]string{tmpDir})

	cfg := &config.Config{
		XRayConfigDir: tmpDir,
		XRayAPIPort:   10085,
	}
	api := &API{
		cfg:     cfg,
		pathVal: pv,
	}

	cfgPath := filepath.Join(tmpDir, "config.json")

	// 1. Missing config file -> 503 and file not created
	body := bytes.NewBufferString(`{"enabled": true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", body)
	rr := httptest.NewRecorder()
	api.XrayGRPCMonitoring(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when config missing, got %d", rr.Code)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Errorf("config.json should not have been created on missing file error")
	}

	// 2. Non-POST -> 405
	reqGet := httptest.NewRequest(http.MethodGet, "/api/xray/grpc/monitoring", nil)
	rrGet := httptest.NewRecorder()
	api.XrayGRPCMonitoring(rrGet, reqGet)
	if rrGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET, got %d", rrGet.Code)
	}

	// 3. Bad request body -> 400
	reqBad := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bytes.NewBufferString(`invalid-json`))
	rrBad := httptest.NewRecorder()
	api.XrayGRPCMonitoring(rrBad, reqBad)
	if rrBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid JSON, got %d", rrBad.Code)
	}

	// Create initial config file
	initialConfig := `{
  "inbounds": [
    {
      "tag": "socks-in",
      "port": 10808,
      "protocol": "socks"
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(initialConfig), 0600); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// Find a free port for testing
	freeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	testPort := freeLn.Addr().(*net.TCPAddr).Port
	_ = freeLn.Close()
	cfg.XRayAPIPort = testPort

	// 4. Enable monitoring -> 200, config contains api inbound and stats
	bodyEnable := bytes.NewBufferString(`{"enabled": true}`)
	reqEnable := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bodyEnable)
	rrEnable := httptest.NewRecorder()
	api.XrayGRPCMonitoring(rrEnable, reqEnable)

	if rrEnable.Code != http.StatusOK {
		t.Fatalf("expected 200 on enable, got %d: %s", rrEnable.Code, rrEnable.Body.String())
	}

	savedData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("failed to read saved config: %v", err)
	}
	var savedObj map[string]interface{}
	if err := json.Unmarshal(savedData, &savedObj); err != nil {
		t.Fatalf("failed to parse saved config: %v", err)
	}
	hasAPIInbound := false
	for _, inb := range savedObj["inbounds"].([]interface{}) {
		if inb.(map[string]interface{})["tag"] == "api" {
			hasAPIInbound = true
			break
		}
	}
	if !hasAPIInbound {
		t.Errorf("saved config does not contain api inbound")
	}

	// 5. Busy port test on a different port that is not yet configured -> 409 and file unchanged
	busyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy test port: %v", err)
	}
	busyPort := busyLn.Addr().(*net.TCPAddr).Port
	cfg.XRayAPIPort = busyPort
	contentBeforeBusy := string(savedData)

	bodyBusy := bytes.NewBufferString(`{"enabled": true}`)
	reqBusy := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bodyBusy)
	rrBusy := httptest.NewRecorder()
	api.XrayGRPCMonitoring(rrBusy, reqBusy)
	_ = busyLn.Close()
	cfg.XRayAPIPort = testPort // restore

	if rrBusy.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict on busy port, got %d: %s", rrBusy.Code, rrBusy.Body.String())
	}
	contentAfterBusy, _ := os.ReadFile(cfgPath)
	if string(contentAfterBusy) != contentBeforeBusy {
		t.Errorf("file was modified despite busy port conflict")
	}

	// 6. Disable monitoring -> 200, api inbound removed, stats remains
	bodyDisable := bytes.NewBufferString(`{"enabled": false}`)
	reqDisable := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bodyDisable)
	rrDisable := httptest.NewRecorder()
	api.XrayGRPCMonitoring(rrDisable, reqDisable)

	if rrDisable.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d: %s", rrDisable.Code, rrDisable.Body.String())
	}

	disabledData, _ := os.ReadFile(cfgPath)
	var disabledObj map[string]interface{}
	_ = json.Unmarshal(disabledData, &disabledObj)

	for _, inb := range disabledObj["inbounds"].([]interface{}) {
		if inb.(map[string]interface{})["tag"] == "api" {
			t.Errorf("api inbound should have been removed on disable")
		}
	}
	if _, ok := disabledObj["stats"]; !ok {
		t.Errorf("stats object should remain after disable")
	}
	// 7. Modular layout test: directory has other .json files, but no config.json
	modularDir := t.TempDir()
	pvModular := utils.NewPathValidator([]string{modularDir})
	cfgModular := &config.Config{
		XRayConfigDir: modularDir,
		XRayAPIPort:   testPort,
	}
	apiModular := &API{
		cfg:     cfgModular,
		pathVal: pvModular,
	}
	// Add an existing modular file (e.g. 04_outbounds.json)
	_ = os.WriteFile(filepath.Join(modularDir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600)

	bodyModEnable := bytes.NewBufferString(`{"enabled": true}`)
	reqModEnable := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bodyModEnable)
	rrModEnable := httptest.NewRecorder()
	apiModular.XrayGRPCMonitoring(rrModEnable, reqModEnable)
	if rrModEnable.Code != http.StatusOK {
		t.Fatalf("expected 200 on modular config enable, got %d: %s", rrModEnable.Code, rrModEnable.Body.String())
	}

	apiJSONPath := filepath.Join(modularDir, "00_api.json")
	if _, err := os.Stat(apiJSONPath); err != nil {
		t.Errorf("expected 00_api.json to be created in modular layout, err: %v", err)
	}

	bodyModDisable := bytes.NewBufferString(`{"enabled": false}`)
	reqModDisable := httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bodyModDisable)
	rrModDisable := httptest.NewRecorder()
	apiModular.XrayGRPCMonitoring(rrModDisable, reqModDisable)
	if rrModDisable.Code != http.StatusOK {
		t.Fatalf("expected 200 on modular config disable, got %d: %s", rrModDisable.Code, rrModDisable.Body.String())
	}
	if _, err := os.Stat(apiJSONPath); !os.IsNotExist(err) {
		t.Errorf("expected 00_api.json to be deleted after disabling, but it exists")
	}

	// Calling disable again when already disabled is idempotent and does not create 00_api.json
	rrModDisable2 := httptest.NewRecorder()
	apiModular.XrayGRPCMonitoring(rrModDisable2, httptest.NewRequest(http.MethodPost, "/api/xray/grpc/monitoring", bytes.NewBufferString(`{"enabled": false}`)))
	if rrModDisable2.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeated disable, got %d", rrModDisable2.Code)
	}
	if _, err := os.Stat(apiJSONPath); !os.IsNotExist(err) {
		t.Errorf("expected 00_api.json to not be created on repeated disable")
	}
}

func TestCapabilitiesGRPCReady(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		XRayConfigDir: tmpDir,
	}
	api := &API{
		cfg: cfg,
	}

	cfgPath := filepath.Join(tmpDir, "config.json")

	// Create mock xkeen script that reports status from an environment variable
	mockScript := filepath.Join(tmpDir, "mock_xkeen.sh")
	scriptContent := "#!/bin/sh\nif [ \"$1\" = \"-status\" ]; then echo \"$MOCK_KERNEL_STATUS\"; fi\n"
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	xkeenSvc := services.NewXKeenService(mockScript, tmpDir)
	api.xkeenSvc = xkeenSvc

	// 1. Config has api inbound and active kernel is xray -> grpc_ready: true
	configWithAPI := `{
  "inbounds": [
    {
      "tag": "api",
      "port": 10085
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(configWithAPI), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	t.Setenv("MOCK_KERNEL_STATUS", "XRay is running")
	api.capsCache = nil // invalidate cache

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	rr := httptest.NewRecorder()
	api.Capabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Success bool                 `json:"success"`
		Data    CapabilitiesResponse `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse capabilities response: %v", err)
	}
	if !resp.Data.XRay.GRPCReady {
		t.Errorf("expected GRPCReady to be true when xray is active and api inbound exists")
	}

	// 2. Active kernel is mihomo -> grpc_ready: false
	t.Setenv("MOCK_KERNEL_STATUS", "Mihomo is running")
	api.capsCache = nil
	rrMihomo := httptest.NewRecorder()
	api.Capabilities(rrMihomo, req)

	var respMihomo struct {
		Success bool                 `json:"success"`
		Data    CapabilitiesResponse `json:"data"`
	}
	_ = json.Unmarshal(rrMihomo.Body.Bytes(), &respMihomo)
	if respMihomo.Data.XRay.GRPCReady {
		t.Errorf("expected GRPCReady to be false when active kernel is mihomo")
	}

	// 3. Active kernel is xray but config lacks api inbound -> grpc_ready: false
	t.Setenv("MOCK_KERNEL_STATUS", "XRay is running")
	api.capsCache = nil
	configWithoutAPI := `{"inbounds": [{"tag": "socks-in"}]}`
	_ = os.WriteFile(cfgPath, []byte(configWithoutAPI), 0600)

	rrNoAPI := httptest.NewRecorder()
	api.Capabilities(rrNoAPI, req)

	var respNoAPI struct {
		Success bool                 `json:"success"`
		Data    CapabilitiesResponse `json:"data"`
	}
	_ = json.Unmarshal(rrNoAPI.Body.Bytes(), &respNoAPI)
	if respNoAPI.Data.XRay.GRPCReady {
		t.Errorf("expected GRPCReady to be false when config lacks api inbound")
	}

	// 4. Modular layout (00_api.json instead of config.json) -> grpc_ready: true
	_ = os.Remove(cfgPath)
	apiJSONPath := filepath.Join(tmpDir, "00_api.json")
	_ = os.WriteFile(apiJSONPath, []byte(configWithAPI), 0600)
	api.capsCache = nil
	rrModular := httptest.NewRecorder()
	api.Capabilities(rrModular, req)
	var respModular struct {
		Success bool                 `json:"success"`
		Data    CapabilitiesResponse `json:"data"`
	}
	_ = json.Unmarshal(rrModular.Body.Bytes(), &respModular)
	if !respModular.Data.XRay.GRPCReady {
		t.Errorf("expected GRPCReady to be true for modular 00_api.json")
	}
}

func TestXrayTestRoute(t *testing.T) {
	api := &API{}

	// 1. Method not allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/xray/test-route", nil)
	rrGet := httptest.NewRecorder()
	api.XrayTestRoute(rrGet, reqGet)
	if rrGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET, got %d", rrGet.Code)
	}

	// 2. Nil service -> 503
	body := bytes.NewBufferString(`{"domain": "example.com"}`)
	reqNil := httptest.NewRequest(http.MethodPost, "/api/xray/test-route", body)
	rrNil := httptest.NewRecorder()
	api.XrayTestRoute(rrNil, reqNil)
	if rrNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when service is nil, got %d", rrNil.Code)
	}

	// 3. Validation tests
	valCases := []struct {
		name string
		json string
	}{
		{"empty target", `{}`},
		{"invalid ip", `{"ip": "999.999.999.999"}`},
		{"port out of range", `{"domain": "example.com", "port": 70000}`},
		{"domain control char", `{"domain": "example\n.com"}`},
		{"domain too long", fmt.Sprintf(`{"domain": "%s.com"}`, string(make([]byte, 255)))},
	}

	for _, tc := range valCases {
		t.Run("val_"+tc.name, func(t *testing.T) {
			reqVal := httptest.NewRequest(http.MethodPost, "/api/xray/test-route", bytes.NewBufferString(tc.json))
			rrVal := httptest.NewRecorder()
			api.XrayTestRoute(rrVal, reqVal)
			if rrVal.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for case %s, got %d: %s", tc.name, rrVal.Code, rrVal.Body.String())
			}
		})
	}

	// 4. Success path with mock server
	mock := testutil.NewMockXrayServer()
	mock.SetRouteResult("example.com", "proxy-out", []string{"auto-group"})
	mock.SetDefaultRoute("direct", nil)

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	routerpb.RegisterRoutingServiceServer(s, mock)
	logpb.RegisterLoggerServiceServer(s, mock)
	go func() { _ = s.Serve(lis) }()
	defer func() {
		s.Stop()
		_ = lis.Close()
	}()

	svc := services.NewXrayGRPCService("127.0.0.1:10085")
	svc.SetActiveKernelFunc(func() string { return "xray" })
	svc.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough://bufnet",
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	})
	api.SetXrayGRPCService(svc)

	// Test route domain -> 200
	reqOk := httptest.NewRequest(http.MethodPost, "/api/xray/test-route", bytes.NewBufferString(`{"domain": "example.com", "port": 443}`))
	rrOk := httptest.NewRecorder()
	api.XrayTestRoute(rrOk, reqOk)
	if rrOk.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrOk.Code, rrOk.Body.String())
	}

	var respOk struct {
		Success bool `json:"success"`
		Data    struct {
			OutboundTag       string   `json:"outbound_tag"`
			OutboundGroupTags []string `json:"outbound_group_tags"`
			Matched           bool     `json:"matched"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rrOk.Body.Bytes(), &respOk); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !respOk.Success || !respOk.Data.Matched || respOk.Data.OutboundTag != "proxy-out" {
		t.Errorf("unexpected route response: %+v", respOk)
	}

	// Test route IP -> 200
	reqIP := httptest.NewRequest(http.MethodPost, "/api/xray/test-route", bytes.NewBufferString(`{"ip": "1.1.1.1"}`))
	rrIP := httptest.NewRecorder()
	api.XrayTestRoute(rrIP, reqIP)
	if rrIP.Code != http.StatusOK {
		t.Fatalf("expected 200 for IP, got %d: %s", rrIP.Code, rrIP.Body.String())
	}

	// Test route unavailable -> 503
	svcDown := services.NewXrayGRPCService("127.0.0.1:10085")
	svcDown.SetActiveKernelFunc(func() string { return "xray" })
	svcDown.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return nil, fmt.Errorf("connection refused")
	})
	api.SetXrayGRPCService(svcDown)

	reqDown := httptest.NewRequest(http.MethodPost, "/api/xray/test-route", bytes.NewBufferString(`{"domain": "example.com"}`))
	rrDown := httptest.NewRecorder()
	api.XrayTestRoute(rrDown, reqDown)
	if rrDown.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 on core unavailable, got %d", rrDown.Code)
	}
}

func TestXrayRestartLogger(t *testing.T) {
	api := &API{}

	// 1. Method not allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/xray/restart-logger", nil)
	rrGet := httptest.NewRecorder()
	api.XrayRestartLogger(rrGet, reqGet)
	if rrGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET, got %d", rrGet.Code)
	}

	// 2. Nil service -> 503
	reqNil := httptest.NewRequest(http.MethodPost, "/api/xray/restart-logger", nil)
	rrNil := httptest.NewRecorder()
	api.XrayRestartLogger(rrNil, reqNil)
	if rrNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when service is nil, got %d", rrNil.Code)
	}

	// 3. Mock server setup
	mock := testutil.NewMockXrayServer()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	statspb.RegisterStatsServiceServer(s, mock)
	routerpb.RegisterRoutingServiceServer(s, mock)
	logpb.RegisterLoggerServiceServer(s, mock)
	go func() { _ = s.Serve(lis) }()
	defer func() {
		s.Stop()
		_ = lis.Close()
	}()

	svc := services.NewXrayGRPCService("127.0.0.1:10085")
	svc.SetActiveKernelFunc(func() string { return "xray" })
	svc.SetDialerFunc(func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough://bufnet",
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return lis.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	})
	api.SetXrayGRPCService(svc)

	// 4. Success -> 200
	reqOk := httptest.NewRequest(http.MethodPost, "/api/xray/restart-logger", nil)
	rrOk := httptest.NewRecorder()
	api.XrayRestartLogger(rrOk, reqOk)
	if rrOk.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rrOk.Code, rrOk.Body.String())
	}
	if mock.RestartLoggerCount() != 1 {
		t.Errorf("expected restartLoggerCount 1, got %d", mock.RestartLoggerCount())
	}

	// 5. Rate limited -> 429
	reqRate := httptest.NewRequest(http.MethodPost, "/api/xray/restart-logger", nil)
	rrRate := httptest.NewRecorder()
	api.XrayRestartLogger(rrRate, reqRate)
	if rrRate.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on immediate repeat, got %d", rrRate.Code)
	}
	if rrRate.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header to be set")
	}
}

func TestXrayTLSPing(t *testing.T) {
	api := &API{}

	// 1. Method not allowed (GET)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/xray/tls-ping", nil)
	rrGet := httptest.NewRecorder()
	api.XrayTLSPing(rrGet, reqGet)
	if rrGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET, got %d", rrGet.Code)
	}

	// 2. Invalid JSON body -> 400
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/xray/tls-ping", bytes.NewBufferString(`{bad json`))
	rrBadJSON := httptest.NewRecorder()
	api.XrayTLSPing(rrBadJSON, reqBadJSON)
	if rrBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on bad JSON, got %d", rrBadJSON.Code)
	}

	// 3. Rejected target (loopback IP) -> 400
	reqLoopback := httptest.NewRequest(http.MethodPost, "/api/xray/tls-ping", bytes.NewBufferString(`{"dest": "127.0.0.1:443"}`))
	rrLoopback := httptest.NewRecorder()
	api.XrayTLSPing(rrLoopback, reqLoopback)
	if rrLoopback.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on loopback target, got %d: %s", rrLoopback.Code, rrLoopback.Body.String())
	}

	// 4. Rejected target (private network) -> 400
	reqPrivate := httptest.NewRequest(http.MethodPost, "/api/xray/tls-ping", bytes.NewBufferString(`{"dest": "192.168.1.1:443"}`))
	rrPrivate := httptest.NewRecorder()
	api.XrayTLSPing(rrPrivate, reqPrivate)
	if rrPrivate.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on private IP target, got %d", rrPrivate.Code)
	}

	// 5. Rejected target (panel port match) -> 400
	reqPanelPort := httptest.NewRequest(http.MethodPost, "/api/xray/tls-ping", bytes.NewBufferString(`{"dest": "example.com:8090"}`))
	rrPanelPort := httptest.NewRecorder()
	api.XrayTLSPing(rrPanelPort, reqPanelPort)
	if rrPanelPort.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on panel port match, got %d", rrPanelPort.Code)
	}

	// 6. Rejected server_name (too long) -> 400
	longSN := fmt.Sprintf(`{"dest": "example.com:443", "server_name": "%s"}`, string(make([]byte, 255)))
	reqLongSN := httptest.NewRequest(http.MethodPost, "/api/xray/tls-ping", bytes.NewBufferString(longSN))
	rrLongSN := httptest.NewRecorder()
	api.XrayTLSPing(rrLongSN, reqLongSN)
	if rrLongSN.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on excessive server_name length, got %d", rrLongSN.Code)
	}

	// 7. Valid target -> 200 (returns diagnostic result even if offline)
	reqValid := httptest.NewRequest(http.MethodPost, "/api/xray/tls-ping", bytes.NewBufferString(`{"dest": "cloudflare.com:443", "server_name": "cloudflare.com", "alpn": ["h2"]}`))
	rrValid := httptest.NewRecorder()
	api.XrayTLSPing(rrValid, reqValid)
	if rrValid.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid target, got %d: %s", rrValid.Code, rrValid.Body.String())
	}

	var resp struct {
		Success bool                  `json:"success"`
		Data    services.TLSPingResult `json:"data"`
	}
	if err := json.Unmarshal(rrValid.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success envelope to be true")
	}
}



