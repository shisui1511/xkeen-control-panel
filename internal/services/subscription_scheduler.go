package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var ErrMihomoAPINotConfigured = errors.New("Mihomo API URL is not configured")

// ErrMihomoNotRunning — контроллер Mihomo не принимает соединения (ядро
// остановлено). Перезагружать провайдер некому: Mihomo сам скачает подписку
// при следующем запуске.
var ErrMihomoNotRunning = errors.New("Mihomo is not running")

// MihomoAPIStatusError описывает неуспешный HTTP-статус ответа Clash API,
// позволяя обработчикам различать 404 (неизвестный провайдер), 401 и прочие
// ошибки вместо неразличимого текста.
type MihomoAPIStatusError struct {
	StatusCode int
}

func (e *MihomoAPIStatusError) Error() string {
	return fmt.Sprintf("API returned status %d", e.StatusCode)
}

// SetKernelApplier подключает общий исполнитель «применить к ядру»: он решает,
// перезапускать ли целевое ядро после изменения его фрагментов.
func (s *SubscriptionService) SetKernelApplier(a *KernelApplier) {
	s.applier = a
}

// restartXkeenIfRunning применяет изменённые фрагменты перезапуском XKeen, но
// только активного и запущенного целевого ядра: остановленное пользователем
// ядро обновление или правка подписки не запускает, а изменение, касающееся
// только другого ядра, активное ядро не перезапускает. Решение принимает
// KernelApplier по списку kernels (xray, mihomo); без него ничего не
// перезапускается.
func (s *SubscriptionService) restartXkeenIfRunning(subID, reason string, kernels ...string) {
	cleanID := utils.SanitizeLogInput(subID)
	if s.applier == nil {
		log.Printf("subscription %s: no kernel applier, skip restart after %s", cleanID, utils.SanitizeLogInput(reason))
		return
	}
	res := s.applier.Apply(kernels...)
	if res.Outcome == ApplyRestartFailed {
		log.Printf("subscription %s: %s: outcome=%s kernel=%s: %s", cleanID, reason, res.Outcome, res.Kernel, utils.SanitizeLogInput(res.Error))
		return
	}
	log.Printf("subscription %s: %s: outcome=%s kernel=%s", cleanID, reason, res.Outcome, res.Kernel)
}

// addKernelTarget добавляет ядро в набор целей рестарта без повторов.
func addKernelTarget(targets []string, kernel string) []string {
	if slices.Contains(targets, kernel) {
		return targets
	}
	return append(targets, kernel)
}

func (s *SubscriptionService) SetKernelService(svc KernelStatusProvider) {
	s.kernelSvc = svc
}

func (s *SubscriptionService) SetMihomoService(svc *MihomoService) {
	s.mihomoSvc = svc
}

func (s *SubscriptionService) SetMihomoAPI(apiURL, secret string) {
	s.mihomoAPIURL = apiURL
	s.mihomoSecret = secret
}

// SetMihomoSecretResolver задаёт fallback-резолвер секрета Clash API,
// который вызывается, когда секрет не задан в конфиге панели (типовой
// сценарий: секрет живёт только в config.yaml Mihomo). Вызывается один раз
// при старте, до запуска фоновых горутин.
func (s *SubscriptionService) SetMihomoSecretResolver(fn func() string) {
	s.mihomoSecretResolver = fn
}

