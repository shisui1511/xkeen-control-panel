package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	xkeencontrolpanel "github.com/shisui1511/xkeen-control-panel"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/handlers"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

var (
	Version    = "dev"
	configPath = flag.String("config", "/opt/etc/xkeen-control-panel/config.json", "Path to config file")
)

func main() {
	// Handle version flag before flag.Parse
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "-version" || arg == "--version" {
			fmt.Println(Version)
			os.Exit(0)
		}
	}

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
		SecureCookie:    cfg.Auth.SecureCookie,
		HTTPS: server.HTTPSConfig{
			Enabled:  cfg.HTTPS.Enabled,
			CertPath: cfg.HTTPS.CertPath,
			KeyPath:  cfg.HTTPS.KeyPath,
		},
		SavePasswordHash: func(hash string) error {
			return cfg.SavePasswordHash(*configPath, hash)
		},
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

	// API handlers
	api := handlers.NewAPI(cfg, srv)

	// Public endpoints
	srv.Handle("/api/version", api.Version)

	// Protected endpoints
	srv.HandleProtected("/api/config/list", api.ConfigList)
	srv.HandleProtected("/api/config/read", api.ConfigRead)
	srv.HandleProtected("/api/config/save", api.ConfigSave)
	srv.HandleProtected("/api/config/backups", api.ConfigBackups)
	srv.HandleProtected("/api/config/create", api.ConfigCreate)
	srv.HandleProtected("/api/config/delete", api.ConfigDelete)
	srv.HandleProtected("/api/config/rename", api.ConfigRename)
	srv.HandleProtected("/api/service/status", api.ServiceStatus)
	srv.HandleProtected("/api/service/control", api.ServiceControl)
	srv.HandleProtected("/api/logs/ws", api.LogsWebSocket)
	srv.HandleProtected("/api/mihomo/status", api.MihomoStatus)
	srv.HandleProtected("/api/mihomo/control", api.MihomoControl)
	srv.HandleProtected("/api/mihomo/proxy/", api.MihomoProxy)
	srv.HandleProtected("/api/system/stats", api.SystemStats)

	// Update endpoints
	srv.HandleProtected("/api/update/check", api.UpdateCheck)
	srv.HandleProtected("/api/update/changelog", api.UpdateChangelog)
	srv.HandleProtected("/api/update/install", api.UpdateInstall)
	srv.HandleProtected("/api/update/rollback", api.UpdateRollback)
	srv.HandleProtected("/api/update/status", api.UpdateStatusEndpoint)

	// Subscription endpoints
	srv.HandleProtected("/api/subscriptions", api.SubscriptionList)
	srv.HandleProtected("/api/subscriptions/add", api.SubscriptionAdd)
	srv.HandleProtected("/api/subscriptions/update", api.SubscriptionUpdate)
	srv.HandleProtected("/api/subscriptions/delete", api.SubscriptionDelete)
	srv.HandleProtected("/api/subscriptions/refresh", api.SubscriptionRefresh)
	srv.HandleProtected("/api/subscriptions/refresh-all", api.SubscriptionRefreshAll)

	// Kernel endpoints
	srv.HandleProtected("/api/kernels", api.KernelList)
	srv.HandleProtected("/api/kernels/xray/check", api.KernelCheck)
	srv.HandleProtected("/api/kernels/xray/install", api.KernelInstall)
	srv.HandleProtected("/api/kernels/xray/status", api.KernelStatus)
	srv.HandleProtected("/api/kernels/xray/channel", api.KernelChannel)
	srv.HandleProtected("/api/kernels/mihomo/check", api.KernelCheck)
	srv.HandleProtected("/api/kernels/mihomo/install", api.KernelInstall)
	srv.HandleProtected("/api/kernels/mihomo/status", api.KernelStatus)
	srv.HandleProtected("/api/kernels/mihomo/channel", api.KernelChannel)

	// Network Tools endpoints
	srv.HandleProtected("/api/network/ping", api.NetworkPing)
	srv.HandleProtected("/api/network/traceroute", api.NetworkTraceroute)
	srv.HandleProtected("/api/network/dns", api.NetworkDNS)
	srv.HandleProtected("/api/network/http", api.NetworkHTTPTest)
	srv.HandleProtected("/api/network/ip", api.NetworkIP)

	// Smart Proxy Manager endpoints
	srv.HandleProtected("/api/smart-proxy/profiles", api.SmartProxyList)
	srv.HandleProtected("/api/smart-proxy/profiles/get", api.SmartProxyGet)
	srv.HandleProtected("/api/smart-proxy/profiles/add", api.SmartProxyAdd)
	srv.HandleProtected("/api/smart-proxy/profiles/update", api.SmartProxyUpdate)
	srv.HandleProtected("/api/smart-proxy/profiles/delete", api.SmartProxyDelete)
	srv.HandleProtected("/api/smart-proxy/profiles/enabled", api.SmartProxySetEnabled)
	srv.HandleProtected("/api/smart-proxy/status", api.SmartProxyStatus)

	// Traffic Quotas endpoints
	srv.HandleProtected("/api/traffic/quotas", api.TrafficQuotaList)
	srv.HandleProtected("/api/traffic/quotas/get", api.TrafficQuotaGet)
	srv.HandleProtected("/api/traffic/quotas/add", api.TrafficQuotaAdd)
	srv.HandleProtected("/api/traffic/quotas/update", api.TrafficQuotaUpdate)
	srv.HandleProtected("/api/traffic/quotas/delete", api.TrafficQuotaDelete)
	srv.HandleProtected("/api/traffic/quotas/enabled", api.TrafficQuotaSetEnabled)
	srv.HandleProtected("/api/traffic/quotas/reset", api.TrafficQuotaReset)
	srv.HandleProtected("/api/traffic/stats", api.TrafficStats)
	srv.HandleProtected("/api/traffic/alerts", api.TrafficAlerts)
	srv.HandleProtected("/api/traffic/alerts/clear", api.TrafficAlertsClear)

	// Start background services
	smartProxySvc := services.NewSmartProxyService(cfg.DataDir, cfg.MihomoAPIURL)
	smartProxySvc.Start()
	api.SetSmartProxyService(smartProxySvc)
	defer smartProxySvc.Stop()

	trafficQuotaSvc := services.NewTrafficQuotaService(cfg.DataDir, cfg.MihomoAPIURL)
	trafficQuotaSvc.Start()
	api.SetTrafficQuotaService(trafficQuotaSvc)
	defer trafficQuotaSvc.Stop()

	// DAT Manager
	datSvc := services.NewDATManagerService()
	api.SetDATManagerService(datSvc)

	srv.HandleProtected("/api/dat/list", api.DATList)
	srv.HandleProtected("/api/dat/update", api.DATUpdate)

	// Xkeen Console
	xkeenPath := "/opt/bin/xkeen"
	consoleSvc := services.NewConsoleService(xkeenPath)
	api.SetConsoleService(consoleSvc)
	srv.HandleProtected("/api/console/commands", api.ConsoleListCommands)
	srv.HandleProtected("/api/console/execute", api.ConsoleExecute)

	log.Printf("XKeen Control Panel v%s starting...", Version)
	if cfg.Auth.PasswordHash == "" {
		log.Printf("⚠️  No password set. Please visit http://localhost:%d to complete setup.", cfg.Port)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
