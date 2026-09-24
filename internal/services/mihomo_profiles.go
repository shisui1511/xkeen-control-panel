package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Mihomo profiles: complete configs in <mihomo dir>/profiles/<name>.yaml with
// config.yaml being a symlink to the active one. This is the layout other
// tools on the router already use, so the panel adopts it instead of
// inventing its own.

var (
	ErrProfileNotFound    = errors.New("profile not found")
	ErrProfileExists      = errors.New("profile already exists")
	ErrProfileActive      = errors.New("profile is active")
	ErrProfileInvalidName = errors.New("invalid profile name")
	ErrProfilesUnmanaged  = errors.New("config.yaml is not a profile symlink")
)

var profileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

const profileMinimalConfig = `# Mihomo profile created by XKeen Control Panel
mixed-port: 7890
mode: rule
log-level: info
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
`

// MihomoProfile describes one profile file.
type MihomoProfile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"`
	Active bool   `json:"active"`
}

// MihomoProfilesState is the list plus how config.yaml is laid out.
type MihomoProfilesState struct {
	// Managed is true when config.yaml is a symlink into profiles/.
	Managed  bool            `json:"managed"`
	Active   string          `json:"active,omitempty"`
	Profiles []MihomoProfile `json:"profiles"`
}

// MihomoProfileService manages Mihomo profiles. Validation, restart and
// health checks are injected so the service stays testable without a core.
type MihomoProfileService struct {
	dir       string
	backupDir string

	// Validate runs "mihomo -t" for a config file.
	Validate func(path string) error
	// CoreActive reports whether Mihomo is the kernel XKeen currently runs;
	// only then activation restarts it.
	CoreActive func() bool
	// Restart restarts the running core.
	Restart func() error
	// Healthy reports whether the core came up after a restart.
	Healthy func() bool

	mu sync.Mutex
}

func NewMihomoProfileService(mihomoDir, dataDir string) *MihomoProfileService {
	return &MihomoProfileService{
		dir:       filepath.Clean(mihomoDir),
		backupDir: filepath.Join(dataDir, "backup", "mihomo-profiles"),
	}
}

func (s *MihomoProfileService) profilesDir() string { return filepath.Join(s.dir, "profiles") }
func (s *MihomoProfileService) configPath() string  { return filepath.Join(s.dir, "config.yaml") }

func (s *MihomoProfileService) profilePath(name string) (string, error) {
	if !profileNameRe.MatchString(name) || strings.Contains(name, "..") {
		return "", ErrProfileInvalidName
	}
	return filepath.Join(s.profilesDir(), name+".yaml"), nil
}

// activeProfile returns the profile config.yaml points to, or "" with
// ErrProfilesUnmanaged when config.yaml is a regular file or points elsewhere.
func (s *MihomoProfileService) activeProfile() (string, error) {
	target, err := os.Readlink(s.configPath())
	if err != nil {
		return "", ErrProfilesUnmanaged
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(s.dir, target)
	}
	target = filepath.Clean(target)
	if filepath.Dir(target) != s.profilesDir() || filepath.Ext(target) != ".yaml" {
		return "", ErrProfilesUnmanaged
	}
	return strings.TrimSuffix(filepath.Base(target), ".yaml"), nil
}

// List returns all profiles and the active one.
func (s *MihomoProfileService) List() (*MihomoProfilesState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *MihomoProfileService) listLocked() (*MihomoProfilesState, error) {
	state := &MihomoProfilesState{Profiles: []MihomoProfile{}}
	active, err := s.activeProfile()
	if err == nil {
		state.Managed = true
		state.Active = active
	}
	entries, err := os.ReadDir(s.profilesDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".yaml" {
			continue
		}
		base := strings.TrimSuffix(name, ".yaml")
		if !profileNameRe.MatchString(base) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		state.Profiles = append(state.Profiles, MihomoProfile{
			Name:   base,
			Size:   info.Size(),
			Mtime:  info.ModTime().Unix(),
			Active: state.Managed && base == active,
		})
	}
	sort.Slice(state.Profiles, func(i, j int) bool { return state.Profiles[i].Name < state.Profiles[j].Name })
	return state, nil
}

