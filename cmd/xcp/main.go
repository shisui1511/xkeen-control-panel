package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	xkeencontrolpanel "github.com/shisui1511/xkeen-control-panel"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/handlers"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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

	// Router-grade RAM/GC limits (STAB-06): Keenetic devices typically have
	// 128-256 MB total RAM shared with the kernel and other services. A
	// soft-memory-limit plus a moderately aggressive GC target keeps XCP's
	// own footprint predictable instead of relying on the Go runtime's
	// default heap-doubling behavior, which can otherwise contribute to
	// OOM-killer intervention on a loaded router (see Phase 99 analysis).
	//
	// 96 MiB (not a tighter 45 MiB) leaves headroom for existing code paths
	// that already buffer multi-MB payloads fully in memory in a single
	// allocation — kernel binary downloads (KernelService.FetchBinary), DAT
	// geodata updates, and diagnostics snapshot .tar.gz creation — so a
	// single such operation doesn't push the soft limit into continuous-GC
	// territory (SetMemoryLimit trades memory for CPU once resident heap
	// approaches it; on low-power router CPUs that trade can itself produce
	// the sluggishness this phase is meant to prevent). GOGC=50 is a milder
	// target than the previous 30 for the same reason — enough headroom
	// between collections to avoid GC thrashing under normal operation,
	// while SetMemoryLimit remains the hard backstop against unbounded
	// growth.
	memLimit := os.Getenv("GOMEMLIMIT")
	if memLimit == "" {
		debug.SetMemoryLimit(96 * 1024 * 1024) // 96 MiB default
	}
	gcPercent := os.Getenv("GOGC")
	if gcPercent == "" {
		debug.SetGCPercent(50) // 50 default
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("Failed to load config: %v. Creating default...", err)
		cfg = config.Default()
		cfg.ConfigPath = *configPath
		if err := os.MkdirAll(filepath.Dir(*configPath), 0755); err == nil {
			_ = config.Save(*configPath, cfg)
		}
	}

	effectiveMemLimit := memLimit
	if effectiveMemLimit == "" {
		effectiveMemLimit = "96MiB (default)"
	}
	effectiveGC := gcPercent
	if effectiveGC == "" {
		effectiveGC = "50 (default)"
	}
	goExp := os.Getenv("GOEXPERIMENT")
	if goExp == "" {
		goExp = "none (greenteagc default)"
	}

	// Setup logging to file if configured with size-based rotation (1 MB)
	if cfg.XCPLogPath != "" {
		logWriter, err := utils.NewRotateWriter(cfg.XCPLogPath, 1*1024*1024)
		if err == nil {
			log.SetOutput(logWriter)
			defer logWriter.Close()
		} else {
			log.Printf("Failed to initialize log rotator for %s: %v", cfg.XCPLogPath, err)
		}
	}

	// System sysctl profile (STAB-06): no-op on non-Entware machines (see
	// DeploySysctlProfile's "/opt/etc missing" detection), so this is safe
	// to always call, including during local development.
	if err := services.DeploySysctlProfile(""); err != nil {
		log.Printf("Failed to deploy sysctl profile: %v", err)
	}

	// Auto-recover a missing Mihomo config.yaml so the kernel has something
	// valid to start with instead of crash-looping (STAB-04).
	if err := services.EnsureDefaultMihomoConfig(cfg.MihomoConfigDir); err != nil {
		log.Printf("Failed to ensure default Mihomo config: %v", err)
	}

	srvCfg := &server.Config{
		Port:             cfg.Port,
		LoopbackPort:     cfg.LoopbackPort,
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

	// Profiling endpoints (protected)
	srv.HandleProtected("/debug/pprof/", pprof.Index)
	srv.HandleProtected("/debug/pprof/cmdline", pprof.Cmdline)
	srv.HandleProtected("/debug/pprof/profile", pprof.Profile)
	srv.HandleProtected("/debug/pprof/symbol", pprof.Symbol)
	srv.HandleProtected("/debug/pprof/trace", pprof.Trace)

	// Public endpoints
	srv.Handle("/api/version", api.Version)
	srv.HandleProtected("/api/capabilities", api.Capabilities)
	srv.Handle("/api/provider.yaml", api.MihomoProviderAdapter)
	srv.Handle("/mihomo/provider.yaml", api.MihomoProviderRedirect)
	srv.Handle("/mihomo/hwid/provider.yaml", api.MihomoProviderRedirect)

	// Protected endpoints
	srv.HandleProtected("/api/config/list", api.ConfigList)
	srv.HandleProtected("/api/config/read", api.ConfigRead)
	srv.HandleProtected("/api/config/save", api.ConfigSave)
	srv.HandleProtected("/api/config/backups", api.ConfigBackups)
	srv.HandleProtected("/api/config/create", api.ConfigCreate)
	srv.HandleProtected("/api/config/delete", api.ConfigDelete)
	srv.HandleProtected("/api/config/rename", api.ConfigRename)
	srv.HandleProtected("/api/config/validate", api.ConfigValidate)
	srv.HandleProtected("/api/config/preflight", api.ConfigPreflight)
	srv.HandleProtected("/api/config/smart-merge", api.ConfigSmartMerge)
	srv.HandleProtected("/api/rules/custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			api.UserRulesList(w, r)
		} else {
			api.UserRulesSave(w, r)
		}
	})
	srv.HandleProtected("/api/config/mihomo-migrate-socket", api.MihomoMigrateSocket)
	srv.HandleProtected("/api/settings", api.SettingsGet)
	srv.HandleProtected("/api/settings/https", api.SettingsHTTPS)
	srv.HandleProtected("/api/settings/dev-mode", api.SettingsDevMode)
	srv.HandleProtected("/api/service/status", api.ServiceStatus)
	srv.HandleProtected("/api/service/control", api.ServiceControl)
	srv.HandleProtected("/api/service/dns-redirect", api.ServiceDNSRedirect)
	srv.HandleProtected("/api/service/restart-log", api.ServiceRestartLog)
	srv.HandleProtected("/api/logs/ws", api.LogsWebSocket)
	srv.HandleProtected("/api/logs/history", api.LogsHistory)
	srv.HandleProtected("/api/logs/flash-health", api.LogsFlashHealth)
	srv.HandleProtected("/api/logs/level", api.LogsSetLevel)
	srv.HandleProtected("/api/logs/clear", api.LogsClear)
	srv.HandleProtected("/api/logs/download", api.LogsDownload)
	srv.HandleProtected("/api/mihomo/status", api.MihomoStatus)
	srv.HandleProtected("/api/mihomo/groups", api.MihomoGroups)
	srv.HandleProtected("/api/mihomo/dns/query", api.MihomoDNSQuery)
	srv.HandleProtected("/api/mihomo/cache/fakeip/flush", api.MihomoFlushFakeIP)
	srv.HandleProtected("/api/mihomo/proxy/", api.MihomoProxy)
	srv.HandleProtected("/api/xray/reality/keygen", api.XrayRealityKeygen)
	srv.HandleProtected("/api/xray/stats", api.XrayStats)
	srv.HandleProtected("/api/xray/grpc/monitoring", api.XrayGRPCMonitoring)
	srv.HandleProtected("/api/xray/test-route", api.XrayTestRoute)
	srv.HandleProtected("/api/xray/restart-logger", api.XrayRestartLogger)
	srv.HandleProtected("/api/xray/tls-ping", api.XrayTLSPing)
	srv.HandleProtected("/api/system/stats", api.SystemStats)
	srv.HandleProtected("/api/system/clients", api.SystemClients)
	srv.HandleProtected("/api/system/diagnostics", api.DiagnosticsDownload)

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
	srv.HandleProtected("/api/subscriptions/node-dialer-proxy", api.SubscriptionSetNodeDialerProxy)
	srv.HandleProtected("/api/subscriptions/dialer-proxy-targets", api.SubscriptionDialerProxyTargets)
	srv.HandleProtected("/api/proxy-providers", api.ProxyProvidersRouter)
	srv.HandleProtected("/api/proxy-providers/", api.ProxyProvidersRouter)

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
	smartProxySvc.SetMihomoService(api.MihomoService())
	smartProxySvc.Start()
	api.SetSmartProxyService(smartProxySvc)
	defer smartProxySvc.Stop()

	trafficQuotaSvc := services.NewTrafficQuotaService(cfg.DataDir, cfg.MihomoAPIURL, cfg.MihomoSecret)
	trafficQuotaSvc.SetMihomoService(api.MihomoService())
	trafficQuotaSvc.Start()
	api.SetTrafficQuotaService(trafficQuotaSvc)
	defer trafficQuotaSvc.Stop()

	xrayGRPCSvc := services.NewXrayGRPCService(fmt.Sprintf("127.0.0.1:%d", cfg.XRayAPIPort))
	xrayGRPCSvc.SetActiveKernelFunc(func() string {
		if kSvc := api.KernelService(); kSvc != nil {
			for _, info := range kSvc.List() {
				if info.ProcessStatus == "running" {
					return info.Name
				}
			}
		}
		if xSvc := api.XKeenService(); xSvc != nil {
			if status, err := xSvc.Status(); err == nil {
				lower := strings.ToLower(status)
				if strings.Contains(lower, "xray") {
					return "xray"
				} else if strings.Contains(lower, "mihomo") {
					return "mihomo"
				}
			}
		}
		return "xray"
	})
	xrayGRPCSvc.Start()
	api.SetXrayGRPCService(xrayGRPCSvc)
	defer xrayGRPCSvc.Stop()

	// Watchdog / circuit breaker (STAB-05): supervises the active kernel and
	// disarms the XKEEN_TPROXY iptables interception after 3 consecutive
	// failed health checks, so a wedged proxy kernel doesn't leave the LAN
	// without internet access.
	watchdogSvc := services.NewWatchdogService(api.XKeenService(), cfg.MihomoConfigDir, cfg.XRayConfigDir)
	watchdogSvc.Start()
	defer watchdogSvc.Stop()

	// Unified Log Dispatcher (LOGHUB-04, LOGHUB-05, LOGHUB-06)
	logDir := filepath.Dir(cfg.XCPLogPath)
	if logDir == "" || logDir == "." {
		logDir = "/opt/var/log"
	}
	logDispatcher := services.NewLogDispatcher(cfg.LogSources, logDir, cfg.MihomoAPIURL)
	logDispatcher.Start()
	defer logDispatcher.Stop()
	api.SetLogDispatcher(logDispatcher)

	// Config Snapshots
	xrayDir := filepath.Dir(cfg.XRayConfigDir)                            // e.g. /opt/etc/xray
	xkeenDir := filepath.Join(filepath.Dir(cfg.MihomoConfigDir), "xkeen") // e.g. /opt/etc/xkeen
	snapshotSvc := services.NewSnapshotService(cfg.DataDir, []string{
		xrayDir,
		cfg.MihomoConfigDir,
		xkeenDir,
		cfg.DataDir,
	})
	api.SetSnapshotService(snapshotSvc)
	srv.HandleProtected("/api/snapshots/list", api.SnapshotList)
	srv.HandleProtected("/api/snapshots/create", api.SnapshotCreate)
	srv.HandleProtected("/api/snapshots/upload", api.SnapshotUpload)
	srv.HandleProtected("/api/snapshots/", api.SnapshotRouter)

	// DAT Manager
	datSvc := services.NewDATManagerService()
	api.SetDATManagerService(datSvc)

	srv.HandleProtected("/api/dat/list", api.DATList)
	srv.HandleProtected("/api/dat/tags", api.DATListTags)
	srv.HandleProtected("/api/dat/update", api.DATUpdate)
	srv.HandleProtected("/api/dat/rollback", api.DATRollback)
	srv.HandleProtected("/api/dat/search", api.DATSearch)

	// Xkeen Console
	consoleSvc := services.NewConsoleService(cfg.XKeenBinary)
	api.SetConsoleService(consoleSvc)
	srv.HandleProtected("/api/console/commands", api.ConsoleListCommands)
	srv.HandleProtected("/api/console/execute", api.ConsoleExecute)

	// Interactive PTY Terminal
	ptySvc := services.NewPTYService()
	api.SetPTYService(ptySvc)
	defer ptySvc.CloseAll()
	srv.HandleProtected("/api/terminal/ws", api.TerminalWebSocket)

	// Assets Service
	srv.HandleProtected("/api/assets/definition", api.AssetsDefinition)

	// User Custom Rules Service (TMPL-06, TMPL-07)
	userRulesSvc := services.NewUserRulesService(cfg.DataDir)
	api.SetUserRulesService(userRulesSvc)

	// Templates
	templatesFS, err := xkeencontrolpanel.GetTemplatesFS()
	if err != nil {
		log.Fatalf("failed to load embedded templates: %v", err)
	}
	templateSvc := services.NewTemplateService(templatesFS, cfg.DataDir, cfg.TemplatesRepoURL, api.GetAssetsService())
	api.SetTemplateService(templateSvc)
	srv.HandleProtected("/api/templates/list", api.TemplateList)
	srv.HandleProtected("/api/templates/fetch", api.TemplateFetch)
	srv.HandleProtected("/api/templates/update", api.TemplateUpdate)
	srv.HandleProtected("/api/templates/status", api.TemplateStatus)
	srv.HandleProtected("/api/templates/check", api.TemplateCheck)

	// Фоновый чекер обновлений шаблонов
	templatesCtx, cancelTemplatesChecker := context.WithCancel(context.Background())
	defer cancelTemplatesChecker()
	go templateSvc.StartBackgroundChecker(templatesCtx)

	// Subscriptions + auto-refresh scheduler
	subscriptionSvc := services.NewSubscriptionService(cfg.DataDir, cfg.XRayConfigDir, cfg.MihomoConfigDir)
	subscriptionSvc.SetPanelAddress(cfg.Port, cfg.HTTPS.Enabled, cfg.LoopbackPort)
	subscriptionSvc.SetConsoleService(consoleSvc)
	subscriptionSvc.SetMihomoService(api.MihomoService())
	subscriptionSvc.SetMihomoAPI(cfg.MihomoAPIURL, cfg.MihomoSecret)
	// Fallback-резолвер секрета Clash API: при пустом MihomoSecret в конфиге
	// панели секрет читается из config.yaml Mihomo (как у остальных
	// потребителей Clash API). Без него ручное и автоматическое обновление
	// proxy-providers получает 401 на типовых развертываниях.
	subscriptionSvc.SetMihomoSecretResolver(api.ResolveMihomoSecret)
	api.SetSubscriptionService(subscriptionSvc)

	// Привести блоки proxy-providers в config.yaml к текущему формату: без
	// этого новые директивы блока доезжают до уже настроенных подписок только
	// после ручного пересохранения.
	subscriptionSvc.SyncMihomoProviderBlocks()

	// Start subscription auto-refresh scheduler. It checks every 15 minutes
	// and refreshes any subscription whose Interval has elapsed.
	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	go subscriptionSvc.RunScheduler(schedulerCtx, 15*time.Minute)
	defer cancelScheduler()

	// Run orphaned subscriptions cleanup at startup and schedule it every 24 hours
	go subscriptionSvc.CleanOrphanedSubscriptions()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-schedulerCtx.Done():
				return
			case <-ticker.C:
				subscriptionSvc.CleanOrphanedSubscriptions()
			}
		}
	}()

	// Subscription health checker (TCP-dial каждые 5 минут)
	healthSvc := services.NewSubscriptionHealthService(cfg.DataDir, subscriptionSvc)
	healthSvc.Start()
	api.SetSubscriptionHealthService(healthSvc)
	defer healthSvc.Stop()

	// Network Tools
	networkSvc := services.NewNetworkToolsService(cfg.MihomoAPIURL)
	networkSvc.SetMihomoService(api.MihomoService())
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

	log.Printf("XKeen Control Panel v%s starting... (Go: %s, GOMEMLIMIT: %s, GOGC: %s, GOEXPERIMENT: %s)",
		Version, runtime.Version(), effectiveMemLimit, effectiveGC, goExp)
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
		cancelTemplatesChecker()
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
		sig := <-sigCh
		log.Printf("Received signal %s during restart, exiting...", sig)
	}
	log.Println("Server stopped.")
}
