package utils

import (
	"regexp"
	"strings"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape codes from a string
func StripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// SanitizeLogInput replaces newline characters to prevent log injection
func SanitizeLogInput(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
