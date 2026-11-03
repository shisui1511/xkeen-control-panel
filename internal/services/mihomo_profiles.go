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

	"gopkg.in/yaml.v3"

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
	// ErrProfileReserved — имя занято панелью: префикс xcp- без учёта регистра
	// (D-24). Профиль пользователя с таким именем панель сочла бы своим файлом и
	// удалила как сироту.
	ErrProfileReserved = errors.New("profile name is reserved by the panel")
	// ErrProfileReturnTarget — профиль служит целью возврата Mihomo, пока им
	// управляет панель: удалять и переименовывать его нельзя (D-12).
	ErrProfileReturnTarget = errors.New("profile is the return target of Mihomo")
	// ErrPanelProfileAbsent — файла профиля панели (xcp-main.yaml) сейчас нет:
	// копировать нечего (D-11).
	ErrPanelProfileAbsent = errors.New("panel profile file is absent")

	// ErrRestartSkipped — Restart решил не перезапускать ядро: под замком
	// применения оно оказалось остановленным или неактивным. Активация
	// профиля при этом остаётся в силе, откат не делается.
	ErrRestartSkipped = errors.New("restart skipped: core is not running or not active")
)

var profileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

const (
	// reservedProfilePrefix — префикс имён, которыми владеет панель (xcp-main и
	// будущие файлы панели).
	reservedProfilePrefix = "xcp-"
	// panelProfileName — профиль панели в каталоге profiles/.
	panelProfileName = "xcp-main"
	// xkeenProfileBase — основа имени, под которым прежний обычный config.yaml XKeen
	// переезжает в profiles/ при активации (D-27). Пакет services не импортирует
	// configlayer (цикл): значение и суффиксы совпадают с configlayer.XKeenProfileBase
	// и FreeProfileRel.
	xkeenProfileBase = "config-xkeen"
	// maxProfileSuffix — наибольший суффикс -N у свободного имени.
	maxProfileSuffix = 99
	// plainConfigName — имя обычного файла config.yaml в списке профилей.
	plainConfigName = "config.yaml"

	// Владельцы профилей в списке.
	profileOwnerPanel = "panel"
	profileOwnerUser  = "user"
	profileOwnerXKeen = "xkeen"
)

// isReservedProfileName — имя занято панелью: префикс xcp- без учёта регистра.
func isReservedProfileName(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), reservedProfilePrefix)
}

// IsReservedProfileName — isReservedProfileName для обработчиков.
func IsReservedProfileName(name string) bool { return isReservedProfileName(name) }

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
	// Owner — кто владеет файлом: panel (xcp-main), user (прочие профили), xkeen
	// (обычный config.yaml).
	Owner string `json:"owner"`
}

// MihomoProfilesState is the list plus how config.yaml is laid out.
type MihomoProfilesState struct {
	// Managed is true when config.yaml is a symlink into profiles/.
	Managed  bool            `json:"managed"`
	Active   string          `json:"active,omitempty"`
	Profiles []MihomoProfile `json:"profiles"`
	// PlainConfig — config.yaml, когда это обычный файл XKeen, а не ссылка на профиль.
	PlainConfig *MihomoProfile `json:"plain_config,omitempty"`
	// ReturnTarget — профиль, на который вернётся Mihomo при отпускании (пусто, когда
	// панель Mihomo не управляет).
	ReturnTarget string `json:"return_target,omitempty"`
}

// MihomoProfileService manages Mihomo profiles. Validation, restart and
// health checks are injected so the service stays testable without a core.
type MihomoProfileService struct {
	dir       string
	backupDir string

	// Validate runs "mihomo -t" for a config file.
	Validate func(path string) error
	// ValidateContent runs "mihomo -t" for profile content in the mirror of the
	// Mihomo root (no file on disk). Used when Validate is not set and for
	// profiles created from a template or a copy.
	ValidateContent func(data []byte) error
	// CoreActive reports whether Mihomo is the kernel XKeen currently runs;
	// only then activation restarts it.
	CoreActive func() bool
	// Restart restarts the running core. It may return ErrRestartSkipped when
	// the core turned out not to need a restart at that moment.
	Restart func() error
	// RollbackRestart поднимает ядро после отката активации: процесс, который
	// не запустился с новым профилем, остановлен не пользователем, поэтому
	// решение «остановлено — не запускать» не применяется. Не задан — Restart.
	RollbackRestart func() error
	// RunningPID returns the PID of the running Mihomo process, 0 when it is
	// not running. Activate reads it before the restart.
	RunningPID func() int
	// Healthy reports whether the core came up after a restart as a NEW process:
	// oldPID is the PID taken before the restart and must not count.
	Healthy func(oldPID int) bool
	// ReturnTarget — профиль, на который вернётся Mihomo при отпускании, пока им
	// управляет панель; пусто — не управляет. Читается при каждом вызове.
	ReturnTarget func() string

	mu sync.Mutex
}

