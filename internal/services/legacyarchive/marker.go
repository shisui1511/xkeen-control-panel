package legacyarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

const (
	archiveName = "old-panel.tar.gz"
	markerName  = "archive.json"
	urlsName    = "urls.json"
	prepared    = "prepared"
	complete    = "complete"
)

// Result — несекретный итог для журнала и будущего уведомления.
type Result struct {
	ArchivePath       string
	Complete          bool
	URLCount          int
	KernelCleanupDone bool
	Warning           string
}

type marker struct {
	State             string      `json:"state"`
	Archive           string      `json:"archive"`
	CreatedAt         time.Time   `json:"created_at"`
	Files             []string    `json:"files"`
	URLCount          int         `json:"url_count"`
	KernelCleanupDone bool        `json:"kernel_cleanup_done"`
	Ownership         []ownership `json:"ownership,omitempty"`
	Sources           []source    `json:"sources,omitempty"`
}

type source struct {
	Name      string   `json:"name"`
	Identity  identity `json:"identity"`
	Directory bool     `json:"directory,omitempty"`
	Digest    []byte   `json:"sha256,omitempty"`
	Children  []source `json:"children,omitempty"`
}

type identity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

func identityOf(info os.FileInfo) identity {
	stat := info.Sys().(*syscall.Stat_t)
	return identity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}
}

type ownership struct {
	ID           string `json:"id"`
	ProviderName string `json:"provider_name,omitempty"`
	XrayHash     string `json:"xray_hash,omitempty"`
	TagPrefix    string `json:"tag_prefix,omitempty"`
	RoutingMode  string `json:"routing_mode,omitempty"`
}

type registryData struct {
	urls   []string
	seen   map[string]bool
	owners []ownership
}

var registryName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var errRegistry = errors.New("не удалось прочитать реестр старых подписок")
var errMarker = errors.New("не удалось проверить маркер архива")

// read читает только поля верхнего уровня записи. Кэш узлов внутри
// старого реестра пропускается токенами без построения объектов в памяти.
func (r *registryData) read(f *os.File) error {
	dec := json.NewDecoder(f)
	first, err := dec.Token()
	if err != nil {
		return errRegistry
	}
	if first == json.Delim('[') {
		for dec.More() {
			if err := r.readRecord(dec); err != nil {
				return errRegistry
			}
		}
		if end, err := dec.Token(); err != nil || end != json.Delim(']') {
			return errRegistry
		}
	} else if first != nil {
		return errRegistry
	}
	if _, err := dec.Token(); err != io.EOF {
		return errRegistry
	}
	return nil
}

func (r *registryData) readRecord(dec *json.Decoder) error {
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return errRegistry
	}
	var owner ownership
	var url string
	fields := make(map[json.Token]bool)
	unique := true
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		if fields[key] {
			unique = false
		}
		fields[key] = true
		value, err := dec.Token()
		if err != nil {
			return err
		}
		if literal, ok := value.(string); ok {
			switch key {
			case "id":
				owner.ID = literal
			case "provider_name":
				owner.ProviderName = literal
			case "url":
				url = literal
			case "last_hash":
				owner.XrayHash = literal
			case "tag_prefix":
				owner.TagPrefix = literal
			case "routing_mode":
				owner.RoutingMode = literal
			}
		}
		for depth := tokenDepth(value); depth > 0; {
			token, err := dec.Token()
			if err != nil {
				return err
			}
			depth += tokenDepth(token)
		}
	}
	if end, err := dec.Token(); err != nil || end != json.Delim('}') {
		return errRegistry
	}
	if url != "" && !r.seen[url] {
		if r.seen == nil {
			r.seen = make(map[string]bool)
		}
		r.seen[url] = true
		r.urls = append(r.urls, url)
	}
	if unique && registryName.MatchString(owner.ID) && (owner.ProviderName == "" || registryName.MatchString(owner.ProviderName)) {
		r.owners = append(r.owners, owner)
	}
	return nil
}

func tokenDepth(token json.Token) int {
	switch token {
	case json.Delim('{'), json.Delim('['):
		return 1
	case json.Delim('}'), json.Delim(']'):
		return -1
	default:
		return 0
	}
}

func (m *marker) result(dir string) Result {
	var path string
	if len(m.Files) != 0 {
		path = filepath.Join(dir, m.Archive)
	}
	return Result{
		ArchivePath:       path,
		Complete:          m.State == complete,
		URLCount:          m.URLCount,
		KernelCleanupDone: m.KernelCleanupDone,
	}
}

