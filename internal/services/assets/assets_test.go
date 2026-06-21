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

	// Update with valid JSON (compatible version)
	validJSON := []byte(`{ "schema_version": "1.0.1", "custom": true }`)
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
	if schema["schema_version"] != "1.0.1" {
		t.Errorf("expected schema_version '1.0.1', got '%v'", schema["schema_version"])
	}

	// Now write another valid one to trigger backup test (compatible version)
	newValidJSON := []byte(`{ "schema_version": "1.1.0", "custom": true }`)
	err = svc.UpdateDefinition(newValidJSON)
	if err != nil {
		t.Fatalf("expected no error updating with new valid JSON, got: %v", err)
	}

	// Check if backup exists and has the correct previous version (1.0.1)
	bakPath := filepath.Join(tempDir, "assets-definition.json.bak")
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("expected backup file to exist, got error: %v", err)
	}
	var bakSchema map[string]interface{}
	if err := json.Unmarshal(bakData, &bakSchema); err != nil {
		t.Fatalf("failed to unmarshal backup: %v", err)
	}
	if bakSchema["schema_version"] != "1.0.1" {
		t.Errorf("expected backup schema_version '1.0.1', got '%v'", bakSchema["schema_version"])
	}
}

func TestAssetsService_CheckCompatibility(t *testing.T) {
	svc := NewService(t.TempDir())

	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "Equal major version",
			json:    `{"schema_version": "1.0.0"}`,
			wantErr: false,
		},
		{
			name:    "Higher minor version",
			json:    `{"schema_version": "1.2.3"}`,
			wantErr: false,
		},
		{
			name:    "Higher major version",
			json:    `{"schema_version": "2.0.0"}`,
			wantErr: true,
		},
		{
			name:    "Lower major version with 'v' prefix",
			json:    `{"schema_version": "v1.0.0"}`,
			wantErr: false,
		},
		{
			name:    "Higher major version with 'v' prefix",
			json:    `{"schema_version": "v2.0.0"}`,
			wantErr: true,
		},
		{
			name:    "Lower major version with 'V' prefix",
			json:    `{"schema_version": "V1.0.0"}`,
			wantErr: false,
		},
		{
			name:    "Missing version",
			json:    `{"something": "else"}`,
			wantErr: true,
		},
		{
			name:    "Invalid JSON",
			json:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.CheckCompatibility([]byte(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckCompatibility() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAssetsService_UpdateDefinition_BackupRollback(t *testing.T) {
	tempDir := t.TempDir()
	svc := NewService(tempDir)

	// Write initial valid version
	initialJSON := []byte(`{"schema_version": "1.0.0", "val": "A"}`)
	err := svc.UpdateDefinition(initialJSON)
	if err != nil {
		t.Fatalf("failed to write initial: %v", err)
	}

	// Update with invalid JSON
	err = svc.UpdateDefinition([]byte(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}

	// Verify original content A is preserved
	def, err := svc.GetDefinition()
	if err != nil {
		t.Fatalf("failed to get definition: %v", err)
	}
	var s map[string]interface{}
	json.Unmarshal(def, &s)
	if s["val"] != "A" {
		t.Errorf("expected val to be 'A', got '%v'", s["val"])
	}

	// Update with incompatible version
	err = svc.UpdateDefinition([]byte(`{"schema_version": "2.0.0", "val": "B"}`))
	if err == nil {
		t.Error("expected error for incompatible version, got nil")
	}

	// Verify original content A is preserved
	def, err = svc.GetDefinition()
	if err != nil {
		t.Fatalf("failed to get definition: %v", err)
	}
	json.Unmarshal(def, &s)
	if s["val"] != "A" {
		t.Errorf("expected val to be 'A', got '%v'", s["val"])
	}
}

func TestAssetsService_AutoMigration(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Write an outdated mock assets file to disk (missing youtube@domain and having an outdated url for telegram@ipcidr)
	diskSchema := map[string]interface{}{
		"schema_version": "1.0.0",
		"mihomo": map[string]interface{}{
			"presets": []interface{}{},
			"rule_providers": []interface{}{
				map[string]interface{}{
					"name": "telegram@ipcidr",
					"url":  "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geoip/telegram.mrs",
				},
				map[string]interface{}{
					"name": "custom@domain",
					"url":  "https://example.com/custom.mrs",
				},
			},
		},
	}
	diskData, err := json.Marshal(diskSchema)
	if err != nil {
		t.Fatalf("failed to marshal disk schema: %v", err)
	}

	diskPath := filepath.Join(tempDir, "assets-definition.json")
	if err := os.WriteFile(diskPath, diskData, 0600); err != nil {
		t.Fatalf("failed to write disk schema: %v", err)
	}

	// 2. Initialize service and trigger GetDefinition
	svc := NewService(tempDir)
	def, err := svc.GetDefinition()
	if err != nil {
		t.Fatalf("failed to get definition: %v", err)
	}

	// 3. Verify that:
	// - youtube@domain (missing from disk but present in embedded defaultAssets) was added.
	// - telegram@ipcidr URL was updated to the embedded version.
	// - custom@domain (not in embedded defaultAssets but present on disk) was preserved.
	var migrated map[string]interface{}
	if err := json.Unmarshal(def, &migrated); err != nil {
		t.Fatalf("failed to unmarshal migrated schema: %v", err)
	}

	mihomo, ok := migrated["mihomo"].(map[string]interface{})
	if !ok {
		t.Fatal("mihomo section missing in migrated schema")
	}
	providers, ok := mihomo["rule_providers"].([]interface{})
	if !ok {
		t.Fatal("rule_providers missing in migrated schema")
	}

	foundYoutube := false
	foundTelegramUpdated := false
	foundCustom := false

	for _, p := range providers {
		pMap, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		name := pMap["name"].(string)
		url := pMap["url"].(string)

		if name == "youtube@domain" {
			foundYoutube = true
		}
		if name == "telegram@ipcidr" {
			// Embedded url: "https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/telegram@ipcidr.mrs"
			if url == "https://github.com/zxc-rv/zkeenip-rulesets/releases/latest/download/telegram@ipcidr.mrs" {
				foundTelegramUpdated = true
			}
		}
		if name == "custom@domain" {
			foundCustom = true
		}
	}

	if !foundYoutube {
		t.Error("youtube@domain was not added during migration")
	}
	if !foundTelegramUpdated {
		t.Error("telegram@ipcidr URL was not updated to embedded version")
	}
	if !foundCustom {
		t.Error("custom@domain was not preserved during migration")
	}
}
