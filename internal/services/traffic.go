package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// ProxyTraffic holds accumulated traffic per proxy
type ProxyTraffic struct {
	ProxyName     string `json:"proxy_name"`
	UploadBytes   int64  `json:"upload_bytes"`
	DownloadBytes int64  `json:"download_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
}

// ClientTraffic holds aggregated traffic for a LAN client IP
type ClientTraffic struct {
	IP          string `json:"ip"`
	Upload      int64  `json:"upload"`
	Download    int64  `json:"download"`
	TotalBytes  int64  `json:"total_bytes"`
	ActiveConns int    `json:"active_connections"`
}

// TrafficPeaks holds peak upload and download rates over calendar periods with timestamps
type TrafficPeaks struct {
	PeakHourUp       int64 `json:"peak_hour_up"`
	PeakHourDown     int64 `json:"peak_hour_down"`
	PeakDayUp        int64 `json:"peak_day_up"`
	PeakDayDown      int64 `json:"peak_day_down"`
	PeakWeekUp       int64 `json:"peak_week_up"`
	PeakWeekDown     int64 `json:"peak_week_down"`
	PeakHourUpTime   int64 `json:"peak_hour_up_time"`
	PeakHourDownTime int64 `json:"peak_hour_down_time"`
	PeakDayUpTime    int64 `json:"peak_day_up_time"`
	PeakDayDownTime  int64 `json:"peak_day_down_time"`
	PeakWeekUpTime   int64 `json:"peak_week_up_time"`
	PeakWeekDownTime int64 `json:"peak_week_down_time"`
	HourStart        int64 `json:"hour_start"`
	DayStart         int64 `json:"day_start"`
	WeekStart        int64 `json:"week_start"`
}

// TrafficStore — содержимое файла статистики трафика: учёт по прокси, пики и
// момент сброса. Лимиты расхода хранит служба лимитов (traffic-limits.json).
type TrafficStore struct {
	ProxyStats map[string]*ProxyTraffic `json:"proxy_stats"`
	Peaks      TrafficPeaks             `json:"peaks"`
	ResetTime  int64                    `json:"reset_time"`
}

// trafficStatsFileName — файл статистики трафика в корне DataDir. С фазы 152 он
// лежит вне каталога traffic/: прежний traffic/traffic.json с квотами уходит в
// архив и не читается, статистика начинается с нуля.
const trafficStatsFileName = "traffic-stats.json"

// errTrafficStatsPath — путь файла статистики не прошёл проверку: запись не идёт.
var errTrafficStatsPath = errors.New("traffic: stats file path rejected")

// saveLockThrottle is the minimum interval between background/periodic saves
// (the reset of statistics uses force=true and bypasses throttling).
const saveLockThrottle = 1 * time.Minute

// maxTrafficFileSize is the rotation threshold for traffic-stats.json.
const maxTrafficFileSize = 5 * 1024 * 1024 // 5 MB

// wsReadDeadline bounds how long streamConnections/streamTraffic will block on
// ReadMessage without receiving anything from Mihomo. Mihomo emits a snapshot
// roughly once per second on both endpoints even when idle, so this is a
// generous margin that only trips for a genuinely stalled connection (STAB-02).
const wsReadDeadline = 30 * time.Second

// kernelAsleepPollInterval bounds sleep duration before rechecking kernel liveness (D-24).
const kernelAsleepPollInterval = 15 * time.Second

// mihomoConnMetadata holds metadata about connection protocol and client
type mihomoConnMetadata struct {
	Network  string `json:"network"`
	SourceIP string `json:"sourceIP"`
	Host     string `json:"host"`
}

// mihomoConn is a single connection entry from the Mihomo /connections stream.
type mihomoConn struct {
	ID       string             `json:"id"`
	Chains   []string           `json:"chains"`
	Upload   int64              `json:"upload"`
	Download int64              `json:"download"`
	Metadata mihomoConnMetadata `json:"metadata"`
}

// ConnDelta — прирост байтов одного соединения Mihomo между снимками /connections:
// Chains — цепочка исходящих соединения как в снимке, Bytes — upload+download.
// Соединения DIRECT и REJECT тоже входят: их отбрасывает получатель.
type ConnDelta struct {
	Chains []string
	Bytes  int64
}

// Состояния потока /connections (StreamState).
const (
	StreamStateOK       = "ok"
	StreamStateDown     = "down"
	StreamStateStopped  = "stopped"
	StreamStateNoSocket = "no_socket"
)

// wsLinkState — состояние потока WS одной метки для журнала и StreamState.
type wsLinkState struct {
	// connected — поток подключён сейчас.
	connected bool
	// ever — поток хоть раз подключался после старта панели.
	ever bool
	// failing — идёт полоса отказов: первая ошибка уже записана в журнал.
	failing bool
}

// TrafficQuotaService — служба потоков трафика Mihomo: статистика по прокси, пики,
// потоки /connections и /traffic для подписчиков и дельты соединений для службы
// лимитов. Группы она не переключает: выбор групп ставит только арбитр маршрутизации.
type TrafficQuotaService struct {
	// path — проверенный путь файла статистики; "" — путь не прошёл проверку.
	path       string
	secret     string
	proxyStats map[string]*ProxyTraffic
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	lastSave   time.Time // time of last successful disk write (protected by mu)

	// connectionTracker maps connection ID -> last seen {upload, download}
	connectionTracker sync.Map

	// fan-out: подписчики получают raw JSON-снимки подключений
	connSubs   map[chan []byte]struct{}
	connSubsMu sync.RWMutex

	peaks             TrafficPeaks
	activeConnsCount  int64
	tcpConnsCount     int64
	udpConnsCount     int64
	topClients        []ClientTraffic
	totalClientsBytes int64 // sum across ALL LAN clients, before topClients is truncated to 5
	trafficSubs       map[chan []byte]struct{}
	trafficSubsMu     sync.RWMutex

	mihomoSvc          *MihomoService
	resetTime          int64
	trackerInitialized bool

	// Kernel liveness check and sleep coordination (WD-05).
	kernelAliveCheck func() bool
	asleep           bool

	// connDeltaHook получает ненулевые дельты всех соединений снимка (D-02);
	// вызывается вне s.mu. Защищено s.mu.
	connDeltaHook func([]ConnDelta)

	// wsLinks — состояние потоков WS по метке ("connections", "traffic").
	wsLinksMu sync.Mutex
	wsLinks   map[string]*wsLinkState
}

func NewTrafficQuotaService(dataDir, secret string, mihomo *MihomoService) *TrafficQuotaService {
	ctx, cancel := context.WithCancel(context.Background())
	svc := &TrafficQuotaService{
		mihomoSvc:   mihomo,
		secret:      secret,
		proxyStats:  make(map[string]*ProxyTraffic),
		ctx:         ctx,
		cancel:      cancel,
		connSubs:    make(map[chan []byte]struct{}),
		trafficSubs: make(map[chan []byte]struct{}),
		wsLinks:     make(map[string]*wsLinkState),
	}
	path := filepath.Join(dataDir, trafficStatsFileName)
	if clean, err := utils.NewPathValidator([]string{dataDir}).ValidateUnder(path, dataDir); err != nil {
		log.Printf("traffic: путь файла статистики не прошёл проверку, статистика не сохраняется")
	} else {
		svc.path = clean
	}
	svc.load()
	return svc
}

// SetConnDeltaHook задаёт получателя дельт соединений снимка /connections (служба
// лимитов). Хук вызывается вне мьютекса службы и не должен блокировать надолго.
func (s *TrafficQuotaService) SetConnDeltaHook(fn func([]ConnDelta)) {
	s.mu.Lock()
	s.connDeltaHook = fn
	s.mu.Unlock()
}

// StreamState сообщает состояние потока /connections: no_socket — у профиля Mihomo
// нет Unix-контроллера (151 D-13), stopped — ядро не запущено, down — поток сейчас
// не подключён, ok — снимки идут.
func (s *TrafficQuotaService) StreamState() string {
	if s.mihomoSvc == nil {
		return StreamStateStopped
	}
	if _, err := s.mihomoSvc.UnixController(); err != nil {
		return StreamStateNoSocket
	}
	if !s.kernelAlive() {
		return StreamStateStopped
	}
	s.wsLinksMu.Lock()
	defer s.wsLinksMu.Unlock()
	if l := s.wsLinks["connections"]; l != nil && l.connected {
		return StreamStateOK
	}
	return StreamStateDown
}

// wsLinkLocked возвращает состояние потока метки. Caller holds s.wsLinksMu.
func (s *TrafficQuotaService) wsLinkLocked(label string) *wsLinkState {
	l := s.wsLinks[label]
	if l == nil {
		l = &wsLinkState{}
		s.wsLinks[label] = l
	}
	return l
}

// streamUp отмечает подключение потока. В журнал — первое подключение после
// старта и восстановление после полосы отказов; обычные переподключения (смена
// профиля, контроллера) — молча.
func (s *TrafficQuotaService) streamUp(label string) {
	s.wsLinksMu.Lock()
	l := s.wsLinkLocked(label)
	first, recovered := !l.ever, l.failing
	l.connected, l.ever, l.failing = true, true, false
	s.wsLinksMu.Unlock()
	switch {
	case recovered:
		log.Printf("TrafficQuota: WS %s восстановлен", label)
	case first:
		log.Printf("TrafficQuota: WS %s подключён", label)
	}
}

// streamDown отмечает, что поток не подключён. err != nil — отказ: в журнал
// пишется только первая ошибка полосы, повторы — молча (паузы задаёт
// wsReconnectLoop). err == nil — штатное завершение (смена профиля, остановка ядра).
func (s *TrafficQuotaService) streamDown(label string, err error) {
	s.wsLinksMu.Lock()
	l := s.wsLinkLocked(label)
	l.connected = false
	logIt := err != nil && !l.failing
	if err != nil {
		l.failing = true
	}
	s.wsLinksMu.Unlock()
	if logIt {
		log.Printf("TrafficQuota: WS %s недоступен: %v — повторы без записи в журнал до восстановления", label, err)
	}
}

// SetKernelAliveCheck configures the predicate used to determine whether the Mihomo
// kernel is actively running (D-22, WD-05). When not set (e.g. in tests), the service
// assumes the kernel is running.
func (s *TrafficQuotaService) SetKernelAliveCheck(fn func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kernelAliveCheck = fn
}

// kernelAlive reports whether the Mihomo kernel is running according to the
// injected kernelAliveCheck predicate. Returns true if no check is installed (D-22).
func (s *TrafficQuotaService) kernelAlive() bool {
	s.mu.RLock()
	fn := s.kernelAliveCheck
	s.mu.RUnlock()
	if fn == nil {
		return true
	}
	return fn()
}

// handleKernelSleep завершает наблюдение и очищает временные счётчики потоков.
func (s *TrafficQuotaService) handleKernelSleep(label string, stopped bool) {
	s.mu.Lock()
	if s.asleep {
		s.mu.Unlock()
		return
	}
	s.asleep = true

	if stopped {
		log.Printf("TrafficQuota: [%s] Mihomo kernel is not running, entering sleep mode", label)
	}

	// Clear connection tracking map so deltas start from zero upon resume.
	s.connectionTracker.Range(func(key, value interface{}) bool {
		s.connectionTracker.Delete(key)
		return true
	})
	s.trackerInitialized = false
	s.activeConnsCount = 0
	s.tcpConnsCount = 0
	s.udpConnsCount = 0

	// Force write state to disk (D-26).
	_ = s.saveLocked(true)

	peaksCopy := s.peaks
	topClientsCopy := make([]ClientTraffic, len(s.topClients))
	copy(topClientsCopy, s.topClients)
	totalClientsBytesCopy := s.totalClientsBytes
	s.mu.Unlock()

	// Broadcast zero traffic snapshot to subscribers (D-25).
	trafficPayload := map[string]interface{}{
		"up":                  int64(0),
		"down":                int64(0),
		"connections":         int64(0),
		"tcp_connections":     int64(0),
		"udp_connections":     int64(0),
		"peaks":               peaksCopy,
		"top_clients":         topClientsCopy,
		"total_clients_bytes": totalClientsBytesCopy,
	}
	if raw, err := json.Marshal(trafficPayload); err == nil {
		s.broadcastTraffic(raw)
	}

	// Broadcast empty connections array to subscribers (D-25).
	connPayload := struct {
		Connections []mihomoConn `json:"connections"`
	}{
		Connections: []mihomoConn{},
	}
	if raw, err := json.Marshal(connPayload); err == nil {
		s.broadcastConnections(raw)
	}
}

// handleKernelWake transitions the service out of asleep state when the kernel is running (D-26).
func (s *TrafficQuotaService) handleKernelWake(label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.asleep {
		return
	}
	s.asleep = false
	log.Printf("TrafficQuota: [%s] Mihomo Unix controller configured, resuming monitoring", label)
}

func (s *TrafficQuotaService) controllerSecret(info ControllerInfo) string {
	if info.Secret != "" {
		return info.Secret
	}
	return s.secret
}

func (s *TrafficQuotaService) dialStream(ctx context.Context, info ControllerInfo, path string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		NetDialContext:   unixControllerDialer(info.Target, nil),
	}
	header := http.Header{}
	if secret := s.controllerSecret(info); secret != "" {
		header.Set("Authorization", "Bearer "+secret)
	}
	conn, resp, err := dialer.DialContext(ctx, "ws://localhost"+path, header)
	if err != nil && resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	return conn, err
}

// Start запускает потоки /connections и /traffic; каждый переподключается сам
// (wsReconnectLoop).
func (s *TrafficQuotaService) Start() {
	s.wg.Add(2)
	go s.connectionsWSLoop()
	go s.trafficWSLoop()
}

func (s *TrafficQuotaService) Stop() {
	s.cancel()
	s.wg.Wait()

	// Последняя запись — без fsync (см. saveLocked): новый процесс панели читает
	// файл из кэша страниц, а ожидание носителя держало выход панели дольше окна
	// init-скрипта.
	s.mu.Lock()
	if err := s.writeStoreLocked(utils.AtomicWriteFileNoSync); err != nil {
		log.Printf("traffic: final save failed: %v", err)
	}
	s.mu.Unlock()
}

// load читает файл статистики. Нет файла, путь не прошёл проверку или содержимое
// повреждено — статистика начинается с нуля.
func (s *TrafficQuotaService) load() {
	var store TrafficStore
	data, err := s.readStore()
	if err == nil {
		err = json.Unmarshal(data, &store)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.resetTime = time.Now().Unix()
		return
	}
	if store.ProxyStats != nil {
		s.proxyStats = store.ProxyStats
	}
	s.peaks = store.Peaks
	s.resetTime = store.ResetTime
	if s.resetTime == 0 {
		s.resetTime = time.Now().Unix()
	}
}

// readStore читает файл статистики по проверенному пути.
func (s *TrafficQuotaService) readStore() ([]byte, error) {
	if s.path == "" {
		return nil, errTrafficStatsPath
	}
	return os.ReadFile(s.path)
}

// saveLocked writes state to disk. Caller MUST hold s.mu (write lock).
// If force is false the write is skipped when a previous write happened
// within saveLockThrottle — suitable for high-frequency periodic saves.
// Pass force=true for the reset of statistics so it is persisted immediately.
//
// Периодическое сохранение (force=false) идёт без fsync: под идущей записью на
// флешку fsync ждёт фиксации журнала ext4 десятки секунд, держа s.mu и цикл
// WebSocket, а остановка панели ждала этот цикл дольше окна init-скрипта. Потеря
// последнего снимка счётчиков при обрыве питания допустима (подмена файла
// остаётся атомарной, utils.AtomicWriteFileNoSync); сброс статистики пишется с fsync.
func (s *TrafficQuotaService) saveLocked(force bool) error {
	if !force && !s.lastSave.IsZero() && time.Since(s.lastSave) < saveLockThrottle {
		return nil
	}
	write := utils.AtomicWriteFile
	if !force {
		write = utils.AtomicWriteFileNoSync
	}
	if err := s.writeStoreLocked(write); err != nil {
		return err
	}
	s.lastSave = time.Now()
	s.rotateIfNeeded()
	return nil
}

// writeStoreLocked сериализует состояние и пишет файл статистики функцией write.
// Caller MUST hold s.mu (write lock).
func (s *TrafficQuotaService) writeStoreLocked(write func(path string, data []byte, perm os.FileMode) error) error {
	if s.path == "" {
		return errTrafficStatsPath
	}
	store := TrafficStore{
		ProxyStats: s.proxyStats,
		Peaks:      s.peaks,
		ResetTime:  s.resetTime,
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return write(s.path, data, 0600)
}

// rotateIfNeeded renames the stats file to a timestamped .bak when it exceeds
// maxTrafficFileSize and writes the current state back at once. Caller MUST hold
// s.mu (write lock).
func (s *TrafficQuotaService) rotateIfNeeded() {
	if s.path == "" {
		return
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Size() < maxTrafficFileSize {
		return
	}
	bakPath := fmt.Sprintf("%s.%s.bak", s.path, time.Now().Format("20060102-150405"))
	if err := os.Rename(s.path, bakPath); err != nil {
		log.Printf("traffic: rotate failed: %v", err)
		return
	}
	log.Printf("traffic: %s exceeded 5 MB, rotated → %s", trafficStatsFileName, bakPath)
	if err := s.writeStoreLocked(utils.AtomicWriteFile); err != nil {
		log.Printf("traffic: rotate write failed: %v", err)
	}
}

// --- Stats ---

func (s *TrafficQuotaService) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	proxyList := make([]ProxyTraffic, 0, len(s.proxyStats))
	var totalUpload, totalDownload int64
	for _, stat := range s.proxyStats {
		proxyList = append(proxyList, *stat)
		totalUpload += stat.UploadBytes
		totalDownload += stat.DownloadBytes
	}

	return map[string]interface{}{
		"proxies":        proxyList,
		"total_upload":   totalUpload,
		"total_download": totalDownload,
		"total":          totalUpload + totalDownload,
		"peaks":          s.peaks,
		"reset_time":     s.resetTime,
	}
}

func (s *TrafficQuotaService) ResetStats() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.proxyStats = make(map[string]*ProxyTraffic)

	now := time.Now()
	s.resetTime = now.Unix()
	s.peaks = TrafficPeaks{
		HourStart: now.Truncate(time.Hour).Unix(),
		DayStart:  time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix(),
	}
	offset := int(now.Weekday() - time.Monday)
	if offset < 0 {
		offset += 7
	}
	s.peaks.WeekStart = time.Date(now.Year(), now.Month(), now.Day()-offset, 0, 0, 0, 0, now.Location()).Unix()

	return s.saveLocked(true)
}

// --- Collector ---

// connectionsWSLoop connects to Mihomo's /connections WebSocket endpoint and
// processes real-time connection snapshots. Reconnects automatically with
// exponential backoff (see wsReconnectLoop) when the stream is interrupted.
func (s *TrafficQuotaService) connectionsWSLoop() {
	s.wsReconnectLoop("connections", s.streamConnections)
}

// wsReconnectLoop повторяет сетевые отказы только у настроенного Unix-контроллера.
func (s *TrafficQuotaService) wsReconnectLoop(label string, streamFn func(context.Context, ControllerInfo) error) {
	defer s.wg.Done()

	if s.mihomoSvc == nil {
		return
	}
	const baseBackoff = 5 * time.Second
	const shortBackoffCap = 60 * time.Second
	const extendedBackoffCap = 5 * time.Minute
	const rapidFailureThreshold = 3 * time.Second
	const extendedAfterFailures = 5

	backoff := baseBackoff
	consecutiveFailures := 0
	for s.ctx.Err() == nil {
		ctx, cancel := s.mihomoSvc.ControllerContext(s.ctx)
		if !s.kernelAlive() {
			s.streamDown(label, nil)
			s.handleKernelSleep(label, true)
			select {
			case <-ctx.Done():
			case <-time.After(kernelAsleepPollInterval):
			}
			cancel()
			backoff, consecutiveFailures = baseBackoff, 0
			continue
		}
		info, err := s.mihomoSvc.UnixController()
		if err != nil {
			s.streamDown(label, nil)
			s.handleKernelSleep(label, false)
			<-ctx.Done()
			cancel()
			backoff, consecutiveFailures = baseBackoff, 0
			continue
		}

		s.handleKernelWake(label)
		start := time.Now()
		err = streamFn(ctx, info)
		if ctx.Err() != nil || !s.mihomoSvc.HasController() {
			s.streamDown(label, nil)
			cancel()
			backoff, consecutiveFailures = baseBackoff, 0
			continue
		}
		elapsed := time.Since(start)

		if elapsed > 30*time.Second {
			// Session was healthy for a while and later dropped — start over.
			backoff = baseBackoff
			consecutiveFailures = 0
		} else if elapsed < rapidFailureThreshold {
			// Near-instant failure (e.g. connection refused) — Mihomo is
			// likely stopped rather than just having a transient hiccup.
			consecutiveFailures++
		}

		backoffCap := shortBackoffCap
		if consecutiveFailures >= extendedAfterFailures {
			backoffCap = extendedBackoffCap
		}

		// Журнал — только первая ошибка полосы отказов и восстановление (streamUp);
		// повторы с той же нарастающей паузой идут молча.
		s.streamDown(label, err)

		select {
		case <-time.After(backoff):
			backoff = min(backoff*2, backoffCap)
		case <-ctx.Done():
			backoff, consecutiveFailures = baseBackoff, 0
		}
		cancel()
	}
}

// streamConnections читает снимки подключений до смены профиля или обрыва.
func (s *TrafficQuotaService) streamConnections(ctx context.Context, info ControllerInfo) error {
	conn, err := s.dialStream(ctx, info, "/connections")
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	s.streamUp("connections")

	for {
		// Mihomo emits a /connections snapshot roughly once per second even when
		// idle. A read deadline well above that cadence detects a socket that
		// accepted the handshake but then went silent (e.g. a wedged Mihomo
		// process), forcing a reconnect through the backoff loop instead of
		// blocking this goroutine on ReadMessage indefinitely (STAB-02).
		_ = conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var payload struct {
			Connections []mihomoConn `json:"connections"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		s.broadcastConnections(raw)
		s.processConnSnapshot(payload.Connections)
	}
}

