package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// PreflightIssue represents a single validation issue (error or warning) from a preflight check.
type PreflightIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PreflightResult holds the outcome of a preflight validation.
type PreflightResult struct {
	Valid    bool
	Errors   []PreflightIssue
	Warnings []PreflightIssue
}

// ControllerInfo contains parsed information about Mihomo external controller connection.
type ControllerInfo struct {
	Type       string // "unix" | "tcp" | "none"
	Target     string // "/opt/var/run/mihomo.sock" or "127.0.0.1:9090" / "0.0.0.0:9090"
	Secret     string
	IsInsecure bool // true if TCP 0.0.0.0 or :port
}

// ErrMihomoNoSocket означает, что в профиле не настроен Unix-контроллер.
var ErrMihomoNoSocket = errors.New("mihomo unix controller is not configured")

const mihomoControllerURL = "http://localhost"

type MihomoService struct {
	BinaryPath string
	XKeenPath  string
	ConfigDir  string

	ctrlCacheMu sync.RWMutex
	ctrlCache   ControllerInfo
	ctrlFile    os.FileInfo
	ctrlPath    string

	clientMu     sync.Mutex
	cachedClient *http.Client
	cachedTarget string

	controllerMu     sync.Mutex
	controllerCtx    context.Context
	controllerCancel context.CancelFunc

	// provMu охраняет карту «узел провайдера → провайдер» для замера задержки
	// узлов, которых нет в /proxies (providerOf).
	provMu     sync.Mutex
	provByName map[string]string
	provAt     time.Time
}

func NewMihomoService(binary, xkeenPath, configDir string) *MihomoService {
	return &MihomoService{
		BinaryPath: binary,
		XKeenPath:  xkeenPath,
		ConfigDir:  configDir,
	}
}

func (s *MihomoService) Status() (string, error) {
	// Status checks if the binary is in the process list
	cmd := exec.Command("pidof", filepath.Base(s.BinaryPath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "stopped", nil
	}
	pids := strings.Fields(strings.TrimSpace(string(out)))
	var activePids []string
	for _, pidStr := range pids {
		if !isShortLivedOrHelperProcess(pidStr) {
			activePids = append(activePids, pidStr)
		}
	}
	if len(activePids) > 0 {
		return "running (pid: " + strings.Join(activePids, " ") + ")", nil
	}
	return "stopped", nil
}

func (s *MihomoService) ParseControllerConfig() (ControllerInfo, error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	fi, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
		fi, err = os.Stat(configPath)
	}
	if err != nil {
		return ControllerInfo{Type: "none"}, fmt.Errorf("failed to open config: %w", err)
	}

	s.ctrlCacheMu.RLock()
	// При активации симлинк может начать указывать на другой файл с тем же mtime.
	if s.ctrlFile != nil && s.ctrlPath == configPath && os.SameFile(fi, s.ctrlFile) &&
		fi.ModTime().Equal(s.ctrlFile.ModTime()) && fi.Size() == s.ctrlFile.Size() {
		cached := s.ctrlCache
		s.ctrlCacheMu.RUnlock()
		return cached, nil
	}
	s.ctrlCacheMu.RUnlock()

	file, err := os.Open(configPath)
	if err != nil {
		return ControllerInfo{Type: "none"}, fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	fi, err = file.Stat()
	if err != nil {
		return ControllerInfo{Type: "none"}, fmt.Errorf("failed to stat config: %w", err)
	}
	var cfg struct {
		Unix   string `yaml:"external-controller-unix"`
		TCP    string `yaml:"external-controller"`
		Secret string `yaml:"secret"`
		Legacy string `yaml:"external-controller-secret"`
	}
	// Ключи контроллера верхнего уровня могут стоять после правил и провайдеров.
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return ControllerInfo{Type: "none"}, fmt.Errorf("failed to parse config: %w", err)
	}
	res := ControllerInfo{Type: "none", Secret: cfg.Secret}
	if res.Secret == "" {
		res.Secret = cfg.Legacy
	}
	if unixCtrl := strings.TrimSpace(cfg.Unix); unixCtrl != "" {
		res.Type = "unix"
		res.Target = unixCtrl
		if !filepath.IsAbs(unixCtrl) {
			res.Target = filepath.Join(s.ConfigDir, unixCtrl)
		}
	} else if tcpCtrl := strings.TrimSpace(cfg.TCP); tcpCtrl != "" {
		res.Type = "tcp"
		res.Target = tcpCtrl
		res.IsInsecure = strings.HasPrefix(tcpCtrl, "0.0.0.0:") || strings.HasPrefix(tcpCtrl, ":") || tcpCtrl == "0.0.0.0"
	}

	s.ctrlCacheMu.Lock()
	s.ctrlCache = res
	s.ctrlFile = fi
	s.ctrlPath = configPath
	s.ctrlCacheMu.Unlock()

	return res, nil
}

