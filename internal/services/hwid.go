package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var macSeparatorsRe = regexp.MustCompile(`[^0-9A-Fa-f]`)

func normalizeMACtoHWID(mac string) string {
	clean := macSeparatorsRe.ReplaceAllString(strings.TrimSpace(mac), "")
	clean = strings.ToUpper(clean)
	if len(clean) == 12 && clean != "000000000000" && clean != "FFFFFFFFFFFF" {
		return clean
	}
	return ""
}

func detectRouterMAC() string {
	// 1. Prefer br0 and eth0 (standard on Keenetic/Netcraze routers)
	for _, iface := range []string{"br0", "eth0"} {
		data, err := os.ReadFile(filepath.Join("/sys/class/net", iface, "address"))
		if err == nil {
			if hwid := normalizeMACtoHWID(string(data)); hwid != "" {
				return hwid
			}
		}
	}

	// 2. Scan network interfaces in /sys/class/net
	if entries, err := os.ReadDir("/sys/class/net"); err == nil {
		for _, entry := range entries {
			if entry.Name() == "lo" {
				continue
			}
			data, err := os.ReadFile(filepath.Join("/sys/class/net", entry.Name(), "address"))
			if err == nil {
				if hwid := normalizeMACtoHWID(string(data)); hwid != "" {
					return hwid
				}
			}
		}
	}

	// 3. Fallback to net.Interfaces()
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			if hwid := normalizeMACtoHWID(iface.HardwareAddr.String()); hwid != "" {
				return hwid
			}
		}
	}

	return ""
}

// LoadOrGenerateHWID определяет стабильный HWID роутера:
// 1) переменные окружения XCP_HWID / XCP_MIHOMO_HWID / XCP_DEVICE_HWID
// 2) валидный 12-значный MAC-адрес из xcp_hwid.txt
// 3) аппаратный MAC-адрес сетевого интерфейса роутера (br0/eth0, в верхнем регистре без двоеточий, например 123456789ABC)
// 4) machine-id SHA256 / случайный стабильный 12-значный hex-идентификатор
func LoadOrGenerateHWID(dataDir string) string {
	// 1. Environment variable override
	for _, envKey := range []string{"XCP_HWID", "XCP_MIHOMO_HWID", "XCP_DEVICE_HWID"} {
		if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
			if hwid := normalizeMACtoHWID(val); hwid != "" {
				return hwid
			}
			if len(val) <= 128 {
				return val
			}
		}
	}

	dir := filepath.Join(dataDir, "data")
	path := filepath.Join(dir, "xcp_hwid.txt")

	// 2. Check existing xcp_hwid.txt (if it's a valid 12-char hex MAC)
	if data, err := os.ReadFile(path); err == nil {
		trimmed := strings.TrimSpace(string(data))
		if hwid := normalizeMACtoHWID(trimmed); hwid != "" {
			return hwid
		}
		// If xcp_hwid.txt contains a legacy 36-char UUID v4, we fall through to detectRouterMAC
		// so that routers automatically upgrade to their real hardware MAC.
	}

	// 3. Detect hardware MAC from router interfaces
	if macHWID := detectRouterMAC(); macHWID != "" {
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(path, []byte(macHWID), 0600)
		return macHWID
	}

	// 5. Fallback: machine-id SHA256 (12 chars hex)
	for _, midPath := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(midPath); err == nil {
			raw := strings.TrimSpace(string(data))
			if raw != "" {
				sum := sha256.Sum256([]byte(raw))
				hwid := strings.ToUpper(hex.EncodeToString(sum[:6]))
				_ = os.MkdirAll(dir, 0755)
				_ = os.WriteFile(path, []byte(hwid), 0600)
				return hwid
			}
		}
	}

	// 6. Last resort fallback: random 12-char hex MAC
	var u [6]byte
	if _, err := rand.Read(u[:]); err == nil {
		u[0] = (u[0] | 0x02) & 0xfe
		hwid := strings.ToUpper(hex.EncodeToString(u[:]))
		_ = os.MkdirAll(dir, 0755)
		_ = os.WriteFile(path, []byte(hwid), 0600)
		return hwid
	}

	return "001122334455"
}
