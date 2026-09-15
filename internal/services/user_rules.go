package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// UserRule represents a custom user-defined routing rule preserved across template switches.
type UserRule struct {
	ID      string `json:"id"`
	Type    string `json:"type"`   // "domain", "domain_suffix", "domain_keyword", "ip_cidr", "port"
	Value   string `json:"value"`  // e.g. "example.com", "192.168.1.50", "8080"
	Target  string `json:"target"` // "direct", "proxy", "reject"
	Group   string `json:"group,omitempty"`
	Comment string `json:"comment,omitempty"`
	Enabled bool   `json:"enabled"`
}

// UserRulesService manages persistence and conversion of user custom rules.
type UserRulesService struct {
	mu       sync.RWMutex
	filePath string
	rules    []UserRule
}

// NewUserRulesService initializes the UserRulesService with a JSON storage path.
func NewUserRulesService(dataDir string) *UserRulesService {
	filePath := filepath.Join(dataDir, "user_rules.json")
	s := &UserRulesService{
		filePath: filePath,
		rules:    make([]UserRule, 0),
	}
	s.load()
	return s
}

func (s *UserRulesService) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var loaded []UserRule
	if err := json.Unmarshal(data, &loaded); err == nil {
		s.rules = loaded
	}
}

// List returns a copy of all user custom rules.
func (s *UserRulesService) List() []UserRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]UserRule, len(s.rules))
	copy(res, s.rules)
	return res
}

// Save validates and persists user custom rules to disk.
func (s *UserRulesService) Save(rules []UserRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, r := range rules {
		if r.ID == "" {
			rules[i].ID = fmt.Sprintf("rule_%d", i+1)
		}
		if r.Target == "" {
			rules[i].Target = "direct"
		}
	}

	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpFile, s.filePath); err != nil {
		return err
	}

	s.rules = rules
	return nil
}

// ToMihomoRules converts enabled user rules to Mihomo YAML rule strings.
func (s *UserRulesService) ToMihomoRules(proxyGroupName string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if proxyGroupName == "" {
		proxyGroupName = "PROXY"
	}

	var res []string
	for _, r := range s.rules {
		if !r.Enabled || strings.TrimSpace(r.Value) == "" {
			continue
		}

		target := strings.ToUpper(r.Target)
		if target == "PROXY" {
			if r.Group != "" {
				target = r.Group
			} else {
				target = proxyGroupName
			}
		}

		val := strings.TrimSpace(r.Value)
		switch r.Type {
		case "domain":
			res = append(res, fmt.Sprintf("DOMAIN,%s,%s", val, target))
		case "domain_suffix", "suffix":
			res = append(res, fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", val, target))
		case "domain_keyword", "keyword":
			res = append(res, fmt.Sprintf("DOMAIN-KEYWORD,%s,%s", val, target))
		case "ip_cidr", "ip":
			res = append(res, fmt.Sprintf("IP-CIDR,%s,%s,no-resolve", val, target))
		case "port":
			res = append(res, fmt.Sprintf("DST-PORT,%s,%s", val, target))
		default:
			// Fallback: if value has dot and no slash, treat as suffix
			if strings.Contains(val, "/") {
				res = append(res, fmt.Sprintf("IP-CIDR,%s,%s,no-resolve", val, target))
			} else {
				res = append(res, fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", val, target))
			}
		}
	}

	return res
}

// ToXrayRules converts enabled user rules to Xray routing rule objects.
func (s *UserRulesService) ToXrayRules(proxyTag string) []map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if proxyTag == "" {
		proxyTag = "proxy"
	}

	var rules []map[string]interface{}
	for _, r := range s.rules {
		if !r.Enabled || strings.TrimSpace(r.Value) == "" {
			continue
		}

		target := strings.ToLower(r.Target)
		if target == "proxy" {
			if r.Group != "" {
				target = r.Group
			} else {
				target = proxyTag
			}
		}

		val := strings.TrimSpace(r.Value)
		switch r.Type {
		case "domain":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"full:" + val},
				"user_rule":   true,
			})
		case "domain_suffix", "suffix":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"domain:" + val},
				"user_rule":   true,
			})
		case "domain_keyword", "keyword":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"keyword:" + val},
				"user_rule":   true,
			})
		case "ip_cidr", "ip":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"ip":          []string{val},
				"user_rule":   true,
			})
		case "port":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"port":        val,
				"user_rule":   true,
			})
		default:
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{val},
				"user_rule":   true,
			})
		}
	}

	return rules
}

const (
	// UserRulesBeginMarker marks the beginning of user-defined rules in config.yaml.
	UserRulesBeginMarker = "# --- BEGIN USER RULES ---"
	// UserRulesEndMarker marks the end of user-defined rules in config.yaml.
	UserRulesEndMarker = "# --- END USER RULES ---"
)