type archiveDir struct {
	*os.Root
	validator *utils.PathValidator
	info      os.FileInfo
}

func (d archiveDir) checkLocation() error {
	info, err := os.Lstat(d.Name())
	if err != nil || !os.SameFile(d.info, info) {
		return errUnsafePath
	}
	return nil
}

func (d archiveDir) validate(name string) error {
	if !filepath.IsLocal(name) || filepath.Base(name) != name || name == "." {
		return errUnsafePath
	}
	path := filepath.Join(d.Name(), name)
	clean, err := d.validator.ValidateUnder(path, d.Name())
	if err != nil || clean != path {
		return errUnsafePath
	}
	return d.checkLocation()
}

func (d archiveDir) lookup(name string) (os.FileInfo, error) {
	if err := d.validate(name); err != nil {
		return nil, err
	}
	info, err := d.Root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && (!info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1) {
		return nil, errUnsafePath
	}
	return info, nil
}

func (d archiveDir) withDir(name string, info os.FileInfo, action func(archiveDir) (Result, error)) (Result, error) {
	if !info.IsDir() {
		return Result{}, errUnsafePath
	}
	root, err := d.Root.OpenRoot(name)
	if err != nil {
		return Result{}, err
	}
	defer root.Close()
	pinned, err := root.Stat(".")
	if err != nil {
		return Result{}, err
	}
	current, err := d.lookup(name)
	if err != nil || !os.SameFile(info, pinned) || !os.SameFile(info, current) {
		return Result{}, errUnsafePath
	}
	return action(archiveDir{root, d.validator, pinned})
}

func (d archiveDir) visitDir(name string, info os.FileInfo, action func(archiveDir) error) error {
	_, err := d.withDir(name, info, func(child archiveDir) (Result, error) {
		return Result{}, action(child)
	})
	return err
}

