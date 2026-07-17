package services

import (
	"crypto/rand"
	"encoding/json"
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

// loadOrGenerateHWID читает UUID устройства из файла xcp_hwid.txt или
// генерирует новый UUID v4 и сохраняет его для следующих запусков.
// Используется как x-hwid header для провайдеров с HWID Device Limit.
func loadOrGenerateHWID(dataDir string) string {
	dir := filepath.Join(dataDir, "data")
	path := filepath.Join(dir, "xcp_hwid.txt")
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); len(id) == 36 {
			return id
		}
	}
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		return ""
	}
	u[6] = (u[6] & 0x0f) | 0x40 // version 4
	u[8] = (u[8] & 0x3f) | 0x80 // variant bits
	id := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(path, []byte(id), 0600)
	return id
}

func (s *SubscriptionService) subPath(filename string) string {
	dir := filepath.Join(s.dataDir, "subscriptions")
	_ = os.MkdirAll(dir, 0755)

	// Sanitize filename to prevent path traversal (CWE-22)
	filename = filepath.Base(filename)
	clean := filepath.Clean(filename)
	if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_\-\.]+$`, clean); !matched {
		return filepath.Join(dir, "invalid_id")
	}
	if strings.Contains(clean, "..") || strings.Contains(clean, "/") || strings.Contains(clean, "\\") {
		return filepath.Join(dir, "invalid_id")
	}
	return filepath.Join(dir, clean)
}

func (s *SubscriptionService) SetHTTPClient(client *http.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.httpClient = client
}

func (s *SubscriptionService) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.subPath("subscriptions.json")
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &s.subscriptions)
	}

	// Импортируем подписки из config.yaml Mihomo (для существующих провайдеров на роутере)
	migrated2 := s.migrateFromMihomoConfig()

	needSave := migrated2
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == "" {
			s.subscriptions[i].ID = fmt.Sprintf("sub_%d_%d", time.Now().Unix(), i)
			needSave = true
		}
		if s.subscriptions[i].ProviderName == "" {
			s.subscriptions[i].ProviderName = s.subscriptions[i].GetProviderName()
			needSave = true
		}
	}
	if needSave {
		indentData, err := json.MarshalIndent(s.subscriptions, "", "  ")
		if err == nil {
			utils.AtomicWriteFile(path, indentData, 0600)
		}
	}
}

func (s *SubscriptionService) save() error {
	// Note: mu must be held by caller or we use a separate locked version
	path := s.subPath("subscriptions.json")
	data, err := json.MarshalIndent(s.subscriptions, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(path, data, 0600)
}

func (s *SubscriptionService) populateMihomoIntegrated(subs []Subscription) {
	configDir := s.mihomoConfigDir
	if configDir == "" {
		configDir = "/opt/etc/mihomo"
	}
	configPath := filepath.Join(configDir, "config.yaml")

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	lines := strings.Split(string(rawConfig), "\n")
	start, end, indent := findTopLevelSection(lines, "proxy-providers")
	if start == -1 {
		return
	}

	// Extract provider names
	providers := extractProviderBlocks(lines, start, end, indent)
	activeProviders := make(map[string]bool)
	for _, p := range providers {
		activeProviders[strings.ToLower(p.ID)] = true
	}

	for i := range subs {
		providerName := subs[i].GetProviderName()
		if activeProviders[providerName] {
			subs[i].MihomoIntegrated = true
		} else {
			subs[i].MihomoIntegrated = false
		}
	}
}

func (s *SubscriptionService) List() []Subscription {
	s.mu.RLock()
	res := make([]Subscription, len(s.subscriptions))
	for i := range s.subscriptions {
		res[i] = s.subscriptions[i].Clone()
		res[i].ProxyCount = s.getProxyCount(&res[i])
	}
	s.mu.RUnlock()
	s.populateMihomoIntegrated(res)
	return res
}

func (s *SubscriptionService) Get(id string) *Subscription {
	var cloned *Subscription
	s.mu.RLock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			c := s.subscriptions[i].Clone()
			c.ProxyCount = s.getProxyCount(&c)
			cloned = &c
			break
		}
	}
	s.mu.RUnlock()

	if cloned != nil {
		slice := []Subscription{*cloned}
		s.populateMihomoIntegrated(slice)
		cloned = &slice[0]
	}
	return cloned
}

func (s *SubscriptionService) GetHWID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hwid
}

func (s *SubscriptionService) getProxyCount(sub *Subscription) int {
	if sub.EnableMihomo {
		// Для Mihomo используем кэшированный счётчик из последнего refresh.
		return sub.LastCount
	}
	path := s.getFragmentPath(sub)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var outbounds []Outbound
	if err := json.Unmarshal(data, &outbounds); err != nil {
		var wrapper struct {
			Outbounds []Outbound `json:"outbounds"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 == nil {
			outbounds = wrapper.Outbounds
		} else {
			return 0
		}
	}
	return len(outbounds)
}

