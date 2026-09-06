package services

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// RealityKeypair holds generated Reality credentials.
type RealityKeypair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
}

// GenerateRealityKeypair generates a new x25519 keypair and random 8-byte short ID for Xray Reality.
func GenerateRealityKeypair() (*RealityKeypair, error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	pub := priv.PublicKey()

	// Xray Reality uses RawURLEncoding (base64 without padding)
	privKeyStr := base64.RawURLEncoding.EncodeToString(priv.Bytes())
	pubKeyStr := base64.RawURLEncoding.EncodeToString(pub.Bytes())

	shortBytes := make([]byte, 8)
	if _, err := rand.Read(shortBytes); err != nil {
		return nil, fmt.Errorf("failed to generate short id: %w", err)
	}
	shortIDStr := hex.EncodeToString(shortBytes)

	return &RealityKeypair{
		PrivateKey: privKeyStr,
		PublicKey:  pubKeyStr,
		ShortID:    shortIDStr,
	}, nil
}

// TLSPingResult holds the outcome and cryptographic diagnostics of a TLS handshake ping.
type TLSPingResult struct {
	OK              bool     `json:"ok"`
	TLSVersion      string   `json:"tls_version,omitempty"`
	CipherSuite     string   `json:"cipher_suite,omitempty"`
	ALPN            string   `json:"alpn,omitempty"`
	PeerCN          string   `json:"peer_cn,omitempty"`
	DNSNames        []string `json:"dns_names,omitempty"`
	NotBefore       string   `json:"not_before,omitempty"`
	NotAfter        string   `json:"not_after,omitempty"`
	DaysUntilExpiry int      `json:"days_until_expiry,omitempty"`
	ChainLength     int      `json:"chain_length,omitempty"`
	HandshakeMs     int64    `json:"handshake_ms"`
	Error           string   `json:"error,omitempty"`
}

// ValidateTLSTarget checks if the target destination is safe to connect to,
// preventing SSRF, port scanning against the panel, and loopback/private IP connections.
// It resolves domain names once, validates all returned IPs, and returns the concrete
// vetted dial address (ip:port) and the server name (SNI), mitigating DNS-rebinding TOCTOU attacks.
func ValidateTLSTarget(dest string, panelPort int) (string, string, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", "", errors.New("destination cannot be empty")
	}

	for _, r := range dest {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", "", errors.New("destination contains invalid or control characters")
		}
	}

	host := dest
	port := 443

	if strings.Contains(dest, ":") {
		if h, p, err := net.SplitHostPort(dest); err == nil {
			host = h
			parsedPort, err := strconv.Atoi(p)
			if err != nil || parsedPort < 1 || parsedPort > 65535 {
				return "", "", errors.New("port must be between 1 and 65535")
			}
			port = parsedPort
		} else if ip := net.ParseIP(dest); ip != nil {
			// Bare IPv6 literal (e.g. "::1" or "2001:db8::1")
			host = dest
			port = 443
		} else {
			return "", "", fmt.Errorf("invalid host:port format: %w", err)
		}
	}

	if host == "" {
		return "", "", errors.New("host cannot be empty")
	}
	if len(host) > 253 {
		return "", "", errors.New("host exceeds maximum length of 253 characters")
	}

	if panelPort > 0 && port == panelPort {
		return "", "", errors.New("destination port matches control panel port")
	}

	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".local") || strings.HasSuffix(lowerHost, ".internal") || strings.HasSuffix(lowerHost, ".lan") {
		return "", "", errors.New("loopback or local domain destination is prohibited")
	}

	if ip := net.ParseIP(host); ip != nil {
		if err := isProhibitedIP(ip); err != nil {
			return "", "", err
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(port)), host, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve host: %w", err)
	}
	if len(ips) == 0 {
		return "", "", errors.New("no IP addresses resolved for host")
	}

	var chosen net.IP
	for _, resolvedIP := range ips {
		if err := isProhibitedIP(resolvedIP); err != nil {
			return "", "", fmt.Errorf("domain resolves to a prohibited IP: %w", err)
		}
		if chosen == nil {
			chosen = resolvedIP
		}
	}

	return net.JoinHostPort(chosen.String(), strconv.Itoa(port)), host, nil
}

