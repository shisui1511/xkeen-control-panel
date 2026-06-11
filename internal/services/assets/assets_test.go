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
		}
	}
}
