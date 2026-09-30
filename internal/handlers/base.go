package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/assets"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type API struct {
	cfg *config.Config
	// logsRescanInterval — период проверки новых файлов в запасном потоке
	// логов; ноль — defaultLogsRescanInterval
	logsRescanInterval    time.Duration
	srv                   *server.Server
	xkeenSvc              *services.XKeenService
	mihomoSvc             *services.MihomoService
	configSvc             *services.ConfigService
	subscriptionSvc       *services.SubscriptionService
	subscriptionHealthSvc *services.SubscriptionHealthService
	kernelSvc             *services.KernelService
	kernelApplier         *services.KernelApplier
	networkSvc            *services.NetworkToolsService
	smartProxySvc         *services.SmartProxyService
	xkeenSettingsSvc      *services.XKeenSettingsService
	mihomoProfileSvc      *services.MihomoProfileService
	xrayAccessLogSvc      *services.XrayAccessLogService
	trafficQuotaSvc       *services.TrafficQuotaService
	watchdogSvc           *services.WatchdogService
	xkeenStatus           *services.XKeenStatusCache
	updateScheduler       *UpdateScheduler
	xrayGRPCSvc           *services.XrayGRPCService
	datSvc                *services.DATManagerService
	snapshotSvc           *services.SnapshotService
	consoleSvc            *services.ConsoleService
	ptySvc                *services.PTYService
	xkeenInstaller        *services.XKeenInstaller
	templateSvc           *services.TemplateService
	logDispatcher         *services.LogDispatcher
	userRulesSvc          *services.UserRulesService
	routeTracerSvc        *services.RouteTracerService
	clientResolver        *services.ClientResolver
	assetsSvc             *assets.AssetsService
	pathVal               *utils.PathValidator
	delayGuard            *DelayGuard
	configValCache        bool
	configValCacheTime    time.Time
	configValCacheMutex   sync.Mutex
	capsCache             interface{}
	capsCacheTime         time.Time
	capsCacheMutex        sync.Mutex
	sslDaysCache          int
	sslDaysCacheTime      time.Time
	sslDaysCacheMutex     sync.Mutex
	lastRestartLogger     time.Time
	restartLoggerMutex    sync.Mutex
}

func NewAPI(cfg *config.Config, srv *server.Server) *API {
	assetsSvc := assets.NewService(cfg.DataDir)
	return &API{
		cfg:            cfg,
		srv:            srv,
		xkeenSvc:       services.NewXKeenService(cfg.XKeenBinary, cfg.DataDir),
		mihomoSvc:      services.NewMihomoService(cfg.MihomoBinary, cfg.XKeenBinary, cfg.MihomoConfigDir),
		configSvc:      services.NewConfigService(cfg.XRayConfigDir, cfg.AllowedRoots),
		clientResolver: services.NewClientResolver(),
		assetsSvc:      assetsSvc,
		pathVal:        utils.NewPathValidator(cfg.AllowedRoots),
		delayGuard:     NewDelayGuard(32, 15*time.Second),
	}
}

func (a *API) SetClientResolver(svc *services.ClientResolver) {
	a.clientResolver = svc
}

func (a *API) ClientResolver() *services.ClientResolver {
	return a.clientResolver
}

func (a *API) SetSmartProxyService(svc *services.SmartProxyService) {
	a.smartProxySvc = svc
}

func (a *API) SetTrafficQuotaService(svc *services.TrafficQuotaService) {
	a.trafficQuotaSvc = svc
}

func (a *API) SetWatchdogService(svc *services.WatchdogService) {
	a.watchdogSvc = svc
}

func (a *API) WatchdogService() *services.WatchdogService {
	return a.watchdogSvc
}

// SetXKeenStatusCache подключает кэш статуса XKeen; создаётся и останавливается в main.
func (a *API) SetXKeenStatusCache(c *services.XKeenStatusCache) {
	a.xkeenStatus = c
}

func (a *API) XKeenStatusCache() *services.XKeenStatusCache {
	return a.xkeenStatus
}