func (s *SubscriptionService) Refresh(id string) error {
	safeID := filepath.Base(id)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")

	// Prevent concurrent refreshes for the same ID
	if _, loaded := s.ongoing.LoadOrStore(safeID, struct{}{}); loaded {
		return fmt.Errorf("refresh already in progress for this subscription")
	}
	defer s.ongoing.Delete(safeID)

	subCopy, ok := func() (Subscription, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		sub := s.GetLocked(safeID)
		if sub == nil {
			return Subscription{}, false
		}
		return sub.Clone(), true
	}()
	if !ok {
		return fmt.Errorf("subscription not found")
	}

	if !subCopy.EnableXray && !subCopy.EnableMihomo {
		return fmt.Errorf("subscription is not enabled for any kernel")
	}

	var refreshErr error
	xrayChanged := false
	xraySuccess := false
	// metadataFetched — панель сама сходила к провайдеру и получила свежие
	// заголовки. Для Mihomo-only подписок это неверно: тело качает Mihomo
	// через loopback-адаптер, он же сохраняет метаданные (PersistHeaderMetadata).
	metadataFetched := false

	// Тело подписки нужно только Xray-пути — его парсит панель. Для Mihomo
	// подписку скачивает сам Mihomo по команде reload, поэтому качать её здесь
	// ещё раз значит удваивать обращения к провайдеру и HWID-хиты на один клик.
	if subCopy.EnableXray {
		body, headers, err := s.downloadWithUA(context.Background(), subCopy.URL, &subCopy, subscriptionUserAgentXray)
		if err != nil {
			s.mu.Lock()
			if live := s.GetLocked(safeID); live != nil {
				live.LastError = err.Error()
				_ = s.save()
			}
			s.mu.Unlock()
			return err
		}
		metadataFetched = true
		subCopy.LastUpdate = time.Now()
		if !isHTMLResponse(body, headers.Get("Content-Type")) {
			applySubscriptionHeaders(headers, &subCopy)
		}

		if err := s.refreshXray(&subCopy, body, headers); err != nil {
			refreshErr = err
		} else {
			xrayChanged = subCopy.LastChanged
			xraySuccess = true
		}
	}

	if subCopy.EnableMihomo {
		providerName := subCopy.GetProviderName()
		activeKernel := ""
		if s.kernelSvc != nil {
			activeKernel = s.kernelSvc.GetActiveKernel()
		}
		log.Printf("[Subscriptions] Mihomo reload triggered for provider %s (active kernel: %s)", providerName, activeKernel)
		err := s.TriggerMihomoProviderReload(providerName)
		if err != nil {
			log.Printf("[Subscriptions] Mihomo reload failed: %v", err)
		}
		// Остановленный Mihomo — не ошибка подписки, если узлы уже получены
		// для Xray: провайдер подтянется при запуске ядра.
		if subCopy.EnableXray && errors.Is(err, ErrMihomoNotRunning) {
			err = nil
		}
		if err != nil {
			if !subCopy.EnableXray || refreshErr == nil {
				refreshErr = err
			}
			// Mihomo не поднял подписку (не запущен / API недоступен), а UI всё
			// равно ждёт свежие срок действия и трафик. Единственный случай,
			// когда Mihomo-only подписку качает панель.
			if !subCopy.EnableXray && s.fetchHeadersOnly(&subCopy) {
				metadataFetched = true
				subCopy.LastUpdate = time.Now()
			}
		}
	}

	subCopy.LastChanged = xrayChanged

	// Persist last_error and successfully parsed fields so frontend can show error state
	s.mu.Lock()
	defer s.mu.Unlock()
	if live := s.GetLocked(safeID); live != nil {
		if refreshErr != nil {
			live.LastError = refreshErr.Error()
		} else {
			live.LastError = ""
		}

		// Метаданные заголовков перезаписываем только если панель сама ходила к
		// провайдеру. Иначе их уже сохранил loopback-адаптер, и затирать их
		// пустыми значениями из subCopy нельзя.
		if metadataFetched {
			live.LastUpdate = subCopy.LastUpdate
			live.Upload = subCopy.Upload
			live.Download = subCopy.Download
			live.Total = subCopy.Total
			live.Expire = subCopy.Expire
			live.ProfileTitle = subCopy.ProfileTitle
			live.ProfileUpdateHours = subCopy.ProfileUpdateHours
			live.SupportURL = subCopy.SupportURL
			live.ProfileWebPageURL = subCopy.ProfileWebPageURL
			live.ProviderType = subCopy.ProviderType

			// Временное имя провайдера (из ID) заменяется на бренд из profile-title.
			s.maybeRenameProviderLocked(live)
		}

		// Update Xray state if its step succeeded
		if xraySuccess {
			live.LastHash = subCopy.LastHash
			live.LastSkipped = subCopy.LastSkipped
			live.DeviceRejected = subCopy.DeviceRejected
			live.LastWarning = subCopy.LastWarning
			if !subCopy.EnableMihomo || live.DetectedFormat == "" {
				live.DetectedFormat = subCopy.DetectedFormat
			}
		}

		// Update shared/derived fields based on which kernel succeeded.
		if xraySuccess {
			live.Nodes = subCopy.Nodes
			live.Announcement = subCopy.Announcement
			if !subCopy.EnableMihomo || live.LastCount == 0 {
				live.LastCount = subCopy.LastCount
			}
		}

		live.LastChanged = xraySuccess && xrayChanged

		_ = s.save()
	}

	if refreshErr == ErrMihomoAPINotConfigured {
		return nil
	}
	return refreshErr
}

