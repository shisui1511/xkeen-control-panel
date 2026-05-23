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
	"sort"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

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
	if err := s.ensureDir(); err != nil {
		return SnapshotMeta{}, fmt.Errorf("snapshots dir: %w", err)
	}

	id := time.Now().Format("20060102-150405")
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
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			arcName := filepath.Join(base, rel)

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
	return filepath.Join(s.snapshotsDir(), id+".json")
}

func (s *SnapshotService) archivePath(id string) string {
	return filepath.Join(s.snapshotsDir(), id+".tar.gz")
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
	// Sanitize id: only alphanumeric + dash
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-') {
			return "", fmt.Errorf("invalid snapshot id")
		}
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

	// Build map: base name → full path
	dirMap := make(map[string]string)
	for _, d := range s.configDirs {
		dirMap[filepath.Base(d)] = filepath.Clean(d)
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// hdr.Name = "configs/file.json" or "mihomo/config.yaml"
		parts := strings.SplitN(filepath.ToSlash(hdr.Name), "/", 2)
		if len(parts) < 2 {
			continue
		}
		baseDir, rel := parts[0], parts[1]
		if rel == "" {
			continue // directory entry
		}

		destRoot, ok := dirMap[baseDir]
		if !ok {
			continue // unknown dir, skip
		}

		destPath := filepath.Join(destRoot, filepath.FromSlash(rel))
		// Safety: ensure dest is within destRoot
		if !strings.HasPrefix(filepath.Clean(destPath), destRoot+string(filepath.Separator)) &&
			filepath.Clean(destPath) != destRoot {
			continue
		}

		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
			return err
		}
		// Write with size limit: 10 MB per file
		out, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, 10*1024*1024)); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	return nil
}

// Delete removes a snapshot archive and its metadata.
func (s *SnapshotService) Delete(id string) error {
	if _, err := s.ArchivePath(id); err != nil {
		return err
	}
	os.Remove(s.archivePath(id))
	os.Remove(s.metaPath(id))
	return nil
}
