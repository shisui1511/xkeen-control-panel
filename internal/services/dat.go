package services

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

	// 1. Path validation - strictly root files only to prevent path injection
	safeName := filepath.Base(filepath.Clean(localPath))
	if safeName == "." || safeName == ".." || safeName == "" {
		return 0, fmt.Errorf("invalid file name")
	}

	// Basic regex check for the filename to be even safer
	if !safePathComponentRe.MatchString(safeName) {
		return 0, fmt.Errorf("invalid characters in file name")
	}

	// Determine base directory (prefer xray for .dat, mihomo for .mmdb)
	baseDir := s.xrayDir
	if strings.HasSuffix(safeName, ".mmdb") {
		baseDir = s.mihomoDir
	} else {
		// For .dat, check if it already exists in mihomo
		if _, err := os.Stat(filepath.Join(s.mihomoDir, safeName)); err == nil {
			baseDir = s.mihomoDir
		}
	}

	// Final absolute path - fully controlled and sanitized
	targetAbs := filepath.Join(baseDir, safeName)

	// 2. URL validation & SSRF protection
	u, err := url.Parse(remoteURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, fmt.Errorf("invalid or unsupported URL scheme")
	}

	// Robust SSRF protection using custom DialContext that prevents connections to private IPs
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
					return nil, fmt.Errorf("access to private network is prohibited")
				}
			}
			return (&net.Dialer{Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Minute,
	}

	resp, err := client.Get(u.String())
	if err != nil {
		return 0, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create temp file in the same directory
	out, err := os.CreateTemp(baseDir, safeName+".*.tmp")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFile := out.Name()

	written, err := io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmpFile)
		return 0, fmt.Errorf("failed to write file: %w", err)
	}

	// targetAbs is now fully sanitized and restricted to baseDir
	if err := os.Rename(tmpFile, targetAbs); err != nil {
		os.Remove(tmpFile)
		return 0, fmt.Errorf("failed to replace file: %w", err)
	}

	return written, nil
}


var safePathComponentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