func (s *SubscriptionService) refreshXray(sub *Subscription, body []byte, headers http.Header) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in parser: %v", r)
			log.Printf("[Subscriptions] PANIC recovered: %v", r)
		}
	}()

	outbounds, skipReasons, err := parseSubscriptionBody(body, headers.Get("Content-Type"), sub)
	if err != nil {
		return err
	}

	s.mu.Lock()

	// Re-get sub in case it was modified
	live := s.GetLocked(sub.ID)
	if live == nil {
		s.mu.Unlock()
		return fmt.Errorf("subscription not found")
	}

	// Apply filters
	outbounds = s.applyFilters(outbounds, live)

	// Провайдер, отклонивший устройство, отдаёт только узлы-заглушки. Считаем до
	// writeFragment: он мутирует теги outbounds.
	working, stubs := countXrayWorkingAndStubs(outbounds)

	fragmentPath := s.getFragmentPath(live)

	// Все узлы — заглушки, а раньше во фрагменте были рабочие: не затираем их и
	// не перезапускаем ядро (как при ошибке скачивания), только помечаем отказ.
	if working == 0 && stubs > 0 && s.fragmentOutboundCount(fragmentPath) > 0 {
		sub.Nodes = make([]SubscriptionNode, len(live.Nodes))
		for i := range live.Nodes {
			sub.Nodes[i] = live.Nodes[i].Clone()
		}
		sub.Announcement = parseAnnouncement(body, headers)
		sub.LastHash = live.LastHash
		sub.LastChanged = false
		sub.LastCount = live.LastCount
		sub.LastSkipped += stubs
		sub.DeviceRejected = true
		sub.LastWarning = live.LastWarning
		sub.LastUpdate = time.Now()

		log.Printf("[Subscriptions] Refresh Xray ID: %s: provider returned only stub nodes (%d), keeping previous outbounds", sub.ID, stubs)
		report := &ParseReport{
			ParsedCount:  sub.LastCount,
			SkippedCount: sub.LastSkipped,
			Skipped:      skipReasons,
			Timestamp:    sub.LastUpdate,
		}
		s.saveDebugFiles(sub.ID, body, headers, report)
		s.mu.Unlock()
		return nil
	}

	// Generate fragment file
	nodes, err := s.writeFragment(fragmentPath, outbounds, live)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	// Выбор пользователя переживает refresh: узел ищется по тегу, затем по
	// адресу и порту. Пропавший узел заменяется первым рабочим с предупреждением.
	sub.LastWarning = ""
	if live.SelectedTag != "" {
		tag, lost := s.resolveSelectionLocked(live, nodes)
		live.SelectedTag = tag
		live.SelectedServer = ""
		if tag == "" {
			// Рабочих узлов не осталось: выбирать нечего.
			live.IsDefault = false
		}
		for i := range nodes {
			nodes[i].Active = tag != "" && nodes[i].Tag == tag
			if nodes[i].Active {
				live.SelectedServer = nodes[i].Server
			}
		}
		if lost {
			if tag == "" {
				// Рабочих узлов не осталось — не путать с заменой на первый
				// рабочий узел: текст предупреждения должен различать оба
				// исхода (IN-01 из код-ревью фазы 133).
				sub.LastWarning = warningSelectedNodeGone
				log.Printf("[Subscriptions] Refresh Xray ID: %s: selected node disappeared, no working nodes remain", sub.ID)
			} else {
				sub.LastWarning = warningSelectedNodeLost
				log.Printf("[Subscriptions] Refresh Xray ID: %s: selected node disappeared, selected %q instead", sub.ID, tag)
			}
		}
	}

	sub.Nodes = nodes
	sub.Announcement = parseAnnouncement(body, headers)
	sub.DeviceRejected = working == 0 && stubs > 0
	sub.LastSkipped += stubs
	sub.LastCount -= stubs
	if sub.LastCount < 0 {
		sub.LastCount = 0
	}

	// В режиме "auto" — создать routing-фрагмент с balancer и правилом для !CN.
	if live.RoutingMode == "auto" {
		tags := make([]string, 0, len(nodes))
		for _, n := range nodes {
			if allowedXrayProtocols[n.Protocol] && !n.Stub {
				tags = append(tags, n.Tag)
			}
		}
		routingPath := s.getRoutingFragmentPath(live)
		if err := s.writeRoutingFragment(routingPath, live, tags); err != nil {
			log.Printf("routing fragment write error for %s: %v", live.ID, err)
		}
	} else {
		// Если режим изменился с auto → manual, удаляем старый routing-фрагмент.
		os.Remove(s.getRoutingFragmentPath(live))
	}

	sub.LastUpdate = time.Now()

	// Сравниваем хэши фрагмента конфигурации — restart только при реальных изменениях.
	fragmentBytes, err := os.ReadFile(fragmentPath)
	var newHash string
	if err == nil {
		h := sha256.Sum256(fragmentBytes)
		newHash = fmt.Sprintf("%x", h[:])
	}
	oldHash := live.LastHash
	sub.LastHash = newHash

	needRestart := false
	if newHash != oldHash {
		sub.LastChanged = true
		// Фрагмент без рабочих узлов (одни заглушки) поведение ядра не меняет.
		needRestart = !(working == 0 && stubs > 0)
	} else {
		sub.LastChanged = false
	}

	// Копии выбранных узлов под стабильными тегами перестраиваются после каждой
	// перезаписи фрагмента: иначе они устаревают вместе с адресом узла.
	selChanged, selErr := s.writeSelectionFilesLocked()
	if selErr != nil {
		log.Printf("[Subscriptions] Refresh Xray ID: %s: failed to rebuild selection files: %v", sub.ID, selErr)
	}
	needRestart = needRestart || selChanged

	// Логирование UA-ответа
	log.Printf("[Subscriptions] Refresh Xray ID: %s, Format: %s, Size: %d bytes, Proxies: %d, Skipped: %d",
		sub.ID, sub.DetectedFormat, len(body), sub.LastCount, sub.LastSkipped)

	// Сохранение файлов отладки
	report := &ParseReport{
		ParsedCount:  sub.LastCount,
		SkippedCount: sub.LastSkipped,
		Skipped:      skipReasons,
		Timestamp:    sub.LastUpdate,
	}
	s.saveDebugFiles(sub.ID, body, headers, report)

	s.mu.Unlock()

	if needRestart {
		s.restartXkeenIfRunning(sub.ID, "xray fragment update", "xray")
	}

	return nil
}

