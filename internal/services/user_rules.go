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
	Type    string `json:"type"`    // "domain", "domain_suffix", "domain_keyword", "ip_cidr", "port"
	Value   string `json:"value"`   // e.g. "example.com", "192.168.1.50", "8080"
	Target  string `json:"target"`  // "direct", "proxy", "reject"
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
			target = proxyGroupName
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
			target = proxyTag
		}

		val := strings.TrimSpace(r.Value)
		switch r.Type {
		case "domain":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"full:" + val},
			})
		case "domain_suffix", "suffix":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"domain:" + val},
			})
		case "domain_keyword", "keyword":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{"keyword:" + val},
			})
		case "ip_cidr", "ip":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"ip":          []string{val},
			})
		case "port":
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"port":        val,
			})
		default:
			rules = append(rules, map[string]interface{}{
				"type":        "field",
				"outboundTag": target,
				"domain":      []string{val},
			})
		}
	}

	return rules
}
