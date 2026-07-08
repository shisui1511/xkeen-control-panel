package services

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var snapshotIDRx = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// SnapshotMeta describes a stored config snapshot.
type SnapshotMeta struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	CreatedAt int64  `json:"created_at"`
	SizeBytes int64  `json:"size_bytes"`
}

// SnapshotService manages tar.gz snapshots of config directories.
type SnapshotService struct {
	dataDir    string
	configDirs []string // directories to include in snapshot
	mu         sync.Mutex
}

func NewSnapshotService(dataDir string, configDirs []string) *SnapshotService {
	return &SnapshotService{dataDir: dataDir, configDirs: configDirs}
}

func (s *SnapshotService) snapshotsDir() string {
	return filepath.Join(s.dataDir, "snapshots")
}

func (s *SnapshotService) ensureDir() error {
	return os.MkdirAll(s.snapshotsDir(), 0750)
}

// Create builds a tar.gz snapshot of all config dirs and saves it.
// label is an optional human-readable name.
func (s *SnapshotService) Create(label string) (SnapshotMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return SnapshotMeta{}, fmt.Errorf("snapshots dir: %w", err)
	}

	id := time.Now().Format("20060102-150405")
	baseID := id
	for i := 1; ; i++ {
		if _, err := os.Stat(filepath.Join(s.snapshotsDir(), id+".tar.gz")); os.IsNotExist(err) {
			break
		}
		id = fmt.Sprintf("%s-%d", baseID, i)
	}
	archivePath := filepath.Join(s.snapshotsDir(), id+".tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		return SnapshotMeta{}, fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	for _, dir := range s.configDirs {
		dir = filepath.Clean(dir)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		base := filepath.Base(dir)
		err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // skip unreadable entries
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil // skip symlinks
			}
			if d.IsDir() {
				if path == filepath.Join(s.dataDir, "snapshots") || path == filepath.Join(s.dataDir, "tmp") {
					return filepath.SkipDir
				}
			}
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".dat") {
				return nil // skip heavy geoip/geosite files
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			arcName := filepath.ToSlash(filepath.Join(base, rel))

			fi, err := d.Info()
			if err != nil {
				return nil
			}

			hdr, err := tar.FileInfoHeader(fi, "")
			if err != nil {
				return nil
			}
			hdr.Name = arcName
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if !d.IsDir() {
				sf, err := os.Open(path)
				if err != nil {
					return nil
				}
				defer sf.Close()
				_, err = io.Copy(tw, sf)
				return err
			}
			return nil
		})
		if err != nil {
			tw.Close()
			gw.Close()
			os.Remove(archivePath)
			return SnapshotMeta{}, fmt.Errorf("walk %s: %w", dir, err)
		}
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		os.Remove(archivePath)
		return SnapshotMeta{}, err
	}
	if err := gw.Close(); err != nil {
		os.Remove(archivePath)
		return SnapshotMeta{}, err
	}
	// Flush and stat to get size
	f.Close()

	fi, err := os.Stat(archivePath)
	if err != nil {
		return SnapshotMeta{}, err
	}

	meta := SnapshotMeta{
		ID:        id,
		Label:     label,
		CreatedAt: time.Now().Unix(),
		SizeBytes: fi.Size(),
	}

	// Persist metadata
	if err := s.saveMeta(id, meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func (s *SnapshotService) metaPath(id string) string {
	return filepath.Join(s.snapshotsDir(), filepath.Base(filepath.Clean(id))+".json")
}

func (s *SnapshotService) archivePath(id string) string {
	return filepath.Join(s.snapshotsDir(), filepath.Base(filepath.Clean(id))+".tar.gz")
}

func (s *SnapshotService) saveMeta(id string, meta SnapshotMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return utils.AtomicWriteFile(s.metaPath(id), data, 0600)
}

// List returns all snapshots sorted newest-first.
func (s *SnapshotService) List() ([]SnapshotMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.snapshotsDir())
	if err != nil {
		return nil, err
	}
	var metas []SnapshotMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.snapshotsDir(), e.Name()))
		if err != nil {
			continue
		}
		var m SnapshotMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].CreatedAt > metas[j].CreatedAt
	})
	return metas, nil
}

// ArchivePath returns the absolute path to the tar.gz for streaming.
// Returns error if the archive does not exist.
func (s *SnapshotService) ArchivePath(id string) (string, error) {
	if !snapshotIDRx.MatchString(id) {
		return "", fmt.Errorf("invalid snapshot id")
	}
	p := s.archivePath(id)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("snapshot not found")
	}
	return p, nil
}

