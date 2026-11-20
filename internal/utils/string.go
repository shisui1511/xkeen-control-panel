package utils

import (
	"regexp"
	"strings"
)

// ansiRegex matches ANSI CSI sequences (colors, cursor movements) and
// character set designations such as ESC ( B.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\([a-zA-Z]`)

// StripANSI removes ANSI escape codes from a string. It is the single
// implementation shared by command output cleanup and log redaction.
func StripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// SanitizeLogInput replaces newline characters to prevent log injection
func SanitizeLogInput(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
