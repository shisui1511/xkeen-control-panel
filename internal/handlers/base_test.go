package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

func TestAPIGettersAndSetters(t *testing.T) {
	api := &API{}

	// ClientResolver
	api.SetClientResolver(nil)
	if api.ClientResolver() != nil {
		t.Error("expected nil ClientResolver")
	}

	// SmartProxyService
	api.SetSmartProxyService(nil)

	// TrafficQuotaService
	api.SetTrafficQuotaService(nil)

	// WatchdogService
	api.SetWatchdogService(nil)
	if api.WatchdogService() != nil {
		t.Error("expected nil WatchdogService")
	}

	// XrayGRPCService
	api.SetXrayGRPCService(nil)
	if api.XrayGRPCService() != nil {
		t.Error("expected nil XrayGRPCService")
	}

	// DATManagerService
	api.SetDATManagerService(nil)

	// SnapshotService
	api.SetSnapshotService(nil)

	// ConsoleService
	api.SetConsoleService(nil)

	// PTYService
	api.SetPTYService(nil)

	// TemplateService
	api.SetTemplateService(nil)

	// LogDispatcher
	api.SetLogDispatcher(nil)
	if api.LogDispatcher() != nil {
		t.Error("expected nil LogDispatcher")
	}

	// UserRulesService
	api.SetUserRulesService(nil)
	if api.UserRulesService() != nil {
		t.Error("expected nil UserRulesService")
	}

	// AssetsService
	api.SetAssetsService(nil)
	if api.GetAssetsService() != nil {
		t.Error("expected nil AssetsService")
	}

	// MihomoService
	if api.MihomoService() != nil {
		t.Error("expected nil MihomoService")
	}

	// XKeenService
	if api.XKeenService() != nil {
		t.Error("expected nil XKeenService")
	}

	// KernelService
	ks := services.NewKernelService()
	api.SetKernelService(ks)
	if api.KernelService() != ks {
		t.Error("expected non-nil KernelService")
	}

	// SubscriptionService
	api.SetSubscriptionService(nil)

	// SubscriptionHealthService
	api.SetSubscriptionHealthService(nil)

	// NetworkToolsService
	api.SetNetworkToolsService(nil)

	// ClearCapabilitiesCache
	api.ClearCapabilitiesCache()
}

