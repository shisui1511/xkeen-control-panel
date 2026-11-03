package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// gzipMinSize — файлы меньше этого порога сжимать невыгодно: заголовок gzip
// и лишняя работа съедают выигрыш.
const gzipMinSize = 1024

// compressibleExt — текстовые типы статики панели. Картинки PNG уже сжаты.
var compressibleExt = map[string]bool{
	".js":    true,
	".mjs":   true,
	".css":   true,
	".html":  true,
	".json":  true,
	".svg":   true,
	".map":   true,
	".txt":   true,
	".wasm":  true,
	".woff":  false,
	".woff2": false,
}

type gzipEntry struct {
	once sync.Once
	data []byte // nil — сжимать не стоит (мал/не сжался/ошибка чтения)
}

// staticGzip отдаёт встроенную статику сжатой. Фронтенд весит несколько
// мегабайт, а роутер обычно доступен по Wi-Fi: без сжатия первая загрузка
// панели тянула ~400 КБ только стартовых JS/CSS. Сжатие делается один раз на
// файл (встроенная ФС неизменна) и хранится в памяти — CPU роутера не тратится
// на каждый запрос.
type staticGzip struct {
	web   fs.FS
	cache sync.Map // имя файла -> *gzipEntry
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

func (s *staticGzip) compressed(name string) []byte {
	v, ok := s.cache.Load(name)
	if !ok {
		// Запись в кэш только для реально существующих файлов: иначе запросы
		// к выдуманным *.js раздували бы карту без предела.
		if st, err := fs.Stat(s.web, name); err != nil || st.IsDir() {
			return nil
		}
		v, _ = s.cache.LoadOrStore(name, &gzipEntry{})
	}
	e := v.(*gzipEntry)
	e.once.Do(func() { e.data = gzipFile(s.web, name) })
	return e.data
}

// gzipFile возвращает сжатое содержимое или nil, если сжимать не стоит.
func gzipFile(web fs.FS, name string) []byte {
	raw, err := fs.ReadFile(web, name)
	if err != nil || len(raw) < gzipMinSize {
		return nil
	}
	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if _, err := gw.Write(raw); err != nil {
		return nil
	}
	if err := gw.Close(); err != nil {
		return nil
	}
	// Сжатие должно экономить хотя бы ~10%, иначе отдаём как есть.
	if buf.Len() >= len(raw)-len(raw)/10 {
		return nil
	}
	return buf.Bytes()
}

// serve пытается отдать сжатую версию файла. Возвращает false, если запрос
// нужно передать обычному http.FileServer.
func (s *staticGzip) serve(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("Range") != "" {
		return false
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	ext := strings.ToLower(path.Ext(name))
	if !compressibleExt[ext] {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r) {
		return false
	}
	data := s.compressed(name)
	if data == nil {
		return false
	}
	h := w.Header()
	if ct := mime.TypeByExtension(ext); ct != "" {
		h.Set("Content-Type", ct)
	}
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, bytes.NewReader(data))
	}
	return true
}
