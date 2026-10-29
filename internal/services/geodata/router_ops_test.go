//go:build router

package geodata

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Сценарии операций записи менеджера geodata на устройстве. Ядро не читается и не
// переключается — метки ядра нет.

// rtOpsEntry — запись с этим файлом и адресом есть в xkeen.json хранилища.
func rtOpsEntry(t *testing.T, store *lists.GeodataStore, file, url string) bool {
	t.Helper()
	st, err := store.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, e := range st.Entries {
		if e.File == file && (url == "" || e.URL == url) {
			return true
		}
	}
	return false
}

// rtOpsCode — код ошибки операции: код пакета lists или probe_blocked.
func rtOpsCode(err error) string {
	var pb *ProbeBlockedError
	if errors.As(err, &pb) {
		return "probe_blocked"
	}
	return lists.ErrorCode(err)
}

// rtOpsWantCode проверяет, что операция отказала с кодом want.
func rtOpsWantCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ожидался отказ %s, запись прошла", want)
	}
	if got := rtOpsCode(err); got != want {
		t.Fatalf("код отказа %q (%v), ожидался %s", got, err, want)
	}
}

// rtOpsKernels — какие ядра запущены сейчас: после -ug они обязаны вернуться.
func rtOpsKernels() map[string]bool {
	kernels := map[string]bool{}
	for _, name := range []string{"xray", "mihomo"} {
		if rtRunPid(name) != "" {
			kernels[name] = true
		}
	}
	return kernels
}

// rtOpsLines — вывод последнего `-ug` построчно.
func rtOpsLines(svc *Service) []string {
	text := svc.Output("ug", 0).Text
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// rtOpsBlocks — сколько блоков «Запрос информации о <название>...» в выводе.
func rtOpsBlocks(lines []string, name string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, "Запрос информации о "+name+"...") {
			n++
		}
	}
	return n
}

