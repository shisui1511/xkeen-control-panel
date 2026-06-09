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

// Logging is an HTTP middleware that logs request method, URL path, response status code, and duration.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		log.Printf("%s %s %d %s", sanitizeLogInput(r.Method), sanitizeLogInput(r.URL.Path), wrapped.statusCode, time.Since(start))
	})
}
