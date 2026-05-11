package services

import (
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func validateKernelPath(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return errors.New("path traversal detected")
	}
	return nil
}

func safeTempPath(name string) (string, error) {
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", errors.New("invalid temp file name")
	}
	return filepath.Join(os.TempDir(), name), nil
}

// KernelInfo holds info about an installed kernel
type KernelInfo struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	BinaryPath     string `json:"binary_path"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	HasUpdate      bool   `json:"has_update"`
	Channel        string `json:"channel"` // stable, preview
	Repo           string `json:"repo"`
	Status         string `json:"status"` // idle, checking, downloading, installing, done, failed
	Message        string `json:"message"`
}

// KernelService manages proxy kernels (xray, mihomo)
type KernelService struct {
	kernels map[string]*KernelInfo
}

func NewKernelService() *KernelService {
	svc := &KernelService{
		kernels: make(map[string]*KernelInfo),
	}

	// Register known kernels
	svc.kernels["xray"] = &KernelInfo{
		Name:        "xray",
		DisplayName: "Xray Core",
		BinaryPath:  "/opt/bin/xray",
		Channel:     "stable",
		Repo:        "XTLS/Xray-core",
	}

	svc.kernels["mihomo"] = &KernelInfo{
		Name:        "mihomo",
		DisplayName: "Mihomo (Clash.Meta)",
		BinaryPath:  "/opt/bin/mihomo",
		Channel:     "stable",
		Repo:        "MetaCubeX/mihomo",
	}

	// Detect current versions
	for _, k := range svc.kernels {
		k.CurrentVersion = svc.detectVersion(k)
	}

	return svc
}

func (s *KernelService) List() []KernelInfo {
	result := make([]KernelInfo, 0, len(s.kernels))
	for _, k := range s.kernels {
		result = append(result, *k)
	}
	return result
}

func (s *KernelService) Get(name string) *KernelInfo {
	if k, ok := s.kernels[name]; ok {
		// Refresh version
		k.CurrentVersion = s.detectVersion(k)
		return k
	}
	return nil
}

func (s *KernelService) SetChannel(name, channel string) bool {
	if k, ok := s.kernels[name]; ok {
		k.Channel = channel
		return true
	}
	return false
}

// detectVersion runs the binary with version flag
func (s *KernelService) detectVersion(k *KernelInfo) string {
	if _, err := os.Stat(k.BinaryPath); os.IsNotExist(err) {
		return "not installed"
	}

	var cmd *exec.Cmd
	switch k.Name {
	case "xray":
		cmd = exec.Command(k.BinaryPath, "version")
	case "mihomo":
		cmd = exec.Command(k.BinaryPath, "-v")
	default:
		return "unknown"
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "error"
	}

	return s.parseVersion(k.Name, string(out))
}

func (s *KernelService) parseVersion(name, output string) string {
	output = strings.TrimSpace(output)
	switch name {
	case "xray":
		// Xray 1.8.24 (Xray, Penetrates Everything.) ...
		re := regexp.MustCompile(`Xray\s+([\d.]+)`)
		if m := re.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
	case "mihomo":
		// Mihomo Version: v1.18.0 ...
		re := regexp.MustCompile(`(?:Mihomo\s+)?Version[:\s]*v?([\d.]+)`)
		if m := re.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
	}
	return "unknown"
}

// CheckLatest queries GitHub API for latest release
func (s *KernelService) CheckLatest(name string) error {
	k := s.kernels[name]
	if k == nil {
		return fmt.Errorf("kernel not found: %s", name)
	}

	k.Status = "checking"
	k.Message = "Checking for updates..."

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", k.Repo)
	if k.Channel != "stable" {
		// For preview/beta, list all releases and pick first prerelease or latest
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=5", k.Repo)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		k.Status = "failed"
		k.Message = "GitHub API error: " + err.Error()
		return err
	}
	defer resp.Body.Close()

	if k.Channel == "stable" {
		var release struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			k.Status = "failed"
			k.Message = "Parse error: " + err.Error()
			return err
		}
		k.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	} else {
		var releases []struct {
			TagName    string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			k.Status = "failed"
			k.Message = "Parse error: " + err.Error()
			return err
		}
		for _, rel := range releases {
			if k.Channel == "preview" && rel.Prerelease {
				k.LatestVersion = strings.TrimPrefix(rel.TagName, "v")
				break
			}
			if k.Channel == "stable" && !rel.Prerelease {
				k.LatestVersion = strings.TrimPrefix(rel.TagName, "v")
				break
			}
		}
	}

	k.HasUpdate = k.LatestVersion != "" && k.LatestVersion != k.CurrentVersion
	k.Status = "idle"
	k.Message = ""
	return nil
}

// Install downloads and installs the kernel
func (s *KernelService) Install(name string) error {
	k := s.kernels[name]
	if k == nil {
		return fmt.Errorf("kernel not found: %s", name)
	}

	k.Status = "downloading"
	k.Message = "Downloading..."

	arch := runtime.GOARCH
	if arch == "mipsle" || arch == "mipsel" {
		arch = "mipsle-softfloat"
	} else if arch == "mips" {
		arch = "mips-softfloat"
	}

	downloadURL, filename := s.buildDownloadURL(k, arch)
	if downloadURL == "" {
		k.Status = "failed"
		k.Message = "Unsupported architecture: " + arch
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	tempFile, err := safeTempPath(filename)
	if err != nil {
		k.Status = "failed"
		k.Message = "Invalid filename: " + err.Error()
		return err
	}
	if err := s.downloadFile(downloadURL, tempFile); err != nil {
		k.Status = "failed"
		k.Message = "Download failed: " + err.Error()
		return err
	}

	// Backup current binary
	k.Status = "installing"
	k.Message = "Creating backup..."

	backupDir := filepath.Join(filepath.Dir(k.BinaryPath), ".backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		k.Status = "failed"
		k.Message = "Backup dir failed: " + err.Error()
		return err
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.bak.%d", name, time.Now().Unix()))
	if err := validateKernelPath(backupPath); err != nil {
		return err
	}

	if _, err := os.Stat(k.BinaryPath); err == nil {
		if err := copyFile(k.BinaryPath, backupPath); err != nil {
			k.Status = "failed"
			k.Message = "Backup failed: " + err.Error()
			return err
		}
	}

	// Extract if needed
	extractedPath := tempFile
	if strings.HasSuffix(tempFile, ".zip") {
		k.Message = "Extracting..."
		extracted, err := s.extractZip(tempFile, name)
		if err != nil {
			k.Status = "failed"
			k.Message = "Extract failed: " + err.Error()
			return err
		}
		extractedPath = extracted
	} else if strings.HasSuffix(tempFile, ".gz") {
		k.Message = "Extracting..."
		extracted, err := s.extractGz(tempFile)
		if err != nil {
			k.Status = "failed"
			k.Message = "Extract failed: " + err.Error()
			return err
		}
		extractedPath = extracted
	}

	// Make executable and replace
	if err := validateKernelPath(extractedPath); err != nil {
		return err
	}
	if err := os.Chmod(extractedPath, 0755); err != nil {
		k.Status = "failed"
		k.Message = "Chmod failed: " + err.Error()
		return err
	}

	// Atomic replace
	tempDest := filepath.Join(filepath.Dir(k.BinaryPath), filepath.Base(k.BinaryPath)+".new")
	if err := validateKernelPath(extractedPath); err != nil {
		return err
	}
	if err := validateKernelPath(tempDest); err != nil {
		return err
	}
	if err := os.Rename(extractedPath, tempDest); err != nil {
		k.Status = "failed"
		k.Message = "Replace failed: " + err.Error()
		return err
	}
	if err := os.Rename(tempDest, k.BinaryPath); err != nil {
		// Try rollback
		if strings.HasPrefix(filepath.Clean(backupPath), "/") {
			_ = os.Rename(backupPath, k.BinaryPath)
		}
		k.Status = "failed"
		k.Message = "Replace failed: " + err.Error()
		return err
	}

	// Verify new version
	k.CurrentVersion = s.detectVersion(k)
	k.HasUpdate = false
	k.Status = "done"
	k.Message = "Updated to " + k.CurrentVersion

	return nil
}

func (s *KernelService) buildDownloadURL(k *KernelInfo, arch string) (string, string) {
	version := k.LatestVersion
	if version == "" {
		return "", ""
	}

	switch k.Name {
	case "xray":
		// Xray: Xray-linux-arm64-v8a.zip or Xray-linux-mipsle-softfloat.zip
		var file string
		switch arch {
		case "arm64":
			file = fmt.Sprintf("Xray-linux-%s-v8a.zip", arch)
		case "mipsle-softfloat", "mips-softfloat":
			file = fmt.Sprintf("Xray-linux-%s.zip", arch)
		default:
			return "", ""
		}
		return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", k.Repo, version, file), file

	case "mihomo":
		// Mihomo: mihomo-linux-arm64-v1.18.0.gz or mihomo-linux-mipsle-softfloat-v1.18.0.gz
		var file string
		switch arch {
		case "arm64", "mipsle-softfloat", "mips-softfloat":
			file = fmt.Sprintf("mihomo-linux-%s-v%s.gz", arch, version)
		default:
			return "", ""
		}
		return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", k.Repo, version, file), file
	}

	return "", ""
}

func (s *KernelService) downloadFile(url, filepath string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (s *KernelService) extractZip(zipPath, binaryName string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == binaryName || f.Name == binaryName+"-linux-"+runtime.GOARCH {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			defer rc.Close()

			outPath, err := safeTempPath(binaryName + ".new")
			if err != nil {
				return "", err
			}
			if err := validateKernelPath(outPath); err != nil {
				return "", err
			}
			out, err := os.Create(outPath)
			if err != nil {
				return "", err
			}
			defer out.Close()

			_, err = io.Copy(out, rc)
			return outPath, err
		}
	}

	return "", fmt.Errorf("binary not found in archive")
}

func copyFile(src, dst string) error {
	if !strings.HasPrefix(filepath.Clean(src), "/") {
		return errors.New("invalid src path")
	}
	if !strings.HasPrefix(filepath.Clean(dst), "/") {
		return errors.New("invalid dst path")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func (s *KernelService) extractGz(gzPath string) (string, error) {
	f, err := os.Open(gzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gr.Close()

	outPath := strings.TrimSuffix(filepath.Base(gzPath), ".gz")
	outPath, err = safeTempPath(outPath)
	if err != nil {
		return "", err
	}
	if err := validateKernelPath(outPath); err != nil {
		return "", err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, gr)
	return outPath, err
}
