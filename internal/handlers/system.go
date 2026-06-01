package handlers

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type SystemStats struct {
	Memory struct {
		Total uint64 `json:"total"`
		Used  uint64 `json:"used"`
		Free  uint64 `json:"free"`
	} `json:"memory"`
	Load   [3]float64 `json:"load"`
	Uptime struct {
		Seconds float64 `json:"seconds"`
		Days    int     `json:"days"`
		Hours   int     `json:"hours"`
		Minutes int     `json:"minutes"`
	} `json:"uptime"`
	GoRuntime struct {
		Goroutines int    `json:"goroutines"`
		HeapAlloc  uint64 `json:"heap_alloc"`
		HeapSys    uint64 `json:"heap_sys"`
		NumGC      uint32 `json:"num_gc"`
		GoVersion  string `json:"go_version"`
		GOMAXPROCS int    `json:"gomaxprocs"`
		GOARCH     string `json:"goarch"`
	} `json:"go_runtime"`
	RouterModel    string   `json:"router_model"`
	Hostname       string   `json:"hostname"`
	WANStatus      string   `json:"wan_status"`
	DefaultGateway string   `json:"default_gateway"`
	DNSServers     []string `json:"dns_servers"`
	DNSResolving   bool     `json:"dns_resolving"`
	InvalidConfig  bool     `json:"invalid_config"`
	// New fields for reference-matching System Info card
	Platform      string `json:"platform"`
	KernelVersion string `json:"kernel_version"`
	IPInterface   string `json:"ip_interface"`
	Timezone      string `json:"timezone"`
	ConfigPath    string `json:"config_path"`
	ConfigLines   int    `json:"config_lines"`
	BootTime      string `json:"boot_time"`
}

func (a *API) SystemStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	stats := SystemStats{}

	// Memory from /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			key := strings.TrimSuffix(fields[0], ":")
			val, _ := strconv.ParseUint(fields[1], 10, 64)
			val *= 1024 // kB to bytes

			switch key {
			case "MemTotal":
				stats.Memory.Total = val
			case "MemAvailable":
				stats.Memory.Free = val
			}
		}
		stats.Memory.Used = stats.Memory.Total - stats.Memory.Free
	}

	// Load average from /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			stats.Load[0], _ = strconv.ParseFloat(fields[0], 64)
			stats.Load[1], _ = strconv.ParseFloat(fields[1], 64)
			stats.Load[2], _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// Uptime from /proc/uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			seconds, _ := strconv.ParseFloat(fields[0], 64)
			stats.Uptime.Seconds = seconds
			stats.Uptime.Days = int(seconds) / 86400
			stats.Uptime.Hours = (int(seconds) % 86400) / 3600
			stats.Uptime.Minutes = (int(seconds) % 3600) / 60
		}
	}

	// Go runtime stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	stats.GoRuntime.Goroutines = runtime.NumGoroutine()
	stats.GoRuntime.HeapAlloc = m.HeapAlloc
	stats.GoRuntime.HeapSys = m.HeapSys
	stats.GoRuntime.NumGC = m.NumGC
	stats.GoRuntime.GoVersion = runtime.Version()
	stats.GoRuntime.GOMAXPROCS = runtime.GOMAXPROCS(0)
	stats.GoRuntime.GOARCH = runtime.GOARCH

	// Router telemetry and network diagnostic fields
	stats.RouterModel = getRouterModel()
	stats.Hostname, _ = os.Hostname()
	stats.WANStatus, stats.DefaultGateway = getWANStats()
	stats.DNSServers = getDNSServers()
	stats.DNSResolving = testDNSResolving()
	stats.InvalidConfig = a.checkActiveConfigsInvalid()

	// Extended system info for reference-matching UI
	stats.Platform = runtime.GOOS + "/" + runtime.GOARCH
	stats.KernelVersion = getKernelVersion()
	stats.IPInterface = getPrimaryLANIP()
	stats.Timezone = getSystemTimezone()
	stats.ConfigPath = "/opt/etc/xkeen/"
	stats.ConfigLines = countDirLines(a.cfg.XRayConfigDir) + countDirLines(a.cfg.MihomoConfigDir)
	if stats.Uptime.Seconds > 0 {
		bootTime := time.Now().Add(-time.Duration(stats.Uptime.Seconds) * time.Second)
		stats.BootTime = bootTime.Format("02.01.06 15:04:05") + " " + getUTCOffset()
	}

	a.jsonResponse(w, stats)
}

func getRouterModel() string {
	for _, path := range []string{"/proc/device-tree/model", "/sys/firmware/devicetree/base/model"} {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", ""))
		}
	}
	return "Keenetic Router"
}

