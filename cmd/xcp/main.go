package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	xkeencontrolpanel "github.com/shisui1511/xkeen-control-panel"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/handlers"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

var (
	Version    = "dev"
	configPath = flag.String("config", "/opt/etc/xcp/config.json", "Path to config file")
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
		cfg.ConfigPath = *configPath
		if err := os.MkdirAll(filepath.Dir(*configPath), 0755); err == nil {
			_ = config.Save(*configPath, cfg)
		}
	}

	// Setup logging to file if configured
	if cfg.XCPLogPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.XCPLogPath), 0755); err == nil {
			logFile, err := os.OpenFile(cfg.XCPLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				log.SetOutput(logFile)
			} else {
				log.Printf("Failed to open log file %s: %v", cfg.XCPLogPath, err)
			}
		} else {
			log.Printf("Failed to create log directory for %s: %v", cfg.XCPLogPath, err)
		}
	}

	srvCfg := &server.Config{
		Port:             cfg.Port,
		XRayConfigDir:    cfg.XRayConfigDir,
		XKeenBinary:      cfg.XKeenBinary,
		MihomoConfigDir:  cfg.MihomoConfigDir,
		MihomoBinary:     cfg.MihomoBinary,
		AllowedRoots:     cfg.AllowedRoots,
		LogLevel:         cfg.LogLevel,
		DataDir:          cfg.DataDir,
		PasswordHash:     cfg.Auth.PasswordHash,
		SecureCookie:     cfg.Auth.SecureCookie,
		MaxLoginAttempts: cfg.Auth.MaxLoginAttempts,
		LockoutDuration:  time.Duration(cfg.Auth.LockoutDuration) * time.Minute,
		HTTPS: server.HTTPSConfig{
			Enabled:  cfg.HTTPS.Enabled,
			CertPath: cfg.HTTPS.CertPath,
			KeyPath:  cfg.HTTPS.KeyPath,
		},
		SavePasswordHash: func(hash string) error {
			return cfg.SavePasswordHash(cfg.ConfigPath, hash)
		},
	}

	webFS, err := xkeencontrolpanel.GetWebFS()
	if err != nil {
		log.Fatalf("failed to load embedded web assets: %v", err)
	}
	srv, err := server.New(srvCfg, Version, webFS)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Auth endpoints (public)
	authSvc := srv.GetAuthService()
	defer authSvc.Stop()
	srv.Handle("/api/auth/login", authSvc.HandleLogin)
	srv.HandleProtected("/api/auth/logout", authSvc.HandleLogout)
	srv.Handle("/api/auth/me", authSvc.HandleMe)
	srv.Handle("/api/auth/setup", authSvc.HandleSetup)

	// API handlers
	api := handlers.NewAPI(cfg, srv)
	srv.HandleProtected("/api/auth/change-password", api.ChangePassword)

	// Public endpoints
	srv.Handle("/api/version", api.Version)
	srv.HandleProtected("/api/capabilities", api.Capabilities)

	// Protected endpoints
	srv.HandleProtected("/api/config/list", api.ConfigList)
	srv.HandleProtected("/api/config/read", api.ConfigRead)
	srv.HandleProtected("/api/config/save", api.ConfigSave)
	srv.HandleProtected("/api/config/mihomo-merge", api.MihomoMergeSave)
	srv.HandleProtected("/api/config/backups", api.ConfigBackups)
	srv.HandleProtected("/api/config/create", api.ConfigCreate)
	srv.HandleProtected("/api/config/delete", api.ConfigDelete)
	srv.HandleProtected("/api/config/rename", api.ConfigRename)
	srv.HandleProtected("/api/config/validate", api.ConfigValidate)
	srv.HandleProtected("/api/config/preflight", api.ConfigPreflight)
	srv.HandleProtected("/api/settings", api.SettingsGet)
	srv.HandleProtected("/api/settings/https", api.SettingsHTTPS)
	srv.HandleProtected("/api/settings/dev-mode", api.SettingsDevMode)
	srv.HandleProtected("/api/service/status", api.ServiceStatus)
	srv.HandleProtected("/api/service/control", api.ServiceControl)
	srv.HandleProtected("/api/service/restart-log", api.ServiceRestartLog)
	srv.HandleProtected("/api/logs/ws", api.LogsWebSocket)
	srv.HandleProtected("/api/logs/download", api.LogsDownload)
	srv.HandleProtected("/api/mihomo/status", api.MihomoStatus)
	srv.HandleProtected("/api/mihomo/proxy/", api.MihomoProxy)
	srv.HandleProtected("/api/system/stats", api.SystemStats)

	// Update endpoints
	srv.HandleProtected("/api/update/check", api.UpdateCheck)
	srv.HandleProtected("/api/update/changelog", api.UpdateChangelog)
	srv.HandleProtected("/api/update/install", api.UpdateInstall)
	srv.HandleProtected("/api/update/rollback", api.UpdateRollback)
	srv.HandleProtected("/api/update/status", api.UpdateStatusEndpoint)
	srv.HandleProtected("/api/update/events", api.UpdateEventsSSE)
	srv.HandleProtected("/api/update/channel", api.UpdateChannelHandler)

	// Subscription endpoints
	srv.HandleProtected("/api/outbound/parse", api.OutboundParse)
	srv.HandleProtected("/api/outbound/import", api.OutboundImport)
	srv.HandleProtected("/api/outbound/import-bulk", api.OutboundImportBulk)
	srv.HandleProtected("/api/subscriptions", api.SubscriptionList)
	srv.HandleProtected("/api/subscriptions/add", api.SubscriptionAdd)
	srv.HandleProtected("/api/subscriptions/update", api.SubscriptionUpdate)
	srv.HandleProtected("/api/subscriptions/delete", api.SubscriptionDelete)
	srv.HandleProtected("/api/subscriptions/refresh", api.SubscriptionRefresh)
	srv.HandleProtected("/api/subscriptions/refresh-all", api.SubscriptionRefreshAll)
	srv.HandleProtected("/api/subscriptions/raw", api.SubscriptionRaw)
	srv.HandleProtected("/api/subscriptions/parse-report", api.SubscriptionParseReport)
	srv.HandleProtected("/api/subscriptions/nodes", api.SubscriptionNodes)
	srv.HandleProtected("/api/subscriptions/health", api.SubscriptionHealth)
	srv.HandleProtected("/api/subscriptions/active", api.SubscriptionSetActive)

	// Network Tools endpoints
	srv.HandleProtected("/api/network/ping", api.NetworkPing)
	srv.HandleProtected("/api/network/traceroute", api.NetworkTraceroute)
	srv.HandleProtected("/api/network/dns", api.NetworkDNS)
	srv.HandleProtected("/api/network/http", api.NetworkHTTPTest)
	srv.HandleProtected("/api/network/ip", api.NetworkIP)
	srv.HandleProtected("/api/network/proxy-test", api.NetworkProxyTest)
	srv.HandleProtected("/api/network/port-check", api.NetworkPortCheck)

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
	srv.HandleProtected("/api/traffic/ws", api.TrafficWebSocket)
	srv.HandleProtected("/api/traffic/reset", api.TrafficReset)
	srv.HandleProtected("/api/mihomo/connections/ws", api.ConnectionsWebSocket)

	// Start background services
	smartProxySvc := services.NewSmartProxyService(cfg.DataDir, cfg.MihomoAPIURL)
	smartProxySvc.Start()
	api.SetSmartProxyService(smartProxySvc)
	defer smartProxySvc.Stop()

	trafficQuotaSvc := services.NewTrafficQuotaService(cfg.DataDir, cfg.MihomoAPIURL, cfg.MihomoSecret)
	trafficQuotaSvc.Start()
	api.SetTrafficQuotaService(trafficQuotaSvc)
	defer trafficQuotaSvc.Stop()

	// Config Snapshots
	snapshotSvc := services.NewSnapshotService(cfg.DataDir, []string{cfg.XRayConfigDir, cfg.MihomoConfigDir})
	api.SetSnapshotService(snapshotSvc)
	srv.HandleProtected("/api/snapshots/list", api.SnapshotList)
	srv.HandleProtected("/api/snapshots/create", api.SnapshotCreate)
	srv.HandleProtected("/api/snapshots/", api.SnapshotRouter)

	// DAT Manager
	datSvc := services.NewDATManagerService()
	api.SetDATManagerService(datSvc)

	srv.HandleProtected("/api/dat/list", api.DATList)
	srv.HandleProtected("/api/dat/tags", api.DATListTags)
	srv.HandleProtected("/api/dat/update", api.DATUpdate)
	srv.HandleProtected("/api/dat/rollback", api.DATRollback)

	// Xkeen Console
	consoleSvc := services.NewConsoleService(cfg.XKeenBinary)
	api.SetConsoleService(consoleSvc)
	srv.HandleProtected("/api/console/commands", api.ConsoleListCommands)
	srv.HandleProtected("/api/console/execute", api.ConsoleExecute)

	// Templates
	templatesFS, err := xkeencontrolpanel.GetTemplatesFS()
	if err != nil {
		log.Fatalf("failed to load embedded templates: %v", err)
	}
	templateSvc := services.NewTemplateService(templatesFS, cfg.DataDir)
	api.SetTemplateService(templateSvc)
	srv.HandleProtected("/api/templates/list", api.TemplateList)
	srv.HandleProtected("/api/templates/fetch", api.TemplateFetch)
	srv.HandleProtected("/api/templates/update", api.TemplateUpdate)

	// Subscriptions + auto-refresh scheduler
	subscriptionSvc := services.NewSubscriptionService(cfg.DataDir, cfg.XRayConfigDir, cfg.MihomoConfigDir)
	subscriptionSvc.SetConsoleService(consoleSvc)
	api.SetSubscriptionService(subscriptionSvc)

	// Start subscription auto-refresh scheduler. It checks every 15 minutes
	// and refreshes any subscription whose Interval has elapsed.
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	go subscriptionSvc.RunScheduler(schedulerCtx, 15*time.Minute)
	defer cancelScheduler()

	// Subscription health checker (TCP-dial каждые 5 минут)
	healthSvc := services.NewSubscriptionHealthService(cfg.DataDir, subscriptionSvc)
	healthSvc.Start()
	api.SetSubscriptionHealthService(healthSvc)
	defer healthSvc.Stop()

	// Network Tools
	networkSvc := services.NewNetworkToolsService(cfg.MihomoAPIURL)
	api.SetNetworkToolsService(networkSvc)

	// Kernels
	kernelSvc := services.NewKernelService()
	api.SetKernelService(kernelSvc)
	subscriptionSvc.SetKernelService(kernelSvc)
	srv.HandleProtected("/api/kernels", api.KernelList)
	srv.HandleProtected("/api/kernels/debug", api.KernelDebug)
	srv.HandleProtected("/api/kernels/{name}/check", api.KernelCheck)
	srv.HandleProtected("/api/kernels/{name}/install", api.KernelInstall)
	srv.HandleProtected("/api/kernels/{name}/status", api.KernelStatus)
	srv.HandleProtected("/api/kernels/{name}/channel", api.KernelChannel)
	srv.HandleProtected("/api/kernels/{name}/rollback", api.KernelRollback)
	srv.HandleProtected("/api/kernels/{name}/download", api.KernelDownload)

	log.Printf("XKeen Control Panel v%s starting...", Version)
	if cfg.Auth.PasswordHash == "" {
		log.Printf("⚠️  No password set. Please visit http://localhost:%d to complete setup.", cfg.Port)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	srvErrCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			srvErrCh <- err
		}
	}()

	select {
	case sig := <-sigCh:
		log.Printf("Received signal %s, shutting down...", sig)
		cancelScheduler()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	case err := <-srvErrCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
		// If the server was closed (e.g. during update restart), wait for either a signal
		// or for the update process to call os.Exit().
		select {
		case sig := <-sigCh:
			log.Printf("Received signal %s during restart, exiting...", sig)
		}
	}
	log.Println("Server stopped.")
}
