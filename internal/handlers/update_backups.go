package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Резервные копии бинарника перед обновлением: xcp.bak.<unix>[.<версия>].
// Версия в имени появилась позже, у старых копий её нет. Build metadata (+g<sha>)
// отбрасывается: PathValidator не пропускает «+».
var backupNameRe = regexp.MustCompile(`^xcp\.bak\.([0-9]+)(?:\.([0-9A-Za-z.-]+))?$`)

// UpdateBackup — одна резервная копия для отката.
type UpdateBackup struct {
	Name      string `json:"name"`
	Version   string `json:"version,omitempty"`
	CreatedAt int64  `json:"created_at"`
	Size      int64  `json:"size"`
}

func backupFileName(now time.Time, version string) string {
	version, _, _ = strings.Cut(strings.TrimPrefix(version, "v"), "+")
	name := fmt.Sprintf("xcp.bak.%d", now.Unix())
	if version != "" && backupNameRe.MatchString(name+"."+version) {
		name += "." + version
	}
	return name
}

// listBackups возвращает копии от новых к старым.
func listBackups(dir string) ([]UpdateBackup, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []UpdateBackup{}, nil
		}
		return nil, err
	}
	backups := []UpdateBackup{}
	for _, e := range entries {
		m := backupNameRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		created, _ := strconv.ParseInt(m[1], 10, 64)
		backups = append(backups, UpdateBackup{
			Name:      e.Name(),
			Version:   m[2],
			CreatedAt: created,
			Size:      info.Size(),
		})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt > backups[j].CreatedAt })
	return backups, nil
}

// resolveBackup возвращает путь к копии name в dir, пустое name — самая новая.
func resolveBackup(dir, name string) (string, error) {
	if name == "" {
		backups, err := listBackups(dir)
		if err != nil {
			return "", err
		}
		if len(backups) == 0 {
			return "", os.ErrNotExist
		}
		name = backups[0].Name
	}
	if !backupNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid backup name")
	}
	path, err := utils.NewPathValidator([]string{dir}).Validate(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return path, nil
}

// UpdateBackups — GET /api/update/backups: резервные копии для отката.
func (a *API) UpdateBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		a.errorResponse(w, a.t(r, "error.method_not_allowed"), http.StatusMethodNotAllowed)
		return
	}
	backups, err := listBackups(filepath.Join(a.cfg.DataDir, "backup"))
	if err != nil {
		JSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSONSuccess(w, backups)
}