// xkeenStatusSnapshot — последнее известное состояние XKeen. Без кэша (тестовые
// сборки API) делает один прямой опрос: это запасной путь, а не ленивая
// инициализация сервиса.
func (a *API) xkeenStatusSnapshot() services.XKeenStatusSnapshot {
	if a.xkeenStatus != nil {
		return a.xkeenStatus.Snapshot()
	}
	if a.xkeenSvc == nil {
		return services.XKeenStatusSnapshot{Stale: true}
	}
	out, err := a.xkeenSvc.Status()
	if err != nil {
		return services.XKeenStatusSnapshot{Stale: true, LastErr: err.Error()}
	}
	return services.XKeenStatusSnapshot{Raw: out, UpdatedAt: time.Now()}
}

// xkeenStatusAge — возраст снимка по часам кэша (или системным без кэша).
func (a *API) xkeenStatusAge(snap services.XKeenStatusSnapshot) (int, bool) {
	if a.xkeenStatus != nil {
		return a.xkeenStatus.AgeSeconds(snap)
	}
	return snap.AgeSeconds(time.Now())
}

// xkeenVersion — версия XKeen из кэша (без запуска xkeen на запросе);
// без кэша — прямое чтение `xkeen -v`.
func (a *API) xkeenVersion() string {
	if a.xkeenStatus != nil {
		if v := a.xkeenStatus.Snapshot().Version; v != "" {
			return v
		}
		return "unknown"
	}
	return a.xkeenSvc.GetVersion()
}

// invalidateXKeenVersion помечает устаревшими статус и версию XKeen
// (после установщика XKeen); nil-безопасно.
func (a *API) invalidateXKeenVersion() {
	if a.xkeenStatus != nil {
		a.xkeenStatus.InvalidateVersion()
	}
}

// invalidateXKeenStatus помечает кэш статуса устаревшим (nil-безопасно).
func (a *API) invalidateXKeenStatus() {
	if a.xkeenStatus != nil {
		a.xkeenStatus.Invalidate()
	}
}

func (a *API) SetXrayGRPCService(svc *services.XrayGRPCService) {
	a.xrayGRPCSvc = svc
}

func (a *API) XrayGRPCService() *services.XrayGRPCService {
	return a.xrayGRPCSvc
}

func (a *API) SetDATManagerService(svc *services.DATManagerService) {
	a.datSvc = svc
}

func (a *API) SetSnapshotService(svc *services.SnapshotService) {
	a.snapshotSvc = svc
}

func (a *API) SetConsoleService(svc *services.ConsoleService) {
	a.consoleSvc = svc
}

func (a *API) SetPTYService(svc *services.PTYService) {
	a.ptySvc = svc
}

func (a *API) SetXKeenInstaller(svc *services.XKeenInstaller) {
	a.xkeenInstaller = svc
}

func (a *API) SetTemplateService(svc *services.TemplateService) {
	a.templateSvc = svc
}

func (a *API) SetLogDispatcher(svc *services.LogDispatcher) {
	a.logDispatcher = svc
}

func (a *API) LogDispatcher() *services.LogDispatcher {
	return a.logDispatcher
}

func (a *API) SetUserRulesService(svc *services.UserRulesService) {
	a.userRulesSvc = svc
}

func (a *API) UserRulesService() *services.UserRulesService {
	return a.userRulesSvc
}

func (a *API) SetRouteTracerService(svc *services.RouteTracerService) {
	a.routeTracerSvc = svc
}

func (a *API) RouteTracerService() *services.RouteTracerService {
	return a.routeTracerSvc
}

func (a *API) SetAssetsService(svc *assets.AssetsService) {
	a.assetsSvc = svc
}

func (a *API) GetAssetsService() *assets.AssetsService {
	return a.assetsSvc
}

func (a *API) MihomoService() *services.MihomoService {
	return a.mihomoSvc
}

func (a *API) XKeenService() *services.XKeenService {
	return a.xkeenSvc
}

