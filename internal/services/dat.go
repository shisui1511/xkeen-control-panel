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
	Name        string `json:"name"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	LastUpdate  int64  `json:"last_update"`
	Exists      bool   `json:"exists"`
	Type        string `json:"type"` // "xray" or "mihomo"
	IsSymlink   bool   `json:"is_symlink"`
	SymlinkTo   string `json:"symlink_to,omitempty"`
	TagCount    int    `json:"tag_count,omitempty"`
	RecordCount int    `json:"record_count,omitempty"`
	Version     string `json:"version,omitempty"`
	Info        string `json:"info,omitempty"`
}

type datCacheEntry struct {
	Size        int64
	ModTime     int64
	TagCount    int
	RecordCount int
	Version     string
	Info        string
}

type DATManagerService struct {
	xrayDir    string
	mihomoDir  string
	binaryPath string
	mu         sync.RWMutex
	cache      map[string]datCacheEntry
	cacheMu    sync.Mutex
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
		cache:      make(map[string]datCacheEntry),
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

				// Check cache
				s.cacheMu.Lock()
				entry, found := s.cache[match]
				s.cacheMu.Unlock()

				if !found || entry.Size != f.Size || entry.ModTime != f.LastUpdate {
					entry = datCacheEntry{
						Size:    f.Size,
						ModTime: f.LastUpdate,
					}
					// Parse version based on ModTime
					entry.Version = "v" + time.Unix(f.LastUpdate, 0).UTC().Format("200601")

					if strings.HasSuffix(f.Name, ".dat") {
						// Extract tags and calculate record count
						if tags, err := s.ListTags(f.Name); err == nil {
							entry.TagCount = len(tags)
							for _, t := range tags {
								entry.RecordCount += t.Count
							}
						}
					} else if strings.HasSuffix(f.Name, ".mmdb") {
						lowerName := strings.ToLower(f.Name)
						if strings.Contains(lowerName, "country") {
							entry.Info = "MaxMind GeoLite2"
						} else if strings.Contains(lowerName, "asn") {
							entry.Info = "IPInfo ASN"
						} else {
							entry.Info = "MaxMind DB"
						}
					}

					s.cacheMu.Lock()
					s.cache[match] = entry
					s.cacheMu.Unlock()
				}

				f.TagCount = entry.TagCount
				f.RecordCount = entry.RecordCount
				f.Version = entry.Version
				f.Info = entry.Info

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

	// Backup existing files first
	scanAndBackup := func(dir string, patterns ...string) {
		for _, pattern := range patterns {
			matches, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, match := range matches {
				_ = backupFile(match)
			}
		}
	}
	scanAndBackup(s.xrayDir, "*.dat")
	scanAndBackup(s.mihomoDir, "*.dat", "*.mmdb")

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

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return 0, fmt.Errorf("access to localhost is prohibited")
	}

	// Redundant check to satisfy CodeQL SSRF analysis.
	// Actual security is provided by SafeHTTPClient's DialContext to prevent TOCTOU.
	ips, err := net.LookupIP(host)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve host: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return 0, fmt.Errorf("access to private network is prohibited")
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
	_ = backupFile(targetAbs)
	if err := os.Rename(tmpFile, targetAbs); err != nil {
		os.Remove(tmpFile)
		return 0, fmt.Errorf("failed to replace file: %w", err)
	}

	return written, nil
}

var safePathComponentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// DATTagResult holds metadata about a tag inside a .dat file.
type DATTagResult struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"` // number of entries under this tag (0 if unknown)
}

