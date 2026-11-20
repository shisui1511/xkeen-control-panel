package utils

import (
	"regexp"
	"strings"
)

var (
	// urlTokenRegex matches sensitive URL query parameters
	urlTokenRegex = regexp.MustCompile(`(?i)(token|secret|key|password|auth)=([^&\s]+)`)

	// bearerRegex matches Authorization Bearer tokens
	bearerRegex = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9_\-\.+=/]+`)

	// sensitiveKeyAlt is the shared alternation of sensitive key names.
	sensitiveKeyAlt = `(?:privateKey|private_key|private-key|publicKey|public_key|public-key|shortId|short_id|short-id|psk|pre-shared-key|pre_shared_key|preSharedKey|password|secret|uuid|header-protection-key|header_protection_key|headerProtectionKey)`

	// sensitiveKVQuotedRegex matches quoted sensitive values ("...", '...'); the
	// value may contain spaces and commas — capture up to the closing quote.
	sensitiveKVQuotedRegex = regexp.MustCompile(`(?i)(["']?` + sensitiveKeyAlt + `["']?\s*[:=]\s*)(["'])[^"'\r\n]*(["'])`)

	// sensitiveKVRegex matches unquoted sensitive values in logs/configs. The
	// value class is widened to the end of the line (minus quotes and closing
	// brace) so secrets containing spaces/commas are fully redacted rather than
	// truncated at the first whitespace. The value must start with a
	// non-space/non-quote char so a quoted value (already handled above) is not
	// re-matched via the separating whitespace.
	sensitiveKVRegex = regexp.MustCompile(`(?i)(["']?` + sensitiveKeyAlt + `["']?\s*[:=]\s*)[^\s"'\r\n}][^"'\r\n}]*`)

	// lanIPRegex matches private IPv4 addresses
	lan192Regex = regexp.MustCompile(`\b192\.168\.(\d{1,3})\.(\d{1,3})\b`)
	lan172Regex = regexp.MustCompile(`\b172\.(1[6-9]|2[0-9]|3[0-1])\.(\d{1,3})\.(\d{1,3})\b`)
	lan10Regex  = regexp.MustCompile(`\b10\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b`)

	// timestamp1970Regex matches 1970/01/01 or 1970-01-01 timestamps produced before NTP sync
	timestamp1970Regex = regexp.MustCompile(`\b1970[-/](?:01|1)[-/](?:01|1)`)
)

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

	// 4. Redact Key-Value pairs (quoted values first, then unquoted-to-EOL)
	cleaned = sensitiveKVQuotedRegex.ReplaceAllString(cleaned, "${1}${2}*REDACTED*${3}")
	cleaned = sensitiveKVRegex.ReplaceAllString(cleaned, "${1}*REDACTED*")

	// 5. Mask LAN IPs
	cleaned = lan192Regex.ReplaceAllString(cleaned, "192.168.***.***")
	cleaned = lan172Regex.ReplaceAllString(cleaned, "172.${1}.***.***")
	cleaned = lan10Regex.ReplaceAllString(cleaned, "10.***.***.***")

	return cleaned
}