// resolveSelectionLocked находит узел выбора пользователя среди свежих узлов
// подписки: сначала по тегу, затем по адресу и порту. Учитываются только
// рабочие узлы (разрешённый протокол, не заглушка). Пустой выбор даёт пустой
// тег. Узел не найден — возвращается первый рабочий и lost=true; если рабочих
// нет, тег пуст и lost=true. mu должен быть захвачен вызывающим.
func (s *SubscriptionService) resolveSelectionLocked(live *Subscription, nodes []SubscriptionNode) (tag string, lost bool) {
	if live == nil || live.SelectedTag == "" {
		return "", false
	}
	usable := func(n *SubscriptionNode) bool {
		return allowedXrayProtocols[n.Protocol] && !n.Stub
	}
	for i := range nodes {
		if usable(&nodes[i]) && nodes[i].Tag == live.SelectedTag {
			return nodes[i].Tag, false
		}
	}
	if live.SelectedServer != "" {
		for i := range nodes {
			if usable(&nodes[i]) && nodes[i].Server == live.SelectedServer {
				return nodes[i].Tag, false
			}
		}
	}
	for i := range nodes {
		if usable(&nodes[i]) {
			return nodes[i].Tag, true
		}
	}
	return "", true
}

// fragmentOutboundCount возвращает число outbounds во фрагменте подписки
// или 0, если файла нет или он не разбирается.
func (s *SubscriptionService) fragmentOutboundCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var wrapper struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return 0
	}
	return len(wrapper.Outbounds)
}