func (a *API) SetKernelService(svc *services.KernelService) {
	a.kernelSvc = svc
	// Применение конфигурации решает по статусам ядер: собираем его здесь, при
	// старте, а не лениво в обработчике.
	if svc == nil {
		a.kernelApplier = nil
		return
	}
	a.kernelApplier = services.NewKernelApplier(svc, a.xkeenSvc)
}

// KernelApplier — общий исполнитель «применить конфиг к ядру»; nil до
// SetKernelService.
func (a *API) KernelApplier() *services.KernelApplier {
	return a.kernelApplier
}

func (a *API) KernelService() *services.KernelService {
	return a.kernelSvc
}

func (a *API) SetSubscriptionService(svc *services.SubscriptionService) {
	a.subscriptionSvc = svc
}

func (a *API) SetSubscriptionHealthService(svc *services.SubscriptionHealthService) {
	a.subscriptionHealthSvc = svc
}

func (a *API) SetNetworkToolsService(svc *services.NetworkToolsService) {
	a.networkSvc = svc
}

func (a *API) ClearCapabilitiesCache() {
	a.capsCacheMutex.Lock()
	defer a.capsCacheMutex.Unlock()
	a.capsCache = nil
}

// ResolveMihomoSecret возвращает секрет Clash API: сначала из конфига панели,
// при его отсутствии — fallback на secret из config.yaml Mihomo (ParseConfig).
// Используется всеми потребителями Clash API, включая SubscriptionService
// (через SetMihomoSecretResolver в main.go).
func (a *API) ResolveMihomoSecret() string {
	secret := a.cfg.MihomoSecret
	if secret == "" && a.mihomoSvc != nil {
		if _, parsedSecret, err := a.mihomoSvc.ParseConfig(); err == nil && parsedSecret != "" {
			secret = parsedSecret
		}
	}
	return secret
}

func (a *API) t(r *http.Request, key string) string {
	return i18n.T(i18n.LangFromContext(r.Context()), key)
}

func (a *API) jsonResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) errorResponse(w http.ResponseWriter, message string, status int) {
	JSONError(w, status, message)
}

// setupXrayCmdEnv configures XRAY_LOCATION_ASSET environment variable for xray -test commands
// to ensure Xray can find geodata (.dat) files during syntax validation.
func setupXrayCmdEnv(cmd *exec.Cmd, configDir string) {
	env := os.Environ()
	for _, e := range env {
		if strings.HasPrefix(e, "XRAY_LOCATION_ASSET=") {
			cmd.Env = env
			return
		}
	}
	candidates := []string{
		"/opt/etc/xray/dat",
		"/opt/share/xray",
		"/opt/etc/xray",
	}
	if configDir != "" {
		candidates = append(candidates, filepath.Dir(configDir), configDir)
	}
	for _, dir := range candidates {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			cmd.Env = append(env, "XRAY_LOCATION_ASSET="+dir)
			return
		}
	}
	cmd.Env = env
}

// getActiveKernelName returns the name of the currently running active kernel ("mihomo", "xray", "both", or "none").
func (a *API) getActiveKernelName() string {
	var active string
	if a.kernelSvc != nil {
		var running []string
		for _, info := range a.kernelSvc.List() {
			if info.ProcessStatus == "running" {
				running = append(running, info.Name)
			}
		}
		switch len(running) {
		case 0:
			// fall through to the xkeenSvc fallback below
		case 1:
			active = running[0]
		default:
			active = "both"
		}
	}
	if active == "" && a.xkeenSvc != nil {
		// Устаревший снимок не выдаётся за факт о запущенном ядре.
		if snap := a.xkeenStatusSnapshot(); !snap.Stale && snap.Raw != "" {
			lower := strings.ToLower(snap.Raw)
			if strings.Contains(lower, "xray") && strings.Contains(lower, "mihomo") {
				active = "both"
			} else if strings.Contains(lower, "xray") {
				active = "xray"
			} else if strings.Contains(lower, "mihomo") {
				active = "mihomo"
			}
		}
	}
	if active == "" {
		active = "none"
	}
	return active
}
