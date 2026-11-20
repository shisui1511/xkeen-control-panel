package legacyarchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

const (
	cleanupJournal = "kernel-clean.json"
	backupReason   = "legacy_cleanup"
)

var (
	errValidation = errors.New("ядро не подтвердило конфигурацию после очистки")
	errRollback   = errors.New("не удалось восстановить весь набор файлов из копии")
	errRestart    = errors.New("не удалось применить очищенную конфигурацию")
)

// Cleanup использует существующий цикл сверки слоя и общий замок применения.
type Cleanup struct {
	DataDir  string
	Roots    configlayer.Roots
	Binaries func() configlayer.Binaries
	XrayEnv  func(string) []string

	// noticesMu охраняет lastNotices — последний набор уведомлений, который
	// Notices отдаёт, пока замок данных держит очистка или запуск.
	noticesMu   sync.Mutex
	lastNotices []configlayer.NoticeView
}

// Scan ищет следы старой панели, дожидаясь общего замка данных.
func (c *Cleanup) Scan() []Finding {
	findings, err := c.scan(lockWait)
	if err != nil {
		findings = append(findings, Finding{File: Directory, Reason: archiveWarning(err)})
	}
	return findings
}

func (c *Cleanup) scan(mode lockMode) ([]Finding, error) {
	var findings []Finding
	_, err := withDataMode(c.DataDir, mode, func(data archiveDir) (Result, error) {
		stored, err := readKernelMarker(data)
		if err != nil {
			return Result{}, err
		}
		for _, kernel := range []string{configlayer.KernelXray, configlayer.KernelMihomo} {
			tree, err := c.openTree(kernel)
			switch {
			case os.IsNotExist(err):
			case err != nil:
				findings = append(findings, Finding{Kernel: kernel, File: ".", Reason: errUnsafePath.Error()})
			default:
				seen, _ := tree.scan(stored, false)
				tree.Close()
				findings = append(findings, seen...)
			}
		}
		return Result{}, nil
	})
	return findings, err
}

// Notices переводит находки в одно предупреждение без содержимого файлов.
// Слой зовёт её под замком сверки, поэтому замок данных берётся без ожидания:
// пока его держат очистка или запуск (проверка ядром и перезапуск), сверки не
// встают, а Notices отдаёт прежний набор уведомлений.
func (c *Cleanup) Notices() []configlayer.NoticeView {
	findings, err := c.scan(lockTry)
	c.noticesMu.Lock()
	defer c.noticesMu.Unlock()
	if errors.Is(err, errBusy) {
		return c.lastNotices
	}
	if err != nil {
		findings = append(findings, Finding{File: Directory, Reason: archiveWarning(err)})
	}
	c.lastNotices = noticesFor(findings)
	return c.lastNotices
}

func noticesFor(findings []Finding) []configlayer.NoticeView {
	if len(findings) == 0 {
		return nil
	}
	var files, blocked []string
	for _, f := range findings {
		name := f.File
		if f.Kernel != "" {
			name = f.Kernel + "/" + name
		}
		files = append(files, name)
		if f.Reason != "" {
			blocked = append(blocked, name+" — "+f.Reason)
		}
	}
	return []configlayer.NoticeView{{
		ID: configlayer.LegacyLeftoversNotice, Kind: "warning",
		Params: map[string]string{"files": strings.Join(files, "\n"), "blocked": strings.Join(blocked, "\n")},
	}}
}

// Startup восстанавливает прерванную запись и один раз решает, можно ли снять
// файлы выбора без остальных вставок. Отказ проверки оставляет их для кнопки.
func (c *Cleanup) Startup(ctx context.Context, restart configlayer.LegacyRestart) error {
	return c.run(ctx, true, restart)
}

// Clean предназначена для вызова под TryBegin. Её план строится из текущих
// файлов, а перед записью сверяются байты, идентичность и алиасы.
func (c *Cleanup) Clean(ctx context.Context, restart configlayer.LegacyRestart) error {
	return c.run(ctx, false, restart)
}

