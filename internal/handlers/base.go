package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/i18n"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type API struct {
	cfg             *config.Config
	srv             *server.Server
	xkeenSvc        *services.XKeenService
	mihomoSvc       *services.MihomoService
	configSvc       *services.ConfigService
	subscriptionSvc *services.SubscriptionService
	kernelSvc       *services.KernelService
	networkSvc      *services.NetworkToolsService
	smartProxySvc   *services.SmartProxyService
	trafficQuotaSvc *services.TrafficQuotaService
	datSvc          *services.DATManagerService
	consoleSvc      *services.ConsoleService
	templateSvc     *services.TemplateService
	pathVal         *utils.PathValidator
}

func NewAPI(cfg *config.Config, srv *server.Server) *API {
	return &API{
		cfg:       cfg,
		srv:       srv,
		xkeenSvc:  services.NewXKeenService(cfg.XKeenBinary),
		mihomoSvc: services.NewMihomoService(cfg.MihomoBinary, cfg.MihomoConfigDir),
		configSvc: services.NewConfigService(cfg.XRayConfigDir),
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

func (a *API) SetConsoleService(svc *services.ConsoleService) {
	a.consoleSvc = svc
}

func (a *API) SetTemplateService(svc *services.TemplateService) {
	a.templateSvc = svc
}

func (a *API) SetKernelService(svc *services.KernelService) {
	a.kernelSvc = svc
}

func (a *API) SetSubscriptionService(svc *services.SubscriptionService) {
	a.subscriptionSvc = svc
}

func (a *API) SetNetworkToolsService(svc *services.NetworkToolsService) {
	a.networkSvc = svc
}

func (a *API) t(r *http.Request, key string) string {
	return i18n.T(i18n.LangFromContext(r.Context()), key)
}

func (a *API) jsonResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) errorResponse(w http.ResponseWriter, message string, status int) {
	http.Error(w, message, status)
}
