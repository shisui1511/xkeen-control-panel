package services

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func (s *ConfigService) resolvePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return "", errors.New("path traversal detected")
	}
	// Note: We don't check against s.ConfigDir here anymore because the API handler
	// uses PathValidator which checks against multiple AllowedRoots.
	return clean, nil
}

type ConfigService struct {
	ConfigDir string
}

func NewConfigService(dir string) *ConfigService {
	return &ConfigService{ConfigDir: dir}
}

func (s *ConfigService) List(dir string) ([]string, error) {
	if dir == "" {
		dir = s.ConfigDir
	}
	var allFiles []string
	extensions := []string{"*.json", "*.yaml", "*.yml", "*.conf"}
	for _, ext := range extensions {
		pattern := filepath.Join(dir, ext)
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		allFiles = append(allFiles, files...)
	}
	sort.Strings(allFiles)
	return allFiles, nil
}

func (s *ConfigService) Read(path string) ([]byte, error) {
	path, err := s.resolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *ConfigService) Save(path string, data []byte) error {
	path, err := s.resolvePath(path)
	if err != nil {
		return err
	}

	// Create backup in 'backups' subdirectory
	backupDir := filepath.Join(filepath.Dir(path), "backups")
	backupPath := filepath.Join(backupDir, filepath.Base(path)+".backup-"+time.Now().Format("20060102-150405"))

	if s.Exists(path) {
		oldData, err := os.ReadFile(path)
		if err == nil {
			if err := os.MkdirAll(backupDir, 0755); err != nil {
				return err
			}
			if err := utils.AtomicWriteFile(backupPath, oldData, 0644); err != nil {
				return err
			}
		}
	}

	// Rotate backups - keep only last 5
	if err := s.rotateBackups(path, 5); err != nil {
		return err
	}

	return utils.AtomicWriteFile(path, data, 0644)
}

func (s *ConfigService) Exists(path string) bool {
	path, err := s.resolvePath(path)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return !os.IsNotExist(err)
}

func (s *ConfigService) ListBackups(path string) ([]string, error) {
	path, err := s.resolvePath(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	backupDir := filepath.Join(dir, "backups")
	base := filepath.Base(path)
	pattern := filepath.Join(backupDir, base+".backup-*")

	backups, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Validate and filter backups to ensure they are within config dir
	var valid []string
	for _, b := range backups {
		if _, err := s.resolvePath(b); err != nil {
			continue
		}
		valid = append(valid, b)
	}
	backups = valid

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		iInfo, _ := os.Stat(backups[i])
		jInfo, _ := os.Stat(backups[j])
		if iInfo == nil || jInfo == nil {
			return false
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return backups, nil
}

func (s *ConfigService) rotateBackups(path string, keep int) error {
	path, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	backups, err := s.ListBackups(path)
	if err != nil || len(backups) <= keep {
		return err
	}

	// Delete old backups
	for i := keep; i < len(backups); i++ {
		if _, err := s.resolvePath(backups[i]); err != nil {
			continue
		}
		if err := os.Remove(backups[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConfigService) Create(path string) error {
	path, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	if s.Exists(path) {
		return os.ErrExist
	}
	return os.WriteFile(path, []byte("{}"), 0644)
}

func (s *ConfigService) Delete(path string) error {
	path, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	if !s.Exists(path) {
		return os.ErrNotExist
	}
	return os.Remove(path)
}

func (s *ConfigService) Rename(oldPath, newPath string) error {
	oldPath, err := s.resolvePath(oldPath)
	if err != nil {
		return err
	}
	newPath, err = s.resolvePath(newPath)
	if err != nil {
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