func (s *MihomoService) ParseConfig() (controller string, secret string, err error) {
	info, err := s.ParseControllerConfig()
	if err != nil {
		return "", "", err
	}
	return info.Target, info.Secret, nil
}

// EnsureSocketDir ensures that the directory for Unix domain socket exists.
func (s *MihomoService) EnsureSocketDir() error {
	dir := "/opt/var/run"
	info, err := s.ParseControllerConfig()
	if err == nil && info.Type == "unix" && info.Target != "" {
		dir = filepath.Dir(info.Target)
	}
	return os.MkdirAll(dir, 0755)
}

// CleanupStaleSocket removes the socket file if Mihomo process is not currently running.
func (s *MihomoService) CleanupStaleSocket() error {
	info, err := s.ParseControllerConfig()
	if err != nil || info.Type != "unix" || info.Target == "" {
		return nil
	}
	status, err := s.Status()
	if err == nil && strings.Contains(status, "running") {
		// Process is running, do not delete active socket
		return nil
	}
	if _, err := os.Stat(info.Target); err == nil {
		return os.Remove(info.Target)
	}
	return nil
}

// UnixController возвращает параметры единственного транспорта Clash API панели.
func (s *MihomoService) UnixController() (ControllerInfo, error) {
	info, err := s.ParseControllerConfig()
	if err != nil {
		return ControllerInfo{}, err
	}
	if info.Type != "unix" || info.Target == "" {
		return ControllerInfo{}, ErrMihomoNoSocket
	}
	return info, nil
}

// ControllerContext отменяется при следующем событии жизненного цикла ядра.
// Подписка берётся до чтения профиля, чтобы не потерять переход между чтением и ожиданием.
func (s *MihomoService) ControllerContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	s.controllerMu.Lock()
	if s.controllerCtx == nil {
		s.controllerCtx, s.controllerCancel = context.WithCancel(context.Background())
	}
	stop := context.AfterFunc(s.controllerCtx, cancel)
	s.controllerMu.Unlock()
	return ctx, func() {
		stop()
		cancel()
	}
}

// NotifyControllerChanged будит все службы и отменяет текущие потоки старого профиля.
func (s *MihomoService) NotifyControllerChanged() {
	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()
	if s.controllerCancel != nil {
		s.controllerCancel()
		s.controllerCtx, s.controllerCancel = nil, nil
	}
}

// GetDialContext возвращает dialer для Unix-контроллера текущего профиля.
func (s *MihomoService) GetDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	info, err := s.UnixController()
	return unixControllerDialer(info.Target, err)
}

func unixControllerDialer(target string, err error) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		if err != nil {
			return nil, err
		}
		if target == "" {
			return nil, ErrMihomoNoSocket
		}
		var d net.Dialer
		return d.DialContext(ctx, "unix", target)
	}
}

// GetHTTPTransport returns the shared *http.Transport for the Mihomo API — the
// one behind GetHTTPClient. A fresh transport per request would leave its
// keep-alive connections open and pile up sockets under UI polling.
func (s *MihomoService) GetHTTPTransport() *http.Transport {
	return s.GetHTTPClient().Transport.(*http.Transport)
}

// GetHTTPClient returns an *http.Client with 30s timeout using GetHTTPTransport.
// It reuses the underlying client and connection pool unless the controller target or type changes.
func (s *MihomoService) GetHTTPClient() *http.Client {
	// Без подтверждённого сокета транспорт возвращает ошибку до dial.
	info, _ := s.UnixController()
	return s.httpClient(info.Target)
}

func (s *MihomoService) httpClient(target string) *http.Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()

	if s.cachedClient != nil && s.cachedTarget == target {
		return s.cachedClient
	}

	transport := &http.Transport{
		DialContext:           unixControllerDialer(target, nil),
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,
	}

	// Контроллер сменил адрес: соединения со старым больше не нужны
	if s.cachedClient != nil {
		s.cachedClient.CloseIdleConnections()
	}
	s.cachedClient = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
	s.cachedTarget = target

	return s.cachedClient
}