// ListTags reads country_code / tag names from an Xray-format GeoSiteList or GeoIPList .dat file.
// It uses a minimal protobuf parser — no external dependencies required.
// Works with files up to ~100 MB; reads the entire file into memory once.
func (s *DATManagerService) ListTags(name string) ([]DATTagResult, error) {
	safeName := filepath.Base(filepath.Clean(name))
	if !safePathComponentRe.MatchString(safeName) {
		return nil, fmt.Errorf("invalid file name")
	}

	// Look in both directories
	var path string
	for _, dir := range []string{s.xrayDir, s.mihomoDir} {
		candidate := filepath.Join(dir, safeName)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("file not found: %s", safeName)
	}
	if !strings.HasSuffix(safeName, ".dat") {
		return nil, fmt.Errorf("only .dat files are supported for tag listing")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	tags, err := parseDATTags(data)
	if err != nil {
		return nil, err
	}
	return tags, nil
}

type DATSearchResult struct {
	Tag     string   `json:"tag"`
	Entries []string `json:"entries"`
	Total   int      `json:"total"`
	Page    int      `json:"page"`
	HasMore bool     `json:"has_more"`
}

func (s *DATManagerService) SearchTag(filename, tag, query string, page, pageSize int) (*DATSearchResult, error) {
	safeName := filepath.Base(filepath.Clean(filename))
	if !safePathComponentRe.MatchString(safeName) {
		return nil, fmt.Errorf("invalid file name")
	}

	var path string
	for _, dir := range []string{s.xrayDir, s.mihomoDir} {
		candidate := filepath.Join(dir, safeName)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("file not found: %s", safeName)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	isGeoIP := strings.Contains(strings.ToLower(safeName), "geoip") || strings.Contains(strings.ToLower(tag), "geoip")

	var entries []string
	pos := 0
	for pos < len(data) {
		outerTag, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wireType := outerTag & 0x7
		fieldNum := outerTag >> 3

		if wireType != 2 {
			pos, _ = pbSkipField(data, pos, wireType)
			continue
		}

		length, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		if length > uint64(len(data)-pos-n) {
			break
		}
		pos += n
		end := pos + int(length)

		if fieldNum == 1 {
			entryData := data[pos:end]
			entryTag, _ := parseDATEntry(entryData)
			if entryTag == tag {
				if isGeoIP {
					entries = parseDATEntryCIDRs(entryData)
				} else {
					entries = parseDATEntryDomains(entryData)
				}
				break
			}
		}
		pos = end
	}

	var filtered []string
	if query != "" {
		lowerQuery := strings.ToLower(query)
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e), lowerQuery) {
				filtered = append(filtered, e)
			}
		}
	} else {
		filtered = entries
	}

	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	total := len(filtered)
	start := page * pageSize
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}

	endIdx := start + pageSize
	if endIdx > total {
		endIdx = total
	}

	pagedEntries := filtered[start:endIdx]
	hasMore := endIdx < total

	return &DATSearchResult{
		Tag:     tag,
		Entries: pagedEntries,
		Total:   total,
		Page:    page,
		HasMore: hasMore,
	}, nil
}

func parseDATEntryDomains(data []byte) []string {
	var domains []string
	pos := 0
	for pos < len(data) {
		ft, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wireType := ft & 0x7
		fieldNum := ft >> 3

		if wireType != 2 {
			pos, _ = pbSkipField(data, pos, wireType)
			continue
		}

		length, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		if length > uint64(len(data)-pos-n) {
			break
		}
		pos += n
		end := pos + int(length)

		if fieldNum == 2 {
			dom := parseSingleDomain(data[pos:end])
			if dom != "" {
				domains = append(domains, dom)
			}
		}
		pos = end
	}
	return domains
}

func parseSingleDomain(data []byte) string {
	var domainVal string
	pos := 0
	for pos < len(data) {
		ft, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wireType := ft & 0x7
		fieldNum := ft >> 3

		if fieldNum == 2 && wireType == 2 {
			length, n2 := pbReadVarint(data, pos)
			if n2 == 0 {
				break
			}
			if length > uint64(len(data)-pos-n2) {
				break
			}
			pos += n2
			end := pos + int(length)
			domainVal = string(data[pos:end])
			pos = end
		} else {
			var err error
			pos, err = pbSkipField(data, pos, wireType)
			if err != nil {
				break
			}
		}
	}
	return domainVal
}

