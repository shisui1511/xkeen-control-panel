package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebSocketOrigin_Bypass verifies that the WebSocket upgrader rejects
// connections that omit the Origin header, preventing cross-site hijacking.
func TestWebSocketOrigin_Bypass(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		host        string
		expectAllow bool
	}{
		{
			name:        "no Origin header",
			origin:      "",
			host:        "localhost:8090",
			expectAllow: false,
		},
		{
			name:        "matching Origin",
			origin:      "http://localhost:8090",
			host:        "localhost:8090",
			expectAllow: true,
		},
		{
			name:        "mismatched Origin",
			origin:      "http://evil.com",
			host:        "localhost:8090",
			expectAllow: false,
		},
		{
			name:        "invalid Origin URL",
			origin:      "://not-a-url",
			host:        "localhost:8090",
			expectAllow: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/logs/ws", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			got := upgrader.CheckOrigin(req)
			if got != tc.expectAllow {
				t.Errorf("CheckOrigin = %v, want %v", got, tc.expectAllow)
			}
		})
	}
}
