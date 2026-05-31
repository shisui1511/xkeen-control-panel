package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NetworkToolsService provides network diagnostic tools
type NetworkToolsService struct {
	mihomoAPIURL string
}

func NewNetworkToolsService(mihomoAPIURL string) *NetworkToolsService {
	return &NetworkToolsService{
		mihomoAPIURL: mihomoAPIURL,
	}
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
func (s *NetworkToolsService) HTTPTest(rawURL string, timeout int) (*CurlResult, error) {
	if timeout <= 0 || timeout > 60 {
		timeout = 10
	}

	// Validate URL scheme — only http and https are allowed
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return &CurlResult{
			URL:     rawURL,
			Success: false,
			Error:   "only http and https URLs are allowed",
		}, nil
	}

	result := &CurlResult{
		URL: rawURL,
	}

	var cmd *exec.Cmd
	if _, err := exec.LookPath("curl"); err == nil {
		cmd = exec.Command("curl", "-sL", "--connect-timeout", fmt.Sprintf("%d", timeout),
			"-w", "\nHTTP_CODE: %{http_code}\nTIME_TOTAL: %{time_total}\nSIZE: %{size_download}",
			"--", rawURL)
	} else if _, err := exec.LookPath("wget"); err == nil {
		cmd = exec.Command("wget", "-qO", "-", "--timeout="+fmt.Sprintf("%d", timeout), "--", rawURL)
	} else {
		result.Success = false
		result.Error = "curl or wget not found"
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
		conn, err := client.Dial("tcp", strings.TrimPrefix(strings.TrimPrefix(svc, "https://"), "http://")+":443")
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
	result.Error = "failed to detect public IP"
	return result, nil
}

// ProxyTestResult holds proxy test output
type ProxyTestResult struct {
	ProxyName string `json:"proxy_name"`
	URL       string `json:"url"`
	Success   bool   `json:"success"`
	Delay     int    `json:"delay"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

// PortCheckResult holds port check output
type PortCheckResult struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Success bool   `json:"success"`
	RTTMs   int64  `json:"rtt_ms"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// ProxyDelayTest makes an HTTP request to the Clash API to test proxy delay
func (s *NetworkToolsService) ProxyDelayTest(proxyName, targetURL string, timeoutMs int) (*ProxyTestResult, error) {
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}

	result := &ProxyTestResult{
		ProxyName: proxyName,
		URL:       targetURL,
	}

	escapedProxy := url.PathEscape(proxyName)
	apiURL := fmt.Sprintf("%s/proxies/%s/delay?url=%s&timeout=%d",
		strings.TrimSuffix(s.mihomoAPIURL, "/"),
		escapedProxy,
		url.QueryEscape(targetURL),
		timeoutMs,
	)

	client := &http.Client{
		Timeout: time.Duration(timeoutMs+1000) * time.Millisecond,
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = fmt.Sprintf("Failed to create HTTP request: %s", err.Error())
		return result, nil
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = fmt.Sprintf("Failed to execute request to Clash API: %s", err.Error())
		return result, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = fmt.Sprintf("Failed to read response body: %s", err.Error())
		return result, nil
	}

	if resp.StatusCode != http.StatusOK {
		result.Success = false
		var errData struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &errData) == nil && errData.Message != "" {
			result.Error = errData.Message
		} else {
			result.Error = fmt.Sprintf("Clash API returned status %d", resp.StatusCode)
		}
		result.Output = string(body)
		return result, nil
	}

	var data struct {
		Delay int `json:"delay"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("Failed to parse Clash API response: %s", err.Error())
		result.Output = string(body)
		return result, nil
	}

	result.Success = true
	result.Delay = data.Delay
	result.Output = fmt.Sprintf("Proxy: %s\nTarget URL: %s\nDelay: %d ms\nStatus: Reachable", proxyName, targetURL, data.Delay)
	return result, nil
}

// PortCheck checks if a TCP port on a remote host is open and measures RTT
func (s *NetworkToolsService) PortCheck(host string, port int, timeout time.Duration) (*PortCheckResult, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	result := &PortCheckResult{
		Host: host,
		Port: port,
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	rtt := time.Since(start).Milliseconds()

	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = fmt.Sprintf("Connection to %s failed:\n%s", addr, err.Error())
		return result, nil
	}
	conn.Close()

	result.Success = true
	result.RTTMs = rtt
	result.Output = fmt.Sprintf("Host: %s\nPort: %d\nStatus: Open\nRTT: %d ms", host, port, rtt)
	return result, nil
}