// getFragmentPath — фрагмент outbounds подписки. Суффикс tail в имени велит Xray
// дописывать его outbounds в конец итогового списка, чтобы фрагмент не
// перехватывал дефолтный outbound у файлов XKeen (см. subscription_selection.go).
func (s *SubscriptionService) getFragmentPath(sub *Subscription) string {
	path := filepath.Join(s.configDir, fmt.Sprintf("04_outbounds.%s.tail.json", subscriptionSafeID(sub)))
	if path == s.selectionTailPath() {
		// ID подписки zz_xcp_selected совпал бы с файлом выбранных узлов.
		path = filepath.Join(s.configDir, fmt.Sprintf("04_outbounds.%s_sub.tail.json", subscriptionSafeID(sub)))
	}
	return path
}

// legacyFragmentPath — прежнее имя фрагмента (без tail), которое подхватывается
// миграцией при старте панели. Пустая строка, если имя совпало бы с файлом
// дефолта: его переименовывать и удалять как фрагмент нельзя.
func (s *SubscriptionService) legacyFragmentPath(sub *Subscription) string {
	path := filepath.Join(s.configDir, fmt.Sprintf("04_outbounds.%s.json", subscriptionSafeID(sub)))
	if path == s.selectionDefaultPath() {
		return ""
	}
	return path
}

func (s *SubscriptionService) getRoutingFragmentPath(sub *Subscription) string {
	return filepath.Join(s.configDir, fmt.Sprintf("05_routing.%s.json", subscriptionSafeID(sub)))
}

