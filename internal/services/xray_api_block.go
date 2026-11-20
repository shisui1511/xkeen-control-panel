package services

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// CheckPortAvailable tests whether the given TCP port can be bound on loopback.
func CheckPortAvailable(port int) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use (%w). Known busy ports: 10808/10809 (XKeen), 9090 (Mihomo API), 8088/8090/8091 (Web UI)", port, err)
	}
	_ = ln.Close()
	return nil
}

// ProvisionXrayAPIBlock adds or updates the Xray gRPC api inbound, stats/policy counters,
// and the mandatory routing rule (as rule 0) in the given JSON configuration.
// It is idempotent: repeated calls do not duplicate the api inbound or rule.
func ProvisionXrayAPIBlock(existingContent string, apiPort int) (string, error) {
	if apiPort <= 0 || apiPort > 65535 {
		return existingContent, fmt.Errorf("invalid api port: %d", apiPort)
	}

	// 1. Parse existing JSON
	var root map[string]interface{}
	trimmed := strings.TrimSpace(existingContent)
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
			return "", fmt.Errorf("invalid existing Xray JSON: %w", err)
		}
	}
	if root == nil {
		root = make(map[string]interface{})
	}

	// 2. Check if API inbound is already configured on this port
	alreadyConfigured := false
	if inbs, ok := root["inbounds"].([]interface{}); ok {
		for _, inb := range inbs {
			if m, ok := inb.(map[string]interface{}); ok {
				if tag, _ := m["tag"].(string); tag == "api" {
					switch p := m["port"].(type) {
					case float64:
						if int(p) == apiPort {
							alreadyConfigured = true
						}
					case int:
						if p == apiPort {
							alreadyConfigured = true
						}
					case string:
						if pInt, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && pInt == apiPort {
							alreadyConfigured = true
						}
					}
				}
			}
		}
	}

	// 3. Port availability test only if port is not already configured
	if !alreadyConfigured {
		if err := CheckPortAvailable(apiPort); err != nil {
			return existingContent, err
		}
	}

	// 3. Ensure "stats": {}
	if _, ok := root["stats"]; !ok {
		root["stats"] = map[string]interface{}{}
	}

	// 4. Set "api" object with tag "api" and services
	root["api"] = map[string]interface{}{
		"tag": "api",
		"services": []string{
			"StatsService",
			"RoutingService",
			"LoggerService",
		},
	}

	// 5. Ensure "policy.system" with stats flags
	var policyMap map[string]interface{}
	if p, ok := root["policy"].(map[string]interface{}); ok && p != nil {
		policyMap = p
	} else {
		policyMap = make(map[string]interface{})
		root["policy"] = policyMap
	}
	var systemMap map[string]interface{}
	if s, ok := policyMap["system"].(map[string]interface{}); ok && s != nil {
		systemMap = s
	} else {
		systemMap = make(map[string]interface{})
		policyMap["system"] = systemMap
	}
	systemMap["statsInboundUplink"] = true
	systemMap["statsInboundDownlink"] = true
	systemMap["statsOutboundUplink"] = true
	systemMap["statsOutboundDownlink"] = true

	// 6. Inbound for dokodemo-door on 127.0.0.1:apiPort
	apiInbound := map[string]interface{}{
		"tag":      "api",
		"listen":   "127.0.0.1",
		"port":     apiPort,
		"protocol": "dokodemo-door",
		"settings": map[string]interface{}{
			"address": "127.0.0.1",
		},
	}

	var inbounds []interface{}
	if inbs, ok := root["inbounds"].([]interface{}); ok {
		inbounds = inbs
	}
	replacedInb := false
	for i, inb := range inbounds {
		if m, ok := inb.(map[string]interface{}); ok {
			if tag, ok := m["tag"].(string); ok && tag == "api" {
				inbounds[i] = apiInbound
				replacedInb = true
				break
			}
		}
	}
	if !replacedInb {
		inbounds = append(inbounds, apiInbound)
	}
	root["inbounds"] = inbounds

	// 7. Routing rule: inboundTag ["api"] -> outboundTag "api", MUST be rule 0
	apiRule := map[string]interface{}{
		"type":        "field",
		"inboundTag":  []string{"api"},
		"outboundTag": "api",
	}

	var routingMap map[string]interface{}
	if r, ok := root["routing"].(map[string]interface{}); ok && r != nil {
		routingMap = r
	} else {
		routingMap = make(map[string]interface{})
		root["routing"] = routingMap
	}

	var rules []interface{}
	if rs, ok := routingMap["rules"].([]interface{}); ok {
		rules = rs
	}

	// Filter out any existing rule that routes api inbound
	var filteredRules []interface{}
	for _, r := range rules {
		if rm, ok := r.(map[string]interface{}); ok {
			isAPIRule := false
			if inbTags, ok := rm["inboundTag"].([]interface{}); ok {
				for _, t := range inbTags {
					if s, ok := t.(string); ok && s == "api" {
						isAPIRule = true
						break
					}
				}
			} else if inbTag, ok := rm["inboundTag"].(string); ok && inbTag == "api" {
				isAPIRule = true
			}
			if isAPIRule {
				continue
			}
		}
		filteredRules = append(filteredRules, r)
	}

	newRules := make([]interface{}, 0, len(filteredRules)+1)
	newRules = append(newRules, apiRule)
	newRules = append(newRules, filteredRules...)
	routingMap["rules"] = newRules

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal Xray config with api block: %w", err)
	}
	return string(out), nil
}

// DeprovisionXrayAPIBlock removes the api inbound and routing rule from Xray JSON config.
// The stats and policy blocks are preserved for future use.
func DeprovisionXrayAPIBlock(existingContent string) (string, error) {
	trimmed := strings.TrimSpace(existingContent)
	if trimmed == "" {
		return existingContent, nil
	}

	var root map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return "", fmt.Errorf("invalid existing Xray JSON: %w", err)
	}
	if root == nil {
		return existingContent, nil
	}

	// 1. Remove api inbound
	// Пустые срезы, а не nil: иначе ключи сериализуются как null.
	if inbs, ok := root["inbounds"].([]interface{}); ok {
		newInbounds := make([]interface{}, 0, len(inbs))
		for _, inb := range inbs {
			if m, ok := inb.(map[string]interface{}); ok {
				if tag, ok := m["tag"].(string); ok && tag == "api" {
					continue
				}
			}
			newInbounds = append(newInbounds, inb)
		}
		root["inbounds"] = newInbounds
	}

	// 2. Remove api routing rule
	if r, ok := root["routing"].(map[string]interface{}); ok && r != nil {
		if rs, ok := r["rules"].([]interface{}); ok {
			newRules := make([]interface{}, 0, len(rs))
			for _, rule := range rs {
				if rm, ok := rule.(map[string]interface{}); ok {
					isAPIRule := false
					if inbTags, ok := rm["inboundTag"].([]interface{}); ok {
						for _, t := range inbTags {
							if s, ok := t.(string); ok && s == "api" {
								isAPIRule = true
								break
							}
						}
					} else if inbTag, ok := rm["inboundTag"].(string); ok && inbTag == "api" {
						isAPIRule = true
					}
					if isAPIRule && rm["outboundTag"] == "api" {
						continue
					}
				}
				newRules = append(newRules, rule)
			}
			r["rules"] = newRules
		}
	}

	// 3. Remove api object.
	// Удаляется любой объект api, не только с тегом "api": FindXrayAPIFragment считает API включённым по наличию объекта, и
	// оставленный объект не дал бы выключить мониторинг.
	delete(root, "api")

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal deprovisioned Xray config: %w", err)
	}
	return string(out), nil
}