func (s *SubscriptionService) Add(sub *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sub.ID == "" {
		sub.ID = fmt.Sprintf("sub_%d", time.Now().Unix())
	} else {
		// Санитизируем ID — только [a-z0-9_-] допустимы в имени файла.
		sub.ID = strings.ToLower(sub.ID)
		sub.ID = invalidIDCharsRe.ReplaceAllString(sub.ID, "_")
	}

	sub.ProviderName = sub.GetProviderName()

	if sub.EnableMihomo {
		configDir := s.mihomoConfigDir
		if configDir == "" {
			configDir = "/opt/etc/mihomo"
		}
		configPath := filepath.Join(configDir, "config.yaml")

		s.mihomoMu.Lock()
		if rawConfig, err := os.ReadFile(configPath); err == nil {
			providerBlock := s.generateMihomoProxyProviderBlockLocked(sub, s.panelPort, s.panelHTTPS, s.loopbackPort)
			providerName := sub.ProviderName
			newConfig := ReplaceMihomoProxyProvider(string(rawConfig), providerName, providerBlock)
			for _, group := range sub.MihomoGroups {
				newConfig = UpdateMihomoGroupProviders(newConfig, group, providerName, false)
			}
			if string(rawConfig) != newConfig {
				if err := utils.AtomicWriteFile(configPath, []byte(newConfig), 0600); err != nil {
					log.Printf("[Subscriptions] failed to update config.yaml for provider %s: %v", providerName, err)
				}
			}
		}
		s.mihomoMu.Unlock()
	}

	s.subscriptions = append(s.subscriptions, *sub)
	return s.save()
}

