package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	xkeencontrolpanel "github.com/shisui1511/xkeen-control-panel"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/handlers"
	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/server"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var (
	Version           = "dev"
	configPath        = flag.String("config", "/opt/etc/xcp/config.json", "Path to config file")
	resetPasswordFlag = flag.Bool("reset-password", false, "Reset the admin password from the router (SSH), no restart required")
	passwordStdinFlag = flag.Bool("password-stdin", false, "Read the new password from the first line of stdin (for scripts), requires --reset-password")
	setupCodeFlag     = flag.Bool("setup-code", false, "Print the current first-run setup code (only while no password is set)")
)

func main() {
	// Handle version flag before flag.Parse
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "-version" || arg == "--version" {
			fmt.Println(Version)
			os.Exit(0)
		}
	}

	flag.Usage = func() { printUsage(os.Stderr) }
	flag.Parse()

	// Keenetic/Netcraze firmware keeps the timezone as a POSIX string Go cannot read on its own;
	// without this every schedule and timestamp runs in UTC. Applied before the CLI modes
	// below too: --reset-password appends to the same xcp.log as the daemon.
	appliedTZ := utils.ApplySystemTimezone()

	// --reset-password/--setup-code (134-10) — CLI-режимы, работающие с
	// config.json напрямую, без запуска демона панели вообще. Обрабатываются
	// сразу после flag.Parse(), до любой инициализации демона (лог-файл,
	// sysctl, фоновые сервисы) — они не зависят от неё и не должны её
	// запускать.
	if *passwordStdinFlag && !*resetPasswordFlag {
		fmt.Fprintln(os.Stderr, "--password-stdin требует --reset-password")
		os.Exit(2)
	}
	if *resetPasswordFlag {
		os.Exit(runResetPassword(*configPath, *passwordStdinFlag, defaultCLIDeps()))
	}
	if *setupCodeFlag {
		os.Exit(runSetupCode(*configPath, defaultCLIDeps()))
	}

	// Router-grade RAM/GC limits (STAB-06): Keenetic/Netcraze devices typically have
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
	//
	// Both knobs are env-overridable, but by different mechanisms:
	//   - GOMEMLIMIT: when present in the environment (e.g. via xcp.env),
	//     debug.SetMemoryLimit is skipped entirely and the Go runtime honors
	//     the env value directly.
	//   - GOGC: an explicit numeric value is validated and re-applied via
	//     debug.SetGCPercent; "off" is left for the runtime to honor; an
	//     invalid value is rejected with a log line and replaced by the
	//     hardcoded router default (50).
	// Setting only one knob leaves the other at its hardcoded router default.
	// GOEXPERIMENT (e.g. nogreenteagc) is likewise a pure-runtime escape hatch.
	memLimit := os.Getenv("GOMEMLIMIT")
	if memLimit == "" {
		debug.SetMemoryLimit(96 * 1024 * 1024) // 96 MiB default
	}
	gcPercent := os.Getenv("GOGC")
	if n, err := strconv.Atoi(strings.TrimSpace(gcPercent)); err == nil {
		debug.SetGCPercent(n) // honor an explicit numeric GOGC
	} else if gcPercent == "off" {
		// GC disabled explicitly via env; the runtime already honors it.
	} else {
		if gcPercent != "" {
			log.Printf("Ignoring invalid GOGC=%q, applying router default 50", gcPercent)
		}
		debug.SetGCPercent(50) // 50 default
		gcPercent = ""         // fall back to default reporting below
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

	// Журнал в файл: ротация по размеру (1 МБ), схлопывание повторов (D-16, D-38) и
	// асинхронная запись через ограниченную очередь (utils.AsyncWriter): строку
	// журнала — в том числе журнала доступа (middleware.Logging) и строки остановки
	// по сигналу — не ждёт ни обработчик запроса, ни поток остановки. Под застоем
	// журнала ext4 на флешке запись строки стояла 12–17 с. Закрытие — последним
	// отложенным шагом: дописать очередь не дольше shutdownLogDrain.
	var logWriter *utils.AsyncWriter
	if cfg.XCPLogPath != "" {
		rotator, err := utils.NewRotateWriter(cfg.XCPLogPath, 1*1024*1024)
		if err == nil {
			logWriter = utils.NewAsyncWriter(utils.NewDeduplicatingWriter(rotator), utils.AsyncWriterLimit)
			log.SetOutput(logWriter)
			defer func() { _ = logWriter.Close(shutdownLogDrain) }()
		} else {
			log.Printf("Failed to initialize log rotator for %s: %v", cfg.XCPLogPath, err)
		}
	}
	if appliedTZ != "" {
		log.Printf("Timezone: applied system timezone %s", appliedTZ)
	}

	// Config.Load() не логирует сама (лог-файл на тот момент ещё не
	// настроен, см. выше) — записи миграции TTL сессии (D-06, legacy
	// session_timeout_hours → session_idle_ttl_hours) пишутся сюда, теперь,
	// когда xcp.log уже подключён.
	for _, m := range cfg.Migrations {
		log.Printf("config migration: %s", m)
	}
	if cfg.NeedsSave {
		if err := config.Save(cfg.ConfigPath, cfg); err != nil {
			log.Printf("Failed to save migrated config: %v", err)
		}
	}

	// Аварийный выход сам на носитель не пишет: строка ставится в очередь журнала и
	// ждёт записи не дольше shutdownLogDrain (раньше Flush/Printf/Close xcp.log шли
	// синхронно и под застоем журнала ФС стояли секундами — уже после срока).
	fatalf := func(format string, v ...interface{}) {
		log.Printf(format, v...)
		if logWriter != nil {
			logWriter.Flush(shutdownLogDrain)
		}
		os.Exit(1)
	}
	// Шаги остановки (отложенные Stop ниже) и срок выхода после сигнала.
	stops := &shutdownSteps{}
	// Последним из шагов: дождаться финальных записей, не уложившихся в бюджет.
	defer stops.drainSaves()

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
		Port:               cfg.Port,
		LoopbackPort:       cfg.LoopbackPort,
		XRayConfigDir:      cfg.XRayConfigDir,
		XKeenBinary:        cfg.XKeenBinary,
		MihomoConfigDir:    cfg.MihomoConfigDir,
		MihomoBinary:       cfg.MihomoBinary,
		AllowedRoots:       cfg.AllowedRoots,
		LogLevel:           cfg.LogLevel,
		DataDir:            cfg.DataDir,
		PasswordHash:       cfg.Auth.PasswordHash,
		MaxLoginAttempts:   cfg.Auth.MaxLoginAttempts,
		LockoutDuration:    time.Duration(cfg.Auth.LockoutDuration) * time.Minute,
		SessionIdleTTL:     time.Duration(cfg.Auth.SessionIdleTTLHours) * time.Hour,
		SessionAbsoluteTTL: time.Duration(cfg.Auth.SessionAbsoluteTTLDays) * 24 * time.Hour,
		HTTPS: server.HTTPSConfig{
			CertPath: cfg.HTTPS.CertPath,
			KeyPath:  cfg.HTTPS.KeyPath,
		},
		SavePasswordHash: func(hash string) error {
			return cfg.SavePasswordHash(cfg.ConfigPath, hash)
		},
	}

	webFS, err := xkeencontrolpanel.GetWebFS()
	if err != nil {
		fatalf("failed to load embedded web assets: %v", err)
	}
	srv, err := server.New(srvCfg, Version, webFS)
	if err != nil {
		fatalf("Failed to create server: %v", err)
	}

	// Auth endpoints (public)
	authSvc := srv.GetAuthService()
	// Финальная запись сессий — со своим бюджетом (shutdownSteps.save).
	defer stops.save("auth", authSvc.Stop)

	// SIGHUP триггерит горячую перезагрузку пароля (D-22): xcp --reset-password
	// (134-10) меняет password_hash в config.json, пока панель уже запущена —
	// без этого обработчика SIGHUP завершил бы процесс (поведение Go по
	// умолчанию), а не применил новый пароль без рестарта.
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	defer signal.Stop(hupCh)
	go func() {
		for range hupCh {
			if err := reloadAuthFromConfig(*configPath, cfg, authSvc); err != nil {
				log.Printf("[auth] password reload failed: %v", err)
			}
		}
	}()

	srv.Handle("/api/auth/login", authSvc.HandleLogin)
	srv.HandleProtected("/api/auth/logout", authSvc.HandleLogout)
	srv.Handle("/api/auth/me", authSvc.HandleMe)
	srv.Handle("/api/auth/setup", authSvc.HandleSetup)

	// API handlers
	api := handlers.NewAPI(cfg, srv)
	// Гейт по активному ядру для всех защищённых маршрутов ядер (таблица в
	// handlers/kernel_gate.go); вызывать до регистрации защищённых маршрутов.
	srv.SetProtectedWrapper(api.KernelRouteWrapper)

	// Кэш статуса XKeen: единственный владелец опроса `xkeen -status` / `-v`,
	// обработчики отвечают из его снимка
	xkeenStatusCache := services.NewXKeenStatusCache(api.XKeenService())
	xkeenStatusCache.Start()
	api.SetXKeenStatusCache(xkeenStatusCache)
	defer stops.run("xkeen-status-cache", xkeenStatusCache.Stop)
	srv.HandleProtected("/api/auth/change-password", api.ChangePassword)
	srv.HandleProtected("/api/auth/sessions", api.AuthSessions)
	srv.HandleProtected("/api/auth/sessions/terminate", api.AuthSessionTerminate)
	srv.HandleProtected("/api/auth/sessions/terminate-others", api.AuthSessionsTerminateOthers)

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
	srv.HandleProtected("/api/rules/test", api.RouteTest)
	srv.HandleProtected("/api/rule-providers/info", api.RuleProvidersInfo)
	srv.HandleProtected("/api/rule-providers/content", api.RuleProviderContent)
	srv.HandleProtected("/api/rule-providers/check-url", api.RuleProviderCheckURL)
	srv.HandleProtected("/api/config/mihomo-migrate-socket", api.MihomoMigrateSocket)
	srv.HandleProtected("/api/settings", api.SettingsGet)
	srv.HandleProtected("/api/settings/dev-mode", api.SettingsDevMode)
	srv.HandleProtected("/api/settings/config-layer", api.SettingsConfigLayer)
	srv.HandleProtected("/api/settings/session", api.SessionSettings)

	// XKeen own settings: proxied/excluded ports, excluded IPs, xkeen.json
	xkeenSettingsSvc := services.NewXKeenSettingsService(services.DefaultXKeenConfigDir, cfg.DataDir, cfg.AllowedRoots)
	api.SetXKeenSettingsService(xkeenSettingsSvc)
	srv.HandleProtected("/api/xkeen/settings", api.XKeenSettingsList)
	srv.HandleProtected("/api/xkeen/settings/validate", api.XKeenSettingsValidate)
	srv.HandleProtected("/api/xkeen/settings/save", api.XKeenSettingsSave)

	// Mihomo profiles: profiles/<name>.yaml with config.yaml as a symlink
	api.SetMihomoProfileService(services.NewMihomoProfileService(cfg.MihomoConfigDir, cfg.DataDir))
	srv.HandleProtected("/api/mihomo/profiles", api.MihomoProfiles)
	for _, action := range []string{"create", "rename", "delete", "activate", "adopt"} {
		srv.HandleProtected("/api/mihomo/profiles/"+action, api.MihomoProfileAction)
	}

	// Xray access log by device
	api.SetXrayAccessLogService(services.NewXrayAccessLogService(cfg.XRayConfigDir))
	srv.HandleProtected("/api/xray/access-log", api.XrayAccessLog)
	srv.HandleProtected("/api/xray/access-log/toggle", api.XrayAccessLogToggle)
	srv.HandleProtected("/api/service/status", api.ServiceStatus)
	srv.HandleProtected("/api/service/control", api.ServiceControl)
	srv.HandleProtected("/api/service/dns-redirect", api.ServiceDNSRedirect)
	srv.HandleProtected("/api/service/restart-log", api.ServiceRestartLog)
	srv.HandleProtected("/api/service/watchdog/status", api.WatchdogStatus)
	srv.HandleProtected("/api/service/watchdog/reset", api.WatchdogReset)
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
	srv.HandleProtected("/api/xray/uuid", api.XrayUUID)
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
	srv.HandleProtected("/api/update/backups", api.UpdateBackups)
	srv.HandleProtected("/api/update/state", api.UpdateStateHandler)
	srv.HandleProtected("/api/update/settings", api.UpdateSettingsHandler)
	srv.HandleProtected("/api/update/status", api.UpdateStatusEndpoint)
	srv.HandleProtected("/api/update/events", api.UpdateEventsSSE)
	srv.HandleProtected("/api/update/channel", api.UpdateChannelHandler)

	// Subscription endpoints
	srv.HandleProtected("/api/outbound/parse", api.OutboundParse)
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
	srv.HandleProtected("/api/subscriptions/active/clear", api.SubscriptionClearActive)
	srv.HandleProtected("/api/subscriptions/node-dialer-proxy", api.SubscriptionSetNodeDialerProxy)
	srv.HandleProtected("/api/subscriptions/dialer-proxy-targets", api.SubscriptionDialerProxyTargets)
	srv.HandleProtected("/api/proxy-providers", api.ProxyProvidersRouter)
	srv.HandleProtected("/api/proxy-providers/", api.ProxyProvidersRouter)

	// Network IP endpoint (Router WAN IP detection)
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
	srv.HandleProtected("/api/traffic/ws", api.TrafficWebSocket)
	srv.HandleProtected("/api/traffic/reset", api.TrafficReset)
	srv.HandleProtected("/api/mihomo/connections/ws", api.ConnectionsWebSocket)

	// Kernel service must exist before background services that query it
	// (traffic quota liveness check) are started.
	kernelSvc := services.NewKernelService(cfg.DataDir)
	kernelSvc.SetMihomoConfigDir(cfg.MihomoConfigDir)
	api.SetKernelService(kernelSvc)
	// После start/stop/restart/switch кэш статуса и активного ядра обновляются
	// сразу, без ожидания. Хук однослотовый, поэтому оба сброса — в одной функции.
	api.XKeenService().SetLifecycleHook(func() {
		xkeenStatusCache.Invalidate()
		kernelSvc.InvalidateActiveState()
	})

	// Start background services
	smartProxySvc := services.NewSmartProxyService(cfg.DataDir, cfg.MihomoAPIURL)
	smartProxySvc.SetMihomoService(api.MihomoService())
	smartProxySvc.Start()
	api.SetSmartProxyService(smartProxySvc)
	defer stops.run("smart-proxy", smartProxySvc.Stop)

	trafficQuotaSvc := services.NewTrafficQuotaService(cfg.DataDir, cfg.MihomoAPIURL, cfg.MihomoSecret)
	trafficQuotaSvc.SetMihomoService(api.MihomoService())
	// Живость ядра — по процессам, без определения версии: опрос квот не должен
	// ждать конца операции XKeen (ProcessStates не запускает бинарники).
	trafficQuotaSvc.SetKernelAliveCheck(func() bool {
		for _, st := range kernelSvc.ProcessStates() {
			if st.Name == "mihomo" && st.Status == "running" {
				return true
			}
		}
		return false
	})
	trafficQuotaSvc.Start()
	if xSvc := api.XKeenService(); xSvc != nil {
		xSvc.SetKernelStartedHook(trafficQuotaSvc.NotifyKernelStarted)
	}
	api.SetTrafficQuotaService(trafficQuotaSvc)
	// Финальная запись счётчиков трафика — со своим бюджетом (shutdownSteps.save).
	defer stops.save("traffic-quota", trafficQuotaSvc.Stop)

	xrayGRPCSvc := services.NewXrayGRPCService(fmt.Sprintf("127.0.0.1:%d", cfg.XRayAPIPort))
	// Единый резолвер активного ядра; при конфликте Kernel пуст, и gRPC Xray не опрашивается.
	xrayGRPCSvc.SetActiveKernelFunc(func() string { return kernelSvc.ActiveState().Kernel })
	xrayGRPCSvc.Start()
	api.SetXrayGRPCService(xrayGRPCSvc)
	defer stops.run("xray-grpc", xrayGRPCSvc.Stop)

	// Watchdog / circuit breaker (STAB-05): supervises the active kernel and
	// disarms the XKEEN_TPROXY iptables interception after 3 consecutive
	// failed health checks, so a wedged proxy kernel doesn't leave the LAN
	// without internet access.
	watchdogSvc := services.NewWatchdogService(api.XKeenService(), cfg.MihomoConfigDir, cfg.XRayConfigDir)
	// Пока идёт установка, откат или загрузка ядра, проверки здоровья
	// приостановлены: замена бинарника под нагрузкой не должна снимать TPROXY (KERN-03).
	watchdogSvc.SetKernelBusyFunc(kernelSvc.Busy)
	// Если аварийное снятие удалило правила перехвата, после восстановления ядра
	// watchdog один раз возвращает их перезапуском XKeen (KERN-03).
	if xSvc := api.XKeenService(); xSvc != nil {
		watchdogSvc.SetRearmFunc(func() error {
			_, err := xSvc.Restart()
			return err
		})
	}
	watchdogSvc.Start()
	api.SetWatchdogService(watchdogSvc)
	defer stops.run("watchdog", watchdogSvc.Stop)

	// Фоновая проверка обновлений (уведомления) и автоустановка в окно времени
	updateScheduler := handlers.NewUpdateScheduler(api)
	updateScheduler.Start()
	api.SetUpdateScheduler(updateScheduler)
	defer stops.run("update-scheduler", updateScheduler.Stop)

	// Unified Log Dispatcher (LOGHUB-04, LOGHUB-05, LOGHUB-06)
	logDir := filepath.Dir(cfg.XCPLogPath)
	if logDir == "" || logDir == "." {
		logDir = "/opt/var/log"
	}
	logDispatcher := services.NewLogDispatcher(cfg.LogSources, logDir, cfg.MihomoAPIURL)
	logDispatcher.Start()
	defer stops.run("log-dispatcher", logDispatcher.Stop)
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
	snapshotSvc.SetXrayRoot(cfg.XRayConfigDir)
	api.SetSnapshotService(snapshotSvc)
	srv.HandleProtected("/api/snapshots/list", api.SnapshotList)
	srv.HandleProtected("/api/snapshots/create", api.SnapshotCreate)
	srv.HandleProtected("/api/snapshots/upload", api.SnapshotUpload)
	srv.HandleProtected("/api/snapshots/", api.SnapshotRouter)

	// DAT Manager
	datSvc := services.NewDATManagerService()
	datSvc.SetBinaries(cfg.MihomoBinary, cfg.XrayBinary, cfg.XRayConfigDir)
	api.SetDATManagerService(datSvc)

	srv.HandleProtected("/api/dat/list", api.DATList)
	srv.HandleProtected("/api/dat/tags", api.DATListTags)
	srv.HandleProtected("/api/dat/update", api.DATUpdate)
	srv.HandleProtected("/api/dat/rollback", api.DATRollback)
	srv.HandleProtected("/api/dat/search", api.DATSearch)
	srv.HandleProtected("/api/dat/lookup", api.DATLookup)

	// Xkeen Console
	consoleSvc := services.NewConsoleService(cfg.XKeenBinary)
	api.SetConsoleService(consoleSvc)
	srv.HandleProtected("/api/console/commands", api.ConsoleListCommands)
	srv.HandleProtected("/api/console/execute", api.ConsoleExecute)

	// Interactive PTY Terminal
	ptySvc := services.NewPTYService()
	api.SetPTYService(ptySvc)
	defer stops.run("pty", ptySvc.CloseAll)
	api.SetXKeenInstaller(services.NewXKeenInstaller())
	srv.HandleProtected("/api/terminal/ws", api.TerminalWebSocket)

	// Assets Service
	srv.HandleProtected("/api/assets/definition", api.AssetsDefinition)

	// User Custom Rules Service (TMPL-06, TMPL-07)
	userRulesSvc := services.NewUserRulesService(cfg.DataDir)
	api.SetUserRulesService(userRulesSvc)

	// Route Tracer Service (ROUTE-04)
	routeTracerSvc := services.NewRouteTracerService(userRulesSvc, api.MihomoService(), cfg.MihomoConfigDir)
	routeTracerSvc.SetRuleEvaluation(datSvc.MihomoGeoTagLookup(cfg.MihomoConfigDir), cfg.MihomoBinary, filepath.Join(cfg.DataDir, "cache", "rulesets"))
	api.SetRouteTracerService(routeTracerSvc)

	// Templates
	templatesFS, err := xkeencontrolpanel.GetTemplatesFS()
	if err != nil {
		fatalf("failed to load embedded templates: %v", err)
	}
	templateSvc := services.NewTemplateService(templatesFS, cfg.DataDir)
	api.SetTemplateService(templateSvc)
	srv.HandleProtected("/api/templates/list", api.TemplateList)

	// Subscriptions + auto-refresh scheduler
	subscriptionSvc := services.NewSubscriptionService(cfg.DataDir, cfg.XRayConfigDir, cfg.MihomoConfigDir)
	subscriptionSvc.SetPanelAddress(cfg.Port, cfg.HTTPS.Enabled, cfg.LoopbackPort)
	subscriptionSvc.SetKernelApplier(api.KernelApplier())
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
	defer stops.run("subscription-health", healthSvc.Stop)

	// Network Tools
	networkSvc := services.NewNetworkToolsService(cfg.MihomoAPIURL)
	networkSvc.SetMihomoService(api.MihomoService())
	api.SetNetworkToolsService(networkSvc)

	// Kernels
	subscriptionSvc.SetKernelService(kernelSvc)
	srv.HandleProtected("/api/kernels", api.KernelList)
	srv.HandleProtected("/api/kernels/debug", api.KernelDebug)
	srv.HandleProtected("/api/kernels/{name}/check", api.KernelCheck)
	srv.HandleProtected("/api/kernels/{name}/install", api.KernelInstall)
	srv.HandleProtected("/api/kernels/{name}/status", api.KernelStatus)
	srv.HandleProtected("/api/kernels/{name}/channel", api.KernelChannel)
	srv.HandleProtected("/api/kernels/{name}/rollback", api.KernelRollback)
	srv.HandleProtected("/api/kernels/{name}/upload", api.KernelUpload)
	srv.HandleProtected("/api/kernels/{name}/download", api.KernelDownload)

	// Слой «Конфигурация» (фаза 144): создаётся и запускается здесь, Stop — через
	// defer. Замок жизненного цикла — тот же, что у KernelApplier и обработчиков.
	// Не создался — слой не подключается, маршруты отвечают 404.
	configLayer, err := configlayer.New(configlayer.Options{
		DataDir: cfg.DataDir,
		Roots:   configlayer.Roots{Xray: cfg.XRayConfigDir, Mihomo: cfg.MihomoConfigDir},
		Enabled: func() bool {
			cfg.RLock()
			defer cfg.RUnlock()
			return cfg.ConfigLayer
		},
		DevMode: func() bool {
			cfg.RLock()
			defer cfg.RUnlock()
			return cfg.DevMode
		},
		Binaries: func() configlayer.Binaries {
			return configlayer.Binaries{
				Xray:   installedKernelBinary(kernelSvc, "xray"),
				Mihomo: installedKernelBinary(kernelSvc, "mihomo"),
			}
		},
		XrayEnv: func(dir string) []string { return utils.XrayAssetEnv(os.Environ(), dir) },
		KernelVersions: func() []configlayer.KernelVersionInput {
			cfg.RLock()
			xkeenBin := cfg.XKeenBinary
			cfg.RUnlock()
			_, statErr := os.Stat(xkeenBin)
			inputs := []configlayer.KernelVersionInput{{
				Name:      "xkeen",
				Installed: xkeenBin != "" && statErr == nil,
				Version:   xkeenStatusCache.Snapshot().Version,
			}}
			// Здесь нужна версия (версионные ворота): при пустом кеше List ждёт конца
			// операции XKeen, не дольше kernelHelperWait.
			for _, info := range kernelSvc.List() {
				inputs = append(inputs, configlayer.KernelVersionInput{
					Name:      info.Name,
					Installed: info.ProcessStatus != "not_installed",
					Version:   info.CurrentVersion,
				})
			}
			return inputs
		},
		Applier:       api.KernelApplier(),
		ProcessStates: kernelSvc.ProcessStates,
		Mihomo:        api.MihomoService(),
		MihomoAPIReady: func() bool {
			reachable, _ := api.MihomoService().ProbeAPI(api.ResolveMihomoSecret())
			return reachable
		},
		Lifecycle: api.LifecycleLock(),
		// Фрагменты существующих подписок ведёт старый слой: новый не считает их
		// сиротами. Набор запрашивается при каждом обращении, подписки меняются.
		ForeignOwned: func(kernel, rel string) bool {
			return kernel == configlayer.KernelXray && subscriptionSvc.OwnedFileNames()[filepath.Base(rel)]
		},
	})
	if err != nil {
		log.Printf("Слой конфигурации не создан, маршруты /api/configlayer/* отвечают 404: %v", err)
	} else {
		// Сервис источников (фаза 145): создаётся только здесь. Генераторы, хук
		// фиксации живых данных и колбэк «дрейф решён» регистрируются до Start слоя
		// (после него слой их не принимает). Не создался или не подключился —
		// маршруты /api/configlayer/sources* отвечают 404.
		// Одни и те же источники сведений о ядрах у сервисов источников и
		// маршрутизации.
		installedKernels := func() configlayer.InstalledKernels {
			return configlayer.InstalledKernels{
				Xray:   installedKernelBinary(kernelSvc, "xray") != "",
				Mihomo: installedKernelBinary(kernelSvc, "mihomo") != "",
			}
		}
		// Нужна версия (UA подписки Mihomo, ворота функций): при пустом кеше Get
		// ждёт конца операции XKeen, не дольше kernelHelperWait.
		kernelVersion := func(kernel string) string {
			info := kernelSvc.Get(kernel)
			if info == nil {
				return ""
			}
			// Служебные значения вместо версии — версия не определена.
			switch info.CurrentVersion {
			case "not installed", "error", "unknown":
				return ""
			}
			return info.CurrentVersion
		}
		kernelRunning := func(kernel string) bool {
			for _, st := range kernelSvc.ProcessStates() {
				if st.Name == kernel && st.Status == "running" {
					return true
				}
			}
			return false
		}
		sourcesSvc, srcErr := sources.New(sources.Deps{
			DataDir:       cfg.DataDir,
			Layer:         configLayer,
			Installed:     installedKernels,
			KernelVersion: kernelVersion,
			ActiveKernel:  func() string { return kernelSvc.ActiveState().Kernel },
			Running:       kernelRunning,
		})
		if srcErr != nil {
			log.Printf("Сервис источников не создан, маршруты /api/configlayer/sources* отвечают 404: %v", srcErr)
			sourcesSvc = nil
		}
		if sourcesSvc != nil {
			for _, g := range sourcesSvc.Generators() {
				if srcErr = configLayer.RegisterGenerator(g); srcErr != nil {
					break
				}
			}
			if srcErr == nil {
				srcErr = configLayer.AddLiveCommitter(sourcesSvc.CommitLive)
			}
			if srcErr == nil {
				srcErr = configLayer.OnDriftResolved(sourcesSvc.OnDriftResolved)
			}
			if srcErr != nil {
				log.Printf("Сервис источников не подключён к слою, маршруты /api/configlayer/sources* отвечают 404: %v", srcErr)
				sourcesSvc = nil
			}
		}
		// Сервис маршрутизации (фаза 146) создаётся только здесь, после сервиса
		// источников: генераторы файлов ядер и проверка регистрируются до Start
		// слоя. Не создался или не подключился — маршруты
		// /api/configlayer/routing* отвечают 404, панель работает без них.
		var routingSvc *routing.Service
		if sourcesSvc != nil {
			var routeErr error
			routingSvc, routeErr = routing.New(routing.Deps{
				DataDir: cfg.DataDir,
				Layer:   configLayer,
				Sources: sourcesSvc,
				Lists:   lists.NewIndex(lists.DefaultDatDir),
				Geodata: lists.NewGeodataStore(xkeenSettingsSvc, exec.LookPath, func(ctx context.Context) error {
					return runXKeenUpdateGeo(ctx, consoleSvc)
				}),
				Installed:     installedKernels,
				KernelVersion: kernelVersion,
				Running:       kernelRunning,
				Mihomo:        api.MihomoService(),
				Ping:          pingNodeTLS,
				GOARCH:        runtime.GOARCH,
			})
			if routeErr == nil {
				for _, g := range routingSvc.Generators() {
					if routeErr = configLayer.RegisterGenerator(g); routeErr != nil {
						break
					}
				}
			}
			if routeErr == nil {
				routeErr = configLayer.RegisterChecker(routingSvc.Checker())
			}
			// Хуки выбора участника (D-36, D-39): фиксатор выбора идёт после
			// фиксатора источников (каталог узлов считается по уже зафиксированным
			// источникам), сверка now и повторный выбор — после каждого применения.
			if routeErr == nil {
				routeErr = configLayer.AddLiveCommitter(routingSvc.CommitLive)
			}
			if routeErr == nil {
				routeErr = configLayer.AddAfterApply(routingSvc.AfterApply)
			}
			if routeErr == nil {
				// Хук групп по умолчанию ставится только после полной регистрации.
				routingSvc.Attach()
			} else {
				log.Printf("Сервис маршрутизации не подключён к слою, маршруты /api/configlayer/routing* отвечают 404: %v", routeErr)
				if routingSvc != nil {
					// Контекст сервиса не должен остаться живым без владельца.
					routingSvc.Stop()
				}
				routingSvc = nil
			}
		}
		configLayer.Start()
		api.SetConfigLayer(configLayer)
		defer stops.run("config-layer", configLayer.Stop)
		if sourcesSvc != nil {
			// Планировщик подписок стартует после слоя; defer идёт после defer слоя,
			// поэтому сервис останавливается раньше слоя.
			sourcesSvc.Start()
			api.SetSourcesService(sourcesSvc)
			defer stops.run("sources", sourcesSvc.Stop)
		}
		if routingSvc != nil {
			// Как и сервис источников: стартует после слоя, останавливается раньше него.
			routingSvc.Start()
			api.SetRoutingService(routingSvc)
			defer stops.run("routing", routingSvc.Stop)
			// Наблюдатель выбора (D-39): при каждом новом процессе Mihomo ставит
			// выбор групп из State.Live. Создаётся только здесь; defer идёт после
			// defer сервиса маршрутизации, поэтому наблюдатель останавливается первым.
			selWatch := routing.NewSelectionWatcher(routingSvc, func(kernel string) int {
				for _, st := range kernelSvc.ProcessStates() {
					if st.Name == kernel && st.Status == "running" {
						return st.PID
					}
				}
				return 0
			}, 0)
			selWatch.Start()
			defer stops.run("routing-selection", selWatch.Stop)
		}
	}
	srv.HandleProtected("/api/configlayer/state", api.ConfigLayerState)
	srv.HandleProtected("/api/configlayer/events", api.ConfigLayerEvents)
	srv.HandleProtected("/api/configlayer/draft", api.ConfigLayerDraft)
	srv.HandleProtected("/api/configlayer/draft/reset", api.ConfigLayerDraftReset)
	srv.HandleProtected("/api/configlayer/apply", api.ConfigLayerApply)
	srv.HandleProtected("/api/configlayer/files/rebuild", api.ConfigLayerFilesRebuild)
	srv.HandleProtected("/api/configlayer/files/release", api.ConfigLayerFilesRelease)
	srv.HandleProtected("/api/configlayer/files/diff", api.ConfigLayerFilesDiff)
	srv.HandleProtected("/api/configlayer/notices/dismiss", api.ConfigLayerNoticesDismiss)
	srv.HandleProtected("/api/configlayer/diag", api.ConfigLayerDiag)
	srv.HandleProtected("/api/configlayer/sources", api.ConfigSources)
	srv.HandleProtected("/api/configlayer/sources/preview", api.ConfigSourcesPreview)
	srv.HandleProtected("/api/configlayer/sources/add", api.ConfigSourcesAdd)
	srv.HandleProtected("/api/configlayer/sources/update", api.ConfigSourcesUpdate)
	srv.HandleProtected("/api/configlayer/sources/delete", api.ConfigSourcesDelete)
	srv.HandleProtected("/api/configlayer/sources/refresh", api.ConfigSourcesRefresh)
	srv.HandleProtected("/api/configlayer/sources/reveal", api.ConfigSourcesReveal)
	srv.HandleProtected("/api/configlayer/sources/diagnostics", api.ConfigSourcesDiagnostics)
	srv.HandleProtected("/api/configlayer/sources/nodes/preview", api.ConfigSourcesNodePreview)
	srv.HandleProtected("/api/configlayer/sources/nodes/raw", api.ConfigSourcesNodeRaw)
	srv.HandleProtected("/api/configlayer/sources/nodes/save", api.ConfigSourcesNodeSave)
	srv.HandleProtected("/api/configlayer/sources/nodes/delete", api.ConfigSourcesNodeDelete)
	srv.HandleProtected("/api/configlayer/routing", api.ConfigRouting)
	srv.HandleProtected("/api/configlayer/routing/groups/save", api.ConfigRoutingGroupsSave)
	srv.HandleProtected("/api/configlayer/routing/groups/preview", api.ConfigRoutingGroupsPreview)
	srv.HandleProtected("/api/configlayer/routing/groups/delete", api.ConfigRoutingGroupsDelete)
	srv.HandleProtected("/api/configlayer/routing/impact", api.ConfigRoutingImpact)
	srv.HandleProtected("/api/configlayer/routing/rules/save", api.ConfigRoutingRulesSave)
	srv.HandleProtected("/api/configlayer/routing/rules/delete", api.ConfigRoutingRulesDelete)
	srv.HandleProtected("/api/configlayer/routing/rules/move", api.ConfigRoutingRulesMove)
	srv.HandleProtected("/api/configlayer/routing/rules/enable", api.ConfigRoutingRulesEnable)
	srv.HandleProtected("/api/configlayer/routing/final", api.ConfigRoutingFinal)
	srv.HandleProtected("/api/configlayer/routing/probe", api.ConfigRoutingProbe)
	srv.HandleProtected("/api/configlayer/routing/take", api.ConfigRoutingTake)
	srv.HandleProtected("/api/configlayer/routing/release", api.ConfigRoutingRelease)
	srv.HandleProtected("/api/configlayer/routing/takeover", api.ConfigRoutingTakeover)
	srv.HandleProtected("/api/configlayer/routing/reclaim", api.ConfigRoutingReclaim)
	srv.HandleProtected("/api/configlayer/routing/select", api.ConfigRoutingSelect)
	srv.HandleProtected("/api/configlayer/routing/lists", api.ConfigRoutingLists)
	srv.HandleProtected("/api/configlayer/routing/lists/geodata/preview", api.ConfigRoutingGeodataPreview)
	srv.HandleProtected("/api/configlayer/routing/lists/geodata/add", api.ConfigRoutingGeodataAdd)
	srv.HandleProtected("/api/configlayer/routing/presets", api.ConfigRoutingPresets)
	srv.HandleProtected("/api/configlayer/routing/presets/plan", api.ConfigRoutingPresetsPlan)
	srv.HandleProtected("/api/configlayer/routing/presets/apply", api.ConfigRoutingPresetsApply)

	log.Printf("XKeen Control Panel v%s starting... (Go: %s, GOMEMLIMIT: %s, GOGC: %s, GOEXPERIMENT: %s)",
		strings.TrimPrefix(Version, "v"), runtime.Version(), effectiveMemLimit, effectiveGC, goExp)
	if cfg.Auth.PasswordHash == "" {
		log.Printf("⚠️  No password set. Please visit https://<router-ip>:%d to complete setup.", cfg.Port)
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
		// Срок — до первой строки журнала: остановку ничто не задерживает до него.
		stops.arm(shutdownDeadline, fatalf)
		log.Printf("Received signal %s, shutting down...", sig)
		stops.enter("http")
		cancelScheduler()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownHTTPTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	case err := <-srvErrCh:
		if !errors.Is(err, http.ErrServerClosed) {
			fatalf("Server error: %v", err)
		}
		// If the server was closed (e.g. during update restart), wait for either a signal
		// or for the update process to call os.Exit().
		sig := <-sigCh
		stops.arm(shutdownDeadline, fatalf)
		log.Printf("Received signal %s during restart, exiting...", sig)
	}
	log.Println("Server stopped.")
}

