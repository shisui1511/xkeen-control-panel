package utils

import "regexp"

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape codes from a string
func StripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}
