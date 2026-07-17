package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// providerFetchBudget — общий тайм-аут на весь цикл ProviderFetch.
const providerFetchBudget = 20 * time.Second

var (
	// yamlTopLevelKeyRe матчит top-level ключ YAML документа: строка без
	// ведущих пробелов вида "key:" или "key: value".
	yamlTopLevelKeyRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+):\s*(.*)$`)

	// providerNodeNameLineRe считает записи "- name:" в Clash/Mihomo YAML.
	providerNodeNameLineRe = regexp.MustCompile(`(?m)^\s*-\s*name\s*:\s*`)

	// providerURISchemeRe считает share-link записи (vless://, vmess:// и т.д.)
	providerURISchemeRe = regexp.MustCompile(`(?i)\b(?:vless|vmess|trojan|ss|hy2|hysteria2|tuic|socks5?|http-proxy)://`)
)

// ProviderFetch скачивает подписку с upstream-провайдера, используя единый
// User-Agent панели (subscriptionUserAgent), конвертирует ответ в
// Mihomo-совместимый provider payload (только секция proxies:), кэширует
// результат на диск и возвращает payload.
func (s *SubscriptionService) ProviderFetch(ctx context.Context, upstreamURL string, sub *Subscription) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, providerFetchBudget)
	defer cancel()

	body, _, err := s.downloadWithUA(ctx, upstreamURL, sub, subscriptionUserAgent)
	if err != nil {
		return nil, err
	}
	payload, _ := providerPayload(body)
	if payload == nil {
		return nil, fmt.Errorf("upstream returned empty/unparseable payload")
	}

	if countProviderNodes(string(payload)) > 0 {
		if err := s.cacheProviderPayload(sub, payload); err != nil {
			log.Printf("[Subscriptions] Failed to cache provider payload for %s: %v", upstreamURL, err)
		}
	} else {
		log.Printf("[Subscriptions] Upstream %s returned empty/unparseable payload, keeping previous cache", upstreamURL)
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
		return []byte("proxies: []\n"), "empty"
	}

	if trimmed[0] == '[' || trimmed[0] == '{' {
		scratchSub := &Subscription{}
		outbounds, _, err := parseSubscriptionBody(trimmed, "application/json", scratchSub)
		if err == nil && len(outbounds) > 0 {
			scratchSvc := &SubscriptionService{}
			nodes := scratchSvc.outboundsToNodes(outbounds, scratchSub)
			yamlOut, _ := scratchSvc.convertSubscriptionNodesToClashYAML(nodes)
			return []byte(yamlOut), "xray-json"
		}
	}

	content := string(trimmed)
	if section, wasFullConfig := extractProxiesSection(content); section != "" {
		if wasFullConfig {
			return []byte(section), "yaml-full"
		}
		return []byte(section), "yaml-proxies"
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return []byte(content), "raw"
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
