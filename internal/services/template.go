package services

import (
	"fmt"
	"io"
	"net/http"
	"sync"
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

func (s *TemplateService) Fetch(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to fetch template: %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}