func parseDATEntryCIDRs(data []byte) []string {
	var cidrs []string
	pos := 0
	for pos < len(data) {
		ft, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wireType := ft & 0x7
		fieldNum := ft >> 3

		if wireType != 2 {
			pos, _ = pbSkipField(data, pos, wireType)
			continue
		}

		length, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		if length > uint64(len(data)-pos-n) {
			break
		}
		pos += n
		end := pos + int(length)

		if fieldNum == 2 {
			cidrStr := parseSingleCIDR(data[pos:end])
			if cidrStr != "" {
				cidrs = append(cidrs, cidrStr)
			}
		}
		pos = end
	}
	return cidrs
}

func parseSingleCIDR(data []byte) string {
	var ip []byte
	var prefix uint32 = 0
	pos := 0
	for pos < len(data) {
		ft, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wireType := ft & 0x7
		fieldNum := ft >> 3

		if fieldNum == 1 && wireType == 2 {
			length, n2 := pbReadVarint(data, pos)
			if n2 == 0 {
				break
			}
			if length > uint64(len(data)-pos-n2) {
				break
			}
			pos += n2
			end := pos + int(length)
			ip = data[pos:end]
			pos = end
		} else if fieldNum == 2 && wireType == 0 {
			val, n2 := pbReadVarint(data, pos)
			if n2 == 0 {
				break
			}
			prefix = uint32(val)
			pos += n2
		} else {
			var err error
			pos, err = pbSkipField(data, pos, wireType)
			if err != nil {
				break
			}
		}
	}

	if len(ip) == 4 || len(ip) == 16 {
		netIP := net.IP(ip)
		return fmt.Sprintf("%s/%d", netIP.String(), prefix)
	}
	return ""
}

// parseDATTags extracts country_code tags from a serialised GeoIPList or GeoSiteList.
//
// Protobuf encoding used here:
//
//	GeoIPList   { repeated GeoIP   entry = 1; }
//	GeoSiteList { repeated GeoSite entry = 1; }
//	GeoIP/GeoSite { string country_code = 1; ... }
//
// Outer message: field-1 LEN (repeated entries).
// Inner message: field-1 LEN (country_code string) + more fields (skipped).
func parseDATTags(data []byte) ([]DATTagResult, error) {
	var results []DATTagResult
	pos := 0

	for pos < len(data) {
		// Read outer field tag
		outerTag, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n

		wireType := outerTag & 0x7
		fieldNum := outerTag >> 3

		if wireType != 2 {
			// Skip non-LEN fields
			pos, _ = pbSkipField(data, pos, wireType)
			continue
		}

		// Read length of sub-message
		length, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		if length > uint64(len(data)-pos-n) {
			break
		}
		pos += n
		end := pos + int(length)

		if fieldNum == 1 {
			// Parse entry sub-message to find country_code (field 1) and count entries (field 2)
			tag, count := parseDATEntry(data[pos:end])
			if tag != "" {
				results = append(results, DATTagResult{Tag: tag, Count: count})
			}
		}
		pos = end
	}

	return results, nil
}

// parseDATEntry reads a single GeoIP or GeoSite message and returns
// (country_code, number of domain/cidr sub-entries under field 2).
func parseDATEntry(data []byte) (string, int) {
	var tag string
	count := 0
	pos := 0

	for pos < len(data) {
		ft, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n

		wireType := ft & 0x7
		fieldNum := ft >> 3

		if wireType != 2 {
			pos, _ = pbSkipField(data, pos, wireType)
			continue
		}

		length, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		if length > uint64(len(data)-pos-n) {
			break
		}
		pos += n

		end := pos + int(length)

		switch fieldNum {
		case 1: // country_code
			tag = string(data[pos:end])
		case 2: // domain / cidr list entry — just count them
			count++
		}
		pos = end
	}

	return tag, count
}

