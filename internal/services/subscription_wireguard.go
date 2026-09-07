package services

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// normalizeWireguardLocalAddress ensures local CIDR mask (/32 for IPv4, /128 for IPv6).
func normalizeWireguardLocalAddress(addr string) string {
	clean := strings.TrimSpace(addr)
	if clean == "" {
		return ""
	}
	if strings.Contains(clean, "/") {
		return clean
	}
	ip := net.ParseIP(clean)
	if ip == nil {
		return clean
	}
	if ip.To4() != nil {
		return clean + "/32"
	}
	return clean + "/128"
}

// decodeWireguardReserved parses reserved bytes from either a slice of 3 ints or a 3-byte base64 string.
func decodeWireguardReserved(raw interface{}) []int {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []int:
		if len(v) == 3 {
			return v
		}
	case []interface{}:
		if len(v) == 3 {
			res := make([]int, 3)
			for i, elem := range v {
				switch val := elem.(type) {
				case int:
					res[i] = val
				case float64:
					res[i] = int(val)
				case int64:
					res[i] = int(val)
				case string:
					num, err := strconv.Atoi(strings.TrimSpace(val))
					if err != nil {
						return nil
					}
					res[i] = num
				default:
					return nil
				}
			}
			return res
		}
	case string:
		clean := strings.TrimSpace(v)
		if clean == "" {
			return nil
		}
		// Try base64
		data, err := base64.StdEncoding.DecodeString(clean)
		if err == nil && len(data) == 3 {
			return []int{int(data[0]), int(data[1]), int(data[2])}
		}
		// Try comma-separated e.g. "0,0,0"
		parts := strings.Split(clean, ",")
		if len(parts) == 3 {
			res := make([]int, 3)
			for i, p := range parts {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil {
					return nil
				}
				res[i] = n
			}
			return res
		}
	}
	return nil
}

// isValidWireguardKey validates that a WireGuard key (secretKey, publicKey, preSharedKey)
// is a valid base64 string decoding to exactly 32 bytes (Curve25519 key size).
func isValidWireguardKey(key string) bool {
	clean := strings.TrimSpace(key)
	if clean == "" {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(clean)
	}
	return err == nil && len(b) == 32
}

// wireguardNodeToOutbound converts a SubscriptionNode into an Xray WireGuard Outbound.
// Returns (outbound, "") on success, or (nil, reason) if essential data is missing.
func wireguardNodeToOutbound(n *SubscriptionNode) (*Outbound, string) {
	if n == nil {
		return nil, "empty node"
	}
	secretKey := strings.TrimSpace(n.SecretKey)
	if secretKey == "" {
		return nil, "missing secretKey"
	}
	if !isValidWireguardKey(secretKey) {
		return nil, "invalid wireguard secretKey: expected 32-byte base64"
	}
	server := strings.TrimSpace(n.Server)
	if server == "" {
		return nil, "missing peer endpoint"
	}
	publicKey := strings.TrimSpace(n.PublicKey)
	if publicKey == "" {
		return nil, "missing peer publicKey"
	}
	if !isValidWireguardKey(publicKey) {
		return nil, "invalid wireguard peer publicKey: expected 32-byte base64"
	}

	peer := map[string]interface{}{
		"endpoint":  server,
		"publicKey": publicKey,
	}
	if psk := strings.TrimSpace(n.PreSharedKey); psk != "" {
		if !isValidWireguardKey(psk) {
			return nil, "invalid wireguard preSharedKey: expected 32-byte base64"
		}
		peer["preSharedKey"] = psk
	}
	if n.KeepAlive > 0 {
		peer["keepAlive"] = n.KeepAlive
	}
	if len(n.AllowedIPs) > 0 {
		peer["allowedIPs"] = n.AllowedIPs
	}

	settings := map[string]interface{}{
		"secretKey": secretKey,
		"peers":     []interface{}{peer},
	}

	if len(n.LocalAddresses) > 0 {
		var addrs []string
		for _, a := range n.LocalAddresses {
			if norm := normalizeWireguardLocalAddress(a); norm != "" {
				addrs = append(addrs, norm)
			}
		}
		if len(addrs) > 0 {
			settings["address"] = addrs
		}
	}

	if n.MTU > 0 {
		settings["mtu"] = n.MTU
	}

	if len(n.Reserved) == 3 {
		settings["reserved"] = n.Reserved
	}

	if n.AWG != nil {
		settings["awg"] = n.AWG
	}
	if len(n.DNS) > 0 {
		settings["dns"] = n.DNS
	}

	return &Outbound{
		Tag:      n.Tag,
		Protocol: "wireguard",
		Settings: settings,
	}, ""
}

