//go:build router

package geodata

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
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
	return lists.NewGeodataStore(svc, exec.LookPath)
}

// rtXKeenNumber — числовая версия в выводе `xkeen -v`: формат первой строки зависит от
// выпуска XKeen («Версия XKeen 2.0 Beta» или «XKeen 2.1 Stable»), число одно.
var rtXKeenNumber = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// rtXKeenVersions отдаёт слою настоящую версию установленного XKeen стенда: без неё порог
// пользовательских geodata считался бы недоступным и тест пропускался бы как «слишком старый».
func rtXKeenVersions() []configlayer.KernelVersionInput {
	out, err := exec.Command(defaultXKeenBin, "-v").CombinedOutput()
	if err != nil {
		return []configlayer.KernelVersionInput{{Name: "xkeen"}}
	}
	first, _, _ := strings.Cut(utils.StripANSI(string(out)), "\n")
	return []configlayer.KernelVersionInput{{Name: "xkeen", Installed: true, Version: rtXKeenNumber.FindString(first)}}
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
		DataDir:        dir,
		Roots:          roots,
		Enabled:        func() bool { return true },
		DriftInterval:  time.Hour,
		DebounceDelay:  20 * time.Millisecond,
		KernelVersions: rtXKeenVersions,
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

	// Правки расписания во время запуска: выключенное расписание -ug не запускает, а итог
	// запуска и правки идут в файл состояния одной упорядоченной записью (CR-01).
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(h int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if err := svc.SetSchedule(Schedule{Enabled: false, Day: -1, Hour: h, Minute: 10 + j}); err != nil {
					t.Errorf("SetSchedule во время -ug: %v", err)
				}
			}
		}(i + 5)
	}
	wg.Wait()
	if err := svc.SetSchedule(Schedule{Enabled: false, Day: -1, Hour: 3, Minute: 42}); err != nil {
		t.Fatalf("SetSchedule: %v", err)
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
	// Файл состояния после гонки правок и запуска совпадает с памятью (CR-01).
	disk := loadState(filepath.Join(dir, stateFileName))
	svc.mu.Lock()
	memSched, memLast := svc.st.Schedule, svc.st.LastRun
	memRes := make(map[string]FileResult, len(svc.st.Results))
	for name, r := range svc.st.Results {
		memRes[name] = r
	}
	svc.mu.Unlock()
	if disk.Schedule.Hour != 3 || disk.Schedule.Minute != 42 || memSched.Hour != 3 || memSched.Minute != 42 {
		t.Errorf("файл состояния отстаёт от памяти: расписание на диске %+v, в памяти %+v, ожидалось 3:42", disk.Schedule, memSched)
	}
	if disk.LastRun == nil || memLast == nil || !disk.LastRun.At.Equal(memLast.At) {
		t.Errorf("файл состояния отстаёт от памяти: итог запуска на диске %+v, в памяти %+v", disk.LastRun, memLast)
	}
	for name, r := range memRes {
		if got, ok := disk.Results[name]; !ok || got.Status != r.Status {
			t.Errorf("файл состояния отстаёт от памяти: итог %s на диске %+v, в памяти %+v", name, got, r)
		}
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

// rtGeoCopy копирует файл потоком.
func rtGeoCopy(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatalf("открытие %s: %v", from, err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatalf("создание %s: %v", to, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("копирование в %s: %v", to, err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// rtGeoRow ищет строку источника по имени файла.
func rtGeoRow(v View, file string) (SourceView, bool) {
	for _, r := range v.Sources {
		if r.File == file {
			return r, true
		}
	}
	return SourceView{}, false
}

// TestRouterGeodataView — снимок вкладки на настоящих dat-файлах стенда: шесть
// стандартных строк, пометки дубля, группа «файл» и отметка «обновлено вне панели».
// xkeen.json и каталог dat стенда не меняются: запись идёт в копию во временном
// каталоге, файлы — копии настоящих dat.
func TestRouterGeodataView(t *testing.T) {
	tg := routertest.Current(t)

	infos, err := lists.NewIndex(lists.DefaultDatDir).Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	var first, second *lists.FileInfo
	for i := range infos {
		if infos[i].Name == "geosite_refilter.dat" {
			first = &infos[i]
			break
		}
		if first == nil && infos[i].Kind == lists.KindSite {
			first = &infos[i]
		}
	}
	if first == nil {
		t.Skip("на стенде нет geosite-файла dat")
	}
	for i := range infos {
		if infos[i].Name != first.Name {
			second = &infos[i]
			break
		}
	}
	if second == nil {
		t.Skip("на стенде один dat-файл: для строки «файл» нужен второй")
	}

	dir := rtGeoTempDir(t, tg.WorkDir)
	datDir := filepath.Join(dir, "dat")
	xkDir := filepath.Join(dir, "xkeen")
	for _, d := range []string{datDir, xkDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Первый файл ложится под стандартным именем, второй — под своим.
	const stdName, otherName = "geosite_refilter.dat", "xcp-view-file.dat"
	rtGeoCopy(t, filepath.Join(lists.DefaultDatDir, first.Name), filepath.Join(datDir, stdName))
	rtGeoCopy(t, filepath.Join(lists.DefaultDatDir, second.Name), filepath.Join(datDir, otherName))

	if _, err := os.Stat(rtRunConfig); err != nil {
		t.Skipf("нет %s: образца xkeen.json нет", rtRunConfig)
	}
	rtGeoCopy(t, rtRunConfig, filepath.Join(xkDir, "xkeen.json"))
	store := rtGeoStore(t, xkDir)
	st, err := store.Status()
	if err != nil {
		t.Fatalf("Status копии xkeen.json: %v", err)
	}
	if !st.JQ {
		t.Skip("стенд: нет jq")
	}
	// Запись с именем стандартного набора и чужим адресом: сперва убрать прежнюю, если она есть.
	if err := store.Apply(lists.GeodataOp{Kind: lists.OpRemove, File: stdName}, "", true); err != nil && lists.ErrorCode(err) != lists.ErrEntryNotFound.Code {
		t.Fatalf("Apply(remove) на копии: %v", err)
	}
	if !st.GeofileUpdate {
		if err := store.Apply(lists.GeodataOp{Kind: lists.OpEnableUpdate}, "", true); err != nil {
			t.Fatalf("Apply(enable_update) на копии: %v", err)
		}
	}
	if err := store.Apply(lists.GeodataOp{Kind: lists.OpAdd, File: stdName, URL: "http://127.0.0.1:9/other.dat"}, "", true); err != nil {
		t.Fatalf("Apply(add) на копии: %v", err)
	}

	layer := rtGeoLayer(t, dir)
	svc := rtGeoService(t, layer, store, lists.NewIndex(datDir), dir)
	svc.Start()

	v, err := svc.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Шесть стандартных строк в порядке StandardSets.
	var std []SourceView
	for _, r := range v.Sources {
		if r.Origin == OriginStandard {
			std = append(std, r)
		}
	}
	if len(std) != len(StandardSets) {
		t.Fatalf("стандартных строк %d, ожидалось %d", len(std), len(StandardSets))
	}
	for i, set := range StandardSets {
		if std[i].File != set.File || std[i].SetID != set.ID {
			t.Errorf("стандартная строка %d: %s/%s, ожидалось %s/%s", i, std[i].File, std[i].SetID, set.File, set.ID)
		}
	}
	row, ok := rtGeoRow(v, stdName)
	if !ok || row.Origin != OriginStandard {
		t.Fatalf("нет стандартной строки %s: %+v", stdName, row)
	}
	if !row.Present || !row.Entry || !row.Dup || !row.DupURL {
		t.Errorf("строка %s: present=%v entry=%v dup=%v dup_url=%v, ожидались все true", stdName, row.Present, row.Entry, row.Dup, row.DupURL)
	}
	other, ok := rtGeoRow(v, otherName)
	if !ok || other.Origin != OriginFile || !other.Present || other.Entry {
		t.Fatalf("строка %s: %+v, ожидалась группа file без записи", otherName, other)
	}
	if v.Files != 2 {
		t.Errorf("files = %d, ожидалось 2", v.Files)
	}
	want := int64(0)
	if got, err := lists.NewIndex(datDir).Files(); err == nil {
		for _, f := range got {
			want += f.Size
		}
	}
	if v.TotalSize != want || want == 0 {
		t.Errorf("total_size = %d, ожидалось %d", v.TotalSize, want)
	}
	if mihomo, err := lists.MihomoFiles(lists.DefaultMihomoDir); err != nil || len(mihomo) != len(v.Mihomo) {
		t.Errorf("файлы Mihomo: снимок %d, каталог %d, ошибка %v", len(v.Mihomo), len(mihomo), err)
	}
	if v.Draft == nil || v.Sources == nil || v.Mihomo == nil {
		t.Errorf("пустые списки снимка должны быть [] (draft=%v sources=%v mihomo=%v)", v.Draft == nil, v.Sources == nil, v.Mihomo == nil)
	}

	// Правка вне панели: время файла изменилось, `-ug` панель не запускала.
	moved := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(filepath.Join(datDir, otherName), moved, moved); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		v, err = svc.Snapshot()
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if r, _ := rtGeoRow(v, otherName); r.Outside != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("отметка «обновлено вне панели» не появилась за 40 с")
		}
		time.Sleep(time.Second)
	}
	if r, _ := rtGeoRow(v, stdName); r.Outside != "" {
		t.Errorf("у нетронутого файла %s отметка outside: %s", stdName, r.Outside)
	}
}
