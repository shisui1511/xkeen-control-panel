package services

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MihomoGeoTagLookup matches a domain or IP against GeoSite.dat and GeoIP.dat
// in the Mihomo directory. A missing database yields a nil map for its type.
func MihomoGeoTagLookup(mihomoDir string) GeoTagLookup {
	return func(_ context.Context, target string) (map[string]bool, map[string]bool, error) {
		var paths []string
		var sites, ips map[string]bool
		sitePath := filepath.Join(mihomoDir, "GeoSite.dat")
		if _, err := os.Stat(sitePath); err == nil {
			paths = append(paths, sitePath)
			sites = map[string]bool{}
		}
		ipPath := filepath.Join(mihomoDir, "GeoIP.dat")
		if _, err := os.Stat(ipPath); err == nil {
			paths = append(paths, ipPath)
			ips = map[string]bool{}
		}
		if len(paths) == 0 {
			return nil, nil, nil
		}

		domain, ip := normalizeQueryInput(target)
		if domain == "" && ip == nil {
			return nil, nil, fmt.Errorf("empty query")
		}
		scanDATMatches(paths, domain, ip, "all", func(_ string, tag string, isGeoIP bool, _ []string) {
			if isGeoIP {
				ips[strings.ToLower(tag)] = true
			} else {
				sites[strings.ToLower(tag)] = true
			}
		})
		return sites, ips, nil
	}
}

// scanDATMatches проверяет выбранные базы и передаёт совпадения вызывающему коду.
func scanDATMatches(paths []string, cleanDomain string, parsedIP net.IP, filterType string, onMatch func(filename, tag string, isGeoIP bool, matches []string)) {
	var resolvedIPs []net.IP
	if parsedIP != nil {
		resolvedIPs = []net.IP{parsedIP}
	} else if filterType == "ip" || filterType == "all" || filterType == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var r net.Resolver
		ips, err := r.LookupIP(ctx, "ip", cleanDomain)
		cancel()
		if err == nil {
			resolvedIPs = ips
		}
	}

	reCache := make(map[string]*regexp.Regexp)

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		filename := filepath.Base(path)
		isGeoIP := isGeoIPFile(data, filename, "")

		if filterType == "domain" && isGeoIP {
			continue
		}
		if filterType == "ip" && !isGeoIP {
			continue
		}

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
			if n == 0 || length > uint64(len(data)-pos-n) {
				break
			}
			pos += n
			end := pos + int(length)

			if fieldNum == 1 {
				entryData := data[pos:end]
				entryTag, _ := parseDATEntry(entryData)
				if entryTag != "" {
					var matches []string
					if isGeoIP {
						if len(resolvedIPs) > 0 {
							matches = matchCIDREntry(entryData, resolvedIPs)
						}
					} else {
						domains := parseDATEntryDomains(entryData)
						for _, d := range domains {
							if matchDomainCached(d, cleanDomain, reCache) {
								matches = append(matches, d)
							}
						}
					}

					if len(matches) > 0 {
						onMatch(filename, entryTag, isGeoIP, matches)
					}
				}
			}
			pos = end
		}
	}
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
	var domainType uint64 = 0
	hasType := false
	pos := 0
	for pos < len(data) {
		ft, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wireType := ft & 0x7
		fieldNum := ft >> 3

		if fieldNum == 1 && wireType == 0 {
			val, n2 := pbReadVarint(data, pos)
			if n2 > 0 {
				domainType = val
				hasType = true
				pos += n2
			}
		} else if fieldNum == 2 && wireType == 2 {
			length, n2 := pbReadVarint(data, pos)
			if n2 == 0 || length > uint64(len(data)-pos-n2) {
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

	if domainVal == "" {
		return ""
	}

	if !hasType {
		if strings.Contains(domainVal, ".") {
			domainType = 2
		} else {
			domainType = 0
		}
	}

	switch domainType {
	case 0:
		return "keyword:" + domainVal
	case 1:
		return "regexp:" + domainVal
	case 2:
		return domainVal
	case 3:
		return "full:" + domainVal
	default:
		return domainVal
	}
}

func parseRawCIDR(data []byte) ([]byte, uint32) {
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
			if n2 == 0 || length > uint64(len(data)-pos-n2) {
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
	return ip, prefix
}

// parseDATEntry reads a single GeoIP or GeoSite message and returns
// (country_code, number of domain/cidr sub-entries under field 2).
func parseDATEntry(data []byte) (string, int) {
	return parseDATEntryFiltered(data, "")
}

func parseDATEntryFiltered(data []byte, targetTag string) (string, int) {
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
			if targetTag != "" && !strings.EqualFold(tag, targetTag) {
				return tag, 0
			}
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
		if pos+8 > len(data) {
			return len(data), fmt.Errorf("truncated 64-bit field")
		}
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
		if pos+4 > len(data) {
			return len(data), fmt.Errorf("truncated 32-bit field")
		}
		return pos + 4, nil
	default:
		return len(data), fmt.Errorf("unknown wire type %d", wireType)
	}
}

func isGeoIPFile(data []byte, filename, tag string) bool {
	lowerName := strings.ToLower(filename)
	lowerTag := strings.ToLower(tag)
	if strings.Contains(lowerName, "geoip") || strings.Contains(lowerTag, "geoip") || strings.Contains(lowerName, "zkeenip") {
		return true
	}
	if strings.Contains(lowerName, "geosite") || strings.Contains(lowerTag, "geosite") || lowerName == "zkeen.dat" {
		return false
	}
	if len(data) > 0 {
		return detectProtobufIsGeoIP(data)
	}
	return false
}

func detectProtobufIsGeoIP(data []byte) bool {
	pos := 0
	for pos < len(data) {
		outerTag, n := pbReadVarint(data, pos)
		if n == 0 {
			break
		}
		pos += n
		wt := outerTag & 0x7
		fn := outerTag >> 3
		if wt != 2 {
			pos, _ = pbSkipField(data, pos, wt)
			continue
		}
		length, n := pbReadVarint(data, pos)
		if n == 0 || length > uint64(len(data)-pos-n) {
			break
		}
		pos += n
		end := pos + int(length)

		if fn == 1 {
			entryData := data[pos:end]
			epos := 0
			for epos < len(entryData) {
				et, en := pbReadVarint(entryData, epos)
				if en == 0 {
					break
				}
				epos += en
				ewt := et & 0x7
				efn := et >> 3
				if ewt != 2 {
					epos, _ = pbSkipField(entryData, epos, ewt)
					continue
				}
				elength, en := pbReadVarint(entryData, epos)
				if en == 0 || elength > uint64(len(entryData)-epos-en) {
					break
				}
				epos += en
				eend := epos + int(elength)

				if efn == 2 {
					itemData := entryData[epos:eend]
					ipos := 0
					for ipos < len(itemData) {
						it, in := pbReadVarint(itemData, ipos)
						if in == 0 {
							break
						}
						ipos += in
						iwt := it & 0x7
						ifn := it >> 3
						// GeoIP: CIDR has field 1 bytes ip (LEN), field 2 prefix (VARINT)
						// GeoSite: Domain has field 1 type (VARINT), field 2 value (LEN)
						if ifn == 1 {
							if iwt == 2 {
								return true
							} else if iwt == 0 {
								return false
							}
						} else if ifn == 2 {
							if iwt == 0 {
								return true
							} else if iwt == 2 {
								return false
							}
						}
						ipos, _ = pbSkipField(itemData, ipos, iwt)
					}
				}
				epos = eend
			}
		}
		pos = end
	}
	return false
}

func normalizeQueryInput(v string) (string, net.IP) {
	s := strings.TrimSpace(v)
	if s == "" {
		return "", nil
	}

	// URL -> host
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			if u.Host != "" {
				s = u.Host
			} else if u.Hostname() != "" {
				s = u.Hostname()
			}
		}
	}

	// Strip path/query/hash if domain.com/path was pasted
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}

	// host:port
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}

	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")

	if ip := net.ParseIP(s); ip != nil {
		return strings.ToLower(s), ip
	}

	return strings.ToLower(s), nil
}

