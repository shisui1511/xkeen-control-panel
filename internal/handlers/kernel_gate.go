package handlers

import (
	"net/http"
	"sort"
	"strings"
)

// GateMode — как маршрут ядра относится к состоянию процесса ядра.
type GateMode string

const (
	// GateLive — маршрут опрашивает живой процесс ядра (Clash API, gRPC,
	// счётчики). При неактивном ядре он сразу отвечает 409 kernel_inactive:
	// без гейта запрос висит до таймаута.
	GateLive GateMode = "live"
	// GateData — маршрут работает с файлами и хранилищем панели и нужен при
	// любом активном ядре: правка конфигурации неактивного ядра — сознательный
	// поток, гейтить его нельзя.
	GateData GateMode = "data"
)

// KernelRoutePolicy — запись таблицы «маршрут → ядро / режим / причина».
type KernelRoutePolicy struct {
	// Kernel — ядро, к которому относится маршрут ("xray" | "mihomo").
	Kernel string
	Mode   GateMode
	// Reason — почему маршрут помечен именно так (что опрашивает live,
	// зачем нужен при неактивном ядре data).
	Reason string
}

// kernelGatedPrefixes — префиксы маршрутов, чья регистрация обязана иметь
// запись в kernelRoutePolicies. Маршрут под префиксом без записи не стартует.
var kernelGatedPrefixes = []string{
	"/api/mihomo/",
	"/api/xray/",
	"/api/smart-proxy/",
	"/api/traffic/",
	"/api/rule-providers/",
	"/api/rules/test",
	"/api/dat/",
	"/api/outbound/",
	"/api/proxy-providers",
}

const (
	reasonMihomoClash   = "обращается к Clash API Mihomo"
	reasonMihomoCounter = "живые счётчики трафика Mihomo"
	reasonXrayGRPC      = "обращается к gRPC-API Xray"
	reasonProfileFiles  = "файлы профилей Mihomo и проверка mihomo -t: конструктор работает при любом ядре"
	reasonQuotaStore    = "JSON-хранилище квот панели: настройка доступна при любом ядре"
	reasonSmartStore    = "хранилище профилей SmartProxy и расписание по часам, процесс ядра не опрашивается"
	reasonGeoFiles      = "геобазы обоих ядер на диске"
	reasonRuleFiles     = "правила и провайдеры правил читаются из файлов и по URL, без процесса ядра"
	reasonXrayFiles     = "конфиги Xray и локальные утилиты, без gRPC-API"
)

