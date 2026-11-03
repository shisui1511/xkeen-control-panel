//go:build router

package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticGzip(t *testing.T) {
	big := strings.Repeat("console.log('xkeen');\n", 200)
	web := fstest.MapFS{
		"index.html":      {Data: []byte("<html>" + strings.Repeat("a", 2000) + "</html>")},
		"assets/app-1.js": {Data: []byte(big)},
		"assets/tiny.js":  {Data: []byte("x")},
		"icons/a.png":     {Data: bytes.Repeat([]byte{0}, 4000)},
	}
	g := &staticGzip{web: web}

	get := func(p, ae string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		if ae != "" {
			r.Header.Set("Accept-Encoding", ae)
		}
		w := httptest.NewRecorder()
		if !g.serve(w, r) {
			w.Code = 0
		}
		return w
	}

	w := get("/assets/app-1.js", "gzip, deflate, br")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip, got code=%d enc=%q", w.Code, w.Header().Get("Content-Encoding"))
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("content-type %q", w.Header().Get("Content-Type"))
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != big {
		t.Fatal("decompressed body mismatch")
	}

	if w := get("/", "gzip"); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("index not gzipped: %d", w.Code)
	}
	for _, c := range []struct{ p, ae string }{
		{"/assets/app-1.js", ""},
		{"/assets/app-1.js", "gzip;q=0"},
		{"/assets/tiny.js", "gzip"},
		{"/icons/a.png", "gzip"},
		{"/assets/missing.js", "gzip"},
	} {
		if w := get(c.p, c.ae); w.Code != 0 {
			t.Fatalf("%s %q: expected fallthrough", c.p, c.ae)
		}
	}
	if _, ok := g.cache.Load("assets/missing.js"); ok {
		t.Fatal("missing file must not be cached")
	}
}