// Adopt converts a plain config.yaml into the profile layout: the file moves
// to profiles/<name>.yaml and config.yaml becomes a symlink to it. The
// running core keeps working since the content is unchanged.
func (s *MihomoProfileService) Adopt(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeProfile(); err == nil {
		return nil
	}
	dst, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrProfileExists
	}
	data, err := os.ReadFile(s.configPath())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.profilesDir(), 0o755); err != nil {
		return err
	}
	if err := utils.AtomicWriteFile(dst, data, 0o600); err != nil {
		return err
	}
	return s.pointConfigTo(name)
}

// Create adds a profile: an empty minimal config, a copy of the active
// config (from == "") or a copy of another profile.
func (s *MihomoProfileService) Create(name, from string, empty bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dst, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrProfileExists
	}
	var data []byte
	switch {
	case empty:
		data = []byte(profileMinimalConfig)
	case from == "":
		// Follows the symlink, so this also works before adoption.
		if data, err = os.ReadFile(s.configPath()); err != nil {
			return err
		}
	default:
		src, err := s.profilePath(from)
		if err != nil {
			return err
		}
		if data, err = os.ReadFile(src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrProfileNotFound
			}
			return err
		}
	}
	if err := os.MkdirAll(s.profilesDir(), 0o755); err != nil {
		return err
	}
	return utils.AtomicWriteFile(dst, data, 0o600)
}

// Rename renames a profile; renaming the active one re-points config.yaml.
func (s *MihomoProfileService) Rename(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, err := s.profilePath(oldName)
	if err != nil {
		return err
	}
	dst, err := s.profilePath(newName)
	if err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return ErrProfileNotFound
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrProfileExists
	}
	active, _ := s.activeProfile()
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	if active == oldName {
		if err := s.pointConfigTo(newName); err != nil {
			_ = os.Rename(dst, src)
			return err
		}
	}
	return nil
}

// Delete removes an inactive profile, keeping a copy in the backup dir.
func (s *MihomoProfileService) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if active, _ := s.activeProfile(); active == name {
		return ErrProfileActive
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ErrProfileNotFound
	}
	if err := os.MkdirAll(s.backupDir, 0o700); err == nil {
		stamp := time.Now().Format("20060102-150405")
		_ = os.WriteFile(filepath.Join(s.backupDir, fmt.Sprintf("%s.%s.yaml", name, stamp)), data, 0o600)
	}
	return os.Remove(path)
}

// ActivationResult tells the UI what happened during activation.
type ActivationResult struct {
	Active     string `json:"active"`
	Restarted  bool   `json:"restarted"`
	RolledBack bool   `json:"rolled_back"`
	Error      string `json:"error,omitempty"`
}

// Activate validates a profile, points config.yaml to it and restarts the
// core when Mihomo is running. If the core does not come back healthy, the
// previous profile is restored and the core restarted again.
func (s *MihomoProfileService) Activate(name string) (*ActivationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.profilePath(name)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, ErrProfileNotFound
	}
	previous, err := s.activeProfile()
	if err != nil {
		return nil, ErrProfilesUnmanaged
	}
	if previous == name {
		return &ActivationResult{Active: name}, nil
	}
	if s.Validate != nil {
		if err := s.Validate(path); err != nil {
			return &ActivationResult{Active: previous, Error: err.Error()}, nil
		}
	}
	if err := s.pointConfigTo(name); err != nil {
		return nil, err
	}
	res := &ActivationResult{Active: name}
	if s.CoreActive == nil || !s.CoreActive() || s.Restart == nil {
		return res, nil
	}

	restartErr := s.Restart()
	res.Restarted = true
	if restartErr == nil && (s.Healthy == nil || s.Healthy()) {
		return res, nil
	}

	// Roll back to the profile that worked before.
	res.RolledBack = true
	res.Active = previous
	if restartErr != nil {
		res.Error = restartErr.Error()
	} else {
		res.Error = "mihomo did not start with the new profile"
	}
	if err := s.pointConfigTo(previous); err != nil {
		return res, fmt.Errorf("rollback failed: %w", err)
	}
	_ = s.Restart()
	return res, nil
}

// pointConfigTo atomically re-points config.yaml to profiles/<name>.yaml.
func (s *MihomoProfileService) pointConfigTo(name string) error {
	tmp := s.configPath() + ".xcp-link"
	_ = os.Remove(tmp)
	if err := os.Symlink(filepath.Join(s.profilesDir(), name+".yaml"), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.configPath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
