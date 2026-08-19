package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// providerFetchBudget — общий тайм-аут на весь цикл ProviderFetch.
const providerFetchBudget = 20 * time.Second

// Метки формата provider payload — записываются в Subscription.DetectedFormat,
// чтобы в UI было видно, отдал ли провайдер готовый Clash YAML или payload
// пришлось конвертировать с потерей полей.
const (
	providerFormatEmpty       = "empty"        // пустое тело ответа
	providerFormatXrayJSON    = "xray-json"    // конвертировано из xray-json
	providerFormatYAMLFull    = "yaml-full"    // полный Clash-конфиг, взята секция proxies:
	providerFormatYAMLProxies = "yaml-proxies" // чистый proxy-provider payload
	providerFormatRaw         = "raw"          // share-links / base64, Mihomo парсит сам
)

var (
	// yamlTopLevelKeyRe матчит top-level ключ YAML документа: строка без
	// ведущих пробелов вида "key:" или "key: value".
	yamlTopLevelKeyRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+):\s*(.*)$`)

	// providerNodeNameLineRe считает записи "- name:" в Clash/Mihomo YAML.
	providerNodeNameLineRe = regexp.MustCompile(`(?m)^\s*-\s*name\s*:\s*`)

	// providerURISchemeRe считает share-link записи (vless://, vmess:// и т.д.).
	// hysteria2 указан раньше hysteria, т.к. является более специфичной
	// альтернативой схемы (не влияет на корректность: "://" сразу после
	// альтернативы всё равно требует полного совпадения схемы, но порядок
	// сохраняет читаемость и намерение).
	providerURISchemeRe = regexp.MustCompile(`(?i)\b(?:vless|vmess|trojan|ss|hy2|hysteria2|hysteria|tuic|socks5?|http-proxy)://`)
)

// ProviderFetch скачивает подписку с upstream-провайдера под User-Agent
// Mihomo (по нему провайдеры отдают готовый Clash YAML), приводит ответ к
// Mihomo-совместимому provider payload — только секция proxies:, — кэширует
// результат на диск и возвращает payload.
//
// Нативный Clash YAML передаётся байт в байт; конвертер из xray-json
// используется только как запасной путь для провайдеров, которые
// Clash-формат не отдают.
func (s *SubscriptionService) ProviderFetch(ctx context.Context, upstreamURL string, sub *Subscription) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, providerFetchBudget)
	defer cancel()

	body, _, err := s.downloadWithUA(ctx, upstreamURL, sub, s.mihomoUserAgent())
	if err != nil {
		return nil, err
	}
	payload, format := providerPayload(body)
	if payload == nil {
		return nil, fmt.Errorf("upstream returned empty/unparseable payload")
	}
	sub.DetectedFormat = format

	nodeCount := countProviderNodes(string(payload))
	if nodeCount > 0 {
		sub.LastCount = nodeCount
		if err := s.cacheProviderPayload(sub, payload); err != nil {
			log.Printf("[Subscriptions] Failed to cache provider payload for %s: %v", utils.SanitizeLogInput(upstreamURL), err)
		}
		if format == providerFormatXrayJSON {
			log.Printf("[Subscriptions] Upstream %s returned xray-json under Mihomo UA; falling back to converter (%d nodes)",
				utils.SanitizeLogInput(upstreamURL), nodeCount)
		}
	} else {
		log.Printf("[Subscriptions] Upstream %s returned empty/unparseable payload, keeping previous cache", utils.SanitizeLogInput(upstreamURL))
	}

	return payload, nil
}

// ProviderFetchWithFallback вызывает ProviderFetch; при сетевой ошибке
// (upstream недоступен) читает последний закэшированный payload с диска.
// Если кэша тоже нет — возвращает исходную ошибку (обработчик отдаёт HTTP 502).
func (s *SubscriptionService) ProviderFetchWithFallback(ctx context.Context, upstreamURL string, sub *Subscription) ([]byte, error) {
	payload, err := s.ProviderFetch(ctx, upstreamURL, sub)
	if err == nil {
		return payload, nil
	}

	cached, readErr := os.ReadFile(s.providerCachePath(sub))
	if readErr != nil || len(bytes.TrimSpace(cached)) == 0 {
		return nil, fmt.Errorf("upstream unavailable and no cached provider file: %w", err)
	}
	return cached, nil
}

// headersOnlyBudget — тайм-аут запроса метаданных подписки.
const headersOnlyBudget = 15 * time.Second

// fetchHeadersOnly забирает у провайдера только метаданные подписки (срок
// действия, трафик, profile-title) и записывает их в sub. Тело ответа
// отбрасывается: узлы в этом сценарии качает Mihomo.
//
// Вызывается лишь как запасной путь, когда Mihomo не смог обновить провайдер
// сам и loopback-адаптеру нечего было сохранить. Возвращает true, если
// метаданные удалось получить.
func (s *SubscriptionService) fetchHeadersOnly(sub *Subscription) bool {
	ctx, cancel := context.WithTimeout(context.Background(), headersOnlyBudget)
	defer cancel()

	resp, err := s.fetchWithUserAgent(ctx, sub.URL, sub, s.mihomoUserAgent())
	if err != nil {
		log.Printf("[Subscriptions] headers-only fetch failed for %s: %v", utils.SanitizeLogInput(sub.ID), err)
		return false
	}
	defer resp.Body.Close()
	// Тело не нужно, но соединение стоит вернуть в пул пригодным к переиспользованию.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxSubscriptionBytes))

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Subscriptions] headers-only fetch for %s returned HTTP %d", utils.SanitizeLogInput(sub.ID), resp.StatusCode)
		return false
	}

	applySubscriptionHeaders(resp.Header, sub)
	return true
}