// ProbeAPI checks if Mihomo API is reachable and authenticated by requesting /version.
func (s *MihomoService) ProbeAPI(secret string) (reachable bool, authenticated bool) {
	info, err := s.UnixController()
	if err != nil {
		return false, false
	}
	req, err := http.NewRequest(http.MethodGet, mihomoControllerURL+"/version", nil)
	if err != nil {
		return false, false
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{
		Transport: s.httpClient(info.Target).Transport,
		Timeout:   3 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, true
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return true, false
	}
	return true, false
}

// ValidateMihomoConfig inspects the Mihomo config.yaml and returns a PreflightResult
// with blocking errors and non-blocking warnings. On read or parse failure, returns
// a non-nil error (the handler converts this to a safe valid:true response).
func (s *MihomoService) ValidateMihomoConfig() (PreflightResult, error) {
	configPath := filepath.Join(s.ConfigDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join(s.ConfigDir, "config.yml")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("failed to read mihomo config: %w", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return PreflightResult{}, fmt.Errorf("failed to parse mihomo config: %w", err)
	}

	var errors []PreflightIssue
	var warnings []PreflightIssue

	// Check external-controller-unix or external-controller
	unixVal, hasUnix := cfg["external-controller-unix"]
	tcpVal, hasTCP := cfg["external-controller"]

	var unixStr, tcpStr string
	if hasUnix {
		if s, ok := unixVal.(string); ok {
			unixStr = strings.TrimSpace(s)
		}
	}
	if hasTCP {
		if s, ok := tcpVal.(string); ok {
			tcpStr = strings.TrimSpace(s)
		}
	}

	if unixStr != "" {
		// Valid unix socket configured
	} else if tcpStr != "" {
		// Valid TCP configured, but check if insecure (listening on 0.0.0.0 or :port)
		if strings.HasPrefix(tcpStr, "0.0.0.0:") || strings.HasPrefix(tcpStr, ":") || tcpStr == "0.0.0.0" {
			warnings = append(warnings, PreflightIssue{
				Code:    "insecure_external_controller",
				Message: "external-controller listens on 0.0.0.0; recommend migrating to Unix domain socket",
			})
		}
	} else {
		// Свой профиль без API — норма: ядро запускается, недоступны только
		// графики, группы и квоты. Предупреждение не останавливает запуск.
		warnings = append(warnings, PreflightIssue{
			Code:    "no_external_controller",
			Message: "external-controller is not configured; Clash API will be unavailable",
		})
	}

	// WARNING: proxy-groups absence means no routing groups defined.
	if _, ok := cfg["proxy-groups"]; !ok {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_proxy_groups",
			Message: "no proxy-groups defined in config",
		})
	}

	// WARNING: rules absence means no routing rules.
	if _, ok := cfg["rules"]; !ok {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_rules",
			Message: "no rules defined in config",
		})
	}

	// WARNING: no proxies AND no proxy-providers.
	_, hasProxies := cfg["proxies"]
	_, hasProxyProviders := cfg["proxy-providers"]
	if !hasProxies && !hasProxyProviders {
		warnings = append(warnings, PreflightIssue{
			Code:    "no_proxies_or_providers",
			Message: "neither proxies nor proxy-providers are defined in config",
		})
	}

	return PreflightResult{
		Valid:    len(errors) == 0,
		Errors:   errors,
		Warnings: warnings,
	}, nil
}

