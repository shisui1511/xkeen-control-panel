//go:build router

package geodata

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// rtProbeDat возвращает копию самого маленького настоящего dat стенда размером не
// меньше MinDatSize и его вид (geosite или geoip); без такого файла тест пропускается.
func rtProbeDat(t *testing.T) ([]byte, string) {
	t.Helper()
	files, err := lists.NewIndex(lists.DefaultDatDir).Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	var cand []lists.FileInfo
	for _, f := range files {
		if f.Size >= MinDatSize {
			cand = append(cand, f)
		}
	}
	if len(cand) == 0 {
		t.Skipf("в %s нет dat-файла не меньше %d байт", lists.DefaultDatDir, MinDatSize)
	}
	sort.Slice(cand, func(i, j int) bool { return cand[i].Size < cand[j].Size })
	data, err := os.ReadFile(filepath.Join(lists.DefaultDatDir, cand[0].Name))
	if err != nil {
		t.Fatalf("чтение %s: %v", cand[0].Name, err)
	}
	kind := lists.SniffGeoSite
	if cand[0].Kind == lists.KindIP {
		kind = lists.SniffGeoIP
	}
	return data, kind
}

// rtProbeServer поднимает настоящий HTTP-сервер на loopback роутера и возвращает его адрес.
func rtProbeServer(t *testing.T, mux http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func rtProbeCheck(res ProbeResult, code string) (ProbeCheck, bool) {
	for _, c := range res.Checks {
		if c.Code == code {
			return c, true
		}
	}
	return ProbeCheck{}, false
}

func rtProbeWant(t *testing.T, res ProbeResult, code, level string) {
	t.Helper()
	c, ok := rtProbeCheck(res, code)
	if !ok {
		t.Fatalf("нет проверки %s: %+v", code, res.Checks)
	}
	if c.Level != level {
		t.Fatalf("проверка %s: уровень %s, ждали %s", code, c.Level, level)
	}
}

func rtProbeNo(t *testing.T, res ProbeResult, code string) {
	t.Helper()
	if _, ok := rtProbeCheck(res, code); ok {
		t.Fatalf("лишняя проверка %s: %+v", code, res.Checks)
	}
}

// TestRouterGeodataProbe — проверка своего источника на настоящем HTTP-сервере
// самого роутера (loopback): классификация без полной загрузки файла.
func TestRouterGeodataProbe(t *testing.T) {
	routertest.Current(t)
	data, datKind := rtProbeDat(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/ok.dat", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "ok.dat", time.Time{}, bytes.NewReader(data))
	})
	mux.HandleFunc("/nosize.dat", func(w http.ResponseWriter, r *http.Request) {
		// Ответ частями со сбросом буфера: сервер уходит в chunked, без Content-Length.
		w.Header().Set("Content-Type", "application/octet-stream")
		for off := 0; off < len(data); off += 4096 {
			end := min(off+4096, len(data))
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	page := append([]byte("<!DOCTYPE html>\n<html><head><title>404 Not Found</title></head><body>"), bytes.Repeat([]byte("<!-- -->"), 4096)...)
	mux.HandleFunc("/page.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "page.html", time.Time{}, bytes.NewReader(page))
	})
	addr := rtProbeServer(t, mux)
	base := "http://" + addr

	t.Run("ok", func(t *testing.T) {
		res := Probe(context.Background(), ProbeInput{File: "own_test.dat", URL: base + "/ok.dat", Mode: ProbeModeAdd, GeoDir: lists.DefaultDatDir})
		if res.Blocked {
			t.Fatalf("годный dat заблокирован: %+v", res.Checks)
		}
		rtProbeWant(t, res, "name_ok", ProbeLevelOK)
		rtProbeWant(t, res, "scheme_http", ProbeLevelWarn)
		rtProbeWant(t, res, "reach_ok", ProbeLevelOK)
		rtProbeWant(t, res, "size_ok", ProbeLevelOK)
		rtProbeWant(t, res, "kind", ProbeLevelOK)
		if res.Route != ProbeRouteDirect {
			t.Fatalf("путь %q, ждали %q", res.Route, ProbeRouteDirect)
		}
		if res.Size != int64(len(data)) {
			t.Fatalf("размер %d, ждали %d", res.Size, len(data))
		}
		if res.Kind != datKind {
			t.Fatalf("вид %q, у файла в индексе %q", res.Kind, datKind)
		}
		rtProbeNo(t, res, "space")
		rtProbeNo(t, res, "heavy")
	})

	t.Run("nosize", func(t *testing.T) {
		res := Probe(context.Background(), ProbeInput{File: "own_test.dat", URL: base + "/nosize.dat", Mode: ProbeModeAdd})
		rtProbeWant(t, res, "size_none", ProbeLevelBlock)
		if !res.Blocked {
			t.Fatalf("ответ без размера не заблокирован: %+v", res.Checks)
		}
	})

	t.Run("html", func(t *testing.T) {
		res := Probe(context.Background(), ProbeInput{File: "own_test.dat", URL: base + "/page.html", Mode: ProbeModeAdd})
		rtProbeWant(t, res, "html", ProbeLevelBlock)
		if !res.Blocked {
			t.Fatalf("HTML-страница не заблокирована: %+v", res.Checks)
		}
	})
}
