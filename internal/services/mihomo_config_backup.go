package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// mihomoConfigBackupRetention — сколько последних бэкапов держать.
// При превышении самые старые удаляются.
const mihomoConfigBackupRetention = 5

// backupMihomoConfig копирует текущий config.yaml в data-директорию с
// timestamp-суффиксом перед любым in-place редактированием.
//
//	dataDir — корневой каталог данных панели (например /opt/xcp_data).
//	configPath — абсолютный путь к Mihomo config.yaml.
//
// Бэкапы попадают в {dataDir}/backup/mihomo/config.yaml.{unixNanoTs}.
// Старые сверх retention автоматически удаляются.
func backupMihomoConfig(dataDir, configPath string) error {
	src, err := os.ReadFile(configPath)
	if err != nil {
		// Конфига нет — нечего бэкапить (новая установка).
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config for backup: %w", err)
	}

	backupDir := filepath.Join(dataDir, "backup", "mihomo")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	ts := time.Now().UnixNano()
	backupPath := filepath.Join(backupDir, fmt.Sprintf("config.yaml.%d", ts))
	if err := os.WriteFile(backupPath, src, 0600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}

	pruneMihomoConfigBackups(backupDir, mihomoConfigBackupRetention)
	return nil
}

// pruneMihomoConfigBackups оставляет только N самых новых бэкапов в директории.
// Сортирует по имени (так как у нас timestamp в имени — это эквивалентно сортировке по времени).
func pruneMihomoConfigBackups(backupDir string, keep int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	type entry struct {
		name string
	}
	var files []entry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Принимаем только наши бэкапы.
		if len(name) < len("config.yaml.") || name[:len("config.yaml.")] != "config.yaml." {
			continue
		}
		files = append(files, entry{name: name})
	}

	if len(files) <= keep {
		return
	}

	// Сортировка по имени (timestamp в имени — natural sort работает).
	sort.Slice(files, func(i, j int) bool {
		return files[i].name < files[j].name
	})

	// Удалить самые старые (всё кроме последних `keep`).
	toRemove := files[:len(files)-keep]
	for _, f := range toRemove {
		_ = os.Remove(filepath.Join(backupDir, f.name))
	}
}