func matchDomainCached(ruleDomain, targetDomain string, cache map[string]*regexp.Regexp) bool {
	cleanRule := strings.TrimSpace(ruleDomain)
	target := strings.ToLower(strings.TrimSpace(targetDomain))
	if cleanRule == "" || target == "" {
		return false
	}

	prefix := ""
	if idx := strings.Index(cleanRule, ":"); idx != -1 {
		prefix = cleanRule[:idx]
		cleanRule = cleanRule[idx+1:]
	}

	switch prefix {
	case "full":
		return target == strings.ToLower(cleanRule)
	case "keyword", "plain":
		return strings.Contains(target, strings.ToLower(cleanRule))
	case "regexp":
		var re *regexp.Regexp
		if cache != nil {
			re = cache[cleanRule]
			if re == nil {
				var err error
				re, err = regexp.Compile(cleanRule)
				if err != nil {
					return false
				}
				cache[cleanRule] = re
			}
		} else {
			var err error
			re, err = regexp.Compile(cleanRule)
			if err != nil {
				return false
			}
		}
		return re.MatchString(target)
	default: // RootDomain
		v := strings.ToLower(cleanRule)
		if !strings.Contains(v, ".") {
			return strings.Contains(target, v)
		}
		return target == v || strings.HasSuffix(target, "."+v)
	}
}

func matchCIDREntry(entryData []byte, targetIPs []net.IP) []string {
	var matches []string
	pos := 0
	for pos < len(entryData) {
		ft, n := pbReadVarint(entryData, pos)
		if n == 0 {
			break
		}
		pos += n
		wt := ft & 0x7
		fn := ft >> 3

		if wt != 2 {
			pos, _ = pbSkipField(entryData, pos, wt)
			continue
		}

		length, n := pbReadVarint(entryData, pos)
		if n == 0 || length > uint64(len(entryData)-pos-n) {
			break
		}
		pos += n
		end := pos + int(length)

		if fn == 2 {
			cIP, prefix := parseRawCIDR(entryData[pos:end])
			if len(cIP) == 4 || len(cIP) == 16 {
				bits := len(cIP) * 8
				if int(prefix) <= bits {
					mask := net.CIDRMask(int(prefix), bits)
					if mask != nil {
						ipNet := net.IPNet{IP: net.IP(cIP).Mask(mask), Mask: mask}
						for _, tip := range targetIPs {
							if ipNet.Contains(tip) {
								matches = append(matches, fmt.Sprintf("%s/%d (IP: %s)", net.IP(cIP).String(), prefix, tip.String()))
								break
							}
						}
					}
				}
			}
		}
		pos = end
	}
	return matches
}
