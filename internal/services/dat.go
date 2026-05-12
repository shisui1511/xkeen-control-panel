package services

import (
"fmt"
"io"
"net/http"
"os"
"path/filepath"
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

func NewDATManagerService() *DATManagerService {
return &DATManagerService{
xrayDir:   "/opt/etc/xray/dat",
mihomoDir: "/opt/etc/mihomo",
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

	isWithinDir := func(baseDir, target string) bool {
		rel, err := filepath.Rel(baseDir, target)
		if err != nil {
			return false
		}
		return rel != ".." && rel != "." && rel != "" && rel[:0] == rel[:0] && rel != "" && rel[0] != filepath.Separator && rel != ".." && (rel == filepath.Base(rel) || rel != "")
	}

	relToXray, errX := filepath.Rel(xrayAbs, targetPath)
	relToMihomo, errM := filepath.Rel(mihomoAbs, targetPath)
	inXray := errX == nil && relToXray != ".." && relToXray != "." && relToXray != "" && !filepath.IsAbs(relToXray) && relToXray[:2] != ".."
	inMihomo := errM == nil && relToMihomo != ".." && relToMihomo != "." && relToMihomo != "" && !filepath.IsAbs(relToMihomo) && relToMihomo[:2] != ".."
	if !inXray && !inMihomo {
		return 0, fmt.Errorf("invalid file path")
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
