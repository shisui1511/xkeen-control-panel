//go:build router

package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipJSON(t *testing.T) {
	big := `{"data":"` + strings.Repeat("x", 5000) + `"}`
	h := GzipJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/big":
			w.Header().Set("Content-Type", "application/json")
			// Тело пишется по частям, как json.Encoder.
			_, _ = io.WriteString(w, big[:100])
			_, _ = io.WriteString(w, big[100:])
		case "/api/small":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/api/text":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, big)
		case "/api/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "data: 1\n\n")
		case "/api/panic":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"partial":`)
			panic("boom")
		}
	}))
	do := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := do("/api/big")
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("big JSON must be gzipped")
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != big {
		t.Fatal("decompressed body mismatch")
	}

	w = do("/api/small")
	if w.Code != http.StatusTeapot || w.Header().Get("Content-Encoding") != "" || w.Body.String() != `{"ok":true}` {
		t.Fatalf("small JSON must pass through: %d %q", w.Code, w.Body.String())
	}
	if w := do("/api/text"); w.Header().Get("Content-Encoding") != "" || w.Body.Len() != len(big) {
		t.Fatal("non-JSON must pass through")
	}
	if w := do("/api/sse"); w.Header().Get("Content-Encoding") != "" || w.Body.String() != "data: 1\n\n" {
		t.Fatal("SSE must pass through")
	}

	pw := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		r := httptest.NewRequest(http.MethodGet, "/api/panic", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		h.ServeHTTP(pw, r)
	}()
	if pw.Body.Len() != 0 || pw.Header().Get("Content-Encoding") != "" {
		t.Fatalf("partial body leaked on panic: %q", pw.Body.String())
	}
}
