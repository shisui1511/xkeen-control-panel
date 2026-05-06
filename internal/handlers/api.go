package handlers

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/gorilla/websocket"
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

func (a *API) Version(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"version": a.srv.GetVersion()})
}

func (a *API) ConfigList(w http.ResponseWriter, r *http.Request) {
	files, err := a.configSvc.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(files)
}

func (a *API) ConfigRead(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	data, err := a.configSvc.Read(cleanPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (a *API) ConfigSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	data, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	err = a.configSvc.Save(cleanPath, data)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigBackups(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	backups, err := a.configSvc.ListBackups(cleanPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(backups)
}

func (a *API) ConfigCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	if err := a.configSvc.Create(cleanPath); err != nil {
		if os.IsExist(err) {
			http.Error(w, "File already exists", 409)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	path := r.URL.Query().Get("path")
	cleanPath, err := a.pathVal.Validate(path)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	if err := a.configSvc.Delete(cleanPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ConfigRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	oldPath := r.URL.Query().Get("old")
	newPath := r.URL.Query().Get("new")

	cleanOldPath, err := a.pathVal.Validate(oldPath)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	cleanNewPath, err := a.pathVal.Validate(newPath)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	if err := a.configSvc.Rename(cleanOldPath, cleanNewPath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", 404)
			return
		}
		if os.IsExist(err) {
			http.Error(w, "Target file already exists", 409)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}

	w.Write([]byte("OK"))
}

func (a *API) ServiceStatus(w http.ResponseWriter, r *http.Request) {
	out, err := a.xkeenSvc.Status()
	if err != nil {
		http.Error(w, out, 500)
		return
	}
	w.Write([]byte(out))
}

func (a *API) ServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	action := r.URL.Query().Get("action")

	var out string
	var err error

	switch action {
	case "start":
		out, err = a.xkeenSvc.Start()
	case "stop":
		out, err = a.xkeenSvc.Stop()
	case "restart":
		out, err = a.xkeenSvc.Restart()
	default:
		http.Error(w, "Invalid action", 400)
		return
	}

	if err != nil {
		http.Error(w, out, 500)
		return
	}
	w.Write([]byte(out))
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (a *API) LogsWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sources := a.cfg.LogSources
	if len(sources) == 0 {
		sources = []string{a.cfg.LogPath}
	}

	var cmd *exec.Cmd
	if len(sources) == 1 {
		cmd = exec.Command("tail", "-f", sources[0])
	} else {
		args := append([]string{"-f"}, sources...)
		cmd = exec.Command("tail", args...)
	}
	stdout, _ := cmd.StdoutPipe()

	if err := cmd.Start(); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("Failed to start log stream\n"))
		return
	}
	defer cmd.Process.Kill()

	scanner := bufio.NewScanner(stdout)
	currentSource := ""
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "==> ") && strings.HasSuffix(line, " <==") {
			currentSource = line[4 : len(line)-4]
			continue
		}
		if currentSource != "" && len(sources) > 1 {
			line = "[" + currentSource + "] " + line
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(line+"\n")); err != nil {
			break
		}
	}
}

func (a *API) MihomoStatus(w http.ResponseWriter, r *http.Request) {
	out, err := a.mihomoSvc.Status()
	if err != nil {
		http.Error(w, out, 500)
		return
	}
	w.Write([]byte(out))
}

func (a *API) MihomoControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	action := r.URL.Query().Get("action")

	var out string
	var err error

	switch action {
	case "start":
		out, err = a.mihomoSvc.Start()
	case "stop":
		out, err = a.mihomoSvc.Stop()
	case "restart":
		out, err = a.mihomoSvc.Restart()
	default:
		http.Error(w, "Invalid action", 400)
		return
	}

	if err != nil {
		http.Error(w, out, 500)
		return
	}
	w.Write([]byte(out))
}

func (a *API) MihomoProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(a.cfg.MihomoAPIURL)
	if err != nil {
		http.Error(w, "Invalid Mihomo API URL", 500)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Mihomo API unavailable: "+err.Error(), 502)
	}

	// Strip /api/mihomo/proxy prefix and forward the rest
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/mihomo/proxy")
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}

	proxy.ServeHTTP(w, r)
}
