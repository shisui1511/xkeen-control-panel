// Package legacyarchive archives the retired configuration data at startup.
package legacyarchive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

const Directory = utils.LegacyArchiveDirectory

var (
	errUnsafePath = errors.New("небезопасный или изменившийся путь")
	errChanged    = errors.New("старые данные изменились после чтения")
)

var sourceNames = []string{"subscriptions", "subscriptions.json", "cache", "user_rules.json"}

// Run оставляет причину сбоя в результате и ошибке. Обе безопасны для журнала
// и уведомления: ошибки файловой системы и декодера могут содержать секреты.
func Run(dataDir string) (Result, error) {
	result, err := run(dataDir)
	if err != nil {
		result.Warning = archiveWarning(err)
		return result, errors.New(result.Warning)
	}
	return result, nil
}

func run(dataDir string) (Result, error) {
	return withData(dataDir, runLocked)
}

// Режимы общего замка DataDir. Чтение метаданных идёт без замка: маркер и
// список заменяются атомарным переименованием. Короткие правки из обработчиков
// не ждут очистку, которая держит замок на время проверки и перезапуска ядра.
type lockMode int

const (
	lockWait lockMode = iota
	lockTry
	lockNone
)

var errBusy = errors.New("операция со старыми данными уже выполняется")

func withData(dataDir string, action func(archiveDir) (Result, error)) (Result, error) {
	return withDataMode(dataDir, lockWait, action)
}

func withDataMode(dataDir string, mode lockMode, action func(archiveDir) (Result, error)) (Result, error) {
	if dataDir == "" {
		return Result{}, errUnsafePath
	}
	path, err := filepath.Abs(dataDir)
	if err != nil {
		return Result{}, err
	}
	validator := utils.NewPathValidator([]string{path})
	clean, err := validator.ValidateUnder(path, path)
	if err != nil {
		return Result{}, errUnsafePath
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, errUnsafePath
	}
	root, err := os.OpenRoot(clean)
	if err != nil {
		return Result{}, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return Result{}, err
	}
	defer dir.Close()
	pinned, err := dir.Stat()
	if err != nil {
		return Result{}, err
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, pinned) || !os.SameFile(info, current) {
		return Result{}, errUnsafePath
	}
	data := archiveDir{root, validator, pinned}
	// Дескриптор каталога даёт общий замок без создания файлов при чистой установке.
	switch mode {
	case lockWait:
		if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX); err != nil {
			return Result{}, err
		}
	case lockTry:
		if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return Result{}, errBusy
			}
			return Result{}, err
		}
	}
	return action(data)
}

func runLocked(data archiveDir) (Result, error) {
	legacyInfo, err := data.lookup(Directory)
	if err != nil && !os.IsNotExist(err) {
		return Result{}, err
	}
	if err == nil {
		return data.withDir(Directory, legacyInfo, func(legacy archiveDir) (Result, error) {
			stored, err := legacy.readMarker()
			if err == nil {
				result := stored.result(legacy.Name())
				if stored.State == complete {
					return result, nil
				}
				return resultAfterCleanup(data, legacy, stored)
			}
			if !os.IsNotExist(err) {
				return Result{}, err
			}
			return prepareAndCleanup(data, legacy)
		})
	}

	found, err := data.hasSources()
	if err != nil || !found {
		return Result{}, err
	}
	if err := data.Root.Mkdir(Directory, 0700); err != nil {
		return Result{}, err
	}
	if err := data.sync(); err != nil {
		return Result{}, err
	}
	legacyInfo, err = data.lookup(Directory)
	if err != nil {
		return Result{}, err
	}
	return data.withDir(Directory, legacyInfo, func(legacy archiveDir) (Result, error) {
		return prepareAndCleanup(data, legacy)
	})
}

