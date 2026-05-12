package services

import (
"fmt"
"io"
"net/http"
"os"
"path/filepath"
"strings"
"sync"
"time"
)

type DATFile struct {
Name       string `json:"name"`
Path       string `json:"path"`
Size       int64  `json:"size"`
LastUpdate int64  `json:"last_update"`
Exists     bool   `json:"exists"`
Type       string `json:"type"` // "xray" or "mihomo"
}

type DATManagerService struct {
xrayDir   string
mihomoDir string
mu        sync.RWMutex
}

func NewDATManagerService(dirs ...string) *DATManagerService {
	xrayDir := "/opt/etc/xray/dat"
	mihomoDir := "/opt/etc/mihomo"

	if len(dirs) > 0 && dirs[0] != "" {
		xrayDir = dirs[0]
	}
	if len(dirs) > 1 && dirs[1] != "" {
		mihomoDir = dirs[1]
	}

	return &DATManagerService{
		xrayDir:   xrayDir,
		mihomoDir: mihomoDir,
	}
}

func (s *DATManagerService) List() []DATFile {
var files []DATFile

s.mu.RLock()
defer s.mu.RUnlock()

// Scan Xray
if matches, err := filepath.Glob(filepath.Join(s.xrayDir, "*.dat")); err == nil {
for _, match := range matches {
f := DATFile{Name: filepath.Base(match), Path: match, Exists: true, Type: "xray"}
if info, err := os.Stat(match); err == nil {
f.Size = info.Size()
f.LastUpdate = info.ModTime().Unix()
}
files = append(files, f)
}
}

// Scan Mihomo .dat
if matches, err := filepath.Glob(filepath.Join(s.mihomoDir, "*.dat")); err == nil {
for _, match := range matches {
f := DATFile{Name: filepath.Base(match), Path: match, Exists: true, Type: "mihomo"}
if info, err := os.Stat(match); err == nil {
f.Size = info.Size()
f.LastUpdate = info.ModTime().Unix()
}
files = append(files, f)
}
}

// Scan Mihomo .mmdb
if matches, err := filepath.Glob(filepath.Join(s.mihomoDir, "*.mmdb")); err == nil {
for _, match := range matches {
f := DATFile{Name: filepath.Base(match), Path: match, Exists: true, Type: "mihomo"}
if info, err := os.Stat(match); err == nil {
f.Size = info.Size()
f.LastUpdate = info.ModTime().Unix()
}
files = append(files, f)
}
}

return files
}

func (s *DATManagerService) UpdateCustom(localPath string, remoteURL string) (int64, error) {
s.mu.Lock()
defer s.mu.Unlock()

	// Safety check: resolve to absolute path and ensure it's within allowed directories.
	cleanPath := filepath.Clean(localPath)
	targetPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return 0, fmt.Errorf("invalid file path")
	}

	xrayAbs, err := filepath.Abs(s.xrayDir)
	if err != nil {
		return 0, fmt.Errorf("invalid service directory")
	}
	mihomoAbs, err := filepath.Abs(s.mihomoDir)
	if err != nil {
		return 0, fmt.Errorf("invalid service directory")
	}


	// Robust path validation: must be within one of the managed directories
	isInside := func(base, target string) bool {
		rel, err := filepath.Rel(base, target)
		if err != nil {
			return false
		}
		// Must not be outside (..) and not the base itself (.)
		return rel != "." && rel != ".." && !filepath.IsAbs(rel) && 
			(len(rel) < 2 || rel[:2] != "..") && 
			!strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}

	if !isInside(xrayAbs, targetPath) && !isInside(mihomoAbs, targetPath) {
		return 0, fmt.Errorf("invalid file path: outside allowed directories")
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(remoteURL)
	if err != nil {
		return 0, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	tmpFile := targetPath + ".tmp"
	out, err := os.Create(tmpFile)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp file: %w", err)
	}

	written, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpFile)
		return 0, fmt.Errorf("failed to write file: %w", err)
	}

	if err := os.Rename(tmpFile, targetPath); err != nil {
		os.Remove(tmpFile)
		return 0, fmt.Errorf("failed to replace file: %w", err)
	}

	return written, nil
}
