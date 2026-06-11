package assets

import (
	_ "embed"
	"encoding/json"
	"log"
	"os"
	"sync"
)

//go:embed default_assets.json
var defaultAssets []byte

type AssetsService struct {
	mu sync.RWMutex
}

func NewService() *AssetsService {
	return &AssetsService{}
}

// GetDefinition loads assets schema from disk (/opt/etc/xcp/assets-definition.json).
// If missing or syntax error, it falls back to defaultAssets.
func (s *AssetsService) GetDefinition() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	diskPath := "/opt/etc/xcp/assets-definition.json"
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
