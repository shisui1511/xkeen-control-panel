package middleware

import (
	"bufio"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

func sanitizeLogInput(s string) string {
	return utils.SanitizeLogInput(s)
}

// slowRequestThreshold marks read requests worth logging even when they succeed.
const slowRequestThreshold = 3 * time.Second

// shouldLogRequest keeps the access log useful on router flash: the UI polls
// status endpoints every few seconds, so successful reads are skipped while
// state changes, errors and slow requests are always recorded.
func shouldLogRequest(method string, status int, elapsed time.Duration) bool {
	if status == http.StatusSwitchingProtocols {
		return false
	}
	if method != http.MethodGet && method != http.MethodHead {
		return true
	}
	return status >= http.StatusBadRequest || elapsed >= slowRequestThreshold
}

// Logging is an HTTP middleware that logs request method, URL path, response status code, and duration.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Handlers such as the Mihomo reverse proxy rewrite r.URL.Path.
		path := r.URL.Path
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		elapsed := time.Since(start)
		if shouldLogRequest(r.Method, wrapped.statusCode, elapsed) {
			log.Printf("%s %s %d %s", sanitizeLogInput(r.Method), sanitizeLogInput(path), wrapped.statusCode, elapsed)
		}
	})
}
