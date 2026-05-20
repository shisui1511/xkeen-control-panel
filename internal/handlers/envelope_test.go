package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestJSONSuccess verifies JSONSuccess writes {success:true, data:...} envelope.
func TestJSONSuccess(t *testing.T) {
	tests := []struct {
		name     string
		data     interface{}
		wantData interface{}
	}{
		{"nil data", nil, nil},
		{"string data", "hello", "hello"},
		{"map data", map[string]int{"x": 42}, map[string]interface{}{"x": float64(42)}},
		{"bool data", true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			JSONSuccess(rr, tc.data)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", ct)
			}

			var resp APIResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !resp.Success {
				t.Errorf("expected success=true, got false")
			}
			if resp.Error != "" {
				t.Errorf("expected no error, got %q", resp.Error)
			}
		})
	}
}

// TestJSONError verifies JSONError writes {success:false, error:...} with correct status code.
func TestJSONError(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		message string
	}{
		{"bad request", http.StatusBadRequest, "invalid input"},
		{"not found", http.StatusNotFound, "resource not found"},
		{"internal error", http.StatusInternalServerError, "server error"},
		{"conflict", http.StatusConflict, "already in progress"},
		{"method not allowed", http.StatusMethodNotAllowed, "method not allowed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			JSONError(rr, tc.code, tc.message)

			if rr.Code != tc.code {
				t.Errorf("expected status %d, got %d", tc.code, rr.Code)
			}
			if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", ct)
			}

			var resp APIResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Success {
				t.Errorf("expected success=false, got true")
			}
			if resp.Error != tc.message {
				t.Errorf("expected error %q, got %q", tc.message, resp.Error)
			}
		})
	}
}