// TestRouterGeodataEnableStandard — двухшаговое включение стандартного набора (D-01)
// настоящим XKeen: временная запись, первый `-ug` качает файл, панель убирает запись и
// сообщает об этом, второй `-ug` качает файл один раз по стандартной ветке XKeen.
// xkeen.json стенда возвращается целиком, файл набора — как был.
func TestRouterGeodataEnableStandard(t *testing.T) {
	tg := routertest.Current(t)
	if _, err := os.Stat(rtRunXKeenBin); err != nil {
		t.Skipf("нет %s: XKeen не установлен", rtRunXKeenBin)
	}
	rtGeoSkipWindow(t, 25*time.Minute)

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
	if !status.GeofileUpdate {
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpEnableUpdate}, "", true); err != nil {
			t.Fatalf("Apply(enable_update): %v", err)
		}
	}

	// Набор: первый из списка, файл которого есть на стенде (он переименовывается во
	// временное имя без суффикса .dat и возвращается). Нет ни одного — берётся первый
	// отсутствующий без переименования: включение с нуля и есть сценарий. zkeen не
	// берётся: его второе имя появляется только на втором запуске.
	var set StandardSet
	renamed := ""
	for _, id := range []string{"refilter_geoip", "refilter_geosite", "v2fly_geosite"} {
		cand, _ := standardSet(id)
		if rtRunExists(filepath.Join(rtRunDatDir, cand.File)) {
			set = cand
			renamed = filepath.Join(rtRunDatDir, cand.File)
			break
		}
	}
	if renamed == "" {
		set, _ = standardSet("refilter_geoip")
	}
	path := filepath.Join(rtRunDatDir, set.File)
	backup := path + ".xcp-bak"
	if renamed != "" {
		if err := os.Rename(path, backup); err != nil {
			t.Fatalf("перенос %s: %v", set.File, err)
		}
	}
	t.Cleanup(func() {
		// Файл, скачанный тестом, заменяется прежним (или удаляется, если прежнего не было).
		_ = os.Remove(path)
		if renamed != "" {
			if err := os.Rename(backup, path); err != nil {
				t.Errorf("возврат %s: %v", set.File, err)
			}
		}
	})

	dir := rtGeoTempDir(t, tg.WorkDir)
	layer := rtGeoLayer(t, dir)
	index := lists.NewIndex(lists.DefaultDatDir)
	svc := rtGeoService(t, layer, store, index, dir)
	svc.Start()

	if err := svc.staticCheck(); err != nil {
		t.Skipf("стенд: %v", err)
	}
	wantURL := svc.vars.URL(set)

	// Шаг 1: включение — временная запись под стандартным именем с адресом XKeen.
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if err := svc.Write(ctx, WriteOp{Kind: WriteEnableSet, Set: set.ID}, "", true, false); err != nil {
		t.Fatalf("Write(enable_set %s): %v", set.ID, err)
	}
	if !rtOpsEntry(t, store, set.File, wantURL) {
		t.Fatalf("в xkeen.json нет записи %s с адресом XKeen", set.File)
	}
	if p, ok := svc.pendingFor(set.File); !ok || p.URL != wantURL {
		t.Fatalf("ожидание включения %s не запомнено: %+v", set.File, p)
	}
	rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteEnableSet, Set: set.ID}, "", true, false), "already_present")

	// Первый -ug: файл скачивает запись geodata, постшаг убирает её.
	kernels := rtOpsKernels()
	runErr := svc.run(ctx, false, "test")
	for name := range kernels {
		if !rtRunWaitKernel(name, 60*time.Second) {
			t.Errorf("ядро %s не вернулось за 60 с после -ug", name)
		}
	}
	svc.mu.Lock()
	lr := svc.st.LastRun
	res := svc.st.Results[set.File]
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
	if !svc.datReady(set.File) {
		if res.Status == StatusFailed || res.Status == StatusKept {
			t.Skipf("сеть стенда: набор %s не скачан (%s)", set.ID, res.Reason)
		}
		t.Fatalf("после -ug файла %s нет или он не dat; итог %+v", set.File, res)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() < MinDatSize {
		t.Fatalf("файл %s: %v, размер меньше %d", set.File, err, MinDatSize)
	}
	if rtOpsEntry(t, store, set.File, wantURL) {
		t.Errorf("временная запись %s осталась в xkeen.json после появления файла", set.File)
	}
	if svc.hasPending() {
		t.Errorf("ожидание включения не снято")
	}
	svc.mu.Lock()
	cleaned := svc.st.Cleaned[set.File]
	svc.mu.Unlock()
	if cleaned.IsZero() {
		t.Errorf("нет отметки Cleaned по %s", set.File)
	}
	found := false
	for _, n := range layer.Snapshot().Notices {
		if n.ID == "geodata_cleanup:"+set.File {
			found = true
		}
	}
	if !found {
		t.Errorf("нет уведомления geodata_cleanup:%s", set.File)
	}

	// Шаг 2: файл на месте, записи нет — набор качается один раз, стандартной веткой.
	runErr = svc.run(ctx, false, "test")
	for name := range kernels {
		if !rtRunWaitKernel(name, 60*time.Second) {
			t.Errorf("ядро %s не вернулось за 60 с после второго -ug", name)
		}
	}
	if runErr != nil {
		t.Fatalf("второй запуск: %v", runErr)
	}
	lines := rtOpsLines(svc)
	if n := rtOpsBlocks(lines, set.Display); n != 1 {
		t.Errorf("блок %q во втором -ug %d раз, ожидался один", set.Display, n)
	}
	if n := rtOpsBlocks(lines, set.File); n != 0 {
		t.Errorf("блок записи %q во втором -ug %d раз: временная запись не убрана", set.File, n)
	}
}

// rtOpsConfig — xkeen.json теста: комментарий владельца, geofile_update выключен,
// gh_proxy пуст и одна запись geodata.
const rtOpsConfig = `{
  // xcp router test: комментарий владельца должен пережить запись
  "xkeen": {
    "geofile_update": false,
    "gh_proxy": "",
    "xray": {
      "geodata": [
        { "file": "xcp-existing.dat", "url": "http://127.0.0.1:9/existing.dat" }
      ]
    }
  }
}
`