type kernelEdits struct {
	tree  *kernelTree
	files []*kernelFile
}

func (c *Cleanup) run(ctx context.Context, startup bool, restart configlayer.LegacyRestart) error {
	_, err := withData(c.DataDir, func(data archiveDir) (Result, error) {
		if err := c.recover(data); err != nil {
			return Result{}, errRollback
		}
		stored, err := readKernelMarker(data)
		if err != nil || stored != nil && stored.State != complete {
			return Result{}, errMarker
		}
		if startup && stored != nil && stored.KernelCleanupDone {
			return Result{}, nil
		}
		var edits []kernelEdits
		defer func() {
			for _, e := range edits {
				e.tree.Close()
			}
		}()
		kernels := []string{configlayer.KernelXray}
		if !startup {
			kernels = append(kernels, configlayer.KernelMihomo)
		}
		for _, kernel := range kernels {
			tree, err := c.openTree(kernel)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				if startup {
					break
				}
				// Чужой корень пропускается целиком. Находка остаётся в сверке.
				continue
			}
			_, files := tree.scan(stored, startup)
			if len(files) == 0 {
				tree.Close()
				continue
			}
			edits = append(edits, kernelEdits{tree, files})
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		validation := c.validate(ctx, edits, startup)
		if startup && validation == nil && len(edits) != 0 && edits[0].hasDependentRules() {
			validation = errValidation
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if validation != nil && !startup {
			return Result{}, validation
		}
		if !startup && len(edits) == 0 {
			return Result{}, nil
		}
		return inLegacy(data, true, func(legacy archiveDir) (Result, error) {
			if stored == nil {
				stored = &marker{State: complete, Archive: archiveName, CreatedAt: time.Now().UTC()}
			}
			if startup {
				stored.KernelCleanupDone = true
				if err := legacy.writeJSON(markerName, stored); err != nil {
					return Result{}, err
				}
			}
			if validation != nil || len(edits) == 0 {
				return Result{}, nil
			}
			return Result{}, c.apply(ctx, data, legacy, stored, edits, restart)
		})
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errValidation), errors.Is(err, errRollback), errors.Is(err, errRestart),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return errors.New(archiveWarning(err))
	}
}

func (e kernelEdits) hasDependentRules() bool {
	tags := make(map[string]bool)
	for _, file := range e.files {
		for _, tag := range outboundTags(file.before) {
			tags[tag] = true
		}
	}
	file, err := e.tree.openConfig(xrayRules)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return true
	}
	type rules struct {
		Rules []struct {
			Own      bool   `json:"user_rule"`
			Outbound string `json:"outboundTag"`
		} `json:"rules"`
	}
	var value struct {
		rules
		Routing *rules `json:"routing"`
	}
	if json.Unmarshal(file.before, &value) != nil {
		return true
	}
	list := &value.rules
	if value.Routing != nil {
		list = value.Routing
	}
	for _, rule := range list.Rules {
		if rule.Own && tags[rule.Outbound] {
			return true
		}
	}
	return false
}

func (c *Cleanup) openTree(kernel string) (*kernelTree, error) {
	switch kernel {
	case configlayer.KernelXray:
		return openKernelTree(kernel, c.Roots.Xray)
	case configlayer.KernelMihomo:
		return openKernelTree(kernel, c.Roots.Mihomo)
	default:
		return nil, errMarker
	}
}

func inLegacy(data archiveDir, create bool, action func(archiveDir) (Result, error)) (Result, error) {
	info, err := data.lookup(Directory)
	if create && os.IsNotExist(err) {
		if err := data.Root.Mkdir(Directory, 0700); err != nil {
			return Result{}, err
		}
		if err := data.sync(); err != nil {
			return Result{}, err
		}
		info, err = data.lookup(Directory)
	}
	if err != nil {
		return Result{}, err
	}
	if info.Mode().Perm() != 0700 {
		return Result{}, errUnsafePath
	}
	return data.withDir(Directory, info, action)
}

