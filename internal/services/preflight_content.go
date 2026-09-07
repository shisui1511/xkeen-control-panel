package services

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidateConfigContent performs 6-stage semantic preflight validation over raw config content.
// It is a pure function: no file I/O, no network calls, no subprocess execution.
// Soft gate: Valid is always true, Errors is always empty, issues are collected in Warnings.
func ValidateConfigContent(kernel string, filename string, content string) PreflightResult {
	result := PreflightResult{
		Valid:    true,
		Errors:   []PreflightIssue{},
		Warnings: []PreflightIssue{},
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		result.Warnings = append(result.Warnings, PreflightIssue{
			Code:    "preflight.syntax",
			Message: "Configuration content is empty",
		})
		return result
	}

	kernel = strings.ToLower(kernel)
	baseFilename := filename
	if idx := strings.LastIndex(filename, "/"); idx >= 0 {
		baseFilename = filename[idx+1:]
	}

	var data map[string]interface{}

	// -------------------------------------------------------------------------
	// Step 1: Syntax Validation (preflight.syntax)
	// -------------------------------------------------------------------------
	if kernel == "mihomo" {
		err := yaml.Unmarshal([]byte(content), &data)
		if err != nil || data == nil {
			result.Warnings = append(result.Warnings, PreflightIssue{
				Code:    "preflight.syntax",
				Message: "Syntax error in YAML configuration",
			})
			return result
		}
	} else if kernel == "xray" {
		err := json.Unmarshal([]byte(content), &data)
		if err != nil || data == nil {
			result.Warnings = append(result.Warnings, PreflightIssue{
				Code:    "preflight.syntax",
				Message: "Syntax error in JSON configuration",
			})
			return result
		}
	} else {
		// Unknown kernel
		return result
	}

	// -------------------------------------------------------------------------
	// Step 2: Remnawave Headers must be list (preflight.header_not_list) [Mihomo only]
	// -------------------------------------------------------------------------
	if kernel == "mihomo" {
		validateRemnawaveHeaders(data, &result)
	}

	// -------------------------------------------------------------------------
	// Step 3: DNS Loop Protection (preflight.dns_loop) [Mihomo only]
	// -------------------------------------------------------------------------
	if kernel == "mihomo" {
		validateDNSLoop(data, &result)
	}

	// -------------------------------------------------------------------------
	// Step 4: DNS-over-VLESS Bypass (preflight.dns_over_vless)
	// -------------------------------------------------------------------------
	validateDNSOverVless(kernel, baseFilename, data, &result)

	// -------------------------------------------------------------------------
	// Step 5: LAN and RDP Bypass (preflight.lan_rdp)
	// -------------------------------------------------------------------------
	validateLanRdp(kernel, baseFilename, data, &result)

	// -------------------------------------------------------------------------
	// Step 6: Service Port Conflicts & TProxy/Hybrid Mode (preflight.port_conflict)
	// -------------------------------------------------------------------------
	validatePortConflicts(kernel, baseFilename, data, &result)

	// -------------------------------------------------------------------------
	// Step 7: AmneziaWG obfuscation options check (preflight.awg_flat_fields) [Mihomo only]
	// -------------------------------------------------------------------------
	if kernel == "mihomo" {
		validateAmneziaWgOptions(data, &result)
	}

	return result
}

// -----------------------------------------------------------------------------
// Step 2: Remnawave Headers
// -----------------------------------------------------------------------------
func validateRemnawaveHeaders(data map[string]interface{}, res *PreflightResult) {
	rawProviders, ok := data["proxy-providers"]
	if !ok || rawProviders == nil {
		return
	}
	providersMap, ok := rawProviders.(map[string]interface{})
	if !ok {
		return
	}

	// Sort keys for deterministic output
	providerNames := make([]string, 0, len(providersMap))
	for name := range providersMap {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)

	for _, name := range providerNames {
		pObj, ok := providersMap[name].(map[string]interface{})
		if !ok {
			continue
		}
		rawHeader, ok := pObj["header"]
		if !ok || rawHeader == nil {
			continue
		}
		if _, isString := rawHeader.(string); isString {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.header_not_list",
				Message: fmt.Sprintf("Proxy provider %q header is a string; Mihomo requires a map of string lists", name),
			})
			continue
		}
		headerMap, ok := rawHeader.(map[string]interface{})
		if !ok {
			continue
		}

		headerKeys := make([]string, 0, len(headerMap))
		for k := range headerMap {
			headerKeys = append(headerKeys, k)
		}
		sort.Strings(headerKeys)

		for _, hName := range headerKeys {
			val := headerMap[hName]
			if _, isString := val.(string); isString {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.header_not_list",
					Message: fmt.Sprintf("Proxy provider %q header %q is a string; Mihomo requires list of strings", name, hName),
				})
			}
		}
	}
}