const rtOpsComment = "комментарий владельца должен пережить запись"

// TestRouterGeodataOps — операции записи на xkeen.json во временном каталоге:
// включение обновления, свой источник после проверки на настоящем HTTP-сервере
// loopback, блок проверки, занятое имя, смена адреса, gh_proxy, дубль, набор каталога,
// «довести включение», отказы и установка jq. `-ug` не запускается.
func TestRouterGeodataOps(t *testing.T) {
	tg := routertest.Current(t)
	if _, err := os.Stat(rtRunXKeenBin); err != nil {
		t.Skipf("нет %s: XKeen не установлен", rtRunXKeenBin)
	}

	dir := rtGeoTempDir(t, tg.WorkDir)
	datDir := filepath.Join(dir, "dat")
	xkDir := filepath.Join(dir, "xkeen")
	for _, d := range []string{datDir, xkDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(xkDir, "xkeen.json")
	if err := os.WriteFile(cfgPath, []byte(rtOpsConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	store := rtGeoStore(t, xkDir)
	status, err := store.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.GeofileUpdate {
		t.Fatalf("в xkeen.json теста geofile_update должен быть выключен")
	}
	layer := rtGeoLayer(t, dir)
	svc := rtGeoService(t, layer, store, lists.NewIndex(datDir), dir)

	read := func() string {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	needJQ := func(t *testing.T) {
		t.Helper()
		if !status.JQ {
			t.Skip("стенд: нет jq")
		}
	}
	backups := func() int {
		m, _ := filepath.Glob(filepath.Join(tg.WorkDir, "rt-geodata-*", "backup", "xkeen", "*"))
		return len(m)
	}
	if err := svc.staticCheck(); err != nil {
		t.Skipf("стенд: %v", err)
	}

	// Сервер на loopback роутера: настоящий dat с Content-Length и HTML-страница.
	data, _ := rtProbeDat(t)
	var hits atomic.Int64
	page := append([]byte("<!DOCTYPE html>\n<html><head><title>404 Not Found</title></head><body>"),
		bytes.Repeat([]byte("<!-- -->"), 4096)...)
	mux := http.NewServeMux()
	for _, name := range []string{"/a.dat", "/b.dat"} {
		mux.HandleFunc(name, func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeContent(w, r, "x.dat", time.Time{}, bytes.NewReader(data))
		})
	}
	mux.HandleFunc("/page.html", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "page.html", time.Time{}, bytes.NewReader(page))
	})
	addr := rtProbeServer(t, mux)
	urlA, urlB, urlPage := "http://"+addr+"/a.dat", "http://"+addr+"/b.dat", "http://"+addr+"/page.html"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	const own = "xcp-ops-test.dat"

	t.Run("enable_update", func(t *testing.T) {
		needJQ(t)
		op := WriteOp{Kind: WriteEnableUpdate}
		pv, err := svc.Preview(op)
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if !strings.Contains(pv.After, `"geofile_update": true`) || pv.Base != lists.ContentBase(read()) || !pv.CommentsKept {
			t.Fatalf("предпросмотр: %+v", pv)
		}
		if err := svc.Write(ctx, op, pv.Base, false, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if st, _ := store.Status(); !st.GeofileUpdate {
			t.Fatalf("geofile_update не включён")
		}
		if !strings.Contains(read(), rtOpsComment) {
			t.Errorf("комментарии потеряны")
		}
	})

	t.Run("add", func(t *testing.T) {
		needJQ(t)
		res, err := svc.Probe(ctx, own, urlA, ProbeModeAdd)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if res.Blocked {
			t.Fatalf("проверка заблокировала настоящий dat: %+v", res)
		}
		rtProbeWant(t, res, "scheme_http", ProbeLevelWarn)
		afterProbe := hits.Load()

		op := WriteOp{Kind: WriteAdd, File: own, URL: urlA}
		pv, err := svc.Preview(op)
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		before := read()
		if pv.Base != lists.ContentBase(before) || pv.Before != before || !pv.CommentsKept {
			t.Fatalf("предпросмотр: base совпадает=%v comments_kept=%v", pv.Base == lists.ContentBase(before), pv.CommentsKept)
		}
		nb := backups()
		if err := svc.Write(ctx, op, pv.Base, false, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if hits.Load() != afterProbe {
			t.Errorf("запись повторила сетевую проверку, хотя кэш свежий (запросов %d, было %d)", hits.Load(), afterProbe)
		}
		if !rtOpsEntry(t, store, own, urlA) {
			t.Fatalf("записи %s нет в xkeen.json", own)
		}
		if !strings.Contains(read(), rtOpsComment) {
			t.Errorf("комментарии потеряны")
		}
		if backups() <= nb {
			t.Errorf("резервная копия xkeen.json не создана")
		}
		if svc.hasPending() {
			t.Errorf("свой источник попал в ожидания включения")
		}
	})

	t.Run("add_blocked", func(t *testing.T) {
		needJQ(t)
		before := read()
		err := svc.Write(ctx, WriteOp{Kind: WriteAdd, File: "xcp-ops-html.dat", URL: urlPage}, "", false, false)
		rtOpsWantCode(t, err, "probe_blocked")
		var pb *ProbeBlockedError
		if !errors.As(err, &pb) || !pb.Result.Blocked || len(pb.Result.Checks) == 0 {
			t.Fatalf("в отказе нет результата проверки: %+v", pb)
		}
		if read() != before {
			t.Errorf("xkeen.json изменился при блоке проверки")
		}
	})

	t.Run("refusals", func(t *testing.T) {
		needJQ(t)
		before := read()
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteAdd, File: "geosite_refilter.dat", URL: urlA}, "", false, false), "name_taken")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteAdd, File: "zkeen.dat", URL: urlA}, "", false, false), "name_taken")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteAdd, File: "../x.dat", URL: urlA}, "", false, false), "name_invalid")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteAdd, File: "xcp-ops-x.txt", URL: urlA}, "", false, false), "name_invalid")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteAdd, File: "xcp-ops-x.dat", URL: "ftp://127.0.0.1/x.dat"}, "", false, false), "url_invalid")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteSetURL, File: "xcp-nonexistent.dat", URL: urlB}, "", false, false), "entry_not_found")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteGhProxy, Value: "https://exa mple.org"}, "", false, false), "url_invalid")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteGhProxy, Value: "ftp://example.org"}, "", false, false), "url_invalid")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteEnableSet, Set: "no_such_set"}, "", false, false), "set_unknown")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: "no_such_kind"}, "", false, false), "bad_op")
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteFinishInclude, File: "geosite_refilter.dat"}, "", false, false), "not_pending")
		if read() != before {
			t.Errorf("xkeen.json изменился при отказах")
		}
	})

	t.Run("set_url", func(t *testing.T) {
		needJQ(t)
		if err := svc.Write(ctx, WriteOp{Kind: WriteSetURL, File: own, URL: urlB}, "", false, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if !rtOpsEntry(t, store, own, urlB) || rtOpsEntry(t, store, own, urlA) {
			t.Fatalf("адрес %s не заменён", own)
		}
		if !strings.Contains(read(), rtOpsComment) {
			t.Errorf("комментарии потеряны")
		}
	})

	t.Run("set_url_invalid", func(t *testing.T) {
		// Запись с пустым адресом (Invalid) чинится сменой адреса: строка источника
		// предлагает «Изменить адрес» и для неё.
		needJQ(t)
		const bad = "xcp-ops-invalid.dat"
		raw := read()
		withBad := strings.Replace(raw, `"geodata": [`, `"geodata": [`+"\n        { \"file\": \""+bad+"\", \"url\": \"\" },", 1)
		if withBad == raw {
			t.Fatalf("в xkeen.json теста нет массива geodata:\n%s", raw)
		}
		if err := os.WriteFile(cfgPath, []byte(withBad), 0o644); err != nil {
			t.Fatal(err)
		}
		st, err := store.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !hasEntryNamed(st.Invalid, bad) {
			t.Fatalf("запись %s не попала в Invalid: %+v", bad, st.Invalid)
		}
		if err := svc.Write(ctx, WriteOp{Kind: WriteSetURL, File: bad, URL: urlB}, "", false, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if !rtOpsEntry(t, store, bad, urlB) {
			t.Fatalf("запись %s не стала корректной с адресом %s:\n%s", bad, urlB, read())
		}
		if st, _ := store.Status(); hasEntryNamed(st.Invalid, bad) {
			t.Errorf("запись %s осталась в Invalid", bad)
		}
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpRemove, File: bad}, "", false); err != nil {
			t.Errorf("уборка записи %s: %v", bad, err)
		}
	})

	t.Run("gh_proxy", func(t *testing.T) {
		if err := svc.Write(ctx, WriteOp{Kind: WriteGhProxy, Value: "https://example.org"}, "", false, false); err != nil {
			t.Fatalf("Write(proxy): %v", err)
		}
		if st, _ := store.Status(); st.GhProxy != "https://example.org" {
			t.Fatalf("gh_proxy = %q", st.GhProxy)
		}
		if err := svc.Write(ctx, WriteOp{Kind: WriteGhProxy, Value: ""}, "", false, false); err != nil {
			t.Fatalf("Write(пусто): %v", err)
		}
		if st, _ := store.Status(); st.GhProxy != "" {
			t.Fatalf("gh_proxy = %q после очистки", st.GhProxy)
		}
	})

	t.Run("dedup", func(t *testing.T) {
		needJQ(t)
		const std = "geosite_v2fly.dat"
		stdPath := filepath.Join(datDir, std)
		if err := os.WriteFile(stdPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpAdd, File: std, URL: urlB}, "", false); err != nil {
			t.Fatalf("подготовка записи %s: %v", std, err)
		}
		if err := svc.Write(ctx, WriteOp{Kind: WriteDedup, File: std}, "", false, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if rtOpsEntry(t, store, std, "") {
			t.Errorf("запись %s не убрана", std)
		}
		if fi, err := os.Stat(stdPath); err != nil || fi.Size() != int64(len(data)) {
			t.Errorf("файл %s не остался на месте: %v", std, err)
		}
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteDedup, File: std}, "", false, false), "not_pending")
	})

	t.Run("dedup_alias", func(t *testing.T) {
		// Запись под вторым именем пары (zkeen.dat), а строка набора шлёт стандартное имя.
		needJQ(t)
		set := StandardSets[0]
		for _, s := range StandardSets {
			if s.Alias != "" {
				set = s
				break
			}
		}
		if set.Alias == "" {
			t.Fatal("в StandardSets нет набора со вторым именем")
		}
		stdPath := filepath.Join(datDir, set.File)
		if err := os.WriteFile(stdPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(stdPath) })
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpAdd, File: set.Alias, URL: urlB}, "", false); err != nil {
			t.Fatalf("подготовка записи %s: %v", set.Alias, err)
		}
		if err := svc.Write(ctx, WriteOp{Kind: WriteDedup, File: set.File}, "", false, false); err != nil {
			t.Fatalf("Write(dedup %s): %v", set.File, err)
		}
		if rtOpsEntry(t, store, set.Alias, "") {
			t.Errorf("запись %s не убрана", set.Alias)
		}
		if fi, err := os.Stat(stdPath); err != nil || fi.Size() != int64(len(data)) {
			t.Errorf("файл %s не остался на месте: %v", set.File, err)
		}
	})

	t.Run("enable_catalog", func(t *testing.T) {
		needJQ(t)
		var set lists.ExtendedSet
		for _, e := range rtProbeCatalog() {
			if e.ID == "roscomvpn_geosite" {
				set = e
			}
		}
		if set.ID == "" {
			t.Fatal("в каталоге нет набора roscomvpn_geosite")
		}
		// Каталог читается по настоящему индексу стенда, запись идёт в xkeen.json теста.
		if err := svc.Write(ctx, WriteOp{Kind: WriteEnableSet, Set: set.ID}, "", false, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if !rtOpsEntry(t, store, set.File, set.URL) {
			t.Fatalf("записи %s нет в xkeen.json", set.File)
		}
		if svc.hasPending() {
			t.Errorf("постоянная запись набора каталога попала в ожидания")
		}
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteEnableSet, Set: set.ID}, "", false, false), "already_present")
	})

	t.Run("finish_include", func(t *testing.T) {
		needJQ(t)
		const std = "geoip_refilter.dat"
		url := svc.vars.URL(StandardSets[1])
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpAdd, File: std, URL: url}, "", false); err != nil {
			t.Fatalf("подготовка записи %s: %v", std, err)
		}
		svc.mu.Lock()
		svc.st.PendingEnables = append(svc.st.PendingEnables, PendingEnable{File: std, URL: url, Since: time.Now()})
		svc.st.IncludeStuck[std] = lists.ErrCommentsLost.Code
		svc.mu.Unlock()
		op := WriteOp{Kind: WriteFinishInclude, File: std}
		pv, err := svc.Preview(op)
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if pv.After == pv.Before {
			t.Errorf("предпросмотр довода без правки")
		}
		if err := svc.Write(ctx, op, pv.Base, true, false); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if rtOpsEntry(t, store, std, "") {
			t.Errorf("запись %s не убрана", std)
		}
		svc.mu.Lock()
		_, stuck := svc.st.IncludeStuck[std]
		svc.mu.Unlock()
		if svc.hasPending() || stuck {
			t.Errorf("ожидание или include_stuck не сняты")
		}
	})

	t.Run("busy", func(t *testing.T) {
		// Записи нет без XKeen и во время -ug.
		other, err := New(Deps{DataDir: rtGeoTempDir(t, tg.WorkDir), Layer: layer, Store: store, Index: lists.NewIndex(datDir), XKeenBin: filepath.Join(dir, "no-xkeen")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(other.Stop)
		rtOpsWantCode(t, other.Write(ctx, WriteOp{Kind: WriteEnableUpdate}, "", false, false), "xkeen_missing")

		release, err := layer.BeginExclusive(ctx, true, configlayer.HolderGeodataUpdate)
		if err != nil {
			t.Fatalf("BeginExclusive: %v", err)
		}
		defer release()
		before := read()
		rtOpsWantCode(t, svc.Write(ctx, WriteOp{Kind: WriteGhProxy, Value: "https://example.org"}, "", false, false), "geodata_busy")
		if read() != before {
			t.Errorf("xkeen.json изменился при занятом замке")
		}
	})

	t.Run("jq", func(t *testing.T) {
		if _, err := os.Stat(opkgBin); err != nil {
			t.Skipf("нет %s", opkgBin)
		}
		if err := svc.InstallJQ(); err != nil {
			t.Fatalf("InstallJQ: %v", err)
		}
		rtOpsWantCode(t, svc.InstallJQ(), "geodata_busy")
		deadline := time.Now().Add(jqTimeout + time.Minute)
		for {
			svc.mu.Lock()
			running := svc.running
			svc.mu.Unlock()
			if !running {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("установка jq не завершилась за %v", jqTimeout+time.Minute)
			}
			time.Sleep(time.Second)
		}
		if chunk := svc.Output("jq", 0); chunk.Text == "" {
			t.Errorf("вывод opkg пуст")
		}
		svc.mu.Lock()
		jqErr := svc.st.JQError
		svc.mu.Unlock()
		if jqErr != "" {
			t.Errorf("установка jq: итог %q", jqErr)
		}
		if st, _ := store.Status(); !st.JQ {
			t.Errorf("jq не найден после установки")
		}
	})
}