func readKernelMarker(data archiveDir) (*marker, error) {
	var stored *marker
	_, err := inLegacy(data, false, func(legacy archiveDir) (Result, error) {
		var err error
		stored, err = legacy.readMarker()
		return Result{}, err
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return stored, err
}

func (c *Cleanup) validate(ctx context.Context, edits []kernelEdits, startup bool) error {
	if len(edits) == 0 {
		return nil
	}
	var bins configlayer.Binaries
	if c.Binaries != nil {
		bins = c.Binaries()
	}
	for _, e := range edits {
		tmp, err := os.MkdirTemp("", "xcp-legacy-check-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if err := e.mirror(tmp); err != nil {
			return err
		}
		var result configlayer.ValidationResult
		if e.tree.kernel == configlayer.KernelXray {
			result = configlayer.ValidateXrayTree(ctx, bins.Xray, tmp, e.tree.Name(), c.XrayEnv)
		} else {
			result = configlayer.ValidateMihomoTree(ctx, bins.Mihomo, tmp, e.tree.Name())
		}
		if result.Skipped && result.NoteCode == configlayer.NoteKernelNotInstalled && !startup {
			// Ядро не установлено: запускать нечего, а кнопка не должна навсегда
			// оставлять файлы старой панели. Автоматический шаг D-03 их не трогает.
			continue
		}
		if !result.OK {
			// Вывод ядра может содержать адреса и параметры конфигурации.
			return errValidation
		}
	}
	return nil
}

// mirror собирает копию так же, как проверка применения слоя: Xray получает
// все *.json верхнего уровня с наложенными правками, Mihomo — config.yaml после
// правки и подключённые геобазы и провайдеры. Чтение идёт через закреплённый
// корень, копия живёт в приватном временном каталоге.
func (e kernelEdits) mirror(tmp string) error {
	if e.tree.kernel == configlayer.KernelMihomo {
		if err := configlayer.MirrorMihomo(e.tree.path, e.files[0].after, tmp); err != nil {
			return err
		}
		return e.tree.checkLocation()
	}
	byName := make(map[string]*kernelFile, len(e.files))
	for _, file := range e.files {
		byName[file.name] = file
	}
	dir, err := e.tree.Root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		var content []byte
		if file, changed := byName[name]; changed {
			if file.after == nil {
				continue
			}
			content = file.after
		} else if content, err = e.tree.Root.ReadFile(name); errors.Is(err, os.ErrNotExist) {
			continue // оборванная ссылка ядром тоже не читается
		} else if err != nil {
			// Ссылка наружу корня: проверка не строится по чужому файлу.
			return errUnsafePath
		}
		if err := os.WriteFile(filepath.Join(tmp, name), content, 0600); err != nil {
			return err
		}
	}
	return e.tree.checkLocation()
}

type rollbackFile struct {
	Kernel, Name, Target  string
	Root, Alias           identity
	Mode                  os.FileMode
	UID, GID              int
	BeforeHash, AfterHash string
}

type cleanupRecord struct {
	Backup string
	Files  []rollbackFile
}

func (c *Cleanup) apply(ctx context.Context, data, legacy archiveDir, stored *marker, edits []kernelEdits, restart configlayer.LegacyRestart) error {
	var removed []*kernelFile
	for _, e := range edits {
		for _, file := range e.files {
			if err := e.tree.verify(file); err != nil {
				return err
			}
			if file.after == nil {
				removed = append(removed, file)
			}
		}
	}
	if len(removed) != 0 {
		if err := appendKernelArchive(legacy, stored, removed); err != nil {
			return err
		}
	}

	record, err := backupEdits(data, edits)
	if err != nil {
		return err
	}
	if err := legacy.writeJSON(cleanupJournal, record); err != nil {
		return err
	}
	var recoverRuntime []func() error
	written := 0
	rollback := func(cause error) error {
		trees := make(map[string]*kernelTree, len(edits))
		for _, e := range edits {
			trees[e.tree.kernel] = e.tree
		}
		if written != 0 {
			partial := cleanupRecord{Backup: record.Backup, Files: record.Files[:written]}
			if err := c.restore(data, partial, trees); err != nil {
				return errRollback
			}
		}
		if err := removePrivate(legacy, cleanupJournal); err != nil {
			return errRollback
		}
		for _, recoverCore := range recoverRuntime {
			if err := recoverCore(); err != nil {
				cause = errRestart
			}
		}
		return cause
	}
	for _, e := range edits {
		for _, file := range e.files {
			if err := ctx.Err(); err != nil {
				return rollback(err)
			}
			changed, err := e.tree.writeEdit(file)
			if changed {
				written++
			}
			if err != nil {
				return rollback(err)
			}
		}
	}
	if restart != nil {
		for _, e := range edits {
			recoverCore, err := restart(ctx, e.tree.kernel)
			if recoverCore != nil {
				recoverRuntime = append(recoverRuntime, recoverCore)
			}
			if err != nil {
				return rollback(errRestart)
			}
		}
	}
	// Снятие журнала фиксирует успешную операцию. До этого момента прерванная
	// запись откатывается из того же набора при следующем старте.
	if err := removePrivate(legacy, cleanupJournal); err != nil {
		return rollback(err)
	}
	return nil
}

func (tree *kernelTree) verify(file *kernelFile) error {
	current, err := tree.openConfig(file.name)
	if err != nil || !os.SameFile(file.info, current.info) || current.target != file.target ||
		!sameOptionalFile(file.alias, current.alias) || !bytes.Equal(current.before, file.before) ||
		current.info.Mode() != file.info.Mode() {
		return errChanged
	}
	return nil
}

func sameOptionalFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b)
}

