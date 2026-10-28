//go:build router

package geodata

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Сценарии сервиса менеджера geodata на устройстве: настоящий слой «Конфигурация»
// во временном каталоге теста, настоящий XKeen и настоящие dat-файлы стенда. Ядро
// тест не читает и не переключает — метки ядра нет.

// rtGeoSkipWindow пропускает тест, если отрезок от сейчас на span по часам роутера
// задевает окно 05:20–05:45 (снимок стенда и cron XKeen).
func rtGeoSkipWindow(t *testing.T, span time.Duration) {
	t.Helper()
	now := time.Now()
	end := now.Add(span)
	for off := 0; off <= 1; off++ {
		from := time.Date(now.Year(), now.Month(), now.Day()+off, 5, 20, 0, 0, now.Location())
		to := from.Add(25 * time.Minute)
		if now.Before(to) && end.After(from) {
			t.Skipf("часы стенда: отрезок теста задевает окно 05:20-05:45 (cron XKeen и снимок)")
		}
	}
}

// rtGeoTempDir — временный каталог в рабочем каталоге теста.
func rtGeoTempDir(t *testing.T, workDir string) string {
	t.Helper()
	dir, err := os.MkdirTemp(workDir, "rt-geodata-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// rtGeoStore создаёт хранилище записей xkeen.json на каталоге xkeenDir: настоящий
// XKeenSettingsService и настоящий поиск jq. Единственный вызов NewGeodataStore в
// роутерных тестах пакета: остальные сценарии строят хранилище только через него.
func rtGeoStore(t *testing.T, xkeenDir string) *lists.GeodataStore {
	t.Helper()
	tg := routertest.Current(t)
	backups := rtGeoTempDir(t, tg.WorkDir)
	svc := services.NewXKeenSettingsService(xkeenDir, backups, []string{xkeenDir})
	return lists.NewGeodataStore(svc, exec.LookPath, nil)
}

// rtGeoLayer создаёт включённый слой во временном каталоге dir и запускает его.
func rtGeoLayer(t *testing.T, dir string) *configlayer.Layer {
	t.Helper()
	roots := configlayer.Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, roots.Mihomo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	layer, err := configlayer.New(configlayer.Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       func() bool { return true },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)
	layer.Start()
	return layer
}

// rtGeoService создаёт сервис менеджера на каталоге данных dir.
func rtGeoService(t *testing.T, layer *configlayer.Layer, store *lists.GeodataStore, index *lists.Index, dir string) *Service {
	t.Helper()
	svc, err := New(Deps{DataDir: dir, Layer: layer, Store: store, Index: index})
	if err != nil {
		t.Fatalf("geodata.New: %v", err)
	}
	t.Cleanup(svc.Stop)
	return svc
}

// rtGeoWaitRun ждёт конца запуска и возвращает его итог.
func rtGeoWaitRun(t *testing.T, svc *Service, limit time.Duration) LastRun {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		svc.mu.Lock()
		running, lr := svc.running, svc.st.LastRun
		svc.mu.Unlock()
		if !running && lr != nil {
			return *lr
		}
		if time.Now().After(deadline) {
			t.Fatalf("запуск не завершился за %v", limit)
		}
		time.Sleep(time.Second)
	}
}

// TestRouterGeodataRunNow — «Обновить сейчас»: исключительный замок слоя берётся
// синхронно, настоящий `xkeen -ug` идёт в фоне, итоги по файлам и вывод переживают
// перезапуск панели.
func TestRouterGeodataRunNow(t *testing.T) {
	tg := routertest.Current(t)
	if _, err := os.Stat(rtRunXKeenBin); err != nil {
		t.Skipf("нет %s: XKeen не установлен", rtRunXKeenBin)
	}
	rtGeoSkipWindow(t, 25*time.Minute)

	dir := rtGeoTempDir(t, tg.WorkDir)
	layer := rtGeoLayer(t, dir)
	store := rtGeoStore(t, "/opt/etc/xkeen")
	index := lists.NewIndex(lists.DefaultDatDir)
	svc := rtGeoService(t, layer, store, index, dir)
	svc.Start()

	var present []StandardSet
	for _, set := range StandardSets {
		if rtRunExists(filepath.Join(rtRunDatDir, set.File)) || (set.Alias != "" && rtRunExists(filepath.Join(rtRunDatDir, set.Alias))) {
			present = append(present, set)
		}
	}
	kernels := map[string]bool{}
	for _, name := range []string{"xray", "mihomo"} {
		if rtRunPid(name) != "" {
			kernels[name] = true
		}
	}

	if err := svc.RunNow(true); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	// Замок слоя занят менеджером: «Применить» и второе обновление по кнопке отказывают.
	if err := layer.StartApply(true); !errors.Is(err, configlayer.ErrGeodataBusy) {
		t.Errorf("StartApply во время -ug: %v, ожидалась ErrGeodataBusy", err)
	}
	if err := svc.RunNow(true); !errors.Is(err, configlayer.ErrGeodataBusy) {
		t.Errorf("второй RunNow: %v, ожидалась ErrGeodataBusy", err)
	}

	lr := rtGeoWaitRun(t, svc, 20*time.Minute)
	for name := range kernels {
		if !rtRunWaitKernel(name, 60*time.Second) {
			t.Errorf("ядро %s не вернулось за 60 с после -ug", name)
		}
	}
	if layer.BusyHolder() != "" {
		t.Errorf("замок слоя не снят после запуска: %q", layer.BusyHolder())
	}

	chunk := svc.Output("ug", 0)
	if chunk.Text == "" {
		t.Fatalf("вывод ug пуст")
	}
	if !lr.Parsed {
		if lr.Reason == RunErrNetwork {
			t.Skip("сеть стенда: XKeen не начал загрузку")
		}
		t.Fatalf("вывод -ug не разобран: %+v", lr)
	}
	for _, set := range present {
		svc.mu.Lock()
		r, ok := svc.st.Results[set.File]
		svc.mu.Unlock()
		if !ok {
			t.Errorf("нет итога по %s", set.File)
			continue
		}
		switch r.Status {
		case ResultUpdated, ResultUnchanged, StatusKept, StatusFailed:
		default:
			t.Errorf("итог %s: статус %q", set.File, r.Status)
		}
	}

	// Состояние и журнал переживают перезапуск панели.
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); err != nil {
		t.Fatalf("нет файла состояния: %v", err)
	}
	svc2 := rtGeoService(t, layer, store, index, dir)
	svc.mu.Lock()
	want := svc.st.Results
	svc.mu.Unlock()
	for name, r := range want {
		got, ok := svc2.st.Results[name]
		if !ok || got.Status != r.Status || got.Reason != r.Reason {
			t.Errorf("итог %s после перезапуска: %+v, ожидался %+v", name, got, r)
		}
	}
	saved, err := os.ReadFile(filepath.Join(dir, "geodata", "last-ug.log"))
	if err != nil {
		t.Fatalf("last-ug.log: %v", err)
	}
	again := svc2.Output("ug", 0)
	if again.Text == "" || again.Text != string(saved) {
		t.Errorf("вывод после перезапуска не совпал с last-ug.log (%d и %d байт)", len(again.Text), len(saved))
	}
}