// DNSQuery performs an interactive DNS resolution query via Mihomo Clash API /dns/query.
func (s *MihomoService) DNSQuery(ctx context.Context, name string, qtype string) ([]byte, error) {
	info, err := s.UnixController()
	if err != nil {
		return nil, err
	}
	reqURL := mihomoControllerURL + "/dns/query"

	if qtype == "" {
		qtype = "A"
	}
	reqURL = fmt.Sprintf("%s?name=%s&type=%s", reqURL, url.QueryEscape(name), url.QueryEscape(qtype))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}

	client := s.httpClient(info.Target)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("mihomo dns query returned status %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// FlushFakeIPCache flushes the Fake-IP cache via Mihomo Clash API /cache/fakeip/flush.
func (s *MihomoService) FlushFakeIPCache(ctx context.Context) error {
	info, err := s.UnixController()
	if err != nil {
		return err
	}
	reqURL := mihomoControllerURL + "/cache/fakeip/flush"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return err
	}
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}

	client := s.httpClient(info.Target)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to flush fake-ip cache (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// HasController сообщает, настроен ли Unix-контроллер Mihomo для Clash API панели.
func (s *MihomoService) HasController() bool {
	_, err := s.UnixController()
	return err == nil
}

// ReloadConfig reloads Mihomo configuration at runtime via PUT /configs?force=true.
func (s *MihomoService) ReloadConfig(configPath string) error {
	info, err := s.UnixController()
	if err != nil {
		return err
	}
	reqURL := mihomoControllerURL + "/configs?force=true"

	payload := map[string]string{"path": configPath}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal reload payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}

	client := s.httpClient(info.Target)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reload mihomo config (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// clashErrBodyMax — сколько символов тела ответа Clash API попадает в ошибку
// методов управления: в теле бывают имена узлов, длинный хвост в журналах не нужен.
const clashErrBodyMax = 64

// ClashStatusError — Clash API ответил кодом, который метод не считает успехом.
type ClashStatusError struct {
	Status int
	Body   string
}

func (e *ClashStatusError) Error() string {
	return fmt.Sprintf("mihomo clash api: status %d: %s", e.Status, e.Body)
}

// newClashStatusError обрезает тело до clashErrBodyMax символов.
func newClashStatusError(status int, body []byte) error {
	text := strings.ToValidUTF8(strings.TrimSpace(string(body)), "")
	if r := []rune(text); len(r) > clashErrBodyMax {
		text = string(r[:clashErrBodyMax])
	}
	return &ClashStatusError{Status: status, Body: text}
}

// clashDo выполняет запрос к Clash API через Unix-сокет с секретом профиля.
// path уже содержит экранированные сегменты. body (если не
// nil) — готовый JSON. Возвращает код ответа и тело.
func (s *MihomoService) clashDo(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	info, err := s.UnixController()
	if err != nil {
		return 0, nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, mihomoControllerURL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}
	resp, err := s.httpClient(info.Target).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, providerBodyMax))
	return resp.StatusCode, data, err
}

// UpdateProvider обновляет один провайдер точечно: PUT /providers/{тип}/{имя}.
// providerType — "proxies" или "rules". Ответ 204 — успех (Mihomo перечитал
// источник), прочие коды — ошибка с кодом ответа. Сверка числа узлов или правил
// остаётся за вызывающим (ProviderCount): 204 не говорит, что набор не пуст.
func (s *MihomoService) UpdateProvider(ctx context.Context, providerType, name string) error {
	if providerType != "proxies" && providerType != "rules" {
		return fmt.Errorf("mihomo providers: неизвестный тип провайдера %q", providerType)
	}
	status, body, err := s.clashDo(ctx, http.MethodPut, "/providers/"+providerType+"/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return newClashStatusError(status, body)
	}
	return nil
}

// ClashDelay — запись истории задержек, которую хранит ядро Mihomo.
type ClashDelay struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

// ClashProxyState — состояние узла или группы в профиле Mihomo.
type ClashProxyState struct {
	Type    string       `json:"type"`
	Now     string       `json:"now"`
	History []ClashDelay `json:"history"`
}