func (tree *kernelTree) writeEdit(file *kernelFile) (bool, error) {
	check := func() error { return tree.verify(file) }
	if file.after != nil {
		uid, gid := ownerIDs(file.info)
		return replaceKernelFile(tree.Root, file.target, file.after, file.info.Mode().Perm(), uid, gid, check, false)
	}
	if err := check(); err != nil {
		return false, err
	}
	if err := tree.Root.Remove(file.target); err != nil {
		return false, err
	}
	return true, syncRoot(tree.Root)
}

func ownerIDs(info os.FileInfo) (int, int) {
	st := info.Sys().(*syscall.Stat_t)
	return int(st.Uid), int(st.Gid)
}

func replaceKernelFile(root *os.Root, name string, data []byte, mode os.FileMode, uid, gid int, check func() error, absent bool) (bool, error) {
	tmp := ".xcp-legacy-" + rand.Text()
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	defer func() {
		if current, err := root.Lstat(tmp); err == nil && os.SameFile(info, current) {
			_ = root.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return false, err
	}
	if ownUID, ownGID := ownerIDs(info); uid != ownUID || gid != ownGID {
		if err := f.Chown(uid, gid); err != nil {
			return false, err
		}
	}
	if err := f.Chmod(mode); err != nil {
		return false, err
	}
	if err := f.Sync(); err != nil {
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := check(); err != nil {
		return false, err
	}
	if current, err := root.Lstat(tmp); err != nil || !os.SameFile(info, current) {
		return false, errChanged
	}
	if absent {
		if err := root.Link(tmp, name); err != nil {
			return false, err
		}
		if err := root.Remove(tmp); err != nil {
			return true, err
		}
	} else if err := root.Rename(tmp, name); err != nil {
		return false, err
	}
	return true, syncRoot(root)
}

func syncRoot(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func validBackupRoot(data archiveDir) error {
	root := data
	for _, name := range []string{"backup", "config-layer"} {
		info, err := root.lookup(name)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || !info.IsDir() {
			return errUnsafePath
		}
		child, err := root.Root.OpenRoot(name)
		if err != nil {
			return err
		}
		defer child.Close()
		root = archiveDir{child, data.validator, info}
		if pinned, err := child.Stat("."); err != nil || !os.SameFile(info, pinned) || root.checkLocation() != nil {
			return errUnsafePath
		}
	}
	return data.checkLocation()
}

func backupEdits(data archiveDir, edits []kernelEdits) (*cleanupRecord, error) {
	if err := validBackupRoot(data); err != nil {
		return nil, err
	}
	set, err := configlayer.NewBackupSet(data.Name(), time.Now(), backupReason)
	if err != nil {
		return nil, err
	}
	record := &cleanupRecord{Backup: filepath.Base(set.Dir)}
	for _, e := range edits {
		for _, file := range e.files {
			if err := set.SaveContent(e.tree.kernel, file.target, backupReason, file.before); err != nil {
				return nil, err
			}
			uid, gid := ownerIDs(file.info)
			r := rollbackFile{
				Kernel: e.tree.kernel, Name: file.name, Target: file.target,
				Root: identityOf(e.tree.info),
				Mode: file.info.Mode().Perm(), UID: uid, GID: gid,
				BeforeHash: legacyDigest(file.before),
			}
			if file.alias != nil {
				r.Alias = identityOf(file.alias)
			}
			if file.after != nil {
				r.AfterHash = legacyDigest(file.after)
			}
			record.Files = append(record.Files, r)
		}
		if err := syncDirectory(filepath.Join(set.Dir, e.tree.kernel)); err != nil {
			return nil, err
		}
	}
	if err := set.WriteMeta(); err != nil {
		return nil, err
	}
	for dir := set.Dir; dir != data.Name(); dir = filepath.Dir(dir) {
		if err := syncDirectory(dir); err != nil {
			return nil, err
		}
	}
	return record, data.sync()
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (c *Cleanup) recover(data archiveDir) error {
	_, err := inLegacy(data, false, func(legacy archiveDir) (Result, error) {
		f, err := legacy.privateFile(cleanupJournal)
		if os.IsNotExist(err) {
			return Result{}, nil
		}
		if err != nil {
			return Result{}, err
		}
		defer f.Close()
		var record cleanupRecord
		if err := json.NewDecoder(f).Decode(&record); err != nil {
			return Result{}, errMarker
		}
		if err := c.restore(data, record, nil); err != nil {
			return Result{}, err
		}
		return Result{}, removePrivate(legacy, cleanupJournal)
	})
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *Cleanup) restore(data archiveDir, record cleanupRecord, pinned map[string]*kernelTree) error {
	if !filepath.IsLocal(record.Backup) || filepath.Base(record.Backup) != record.Backup ||
		!strings.HasPrefix(record.Backup, "apply-") || len(record.Files) == 0 {
		return errMarker
	}
	var failed error
	for i := len(record.Files) - 1; i >= 0; i-- {
		r := record.Files[i]
		err := c.restoreOne(data, record.Backup, r, pinned[r.Kernel])
		failed = errors.Join(failed, err)
	}
	return failed
}

func (c *Cleanup) restoreOne(data archiveDir, backup string, r rollbackFile, tree *kernelTree) error {
	if !filepath.IsLocal(r.Name) || filepath.Base(r.Name) != r.Name ||
		!filepath.IsLocal(r.Target) || filepath.Base(r.Target) != r.Target {
		return errMarker
	}
	if tree == nil {
		var err error
		tree, err = c.openTree(r.Kernel)
		if err != nil {
			return err
		}
		defer tree.Close()
	}
	if r.Root != identityOf(tree.info) {
		return errUnsafePath
	}
	var current os.FileInfo
	absent := false
	check := func() error {
		if r.Alias != (identity{}) {
			info, err := tree.Root.Lstat(r.Name)
			target, linkErr := tree.Root.Readlink(r.Name)
			if err != nil || linkErr != nil || identityOf(info) != r.Alias || target != r.Target {
				return errChanged
			}
		}
		info, err := tree.Root.Lstat(r.Target)
		if absent && os.IsNotExist(err) {
			return nil
		}
		if err != nil || !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 ||
			!os.SameFile(current, info) || current.ModTime() != info.ModTime() || current.Size() != info.Size() {
			return errChanged
		}
		return nil
	}
	var err error
	current, err = tree.Root.Lstat(r.Target)
	switch {
	case os.IsNotExist(err) && r.AfterHash == "":
		absent = true
	case err != nil:
		return err
	default:
		if err := check(); err != nil {
			return err
		}
		f, err := tree.Root.OpenFile(r.Target, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !os.SameFile(current, info) {
			return errChanged
		}
		content, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		hash := legacyDigest(content)
		if hash == r.BeforeHash {
			return check()
		}
		if hash != r.AfterHash {
			return errChanged
		}
	}
	if err := check(); err != nil {
		return err
	}
	var original []byte
	err = readBackup(data, []string{"backup", "config-layer", backup, r.Kernel}, r.Target, &original)
	if err != nil || legacyDigest(original) != r.BeforeHash {
		return errMarker
	}
	_, err = replaceKernelFile(tree.Root, r.Target, original, r.Mode.Perm(), r.UID, r.GID, check, absent)
	return err
}

func readBackup(root archiveDir, dirs []string, name string, out *[]byte) error {
	if len(dirs) == 0 {
		f, err := root.privateFile(name)
		if err != nil {
			return err
		}
		defer f.Close()
		*out, err = io.ReadAll(f)
		return err
	}
	info, err := root.lookup(dirs[0])
	if err != nil {
		return err
	}
	return root.visitDir(dirs[0], info, func(child archiveDir) error {
		return readBackup(child, dirs[1:], name, out)
	})
}

func removePrivate(legacy archiveDir, name string) error {
	info, err := legacy.lookup(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errUnsafePath
	}
	if err := legacy.Root.Remove(name); err != nil {
		return err
	}
	return legacy.sync()
}

func appendKernelArchive(legacy archiveDir, stored *marker, files []*kernelFile) error {
	var input *os.File
	var err error
	input, err = legacy.privateFile(archiveName)
	if err == nil {
		defer input.Close()
	} else if !os.IsNotExist(err) || len(stored.Files) != 0 {
		return err
	}
	names := make(map[string]bool, len(stored.Files)+len(files))
	for _, name := range stored.Files {
		names[name] = true
	}
	err = legacy.replace(archiveName, func(out *os.File) error {
		gz := gzip.NewWriter(out)
		tw := tar.NewWriter(gz)
		var existing map[string]bool
		if input != nil {
			var err error
			existing, err = copyArchive(input, tw)
			if err != nil {
				return err
			}
		}
		for _, file := range files {
			name := "kernels/xray/" + legacyDigest(file.before) + "/" + file.name
			if !existing[name] {
				header := &tar.Header{Name: name, Mode: 0600, Size: int64(len(file.before)), ModTime: file.info.ModTime()}
				if err := tw.WriteHeader(header); err != nil {
					return err
				}
				if _, err := tw.Write(file.before); err != nil {
					return err
				}
			}
			if !names[name] {
				stored.Files = append(stored.Files, name)
				names[name] = true
			}
		}
		return errors.Join(tw.Close(), gz.Close())
	})
	if err != nil {
		return err
	}
	return legacy.writeJSON(markerName, stored)
}

func copyArchive(in io.Reader, out *tar.Writer) (map[string]bool, error) {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	existing := make(map[string]bool)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			_, err = io.Copy(io.Discard, gz)
			return existing, err
		}
		if err != nil {
			return nil, err
		}
		if err := out.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, reader); err != nil {
			return nil, err
		}
		existing[header.Name] = true
	}
}