// Restore extracts a snapshot archive back to the original config dirs.
// Only files whose path prefix matches a known config dir base name are restored.
func (s *SnapshotService) Restore(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !snapshotIDRx.MatchString(id) {
		return fmt.Errorf("invalid snapshot id")
	}
	archPath, err := s.ArchivePath(id)
	if err != nil {
		return err
	}

	f, err := os.Open(archPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// 1. Create temporary restore directory
	tmpParentDir := filepath.Join(s.dataDir, "tmp")
	if err := os.MkdirAll(tmpParentDir, 0750); err != nil {
		return fmt.Errorf("create temp parent dir: %w", err)
	}
	tmpRestoreDir, err := os.MkdirTemp(tmpParentDir, "restore-*")
	if err != nil {
		return fmt.Errorf("create temp restore dir: %w", err)
	}
	defer os.RemoveAll(tmpRestoreDir) // 6. Always clean up

	// Build map: base name → full path
	dirMap := make(map[string]string)
	for _, d := range s.configDirs {
		dirMap[filepath.Base(d)] = filepath.Clean(d)
	}

	// 3. Extract to tmpRestoreDir with Zip Slip check
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Only allow regular files and directories
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeDir {
			continue // skip symlinks, hardlinks, devices, etc.
		}

		if strings.Contains(hdr.Name, "..") {
			return fmt.Errorf("invalid path in archive (Zip Slip prevention)")
		}

		destPath := filepath.Join(tmpRestoreDir, filepath.FromSlash(hdr.Name))
		cleanDest := filepath.Clean(destPath)
		cleanTmp := filepath.Clean(tmpRestoreDir)
		if !strings.HasPrefix(cleanDest, cleanTmp+string(filepath.Separator)) && cleanDest != cleanTmp {
			return fmt.Errorf("invalid path in archive (Zip Slip prevention)")
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("create temp dir %s: %w", destPath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
			return fmt.Errorf("create temp subdir: %w", err)
		}

		if hdr.Size > 10*1024*1024 {
			return fmt.Errorf("file %s exceeds maximum allowed size of 10 MB", hdr.Name)
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode().Perm())
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		// Write with size limit: 10 MB per file
		if _, err := io.Copy(out, io.LimitReader(tr, 10*1024*1024)); err != nil {
			out.Close()
			return fmt.Errorf("write temp file: %w", err)
		}
		out.Close()
	}

	// 4. Validate structure: check that we have at least one valid folder
	entries, err := os.ReadDir(tmpRestoreDir)
	if err != nil {
		return fmt.Errorf("read temp restore dir: %w", err)
	}
	hasValidFolder := false
	for _, entry := range entries {
		if entry.IsDir() {
			name := entry.Name()
			if _, ok := dirMap[name]; ok {
				hasValidFolder = true
				break
			}
		}
	}
	if !hasValidFolder {
		return fmt.Errorf("invalid backup structure: no valid configuration directories found")
	}

	// 5. Copy files from temp directory to target directories
	for name, targetDir := range dirMap {
		srcSubDir := filepath.Join(tmpRestoreDir, name)
		info, err := os.Stat(srcSubDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // this directory wasn't in the snapshot, skip
			}
			return err
		}
		if !info.IsDir() {
			continue
		}

		// Copy recursively
		err = filepath.WalkDir(srcSubDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(srcSubDir, path)
			if err != nil {
				return err
			}
			targetPath := filepath.Join(targetDir, rel)

			// Safety: when writing to xcp/ (which is s.dataDir), do not overwrite or delete the snapshots/ folder!
			if name == filepath.Base(s.dataDir) {
				if rel == "snapshots" || strings.HasPrefix(rel, "snapshots"+string(filepath.Separator)) {
					return nil // do not touch target snapshots directory
				}
				if rel == "tmp" || strings.HasPrefix(rel, "tmp"+string(filepath.Separator)) {
					return nil // do not touch target tmp directory
				}
			}

			if d.IsDir() {
				return os.MkdirAll(targetPath, 0750)
			}

			// Read file info to preserve permissions if possible
			fi, err := d.Info()
			if err != nil {
				return err
			}

			// Write file
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
			if err != nil {
				in.Close()
				return err
			}
			_, err = io.Copy(out, in)
			in.Close()
			out.Close()
			return err
		})
		if err != nil {
			return fmt.Errorf("restore directory %s: %w", name, err)
		}
	}

	return nil
}

// SaveUploaded receives a reader for a tar.gz upload, limits it to 15 MB,
// and saves it to the snapshots directory.
func (s *SnapshotService) SaveUploaded(r io.Reader, filename string) (SnapshotMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureDir(); err != nil {
		return SnapshotMeta{}, fmt.Errorf("snapshots dir: %w", err)
	}

	id := time.Now().Format("20060102-150405")
	baseID := id
	for i := 1; ; i++ {
		if _, err := os.Stat(s.archivePath(id)); os.IsNotExist(err) {
			break
		}
		id = fmt.Sprintf("%s-%d", baseID, i)
	}
	archivePath := s.archivePath(id)

	f, err := os.Create(archivePath)
	if err != nil {
		return SnapshotMeta{}, fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()

	// Limit to 15 MB
	limitReader := io.LimitReader(r, 15*1024*1024)
	written, err := io.Copy(f, limitReader)
	if err != nil {
		os.Remove(archivePath)
		return SnapshotMeta{}, fmt.Errorf("write archive: %w", err)
	}

	// Check if we hit the limit: if there is more data in r
	var oneByte [1]byte
	if n, _ := r.Read(oneByte[:]); n > 0 {
		os.Remove(archivePath)
		return SnapshotMeta{}, fmt.Errorf("uploaded file exceeds maximum size of 15 MB")
	}

	meta := SnapshotMeta{
		ID:        id,
		Label:     "Загружен: " + filename,
		CreatedAt: time.Now().Unix(),
		SizeBytes: written,
	}

	if err := s.saveMeta(id, meta); err != nil {
		os.Remove(archivePath)
		return SnapshotMeta{}, err
	}

	return meta, nil
}

// Delete removes a snapshot archive and its metadata.
func (s *SnapshotService) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Sanitize id: only alphanumeric + dash
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid snapshot id")
		}
	}
	if _, err := s.ArchivePath(id); err != nil {
		return err
	}
	os.Remove(s.archivePath(id))
	os.Remove(s.metaPath(id))
	return nil
}