func (s *SubscriptionService) Update(id string, sub *Subscription) error {
	safeID := filepath.Base(id)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")

	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == safeID {
			// Partial update: preserve ID and all runtime-fetched data.
			// Only overwrite user-editable config fields from the form.
			existing := &s.subscriptions[i]

			oldGroups := existing.MihomoGroups

			// Clean up old Mihomo provider if the name or URL is changing
			configDir := s.mihomoConfigDir
			if configDir == "" {
				configDir = "/opt/etc/mihomo"
			}
			configPath := filepath.Join(configDir, "config.yaml")

			oldProviderName := existing.GetProviderName()
			newProviderName := GetMihomoProviderName(existing.ProfileTitle, sub.Name, sub.URL, existing.ID)

			if oldProviderName != newProviderName && existing.EnableMihomo {
				s.mihomoMu.Lock()
				if rawConfig, err := os.ReadFile(configPath); err == nil {
					newConfig := ReplaceMihomoProxyProvider(string(rawConfig), oldProviderName, "")
					for _, group := range existing.MihomoGroups {
						newConfig = UpdateMihomoGroupProviders(newConfig, group, oldProviderName, true)
					}
					_ = utils.AtomicWriteFile(configPath, []byte(newConfig), 0600)
				}
				s.mihomoMu.Unlock()

				providersDir := filepath.Join(configDir, "proxy_providers")
				providerFilePath := filepath.Join(providersDir, fmt.Sprintf("%s.yaml", oldProviderName))
				if strings.HasPrefix(providerFilePath, providersDir+string(filepath.Separator)) {
					os.Remove(providerFilePath)
				}
			}

			existing.Name = sub.Name
			existing.URL = sub.URL
			existing.TagPrefix = sub.TagPrefix
			existing.Interval = sub.Interval
			existing.Enabled = sub.Enabled
			existing.FilterName = sub.FilterName
			existing.FilterType = sub.FilterType
			// FilterTransport — обновляем только если явно указан (форма может не отправлять поле).
			if sub.FilterTransport != "" {
				existing.FilterTransport = sub.FilterTransport
			}
			existing.UseProviderInterval = sub.UseProviderInterval
			existing.ProviderName = newProviderName

			needRestart := false
			// Clean up Xray if it was enabled and is now disabled
			if existing.EnableXray && !sub.EnableXray {
				os.Remove(s.getFragmentPath(existing))
				os.Remove(s.getRoutingFragmentPath(existing))
				existing.LastHash = ""
				needRestart = true
			}

			// Clean up Mihomo if it was enabled and is now disabled
			if existing.EnableMihomo && !sub.EnableMihomo {
				s.mihomoMu.Lock()
				rawConfig, err := os.ReadFile(configPath)
				if err == nil {
					newConfig := ReplaceMihomoProxyProvider(string(rawConfig), oldProviderName, "")
					newConfig = ReplaceMihomoProxyProvider(newConfig, newProviderName, "")
					for _, group := range existing.MihomoGroups {
						newConfig = UpdateMihomoGroupProviders(newConfig, group, oldProviderName, true)
						newConfig = UpdateMihomoGroupProviders(newConfig, group, newProviderName, true)
					}
					newConfig = ReplaceMihomoProxies(newConfig, existing.ProxyNames, nil)
					for _, group := range existing.MihomoGroups {
						newConfig = UpdateMihomoGroupProxies(newConfig, group, nil, existing.ProxyNames)
					}
					_ = utils.AtomicWriteFile(configPath, []byte(newConfig), 0600)
				}
				s.mihomoMu.Unlock()

				// Delete provider file; sanitize id to prevent path traversal (CWE-22).
				providersDir := filepath.Join(configDir, "proxy_providers")
				providerFilePath := filepath.Join(providersDir, fmt.Sprintf("%s.yaml", oldProviderName))
				if strings.HasPrefix(providerFilePath, providersDir+string(filepath.Separator)) {
					os.Remove(providerFilePath)
				}
				providerFilePathNew := filepath.Join(providersDir, fmt.Sprintf("%s.yaml", newProviderName))
				if strings.HasPrefix(providerFilePathNew, providersDir+string(filepath.Separator)) {
					os.Remove(providerFilePathNew)
				}

				// Reset Mihomo specific fields in existing subscription
				existing.ProxyNames = nil
				existing.ManagedYAML = ""
				existing.LastCount = 0
				existing.LastHashMihomo = ""
				needRestart = true
			}

			existing.EnableXray = sub.EnableXray
			existing.EnableMihomo = sub.EnableMihomo

			if sub.RoutingMode != "" {
				existing.RoutingMode = sub.RoutingMode
			}
			if sub.MihomoGroups != nil {
				existing.MihomoGroups = sub.MihomoGroups
			}

			// Если интеграция Mihomo включена (или была только что включена), обновляем/добавляем провайдер и привязываем его к группам
			if existing.EnableMihomo {
				s.mihomoMu.Lock()
				if rawConfig, err := os.ReadFile(configPath); err == nil {
					providerBlock := s.generateMihomoProxyProviderBlockLocked(existing, s.panelPort, s.panelHTTPS, s.loopbackPort)
					newConfig := ReplaceMihomoProxyProvider(string(rawConfig), newProviderName, providerBlock)
					for _, group := range oldGroups {
						newConfig = UpdateMihomoGroupProviders(newConfig, group, oldProviderName, true)
					}
					for _, group := range existing.MihomoGroups {
						newConfig = UpdateMihomoGroupProviders(newConfig, group, newProviderName, false)
					}
					if string(rawConfig) != newConfig {
						if err := utils.AtomicWriteFile(configPath, []byte(newConfig), 0600); err != nil {
							log.Printf("[Subscriptions] failed to update config.yaml for provider %s: %v", newProviderName, err)
						}
					}
				}
				s.mihomoMu.Unlock()
			}

			if err := s.save(); err != nil {
				return err
			}

			if needRestart && s.consoleSvc != nil {
				if _, err := s.consoleSvc.Execute("-restart"); err != nil {
					cleanID := strings.NewReplacer("\n", "", "\r", "").Replace(safeID)
					log.Printf("subscription %s: xkeen -restart after update (disabled integration): %v", cleanID, err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("subscription not found")
}

func (s *SubscriptionService) Delete(id string) error {
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("invalid subscription ID format")
	}
	safeID := filepath.Base(id)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")

	s.mu.Lock()
	defer s.mu.Unlock()
	// Find subscription
	var sub *Subscription
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == safeID {
			sub = &s.subscriptions[i]
			break
		}
	}
	if sub == nil {
		return fmt.Errorf("subscription not found")
	}

	enableXray := sub.EnableXray
	enableMihomo := sub.EnableMihomo

	// Remove from list
	newList := make([]Subscription, 0, len(s.subscriptions)-1)
	for _, s := range s.subscriptions {
		if s.ID != safeID {
			newList = append(newList, s)
		}
	}
	s.subscriptions = newList

	// Delete managed fragment files.
	if enableXray {
		os.Remove(s.getFragmentPath(sub))
		os.Remove(s.getRoutingFragmentPath(sub)) // noop если файла нет
	}
	if enableMihomo {
		configDir := s.mihomoConfigDir
		if configDir == "" {
			configDir = "/opt/etc/mihomo"
		}
		configPath := filepath.Join(configDir, "config.yaml")

		providerName := GetMihomoProviderName(sub.ProfileTitle, sub.Name, sub.URL, sub.ID)

		s.mihomoMu.Lock()
		rawConfig, err := os.ReadFile(configPath)
		if err == nil {
			newConfig := ReplaceMihomoProxyProvider(string(rawConfig), providerName, "")
			for _, group := range sub.MihomoGroups {
				newConfig = UpdateMihomoGroupProviders(newConfig, group, providerName, true)
			}
			// Также почистим старые прокси на всякий случай
			newConfig = ReplaceMihomoProxies(newConfig, sub.ProxyNames, nil)
			for _, group := range sub.MihomoGroups {
				newConfig = UpdateMihomoGroupProxies(newConfig, group, nil, sub.ProxyNames)
			}
			_ = utils.AtomicWriteFile(configPath, []byte(newConfig), 0600)
		}
		s.mihomoMu.Unlock()

		// Удалить файл провайдера; санитизируем путь к файлу провайдера (CWE-22)
		providersDir := filepath.Join(configDir, "proxy_providers")
		providerFilePath := filepath.Join(providersDir, fmt.Sprintf("%s.yaml", providerName))
		if strings.HasPrefix(providerFilePath, providersDir+string(filepath.Separator)) {
			os.Remove(providerFilePath)
		}
	}

	// Delete diagnostic files
	os.Remove(s.subPath("sub_" + safeID + "_raw.txt"))
	os.Remove(s.subPath("sub_" + safeID + "_headers.json"))
	os.Remove(s.subPath("sub_" + safeID + "_parse_report.json"))

	if err := s.save(); err != nil {
		return err
	}

	if (enableXray || enableMihomo) && s.consoleSvc != nil {
		if _, err := s.consoleSvc.Execute("-restart"); err != nil {
			cleanID := strings.NewReplacer("\n", "", "\r", "").Replace(safeID)
			log.Printf("subscription %s: xkeen -restart after delete: %v", cleanID, err)
		}
	}
	return nil
}

func (s *SubscriptionService) GetLocked(id string) *Subscription {
	for i := range s.subscriptions {
		if s.subscriptions[i].ID == id {
			return &s.subscriptions[i]
		}
	}
	return nil
}

func (s *SubscriptionService) saveDebugFiles(id string, body []byte, headers http.Header, report *ParseReport) {
	safeID := filepath.Base(id)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")

	rawPath := s.subPath("sub_" + safeID + "_raw.txt")
	_ = utils.AtomicWriteFile(rawPath, body, 0600)

	hdrMap := make(map[string][]string)
	for k, v := range headers {
		hdrMap[k] = v
	}
	hdrData, err := json.MarshalIndent(hdrMap, "", "  ")
	if err == nil {
		hdrPath := s.subPath("sub_" + safeID + "_headers.json")
		_ = utils.AtomicWriteFile(hdrPath, hdrData, 0600)
	}

	if report != nil {
		repData, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			repPath := s.subPath("sub_" + safeID + "_parse_report.json")
			_ = utils.AtomicWriteFile(repPath, repData, 0600)
		}
	}
}

// GetRaw возвращает сырое тело ответа и заголовки последней загрузки подписки.
func (s *SubscriptionService) GetRaw(id string) (string, map[string][]string, error) {
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return "", nil, fmt.Errorf("invalid subscription ID format")
	}
	safeID := filepath.Base(id)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")

	s.mu.RLock()
	defer s.mu.RUnlock()

	exists := false
	for _, sub := range s.subscriptions {
		if sub.ID == safeID {
			exists = true
			break
		}
	}
	if !exists {
		return "", nil, fmt.Errorf("subscription not found")
	}

	rawPath := s.subPath("sub_" + safeID + "_raw.txt")
	bodyBytes, err := os.ReadFile(rawPath)
	if err != nil {
		return "", nil, fmt.Errorf("raw response not found: %w", err)
	}

	hdrPath := s.subPath("sub_" + safeID + "_headers.json")
	hdrBytes, err := os.ReadFile(hdrPath)
	if err != nil {
		return string(bodyBytes), nil, nil
	}

	var headers map[string][]string
	if err := json.Unmarshal(hdrBytes, &headers); err != nil {
		return string(bodyBytes), nil, nil
	}

	return string(bodyBytes), headers, nil
}

