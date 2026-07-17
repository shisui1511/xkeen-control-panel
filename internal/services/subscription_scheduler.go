package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var ErrMihomoAPINotConfigured = errors.New("Mihomo API URL is not configured")

// MihomoAPIStatusError описывает неуспешный HTTP-статус ответа Clash API,
// позволяя обработчикам различать 404 (неизвестный провайдер), 401 и прочие
// ошибки вместо неразличимого текста.
type MihomoAPIStatusError struct {
	StatusCode int
}

func (e *MihomoAPIStatusError) Error() string {
	return fmt.Sprintf("API returned status %d", e.StatusCode)
}

// SetConsoleService подключает ConsoleService для триггера xkeen -restart
// после изменения Mihomo config.yaml.
func (s *SubscriptionService) SetConsoleService(svc *ConsoleService) {
	s.consoleSvc = svc
}

func (s *SubscriptionService) SetKernelService(svc KernelStatusProvider) {
	s.kernelSvc = svc
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

	// Download subscription once (outside of lock to avoid blocking other operations)
	body, headers, err := s.downloadWithUA(context.Background(), subCopy.URL, &subCopy, subscriptionUserAgent)
	if err != nil {
		s.mu.Lock()
		if live := s.GetLocked(safeID); live != nil {
			live.LastError = err.Error()
			_ = s.save()
		}
		s.mu.Unlock()
		return err
	}

	subCopy.LastUpdate = time.Now()

	var refreshErr error
	xrayChanged := false
	xraySuccess := false

	if subCopy.EnableXray {
		err := s.refreshXray(&subCopy, body, headers)
		if err != nil {
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
		if err := s.TriggerMihomoProviderReload(providerName); err != nil {
			log.Printf("[Subscriptions] Mihomo reload failed: %v", err)
			if !subCopy.EnableXray || refreshErr == nil {
				refreshErr = err
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

		// Always update HTTP headers metadata as the download itself succeeded.
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

		// Update Xray state if its step succeeded
		if xraySuccess {
			live.LastHash = subCopy.LastHash
			live.LastSkipped = subCopy.LastSkipped
			live.DetectedFormat = subCopy.DetectedFormat
		}

		// Update shared/derived fields based on which kernel succeeded.
		if xraySuccess {
			live.Nodes = subCopy.Nodes
			live.Announcement = subCopy.Announcement
			live.LastCount = subCopy.LastCount
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

	// Generate fragment file
	fragmentPath := s.getFragmentPath(live)
	nodes, err := s.writeFragment(fragmentPath, outbounds, live)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	sub.Nodes = nodes
	sub.Announcement = parseAnnouncement(body, headers)

	// В режиме "auto" — создать routing-фрагмент с balancer и правилом для !CN.
	if live.RoutingMode == "auto" {
		tags := make([]string, 0, len(outbounds))
		for _, ob := range outbounds {
			if allowedXrayProtocols[ob.Protocol] {
				tags = append(tags, ob.Tag)
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
		needRestart = true
	} else {
		sub.LastChanged = false
	}

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

	if needRestart && s.consoleSvc != nil {
		if _, err := s.consoleSvc.Execute("-restart"); err != nil {
			log.Printf("subscription %s: xkeen -restart after xray fragment update: %v", sub.ID, err)
		}
	}

	return nil
}

func (s *SubscriptionService) getFragmentPath(sub *Subscription) string {
	safeID := filepath.Base(sub.ID)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")
	if matched, _ := regexp.MatchString(`^[a-z0-9_-]+$`, safeID); !matched {
		safeID = "safe_id"
	}
	return filepath.Join(s.configDir, fmt.Sprintf("04_outbounds.%s.json", safeID))
}

func (s *SubscriptionService) getRoutingFragmentPath(sub *Subscription) string {
	safeID := filepath.Base(sub.ID)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")
	if matched, _ := regexp.MatchString(`^[a-z0-9_-]+$`, safeID); !matched {
		safeID = "safe_id"
	}
	return filepath.Join(s.configDir, fmt.Sprintf("05_routing.%s.json", safeID))
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

// isRefreshDue returns true if a subscription needs to be refreshed.
func (s *SubscriptionService) isRefreshDue(sub *Subscription, now time.Time) bool {
	if !sub.EnableXray {
		return false // Mihomo-only subs are refreshed natively by Mihomo itself (D-07)
	}
	interval := sub.Interval
	if sub.UseProviderInterval && sub.ProfileUpdateHours > 0 {
		interval = sub.ProfileUpdateHours
	}
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

// recordFailure increments the failure counter and schedules the next retry
func (s *SubscriptionService) recordFailure(id string) {
	rs := &retryState{failCount: 1}
	if val, ok := s.retries.Load(id); ok {
		rs = val.(*retryState)
		rs.failCount++
	}
	delay := backoffMax
	if rs.failCount <= 6 { // 5m * 2^5 = 160m < 4h (backoffMax)
		delay = backoffBase * (1 << uint(rs.failCount-1))
		if delay > backoffMax {
			delay = backoffMax
		}
	}
	rs.nextRetry = time.Now().Add(delay)
	s.retries.Store(id, rs)
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
	if s.mihomoAPIURL == "" {
		return ErrMihomoAPINotConfigured
	}
	// PathEscape — защита в глубину: имя валидируется на уровне handler,
	// но экранирование гарантирует, что спецсимволы не изменят путь/query
	// исходящего запроса.
	reqURL := fmt.Sprintf("%s/providers/proxies/%s", s.mihomoAPIURL, url.PathEscape(providerName))
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
	resp, err := s.localHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("API PUT failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return &MihomoAPIStatusError{StatusCode: resp.StatusCode}
	}
	return nil
}

// SetActiveNode перемещает ноду с указанным тегом на первую позицию в
// 04_outbounds.{id}.json. XRay читает outbounds по порядку и использует первый
// в качестве активного. Доступно только при routing_mode = "manual".
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

	fragmentPath := s.getFragmentPath(sub)
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("outbounds file not found: %w", err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("parse outbounds: %w", err)
	}

	// Находим ноду по тегу
	idx := -1
	for i, ob := range wrapper.Outbounds {
		if ob.Tag == nodeTag {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return fmt.Errorf("node %q not found in subscription outbounds", nodeTag)
	}

	// Перемещаем на первую позицию
	if idx > 0 {
		selected := wrapper.Outbounds[idx]
		newOutbounds := make([]Outbound, 0, len(wrapper.Outbounds))
		newOutbounds = append(newOutbounds, selected)
		newOutbounds = append(newOutbounds, wrapper.Outbounds[:idx]...)
		newOutbounds = append(newOutbounds, wrapper.Outbounds[idx+1:]...)
		wrapper.Outbounds = newOutbounds
	}

	// Обновляем Active-флаг в Nodes
	for i := range sub.Nodes {
		sub.Nodes[i].Active = sub.Nodes[i].Tag == nodeTag
	}

	newData, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if err := utils.AtomicWriteFile(fragmentPath, newData, 0600); err != nil {
		s.mu.Unlock()
		return err
	}
	_ = s.save()
	s.mu.Unlock()

	// Триггер рестарта через ConsoleService.
	if s.consoleSvc != nil {
		if _, err := s.consoleSvc.Execute("-restart"); err != nil {
			log.Printf("subscription %s: xkeen -restart after active node switch: %v", sub.ID, err)
		}
	}

	return nil
}