// kernelRoutePolicies — по одной записи на каждый шаблон HandleProtected из
// cmd/xcp/main.go под kernelGatedPrefixes. Сверяется статическим
// стражем scripts/check-kernel-routes.js.
var kernelRoutePolicies = map[string]KernelRoutePolicy{
	// Mihomo, живые маршруты
	"/api/mihomo/proxy/":             {"mihomo", GateLive, "обратный прокси Clash API: " + reasonMihomoClash},
	"/api/mihomo/dns/query":          {"mihomo", GateLive, "DNS-запрос через Clash API: " + reasonMihomoClash},
	"/api/mihomo/cache/fakeip/flush": {"mihomo", GateLive, "сброс кэша fake-ip: " + reasonMihomoClash},
	"/api/mihomo/connections/ws":     {"mihomo", GateLive, "WebSocket подключений: " + reasonMihomoClash},
	"/api/proxy-providers/":          {"mihomo", GateLive, "обновление и узлы провайдера: " + reasonMihomoClash},
	"/api/traffic/ws":                {"mihomo", GateLive, "WebSocket трафика: " + reasonMihomoCounter},
	"/api/traffic/stats":             {"mihomo", GateLive, reasonMihomoCounter},
	"/api/traffic/alerts":            {"mihomo", GateLive, "оповещения по живым счётчикам Mihomo"},
	"/api/traffic/alerts/clear":      {"mihomo", GateLive, "сброс оповещений по живым счётчикам Mihomo"},
	"/api/traffic/reset":             {"mihomo", GateLive, "сброс живых счётчиков Mihomo"},

	// Xray, живые маршруты
	"/api/xray/stats":          {"xray", GateLive, "статистика: " + reasonXrayGRPC},
	"/api/xray/restart-logger": {"xray", GateLive, "перезапуск логгера: " + reasonXrayGRPC},

	// Mihomo, данные
	"/api/mihomo/status":            {"mihomo", GateData, "локальный статус службы Mihomo, нужен и когда ядро остановлено"},
	"/api/mihomo/groups":            {"mihomo", GateData, "группы читаются из конфигурации на диске: вкладка работает при любом ядре"},
	"/api/mihomo/profiles":          {"mihomo", GateData, reasonProfileFiles},
	"/api/mihomo/profiles/create":   {"mihomo", GateData, reasonProfileFiles},
	"/api/mihomo/profiles/rename":   {"mihomo", GateData, reasonProfileFiles},
	"/api/mihomo/profiles/delete":   {"mihomo", GateData, reasonProfileFiles},
	"/api/mihomo/profiles/activate": {"mihomo", GateData, reasonProfileFiles},
	"/api/mihomo/profiles/adopt":    {"mihomo", GateData, reasonProfileFiles},
	"/api/proxy-providers":          {"mihomo", GateData, "список подписок обоих ядер для вкладки «Провайдеры», при недоступном Clash API деградирует без него"},

	// Xray, данные
	"/api/xray/access-log":        {"xray", GateData, reasonXrayFiles},
	"/api/xray/access-log/toggle": {"xray", GateData, reasonXrayFiles},
	"/api/xray/reality/keygen":    {"xray", GateData, "генерация ключей Reality локально: " + reasonXrayFiles},
	"/api/xray/uuid":              {"xray", GateData, "генерация UUID локально: " + reasonXrayFiles},
	"/api/xray/tls-ping":          {"xray", GateData, "TLS-проверка с роутера напрямую: " + reasonXrayFiles},
	"/api/xray/grpc/monitoring":   {"xray", GateData, "настройка мониторинга в конфиге Xray на диске"},
	"/api/xray/test-route":        {"xray", GateData, "проверка маршрута по конфигурации на диске"},

	// Правила и провайдеры правил
	"/api/rules/test":               {"mihomo", GateData, reasonRuleFiles},
	"/api/rule-providers/info":      {"mihomo", GateData, reasonRuleFiles},
	"/api/rule-providers/content":   {"mihomo", GateData, reasonRuleFiles},
	"/api/rule-providers/check-url": {"mihomo", GateData, reasonRuleFiles},

	// Прочее: конструкторы и утилиты
	"/api/outbound/parse": {"xray", GateData, "разбор ссылки в outbound без процесса ядра: конструктор работает при любом ядре"},

	"/api/dat/list":     {"xray", GateData, reasonGeoFiles},
	"/api/dat/tags":     {"xray", GateData, reasonGeoFiles},
	"/api/dat/update":   {"xray", GateData, reasonGeoFiles},
	"/api/dat/rollback": {"xray", GateData, reasonGeoFiles},
	"/api/dat/search":   {"xray", GateData, reasonGeoFiles},
	"/api/dat/lookup":   {"xray", GateData, reasonGeoFiles},

	"/api/smart-proxy/profiles":         {"mihomo", GateData, reasonSmartStore},
	"/api/smart-proxy/profiles/get":     {"mihomo", GateData, reasonSmartStore},
	"/api/smart-proxy/profiles/add":     {"mihomo", GateData, reasonSmartStore},
	"/api/smart-proxy/profiles/update":  {"mihomo", GateData, reasonSmartStore},
	"/api/smart-proxy/profiles/delete":  {"mihomo", GateData, reasonSmartStore},
	"/api/smart-proxy/profiles/enabled": {"mihomo", GateData, reasonSmartStore},
	"/api/smart-proxy/status":           {"mihomo", GateData, reasonSmartStore},

	"/api/traffic/quotas":         {"mihomo", GateData, reasonQuotaStore},
	"/api/traffic/quotas/get":     {"mihomo", GateData, reasonQuotaStore},
	"/api/traffic/quotas/add":     {"mihomo", GateData, reasonQuotaStore},
	"/api/traffic/quotas/update":  {"mihomo", GateData, reasonQuotaStore},
	"/api/traffic/quotas/delete":  {"mihomo", GateData, reasonQuotaStore},
	"/api/traffic/quotas/enabled": {"mihomo", GateData, reasonQuotaStore},
	"/api/traffic/quotas/reset":   {"mihomo", GateData, reasonQuotaStore},
}

// KernelRoutePolicyFor возвращает политику маршрута по точному шаблону.
func KernelRoutePolicyFor(pattern string) (KernelRoutePolicy, bool) {
	p, ok := kernelRoutePolicies[pattern]
	return p, ok
}

// KernelRoutePolicyKeys возвращает шаблоны всех записей таблицы по алфавиту;
// нужен тесту в cmd/xcp, чтобы найти записи без регистрации маршрута.
func KernelRoutePolicyKeys() []string {
	keys := make([]string, 0, len(kernelRoutePolicies))
	for k := range kernelRoutePolicies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// KernelGatedPrefixes возвращает копию списка префиксов, под которыми каждый
// защищённый маршрут обязан иметь запись в таблице.
func KernelGatedPrefixes() []string {
	return append([]string(nil), kernelGatedPrefixes...)
}

func isKernelGatedPattern(pattern string) bool {
	for _, prefix := range kernelGatedPrefixes {
		if strings.HasPrefix(pattern, prefix) {
			return true
		}
	}
	return false
}

// KernelRouteWrapper — обёртка для Server.SetProtectedWrapper: применяет
// таблицу политик к маршруту при регистрации. Маршрут вне префиксов ядер
// возвращается как есть; под префиксом без записи — паника при старте, чтобы
// новый эндпоинт не мог обойти проверку активного ядра.
func (a *API) KernelRouteWrapper(pattern string, h http.HandlerFunc) http.HandlerFunc {
	if !isKernelGatedPattern(pattern) {
		return h
	}
	p, ok := kernelRoutePolicies[pattern]
	if !ok {
		panic("kernel route policy missing: " + pattern)
	}
	if p.Mode != GateLive {
		return h
	}
	return a.requireKernel(p.Kernel, h)
}

// requireKernel пропускает запрос только когда required — единственное
// активное ядро. Проверка идёт до обработчика, для WebSocket — до апгрейда.
// Без kernelSvc (тестовые сборки API) гейт не действует.
func (a *API) requireKernel(required string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.kernelSvc == nil {
			h(w, r)
			return
		}
		st := a.activeKernelState()
		if !st.Conflict && st.Kernel == required {
			h(w, r)
			return
		}
		JSONErrorKernelInactive(w, a.t(r, "kernel.inactive"), required, st.Label())
	}
}
