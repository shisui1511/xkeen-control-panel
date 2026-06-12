package assets

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestAssetsService_UpdateDefinition(t *testing.T) {
	tempDir := t.TempDir()
	svc := NewService(tempDir)

	// Verify baseline fallback (since no file exists, GetDefinition should return defaultAssets)
	def, err := svc.GetDefinition()
	if err != nil {
		t.Fatalf("expected no error getting definition, got: %v", err)
	}
	if len(def) == 0 {
		t.Fatal("expected non-empty default assets")
	}

	// Update with invalid JSON
	invalidJSON := []byte(`{ "schema_version": `) // invalid syntax
	err = svc.UpdateDefinition(invalidJSON)
	if err == nil {
		t.Error("expected error when updating with invalid JSON, got nil")
	}

	// Update with missing schema_version
	noVersionJSON := []byte(`{ "something_else": "1.0.0" }`)
	err = svc.UpdateDefinition(noVersionJSON)
	if err == nil {
		t.Error("expected error when updating with missing schema_version, got nil")
	}

	// Update with valid JSON
	validJSON := []byte(`{ "schema_version": "2.0.0", "custom": true }`)
	err = svc.UpdateDefinition(validJSON)
	if err != nil {
		t.Fatalf("expected no error updating with valid JSON, got: %v", err)
	}

	// Get again and check content
	def, err = svc.GetDefinition()
	if err != nil {
		t.Fatalf("expected no error getting updated definition, got: %v", err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(def, &schema); err != nil {
		t.Fatalf("failed to unmarshal updated definition: %v", err)
	}
	if schema["schema_version"] != "2.0.0" {
		t.Errorf("expected schema_version '2.0.0', got '%v'", schema["schema_version"])
	}

	// Now write another valid one to trigger backup test
	newValidJSON := []byte(`{ "schema_version": "3.0.0", "custom": true }`)
	err = svc.UpdateDefinition(newValidJSON)
	if err != nil {
		t.Fatalf("expected no error updating with new valid JSON, got: %v", err)
	}

	// Check if backup exists and has the correct previous version (2.0.0)
	bakPath := filepath.Join(tempDir, "assets-definition.json.bak")
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("expected backup file to exist, got error: %v", err)
	}
	var bakSchema map[string]interface{}
	if err := json.Unmarshal(bakData, &bakSchema); err != nil {
		t.Fatalf("failed to unmarshal backup: %v", err)
	}
	if bakSchema["schema_version"] != "2.0.0" {
		t.Errorf("expected backup schema_version '2.0.0', got '%v'", bakSchema["schema_version"])
	}
}