// -----------------------------------------------------------------------------
// Step 3: DNS Loop Protection
// -----------------------------------------------------------------------------
func validateDNSLoop(data map[string]interface{}, res *PreflightResult) {
	rawDNS, ok := data["dns"]
	if !ok || rawDNS == nil {
		return
	}
	dnsMap, ok := rawDNS.(map[string]interface{})
	if !ok {
		return
	}

	listenAddr := ""
	if rawListen, ok := dnsMap["listen"]; ok {
		listenAddr = fmt.Sprintf("%v", rawListen)
	}

	var upstreams []string
	for _, key := range []string{"nameserver", "fallback", "default-nameserver"} {
		if rawList, ok := dnsMap[key].([]interface{}); ok {
			for _, item := range rawList {
				s := fmt.Sprintf("%v", item)
				if s != "" {
					upstreams = append(upstreams, s)
				}
			}
		}
	}

	if len(upstreams) == 0 {
		return
	}

	allLoopback := true
	for _, u := range upstreams {
		if !isLoopbackUpstream(u, listenAddr) {
			allLoopback = false
			break
		}
	}

	if allLoopback {
		res.Warnings = append(res.Warnings, PreflightIssue{
			Code:    "preflight.dns_loop",
			Message: "All configured DNS upstreams point to loopback or proxy's own listen port, causing a DNS loop",
		})
	}
}

func isLoopbackUpstream(upstream string, listenAddr string) bool {
	u := strings.TrimSpace(upstream)
	// Remove scheme if present (e.g. tls://, https://, quic://)
	if idx := strings.Index(u, "://"); idx != -1 {
		u = u[idx+3:]
	}
	// Remove path (e.g. /dns-query)
	if idx := strings.Index(u, "/"); idx != -1 {
		u = u[:idx]
	}

	host := u
	port := ""
	if h, p, err := net.SplitHostPort(u); err == nil {
		host = h
		port = p
	}

	// Host check
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}

	// Compare with listen address
	if listenAddr != "" {
		lHost := listenAddr
		lPort := ""
		if h, p, err := net.SplitHostPort(listenAddr); err == nil {
			lHost = h
			lPort = p
		}
		if (lHost == "" || lHost == "0.0.0.0" || lHost == host) && lPort == port && port != "" {
			return true
		}
	}

	return false
}