func (s *SubscriptionService) writeRoutingFragment(path string, sub *Subscription, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	type Rule struct {
		Type        string   `json:"type"`
		Domain      []string `json:"domain"`
		OutboundTag string   `json:"outboundTag,omitempty"`
		BalancerTag string   `json:"balancerTag,omitempty"`
	}
	type Balancer struct {
		Tag      string   `json:"tag"`
		Selector []string `json:"selector"`
	}
	type Routing struct {
		Balancers []Balancer `json:"balancers,omitempty"`
		Rules     []Rule     `json:"rules"`
	}
	type Fragment struct {
		Routing Routing `json:"routing"`
	}

	var frag Fragment
	domains := []string{"geosite:geolocation-!cn", "geoip:!cn"}

	if sub.TagPrefix != "" {
		balancerTag := sub.ID + "-balancer"
		frag = Fragment{
			Routing: Routing{
				Balancers: []Balancer{{
					Tag:      balancerTag,
					Selector: []string{sub.TagPrefix + "-"},
				}},
				Rules: []Rule{{
					Type:        "field",
					Domain:      domains,
					BalancerTag: balancerTag,
				}},
			},
		}
	} else {
		frag = Fragment{
			Routing: Routing{
				Rules: []Rule{{
					Type:        "field",
					Domain:      domains,
					OutboundTag: tags[0],
				}},
			},
		}
	}

	data, err := json.MarshalIndent(frag, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return utils.AtomicWriteFile(path, data, 0600)
}

// effectiveRefreshIntervalHours возвращает интервал обновления подписки в
// часах, который панель фактически использует: интервал провайдера, если
// use_provider_interval включён и профиль сообщил положительный
// profile_update_hours, иначе — интервал, заданный пользователем в форме.
// Единая точка истины для isRefreshDue и computeNextUpdate (D-12) — до этой
// правки isRefreshDue и фронтенд считали интервал независимо друг от друга,
// что и породило баг B13 (шапка игнорировала use_provider_interval).
func effectiveRefreshIntervalHours(sub *Subscription) int {
	if sub.UseProviderInterval && sub.ProfileUpdateHours > 0 {
		return sub.ProfileUpdateHours
	}
	return sub.Interval
}

// isRefreshDue returns true if a subscription needs to be refreshed.
func (s *SubscriptionService) isRefreshDue(sub *Subscription, now time.Time) bool {
	if !sub.EnableXray {
		return false // Mihomo-only subs are refreshed natively by Mihomo itself (D-07)
	}
	interval := effectiveRefreshIntervalHours(sub)
	if !sub.Enabled || interval <= 0 {
		return false
	}
	if val, ok := s.retries.Load(sub.ID); ok {
		rs := val.(*retryState)
		if now.Before(rs.nextRetry) {
			return false
		}
	}
	return now.Sub(sub.LastUpdate) >= time.Duration(interval)*time.Hour
}

// computeNextUpdate возвращает время следующего обновления Xray-подписки той
// же формулой интервала, что isRefreshDue (D-12) — единственная точка
// расчёта, отдаваемая в API как next_update. nil для Mihomo-only (D-13),
// выключенных подписок и подписок без положительного интервала: у них нет
// собственного таймера обновления Xray. Если запланирован повторный запрос
// backoff'ом (recordFailure) позже, чем обычный расчёт по интервалу —
// возвращается именно он, чтобы isRefreshDue(sub, now) ==
// !computeNextUpdate(sub, now).After(now) выполнялось при любом состоянии.
func (s *SubscriptionService) computeNextUpdate(sub *Subscription, now time.Time) *time.Time {
	if !sub.EnableXray || !sub.Enabled {
		return nil
	}
	interval := effectiveRefreshIntervalHours(sub)
	if interval <= 0 {
		return nil
	}

	base := now
	if !sub.LastUpdate.IsZero() {
		base = sub.LastUpdate.Add(time.Duration(interval) * time.Hour)
	}

	if val, ok := s.retries.Load(sub.ID); ok {
		rs := val.(*retryState)
		if rs.nextRetry.After(base) {
			base = rs.nextRetry
		}
	}

	result := base
	return &result
}

// recordFailure increments the failure counter and schedules the next retry.
//
// Stores a fresh *retryState on every call instead of mutating the previously
// stored pointer in place (Rule 1 — pre-existing data race, T-133-01/edge
// concurrency): computeNextUpdate/isRefreshDue read the *retryState returned
// by sync.Map.Load concurrently with recordFailure on the same subscription
// ID (e.g. a manual refresh failing while List()/Get() render next_update).
// Mutating shared struct fields under those reads is a genuine race even
// though sync.Map itself is safe for concurrent Load/Store — the struct
// pointed to by a previously Loaded value is not. Treating each stored
// *retryState as immutable once published removes the race entirely.
func (s *SubscriptionService) recordFailure(id string) {
	failCount := 1
	if val, ok := s.retries.Load(id); ok {
		failCount = val.(*retryState).failCount + 1
	}
	delay := backoffMax
	if failCount <= 6 { // 5m * 2^5 = 160m < 4h (backoffMax)
		delay = backoffBase * (1 << uint(failCount-1))
		if delay > backoffMax {
			delay = backoffMax
		}
	}
	s.retries.Store(id, &retryState{failCount: failCount, nextRetry: time.Now().Add(delay)})
}

// clearFailure resets the backoff state on a successful refresh.
func (s *SubscriptionService) clearFailure(id string) {
	s.retries.Delete(id)
}

// checkAndRefreshDue scans all subscriptions and launches a goroutine for
func (s *SubscriptionService) checkAndRefreshDue(now time.Time) {
	subs := s.List()
	for _, sub := range subs {
		if s.isRefreshDue(&sub, now) {
			go func(id string) {
				if err := s.Refresh(id); err != nil {
					if !strings.Contains(err.Error(), "already in progress") {
						s.recordFailure(id)
						fc := 0
						if val, ok := s.retries.Load(id); ok {
							fc = val.(*retryState).failCount
						}
						log.Printf("subscription %s: auto-refresh failed (attempt %d): %v", id, fc, err)
					}
				} else {
					s.clearFailure(id)
				}
			}(sub.ID)
		}
	}
}

// RunScheduler starts a background loop that refreshes overdue subscriptions
func (s *SubscriptionService) RunScheduler(ctx context.Context, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.checkAndRefreshDue(t)
		}
	}
}

