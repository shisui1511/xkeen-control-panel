package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

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
	ua := s.selectUserAgent(&subCopy)
	body, headers, err := s.downloadWithUA(context.Background(), subCopy.URL, &subCopy, ua)
	if err != nil {
		s.mu.Lock()
		if live := s.GetLocked(safeID); live != nil {
			live.LastError = err.Error()
			_ = s.save()
		}
		s.mu.Unlock()
		return err
	}

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
		s.TriggerMihomoProviderReload(providerName)
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
	defer s.mu.Unlock()

	// Re-get sub in case it was modified
	live := s.GetLocked(sub.ID)
	if live == nil {
		return fmt.Errorf("subscription not found")
	}

	// Apply filters
	outbounds = s.applyFilters(outbounds, live)

	// Generate fragment file
	fragmentPath := s.getFragmentPath(live)
	nodes, err := s.writeFragment(fragmentPath, outbounds, live)
	if err != nil {
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

	if newHash != oldHash {
		sub.LastChanged = true
		if s.consoleSvc != nil {
			if _, err := s.consoleSvc.Execute("-restart"); err != nil {
				log.Printf("subscription %s: xkeen -restart after xray fragment update: %v", sub.ID, err)
			}
		}
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
	delay := backoffBase * (1 << uint(rs.failCount-1))
	if delay > backoffMax {
		delay = backoffMax
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

func (s *SubscriptionService) TriggerMihomoProviderReload(providerName string) {
	if s.mihomoAPIURL == "" {
		return
	}
	url := fmt.Sprintf("%s/providers/proxies/%s", s.mihomoAPIURL, providerName)
	req, err := http.NewRequest(http.MethodPut, url, nil)
	if err != nil {
		log.Printf("mihomo provider reload Request init failed: %v", err)
		return
	}
	if s.mihomoSecret != "" {
		req.Header.Set("Authorization", "Bearer "+s.mihomoSecret)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("mihomo provider reload API PUT failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		log.Printf("mihomo provider reload API returned status %d", resp.StatusCode)
	}
}

// SetActiveNode перемещает ноду с указанным тегом на первую позицию в
// 04_outbounds.{id}.json. XRay читает outbounds по порядку и использует первый
// в качестве активного. Доступно только при routing_mode = "manual".
func (s *SubscriptionService) SetActiveNode(subscriptionID, nodeTag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub := s.GetLocked(subscriptionID)
	if sub == nil {
		return fmt.Errorf("subscription not found")
	}
	if !sub.EnableXray {
		return fmt.Errorf("active node selection is only supported for Xray subscriptions")
	}
	if sub.RoutingMode == "auto" {
		return fmt.Errorf("cannot set active node in auto routing mode (balancer is managing selection)")
	}

	fragmentPath := s.getFragmentPath(sub)
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		return fmt.Errorf("outbounds file not found: %w", err)
	}

	var wrapper struct {
		Outbounds []Outbound `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
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
		return err
	}
	if err := utils.AtomicWriteFile(fragmentPath, newData, 0600); err != nil {
		return err
	}
	_ = s.save()

	// Триггер рестарта через ConsoleService.
	if s.consoleSvc != nil {
		if _, err := s.consoleSvc.Execute("-restart"); err != nil {
			log.Printf("subscription %s: xkeen -restart after active node switch: %v", sub.ID, err)
		}
	}

	return nil
}