func (data archiveDir) hasSources() (bool, error) {
	for _, name := range sourceNames {
		info, err := data.lookup(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if name == "subscriptions.json" || name == "user_rules.json" {
			if !info.Mode().IsRegular() {
				return false, errUnsafePath
			}
			return true, nil
		}
		var found bool
		err = data.visitDir(name, info, func(child archiveDir) error {
			if name == "cache" {
				providers, err := child.lookup("providers")
				if os.IsNotExist(err) {
					return nil
				}
				if err != nil {
					return err
				}
				return child.visitDir("providers", providers, func(providers archiveDir) error {
					found, err = providers.hasFiles()
					return err
				})
			}
			found, err = child.hasFiles()
			return err
		})
		if found || err != nil {
			return found, err
		}
	}
	return false, nil
}

func (d archiveDir) hasFiles() (bool, error) {
	dir, err := d.Root.Open(".")
	if err != nil {
		return false, err
	}
	defer dir.Close()
	for {
		names, err := dir.Readdirnames(1)
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		name := names[0]
		info, err := d.lookup(name)
		if err != nil || !info.IsDir() {
			return err == nil, err
		}
		var found bool
		err = d.visitDir(name, info, func(child archiveDir) error {
			found, err = child.hasFiles()
			return err
		})
		if err != nil || found {
			return found, err
		}
	}
}

func prepareAndCleanup(data, legacy archiveDir) (Result, error) {
	stored, err := prepare(data, legacy)
	if err != nil || stored == nil {
		return Result{}, err
	}
	return resultAfterCleanup(data, legacy, stored)
}

func resultAfterCleanup(data, legacy archiveDir, stored *marker) (Result, error) {
	err := finish(data, legacy, stored)
	return stored.result(legacy.Name()), err
}

func prepare(data, legacy archiveDir) (*marker, error) {
	found, err := data.hasSources()
	if err != nil || !found {
		return nil, err
	}
	if err := legacy.protectDir(); err != nil {
		return nil, err
	}
	stored := &marker{State: prepared, Archive: archiveName, CreatedAt: time.Now().UTC()}
	registry := registryData{urls: []string{}}
	err = legacy.replace(archiveName, func(out *os.File) error {
		gz := gzip.NewWriter(out)
		tw := tar.NewWriter(gz)
		for _, name := range sourceNames {
			item, err := data.archiveSource(tw, name, "", stored, &registry)
			if err != nil {
				return err
			}
			if item != nil {
				stored.Sources = append(stored.Sources, *item)
			}
		}
		if len(stored.Files) == 0 {
			return errChanged
		}
		return errors.Join(tw.Close(), gz.Close())
	})
	if err != nil {
		return nil, err
	}
	stored.URLCount = len(registry.urls)
	stored.Ownership = registry.owners
	if err := legacy.writeJSON(urlsName, registry.urls); err != nil {
		return nil, err
	}
	if err := legacy.writeJSON(markerName, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

func (d archiveDir) archiveSource(tw *tar.Writer, name, parent string, stored *marker, registry *registryData) (*source, error) {
	info, err := d.lookup(name)
	if os.IsNotExist(err) && parent == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	path := filepath.Join(parent, name)
	item := &source{Name: name, Identity: identityOf(info), Directory: info.IsDir()}
	switch {
	case path == "cache":
		// Родитель может содержать rulesets и другие данные нового слоя.
		err := d.visitDir(name, info, func(cache archiveDir) error {
			if _, err := cache.lookup("providers"); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			provider, err := cache.archiveSource(tw, "providers", path, stored, registry)
			if err == nil {
				item.Children = []source{*provider}
			}
			return err
		})
		if len(item.Children) == 0 {
			return nil, err
		}
		return item, err
	case path == "subscriptions" || path == "cache/providers":
		if !info.IsDir() {
			return nil, errUnsafePath
		}
	case path == "subscriptions.json" || path == "user_rules.json" || path == "subscriptions/subscriptions.json":
		if !info.Mode().IsRegular() {
			return nil, errUnsafePath
		}
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return nil, err
	}
	hdr.Name = filepath.ToSlash(path)
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, err
	}
	if info.IsDir() {
		err := d.visitDir(name, info, func(child archiveDir) error {
			dir, err := child.Root.Open(".")
			if err != nil {
				return err
			}
			defer dir.Close()
			entries, err := dir.ReadDir(-1)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				nested, err := child.archiveSource(tw, entry.Name(), path, stored, registry)
				if err != nil {
					return err
				}
				item.Children = append(item.Children, *nested)
			}
			return nil
		})
		return item, err
	}

	f, err := d.openRegular(name, info)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tw, digest), f); err != nil {
		return nil, err
	}
	item.Digest = digest.Sum(nil)
	if path == "subscriptions.json" || path == "subscriptions/subscriptions.json" {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := registry.read(f); err != nil {
			return nil, err
		}
	}
	if err := d.checkUnchanged(name, info, f); err != nil {
		return nil, err
	}
	stored.Files = append(stored.Files, hdr.Name)
	return item, nil
}