func getWANStats() (string, string) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "offline", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		_ = scanner.Text() // skip header
	}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		dest := fields[1]
		gatewayHex := fields[2]
		iface := fields[0]

		if dest == "00000000" {
			ip, err := parseHexIP(gatewayHex)
			if err == nil {
				// If gateway is 0.0.0.0, we just return the interface name
				if ip == "0.0.0.0" {
					return "online", iface
				}
				return "online", fmt.Sprintf("%s (%s)", ip, iface)
			}
			return "online", iface
		}
	}
	return "offline", ""
}

func parseHexIP(hexStr string) (string, error) {
	if len(hexStr) != 8 {
		return "", fmt.Errorf("invalid hex IP length")
	}
	val, err := strconv.ParseUint(hexStr, 16, 32)
	if err != nil {
		return "", err
	}

	b0 := byte(val & 0xff)
	b1 := byte((val >> 8) & 0xff)
	b2 := byte((val >> 16) & 0xff)
	b3 := byte((val >> 24) & 0xff)

	return fmt.Sprintf("%d.%d.%d.%d", b0, b1, b2, b3), nil
}

func getDNSServers() []string {
	var dns []string
	paths := []string{"/etc/resolv.conf", "/opt/etc/resolv.conf"}
	for _, path := range paths {
		if file, err := os.Open(path); err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "nameserver ") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						dns = append(dns, fields[1])
					}
				}
			}
			file.Close()
			if len(dns) > 0 {
				break
			}
		}
	}
	return dns
}

func testDNSResolving() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := &net.Resolver{}
	addrs, err := r.LookupHost(ctx, "cloudflare.com")
	return err == nil && len(addrs) > 0
}

func (a *API) checkActiveConfigsInvalid() bool {
	a.configValCacheMutex.Lock()
	defer a.configValCacheMutex.Unlock()

	// Cache validation status for 30 seconds to avoid high CPU load
	if time.Since(a.configValCacheTime) < 30*time.Second {
		return a.configValCache
	}

	invalid := false

	// Check Xray configuration
	xrayBin := a.getBinaryPath("xray")
	if xrayBin != "" {
		if _, err := os.Stat(a.cfg.XRayConfigDir); err == nil {
			cmd := exec.Command(xrayBin, "-test", "-confdir", a.cfg.XRayConfigDir)
			if err := cmd.Run(); err != nil {
				invalid = true
			}
		}
	}

	// Check Mihomo configuration if Xray is valid (or if we need to check both)
	if !invalid {
		mihomoBin := a.getBinaryPath("mihomo")
		if mihomoBin != "" {
			if _, err := os.Stat(a.cfg.MihomoConfigDir); err == nil {
				cmd := exec.Command(mihomoBin, "-t", "-d", a.cfg.MihomoConfigDir)
				if err := cmd.Run(); err != nil {
					invalid = true
				}
			}
		}
	}

	a.configValCache = invalid
	a.configValCacheTime = time.Now()
	return invalid
}

func getKernelVersion() string {
	if data, err := os.ReadFile("/proc/version"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			return fields[2]
		}
	}
	return ""
}

func getPrimaryLANIP() string {
	// Prefer common Keenetic LAN bridge interface
	for _, name := range []string{"br0", "br-lan", "eth0"} {
		if iface, err := net.InterfaceByName(name); err == nil {
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil && !ipNet.IP.IsLoopback() {
						return ipNet.IP.String()
					}
				}
			}
		}
	}
	// Fallback: first non-loopback IPv4
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
						return ipNet.IP.String()
					}
				}
			}
		}
	}
	return ""
}

func getSystemTimezone() string {
	// /etc/TZ is common on OpenWrt/Keenetic (e.g. "MSK-3")
	if data, err := os.ReadFile("/etc/TZ"); err == nil {
		tz := strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", ""))
		if tz != "" {
			_, offset := time.Now().Zone()
			hours := offset / 3600
			if hours >= 0 {
				return fmt.Sprintf("%s · UTC+%d", tz, hours)
			}
			return fmt.Sprintf("%s · UTC%d", tz, hours)
		}
	}
	name, offset := time.Now().Zone()
	hours := offset / 3600
	if hours >= 0 {
		return fmt.Sprintf("%s · UTC+%d", name, hours)
	}
	return fmt.Sprintf("%s · UTC%d", name, hours)
}

func getUTCOffset() string {
	_, offset := time.Now().Zone()
	hours := offset / 3600
	if hours >= 0 {
		return fmt.Sprintf("UTC+%d", hours)
	}
	return fmt.Sprintf("UTC%d", hours)
}

func countDirLines(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		if len(data) == 0 {
			continue
		}
		lines := strings.Count(string(data), "\n")
		if !strings.HasSuffix(string(data), "\n") {
			lines++
		}
		total += lines
	}
	return total
}
