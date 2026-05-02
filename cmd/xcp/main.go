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
		PasswordHash:    cfg.Auth.PasswordHash,
	}

	webFS, _ := xkeencontrolpanel.GetWebFS()
	srv, err := server.New(srvCfg, Version, webFS)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Auth endpoints (public)
	authSvc := srv.GetAuthService()
	srv.Handle("/api/auth/login", authSvc.HandleLogin)
	srv.Handle("/api/auth/logout", authSvc.HandleLogout)
	srv.Handle("/api/auth/me", authSvc.HandleMe)
	srv.Handle("/api/auth/setup", authSvc.HandleSetup)

	// Public endpoints
	srv.Handle("/api/version", handlers.NewAPI(cfg, srv).Version)

	// Protected API handlers
	api := handlers.NewAPI(cfg, srv)
	srv.HandleProtected("/api/config/list", api.ConfigList)
	srv.HandleProtected("/api/config/read", api.ConfigRead)
	srv.HandleProtected("/api/config/save", api.ConfigSave)
	srv.HandleProtected("/api/config/backups", api.ConfigBackups)
	srv.HandleProtected("/api/service/status", api.ServiceStatus)
	srv.HandleProtected("/api/service/control", api.ServiceControl)
	srv.HandleProtected("/api/logs/ws", api.LogsWebSocket)
	srv.HandleProtected("/api/mihomo/status", api.MihomoStatus)
	srv.HandleProtected("/api/mihomo/control", api.MihomoControl)

	log.Printf("XKeen Control Panel v%s starting...", Version)
	if cfg.Auth.PasswordHash == "" {
		log.Printf("⚠️  No password set. Please visit http://localhost:%d to complete setup.", cfg.Port)
	}
	
	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