// ProxiesState читает состояние всех прокси вместо ProxyNow на группу: группы и
// узлы профиля — GET /proxies, узлы провайдеров — GET /providers/proxies (в
// /proxies Mihomo их не перечисляет). Снимок даёт «Сейчас» авто-групп и историю
// задержек ядра странице «Прокси»; имя из /proxies главнее одноимённого узла
// провайдера.
func (s *MihomoService) ProxiesState(ctx context.Context) (map[string]ClashProxyState, error) {
	status, body, err := s.clashDo(ctx, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, newClashStatusError(status, body)
	}
	var doc struct {
		Proxies map[string]ClashProxyState `json:"proxies"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("mihomo clash api: разбор ответа: %w", err)
	}
	if doc.Proxies == nil {
		doc.Proxies = map[string]ClashProxyState{}
	}
	status, body, err = s.clashDo(ctx, http.MethodGet, "/providers/proxies", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, newClashStatusError(status, body)
	}
	var pdoc struct {
		Providers map[string]struct {
			Proxies []struct {
				Name string `json:"name"`
				ClashProxyState
			} `json:"proxies"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(body, &pdoc); err != nil {
		return nil, fmt.Errorf("mihomo clash api: разбор ответа провайдеров: %w", err)
	}
	for _, p := range pdoc.Providers {
		for _, px := range p.Proxies {
			if _, ok := doc.Proxies[px.Name]; px.Name != "" && !ok {
				doc.Proxies[px.Name] = px.ClashProxyState
			}
		}
	}
	return doc.Proxies, nil
}

// ProxyNow возвращает выбранного участника группы: GET /proxies/{группа}, поле now.
func (s *MihomoService) ProxyNow(ctx context.Context, group string) (string, error) {
	status, body, err := s.clashDo(ctx, http.MethodGet, "/proxies/"+url.PathEscape(group), nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", newClashStatusError(status, body)
	}
	var doc struct {
		Now string `json:"now"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("mihomo clash api: разбор ответа: %w", err)
	}
	return doc.Now, nil
}

// SelectProxy выбирает участника группы: PUT /proxies/{группа} {"name": ...}.
// Успех — 204.
func (s *MihomoService) SelectProxy(ctx context.Context, group, name string) error {
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return fmt.Errorf("mihomo clash api: тело запроса: %w", err)
	}
	status, body, err := s.clashDo(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), payload)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return newClashStatusError(status, body)
	}
	return nil
}

// ProxyDelay измеряет задержку узла или группы: GET /proxies/{имя}/delay с
// адресом проверки и таймаутом в миллисекундах. Узел провайдера Mihomo в
// /proxies не ищет (404): тогда замер идёт через провайдер,
// GET /providers/proxies/{провайдер}/{имя}/healthcheck. Узел не ответил — ошибка
// с кодом ответа Mihomo.
func (s *MihomoService) ProxyDelay(ctx context.Context, name, testURL string, timeout time.Duration) (int, error) {
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	delay, err := s.delayAt(ctx, "/proxies/"+url.PathEscape(name)+"/delay?"+q.Encode())
	var st *ClashStatusError
	if !errors.As(err, &st) || st.Status != http.StatusNotFound {
		return delay, err
	}
	provider, perr := s.providerOf(ctx, name)
	if perr != nil || provider == "" {
		return delay, err
	}
	return s.delayAt(ctx, "/providers/proxies/"+url.PathEscape(provider)+"/"+url.PathEscape(name)+"/healthcheck?"+q.Encode())
}

// ProviderProxyDelay измеряет задержку узла известного провайдера:
// GET /providers/proxies/{провайдер}/{имя}/healthcheck. Имя узла уникально
// только внутри провайдера, поэтому узлы источников панели меряются через свой
// провайдер, без поиска провайдера по имени (providerOf).
func (s *MihomoService) ProviderProxyDelay(ctx context.Context, provider, name, testURL string, timeout time.Duration) (int, error) {
	q := url.Values{}
	q.Set("url", testURL)
	q.Set("timeout", fmt.Sprintf("%d", timeout.Milliseconds()))
	return s.delayAt(ctx, "/providers/proxies/"+url.PathEscape(provider)+"/"+url.PathEscape(name)+"/healthcheck?"+q.Encode())
}

// providerOfTTL — сколько карта узлов провайдеров считается свежей: пачка
// пинга обходится одним чтением /providers/proxies, а новый набор провайдера
// подхватывается промахом или по сроку.
const providerOfTTL = 30 * time.Second

// providerOf возвращает провайдер, в котором Mihomo держит узел name; пусто —
// узла нет ни в одном провайдере или имя есть у нескольких провайдеров (какой
// узел имелся в виду, по имени не понять). Промах или устаревшая карта
// перечитывают GET /providers/proxies.
func (s *MihomoService) providerOf(ctx context.Context, name string) (string, error) {
	s.provMu.Lock()
	defer s.provMu.Unlock()
	if p, ok := s.provByName[name]; ok && time.Since(s.provAt) < providerOfTTL {
		return p, nil
	}
	status, body, err := s.clashDo(ctx, http.MethodGet, "/providers/proxies", nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", newClashStatusError(status, body)
	}
	var doc struct {
		Providers map[string]struct {
			VehicleType string `json:"vehicleType"`
			Proxies     []struct {
				Name string `json:"name"`
			} `json:"proxies"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("mihomo clash api: разбор ответа провайдеров: %w", err)
	}
	m := map[string]string{}
	for pn, p := range doc.Providers {
		// Compatible — служебные провайдеры групп: их участники есть в /proxies.
		if p.VehicleType == "Compatible" {
			continue
		}
		for _, px := range p.Proxies {
			if px.Name == "" {
				continue
			}
			// Имя у нескольких провайдеров неоднозначно: пустое значение, а не
			// первый провайдер случайного обхода карты.
			if prev, dup := m[px.Name]; dup && prev != pn {
				m[px.Name] = ""
			} else if !dup {
				m[px.Name] = pn
			}
		}
	}
	s.provByName, s.provAt = m, time.Now()
	return m[name], nil
}

// delayAt выполняет запрос замера задержки по готовому пути и читает delay.
func (s *MihomoService) delayAt(ctx context.Context, path string) (int, error) {
	status, body, err := s.clashDo(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, newClashStatusError(status, body)
	}
	var doc struct {
		Delay int `json:"delay"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0, fmt.Errorf("mihomo clash api: разбор ответа: %w", err)
	}
	return doc.Delay, nil
}

// ErrProviderEmpty — Mihomo ответил 204: набор провайдера пуст.
var ErrProviderEmpty = errors.New("mihomo: провайдер пуст")

const (
	// providerErrBodyMax — сколько байт тела ошибки Clash API попадает в сообщение.
	providerErrBodyMax = 300
	// providerBodyMax — предел разбираемого ответа (список узлов бывает большим).
	providerBodyMax = 16 << 20
)

// providerGet выполняет GET к Clash API и возвращает код ответа и тело.
func (s *MihomoService) providerGet(ctx context.Context, info ControllerInfo, reqURL string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, nil, err
	}
	if info.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+info.Secret)
	}
	resp, err := s.httpClient(info.Target).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, providerBodyMax))
	return resp.StatusCode, body, err
}

// providerStatusError — ошибка по коду ответа; тело обрезается до 300 байт.
func providerStatusError(status int, body []byte) error {
	if len(body) > providerErrBodyMax {
		body = body[:providerErrBodyMax]
	}
	return fmt.Errorf("mihomo providers: status %d: %s", status, strings.ToValidUTF8(strings.TrimSpace(string(body)), ""))
}

// ProviderCount возвращает число узлов (providerType "proxies") или правил
// ("rules") провайдера по данным Clash API. Ответ 204 — ErrProviderEmpty: набор
// пуст, и это ошибка применения, а не ноль. Имя провайдера экранируется.
func (s *MihomoService) ProviderCount(ctx context.Context, providerType, name string) (int, error) {
	if providerType != "proxies" && providerType != "rules" {
		return 0, fmt.Errorf("mihomo providers: неизвестный тип провайдера %q", providerType)
	}
	info, err := s.UnixController()
	if err != nil {
		return 0, err
	}
	status, body, err := s.providerGet(ctx, info, mihomoControllerURL+"/providers/"+providerType+"/"+url.PathEscape(name))
	if err != nil {
		return 0, err
	}
	if providerType == "rules" && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		// Точечного GET у rule-провайдера в Clash API может не быть: читаем список.
		return s.ruleCountFromList(ctx, info, name)
	}
	switch status {
	case http.StatusOK:
	case http.StatusNoContent:
		return 0, ErrProviderEmpty
	default:
		return 0, providerStatusError(status, body)
	}
	if providerType == "proxies" {
		var doc struct {
			Proxies []json.RawMessage `json:"proxies"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return 0, fmt.Errorf("mihomo providers: разбор ответа: %w", err)
		}
		return len(doc.Proxies), nil
	}
	var doc struct {
		RuleCount *int `json:"ruleCount"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0, fmt.Errorf("mihomo providers: разбор ответа: %w", err)
	}
	if doc.RuleCount == nil {
		return 0, errors.New("mihomo providers: в ответе нет ruleCount")
	}
	return *doc.RuleCount, nil
}

// ruleCountFromList читает ruleCount провайдера из GET /providers/rules.
func (s *MihomoService) ruleCountFromList(ctx context.Context, info ControllerInfo, name string) (int, error) {
	status, body, err := s.providerGet(ctx, info, mihomoControllerURL+"/providers/rules")
	if err != nil {
		return 0, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNoContent:
		return 0, ErrProviderEmpty
	default:
		return 0, providerStatusError(status, body)
	}
	var doc struct {
		Providers map[string]struct {
			RuleCount int `json:"ruleCount"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0, fmt.Errorf("mihomo providers: разбор ответа: %w", err)
	}
	p, ok := doc.Providers[name]
	if !ok {
		return 0, fmt.Errorf("mihomo providers: провайдер правил %q не найден", name)
	}
	return p.RuleCount, nil
}