// broadcastConnections рассылает raw JSON-снимок всем подписчикам WebSocket.
func (s *TrafficQuotaService) broadcastConnections(raw []byte) {
	s.connSubsMu.RLock()
	defer s.connSubsMu.RUnlock()
	for ch := range s.connSubs {
		select {
		case ch <- raw:
		default: // медленный клиент — пропускаем
		}
	}
}

// SubscribeConnections регистрирует канал для получения снимков подключений.
// Возвращает канал и функцию отписки, которую вызывают при закрытии WebSocket.
func (s *TrafficQuotaService) SubscribeConnections() (ch chan []byte, unsub func()) {
	ch = make(chan []byte, 4)
	s.connSubsMu.Lock()
	s.connSubs[ch] = struct{}{}
	s.connSubsMu.Unlock()
	unsub = func() {
		s.connSubsMu.Lock()
		delete(s.connSubs, ch)
		s.connSubsMu.Unlock()
	}
	return
}

// processConnSnapshot computes per-proxy traffic deltas from a Mihomo
// connections snapshot and accumulates them into proxyStats.
func (s *TrafficQuotaService) processConnSnapshot(connections []mihomoConn) {
	s.mu.Lock()

	activeIDs := make(map[string]bool, len(connections))
	isFirstSnapshot := !s.trackerInitialized
	s.trackerInitialized = true

	// Дельты всех соединений снимка, включая DIRECT и REJECT, для службы лимитов:
	// конечный исходящий и привязку к группам решает она (D-02).
	var deltas []ConnDelta
	hook := s.connDeltaHook

	for _, conn := range connections {
		activeIDs[conn.ID] = true

		var deltaUp, deltaDown int64
		if val, ok := s.connectionTracker.Load(conn.ID); ok {
			last := val.(connStats)
			deltaUp = conn.Upload - last.Upload
			deltaDown = conn.Download - last.Download
		} else {
			if !isFirstSnapshot {
				// New connection: treat current bytes as the delta.
				deltaUp = conn.Upload
				deltaDown = conn.Download
			}
		}

		if deltaUp < 0 {
			deltaUp = 0
		}
		if deltaDown < 0 {
			deltaDown = 0
		}
		s.connectionTracker.Store(conn.ID, connStats{Upload: conn.Upload, Download: conn.Download})

		if hook != nil && deltaUp+deltaDown > 0 && len(conn.Chains) > 0 {
			deltas = append(deltas, ConnDelta{Chains: conn.Chains, Bytes: deltaUp + deltaDown})
		}

		// Учёт по прокси (proxyStats) пропускает DIRECT и REJECT, как прежде.
		if len(conn.Chains) == 0 {
			continue
		}
		proxyName := conn.Chains[len(conn.Chains)-1]
		if proxyName == "" || proxyName == "DIRECT" || proxyName == "REJECT" {
			continue
		}

		if deltaUp > 0 || deltaDown > 0 {
			stat, ok := s.proxyStats[proxyName]
			if !ok {
				stat = &ProxyTraffic{ProxyName: proxyName}
				s.proxyStats[proxyName] = stat
			}
			stat.UploadBytes += deltaUp
			stat.DownloadBytes += deltaDown
			stat.TotalBytes = stat.UploadBytes + stat.DownloadBytes
		}
	}

	// Clean up closed connections from the tracker.
	s.connectionTracker.Range(func(key, value interface{}) bool {
		if !activeIDs[key.(string)] {
			s.connectionTracker.Delete(key)
		}
		return true
	})

	var activeCount, tcpCount, udpCount int64
	activeCount = int64(len(connections))
	clientMap := make(map[string]*ClientTraffic)
	for _, conn := range connections {
		net := strings.ToUpper(conn.Metadata.Network)
		if net == "TCP" {
			tcpCount++
		} else if net == "UDP" {
			udpCount++
		}

		srcIP := conn.Metadata.SourceIP
		if srcIP == "" {
			srcIP = "127.0.0.1"
		}
		c, ok := clientMap[srcIP]
		if !ok {
			c = &ClientTraffic{IP: srcIP}
			clientMap[srcIP] = c
		}
		c.Upload += conn.Upload
		c.Download += conn.Download
		c.TotalBytes += (conn.Upload + conn.Download)
		c.ActiveConns++
	}
	s.activeConnsCount = activeCount
	s.tcpConnsCount = tcpCount
	s.udpConnsCount = udpCount

	topClients := make([]ClientTraffic, 0, len(clientMap))
	var totalClientsBytes int64
	for _, c := range clientMap {
		topClients = append(topClients, *c)
		totalClientsBytes += c.TotalBytes
	}
	sort.Slice(topClients, func(i, j int) bool {
		return topClients[i].TotalBytes > topClients[j].TotalBytes
	})
	if len(topClients) > 5 {
		topClients = topClients[:5]
	}
	s.topClients = topClients
	s.totalClientsBytes = totalClientsBytes

	if err := s.saveLocked(false); err != nil {
		log.Printf("TrafficQuota: failed to save stats: %v", err)
	}
	s.mu.Unlock()

	if hook != nil && len(deltas) > 0 {
		hook(deltas)
	}
}