func NewMihomoProfileService(mihomoDir, dataDir string) *MihomoProfileService {
	return &MihomoProfileService{
		dir:       filepath.Clean(mihomoDir),
		backupDir: filepath.Join(dataDir, "backup", "mihomo-profiles"),
	}
}

// LockWrite takes the profile write lock (s.mu) for a caller that writes a
// profile file itself (ConfigSave). Lock order is always SubscriptionService
// mihomoMu first, then this lock; the service methods take s.mu on their own,
// so a caller that uses them must not call LockWrite.
func (s *MihomoProfileService) LockWrite() { s.mu.Lock() }

// UnlockWrite releases the lock taken by LockWrite.
func (s *MihomoProfileService) UnlockWrite() { s.mu.Unlock() }

// validatePath checks a profile file: through Validate when it is set, otherwise
// by reading the file and passing it to ValidateContent. Without either, nothing
// is checked.
func (s *MihomoProfileService) validatePath(path string) error {
	if s.Validate != nil {
		return s.Validate(path)
	}
	if s.ValidateContent == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return s.ValidateContent(data)
}

// returnTarget читает цель возврата; вызывается до взятия s.mu, чтобы чужой замок
// слоя не оказывался под замком сервиса.
func (s *MihomoProfileService) returnTarget() string {
	if s.ReturnTarget == nil {
		return ""
	}
	return s.ReturnTarget()
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
	target := s.returnTarget()
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.listLocked()
	if err != nil {
		return nil, err
	}
	state.ReturnTarget = target
	return state, nil
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
		// Прочие файлы с префиксом xcp- — сироты панели (FND-03), не профили
		// пользователя: в список не попадают.
		owner := profileOwnerUser
		if base == panelProfileName {
			owner = profileOwnerPanel
		} else if isReservedProfileName(base) {
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
			Owner:  owner,
		})
	}
	sort.Slice(state.Profiles, func(i, j int) bool { return state.Profiles[i].Name < state.Profiles[j].Name })
	if info, err := os.Lstat(s.configPath()); err == nil && info.Mode().IsRegular() {
		state.PlainConfig = &MihomoProfile{
			Name:   plainConfigName,
			Size:   info.Size(),
			Mtime:  info.ModTime().Unix(),
			Active: true,
			Owner:  profileOwnerXKeen,
		}
	}
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
	if isReservedProfileName(name) {
		return ErrProfileReserved
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

// ProfileInvalidError — содержимое профиля не прошло проверку ядра: файл не создан.
// Output — вывод `mihomo -t` без префикса.
type ProfileInvalidError struct {
	Output string
}

func (e *ProfileInvalidError) Error() string { return e.Output }

// CreateFromContent создаёт профиль с готовым содержимым (из шаблона или копии):
// имя не зарезервировано и не занято, содержимое проходит ValidateContent (тот же
// `mihomo -t` в зеркале корня, что у сохранения), файл пишется с правами 0600. Отказ
// проверки — *ProfileInvalidError, файла нет.
func (s *MihomoProfileService) CreateFromContent(name string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dst, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if isReservedProfileName(name) {
		return ErrProfileReserved
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrProfileExists
	}
	if s.ValidateContent != nil {
		if err := s.ValidateContent(data); err != nil {
			if out, ok := strings.CutPrefix(err.Error(), "mihomo -t: "); ok {
				return &ProfileInvalidError{Output: out}
			}
			return err
		}
	}
	if err := os.MkdirAll(s.profilesDir(), 0o755); err != nil {
		return err
	}
	return utils.AtomicWriteFile(dst, data, 0o600)
}

// panelRuleListRe — путь списка правил, который панель пишет сама: имя после xcp-
// подчиняется тому же правилу, что имена файлов панели (латиница в нижнем регистре,
// цифры и дефис). Другие пути копия не трогает.
var panelRuleListRe = regexp.MustCompile(`^\./rule_providers/xcp-([a-z0-9][a-z0-9-]*)\.txt$`)

// panelRuleListPaths возвращает пути списков правил профиля панели вида
// ./rule_providers/xcp-<имя>.txt (без повторов, в порядке появления). Разбор — yaml.v3,
// только ради rule-providers.*.path.
func panelRuleListPaths(data []byte) ([]string, error) {
	var doc struct {
		Providers yaml.Node `yaml:"rule-providers"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	node := &doc.Providers
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	var out []string
	seen := map[string]bool{}
	if node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 1; i < len(node.Content); i += 2 {
		def := node.Content[i]
		if def.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(def.Content); j += 2 {
			if def.Content[j].Value != "path" {
				continue
			}
			p := def.Content[j+1].Value
			if panelRuleListRe.MatchString(p) && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// CopyPanelProfile копирует профиль панели в профиль пользователя (D-04). Копия
// самостоятельна: файлы списков правил rule_providers/xcp-<имя>.txt копируются рядом
// в rule_providers/<профиль>-<имя>.txt (права 0600), пути в копии переписаны, а
// списки замирают на момент копии. Файлы узлов proxy_providers/xcp-* остаются
// ссылками: они переживают отпускание. Нет исходного файла списка — путь переписан
// всё равно, файл появится у ядра. Результат проходит ValidateContent; отказ или сбой
// записи — ни файла профиля, ни копий списков не остаётся. Нет профиля панели —
// ErrPanelProfileAbsent.
func (s *MihomoProfileService) CopyPanelProfile(name string) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dst, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if isReservedProfileName(name) {
		return ErrProfileReserved
	}
	if _, err := os.Stat(dst); err == nil {
		return ErrProfileExists
	}
	src, err := s.profilePath(panelProfileName)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrPanelProfileAbsent
		}
		return err
	}
	paths, err := panelRuleListPaths(data)
	if err != nil {
		// Файл панели не разбирается: копия отвергнута как профиль с ошибкой.
		return &ProfileInvalidError{Output: err.Error()}
	}

	text := string(data)
	listDir := filepath.Join(s.dir, "rule_providers")
	var written []string
	defer func() {
		if err != nil {
			for _, f := range written {
				_ = os.Remove(f)
			}
		}
	}()
	for _, old := range paths {
		// Имя части после xcp- уже проверено panelRuleListRe: путь собирается из
		// проверенного имени профиля и проверенного имени списка, выйти за
		// rule_providers он не может.
		list := panelRuleListRe.FindStringSubmatch(old)[1]
		newRel := "./rule_providers/" + name + "-" + list + ".txt"
		text = strings.ReplaceAll(text, old, newRel)
		body, rerr := os.ReadFile(filepath.Join(listDir, "xcp-"+list+".txt"))
		if rerr != nil {
			if errors.Is(rerr, os.ErrNotExist) {
				continue
			}
			return rerr
		}
		if err = os.MkdirAll(listDir, 0o755); err != nil {
			return err
		}
		target := filepath.Join(listDir, name+"-"+list+".txt")
		if err = utils.AtomicWriteFile(target, body, 0o600); err != nil {
			return err
		}
		written = append(written, target)
	}

	result := []byte(text)
	if s.ValidateContent != nil {
		if err = s.ValidateContent(result); err != nil {
			if out, ok := strings.CutPrefix(err.Error(), "mihomo -t: "); ok {
				err = &ProfileInvalidError{Output: out}
			}
			return err
		}
	}
	if err = os.MkdirAll(s.profilesDir(), 0o755); err != nil {
		return err
	}
	return utils.AtomicWriteFile(dst, result, 0o600)
}

// ReadProfile возвращает содержимое профиля пользователя. Имя проверяется как у
// остальных операций; профиль панели (xcp-*) этим путём не читается.
func (s *MihomoProfileService) ReadProfile(name string) ([]byte, error) {
	path, err := s.profilePath(name)
	if err != nil {
		return nil, err
	}
	if isReservedProfileName(name) {
		return nil, ErrProfileReserved
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrProfileNotFound
		}
		return nil, err
	}
	return data, nil
}

// Rename renames a profile; renaming the active one re-points config.yaml.
func (s *MihomoProfileService) Rename(oldName, newName string) error {
	target := s.returnTarget()
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
	if isReservedProfileName(oldName) || isReservedProfileName(newName) {
		return ErrProfileReserved
	}
	// PrevLink хранит путь к цели возврата: переименование оставило бы висячую ссылку.
	if target != "" && oldName == target {
		return ErrProfileReturnTarget
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
	target := s.returnTarget()
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if isReservedProfileName(name) {
		return ErrProfileReserved
	}
	if target != "" && name == target {
		return ErrProfileReturnTarget
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
		base := strings.TrimSuffix(filepath.Base(path), ".yaml")
		_ = os.WriteFile(filepath.Join(s.backupDir, fmt.Sprintf("%s.%s.yaml", base, stamp)), data, 0o600)
	}
	return os.Remove(path)
}

// ActivationResult tells the UI what happened during activation.
type ActivationResult struct {
	Active     string `json:"active"`
	Restarted  bool   `json:"restarted"`
	RolledBack bool   `json:"rolled_back"`
	Error      string `json:"error,omitempty"`
	// Adopted — имя профиля, в который при активации переехал прежний обычный
	// config.yaml XKeen (D-27); пусто, когда переезда не было или он откатан.
	Adopted string `json:"adopted,omitempty"`
}

// Activate validates a profile, points config.yaml to it and restarts the
// core when Mihomo is running. If the core does not come back healthy, the
// previous profile is restored and the core restarted again. Обычный config.yaml
// не принимается: ErrProfilesUnmanaged (см. ActivateAdopting).
func (s *MihomoProfileService) Activate(name string) (*ActivationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activateLocked(name)
}

// activateLocked — активация, когда config.yaml уже ссылка на профиль. Вызывается
// под s.mu.
func (s *MihomoProfileService) activateLocked(name string) (*ActivationResult, error) {
	path, err := s.profilePath(name)
	if err != nil {
		return nil, err
	}
	if isReservedProfileName(name) {
		return nil, ErrProfileReserved
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
	if err := s.validatePath(path); err != nil {
		return &ActivationResult{Active: previous, Error: err.Error()}, nil
	}
	if err := s.pointConfigTo(name); err != nil {
		return nil, err
	}
	undo := func() error { return s.pointConfigTo(previous) }
	return s.restartConfirmed(&ActivationResult{Active: name}, previous, undo)
}

// ActivateAdopting — Activate, который принимает и обычный config.yaml XKeen
// (D-27): файл одной операцией переезжает в profiles/config-xkeen.yaml (содержимое
// то же, при занятом имени — суффикс -2…-99), config.yaml становится ссылкой на
// выбранный профиль. Не запустилось — возвращается всё: прежний обычный файл с теми же
// байтами и правами, копия удалена. Вернуться на прежний файл можно активацией
// config-xkeen.
func (s *MihomoProfileService) ActivateAdopting(name string) (*ActivationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, lerr := os.Lstat(s.configPath())
	if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return s.activateLocked(name)
	}
	if lerr != nil && !errors.Is(lerr, os.ErrNotExist) {
		return nil, lerr
	}
	if lerr == nil && !info.Mode().IsRegular() {
		return nil, ErrProfilesUnmanaged
	}
	path, err := s.profilePath(name)
	if err != nil {
		return nil, err
	}
	if isReservedProfileName(name) {
		return nil, ErrProfileReserved
	}
	if _, err := os.Stat(path); err != nil {
		return nil, ErrProfileNotFound
	}
	previous := ""
	if lerr == nil {
		previous = plainConfigName
	}
	if err := s.validatePath(path); err != nil {
		return &ActivationResult{Active: previous, Error: err.Error()}, nil
	}

	cfg := s.configPath()
	var (
		keep string
		undo func() error
	)
	if lerr == nil {
		data, err := os.ReadFile(cfg)
		if err != nil {
			return nil, err
		}
		if keep = s.freeProfileName(xkeenProfileBase); keep == "" {
			return nil, ErrProfileExists
		}
		keepPath, err := s.profilePath(keep)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(s.profilesDir(), 0o755); err != nil {
			return nil, err
		}
		if err := utils.AtomicWriteFile(keepPath, data, 0o600); err != nil {
			return nil, err
		}
		perm := info.Mode().Perm()
		undo = func() error {
			// Прежний обычный файл: временный файл рядом и переименование поверх ссылки.
			tmp := cfg + ".xcp-restore"
			_ = os.Remove(tmp)
			if err := os.WriteFile(tmp, data, perm); err != nil {
				_ = os.Remove(tmp)
				return err
			}
			if err := os.Chmod(tmp, perm); err != nil {
				_ = os.Remove(tmp)
				return err
			}
			if err := os.Rename(tmp, cfg); err != nil {
				_ = os.Remove(tmp)
				return err
			}
			return os.Remove(keepPath)
		}
		if err := s.pointConfigTo(name); err != nil {
			_ = os.Remove(keepPath)
			return nil, err
		}
	} else {
		if err := s.pointConfigTo(name); err != nil {
			return nil, err
		}
		undo = func() error { return os.Remove(cfg) }
	}
	res, err := s.restartConfirmed(&ActivationResult{Active: name}, previous, undo)
	if res != nil && keep != "" && !res.RolledBack {
		res.Adopted = keep
	}
	return res, err
}

// freeProfileName возвращает первое свободное имя base[-N] (N от 2 до 99) в
// profiles/; занятость — по Lstat, оборванная ссылка тоже занимает имя. Свободного
// имени нет — пустая строка. Поведение как у configlayer.FreeProfileRel.
func (s *MihomoProfileService) freeProfileName(base string) string {
	for n := 1; n <= maxProfileSuffix; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		if _, err := os.Lstat(filepath.Join(s.profilesDir(), name+".yaml")); errors.Is(err, os.ErrNotExist) {
			return name
		}
	}
	return ""
}

// restartConfirmed перезапускает Mihomo после переключения config.yaml и ждёт новый
// процесс. Отказ запуска — undo возвращает прежний вид config.yaml, ядро
// перезапускается ещё раз; res.Active становится previous. Профиль записан, а ядро
// остановлено или неактивно — перезапуска нет, переключение остаётся.
func (s *MihomoProfileService) restartConfirmed(res *ActivationResult, previous string, undo func() error) (*ActivationResult, error) {
	if s.CoreActive == nil || !s.CoreActive() || s.Restart == nil {
		return res, nil
	}

	oldPID := 0
	if s.RunningPID != nil {
		oldPID = s.RunningPID()
	}
	restartErr := s.Restart()
	if errors.Is(restartErr, ErrRestartSkipped) {
		// Решение CoreActive устарело: ядро остановили между проверкой и
		// рестартом. Профиль записан, перезапускать нечего.
		return res, nil
	}
	res.Restarted = true
	if restartErr == nil && (s.Healthy == nil || s.Healthy(oldPID)) {
		return res, nil
	}

	// Roll back to the config that worked before.
	res.RolledBack = true
	res.Active = previous
	if restartErr != nil {
		res.Error = restartErr.Error()
	} else {
		res.Error = "mihomo did not start with the new profile"
	}
	if err := undo(); err != nil {
		return res, fmt.Errorf("rollback failed: %w", err)
	}
	restart := s.Restart
	if s.RollbackRestart != nil {
		restart = s.RollbackRestart
	}
	if err := restart(); err != nil && !errors.Is(err, ErrRestartSkipped) {
		res.Error += "; " + err.Error()
	}
	return res, nil
}

// pointConfigTo atomically re-points config.yaml to profiles/<name>.yaml.
func (s *MihomoProfileService) pointConfigTo(name string) error {
	target, err := s.profilePath(name)
	if err != nil {
		return err
	}
	tmp := s.configPath() + ".xcp-link"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.configPath()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
