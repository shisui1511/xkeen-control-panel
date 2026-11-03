package middleware

import (
	"bufio"
	"compress/gzip"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// gzipJSONMinSize — ответы короче этого порога уходят как есть: на опросах
// статуса раз в несколько секунд сжатие маленьких ответов лишь тратит CPU роутера.
const gzipJSONMinSize = 1400

var gzipWriterPool = sync.Pool{
	New: func() any {
		gw, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return gw
	},
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.TrimSpace(enc) != "gzip" {
			continue
		}
		if q, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if v, err := strconv.ParseFloat(q, 64); err == nil && v == 0 {
				return false
			}
		}
		return true
	}
	return false
}

// GzipJSON сжимает JSON-ответы API. Списки прокси, подписок и правил Mihomo
// занимают десятки и сотни килобайт, а панель открывают по Wi-Fi или через
// интернет. WebSocket, SSE, уже сжатые ответы и ответы с заданным
// Content-Length проходят без изменений.
func GzipJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.Method == http.MethodHead ||
			r.Header.Get("Upgrade") != "" || !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipJSONWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if p := recover(); p != nil {
				// Паника до отправки заголовков: накопленное отбрасывается, чтобы
				// Recovery ответил 500, а не клиент получил 200 с обрывком тела.
				if gw.gz != nil {
					gw.finish()
				}
				panic(p)
			}
			gw.finish()
		}()
		next.ServeHTTP(gw, r)
	})
}

// gzipJSONWriter копит начало ответа, пока не станет ясно, стоит ли его сжимать.
type gzipJSONWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool // обработчик вызвал WriteHeader
	decided     bool
	buf         []byte
	gz          *gzip.Writer
	hijacked    bool
}

func (w *gzipJSONWriter) WriteHeader(code int) {
	if w.wroteHeader || w.decided {
		return
	}
	w.wroteHeader = true
	w.status = code
	// Ответы без тела и информационные коды не сжимаются.
	if code < 200 || code == http.StatusNoContent || code == http.StatusNotModified || !w.eligible() {
		w.decidePassthrough()
	}
}

func (w *gzipJSONWriter) eligible() bool {
	h := w.Header()
	if h.Get("Content-Encoding") != "" || h.Get("Content-Length") != "" {
		return false
	}
	return strings.HasPrefix(h.Get("Content-Type"), "application/json")
}

// decidePassthrough отправляет накопленное без сжатия.
func (w *gzipJSONWriter) decidePassthrough() {
	w.decided = true
	w.ResponseWriter.WriteHeader(w.status)
	if len(w.buf) > 0 {
		_, _ = w.ResponseWriter.Write(w.buf)
		w.buf = nil
	}
}

func (w *gzipJSONWriter) startGzip() error {
	w.decided = true
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	w.ResponseWriter.WriteHeader(w.status)
	w.gz = gzipWriterPool.Get().(*gzip.Writer)
	w.gz.Reset(w.ResponseWriter)
	_, err := w.gz.Write(w.buf)
	w.buf = nil
	return err
}

func (w *gzipJSONWriter) Write(p []byte) (int, error) {
	if !w.decided {
		if !w.eligible() {
			w.decidePassthrough()
		} else {
			w.buf = append(w.buf, p...)
			if len(w.buf) < gzipJSONMinSize {
				return len(p), nil
			}
			if err := w.startGzip(); err != nil {
				return 0, err
			}
			return len(p), nil
		}
	}
	if w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// Flush завершает решение (поток пошёл — дальше копить нельзя) и сбрасывает данные.
func (w *gzipJSONWriter) Flush() {
	if !w.decided {
		w.decidePassthrough()
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipJSONWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}
	w.hijacked = true
	return h.Hijack()
}

func (w *gzipJSONWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *gzipJSONWriter) finish() {
	if w.hijacked {
		return
	}
	if !w.decided {
		if !w.wroteHeader && len(w.buf) == 0 {
			// Обработчик ничего не написал: net/http сам ответит 200 с пустым телом.
			return
		}
		w.decidePassthrough()
	}
	if w.gz != nil {
		_ = w.gz.Close()
		w.gz.Reset(nil)
		gzipWriterPool.Put(w.gz)
		w.gz = nil
	}
}