// SubscribeTraffic регистрирует канал для получения снимков трафика.
func (s *TrafficQuotaService) SubscribeTraffic() (ch chan []byte, unsub func()) {
	ch = make(chan []byte, 4)
	s.trafficSubsMu.Lock()
	s.trafficSubs[ch] = struct{}{}
	s.trafficSubsMu.Unlock()
	unsub = func() {
		s.trafficSubsMu.Lock()
		delete(s.trafficSubs, ch)
		s.trafficSubsMu.Unlock()
	}
	return
}

// broadcastTraffic рассылает raw JSON-снимок трафика всем подписчикам WebSocket.
func (s *TrafficQuotaService) broadcastTraffic(raw []byte) {
	s.trafficSubsMu.RLock()
	defer s.trafficSubsMu.RUnlock()
	for ch := range s.trafficSubs {
		select {
		case ch <- raw:
		default: // медленный клиент — пропускаем
		}
	}
}

func (s *TrafficQuotaService) trafficWSLoop() {
	s.wsReconnectLoop("traffic", s.streamTraffic)
}

type mihomoTraffic struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

func (s *TrafficQuotaService) streamTraffic(ctx context.Context, info ControllerInfo) error {
	conn, err := s.dialStream(ctx, info, "/traffic")
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	s.streamUp("traffic")

	for {
		_ = conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var payload mihomoTraffic
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		s.processTrafficSnapshot(payload.Up, payload.Down)
	}
}

