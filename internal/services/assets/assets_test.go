package assets

import (
	"encoding/json"
	"testing"
)

func TestDefaultAssetsStructure(t *testing.T) {
	if len(defaultAssets) == 0 {
		t.Fatal("Embedded defaultAssets is empty")
	}

	var schema map[string]interface{}
	err := json.Unmarshal(defaultAssets, &schema)
	if err != nil {
		t.Fatalf("Failed to parse embedded defaultAssets as JSON: %v", err)
	}

	version, ok := schema["schema_version"].(string)
	if !ok {
		t.Error("Missing schema_version key or it is not a string")
	} else if version != "1.0.0" {
		t.Errorf("Expected schema_version '1.0.0', got '%s'", version)
	}

	xraySection, ok := schema["xray"].(map[string]interface{})
	if !ok {
		t.Error("Missing xray key or it is not an object")
	} else {
		presets, ok := xraySection["presets"].([]interface{})
		if !ok || len(presets) == 0 {
			t.Error("Missing presets in xray section or it is empty")
		}
	}

	mihomoSection, ok := schema["mihomo"].(map[string]interface{})
	if !ok {
		t.Error("Missing mihomo key or it is not an object")
	} else {
		presets, ok := mihomoSection["presets"].([]interface{})
		if !ok || len(presets) == 0 {
			t.Error("Missing presets in mihomo section or it is empty")
		} else {
			// Find zkeen-selective preset
			var zkeenSelective map[string]interface{}
			for _, p := range presets {
				presetObj, ok := p.(map[string]interface{})
				if ok && presetObj["id"] == "zkeen-selective" {
					zkeenSelective = presetObj
					break
				}
			}
			if zkeenSelective == nil {
				t.Error("zkeen-selective preset not found")
			} else {
				groups, ok := zkeenSelective["groups"].([]interface{})
				if !ok || len(groups) == 0 {
					t.Error("zkeen-selective missing groups or empty")
				} else {
					hasFallback := false
					hasFastest := false
					for _, g := range groups {
						groupObj, ok := g.(map[string]interface{})
						if !ok {
							continue
						}
						name := groupObj["name"]
						hidden := groupObj["hidden"]
						groupType := groupObj["type"]

						if name == "Fallback" {
							hasFallback = true
							if hidden != true {
								t.Error("Fallback group must be hidden: true")
							}
							if groupType != "fallback" {
								t.Errorf("Expected Fallback type: fallback, got %v", groupType)
							}
						}
						if name == "Fastest" {
							hasFastest = true
							if hidden != true {
								t.Error("Fastest group must be hidden: true")
							}
							if groupType != "url-test" {
								t.Errorf("Expected Fastest type: url-test, got %v", groupType)
							}
						}
					}
					if !hasFallback {
						t.Error("zkeen-selective missing Fallback group")
					}
					if !hasFastest {
						t.Error("zkeen-selective missing Fastest group")
					}
				}
			}
		}
	}
}