// InjectMihomoRules replaces or inserts the user rules block within the rules section of config.yaml.
func (s *UserRulesService) InjectMihomoRules(configPath string, proxyGroupName string) error {
	if configPath == "" {
		return fmt.Errorf("config path cannot be empty")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	lines := strings.Split(string(data), "\n")
	newRules := s.ToMihomoRules(proxyGroupName)

	beginIdx := -1
	endIdx := -1
	markerIndent := "  "

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, UserRulesBeginMarker) {
			beginIdx = i
			leadingSpaces := len(line) - len(strings.TrimLeft(line, " \t"))
			if leadingSpaces > 0 {
				markerIndent = line[:leadingSpaces]
			}
		} else if strings.HasPrefix(trimmed, UserRulesEndMarker) && beginIdx != -1 {
			endIdx = i
			break
		}
	}

	ruleLines := make([]string, 0, len(newRules)+2)
	ruleLines = append(ruleLines, markerIndent+UserRulesBeginMarker)
	for _, r := range newRules {
		ruleLines = append(ruleLines, fmt.Sprintf("%s- %s", markerIndent, r))
	}
	ruleLines = append(ruleLines, markerIndent+UserRulesEndMarker)

	var resultLines []string
	if beginIdx != -1 && endIdx != -1 && endIdx >= beginIdx {
		resultLines = append(resultLines, lines[:beginIdx]...)
		resultLines = append(resultLines, ruleLines...)
		if endIdx+1 < len(lines) {
			resultLines = append(resultLines, lines[endIdx+1:]...)
		}
	} else {
		rulesStart, _, baseIndent := findTopLevelSection(lines, "rules")
		if baseIndent > 0 {
			markerIndent = strings.Repeat(" ", baseIndent)
		}
		ruleLines = make([]string, 0, len(newRules)+2)
		ruleLines = append(ruleLines, markerIndent+UserRulesBeginMarker)
		for _, r := range newRules {
			ruleLines = append(ruleLines, fmt.Sprintf("%s- %s", markerIndent, r))
		}
		ruleLines = append(ruleLines, markerIndent+UserRulesEndMarker)

		if rulesStart != -1 {
			resultLines = append(resultLines, lines[:rulesStart+1]...)
			resultLines = append(resultLines, ruleLines...)
			if rulesStart+1 < len(lines) {
				resultLines = append(resultLines, lines[rulesStart+1:]...)
			}
		} else {
			resultLines = append(resultLines, lines...)
			if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
				resultLines = append(resultLines, "")
			}
			resultLines = append(resultLines, "rules:")
			resultLines = append(resultLines, ruleLines...)
		}
	}

	outContent := strings.Join(resultLines, "\n")
	tmpFile := configPath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(outContent), 0644); err != nil {
		return fmt.Errorf("failed to write tmp config file: %w", err)
	}
	if err := os.Rename(tmpFile, configPath); err != nil {
		return fmt.Errorf("failed to rename tmp config file: %w", err)
	}

	return nil
}

// InjectXrayRules updates user rules in the Xray routing configuration file (05_routing.json).
func (s *UserRulesService) InjectXrayRules(routingPath string, activeOutbound string) error {
	if routingPath == "" {
		return fmt.Errorf("routing path cannot be empty")
	}

	data, err := os.ReadFile(routingPath)
	if err != nil {
		return fmt.Errorf("failed to read routing file %s: %w", routingPath, err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("failed to unmarshal routing json: %w", err)
	}
	if root == nil {
		root = make(map[string]interface{})
	}

	var targetMap map[string]interface{}
	if r, ok := root["routing"].(map[string]interface{}); ok && r != nil {
		targetMap = r
	} else if _, hasRules := root["rules"]; hasRules {
		targetMap = root
	} else {
		targetMap = map[string]interface{}{}
		root["routing"] = targetMap
	}

	var existingRules []interface{}
	if rs, ok := targetMap["rules"].([]interface{}); ok {
		existingRules = rs
	}

	var cleanRules []interface{}
	for _, r := range existingRules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			cleanRules = append(cleanRules, r)
			continue
		}
		if isUserRule, ok := rm["user_rule"].(bool); ok && isUserRule {
			continue
		}
		cleanRules = append(cleanRules, r)
	}

	newUserRules := s.ToXrayRules(activeOutbound)
	var finalRules []interface{}

	insertIdx := 0
	for i, r := range cleanRules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			break
		}
		isSafety := false
		if outTag, _ := rm["outboundTag"].(string); outTag == "api" {
			isSafety = true
		} else if inbTags, ok := rm["inboundTag"].([]interface{}); ok {
			for _, tag := range inbTags {
				if tag == "api" {
					isSafety = true
					break
				}
			}
		}
		if !isSafety {
			if ips, ok := rm["ip"].([]interface{}); ok {
				for _, ip := range ips {
					if ip == "127.0.0.53" || ip == "127.0.0.1" {
						isSafety = true
						break
					}
				}
			}
		}
		if isSafety {
			insertIdx = i + 1
		} else {
			break
		}
	}

	finalRules = append(finalRules, cleanRules[:insertIdx]...)
	for _, nur := range newUserRules {
		finalRules = append(finalRules, nur)
	}
	finalRules = append(finalRules, cleanRules[insertIdx:]...)

	targetMap["rules"] = finalRules

	outData, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal routing json: %w", err)
	}

	tmpFile := routingPath + ".tmp"
	if err := os.WriteFile(tmpFile, outData, 0644); err != nil {
		return fmt.Errorf("failed to write tmp routing file: %w", err)
	}
	if err := os.Rename(tmpFile, routingPath); err != nil {
		return fmt.Errorf("failed to rename tmp routing file: %w", err)
	}

	return nil
}
