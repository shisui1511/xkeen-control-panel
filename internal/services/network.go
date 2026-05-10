package services

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

// NetworkToolsService provides network diagnostic tools
type NetworkToolsService struct{}

func NewNetworkToolsService() *NetworkToolsService {
	return &NetworkToolsService{}
}

// PingResult holds ping output
type PingResult struct {
	Host    string `json:"host"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Ping executes ping command
func (s *NetworkToolsService) Ping(host string, count int) (*PingResult, error) {
	if count <= 0 || count > 20 {
		count = 4
	}

	cmd := exec.Command("ping", "-c", fmt.Sprintf("%d", count), host)
	out, err := cmd.CombinedOutput()

	result := &PingResult{
		Host:   host,
		Output: string(out),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}

	return result, nil
}

// TracerouteResult holds traceroute output
type TracerouteResult struct {
	Host    string `json:"host"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Traceroute executes traceroute command
func (s *NetworkToolsService) Traceroute(host string, maxHops int) (*TracerouteResult, error) {
	if maxHops <= 0 || maxHops > 30 {
		maxHops = 20
	}

	cmd := exec.Command("traceroute", "-m", fmt.Sprintf("%d", maxHops), host)
	out, err := cmd.CombinedOutput()

	result := &TracerouteResult{
		Host:   host,
		Output: string(out),
	}

	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}

	return result, nil
}

// DNSResult holds DNS lookup results
type DNSResult struct {
	Host    string   `json:"host"`
	Records []string `json:"records"`
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
}

// DNSLookup performs DNS lookup
func (s *NetworkToolsService) DNSLookup(host string, recordType string) (*DNSResult, error) {
	if recordType == "" {
		recordType = "A"
	}

	result := &DNSResult{
		Host: host,
	}

	switch strings.ToUpper(recordType) {
	case "A", "AAAA":
		ips, err := net.LookupIP(host)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, nil
		}
		for _, ip := range ips {
			result.Records = append(result.Records, ip.String())
		}
		result.Success = true

	case "CNAME":
		cname, err := net.LookupCNAME(host)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, nil
		}
		result.Records = append(result.Records, cname)
		result.Success = true

	case "MX":
		mxs, err := net.LookupMX(host)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, nil
		}
		for _, mx := range mxs {
			result.Records = append(result.Records, fmt.Sprintf("%s (priority: %d)", mx.Host, mx.Pref))
		}
		result.Success = true

	case "NS":
		nss, err := net.LookupNS(host)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, nil
		}
		for _, ns := range nss {
			result.Records = append(result.Records, ns.Host)
		}
		result.Success = true

	case "TXT":
		txts, err := net.LookupTXT(host)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			return result, nil
		}
		result.Records = txts
		result.Success = true

	default:
		// Try nslookup for other types
		cmd := exec.Command("nslookup", "-type="+recordType, host)
		out, err := cmd.CombinedOutput()
		if err != nil {
			result.Success = false
			result.Error = string(out)
			return result, nil
		}
		result.Records = append(result.Records, string(out))
		result.Success = true
	}

	return result, nil
}

// CurlResult holds curl/wget output
type CurlResult struct {
	URL     string `json:"url"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// HTTPTest performs HTTP request test
func (s *NetworkToolsService) HTTPTest(url string, timeout int) (*CurlResult, error) {
	if timeout <= 0 || timeout > 60 {
		timeout = 10
	}

	result := &CurlResult{
		URL: url,
	}

	var cmd *exec.Cmd
	if _, err := exec.LookPath("curl"); err == nil {
		cmd = exec.Command("curl", "-sL", "--connect-timeout", fmt.Sprintf("%d", timeout),
			"-w", "\nHTTP_CODE: %{http_code}\nTIME_TOTAL: %{time_total}\nSIZE: %{size_download}",
			url)
	} else if _, err := exec.LookPath("wget"); err == nil {
		cmd = exec.Command("wget", "-qO", "-", "--timeout="+fmt.Sprintf("%d", timeout), url)
	} else {
		result.Success = false
		result.Error = "curl или wget не найдены"
		return result, nil
	}

	out, err := cmd.CombinedOutput()
	result.Output = string(out)

	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
	}

	return result, nil
}

// IPInfo holds public IP information
type IPInfo struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// GetPublicIP gets current public IP
func (s *NetworkToolsService) GetPublicIP() (*IPInfo, error) {
	result := &IPInfo{}

	// Try multiple services
	services := []string{
		"https://api.ipify.org",
		"https://icanhazip.com",
		"https://ifconfig.me/ip",
	}

	client := &net.Dialer{Timeout: 5 * time.Second}

	for _, svc := range services {
		conn, err := client.Dial("tcp", svc+":443")
		if err != nil {
			continue
		}
		conn.Close()
		// If we can connect, try curl
		cmd := exec.Command("curl", "-s", "--connect-timeout", "5", svc)
		out, err := cmd.Output()
		if err == nil {
			result.IP = strings.TrimSpace(string(out))
			result.Success = true
			return result, nil
		}
	}

	result.Success = false
	result.Error = "Не удалось определить IP"
	return result, nil
}