func (s *TrafficQuotaService) processTrafficSnapshot(up, down int64) {
	s.mu.Lock()

	now := time.Now()
	nowUnix := now.Unix()
	currentHourStart := now.Truncate(time.Hour).Unix()
	currentDayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()

	offset := int(now.Weekday() - time.Monday)
	if offset < 0 {
		offset += 7
	}
	currentWeekStart := time.Date(now.Year(), now.Month(), now.Day()-offset, 0, 0, 0, 0, now.Location()).Unix()

	// Проверяем календарные интервалы
	if s.peaks.HourStart != currentHourStart {
		s.peaks.PeakHourUp = 0
		s.peaks.PeakHourDown = 0
		s.peaks.PeakHourUpTime = 0
		s.peaks.PeakHourDownTime = 0
		s.peaks.HourStart = currentHourStart
	}
	if s.peaks.DayStart != currentDayStart {
		s.peaks.PeakDayUp = 0
		s.peaks.PeakDayDown = 0
		s.peaks.PeakDayUpTime = 0
		s.peaks.PeakDayDownTime = 0
		s.peaks.DayStart = currentDayStart
	}
	if s.peaks.WeekStart != currentWeekStart {
		s.peaks.PeakWeekUp = 0
		s.peaks.PeakWeekDown = 0
		s.peaks.PeakWeekUpTime = 0
		s.peaks.PeakWeekDownTime = 0
		s.peaks.WeekStart = currentWeekStart
	}

	// Обновляем пики
	if up > s.peaks.PeakHourUp {
		s.peaks.PeakHourUp = up
		s.peaks.PeakHourUpTime = nowUnix
	}
	if down > s.peaks.PeakHourDown {
		s.peaks.PeakHourDown = down
		s.peaks.PeakHourDownTime = nowUnix
	}
	if up > s.peaks.PeakDayUp {
		s.peaks.PeakDayUp = up
		s.peaks.PeakDayUpTime = nowUnix
	}
	if down > s.peaks.PeakDayDown {
		s.peaks.PeakDayDown = down
		s.peaks.PeakDayDownTime = nowUnix
	}
	if up > s.peaks.PeakWeekUp {
		s.peaks.PeakWeekUp = up
		s.peaks.PeakWeekUpTime = nowUnix
	}
	if down > s.peaks.PeakWeekDown {
		s.peaks.PeakWeekDown = down
		s.peaks.PeakWeekDownTime = nowUnix
	}

	conns := s.activeConnsCount
	tcp := s.tcpConnsCount
	udp := s.udpConnsCount
	peaksCopy := s.peaks
	topClientsCopy := make([]ClientTraffic, len(s.topClients))
	copy(topClientsCopy, s.topClients)
	totalClientsBytesCopy := s.totalClientsBytes

	_ = s.saveLocked(false)
	s.mu.Unlock()

	payload := map[string]interface{}{
		"up":                  up,
		"down":                down,
		"connections":         conns,
		"tcp_connections":     tcp,
		"udp_connections":     udp,
		"peaks":               peaksCopy,
		"top_clients":         topClientsCopy,
		"total_clients_bytes": totalClientsBytesCopy,
	}
	raw, err := json.Marshal(payload)
	if err == nil {
		s.broadcastTraffic(raw)
	}
}

// connStats holds last seen bytes for a connection
type connStats struct {
	Upload   int64
	Download int64
}
