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
	if s.Exists(path) {
		oldData, err := os.ReadFile(path)
		if err == nil {
			os.WriteFile(backupPath, oldData, 0644)
		}
	}

	// Rotate backups - keep only last 5
	s.rotateBackups(path, 5)

	return os.WriteFile(path, data, 0644)
}

func (s *ConfigService) Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func (s *ConfigService) ListBackups(path string) ([]string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	pattern := filepath.Join(dir, base+".backup-*")

	backups, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		iInfo, _ := os.Stat(backups[i])
		jInfo, _ := os.Stat(backups[j])
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return backups, nil
}

func (s *ConfigService) rotateBackups(path string, keep int) {
	backups, err := s.ListBackups(path)
	if err != nil || len(backups) <= keep {
		return
	}

	// Delete old backups
	for i := keep; i < len(backups); i++ {
		os.Remove(backups[i])
	}
}