// -----------------------------------------------------------------------------
// Step 4: DNS-over-VLESS Bypass (127.0.0.53 -> DIRECT)
// -----------------------------------------------------------------------------
func validateDNSOverVless(kernel string, filename string, data map[string]interface{}, res *PreflightResult) {
	if kernel == "mihomo" {
		rules, ok := data["rules"].([]interface{})
		if !ok || len(rules) == 0 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.dns_over_vless",
				Message: "Missing rule routing 127.0.0.53 to DIRECT to protect system resolver",
			})
			return
		}

		hasDirect53 := false
		for _, r := range rules {
			str, ok := r.(string)
			if !ok {
				continue
			}
			parts := strings.Split(str, ",")
			if len(parts) < 3 {
				continue
			}
			ruleType := strings.ToUpper(strings.TrimSpace(parts[0]))
			payload := strings.TrimSpace(parts[1])
			target := strings.ToUpper(strings.TrimSpace(parts[2]))

			if target == "DIRECT" {
				if (ruleType == "IP-CIDR" || ruleType == "IP-CIDR6") &&
					(payload == "127.0.0.53/32" || payload == "127.0.0.53" || payload == "127.0.0.0/8") {
					hasDirect53 = true
					break
				}
				if ruleType == "DST-PORT" && (payload == "53" || payload == "53/53") {
					hasDirect53 = true
					break
				}
			}
		}

		if !hasDirect53 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.dns_over_vless",
				Message: "Missing rule routing 127.0.0.53 to DIRECT to protect system resolver",
			})
		}
	} else if kernel == "xray" {
		if !strings.Contains(filename, "routing") {
			return
		}
		routingObj, ok := data["routing"].(map[string]interface{})
		if !ok {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.dns_over_vless",
				Message: "Missing routing section with 127.0.0.53 bypass to direct outbound",
			})
			return
		}
		rules, ok := routingObj["rules"].([]interface{})
		if !ok || len(rules) == 0 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.dns_over_vless",
				Message: "Missing routing rule directing 127.0.0.53 to direct outbound",
			})
			return
		}

		hasDirect53 := false
		for _, r := range rules {
			ruleMap, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			outbound := fmt.Sprintf("%v", ruleMap["outboundTag"])
			if strings.EqualFold(outbound, "direct") || strings.EqualFold(outbound, "freedom") {
				if ipList, ok := ruleMap["ip"].([]interface{}); ok {
					for _, ipItem := range ipList {
						ipStr := fmt.Sprintf("%v", ipItem)
						if ipStr == "127.0.0.53" || ipStr == "127.0.0.53/32" || ipStr == "127.0.0.0/8" {
							hasDirect53 = true
							break
						}
					}
				}
				if portVal, exists := ruleMap["port"]; exists {
					portStr := fmt.Sprintf("%v", portVal)
					if portStr == "53" || strings.HasPrefix(portStr, "53,") || strings.HasSuffix(portStr, ",53") || strings.Contains(portStr, ",53,") {
						hasDirect53 = true
						break
					}
				}
			}
			if hasDirect53 {
				break
			}
		}

		if !hasDirect53 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.dns_over_vless",
				Message: "Missing routing rule directing 127.0.0.53 to direct outbound",
			})
		}
	}
}

// -----------------------------------------------------------------------------
// Step 5: LAN and RDP Bypass
// -----------------------------------------------------------------------------
func validateLanRdp(kernel string, filename string, data map[string]interface{}, res *PreflightResult) {
	if kernel == "mihomo" {
		rules, ok := data["rules"].([]interface{})
		if !ok || len(rules) == 0 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.lan_rdp",
				Message: "Missing direct bypass for private networks (geoip:private) or remote desktop port (3389)",
			})
			return
		}

		hasPrivateDirect := false
		hasRdpDirect := false

		for _, r := range rules {
			str, ok := r.(string)
			if !ok {
				continue
			}
			parts := strings.Split(str, ",")
			if len(parts) < 3 {
				continue
			}
			ruleType := strings.ToUpper(strings.TrimSpace(parts[0]))
			payload := strings.TrimSpace(parts[1])
			target := strings.ToUpper(strings.TrimSpace(parts[2]))

			if target == "DIRECT" {
				if ruleType == "GEOIP" && strings.EqualFold(payload, "private") {
					hasPrivateDirect = true
				}
				if ruleType == "DST-PORT" && (payload == "3389" || strings.Contains(payload, "3389")) {
					hasRdpDirect = true
				}
			}
		}

		if !hasPrivateDirect || !hasRdpDirect {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.lan_rdp",
				Message: "Missing direct bypass for private networks (geoip:private) or remote desktop port (3389)",
			})
		}
	} else if kernel == "xray" {
		if !strings.Contains(filename, "routing") {
			return
		}
		routingObj, ok := data["routing"].(map[string]interface{})
		if !ok {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.lan_rdp",
				Message: "Missing direct bypass for private networks (geoip:private) or remote desktop port (3389)",
			})
			return
		}
		rules, ok := routingObj["rules"].([]interface{})
		if !ok || len(rules) == 0 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.lan_rdp",
				Message: "Missing direct bypass for private networks (geoip:private) or remote desktop port (3389)",
			})
			return
		}

		hasPrivateDirect := false
		hasRdpDirect := false

		for _, r := range rules {
			ruleMap, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			outbound := fmt.Sprintf("%v", ruleMap["outboundTag"])
			if strings.EqualFold(outbound, "direct") || strings.EqualFold(outbound, "freedom") {
				if ipList, ok := ruleMap["ip"].([]interface{}); ok {
					for _, ipItem := range ipList {
						if fmt.Sprintf("%v", ipItem) == "geoip:private" {
							hasPrivateDirect = true
						}
					}
				}
				portVal := fmt.Sprintf("%v", ruleMap["port"])
				if portVal == "3389" || strings.Contains(portVal, "3389") {
					hasRdpDirect = true
				}
			}
		}

		if !hasPrivateDirect || !hasRdpDirect {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.lan_rdp",
				Message: "Missing direct bypass for private networks (geoip:private) or remote desktop port (3389)",
			})
		}
	}
}

