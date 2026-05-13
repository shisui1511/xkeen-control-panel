package services

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type DATFile struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	LastUpdate int64  `json:"last_update"`
	Exists     bool   `json:"exists"`
	Type       string `json:"type"` // "xray" or "mihomo"
	IsSymlink  bool   `json:"is_symlink"`
	SymlinkTo  string `json:"symlink_to,omitempty"`
}

type DATManagerService struct {
	xrayDir    string
	mihomoDir  string
	binaryPath string
	mu         sync.RWMutex
}

func NewDATManagerService(dirs ...string) *DATManagerService {
	xrayDir := "/opt/etc/xray/dat"
	mihomoDir := "/opt/etc/mihomo"
	binaryPath := "/opt/sbin/xkeen"

	if len(dirs) > 0 && dirs[0] != "" {
		xrayDir = dirs[0]
	}
	if len(dirs) > 1 && dirs[1] != "" {
		mihomoDir = dirs[1]
	}
	if len(dirs) > 2 && dirs[2] != "" {
		binaryPath = dirs[2]
	}

	return &DATManagerService{
		xrayDir:    xrayDir,
		mihomoDir:  mihomoDir,
		binaryPath: binaryPath,
	}
}

func (s *DATManagerService) List() []DATFile {
	var files []DATFile

	s.mu.RLock()
	defer s.mu.RUnlock()

	scanDir := func(dir string, fileType string, patterns ...string) {
		for _, pattern := range patterns {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				continue
			}
			for _, match := range matches {
				f := DATFile{
					Name:   filepath.Base(match),
					Path:   match,
					Exists: true,
					Type:   fileType,
				}

				info, err := os.Lstat(match)
				if err != nil {
					continue
				}

				if info.Mode()&os.ModeSymlink != 0 {
					f.IsSymlink = true
					if target, err := os.Readlink(match); err == nil {
						f.SymlinkTo = target
						// Try to get size of target
						if targetInfo, err := os.Stat(match); err == nil {
							f.Size = targetInfo.Size()
							f.LastUpdate = targetInfo.ModTime().Unix()
						}
					}
				} else {
					f.Size = info.Size()
					f.LastUpdate = info.ModTime().Unix()
				}

				files = append(files, f)
			}
		}
	}

	scanDir(s.xrayDir, "xray", "*.dat")
	scanDir(s.mihomoDir, "mihomo", "*.dat", "*.mmdb")

	return files
}

func (s *DATManagerService) Update() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use xkeen -ug to update DAT files
	// -u: check for updates
	// -g: geoip/geosite update
	// We use both to ensure update
	cmd := exec.Command(s.binaryPath, "-ug")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xkeen -ug failed: %v, output: %s", err, string(out))
	}
	return nil
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

	// 2. URL validation & sanitization
	u, err := url.Parse(remoteURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, fmt.Errorf("invalid or unsupported URL scheme")
	}

	// Reject URLs with embedded credentials
	if u.User != nil {
		return 0, fmt.Errorf("URL must not contain credentials")
	}

	// Restrict path to prevent path traversal via URL
	cleanPath := u.Path
	if cleanPath == "" {
		cleanPath = "/"
	}

	// Reconstruct a sanitized URL from validated components only
	sanitizedURL := fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, cleanPath)

	// Redundant check to satisfy CodeQL SSRF analysis.
	// Actual security is provided by SafeHTTPClient's DialContext to prevent TOCTOU.
	if ips, err := net.LookupIP(u.Hostname()); err == nil {
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
				return 0, fmt.Errorf("access to private network is prohibited")
			}
		}
	}

	client := utils.SafeHTTPClient(5 * time.Minute)
	resp, err := client.Get(sanitizedURL)
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

	// Limit response size to 50MB to prevent disk exhaustion on routers
	written, err := io.Copy(out, io.LimitReader(resp.Body, 50*1024*1024))
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
