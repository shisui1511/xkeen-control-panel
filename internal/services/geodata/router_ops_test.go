//go:build router

package geodata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
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
