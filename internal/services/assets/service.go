package assets

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Validate the input data is syntactically valid JSON and contains key "schema_version"
	var schema map[string]interface{}
	if err := json.Unmarshal(newData, &schema); err != nil {
		return fmt.Errorf("invalid JSON syntax: %w", err)
	}
	if _, ok := schema["schema_version"]; !ok {
		return fmt.Errorf("missing schema_version key in schema")
	}

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
