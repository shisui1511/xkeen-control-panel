package nodes

import "strings"

// XrayFingerprint переводит отпечаток uTLS в имя Xray (временная версия:
// таблицы перевода добавляются в задаче 3 плана).
func XrayFingerprint(fp string, reality bool) (out string, replaced bool) {
	return strings.ToLower(strings.TrimSpace(fp)), false
}

// MihomoFingerprint переводит отпечаток uTLS в имя Mihomo (временная версия).
func MihomoFingerprint(fp string, reality bool) (out string, replaced bool) {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return "chrome", false
	}
	return fp, false
}
