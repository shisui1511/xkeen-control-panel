package services

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

func sortStrings(s []string) []string {
	sort.Strings(s)
	return s
}
type ConfigService struct {
	ConfigDir string
}

func NewConfigService(dir string) *ConfigService {
	return &ConfigService{ConfigDir: dir}
}

func (s *ConfigService) List() ([]string, error) {
	pattern := filepath.Join(s.ConfigDir, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (s *ConfigService) Read(path string) ([]byte, error) {
	// Validation should be done by caller (PathValidator)
	return os.ReadFile(path)
}

func (s *ConfigService) Save(path string, data []byte) error {
	// Create backup
	backupPath := path + ".backup-" + time.Now().Format("20060102-150405")
	os.WriteFile(backupPath, data, 0644)
	
	return os.WriteFile(path, data, 0644)
}

func (s *ConfigService) Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
