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
	cfg                   *config.Config
	srv                   *server.Server
	xkeenSvc              *services.XKeenService
	mihomoSvc             *services.MihomoService
	configSvc             *services.ConfigService
	subscriptionSvc       *services.SubscriptionService
	subscriptionHealthSvc *services.SubscriptionHealthService
	kernelSvc             *services.KernelService
	networkSvc            *services.NetworkToolsService
	smartProxySvc         *services.SmartProxyService
	trafficQuotaSvc       *services.TrafficQuotaService
	datSvc                *services.DATManagerService
	snapshotSvc           *services.SnapshotService
	consoleSvc            *services.ConsoleService
	templateSvc           *services.TemplateService
	assetsSvc             *assets.AssetsService
	pathVal               *utils.PathValidator
	configValCache        bool
	configValCacheTime    time.Time
	configValCacheMutex   sync.Mutex
	capsCache             interface{}
	capsCacheTime         time.Time
	capsCacheMutex        sync.Mutex
	sslDaysCache          int
	sslDaysCacheTime      time.Time
	sslDaysCacheMutex     sync.Mutex
}

func NewAPI(cfg *config.Config, srv *server.Server) *API {
	assetsSvc := assets.NewService(cfg.DataDir)
	return &API{
		cfg:       cfg,
		srv:       srv,
		xkeenSvc:  services.NewXKeenService(cfg.XKeenBinary, cfg.DataDir),
		mihomoSvc: services.NewMihomoService(cfg.MihomoBinary, cfg.XKeenBinary, cfg.MihomoConfigDir),
		configSvc: services.NewConfigService(cfg.XRayConfigDir, cfg.AllowedRoots),
		assetsSvc: assetsSvc,
		pathVal:   utils.NewPathValidator(cfg.AllowedRoots),
	}
}

func (a *API) SetSmartProxyService(svc *services.SmartProxyService) {
	a.smartProxySvc = svc
}

func (a *API) SetTrafficQuotaService(svc *services.TrafficQuotaService) {
	a.trafficQuotaSvc = svc
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

func (a *API) SetTemplateService(svc *services.TemplateService) {
	a.templateSvc = svc
}

func (a *API) SetAssetsService(svc *assets.AssetsService) {
	a.assetsSvc = svc
}

func (a *API) GetAssetsService() *assets.AssetsService {
	return a.assetsSvc
}

func (a *API) SetKernelService(svc *services.KernelService) {
	a.kernelSvc = svc
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
		for _, info := range a.kernelSvc.List() {
			if info.ProcessStatus == "running" {
				active = info.Name
				break
			}
		}
	}
	if active == "" && a.xkeenSvc != nil {
		if status, err := a.xkeenSvc.Status(); err == nil {
			lower := strings.ToLower(status)
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

