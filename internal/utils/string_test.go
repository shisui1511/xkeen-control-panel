package utils

import (
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Plain text without ANSI",
			input:    "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name:     "Text with red color",
			input:    "\x1b[31mRed Text\x1b[0m",
			expected: "Red Text",
		},
		{
			name:     "Text with bold green and reset",
			input:    "\x1b[1;32mBold Green\x1b[0m normal",
			expected: "Bold Green normal",
		},
		{
			name:     "Complex 256 color and background codes",
			input:    "\x1b[38;5;196mRed256\x1b[0m \x1b[48;5;226mYellowBg\x1b[0m",
			expected: "Red256 YellowBg",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.expected {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeLogInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Plain text without newlines",
			input:    "normal log entry",
			expected: "normal log entry",
		},
		{
			name:     "String with newline character",
			input:    "first line\nsecond line",
			expected: "first linesecond line",
		},
		{
			name:     "String with carriage return character",
			input:    "prefix\rnewline",
			expected: "prefixnewline",
		},
		{
			name:     "String with CRLF log injection attempt",
			input:    "admin login\r\n[CRITICAL] System breached!\r\n",
			expected: "admin login[CRITICAL] System breached!",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeLogInput(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeLogInput(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