func (d archiveDir) openRegular(name string, info os.FileInfo) (*os.File, error) {
	if !info.Mode().IsRegular() {
		return nil, errUnsafePath
	}
	f, err := d.Root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if err := d.checkUnchanged(name, info, f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (d archiveDir) checkUnchanged(name string, expected os.FileInfo, f *os.File) error {
	pinned, err := f.Stat()
	if err != nil {
		return err
	}
	current, err := d.lookup(name)
	if err != nil || !os.SameFile(expected, pinned) || !os.SameFile(pinned, current) ||
		pinned.ModTime() != expected.ModTime() || current.ModTime() != expected.ModTime() ||
		pinned.Size() != expected.Size() || current.Size() != expected.Size() {
		return errChanged
	}
	return nil
}

func (d archiveDir) protectDir() error {
	if err := d.checkLocation(); err != nil {
		return err
	}
	f, err := d.Root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(0700)
}

func (d archiveDir) sync() error {
	if err := d.checkLocation(); err != nil {
		return err
	}
	f, err := d.Root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (d archiveDir) replace(name string, write func(*os.File) error) error {
	if info, err := d.lookup(name); err == nil && !info.Mode().IsRegular() {
		return errUnsafePath
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp := name + ".tmp"
	if info, err := d.lookup(tmp); err == nil {
		if !info.Mode().IsRegular() {
			return errUnsafePath
		}
		if err := d.Root.Remove(tmp); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := d.Root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	created, err := f.Stat()
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			if current, err := d.lookup(tmp); err == nil && os.SameFile(created, current) {
				_ = d.Root.Remove(tmp)
			}
		}
	}()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := d.checkUnchanged(tmp, info, f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := d.validate(name); err != nil {
		return err
	}
	if err := d.Root.Rename(tmp, name); err != nil {
		return err
	}
	renamed = true
	return d.sync()
}

func (d archiveDir) writeJSON(name string, value any) error {
	return d.replace(name, func(out *os.File) error {
		return json.NewEncoder(out).Encode(value)
	})
}

func (d archiveDir) privateFile(name string) (*os.File, error) {
	info, err := d.lookup(name)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm() != 0600 {
		return nil, errUnsafePath
	}
	return d.openRegular(name, info)
}

func (d archiveDir) readMarker() (*marker, error) {
	f, err := d.privateFile(markerName)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := new(marker)
	dec := json.NewDecoder(f)
	if err := dec.Decode(m); err != nil {
		return nil, errMarker
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errMarker
	}
	if m.Archive != archiveName || m.CreatedAt.IsZero() || m.URLCount < 0 ||
		(len(m.Files) == 0 && (m.State != complete || m.URLCount != 0)) ||
		(m.State != complete && (m.State != prepared || len(m.Sources) == 0)) {
		return nil, errMarker
	}
	return m, nil
}

func finish(data, legacy archiveDir, m *marker) error {
	for _, name := range []string{m.Archive, urlsName} {
		f, err := legacy.privateFile(name)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	entries, err := data.lookup(Directory)
	if err != nil {
		return err
	}
	pinned, err := legacy.Root.Stat(".")
	if err != nil || !os.SameFile(entries, pinned) || pinned.Mode().Perm() != 0700 {
		return errUnsafePath
	}
	if err := legacy.sync(); err != nil {
		return err
	}
	if err := data.sync(); err != nil {
		return err
	}
	for _, item := range m.Sources {
		if !validSource(item, "") {
			return errMarker
		}
	}
	if err := data.removeArchived(m.Sources, ""); err != nil {
		return err
	}
	done := *m
	done.State = complete
	done.Sources = nil
	if err := legacy.writeJSON(markerName, &done); err != nil {
		return err
	}
	*m = done
	return nil
}

func validSource(item source, parent string) bool {
	if item.Name == "." || !filepath.IsLocal(item.Name) || filepath.Base(item.Name) != item.Name {
		return false
	}
	switch filepath.Join(parent, item.Name) {
	case "cache":
		if !item.Directory || len(item.Children) != 1 || item.Children[0].Name != "providers" {
			return false
		}
	case "subscriptions", "cache/providers":
		if !item.Directory {
			return false
		}
	case "subscriptions.json", "subscriptions/subscriptions.json", "user_rules.json":
		if item.Directory {
			return false
		}
	default:
		if parent == "" {
			return false
		}
	}
	if !item.Directory {
		return len(item.Children) == 0 && len(item.Digest) == sha256.Size
	}
	if len(item.Digest) != 0 {
		return false
	}
	for _, child := range item.Children {
		if !validSource(child, filepath.Join(parent, item.Name)) {
			return false
		}
	}
	return true
}

func (d archiveDir) removeArchived(items []source, parent string) error {
	for _, item := range items {
		info, err := d.lookup(item.Name)
		if os.IsNotExist(err) {
			continue // Удаление успело завершиться до предыдущей остановки.
		}
		if err != nil {
			return err
		}
		if info.IsDir() != item.Directory || identityOf(info) != item.Identity {
			return errChanged
		}
		if item.Directory {
			if err := d.visitDir(item.Name, info, func(child archiveDir) error {
				return child.removeArchived(item.Children, filepath.Join(parent, item.Name))
			}); err != nil {
				return err
			}
			if parent == "" && item.Name == "cache" {
				continue
			}
		} else if err := d.verifyDigest(item, info); err != nil {
			return err
		}
		current, err := d.lookup(item.Name)
		if err != nil || !os.SameFile(info, current) || (!item.Directory &&
			(info.ModTime() != current.ModTime() || info.Size() != current.Size())) {
			return errChanged
		}
		// Remove удаляет только запись в закреплённом родителе. Новый файл в
		// каталоге переживёт сбой: непустой каталог нельзя удалить этим вызовом.
		if err := d.Root.Remove(item.Name); err != nil {
			return err
		}
	}
	return d.sync()
}

func (d archiveDir) verifyDigest(item source, info os.FileInfo) error {
	f, err := d.openRegular(item.Name, info)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return err
	}
	if !bytes.Equal(hash.Sum(nil), item.Digest) {
		return errChanged
	}
	return d.checkUnchanged(item.Name, info, f)
}

func archiveWarning(err error) string {
	switch {
	case errors.Is(err, errUnsafePath), errors.Is(err, syscall.ELOOP):
		return errUnsafePath.Error()
	case errors.Is(err, errChanged), errors.Is(err, syscall.ENOTEMPTY):
		return errChanged.Error()
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return "недостаточно места для архива старых данных"
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.EROFS):
		return "нет доступа к файлам архива старых данных"
	case errors.Is(err, errRegistry):
		return errRegistry.Error()
	case errors.Is(err, errMarker):
		return errMarker.Error()
	case errors.Is(err, errBusy):
		return errBusy.Error()
	default:
		return "ошибка файловой операции при архивации старых данных"
	}
}