func (s *SubscriptionService) LockMihomo() {
	s.mihomoMu.Lock()
}

func (s *SubscriptionService) UnlockMihomo() {
	s.mihomoMu.Unlock()
}

func (s *SubscriptionService) TriggerMihomoProviderReload(providerName string) error {
	var client *http.Client
	var reqURL string

	if s.mihomoSvc != nil {
		info, err := s.mihomoSvc.ParseControllerConfig()
		if err == nil && info.Type == "unix" && info.Target != "" {
			client = s.mihomoSvc.GetHTTPClient()
			reqURL = fmt.Sprintf("http://localhost/providers/proxies/%s", url.PathEscape(providerName))
		} else if err == nil && info.Type == "tcp" && info.Target != "" {
			client = s.mihomoSvc.GetHTTPClient()
			t := info.Target
			if !strings.HasPrefix(t, "http://") && !strings.HasPrefix(t, "https://") {
				t = "http://" + t
			}
			reqURL = fmt.Sprintf("%s/providers/proxies/%s", strings.TrimRight(t, "/"), url.PathEscape(providerName))
		}
	}

	if client == nil {
		if s.mihomoAPIURL == "" {
			return ErrMihomoAPINotConfigured
		}
		client = s.localHTTPClient
		reqURL = fmt.Sprintf("%s/providers/proxies/%s", s.mihomoAPIURL, url.PathEscape(providerName))
	}

	// PathEscape — защита в глубину: имя валидируется на уровне handler,
	// но экранирование гарантирует, что спецсимволы не изменят путь/query
	// исходящего запроса.
	req, err := http.NewRequest(http.MethodPut, reqURL, nil)
	if err != nil {
		return fmt.Errorf("request init failed: %w", err)
	}
	secret := s.mihomoSecret
	if secret == "" && s.mihomoSecretResolver != nil {
		secret = s.mihomoSecretResolver()
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return fmt.Errorf("%w: %v", ErrMihomoNotRunning, err)
		}
		return fmt.Errorf("API PUT failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return &MihomoAPIStatusError{StatusCode: resp.StatusCode}
	}
	return nil
}

