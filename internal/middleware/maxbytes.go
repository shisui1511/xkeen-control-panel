package middleware

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
)

type maxBytesResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	statusCode  int
}

func (w *maxBytesResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = code
	w.wroteHeader = true
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *maxBytesResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.statusCode == http.StatusRequestEntityTooLarge {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *maxBytesResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *maxBytesResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

type maxBytesReader struct {
	rc io.ReadCloser
	w  *maxBytesResponseWriter
}

func (m *maxBytesReader) Read(p []byte) (n int, err error) {
	n, err = m.rc.Read(p)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			m.w.Header().Set("Content-Type", "application/json")
			m.w.WriteHeader(http.StatusRequestEntityTooLarge)
			_ = json.NewEncoder(m.w.ResponseWriter).Encode(map[string]string{
				"error": "request body too large",
			})
		}
	}
	return n, err
}

func (m *maxBytesReader) Close() error {
	return m.rc.Close()
}

// MaxBytes returns a middleware that limits the request body size.
// The default limit is 2 MB.
// For specific endpoints, the limit is increased to 10 MB:
// - POST /api/snapshots/upload
// - POST /api/outbound/import
// - POST /api/outbound/import-bulk
func MaxBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(2 * 1024 * 1024) // 2 MB by default

		// Exceptions with 10 MB limit for backup and import/restore operations
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/api/snapshots/upload", "/api/outbound/import", "/api/outbound/import-bulk":
				limit = 10 * 1024 * 1024 // 10 MB
			}
		}

		wrappedWriter := &maxBytesResponseWriter{ResponseWriter: w}
		r.Body = &maxBytesReader{
			rc: http.MaxBytesReader(nil, r.Body, limit),
			w:  wrappedWriter,
		}

		next.ServeHTTP(wrappedWriter, r)
	})
}
