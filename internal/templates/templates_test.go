package templates_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestZkeenYamlStructure(t *testing.T) {
	data, err := os.ReadFile("mihomo/zkeen.yaml")
	if err != nil {
		t.Fatalf("Failed to read zkeen.yaml: %v", err)
	}

	rawText := string(data)
	if strings.Contains(rawText, "external-ui") {
		t.Error("zkeen.yaml raw text must not contain external-ui")
	}

	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse zkeen.yaml as YAML: %v", err)
	}

	// Assert top-level fields
	redirPort, ok := parsed["redir-port"].(int)
	if !ok || redirPort != 5000 {
		t.Errorf("Expected redir-port: 5000, got: %v", parsed["redir-port"])
	}

	tproxyPort, ok := parsed["tproxy-port"].(int)
	if !ok || tproxyPort != 5001 {
		t.Errorf("Expected tproxy-port: 5001, got: %v", parsed["tproxy-port"])
	}

	routingMark, ok := parsed["routing-mark"].(int)
	if !ok || routingMark != 255 {
		t.Errorf("Expected routing-mark: 255, got: %v", parsed["routing-mark"])
	}

	sniffer, ok := parsed["sniffer"].(map[string]interface{})
	if !ok {
		t.Error("Missing sniffer block or not a map")
	} else {
		enable, _ := sniffer["enable"].(bool)
		if !enable {
			t.Error("Expected sniffer.enable: true")
		}
	}
}

func TestXrayTemplatesStructure(t *testing.T) {
	// 1. zkeen-routing.json
	routingData, err := os.ReadFile("xray/zkeen-routing.json")
	if err != nil {
		t.Fatalf("Failed to read zkeen-routing.json: %v", err)
	}

	rawRouting := string(routingData)
	if !strings.Contains(rawRouting, "ext:zkeen.dat") {
		t.Error("zkeen-routing.json must contain literal ext:zkeen.dat")
	}

	var routingParsed map[string]interface{}
	if err := json.Unmarshal(routingData, &routingParsed); err != nil {
		t.Fatalf("Failed to parse zkeen-routing.json as JSON: %v", err)
	}

	routing, ok := routingParsed["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("zkeen-routing.json: missing routing object")
	}
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		t.Fatal("zkeen-routing.json: missing routing.rules array")
	}

	foundTelegram := false
	for _, ruleVal := range rules {
		rule, ok := ruleVal.(map[string]interface{})
		if !ok {
			continue
		}
		ips, _ := rule["ip"].([]interface{})
		hasTelegram := false
		for _, ipVal := range ips {
			if ipStr, ok := ipVal.(string); ok && ipStr == "geoip:telegram" {
				hasTelegram = true
				break
			}
		}
		if hasTelegram {
			foundTelegram = true
			ports, _ := rule["port"].(string)
			if ports != "80,443,596-599,1400,5222" {
				t.Errorf("Expected Telegram port list '80,443,596-599,1400,5222', got '%s'", ports)
			}
		}
	}
	if !foundTelegram {
		t.Error("zkeen-routing.json: routing rule for geoip:telegram not found")
	}

	// 2. observatory.json
	obsData, err := os.ReadFile("xray/observatory.json")
	if err != nil {
		t.Fatalf("Failed to read observatory.json: %v", err)
	}

	var obsParsed map[string]interface{}
	if err := json.Unmarshal(obsData, &obsParsed); err != nil {
		t.Fatalf("Failed to parse observatory.json as JSON: %v", err)
	}

	obs, ok := obsParsed["observatory"].(map[string]interface{})
	if !ok {
		t.Error("observatory.json: missing observatory object")
	} else {
		subSel, _ := obs["subjectSelector"].([]interface{})
		if len(subSel) == 0 {
			t.Error("observatory.json: subjectSelector must be non-empty")
		}
	}

	routingObj, ok := obsParsed["routing"].(map[string]interface{})
	if !ok {
		t.Fatal("observatory.json: missing routing object")
	}
	balancers, ok := routingObj["balancers"].([]interface{})
	if !ok || len(balancers) == 0 {
		t.Fatal("observatory.json: missing routing.balancers array")
	}

	firstBalancer, ok := balancers[0].(map[string]interface{})
	if !ok {
		t.Fatal("observatory.json: balancer is not a map")
	}
	strategy, ok := firstBalancer["strategy"].(map[string]interface{})
	if !ok {
		t.Fatal("observatory.json: balancer missing strategy")
	}
	strategyType, _ := strategy["type"].(string)
	if strategyType != "leastPing" {
		t.Errorf("Expected strategy.type leastPing, got '%s'", strategyType)
	}

	obsRules, ok := routingObj["rules"].([]interface{})
	if !ok {
		t.Fatal("observatory.json: missing routing.rules array")
	}

	foundCatchAll := false
	for _, ruleVal := range obsRules {
		rule, ok := ruleVal.(map[string]interface{})
		if !ok {
			continue
		}
		inboundTags, _ := rule["inboundTag"].([]interface{})
		hasRedirectTproxy := false
		for _, tagVal := range inboundTags {
			tagStr, _ := tagVal.(string)
			if tagStr == "redirect" || tagStr == "tproxy" {
				hasRedirectTproxy = true
			}
		}
		if hasRedirectTproxy {
			foundCatchAll = true
			if _, ok := rule["outboundTag"]; ok {
				t.Error("observatory.json: catch-all inbound rule must use balancerTag only, not outboundTag")
			}
			if _, ok := rule["balancerTag"]; !ok {
				t.Error("observatory.json: catch-all inbound rule must have balancerTag")
			}
		}
	}
	if !foundCatchAll {
		t.Error("observatory.json: catch-all rule with inboundTag redirect/tproxy not found")
	}
}
