package services

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// NetworkToolsService provides network diagnostic services (such as public WAN IP detection).
type NetworkToolsService struct {
	mihomoAPIURL string
	mihomoSvc    *MihomoService
}

func NewNetworkToolsService(mihomoAPIURL string) *NetworkToolsService {
	return &NetworkToolsService{
		mihomoAPIURL: mihomoAPIURL,
	}
}

func (s *NetworkToolsService) SetMihomoService(svc *MihomoService) {
	s.mihomoSvc = svc
}

// IPInfo holds public IP information
type IPInfo struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// GetPublicIP gets current public IP of the router
func (s *NetworkToolsService) GetPublicIP() (*IPInfo, error) {
	result := &IPInfo{}

	// List of reliable public echo endpoints
	services := []string{
		"https://ipinfo.io/ip",
		"https://icanhazip.com",
		"https://ifconfig.me/ip",
		"https://api.ipify.org",
	}

	httpClient := &http.Client{
		Timeout: 4 * time.Second,
	}

	for _, svc := range services {
		req, err := http.NewRequest(http.MethodGet, svc, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "curl/7.88.1")
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		candidate := strings.TrimSpace(string(body))
		parsed := net.ParseIP(candidate)
		if parsed != nil {
			result.IP = parsed.String()
			result.Success = true
			return result, nil
		}
	}

	result.Success = false
	result.Error = "failed to detect public IP"
	return result, nil
}
