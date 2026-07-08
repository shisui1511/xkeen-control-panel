package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoveryMiddleware(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went terribly wrong")
	})

	recoveryMiddleware := Recovery(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rr := httptest.NewRecorder()

	recoveryMiddleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 Internal Server Error, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	var resp map[string]interface{}
	err := json.NewDecoder(rr.Body).Decode(&resp)
	if err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	successVal, exists := resp["success"]
	if !exists {
		t.Error("expected key 'success' in response")
	} else if successVal != false {
		t.Errorf("expected 'success' to be false, got %v", successVal)
	}

	errorVal, exists := resp["error"]
	if !exists {
		t.Error("expected key 'error' in response")
	} else if errorVal != "Internal Server Error" {
		t.Errorf("expected 'error' to be 'Internal Server Error', got %q", errorVal)
	}
}
