package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestShouldLogRequest(t *testing.T) {
	cases := []struct {
		method  string
		status  int
		elapsed time.Duration
		want    bool
	}{
		{http.MethodGet, http.StatusOK, 10 * time.Millisecond, false},
		{http.MethodGet, http.StatusNotModified, 10 * time.Millisecond, false},
		{http.MethodGet, http.StatusSwitchingProtocols, time.Hour, false},
		{http.MethodGet, http.StatusOK, 5 * time.Second, true},
		{http.MethodGet, http.StatusNotFound, time.Millisecond, true},
		{http.MethodGet, http.StatusBadGateway, time.Millisecond, true},
		{http.MethodPost, http.StatusOK, time.Millisecond, true},
		{http.MethodDelete, http.StatusNoContent, time.Millisecond, true},
	}
	for _, c := range cases {
		if got := shouldLogRequest(c.method, c.status, c.elapsed); got != c.want {
			t.Errorf("shouldLogRequest(%s, %d, %s) = %v, want %v", c.method, c.status, c.elapsed, got, c.want)
		}
	}
}
