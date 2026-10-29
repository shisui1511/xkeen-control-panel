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
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Путь сбоя загрузки (D-28): настоящий `xkeen -ug` ходит к настоящему HTTP-серверу на
// loopback роутера, панель разбирает настоящий вывод XKeen. Сервер отдаёт адрес без
// размера, HTML-страницу и закрытый порт; рядом в том же запуске — маленький
// настоящий файл с GitHub. Ядро не переключается и не читается — метки нет; сценарий
// идёт в план-сессии 146.1-21.

const (
	rtFailNoSize = "xcp-fail-nosize.dat"
	rtFailHTML   = "xcp-fail-html.dat"
	rtFailDown   = "xcp-fail-down.dat"
	rtFailOK     = "xcp-ok-roscom.dat"
)

// rtFailDat возвращает копию самого маленького настоящего dat стенда размером не
// меньше MinDatSize; без такого файла тест пропускается.
func rtFailDat(t *testing.T) []byte {
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
	return data
}

// rtFailServe поднимает настоящий HTTP-сервер на loopback роутера: /nosize.dat —
// настоящий dat частями со сбросом буфера (без Content-Length, в том числе на HEAD),
// /page.html — HTML-страница больше 24576 байт (меньшую XKeen отвергнет по размеру
// раньше HTML-маркера). Возвращает адрес вида 127.0.0.1:порт.
func rtFailServe(t *testing.T, data []byte) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/nosize.dat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		for off := 0; off < len(data); off += 4096 {
			end := min(off+4096, len(data))
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	page := append([]byte("<!DOCTYPE html>\n<html><head><title>404 Not Found</title></head><body>"),
		bytes.Repeat([]byte("<!-- -->"), 4096)...)
	mux.HandleFunc("/page.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "page.html", time.Time{}, bytes.NewReader(page))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

// rtFailClosedAddr — адрес закрытого порта: Listen и сразу Close.
func rtFailClosedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// rtFailBlock — строки блока файла в выводе XKeen: от «Запрос информации о <файл>...»
// до следующего блока.
func rtFailBlock(lines []string, file string) []string {
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "Запрос информации о "+file+"...") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "Запрос информации о ") {
			end = i
			break
		}
	}
	return lines[start:end]
}

// rtFailReason — причина по последней строке ошибки XKeen в блоке (маркеры из
// RESEARCH): нет размера, HTML, размер, сеть. Пусто — итоговой строки ошибки нет.
func rtFailReason(block []string) string {
	reason := ""
	for _, l := range block {
		switch {
		case strings.Contains(l, "Не удалось определить ожидаемый размер"):
			reason = ReasonNoSize
		case strings.Contains(l, "HTML-страница"):
			reason = ReasonHTML
		case strings.Contains(l, "Размер загруженного файла") && strings.Contains(l, "не соответствует"):
			reason = ReasonSize
		case strings.Contains(strings.ToLower(l), "не удалось загрузить"):
			reason = ReasonNetwork
		}
	}
	return reason
}

// rtFailEntry записывает в xkeen.json стенда запись file → url (прежнюю с этим именем
// сперва убирает).
func rtFailEntry(t *testing.T, store *lists.GeodataStore, file, url string) {
	t.Helper()
	if err := store.Apply(lists.GeodataOp{Kind: lists.OpRemove, File: file}, "", true); err != nil && lists.ErrorCode(err) != lists.ErrEntryNotFound.Code {
		t.Fatalf("Apply(remove %s): %v", file, err)
	}
	if err := store.Apply(lists.GeodataOp{Kind: lists.OpAdd, File: file, URL: url}, "", true); err != nil {
		t.Fatalf("Apply(add %s): %v", file, err)
	}
}