// normalizeXrayWireguardOutbound ensures valid structure of an existing wireguard outbound.
func normalizeXrayWireguardOutbound(ob Outbound) Outbound {
	if ob.Settings == nil {
		ob.Settings = make(map[string]interface{})
	}
	peers, ok := ob.Settings["peers"].([]interface{})
	if !ok || len(peers) == 0 {
		return ob
	}
	return ob
}

// parseWireGuardLink parses a wireguard:// share-link.
// Returns (outbound, "") on success, or (nil, reason) on skip.
func parseWireGuardLink(link string) (*Outbound, string) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, "malformed wireguard URL"
	}

	secretKey := u.User.Username()
	if secretKey == "" {
		return nil, "missing secret key in userinfo"
	}
	if !isValidWireguardKey(secretKey) {
		return nil, "invalid wireguard secretKey: expected 32-byte base64"
	}

	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return nil, "missing host or port in wireguard URL"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Sprintf("invalid wireguard port: %s", portStr)
	}

	q := u.Query()
	publicKey := q.Get("publickey")
	if publicKey == "" {
		publicKey = q.Get("publicKey")
	}
	if publicKey == "" {
		publicKey = q.Get("public_key")
	}
	if publicKey == "" {
		return nil, "missing public key in query parameters"
	}
	if !isValidWireguardKey(publicKey) {
		return nil, "invalid wireguard peer publicKey: expected 32-byte base64"
	}

	psk := q.Get("presharedkey")
	if psk == "" {
		psk = q.Get("preSharedKey")
	}
	if psk == "" {
		psk = q.Get("pre_shared_key")
	}
	if psk == "" {
		psk = q.Get("psk")
	}

	peer := map[string]interface{}{
		"endpoint":  net.JoinHostPort(host, strconv.Itoa(port)),
		"publicKey": publicKey,
	}
	if psk != "" {
		if !isValidWireguardKey(psk) {
			return nil, "invalid wireguard preSharedKey: expected 32-byte base64"
		}
		peer["preSharedKey"] = psk
	}

	if kaStr := q.Get("keepalive"); kaStr != "" {
		if ka, err := strconv.Atoi(kaStr); err == nil && ka > 0 {
			peer["keepAlive"] = ka
		}
	}

	if allowedIPs := q.Get("allowed_ips"); allowedIPs != "" {
		peer["allowedIPs"] = strings.Split(allowedIPs, ",")
	} else if allowedIPs := q.Get("allowedIPs"); allowedIPs != "" {
		peer["allowedIPs"] = strings.Split(allowedIPs, ",")
	}

	settings := map[string]interface{}{
		"secretKey": secretKey,
		"peers":     []interface{}{peer},
	}

	// Local addresses (ip / address)
	localIP := q.Get("ip")
	if localIP == "" {
		localIP = q.Get("address")
	}
	if localIP != "" {
		var addrs []string
		for _, part := range strings.Split(localIP, ",") {
			if norm := normalizeWireguardLocalAddress(part); norm != "" {
				addrs = append(addrs, norm)
			}
		}
		if len(addrs) > 0 {
			settings["address"] = addrs
		}
	}

	if mtuStr := q.Get("mtu"); mtuStr != "" {
		if mtu, err := strconv.Atoi(mtuStr); err == nil && mtu > 0 {
			settings["mtu"] = mtu
		}
	}

	if resStr := q.Get("reserved"); resStr != "" {
		if res := decodeWireguardReserved(resStr); len(res) == 3 {
			settings["reserved"] = res
		}
	}

	awg := &AWGOptions{RawOptions: make(map[string]interface{})}
	for k, values := range q {
		if len(values) > 0 {
			parseAWGField(awg, k, values[0])
		}
	}
	if !awg.IsEmpty() {
		settings["awg"] = awg
	}

	if dnsStr := q.Get("dns"); dnsStr != "" {
		var dnsList []string
		for _, d := range strings.Split(dnsStr, ",") {
			if clean := strings.TrimSpace(d); clean != "" {
				dnsList = append(dnsList, clean)
			}
		}
		if len(dnsList) > 0 {
			settings["dns"] = dnsList
		}
	}

	tag := u.Fragment
	if tag == "" {
		if strings.EqualFold(u.Scheme, "awg") {
			tag = "AmneziaWG"
		} else {
			tag = "WireGuard"
		}
	}

	return &Outbound{
		Tag:      tag,
		Protocol: "wireguard",
		Settings: settings,
	}, ""
}
