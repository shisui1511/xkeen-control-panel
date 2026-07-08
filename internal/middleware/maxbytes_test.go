package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMaxBytesMiddleware_DefaultLimit(t *testing.T) {
	// Handler that tries to read the whole body
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			// maxBytesReader will write the header/body.
			// The handler might still write error, but maxBytesResponseWriter will suppress it.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})

	mw := MaxBytes(handler)

	t.Run("UnderDefaultLimit", func(t *testing.T) {
		body := make([]byte, 1*1024*1024) // 1 MB
		req := httptest.NewRequest(http.MethodPost, "/api/some-endpoint", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		mw.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})

	t.Run("OverDefaultLimit", func(t *testing.T) {
		body := make([]byte, 3*1024*1024) // 3 MB (limit is 2 MB)
		req := httptest.NewRequest(http.MethodPost, "/api/some-endpoint", bytes.NewReader(body))
		rr := httptest.NewRecorder()

		mw.ServeHTTP(rr, req)

		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("expected 413, got %d", rr.Code)
		}

		var resp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Error != "request body too large" {
			t.Errorf("expected error 'request body too large', got '%s'", resp.Error)
		}
	})
}

func TestMaxBytesMiddleware_ExceptionLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})

	mw := MaxBytes(handler)

	paths := []string{
		"/api/snapshots/upload",
		"/api/outbound/import",
		"/api/outbound/import-bulk",
	}

	for _, path := range paths {
		t.Run("UnderExceptionLimit_"+path, func(t *testing.T) {
			body := make([]byte, 5*1024*1024) // 5 MB (default limit is 2 MB, exception is 10 MB)
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			rr := httptest.NewRecorder()

			mw.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("path %s: expected 200, got %d", path, rr.Code)
			}
		})

		t.Run("OverExceptionLimit_"+path, func(t *testing.T) {
			body := make([]byte, 11*1024*1024) // 11 MB (limit is 10 MB)
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			rr := httptest.NewRecorder()

			mw.ServeHTTP(rr, req)

			if rr.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("path %s: expected 413, got %d", path, rr.Code)
			}
		})
	}
}