// TestRouterGeodataUpdateFailures — сбои загрузки через настоящий XKeen: у файла итог
// kept (старый файл не тронут) или failed (файла нет) с причиной из итоговой строки
// ошибки XKeen и уведомление раздела; в том же запуске маленький настоящий файл с
// GitHub — updated.
func TestRouterGeodataUpdateFailures(t *testing.T) {
	tg := routertest.Current(t)
	if _, err := os.Stat(rtRunXKeenBin); err != nil {
		t.Skipf("нет %s: XKeen не установлен", rtRunXKeenBin)
	}
	rtGeoSkipWindow(t, 25*time.Minute)

	dir := rtGeoTempDir(t, tg.WorkDir)
	layer := rtGeoLayer(t, dir)
	store := rtGeoStore(t, "/opt/etc/xkeen")
	status, err := store.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.JQ {
		t.Skip("стенд: нет jq")
	}

	// XKeen читает только настоящий xkeen.json: исходное содержимое возвращается целиком.
	orig, err := os.ReadFile(rtRunConfig)
	if err != nil {
		t.Skipf("нет %s: %v", rtRunConfig, err)
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(rtRunConfig); err == nil {
		perm = fi.Mode().Perm()
	}
	t.Cleanup(func() {
		if err := utils.AtomicWriteFile(rtRunConfig, orig, perm); err != nil {
			t.Errorf("возврат xkeen.json стенда: %v", err)
		}
	})

	// Адрес без прокси: иначе XKeen пошёл бы к loopback через gh_proxy (Pitfall 1).
	if status.GhProxy != "" {
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpGhProxy, Value: ""}, "", true); err != nil {
			t.Fatalf("Apply(gh_proxy): %v", err)
		}
	}
	if !status.GeofileUpdate {
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpEnableUpdate}, "", true); err != nil {
			t.Fatalf("Apply(enable_update): %v", err)
		}
	}

	data := rtFailDat(t)
	addr := rtFailServe(t, data)
	closed := rtFailClosedAddr(t)

	// URL набора roscomvpn_geosite из каталога: маленький настоящий файл с GitHub.
	cat, err := lists.Resolve(lists.NewIndex(lists.DefaultDatDir), nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	okURL := ""
	for _, e := range cat.Extended {
		if e.ID == "roscomvpn_geosite" {
			okURL = e.URL
		}
	}
	if okURL == "" {
		t.Fatal("в каталоге нет набора roscomvpn_geosite")
	}

	// Файлы в каталоге dat: у первых двух есть старая копия, у остальных файла нет.
	for _, name := range []string{rtFailNoSize, rtFailHTML} {
		if err := os.WriteFile(filepath.Join(rtRunDatDir, name), data, 0o644); err != nil {
			t.Fatalf("копия %s: %v", name, err)
		}
	}
	for _, name := range []string{rtFailDown, rtFailOK} {
		_ = os.Remove(filepath.Join(rtRunDatDir, name))
	}
	t.Cleanup(func() {
		for _, name := range []string{rtFailNoSize, rtFailHTML, rtFailDown, rtFailOK} {
			_ = os.Remove(filepath.Join(rtRunDatDir, name))
		}
	})
	rtFailEntry(t, store, rtFailNoSize, "http://"+addr+"/nosize.dat")
	rtFailEntry(t, store, rtFailHTML, "http://"+addr+"/page.html")
	rtFailEntry(t, store, rtFailDown, "http://"+closed+"/gone.dat")
	rtFailEntry(t, store, rtFailOK, okURL)

	before := map[string]string{
		rtFailNoSize: hashFile(filepath.Join(rtRunDatDir, rtFailNoSize)),
		rtFailHTML:   hashFile(filepath.Join(rtRunDatDir, rtFailHTML)),
	}

	kernels := map[string]bool{}
	for _, name := range []string{"xray", "mihomo"} {
		if rtRunPid(name) != "" {
			kernels[name] = true
		}
	}

	svc := rtGeoService(t, layer, store, lists.NewIndex(lists.DefaultDatDir), dir)
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Minute)
	defer cancel()
	runErr := svc.run(ctx, false, "test")
	for name := range kernels {
		if !rtRunWaitKernel(name, 60*time.Second) {
			t.Errorf("ядро %s не вернулось за 60 с после -ug", name)
		}
	}

	svc.mu.Lock()
	lr := svc.st.LastRun
	results := make(map[string]FileResult, len(svc.st.Results))
	for k, v := range svc.st.Results {
		results[k] = v
	}
	svc.mu.Unlock()
	if lr == nil {
		t.Fatalf("итога запуска нет: %v", runErr)
	}
	if lr.Reason == RunErrNetwork {
		t.Skip("сеть стенда: XKeen не начал загрузку")
	}
	if runErr != nil {
		t.Fatalf("запуск: %v", runErr)
	}

	lines := svc.ugOut.Lines()
	for _, name := range []string{rtFailNoSize, rtFailHTML, rtFailDown, rtFailOK} {
		for _, l := range rtFailBlock(lines, name) {
			// XKeen печатает имя файла; строки с адресом в журнал теста не попадают.
			if !strings.Contains(l, "127.0.0.1") && !strings.Contains(l, "http") {
				t.Logf("%s: %s", name, l)
			}
		}
	}

	// (1) Сбои: итог, старый файл, причина из итоговой строки XKeen.
	wantStatus := map[string]string{rtFailNoSize: StatusKept, rtFailHTML: StatusKept, rtFailDown: StatusFailed}
	for _, name := range []string{rtFailNoSize, rtFailHTML, rtFailDown} {
		r, ok := results[name]
		if !ok {
			t.Errorf("%s: нет итога запуска", name)
			continue
		}
		if r.Status != wantStatus[name] {
			t.Errorf("%s: статус %q, ожидался %q", name, r.Status, wantStatus[name])
		}
		block := rtFailBlock(lines, name)
		if len(block) == 0 {
			t.Errorf("%s: блока файла нет в выводе XKeen", name)
			continue
		}
		want := rtFailReason(block)
		if want == "" {
			t.Errorf("%s: в блоке нет итоговой строки ошибки XKeen", name)
		}
		if r.Reason == "" || r.Reason != want {
			t.Errorf("%s: причина %q, итоговая строка XKeen даёт %q", name, r.Reason, want)
		}
		switch r.Reason {
		case ReasonNoSize, ReasonHTML, ReasonSize, ReasonNetwork:
		default:
			t.Errorf("%s: причина %q вне множества no_size, html, size, network", name, r.Reason)
		}
	}
	// Размер не получен ни одним путём.
	for _, name := range []string{rtFailNoSize, rtFailDown} {
		if results[name].Reason != ReasonNoSize {
			t.Errorf("%s: причина %q, ожидалась %q", name, results[name].Reason, ReasonNoSize)
		}
	}
	for name, h := range before {
		if got := hashFile(filepath.Join(rtRunDatDir, name)); got != h {
			t.Errorf("%s: старый файл изменён (SHA-256 до %s, после %s)", name, h, got)
		}
	}
	if _, err := os.Lstat(filepath.Join(rtRunDatDir, rtFailDown)); err == nil {
		t.Errorf("%s: файл появился, хотя загрузка не удалась", rtFailDown)
	}

	// (2) Успешный путь в том же запуске.
	ok, found := results[rtFailOK]
	if !found || ok.Status != ResultUpdated {
		t.Errorf("%s: итог %+v, ожидался %s", rtFailOK, ok, ResultUpdated)
	}
	if fi, err := os.Stat(filepath.Join(rtRunDatDir, rtFailOK)); err != nil || fi.Size() < MinDatSize {
		t.Errorf("%s: файл не лежит или меньше %d байт: %v", rtFailOK, MinDatSize, err)
	}
	if _, err := lists.NewIndex(lists.DefaultDatDir).Tags(rtFailOK); err != nil {
		t.Errorf("%s: теги не читаются: %v", rtFailOK, err)
	}

	// (3) Уведомления раздела.
	notices := map[string]map[string]string{}
	for _, n := range layer.Snapshot().Notices {
		notices[n.ID] = n.Params
	}
	for _, name := range []string{rtFailNoSize, rtFailHTML, rtFailDown} {
		p, has := notices["geodata_update:"+name]
		if !has {
			t.Errorf("нет уведомления geodata_update:%s", name)
			continue
		}
		if p["file"] != name || p["reason"] != results[name].Reason {
			t.Errorf("уведомление %s: params %v, итог %+v", name, p, results[name])
		}
	}
	if _, has := notices["geodata_update:"+rtFailOK]; has {
		t.Errorf("у успешно обновлённого %s есть уведомление", rtFailOK)
	}

	// (4) Итог запуска.
	if lr.Outcome != outcomePartial || lr.NotUpdated < 3 || !lr.Parsed {
		t.Errorf("итог запуска: %+v, ожидались partial, not_updated >= 3, parsed", *lr)
	}
}
