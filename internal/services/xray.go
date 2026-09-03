package services

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
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
	// CGNAT 100.64.0.0/10
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && (v4[1]&0xc0) == 64 {
			return errors.New("carrier-grade NAT destination IP is prohibited")
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
