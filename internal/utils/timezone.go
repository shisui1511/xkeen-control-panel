package utils

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"time"
)

// systemTZPaths lists the files where Keenetic/Entware/OpenWrt keep the
// router's timezone as a POSIX TZ string (e.g. "MSK-3").
var systemTZPaths = []string{"/etc/TZ", "/opt/etc/TZ", "/var/TZ"}

// ApplySystemTimezone sets time.Local to the router's configured timezone.
//
// On Keenetic /etc/localtime is a symlink to /var/TZ, which holds a POSIX TZ
// string rather than a TZif file. Go cannot parse it and silently falls back
// to UTC, shifting schedules, day/week boundaries and log timestamps by the
// router's UTC offset. An explicit $TZ in the environment keeps precedence.
// Returns the applied POSIX string, or "" when nothing was changed.
func ApplySystemTimezone() string {
	if os.Getenv("TZ") != "" {
		return ""
	}
	if name, _ := time.Now().Zone(); name != "UTC" && name != "" {
		return ""
	}
	for _, p := range systemTZPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		tz := strings.TrimSpace(strings.ReplaceAll(string(data), "\x00", ""))
		loc, err := LocationFromPOSIX(tz)
		if err != nil {
			continue
		}
		time.Local = loc
		return tz
	}
	return ""
}

// LocationFromPOSIX builds a *time.Location from a POSIX TZ string such as
// "MSK-3" or "CET-1CEST,M3.5.0,M10.5.0/3", DST rules included.
//
// It wraps the string into a minimal TZif v2 blob with no transitions: Go
// evaluates the footer TZ string for every instant after the last transition,
// so the resulting location follows the POSIX rules exactly.
func LocationFromPOSIX(tz string) (*time.Location, error) {
	if tz == "" || strings.ContainsAny(tz, "\r\n\t ") {
		return nil, errInvalidTZ
	}
	if _, ok := posixStdOffset(tz); !ok {
		return nil, errInvalidTZ
	}

	var buf bytes.Buffer
	writeBlock := func(version byte) {
		buf.WriteString("TZif")
		buf.WriteByte(version)
		buf.Write(make([]byte, 15))
		// isutcnt, isstdcnt, leapcnt, timecnt, typecnt, charcnt
		for _, n := range []uint32{0, 0, 0, 0, 1, 4} {
			_ = binary.Write(&buf, binary.BigEndian, n)
		}
		// Single ttinfo (UTC placeholder); the footer supplies the real rules.
		_ = binary.Write(&buf, binary.BigEndian, int32(0))
		buf.WriteByte(0) // isdst
		buf.WriteByte(0) // abbreviation index
		buf.WriteString("UTC\x00")
	}
	writeBlock('2')
	writeBlock('2')
	buf.WriteByte('\n')
	buf.WriteString(tz)
	buf.WriteByte('\n')

	loc, err := time.LoadLocationFromTZData(tz, buf.Bytes())
	if err != nil {
		return nil, err
	}
	return loc, nil
}

type tzError string

func (e tzError) Error() string { return string(e) }

const errInvalidTZ = tzError("invalid POSIX TZ string")

// posixStdOffset validates the leading "std offset" part of a POSIX TZ string.
func posixStdOffset(tz string) (string, bool) {
	i := 0
	if strings.HasPrefix(tz, "<") {
		end := strings.IndexByte(tz, '>')
		if end < 0 {
			return "", false
		}
		i = end + 1
	} else {
		for i < len(tz) && (tz[i] >= 'A' && tz[i] <= 'Z' || tz[i] >= 'a' && tz[i] <= 'z') {
			i++
		}
		if i < 3 {
			return "", false
		}
	}
	j := i
	if j < len(tz) && (tz[j] == '+' || tz[j] == '-') {
		j++
	}
	start := j
	for j < len(tz) && (tz[j] >= '0' && tz[j] <= '9' || tz[j] == ':') {
		j++
	}
	if j == start {
		return "", false
	}
	return tz[i:j], true
}
