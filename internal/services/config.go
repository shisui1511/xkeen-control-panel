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
	if s.validator == nil {
		return "", errors.New("path validator not configured")
	}
	// Explicit clean + validate: strips traversal and checks against allowed roots.
	clean := filepath.Clean(path)
	validated, err := s.validator.Validate(clean)
	if err != nil {
		return "", err
	}
	// Extra guard: path must be within one of the known allowed roots (CWE-22).
	for _, root := range s.validator.AllowedRoots {
		cleanRoot := filepath.Clean(root)
		if validated == cleanRoot || strings.HasPrefix(validated, cleanRoot+string(filepath.Separator)) {
			return validated, nil
		}
	}
	return "", errors.New("path not within allowed configuration roots")
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
	safeDir, err := s.resolvePath(dir)
	if err != nil {
		return nil, err
	}
	var allFiles []string
	extensions := []string{"*.json", "*.yaml", "*.yml", "*.conf"}
	for _, ext := range extensions {
		pattern := filepath.Join(safeDir, ext)
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
	safePath, err := s.resolvePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(safePath)
}

func (s *ConfigService) Save(path string, data []byte) error {
	safePath, err := s.resolvePath(path)
	if err != nil {
		return err
	}

	// Build backup paths entirely from the validated safePath.
	backupDir := filepath.Join(filepath.Dir(safePath), "backups")
	backupName := filepath.Base(safePath) + ".backup-" + time.Now().Format("20060102-150405")
	backupPath := filepath.Join(backupDir, backupName)

	if s.Exists(safePath) {
		oldData, err := os.ReadFile(safePath)
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
	if err := s.rotateBackups(safePath, 5); err != nil {
		return err
	}

	return utils.AtomicWriteFile(safePath, data, 0644)
}

func (s *ConfigService) Exists(path string) bool {
	safePath, err := s.resolvePath(path)
	if err != nil {
		return false
	}
	_, err = os.Stat(safePath)
	return !os.IsNotExist(err)
}

func (s *ConfigService) ListBackups(path string) ([]string, error) {
	safePath, err := s.resolvePath(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(safePath)
	backupDir := filepath.Join(dir, "backups")
	base := filepath.Base(safePath)
	pattern := filepath.Join(backupDir, base+".backup-*")

	backups, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Validate and filter backups to ensure they are within config dir.
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
	safePath, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	backups, err := s.ListBackups(safePath)
	if err != nil || len(backups) <= keep {
		return err
	}

	// Delete old backups — each entry was validated by ListBackups/resolvePath.
	for i := keep; i < len(backups); i++ {
		safeBackup, err := s.resolvePath(backups[i])
		if err != nil {
			continue
		}
		if err := os.Remove(safeBackup); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConfigService) Create(path string) error {
	safePath, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	if s.Exists(safePath) {
		return os.ErrExist
	}
	return os.WriteFile(safePath, []byte("{}"), 0644)
}

func (s *ConfigService) Delete(path string) error {
	safePath, err := s.resolvePath(path)
	if err != nil {
		return err
	}
	if !s.Exists(safePath) {
		return os.ErrNotExist
	}
	return os.Remove(safePath)
}

func (s *ConfigService) Rename(oldPath, newPath string) error {
	safeOld, err := s.resolvePath(oldPath)
	if err != nil {
		return err
	}
	safeNew, err := s.resolvePath(newPath)
	if err != nil {
		return err
	}
	if !s.Exists(safeOld) {
		return os.ErrNotExist
	}
	if s.Exists(safeNew) {
		return os.ErrExist
	}
	return os.Rename(safeOld, safeNew)
}
