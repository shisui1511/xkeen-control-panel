package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DATFile represents a geo database file (GeoIP or GeoSite)
type DATFile struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	LastUpdate int64  `json:"last_update"`
	Exists     bool   `json:"exists"`
	RemoteURL  string `json:"remote_url"`
}

// DATManagerService manages GeoIP and GeoSite DAT files
type DATManagerService struct {
	geoIPPath   string
	geoSitePath string
	mmdbPath    string
	mu          sync.RWMutex
}

func NewDATManagerService(dataDir string) *DATManagerService {
	svc := &DATManagerService{
		geoIPPath:   "/opt/etc/xray/dat/geoip.dat",
		geoSitePath: "/opt/etc/xray/dat/geosite.dat",
		mmdbPath:    "/opt/etc/mihomo/country.mmdb",
	}
	return svc
}

// List returns information about all DAT files
func (s *DATManagerService) List() map[string]*DATFile {
	files := map[string]*DATFile{
		"geoip": {
			Name:      "GeoIP",
			Path:      s.geoIPPath,
			RemoteURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		},
		"geosite": {
			Name:      "GeoSite",
			Path:      s.geoSitePath,
			RemoteURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
		},
		"mmdb": {
			Name:      "Country MMDB",
			Path:      s.mmdbPath,
			RemoteURL: "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/Country.mmdb",
		},
	}

	s.mu.RLock()
	for _, f := range files {
		if info, err := os.Stat(f.Path); err == nil {
			f.Exists = true
			f.Size = info.Size()
			f.LastUpdate = info.ModTime().Unix()
		}
	}
	s.mu.RUnlock()

	return files
}

// Update downloads and replaces a DAT file
func (s *DATManagerService) Update(datType string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var localPath, remoteURL string
	switch datType {
	case "geoip":
		localPath = s.geoIPPath
		remoteURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
	case "geosite":
		localPath = s.geoSitePath
		remoteURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
	case "mmdb":
		localPath = s.mmdbPath
		remoteURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/Country.mmdb"
	default:
		return 0, fmt.Errorf("unknown DAT type: %s", datType)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(remoteURL)
	if err != nil {
		return 0, fmt.Errorf("failed to download %s: %w", datType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Download to temp file first
	tmpFile := localPath + ".tmp"
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

	// Replace old file
	if err := os.Rename(tmpFile, localPath); err != nil {
		os.Remove(tmpFile)
		return 0, fmt.Errorf("failed to replace file: %w", err)
	}

	return written, nil
}

// UpdateAll updates all DAT files
func (s *DATManagerService) UpdateAll() (map[string]error, error) {
	types := []string{"geoip", "geosite", "mmdb"}
	results := make(map[string]error)

	for _, t := range types {
		_, err := s.Update(t)
		results[t] = err
	}

	return results, nil
}

// GetUpdateURL returns the download URL for a DAT type
func GetDATUpdateURL(datType string) string {
	urls := map[string]string{
		"geoip":   "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		"geosite": "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
		"mmdb":    "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/Country.mmdb",
	}
	return urls[datType]
}

// GetLatestReleaseInfo returns info about the latest remote release
func GetLatestReleaseInfo() (map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/Loyalsoldier/v2ray-rules-dat/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return data, nil
}