// -----------------------------------------------------------------------------
// Step 6: Port Conflicts & TProxy/Hybrid Mode
// -----------------------------------------------------------------------------
func validatePortConflicts(kernel string, filename string, data map[string]interface{}, res *PreflightResult) {
	if kernel == "mihomo" {
		portKeys := []string{"port", "socks-port", "redir-port", "tproxy-port", "mixed-port"}
		ports := make(map[string]int)

		for _, k := range portKeys {
			if v, ok := data[k]; ok {
				if p, err := strconv.Atoi(fmt.Sprintf("%v", v)); err == nil && p > 0 {
					ports[k] = p
				}
			}
		}

		if rawExt, ok := data["external-controller"]; ok {
			extStr := fmt.Sprintf("%v", rawExt)
			if _, pStr, err := net.SplitHostPort(extStr); err == nil {
				if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
					ports["external-controller"] = p
				}
			}
		}

		if rawDNS, ok := data["dns"].(map[string]interface{}); ok {
			if rawListen, ok := rawDNS["listen"]; ok {
				listenStr := fmt.Sprintf("%v", rawListen)
				if _, pStr, err := net.SplitHostPort(listenStr); err == nil {
					if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
						ports["dns.listen"] = p
					}
				}
			}
		}

		// 1. Check duplicates among defined ports
		seenPorts := make(map[int]string)
		keys := make([]string, 0, len(ports))
		for k := range ports {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			p := ports[k]
			if existingKey, exists := seenPorts[p]; exists {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.port_conflict",
					Message: fmt.Sprintf("Port conflict: %s and %s both use port %d", existingKey, k, p),
				})
			} else {
				seenPorts[p] = k
			}
		}

		// 2. Check reserved project ports: 5000 (redir), 5001 (tproxy), 1053 (dns)
		for _, k := range keys {
			p := ports[k]
			if p == 5000 && k != "redir-port" {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.port_conflict",
					Message: fmt.Sprintf("Port %d configured in %q is reserved for transparent redir-port", p, k),
				})
			} else if p == 5001 && k != "tproxy-port" {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.port_conflict",
					Message: fmt.Sprintf("Port %d configured in %q is reserved for transparent tproxy-port", p, k),
				})
			} else if p == 1053 && k != "dns.listen" {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.port_conflict",
					Message: fmt.Sprintf("Port %d configured in %q is reserved for router DNS listener", p, k),
				})
			}
		}

		// 3. Information notice about mixed mode
		if ports["redir-port"] > 0 && ports["tproxy-port"] > 0 {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.port_conflict",
				Message: "Mixed TProxy and Redir mode active: both redir-port and tproxy-port are configured",
			})
		}
	} else if kernel == "xray" {
		if !strings.Contains(filename, "inbounds") {
			return
		}
		inbounds, ok := data["inbounds"].([]interface{})
		if !ok || len(inbounds) == 0 {
			return
		}

		seenPorts := make(map[int]string)
		for idx, inb := range inbounds {
			inbMap, ok := inb.(map[string]interface{})
			if !ok {
				continue
			}
			tag := fmt.Sprintf("%v", inbMap["tag"])
			if tag == "" || tag == "<nil>" {
				tag = fmt.Sprintf("inbound[%d]", idx)
			}
			if portRaw, ok := inbMap["port"]; ok {
				if p, err := strconv.Atoi(fmt.Sprintf("%v", portRaw)); err == nil && p > 0 {
					if prevTag, exists := seenPorts[p]; exists {
						res.Warnings = append(res.Warnings, PreflightIssue{
							Code:    "preflight.port_conflict",
							Message: fmt.Sprintf("Port conflict: inbounds %q and %q both use port %d", prevTag, tag, p),
						})
					} else {
						seenPorts[p] = tag
					}
				}
			}
		}
	}
}