// SetActiveNode делает узел подписки дефолтным outbound Xray (SUBS-05): копия
// узла под стабильным тегом xcp-<id> пишется в собственный файл дефолта, который
// Xray ставит первым в итоговом списке. Фрагмент подписки и файлы XKeen не
// меняются. Дефолт глобально один: прежний дефолт других подписок сбрасывается.
// Доступно только при routing_mode = "manual".
func (s *SubscriptionService) SetActiveNode(subscriptionID, nodeTag string) error {
	s.mu.Lock()

	sub := s.GetLocked(subscriptionID)
	if sub == nil {
		s.mu.Unlock()
		return fmt.Errorf("subscription not found")
	}
	if !sub.EnableXray {
		s.mu.Unlock()
		return fmt.Errorf("active node selection is only supported for Xray subscriptions")
	}
	if sub.RoutingMode == "auto" {
		s.mu.Unlock()
		return fmt.Errorf("cannot set active node in auto routing mode (balancer is managing selection)")
	}
	if !sub.Enabled {
		s.mu.Unlock()
		return fmt.Errorf("subscription is disabled")
	}

	nodeIdx := -1
	for i := range sub.Nodes {
		if sub.Nodes[i].Tag == nodeTag {
			nodeIdx = i
			break
		}
	}
	if nodeIdx < 0 {
		s.mu.Unlock()
		return fmt.Errorf("node %q: %w", nodeTag, ErrSelectionNodeNotFound)
	}
	if sub.Nodes[nodeIdx].Stub {
		s.mu.Unlock()
		return fmt.Errorf("node %q: %w", nodeTag, ErrStubNodeSelection)
	}
	if !allowedXrayProtocols[sub.Nodes[nodeIdx].Protocol] {
		// Протокол вроде hysteria2/tuic не пишется во фрагмент Xray
		// (writeFragment), поэтому readFragmentOutboundLocked ниже вернул бы
		// generic ErrSelectionNodeNotFound — сообщаем настоящую причину (WR-02).
		s.mu.Unlock()
		return fmt.Errorf("node %q: %w", nodeTag, ErrProtocolNotSupportedByXray)
	}
	if _, err := s.readFragmentOutboundLocked(sub, nodeTag); err != nil {
		s.mu.Unlock()
		return err
	}

	// Снимок состояния для отката, если запись файла дефолта не удалась.
	snapshot := make([]Subscription, len(s.subscriptions))
	for i := range s.subscriptions {
		snapshot[i] = s.subscriptions[i].Clone()
	}

	for i := range s.subscriptions {
		s.subscriptions[i].IsDefault = false
	}
	sub.SelectedTag = nodeTag
	sub.SelectedServer = sub.Nodes[nodeIdx].Server
	sub.IsDefault = true
	for i := range sub.Nodes {
		sub.Nodes[i].Active = sub.Nodes[i].Tag == nodeTag
	}

	changed, err := s.writeSelectionFilesLocked()
	if err != nil {
		copy(s.subscriptions, snapshot)
		s.mu.Unlock()
		return err
	}
	_ = s.save()
	subID := sub.ID
	s.mu.Unlock()

	if changed {
		s.restartXkeenIfRunning(subID, "active node switch", "xray")
	}
	return nil
}

// ClearActiveNode снимает дефолтный статус выбранного узла подписки: файл
// дефолта перестраивается, и дефолтным снова становится первый outbound файлов
// XKeen (direct). Стабильный тег xcp-<id> перестаёт быть дефолтом, но не
// удаляется: выбор (SelectedTag) остаётся, правила роутинга пользователя со
// ссылкой на тег не ломают проверку конфига Xray. Для подписки, которая не
// дефолтная, ничего не меняется.
func (s *SubscriptionService) ClearActiveNode(subscriptionID string) error {
	s.mu.Lock()

	sub := s.GetLocked(subscriptionID)
	if sub == nil {
		s.mu.Unlock()
		return fmt.Errorf("subscription not found")
	}
	if !sub.IsDefault {
		s.mu.Unlock()
		return nil
	}

	sub.IsDefault = false
	changed, err := s.writeSelectionFilesLocked()
	if err != nil {
		sub.IsDefault = true
		s.mu.Unlock()
		return err
	}
	_ = s.save()
	subID := sub.ID
	s.mu.Unlock()

	if changed {
		s.restartXkeenIfRunning(subID, "active node cleared", "xray")
	}
	return nil
}

// warningSelectedNodeLost — код last_warning: выбранный пользователем узел
// пропал из подписки и заменён первым рабочим узлом.
const warningSelectedNodeLost = "selected_node_lost"

// warningSelectedNodeGone — код last_warning: выбранный пользователем узел
// пропал из подписки, а рабочих узлов (не заглушка, разрешённый Xray
// протокол), чтобы его заменить, не осталось (IN-01 из код-ревью фазы 133).
const warningSelectedNodeGone = "selected_node_gone"