func isProhibitedIP(ip net.IP) error {
	if ip.IsUnspecified() {
		return errors.New("unspecified destination IP is prohibited")
	}
	if ip.IsLoopback() {
		return errors.New("loopback destination IP is prohibited")
	}
	if ip.IsPrivate() {
		return errors.New("private network destination IP is prohibited")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return errors.New("link-local destination IP is prohibited")
	}
	if ip.IsMulticast() {
		return errors.New("multicast destination IP is prohibited")
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 {
			return errors.New("unspecified destination IP is prohibited")
		}
		// CGNAT 100.64.0.0/10
		if v4[0] == 100 && (v4[1]&0xc0) == 64 {
			return errors.New("carrier-grade NAT destination IP is prohibited")
		}
		// Benchmarking 198.18.0.0/15
		if v4[0] == 198 && (v4[1]&0xfe) == 18 {
			return errors.New("benchmarking network destination IP is prohibited")
		}
		// Reserved 240.0.0.0/4 (240..255)
		if v4[0] >= 240 {
			return errors.New("reserved network destination IP is prohibited")
		}
	}
	return nil
}

// TLSPing performs a native TLS handshake against the target destination.
// InsecureSkipVerify is intentionally set to true because this utility's purpose
// is diagnosing remote TLS configuration and parameters, not validating certificate trust chains.
func TLSPing(dest string, serverName string, alpn []string) (*TLSPingResult, error) {
	dest = strings.TrimSpace(dest)
	targetAddr := dest
	if !strings.Contains(targetAddr, ":") {
		targetAddr = net.JoinHostPort(targetAddr, "443")
	}

	host, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		host = targetAddr
	}

	if serverName == "" {
		serverName = host
	}

	d := &net.Dialer{Timeout: 5 * time.Second}
	start := time.Now()

	tlsConfig := &tls.Config{
		ServerName:         serverName,
		NextProtos:         alpn,
		InsecureSkipVerify: true, // Intentionally true: diagnostic tool inspecting cipher/version
		MinVersion:         tls.VersionTLS12,
	}

	conn, err := tls.DialWithDialer(d, "tcp", targetAddr, tlsConfig)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return &TLSPingResult{
			OK:          false,
			HandshakeMs: elapsed,
			Error:       err.Error(),
		}, nil
	}
	defer conn.Close()

	state := conn.ConnectionState()
	res := &TLSPingResult{
		OK:          true,
		TLSVersion:  tls.VersionName(state.Version),
		CipherSuite: tls.CipherSuiteName(state.CipherSuite),
		ALPN:        state.NegotiatedProtocol,
		HandshakeMs: elapsed,
	}

	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		res.PeerCN = cert.Subject.CommonName
		res.DNSNames = cert.DNSNames
		res.NotBefore = cert.NotBefore.Format(time.RFC3339)
		res.NotAfter = cert.NotAfter.Format(time.RFC3339)
		res.DaysUntilExpiry = int(time.Until(cert.NotAfter).Hours() / 24)
		res.ChainLength = len(state.PeerCertificates)
	}

	return res, nil
}

// XrayAPIFragmentInfo describes where the Xray API inbound is located in a config directory.
type XrayAPIFragmentInfo struct {
	// Path is the full path to the file containing the API inbound, or the recommended path if none exists.
	Path string
	// APIPresent indicates whether an inbound with tag "api" was found.
	APIPresent bool
	// FileExists indicates whether the file at Path exists on disk.
	FileExists bool
	// IsModular indicates whether the directory uses a modular multi-file layout.
	IsModular bool
	// HasAnyJSON indicates whether any JSON files exist in the configuration directory.
	HasAnyJSON bool
}