// -----------------------------------------------------------------------------
// Step 7: AmneziaWG obfuscation options check (AWGVAL-01..AWGVAL-05)
// -----------------------------------------------------------------------------

type hFieldInfo struct {
	raw string
	min int64
	max int64
}

func parseHField(val interface{}) (hFieldInfo, bool) {
	if val == nil {
		return hFieldInfo{}, false
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", val))
	if s == "" || s == "<nil>" {
		return hFieldInfo{}, false
	}

	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		minVal, err1 := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		maxVal, err2 := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err1 == nil && err2 == nil {
			return hFieldInfo{raw: s, min: minVal, max: maxVal}, true
		}
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return hFieldInfo{raw: s, min: n, max: n}, true
	}
	return hFieldInfo{raw: s, min: 0, max: 0}, false
}

func validateAmneziaWgOptions(data map[string]interface{}, res *PreflightResult) {
	rawProxies, ok := data["proxies"]
	if !ok || rawProxies == nil {
		return
	}
	proxiesList, ok := rawProxies.([]interface{})
	if !ok {
		return
	}

	flatAwgKeys := []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "header-protection-key", "i1", "i2", "i3", "i4", "i5"}

	for _, p := range proxiesList {
		pMap, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		pType := fmt.Sprintf("%v", pMap["type"])
		if !strings.EqualFold(pType, "wireguard") {
			continue
		}
		pName := fmt.Sprintf("%v", pMap["name"])

		// 1. Check for flat fields on root proxy level
		hasFlat := false
		for _, fk := range flatAwgKeys {
			if _, exists := pMap[fk]; exists {
				hasFlat = true
				break
			}
		}
		if hasFlat {
			res.Warnings = append(res.Warnings, PreflightIssue{
				Code:    "preflight.awg_flat_fields",
				Message: fmt.Sprintf("Proxy %q has AmneziaWG parameters at root level instead of nested amnezia-wg-option block", pName),
			})
			continue
		}

		// 2. If amnezia-wg-option is present, check constraints
		if rawAwg, ok := pMap["amnezia-wg-option"]; ok && rawAwg != nil {
			awgMap, ok := rawAwg.(map[string]interface{})
			if !ok {
				continue
			}

			getInt := func(k string) (int, bool) {
				v, exists := awgMap[k]
				if !exists || v == nil {
					return 0, false
				}
				s := strings.TrimSpace(fmt.Sprintf("%v", v))
				if s == "" || s == "<nil>" {
					return 0, false
				}
				n, err := strconv.Atoi(s)
				return n, err == nil
			}

			jmin, hasJmin := getInt("jmin")
			jmax, hasJmax := getInt("jmax")
			s1, hasS1 := getInt("s1")
			s2, hasS2 := getInt("s2")
			s3, hasS3 := getInt("s3")
			s4, hasS4 := getInt("s4")

			// Check constraints
			// 1. Jmin < Jmax
			if hasJmin && hasJmax && jmin >= jmax {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.awg_jmin_jmax",
					Message: fmt.Sprintf("Proxy %q: AmneziaWG jmin (%d) must be strictly less than jmax (%d)", pName, jmin, jmax),
				})
			}

			// 2. S1 + 56 != S2
			if hasS1 && hasS2 && (s1+56) == s2 {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.awg_s1_s2",
					Message: fmt.Sprintf("Proxy %q: AmneziaWG constraint s1 + 56 != s2 violated (s1=%d, s2=%d)", pName, s1, s2),
				})
			}

			// 3. Jmax + 80 > MTU
			mtu := 1280
			if rawMtu, exists := pMap["mtu"]; exists && rawMtu != nil {
				if parsedMtu, err := strconv.Atoi(fmt.Sprintf("%v", rawMtu)); err == nil && parsedMtu > 0 {
					mtu = parsedMtu
				}
			}
			if hasJmax && (jmax+80) > mtu {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.awg_junk_mtu",
					Message: fmt.Sprintf("Proxy %q: AmneziaWG jmax + 80 (%d) exceeds interface MTU (%d), risk of packet fragmentation", pName, jmax+80, mtu),
				})
			}

			// 4. Header-protection-key requires S1..S4 >= 12
			rawHpk := awgMap["header-protection-key"]
			hpkStr := strings.TrimSpace(fmt.Sprintf("%v", rawHpk))
			if rawHpk != nil && hpkStr != "" && hpkStr != "<nil>" {
				var smallS []string
				if hasS1 && s1 < 12 {
					smallS = append(smallS, fmt.Sprintf("s1=%d", s1))
				}
				if hasS2 && s2 < 12 {
					smallS = append(smallS, fmt.Sprintf("s2=%d", s2))
				}
				if hasS3 && s3 < 12 {
					smallS = append(smallS, fmt.Sprintf("s3=%d", s3))
				}
				if hasS4 && s4 < 12 {
					smallS = append(smallS, fmt.Sprintf("s4=%d", s4))
				}
				if len(smallS) > 0 {
					res.Warnings = append(res.Warnings, PreflightIssue{
						Code:    "preflight.awg_s_header_protection",
						Message: fmt.Sprintf("Proxy %q: AmneziaWG header-protection-key requires S parameters >= 12 (violated: %s)", pName, strings.Join(smallS, ", ")),
					})
				}
			}

			// 5. H1-H4 > 4 and uniqueness (supporting both numbers and ranges min-max)
			hKeys := []string{"h1", "h2", "h3", "h4"}
			var parsedH []hFieldInfo
			hasHMinViol := false

			for _, hk := range hKeys {
				if v, exists := awgMap[hk]; exists {
					hInfo, ok := parseHField(v)
					if ok {
						if hInfo.min <= 4 || hInfo.max <= 4 {
							hasHMinViol = true
						}
						parsedH = append(parsedH, hInfo)
					}
				}
			}

			if hasHMinViol {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.awg_h_min",
					Message: fmt.Sprintf("Proxy %q: AmneziaWG parameters h1, h2, h3, h4 must all be > 4", pName),
				})
			}

			// H uniqueness check
			hasHUniqueViol := false
			for i := 0; i < len(parsedH); i++ {
				for j := i + 1; j < len(parsedH); j++ {
					if parsedH[i].raw == parsedH[j].raw || (parsedH[i].min == parsedH[j].min && parsedH[i].max == parsedH[j].max) {
						hasHUniqueViol = true
						break
					}
				}
				if hasHUniqueViol {
					break
				}
			}
			if hasHUniqueViol {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.awg_h_unique",
					Message: fmt.Sprintf("Proxy %q: AmneziaWG parameters h1, h2, h3, h4 must all be unique", pName),
				})
			}

			// 6. CPS <c> token check in I1..I5
			iKeys := []string{"i1", "i2", "i3", "i4", "i5"}
			hasCToken := false
			for _, ik := range iKeys {
				if iv, exists := awgMap[ik]; exists && iv != nil {
					iStr := strings.ToLower(fmt.Sprintf("%v", iv))
					if strings.Contains(iStr, "<c>") {
						hasCToken = true
						break
					}
				}
			}
			if hasCToken {
				res.Warnings = append(res.Warnings, PreflightIssue{
					Code:    "preflight.awg_i_token",
					Message: fmt.Sprintf("Proxy %q: CPS token <c> in I1-I5 is not supported by client", pName),
				})
			}

			// 7. Random trailers informational warning
			if rtVal, exists := awgMap["random-trailers"]; exists && rtVal != nil {
				rtStr := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", rtVal)))
				if rtStr == "true" || rtStr == "yes" || rtStr == "1" {
					res.Warnings = append(res.Warnings, PreflightIssue{
						Code:    "preflight.awg_random_trailers",
						Message: fmt.Sprintf("Proxy %q: AmneziaWG random-trailers enabled; server must support this mode", pName),
					})
				}
			}
		}
	}
}