func TestTrafficHandlers_Extended(t *testing.T) {
	// 1. Nil traffic quota service -> 503
	apiNil := &API{}

	reqStatsNil := httptest.NewRequest(http.MethodGet, "/api/traffic/stats", nil)
	recStatsNil := httptest.NewRecorder()
	apiNil.TrafficStats(recStatsNil, reqStatsNil)
	if recStatsNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil TrafficStats, got %d", recStatsNil.Code)
	}

	reqAlertsNil := httptest.NewRequest(http.MethodGet, "/api/traffic/alerts", nil)
	recAlertsNil := httptest.NewRecorder()
	apiNil.TrafficAlerts(recAlertsNil, reqAlertsNil)
	if recAlertsNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil TrafficAlerts, got %d", recAlertsNil.Code)
	}

	reqClearNil := httptest.NewRequest(http.MethodPost, "/api/traffic/alerts/clear", nil)
	recClearNil := httptest.NewRecorder()
	apiNil.TrafficAlertsClear(recClearNil, reqClearNil)
	if recClearNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil TrafficAlertsClear, got %d", recClearNil.Code)
	}

	reqResetNil := httptest.NewRequest(http.MethodPost, "/api/traffic/reset", nil)
	recResetNil := httptest.NewRecorder()
	apiNil.TrafficReset(recResetNil, reqResetNil)
	if recResetNil.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil TrafficReset, got %d", recResetNil.Code)
	}

	// 2. Initialized service
	tmpDir := t.TempDir()
	quotaSvc := services.NewTrafficQuotaService(tmpDir, "http://localhost:9090", "")
	api := &API{
		trafficQuotaSvc: quotaSvc,
	}

	// Method Not Allowed checks
	reqStatsPost := httptest.NewRequest(http.MethodPost, "/api/traffic/stats", nil)
	recStatsPost := httptest.NewRecorder()
	api.TrafficStats(recStatsPost, reqStatsPost)
	if recStatsPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST TrafficStats, got %d", recStatsPost.Code)
	}

	reqAlertsPost := httptest.NewRequest(http.MethodPost, "/api/traffic/alerts", nil)
	recAlertsPost := httptest.NewRecorder()
	api.TrafficAlerts(recAlertsPost, reqAlertsPost)
	if recAlertsPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST TrafficAlerts, got %d", recAlertsPost.Code)
	}

	reqClearGet := httptest.NewRequest(http.MethodGet, "/api/traffic/alerts/clear", nil)
	recClearGet := httptest.NewRecorder()
	api.TrafficAlertsClear(recClearGet, reqClearGet)
	if recClearGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET TrafficAlertsClear, got %d", recClearGet.Code)
	}

	reqResetGet := httptest.NewRequest(http.MethodGet, "/api/traffic/reset", nil)
	recResetGet := httptest.NewRecorder()
	api.TrafficReset(recResetGet, reqResetGet)
	if recResetGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET TrafficReset, got %d", recResetGet.Code)
	}

	// Valid calls
	reqStatsOK := httptest.NewRequest(http.MethodGet, "/api/traffic/stats", nil)
	recStatsOK := httptest.NewRecorder()
	api.TrafficStats(recStatsOK, reqStatsOK)
	if recStatsOK.Code != http.StatusOK {
		t.Errorf("expected 200 for TrafficStats, got %d", recStatsOK.Code)
	}

	reqAlertsOK := httptest.NewRequest(http.MethodGet, "/api/traffic/alerts", nil)
	recAlertsOK := httptest.NewRecorder()
	api.TrafficAlerts(recAlertsOK, reqAlertsOK)
	if recAlertsOK.Code != http.StatusOK {
		t.Errorf("expected 200 for TrafficAlerts, got %d", recAlertsOK.Code)
	}

	reqClearOK := httptest.NewRequest(http.MethodPost, "/api/traffic/alerts/clear", nil)
	recClearOK := httptest.NewRecorder()
	api.TrafficAlertsClear(recClearOK, reqClearOK)
	if recClearOK.Code != http.StatusOK {
		t.Errorf("expected 200 for TrafficAlertsClear, got %d", recClearOK.Code)
	}

	reqResetOK := httptest.NewRequest(http.MethodPost, "/api/traffic/reset", nil)
	recResetOK := httptest.NewRecorder()
	api.TrafficReset(recResetOK, reqResetOK)
	if recResetOK.Code != http.StatusOK {
		t.Errorf("expected 200 for TrafficReset, got %d", recResetOK.Code)
	}
}

func TestSetupXrayCmdEnv_And_GetActiveKernelName(t *testing.T) {
	tmpDir := t.TempDir()
	datDir := filepath.Join(tmpDir, "dat")
	_ = os.MkdirAll(datDir, 0755)

	// 1. setupXrayCmdEnv without existing XRAY_LOCATION_ASSET
	cmd1 := exec.Command("echo")
	setupXrayCmdEnv(cmd1, datDir)
	if cmd1.Env == nil {
		t.Error("expected non-nil Env in cmd1")
	}

	// 2. setupXrayCmdEnv with existing XRAY_LOCATION_ASSET
	cmd2 := exec.Command("echo")
	t.Setenv("XRAY_LOCATION_ASSET", "/custom/dat")
	setupXrayCmdEnv(cmd2, datDir)
	hasCustom := false
	for _, e := range cmd2.Env {
		if e == "XRAY_LOCATION_ASSET=/custom/dat" {
			hasCustom = true
			break
		}
	}
	if !hasCustom {
		t.Errorf("expected XRAY_LOCATION_ASSET=/custom/dat in cmd2.Env, got %+v", cmd2.Env)
	}

	// 3. getActiveKernelName
	api := &API{
		xkeenSvc: services.NewXKeenService("/bin/true", tmpDir),
	}
	active := api.getActiveKernelName()
	if active != "" && active != "none" && active != "xray" && active != "mihomo" && active != "both" {
		t.Errorf("unexpected active kernel: %s", active)
	}
}
