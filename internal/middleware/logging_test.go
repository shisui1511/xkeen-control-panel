package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type flushableResponseWriter struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushableResponseWriter) Flush() {
	f.flushed = true
}

func TestLoggingFlusher(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected ResponseWriter wrapped by middleware to implement http.Flusher")
			return
		}
		flusher.Flush()
	})

	rec := &flushableResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
	}
	req := httptest.NewRequest("GET", "/test", nil)

	loggingHandler := Logging(handler)
	loggingHandler.ServeHTTP(rec, req)

	if !rec.flushed {
		t.Error("expected Flush to be called on underlying ResponseWriter")
	}
}
