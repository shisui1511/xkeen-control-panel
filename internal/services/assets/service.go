package assets

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

//go:embed default_assets.json
var defaultAssets []byte

type AssetsService struct {
	mu      sync.RWMutex
	dataDir string
}

func NewService(dataDir string) *AssetsService {
	return &AssetsService{
		dataDir: dataDir,
	}
}

// GetDefinition loads assets schema from disk (assets-definition.json in dataDir).
// If missing or syntax error, it falls back to defaultAssets.
// Additionally, it automatically migrates the disk version to ensure all rule-providers
// from the embedded schema are synchronized and up-to-date.
func (s *AssetsService) GetDefinition() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	diskPath := filepath.Join(s.dataDir, "assets-definition.json")
	if _, err := os.Stat(diskPath); err == nil {
		data, err := os.ReadFile(diskPath)
		if err == nil {
			var diskSchema map[string]interface{}
			if err := json.Unmarshal(data, &diskSchema); err == nil {
				latestData := data
				var anyModified bool

				if updated, err := s.migrateRuleProvidersIfNeeded(diskSchema); err == nil && updated != nil {
					latestData = updated
					anyModified = true
					// Re-parse for next migration step
					json.Unmarshal(updated, &diskSchema) //nolint:errcheck
				}
				if updated, err := s.migratePresetsIfNeeded(diskSchema); err == nil && updated != nil {
					latestData = updated
					anyModified = true
				}

				if anyModified {
					if writeErr := utils.AtomicWriteFile(diskPath, latestData, 0600); writeErr == nil {
						return latestData, nil
					}
				}
				return data, nil
			}
			log.Printf("WARNING: assets-definition.json on disk has invalid syntax: %v. Falling back to embedded schema.", err)
		}
	}

	if len(defaultAssets) == 0 {
		return []byte("{}"), nil
	}

	return defaultAssets, nil
}