// GetParseReport возвращает отчет о результатах парсинга последней загрузки подписки.
func (s *SubscriptionService) GetParseReport(id string) (*ParseReport, error) {
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return nil, fmt.Errorf("invalid subscription ID format")
	}
	safeID := filepath.Base(id)
	safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")

	s.mu.RLock()
	defer s.mu.RUnlock()

	exists := false
	for _, sub := range s.subscriptions {
		if sub.ID == safeID {
			exists = true
			break
		}
	}
	if !exists {
		return nil, fmt.Errorf("subscription not found")
	}

	repPath := s.subPath("sub_" + safeID + "_parse_report.json")
	repBytes, err := os.ReadFile(repPath)
	if err != nil {
		return nil, fmt.Errorf("parse report not found: %w", err)
	}

	var report ParseReport
	if err := json.Unmarshal(repBytes, &report); err != nil {
		return nil, err
	}

	return &report, nil
}

// CleanOrphanedSubscriptions deletes cached files for subscriptions that are no longer active in the panel,
// but only if those files are older than 7 days, and system time is synchronized (at least 2026-01-01).
// This execution is throttled to run at most once per hour.
func (s *SubscriptionService) CleanOrphanedSubscriptions() {
	if time.Now().Before(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		log.Println("[Cleanup] System time is before 2026-01-01, skipping orphaned subscription cleanup")
		return
	}

	s.mu.Lock()
	if time.Since(s.lastCleanup) < 1*time.Hour {
		s.mu.Unlock()
		return
	}
	s.lastCleanup = time.Now()
	s.mu.Unlock()

	s.mu.RLock()
	activeIDs := make(map[string]bool)
	for _, sub := range s.subscriptions {
		safeID := filepath.Base(sub.ID)
		safeID = invalidIDCharsRe.ReplaceAllString(strings.ToLower(safeID), "_")
		activeIDs[safeID] = true
	}
	s.mu.RUnlock()

	dir := filepath.Join(s.dataDir, "subscriptions")
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("[Cleanup] Failed to read subscriptions directory: %v", err)
		return
	}

	re := regexp.MustCompile(`^sub_([a-z0-9_-]+)_(raw\.txt|headers\.json|parse_report\.json)$`)
	cleanedIDs := make(map[string]bool)

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		matches := re.FindStringSubmatch(name)
		if len(matches) < 2 {
			continue
		}

		safeID := matches[1]
		if activeIDs[safeID] {
			continue
		}

		if cleanedIDs[safeID] {
			continue
		}

		info, err := file.Info()
		if err != nil {
			log.Printf("[Cleanup] Failed to get file info for %s: %v", name, err)
			continue
		}

		if time.Since(info.ModTime()) > 7*24*time.Hour {
			cleanedIDs[safeID] = true
			log.Printf("[Cleanup] Removing orphaned files for subscription safeID: %s", safeID)

			pathsToDelete := []string{
				s.subPath("sub_" + safeID + "_raw.txt"),
				s.subPath("sub_" + safeID + "_headers.json"),
				s.subPath("sub_" + safeID + "_parse_report.json"),
			}
			for _, p := range pathsToDelete {
				if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
					log.Printf("[Cleanup] Failed to remove orphaned subscription file %s: %v", p, err)
				}
			}
		}
	}
}

