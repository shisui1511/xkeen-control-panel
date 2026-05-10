package services

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func validateFilePath(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	clean := filepath.Clean(path)
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." {
			return errors.New("path traversal detected")
		}
	}
	return nil
}

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
	if err := validateFilePath(path); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *ConfigService) Save(path string, data []byte) error {
	if err := validateFilePath(path); err != nil {
		return err
	}
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
	if err := validateFilePath(path); err != nil {
		return false
	}
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func (s *ConfigService) ListBackups(path string) ([]string, error) {
	if err := validateFilePath(path); err != nil {
		return nil, err
	}
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
	if err := validateFilePath(path); err != nil {
		return
	}
	backups, err := s.ListBackups(path)
	if err != nil || len(backups) <= keep {
		return
	}

	// Delete old backups
	for i := keep; i < len(backups); i++ {
		os.Remove(backups[i])
	}
}

func (s *ConfigService) Create(path string) error {
	if err := validateFilePath(path); err != nil {
		return err
	}
	if s.Exists(path) {
		return os.ErrExist
	}
	return os.WriteFile(path, []byte("{}"), 0644)
}

func (s *ConfigService) Delete(path string) error {
	if err := validateFilePath(path); err != nil {
		return err
	}
	if !s.Exists(path) {
		return os.ErrNotExist
	}
	return os.Remove(path)
}

func (s *ConfigService) Rename(oldPath, newPath string) error {
	if err := validateFilePath(oldPath); err != nil {
		return err
	}
	if err := validateFilePath(newPath); err != nil {
		return err
	}
	if !s.Exists(oldPath) {
		return os.ErrNotExist
	}
	if s.Exists(newPath) {
		return os.ErrExist
	}
	return os.Rename(oldPath, newPath)
}