// providerCachePath возвращает путь к файлу кэша провайдера для подписки.
func (s *SubscriptionService) providerCachePath(sub *Subscription) string {
	configDir := s.mihomoConfigDir
	if configDir == "" {
		configDir = "/opt/etc/mihomo"
	}
	providerName := sub.GetProviderName()
	return filepath.Join(configDir, "proxy_providers", providerName+".yaml")
}

// cacheProviderPayload сохраняет payload провайдера на диск атомарно.
func (s *SubscriptionService) cacheProviderPayload(sub *Subscription, payload []byte) error {
	return utils.AtomicWriteFile(s.providerCachePath(sub), payload, 0600)
}

// yamlTopLevelKeyLine возвращает (key, true), если строка — top-level ключ
// YAML документа (не начинается с пробела/таба, не комментарий, не пустая).
func yamlTopLevelKeyLine(line string) (string, bool) {
	raw := strings.TrimRight(line, "\r\n")
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
		return "", false
	}
	m := yamlTopLevelKeyRe.FindStringSubmatch(raw)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// extractProxiesSection находит top-level ключ "proxies:" в YAML-документе и
// возвращает его секцию целиком (до следующего top-level ключа или конца
// документа). Второй результат — true, если в документе было больше одного
// top-level ключа (т.е. это был полный Clash/Mihomo конфиг, а не чистый
// proxy-provider payload).
func extractProxiesSection(content string) (string, bool) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.SplitAfter(normalized, "\n")

	start := -1
	end := len(lines)
	topLevelKeys := 0

	for idx, line := range lines {
		key, ok := yamlTopLevelKeyLine(line)
		if !ok {
			continue
		}
		topLevelKeys++
		if key == "proxies" && start < 0 {
			start = idx
			continue
		}
		if start >= 0 {
			end = idx
			break
		}
	}

	if start < 0 {
		return "", false
	}

	section := strings.TrimSpace(strings.Join(lines[start:end], "")) + "\n"
	return section, topLevelKeys > 1
}

// providerPayload определяет формат тела ответа подписки и конвертирует его в
// Mihomo-совместимый proxy-provider payload:
//   - xray JSON (массив outbounds/конфигов) → YAML "proxies:" секция.
//   - Полный Clash/Mihomo YAML config (несколько top-level ключей) → только
//     "proxies:" секция.
//   - YAML только с "proxies:" → как есть.
//   - Всё остальное (share-link URI список, base64) → как есть, без изменений
//     (Mihomo умеет парсить эти форматы нативно).
//
// Возвращает (payload, формат-метка): "xray-json", "yaml-full",
// "yaml-proxies" или "raw".
func providerPayload(body []byte) ([]byte, string) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return []byte("proxies: []\n"), providerFormatEmpty
	}

	if trimmed[0] == '[' || trimmed[0] == '{' {
		scratchSub := &Subscription{}
		outbounds, _, err := parseSubscriptionBody(trimmed, "application/json", scratchSub)
		if err == nil && len(outbounds) > 0 {
			scratchSvc := &SubscriptionService{}
			nodes := scratchSvc.outboundsToNodes(outbounds, scratchSub)
			yamlOut, _ := scratchSvc.convertSubscriptionNodesToClashYAML(nodes)
			return []byte(yamlOut), providerFormatXrayJSON
		}
	}

	content := string(trimmed)
	if section, wasFullConfig := extractProxiesSection(content); section != "" {
		if wasFullConfig {
			return []byte(section), providerFormatYAMLFull
		}
		return []byte(section), providerFormatYAMLProxies
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return []byte(content), providerFormatRaw
}

// countProviderNodes оценивает количество нод в provider payload: считает
// "- name:" записи в YAML, либо URI-схемы (vless://, vmess:// и т.д.) в
// текстовом/base64 payload.
func countProviderNodes(payload string) int {
	text := strings.TrimSpace(payload)
	if text == "" {
		return 0
	}

	if matches := providerNodeNameLineRe.FindAllString(text, -1); len(matches) > 0 {
		return len(matches)
	}

	if matches := providerURISchemeRe.FindAllString(text, -1); len(matches) > 0 {
		return len(matches)
	}

	if decoded := tryDecodeBase64Text(text); decoded != "" {
		return len(providerURISchemeRe.FindAllString(decoded, -1))
	}

	return 0
}

// tryDecodeBase64Text пытается декодировать text как base64 (std либо
// URL-safe алфавит); возвращает "" если декодирование не удалось.
func tryDecodeBase64Text(text string) string {
	compact := strings.Join(strings.Fields(text), "")
	if compact == "" {
		return ""
	}
	if decoded, err := base64.StdEncoding.DecodeString(compact); err == nil {
		return string(decoded)
	}
	if decoded, err := base64.URLEncoding.DecodeString(compact); err == nil {
		return string(decoded)
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(compact); err == nil {
		return string(decoded)
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(compact); err == nil {
		return string(decoded)
	}
	return ""
}
