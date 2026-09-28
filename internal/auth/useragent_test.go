package auth

import "testing"

func TestParseUserAgent(t *testing.T) {
	cases := []struct {
		name        string
		ua          string
		wantBrowser string
		wantOS      string
	}{
		{
			name:        "Chrome/Windows",
			ua:          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
			wantBrowser: "Chrome",
			wantOS:      "Windows",
		},
		{
			name:        "Edge/Windows",
			ua:          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36 Edg/118.0.2088.76",
			wantBrowser: "Edge",
			wantOS:      "Windows",
		},
		{
			name:        "Firefox/Linux",
			ua:          "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/118.0",
			wantBrowser: "Firefox",
			wantOS:      "Linux",
		},
		{
			name:        "Safari/iOS",
			ua:          "Mozilla/5.0 (iPhone; CPU iPhone OS 16_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1",
			wantBrowser: "Safari",
			wantOS:      "iOS",
		},
		{
			name:        "Chrome/Android",
			ua:          "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Mobile Safari/537.36",
			wantBrowser: "Chrome",
			wantOS:      "Android",
		},
		{
			name:        "empty",
			ua:          "",
			wantBrowser: "",
			wantOS:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			browser, os := ParseUserAgent(tc.ua)
			if browser != tc.wantBrowser || os != tc.wantOS {
				t.Errorf("ParseUserAgent(%q) = (%q, %q), want (%q, %q)", tc.ua, browser, os, tc.wantBrowser, tc.wantOS)
			}
		})
	}
}
