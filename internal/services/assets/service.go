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
func (s *AssetsService) GetDefinition() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	diskPath := filepath.Join(s.dataDir, "assets-definition.json")
	if _, err := os.Stat(diskPath); err == nil {
		data, err := os.ReadFile(diskPath)
		if err == nil {
			var temp map[string]interface{}
			if err := json.Unmarshal(data, &temp); err == nil {
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