// pbReadVarint decodes a protobuf varint starting at data[offset].
// Returns (value, bytes_consumed); bytes_consumed==0 means error.
func pbReadVarint(data []byte, offset int) (uint64, int) {
	var result uint64
	for i := 0; i < 10 && offset+i < len(data); i++ {
		b := data[offset+i]
		result |= uint64(b&0x7F) << (7 * uint(i))
		if b&0x80 == 0 {
			return result, i + 1
		}
	}
	return 0, 0
}

// pbSkipField advances pos past a field with the given wire type.
// Returns (new_pos, error).
func pbSkipField(data []byte, pos int, wireType uint64) (int, error) {
	switch wireType {
	case 0: // varint
		_, n := pbReadVarint(data, pos)
		if n == 0 {
			return pos, fmt.Errorf("truncated varint")
		}
		return pos + n, nil
	case 1: // 64-bit
		return pos + 8, nil
	case 2: // length-delimited
		length, n := pbReadVarint(data, pos)
		if n == 0 {
			return pos, fmt.Errorf("truncated length")
		}
		if length > uint64(len(data)-pos-n) {
			return len(data), fmt.Errorf("length exceeds data bounds")
		}
		return pos + n + int(length), nil
	case 5: // 32-bit
		return pos + 4, nil
	default:
		return len(data), fmt.Errorf("unknown wire type %d", wireType)
	}
}

func (s *DATManagerService) Rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rollback files in xrayDir
	matches, _ := filepath.Glob(filepath.Join(s.xrayDir, "*.dat"))
	for _, match := range matches {
		_ = rollbackFile(match)
	}

	// Rollback files in mihomoDir
	matches2, _ := filepath.Glob(filepath.Join(s.mihomoDir, "*.dat"))
	for _, match := range matches2 {
		_ = rollbackFile(match)
	}
	matches3, _ := filepath.Glob(filepath.Join(s.mihomoDir, "*.mmdb"))
	for _, match := range matches3 {
		_ = rollbackFile(match)
	}

	return nil
}

func backupFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	// Avoid backing up symlinks (only backup regular files)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(path + ".bak")
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func rollbackFile(path string) error {
	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		return nil
	}
	return os.Rename(bakPath, path)
}

var standardURLs = map[string]string{
	"geosite_refilter.dat": "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/geosite.dat",
	"geoip_refilter.dat":   "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/geoip.dat",
	"geosite_v2fly.dat":    "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
	"geoip_v2fly.dat":      "https://github.com/loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
	"geosite_zkeen.dat":    "https://github.com/jameszeroX/zkeen-domains/releases/latest/download/zkeen.dat",
	"zkeen.dat":            "https://github.com/jameszeroX/zkeen-domains/releases/latest/download/zkeen.dat",
	"geoip_zkeenip.dat":    "https://github.com/jameszeroX/zkeen-ip/releases/latest/download/zkeenip.dat",
	"zkeenip.dat":          "https://github.com/jameszeroX/zkeen-ip/releases/latest/download/zkeenip.dat",
	"geoip.dat":            "https://github.com/loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
	"geosite.dat":          "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat",
}

func (s *DATManagerService) UpdateFile(filename string) error {
	safeName := filepath.Base(filepath.Clean(filename))
	if !safePathComponentRe.MatchString(safeName) {
		return fmt.Errorf("invalid file name")
	}

	urlVal, ok := standardURLs[strings.ToLower(safeName)]
	if !ok {
		return fmt.Errorf("no update URL configured for %s", safeName)
	}

	var path string
	for _, dir := range []string{s.xrayDir, s.mihomoDir} {
		candidate := filepath.Join(dir, safeName)
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		}
	}

	if path == "" {
		if strings.HasSuffix(strings.ToLower(safeName), ".mmdb") {
			path = filepath.Join(s.mihomoDir, safeName)
		} else {
			path = filepath.Join(s.xrayDir, safeName)
		}
	}

	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if target, err := os.Readlink(path); err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(path), target)
			}
			path = target
		}
	}

	_, err = s.UpdateCustom(path, urlVal)
	return err
}