// runXKeenUpdateGeo запускает `xkeen -ug` через консольный сервис. Команда идёт
// в отдельной горутине: сервис ждёт её без контекста, а ожидание вызывающего
// прерывается отменой ctx (процесс при этом доработает сам).
func runXKeenUpdateGeo(ctx context.Context, console *services.ConsoleService) error {
	type result struct {
		res *services.CommandResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		res, err := console.Execute("-ug")
		ch <- result{res, err}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out := <-ch:
		if out.err != nil {
			return out.err
		}
		if out.res == nil || !out.res.Success {
			return errors.New("xkeen -ug завершился с ошибкой")
		}
		return nil
	}
}

// pingNodeTLS проверяет доступность сервера узла по TLS (рукопожатие по адресу,
// порту и SNI узла). Проверка сертификата не нужна: важна достижимость сервера.
// Узел без TLS и Reality (Shadowsocks, VMess и подобные) TLS не отвечает никогда,
// поэтому для него проверяется TCP-соединение с адресом и портом. Протоколы
// поверх UDP (hysteria2, tuic, wireguard) проверяются, как и раньше, по TLS.
// Срок ожидания задаёт ctx; сама проверка ограничена пятью секундами.
func pingNodeTLS(ctx context.Context, n nodes.Node) error {
	if n.TLS == nil && n.Reality == nil && !nodeOverUDP(n.Protocol) {
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var d net.Dialer
		conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(n.Host, strconv.Itoa(n.Port)))
		if err != nil {
			return err
		}
		return conn.Close()
	}
	sni := ""
	if n.TLS != nil {
		sni = n.TLS.SNI
	}
	dest := net.JoinHostPort(n.Host, strconv.Itoa(n.Port))
	type result struct {
		res *services.TLSPingResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		res, err := services.TLSPing(dest, sni, nil, true)
		ch <- result{res, err}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out := <-ch:
		if out.err != nil {
			return out.err
		}
		if out.res == nil || !out.res.OK {
			return errors.New("сервер узла недоступен по TLS")
		}
		return nil
	}
}

// nodeOverUDP сообщает, что протокол узла работает поверх UDP: TCP-соединение с
// его портом ничего не доказывает.
func nodeOverUDP(protocol string) bool {
	switch protocol {
	case "hysteria2", "hysteria", "tuic", "wireguard":
		return true
	}
	return false
}

// installedKernelBinary — путь бинарника ядра, если он есть на диске; иначе
// пустая строка (ядро не установлено). Версия не определяется (BinaryPath, не Get):
// путь нужен слою «Конфигурация» и сервису источников, ждать операции XKeen им незачем.
func installedKernelBinary(kernelSvc *services.KernelService, name string) string {
	path := kernelSvc.BinaryPath(name)
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
