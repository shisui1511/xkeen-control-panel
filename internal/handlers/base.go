package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/user/xkeen-control-panel/internal/config"
	"github.com/user/xkeen-control-panel/internal/server"
	"github.com/user/xkeen-control-panel/internal/services"
	"github.com/user/xkeen-control-panel/internal/utils"
)

type API struct {
	cfg       *config.Config
	srv       *server.Server
	xkeenSvc  *services.XKeenService
	mihomoSvc *services.MihomoService
	configSvc *services.ConfigService
	pathVal   *utils.PathValidator
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

func (a *API) jsonResponse(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) errorResponse(w http.ResponseWriter, message string, status int) {
	http.Error(w, message, status)
}