// FindXrayAPIFragment scans the given Xray config directory to find which JSON fragment
// contains the "api" inbound. If not found, it returns 00_api.json for modular deployments
// or config.json for monolithic deployments.
func FindXrayAPIFragment(configDir string) XrayAPIFragmentInfo {
	if configDir == "" {
		return XrayAPIFragmentInfo{Path: "config.json"}
	}

	entries, err := os.ReadDir(configDir)
	if err != nil {
		p := filepath.Join(configDir, "config.json")
		_, statErr := os.Stat(p)
		return XrayAPIFragmentInfo{
			Path:       p,
			FileExists: statErr == nil,
			HasAnyJSON: statErr == nil,
		}
	}

	var jsonFiles []string
	hasConfigJSON := false
	hasModularAPI := false

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			jsonFiles = append(jsonFiles, entry.Name())
			if entry.Name() == "config.json" {
				hasConfigJSON = true
			}
			if entry.Name() == "00_api.json" {
				hasModularAPI = true
			}
		}
	}

	if len(jsonFiles) == 0 {
		return XrayAPIFragmentInfo{
			Path:       filepath.Join(configDir, "config.json"),
			APIPresent: false,
			FileExists: false,
			IsModular:  false,
			HasAnyJSON: false,
		}
	}

	isModular := len(jsonFiles) > 1 || (len(jsonFiles) == 1 && !hasConfigJSON)

	// Scan all JSON files to see if any already contains inbound with tag "api"
	for _, name := range jsonFiles {
		filePath := filepath.Join(configDir, name)
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			continue
		}
		var root map[string]interface{}
		if json.Unmarshal(data, &root) != nil || root == nil {
			continue
		}
		if inbs, ok := root["inbounds"].([]interface{}); ok {
			for _, inb := range inbs {
				if m, ok := inb.(map[string]interface{}); ok {
					if tag, _ := m["tag"].(string); tag == "api" {
						return XrayAPIFragmentInfo{
							Path:       filePath,
							APIPresent: true,
							FileExists: true,
							IsModular:  isModular,
							HasAnyJSON: true,
						}
					}
				}
			}
		}
	}

	// Not found in any file: determine target path
	if isModular {
		targetPath := filepath.Join(configDir, "00_api.json")
		return XrayAPIFragmentInfo{
			Path:       targetPath,
			APIPresent: false,
			FileExists: hasModularAPI,
			IsModular:  true,
			HasAnyJSON: true,
		}
	}

	targetPath := filepath.Join(configDir, "config.json")
	return XrayAPIFragmentInfo{
		Path:       targetPath,
		APIPresent: false,
		FileExists: hasConfigJSON,
		IsModular:  false,
		HasAnyJSON: true,
	}
}

// ValidateXrayConfigDir validates an Xray configuration directory or file using `xray -test`.
// If xray binary is not found on the system, it returns (true, "") to allow environments without
// Entware (such as CI or local dev) to pass. If validation fails, it returns (false, combinedOutput).
func ValidateXrayConfigDir(configPath string) (bool, string) {
	if configPath == "" {
		return true, ""
	}

	xrayBin := ""
	candidates := []string{"xray", "/opt/sbin/xray", "/opt/bin/xray", "/usr/bin/xray", "/usr/local/bin/xray"}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			xrayBin = p
			break
		}
	}
	if xrayBin == "" {
		return true, ""
	}

	var cmd *exec.Cmd
	st, err := os.Stat(configPath)
	if err == nil && st.IsDir() {
		cmd = exec.Command(xrayBin, "-test", "-confdir", configPath)
	} else {
		cmd = exec.Command(xrayBin, "-test", "-config", configPath)
	}

	// Setup asset location env
	env := os.Environ()
	assetEnvFound := false
	for _, e := range env {
		if strings.HasPrefix(e, "XRAY_LOCATION_ASSET=") {
			assetEnvFound = true
			break
		}
	}
	if !assetEnvFound {
		assetCandidates := []string{
			"/opt/etc/xray/dat",
			"/opt/share/xray",
			"/opt/etc/xray",
		}
		if st != nil && st.IsDir() {
			assetCandidates = append(assetCandidates, filepath.Dir(configPath), configPath)
		} else {
			assetCandidates = append(assetCandidates, filepath.Dir(configPath))
		}
		for _, dir := range assetCandidates {
			if dSt, dErr := os.Stat(dir); dErr == nil && dSt.IsDir() {
				env = append(env, "XRAY_LOCATION_ASSET="+dir)
				break
			}
		}
	}
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(out)
	}
	return true, ""
}