func (s *AssetsService) migrateRuleProvidersIfNeeded(diskSchema map[string]interface{}) ([]byte, error) {
	var defaultSchema map[string]interface{}
	if err := json.Unmarshal(defaultAssets, &defaultSchema); err != nil {
		return nil, err
	}

	embeddedMihomo, ok := defaultSchema["mihomo"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	embeddedProviders, ok := embeddedMihomo["rule_providers"].([]interface{})
	if !ok {
		return nil, nil
	}

	diskMihomo, ok := diskSchema["mihomo"].(map[string]interface{})
	if !ok {
		diskMihomo = make(map[string]interface{})
		diskSchema["mihomo"] = diskMihomo
	}
	diskProvidersRaw, ok := diskMihomo["rule_providers"].([]interface{})
	if !ok {
		diskProvidersRaw = []interface{}{}
	}

	embeddedProvidersMap := make(map[string]map[string]interface{})
	for _, p := range embeddedProviders {
		if pMap, ok := p.(map[string]interface{}); ok {
			if name, ok := pMap["name"].(string); ok {
				embeddedProvidersMap[name] = pMap
			}
		}
	}

	var updatedProviders []interface{}
	diskSeen := make(map[string]bool)
	var modified bool

	for _, p := range diskProvidersRaw {
		pMap, ok := p.(map[string]interface{})
		if !ok {
			updatedProviders = append(updatedProviders, p)
			continue
		}
		name, ok := pMap["name"].(string)
		if !ok {
			updatedProviders = append(updatedProviders, p)
			continue
		}

		diskSeen[name] = true
		embMap, exists := embeddedProvidersMap[name]
		if exists {
			if !areProviderFieldsEqual(pMap, embMap) {
				updatedProviders = append(updatedProviders, embMap)
				modified = true
			} else {
				updatedProviders = append(updatedProviders, pMap)
			}
		} else {
			updatedProviders = append(updatedProviders, pMap)
		}
	}

	for name, embMap := range embeddedProvidersMap {
		if !diskSeen[name] {
			updatedProviders = append(updatedProviders, embMap)
			modified = true
		}
	}

	if modified {
		diskMihomo["rule_providers"] = updatedProviders
		diskSchema["mihomo"] = diskMihomo
		updated, err := json.MarshalIndent(diskSchema, "", "  ")
		if err != nil {
			return nil, err
		}
		return updated, nil
	}

	return nil, nil
}

// migratePresetsIfNeeded compares embedded presets (from default_assets.json) against
// the disk version. If any preset in the embedded list differs from the corresponding
// preset on disk (by matching "id"), the disk preset is overwritten with the embedded
// version. New embedded presets are appended. This ensures that changes like adding
// a new proxy group propagate automatically to existing installations.
func (s *AssetsService) migratePresetsIfNeeded(diskSchema map[string]interface{}) ([]byte, error) {
	var defaultSchema map[string]interface{}
	if err := json.Unmarshal(defaultAssets, &defaultSchema); err != nil {
		return nil, err
	}

	embeddedMihomo, ok := defaultSchema["mihomo"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	embeddedPresets, ok := embeddedMihomo["presets"].([]interface{})
	if !ok {
		return nil, nil
	}

	diskMihomo, ok := diskSchema["mihomo"].(map[string]interface{})
	if !ok {
		diskMihomo = make(map[string]interface{})
		diskSchema["mihomo"] = diskMihomo
	}
	diskPresetsRaw, _ := diskMihomo["presets"].([]interface{})

	// Build a map of embedded presets by id.
	embeddedByID := make(map[string]interface{})
	embeddedOrder := make([]string, 0, len(embeddedPresets))
	for _, p := range embeddedPresets {
		if pMap, ok := p.(map[string]interface{}); ok {
			if id, ok := pMap["id"].(string); ok {
				embeddedByID[id] = pMap
				embeddedOrder = append(embeddedOrder, id)
			}
		}
	}

	// Build a map of disk presets by id for fast lookup.
	diskByID := make(map[string]int) // id → index in diskPresetsRaw
	for i, p := range diskPresetsRaw {
		if pMap, ok := p.(map[string]interface{}); ok {
			if id, ok := pMap["id"].(string); ok {
				diskByID[id] = i
			}
		}
	}

	var modified bool

	// Update or append each embedded preset onto the disk list.
	for _, id := range embeddedOrder {
		embedPreset := embeddedByID[id]
		if idx, exists := diskByID[id]; exists {
			// Overwrite the disk preset with the embedded canonical version.
			embJSON, _ := json.Marshal(embedPreset)
			diskJSON, _ := json.Marshal(diskPresetsRaw[idx])
			if string(embJSON) != string(diskJSON) {
				diskPresetsRaw[idx] = embedPreset
				modified = true
			}
		} else {
			// New preset not present on disk — append it.
			diskPresetsRaw = append(diskPresetsRaw, embedPreset)
			modified = true
		}
	}

	if modified {
		diskMihomo["presets"] = diskPresetsRaw
		diskSchema["mihomo"] = diskMihomo
		updated, err := json.MarshalIndent(diskSchema, "", "  ")
		if err != nil {
			return nil, err
		}
		log.Printf("[AssetsService] migrated presets in assets-definition.json")
		return updated, nil
	}
	return nil, nil
}

func areProviderFieldsEqual(a, b map[string]interface{}) bool {
	keys := []string{"url", "behavior", "format", "outbound"}
	for _, k := range keys {
		aVal, _ := a[k].(string)
		bVal, _ := b[k].(string)
		if aVal != bVal {
			return false
		}
	}

	aPayload, aHas := a["payload"].([]interface{})
	bPayload, bHas := b["payload"].([]interface{})
	if aHas != bHas {
		return false
	}
	if aHas && bHas {
		if len(aPayload) != len(bPayload) {
			return false
		}
		for i := range aPayload {
			aS, _ := aPayload[i].(string)
			bS, _ := bPayload[i].(string)
			if aS != bS {
				return false
			}
		}
	}
	return true
}

// UpdateDefinition validates incoming schema JSON, creates a .bak backup of the existing schema,
// writes atomically, and restores original from backup on errors.
func (s *AssetsService) UpdateDefinition(newData []byte) error {
	if err := s.CheckCompatibility(newData); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	diskPath := filepath.Join(s.dataDir, "assets-definition.json")
	bakPath := diskPath + ".bak"

	// Ensure parent directory exists
	if err := os.MkdirAll(s.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// 2. Safely back up the existing definition file to .bak if it exists
	var backedUp bool
	if _, err := os.Stat(diskPath); err == nil {
		oldData, err := os.ReadFile(diskPath)
		if err != nil {
			return fmt.Errorf("failed to read existing definition for backup: %w", err)
		}
		if err := os.WriteFile(bakPath, oldData, 0600); err != nil {
			return fmt.Errorf("failed to create backup file: %w", err)
		}
		backedUp = true
	}

	// 3. Write new contents using utils.AtomicWriteFile
	if err := utils.AtomicWriteFile(diskPath, newData, 0600); err != nil {
		// 4. Restore the .bak file if the write fails
		if backedUp {
			if restoreErr := os.Rename(bakPath, diskPath); restoreErr != nil {
				log.Printf("ERROR: failed to restore backup file: %v", restoreErr)
			}
		}
		return fmt.Errorf("failed to write new definition: %w", err)
	}

	return nil
}

// CheckCompatibility checks remote schema JSON compatibility against embedded default schema version.
func (s *AssetsService) CheckCompatibility(newData []byte) error {
	var schema map[string]interface{}
	if err := json.Unmarshal(newData, &schema); err != nil {
		return fmt.Errorf("invalid JSON syntax: %w", err)
	}
	schemaVer, ok := schema["schema_version"].(string)
	if !ok {
		return fmt.Errorf("missing schema_version key in schema")
	}

	remoteMajor, err := parseMajorVersion(schemaVer)
	if err != nil {
		return fmt.Errorf("invalid remote schema_version: %w", err)
	}

	var defaultSchema map[string]interface{}
	if err := json.Unmarshal(defaultAssets, &defaultSchema); err != nil {
		return fmt.Errorf("failed to parse default assets: %w", err)
	}
	defaultVer, ok := defaultSchema["schema_version"].(string)
	if !ok {
		return fmt.Errorf("default assets is missing schema_version")
	}
	localMajor, err := parseMajorVersion(defaultVer)
	if err != nil {
		return fmt.Errorf("invalid default schema_version: %w", err)
	}

	if remoteMajor > localMajor {
		return fmt.Errorf("incompatible schema version: remote major version %d is greater than supported local major version %d", remoteMajor, localMajor)
	}
	return nil
}

func parseMajorVersion(v string) (int, error) {
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	dotIdx := strings.Index(v, ".")
	if dotIdx != -1 {
		v = v[:dotIdx]
	}
	var major int
	_, err := fmt.Sscanf(v, "%d", &major)
	if err != nil {
		return 0, fmt.Errorf("invalid version format: %s", v)
	}
	return major, nil
}