func (s *SubscriptionService) migrateFromMihomoConfig() bool {
	configDir := s.mihomoConfigDir
	if configDir == "" {
		configDir = "/opt/etc/mihomo"
	}
	configPath := filepath.Join(configDir, "config.yaml")

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	lines := strings.Split(string(rawConfig), "\n")
	start, end, indent := findTopLevelSection(lines, "proxy-providers")
	if start == -1 {
		return false
	}

	blocks := extractProviderBlocks(lines, start, end, indent)
	if len(blocks) == 0 {
		return false
	}

	migrated := false

	existingURLs := make(map[string]int)
	for i := range s.subscriptions {
		urlClean := strings.TrimSpace(strings.ToLower(s.subscriptions[i].URL))
		existingURLs[urlClean] = i
	}

	cleanURL := func(urlStr string) string {
		urlStr = strings.Trim(strings.TrimSpace(urlStr), `'"`)
		if urlStr == "" {
			return ""
		}
		match := regexp.MustCompile(`[?&]url=([^&]+)`).FindStringSubmatch(urlStr)
		if len(match) > 1 {
			if decoded, err := url.QueryUnescape(match[1]); err == nil {
				return strings.TrimSpace(decoded)
			}
			return strings.TrimSpace(match[1])
		}
		return strings.TrimSpace(urlStr)
	}

	for _, block := range blocks {
		var pType, pURL string
		var pInterval int = 24

		for i := block.StartLine + 1; i < block.EndLine; i++ {
			trimmedLeft := strings.TrimLeft(lines[i], " \t")
			lineIndent := len(lines[i]) - len(trimmedLeft)
			if lineIndent != indent+2 {
				continue
			}

			line := strings.TrimSpace(lines[i])
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.HasPrefix(line, "url:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "url:"))
				pURL = strings.Trim(val, `'"`)
			} else if strings.HasPrefix(line, "type:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "type:"))
				pType = strings.Trim(val, `'"`)
			} else if strings.HasPrefix(line, "interval:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "interval:"))
				val = strings.Trim(val, `'"`)
				var sec int
				if _, err := fmt.Sscanf(val, "%d", &sec); err == nil {
					if sec > 720 {
						pInterval = sec / 3600
					} else {
						pInterval = sec
					}
				}
			}
		}

		if pType != "http" || pURL == "" {
			continue
		}

		originalURL := cleanURL(pURL)
		if originalURL == "" {
			continue
		}

		urlLower := strings.ToLower(originalURL)
		if _, exists := existingURLs[urlLower]; exists {
			continue
		}

		newID := fmt.Sprintf("sub_%d_%d", time.Now().Unix(), len(s.subscriptions))

		name := block.ID
		if name == "" {
			name = "Imported Provider"
		}

		newSub := Subscription{
			ID:           newID,
			Name:         name,
			URL:          originalURL,
			TagPrefix:    name,
			Interval:     pInterval,
			Enabled:      true,
			EnableMihomo: true,
			EnableXray:   false,
			LastUpdate:   time.Time{},
		}

		s.subscriptions = append(s.subscriptions, newSub)
		existingURLs[urlLower] = len(s.subscriptions) - 1
		migrated = true
	}

	return migrated
}

// PersistHeaderMetadata сохраняет метаданные подписки, полученные из HTTP-заголовков.
func (s *SubscriptionService) PersistHeaderMetadata(id string, subCopy *Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := s.GetLocked(id)
	if live == nil {
		return fmt.Errorf("subscription not found")
	}

	live.Upload = subCopy.Upload
	live.Download = subCopy.Download
	live.Total = subCopy.Total
	live.Expire = subCopy.Expire
	live.ProfileTitle = subCopy.ProfileTitle
	live.ProfileUpdateHours = subCopy.ProfileUpdateHours
	live.SupportURL = subCopy.SupportURL
	live.ProfileWebPageURL = subCopy.ProfileWebPageURL
	live.ProviderType = subCopy.ProviderType
	live.HwidLocked = subCopy.HwidLocked
	live.LastUpdate = time.Now()

	return s.save()
}
