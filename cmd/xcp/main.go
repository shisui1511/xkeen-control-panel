package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/user/xkeen-control-panel/internal/config"
	"github.com/user/xkeen-control-panel/internal/handlers"
	"github.com/user/xkeen-control-panel/internal/server"
	"github.com/user/xkeen-control-panel"
)

var (
	Version   = "dev"
	configPath = flag.String("config", "/opt/etc/xkeen-control-panel/config.json", "Path to config file")
)

func main() {
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("Failed to load config: %v. Creating default...", err)
		cfg = config.Default()
		if err := os.MkdirAll(filepath.Dir(*configPath), 0755); err == nil {
			_ = config.Save(*configPath, cfg)
		}
	}

	srvCfg := &server.Config{
		Port:            cfg.Port,
		XRayConfigDir:   cfg.XRayConfigDir,
		XKeenBinary:     cfg.XKeenBinary,
		MihomoConfigDir: cfg.MihomoConfigDir,
		MihomoBinary:    cfg.MihomoBinary,
		AllowedRoots:    cfg.AllowedRoots,
		LogLevel:        cfg.LogLevel,
		DataDir:         cfg.DataDir,
	}

	webFS, _ := xkeencontrolpanel.GetWebFS()
	srv, err := server.New(srvCfg, Version, webFS)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Register API handlers
	api := handlers.NewAPI(cfg, srv)
	srv.Handle("/api/version", api.Version)
	srv.Handle("/api/config/list", api.ConfigList)
	srv.Handle("/api/config/read", api.ConfigRead)
	srv.Handle("/api/config/save", api.ConfigSave)
	srv.Handle("/api/service/status", api.ServiceStatus)
	srv.Handle("/api/service/control", api.ServiceControl)
	srv.Handle("/api/logs/ws", api.LogsWebSocket)
	srv.Handle("/api/mihomo/status", api.MihomoStatus)
	srv.Handle("/api/mihomo/control", api.MihomoControl)

	log.Printf("XKeen Control Panel %s starting on :%d", Version, cfg.Port)
	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
