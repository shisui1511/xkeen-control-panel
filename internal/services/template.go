package services

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "xray" or "mihomo"
	URL         string `json:"url"`
	Content     string `json:"content,omitempty"`
}

type TemplateService struct {
	templates []Template
	mu        sync.RWMutex
}

func NewTemplateService() *TemplateService {
	// Default templates
	return &TemplateService{
		templates: []Template{
			{
				Name:        "Xray: VLESS + Reality",
				Description: "Стандартная конфигурация Xray Reality с Vision",
				Type:        "xray",
				URL:         "https://raw.githubusercontent.com/XTLS/Xray-examples/main/VLESS-Reality-Vision/config.json",
			},
			{
				Name:        "Xray: VMess + WS",
				Description: "VMess через WebSocket (CDN friendly)",
				Type:        "xray",
				URL:         "https://raw.githubusercontent.com/XTLS/Xray-examples/main/VMess-Websocket-TLS/config_client.json",
			},
			{
				Name:        "Mihomo: Basic Config",
				Description: "Базовый конфиг Mihomo с группами прокси",
				Type:        "mihomo",
				URL:         "https://raw.githubusercontent.com/Loyalsoldier/clash-rules/release/config-classic-lite.yaml",
			},
			{
				Name:        "Mihomo: RU Bypass rules",
				Description: "Набор правил для обхода блокировок РФ",
				Type:        "mihomo",
				URL:         "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/master/Clash/Config/ACL4SSR_Online_Mini_MultiMode.ini", // This might need parsing
			},
		},
	}
}

func (s *TemplateService) List() []Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates
}

func (s *TemplateService) FetchByName(name string) (string, error) {
	s.mu.RLock()
	var urlStr string
	for _, t := range s.templates {
		if t.Name == name {
			urlStr = t.URL
			break
		}
	}
	s.mu.RUnlock()

	if urlStr == "" {
		return "", fmt.Errorf("requested template is not allowed or not found")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return "", fmt.Errorf("access to localhost is prohibited")
	}

	// Redundant check to satisfy CodeQL SSRF analysis.
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("failed to resolve host: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return "", fmt.Errorf("access to private network is prohibited")
		}
	}

	client := utils.SafeHTTPClient(10 * time.Second)

	resp, err := client.Get(urlStr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch template: %s", resp.Status)
	}

	// Limit response size to 1MB
	content, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", err
	}

	return string(content), nil
}
