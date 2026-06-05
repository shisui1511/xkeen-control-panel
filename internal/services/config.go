package services

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

func (s *ConfigService) resolvePath(path string) (string, error) {
	if s.validator == nil {
		return "", errors.New("path validator not configured")
	}
	// Explicit clean before validation — makes sanitization visible to static analysis (CWE-22).
	path = filepath.Clean(path)
	return s.validator.Validate(path)
}

type ConfigService struct {
	ConfigDir string
	validator *utils.PathValidator
}

func NewConfigService(dir string, roots []string) *ConfigService {
	return &ConfigService{
		ConfigDir: dir,
		validator: utils.NewPathValidator(roots),
	}
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
	// codeql[go/path-injection] - path is validated by resolvePath/PathValidator above.
	return os.ReadFile(path)
}

func (s *ConfigService) Save(path string, data []byte) error {
	path, err := s.resolvePath(path)
	if err != nil {
		return err
	}

	// Create backup in 'backups' subdirectory.
	// Use filepath.Clean to make sanitization explicit for static analysis (CWE-22).
	backupDir := filepath.Clean(filepath.Join(filepath.Dir(path), "backups"))
	backupPath := filepath.Clean(filepath.Join(backupDir, filepath.Base(path)+".backup-"+time.Now().Format("20060102-150405")))

	if s.Exists(path) {
		// codeql[go/path-injection] - path is validated by resolvePath/PathValidator above.
		oldData, err := os.ReadFile(path)
		if err == nil {
			// codeql[go/path-injection] - backupDir is derived from validated path above.
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
	// codeql[go/path-injection] - path is validated by resolvePath/PathValidator above.
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
		// codeql[go/path-injection] - all entries in backups were validated by resolvePath above.
		iInfo, _ := os.Stat(backups[i])
		// codeql[go/path-injection] - all entries in backups were validated by resolvePath above.
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
		// codeql[go/path-injection] - backups[i] is validated by resolvePath immediately above.
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
	// codeql[go/path-injection] - path is validated by resolvePath/PathValidator above.
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
	// codeql[go/path-injection] - path is validated by resolvePath/PathValidator above.
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
	// codeql[go/path-injection] - both paths are validated by resolvePath/PathValidator above.
	return os.Rename(oldPath, newPath)
}
