package services

import (
	"bufio"
	"bytes"
	"io"
	"regexp"
	"strings"
)

var (
	// ansiRegex matches ANSI escape sequences (colors, cursor movements)
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\([a-zA-Z]`)

	// urlTokenRegex matches sensitive URL query parameters
	urlTokenRegex = regexp.MustCompile(`(?i)(token|secret|key|password|auth)=([^&\s]+)`)

	// bearerRegex matches Authorization Bearer tokens
	bearerRegex = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9_\-\.+=/]+`)

	// sensitiveKVRegex matches key-value pairs with sensitive fields in logs/configs
	sensitiveKVRegex = regexp.MustCompile(`(?i)(["']?(?:privateKey|private_key|private-key|publicKey|public_key|public-key|shortId|short_id|short-id|psk|password|secret|uuid)["']?\s*[:=]\s*["']?)[^"',\s}]+(["']?)`)

	// lanIPRegex matches private IPv4 addresses
	lan192Regex = regexp.MustCompile(`\b192\.168\.(\d{1,3})\.(\d{1,3})\b`)
	lan172Regex = regexp.MustCompile(`\b172\.(1[6-9]|2[0-9]|3[0-1])\.(\d{1,3})\.(\d{1,3})\b`)
	lan10Regex  = regexp.MustCompile(`\b10\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b`)

	// timestamp1970Regex matches 1970/01/01 or 1970-01-01 timestamps produced before NTP sync
	timestamp1970Regex = regexp.MustCompile(`\b1970[-/](?:01|1)[-/](?:01|1)\b`)
)

// StripANSI removes all ANSI escape sequences from input string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// StripCarriageReturns removes carriage returns (\r) often emitted by progress bars.
func StripCarriageReturns(s string) string {
	return strings.ReplaceAll(s, "\r", "")
}

// NormalizeTimestamps replaces 1970-era timestamps with an explicit NTP-sync marker.
func NormalizeTimestamps(s string) string {
	return timestamp1970Regex.ReplaceAllString(s, "[NTP:1970]")
}

// RedactSensitiveText replaces passwords, tokens, Reality keys, LAN IPs and cleans control characters.
func RedactSensitiveText(s string) string {
	// 1. Strip ANSI and carriage returns
	cleaned := StripANSI(s)
	cleaned = StripCarriageReturns(cleaned)
	cleaned = NormalizeTimestamps(cleaned)

	// 2. Redact URL tokens
	cleaned = urlTokenRegex.ReplaceAllString(cleaned, "${1}=*REDACTED*")

	// 3. Redact Bearer tokens
	cleaned = bearerRegex.ReplaceAllString(cleaned, "${1}*REDACTED*")

	// 4. Redact Key-Value pairs
	cleaned = sensitiveKVRegex.ReplaceAllString(cleaned, "${1}*REDACTED*${2}")

	// 5. Mask LAN IPs
	cleaned = lan192Regex.ReplaceAllString(cleaned, "192.168.***.***")
	cleaned = lan172Regex.ReplaceAllString(cleaned, "172.${1}.***.***")
	cleaned = lan10Regex.ReplaceAllString(cleaned, "10.***.***.***")

	return cleaned
}

// RedactionReader wraps an io.Reader, sanitizing line by line on the fly.
type RedactionReader struct {
	scanner *bufio.Scanner
	buf     bytes.Buffer
}

// NewRedactionReader creates an io.Reader that streams redacted content.
func NewRedactionReader(r io.Reader) io.Reader {
	return &RedactionReader{
		scanner: bufio.NewScanner(r),
	}
}

func (rr *RedactionReader) Read(p []byte) (int, error) {
	if rr.buf.Len() == 0 {
		if !rr.scanner.Scan() {
			if err := rr.scanner.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		line := rr.scanner.Text()
		redacted := RedactSensitiveText(line)
		rr.buf.WriteString(redacted)
		rr.buf.WriteByte('\n')
	}
	return rr.buf.Read(p)
}
