//go:build router

package geodata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// Одно задание менеджера за раз, шаг удаления под учётом Stop и запись xkeen.json под
// исключительным замком (REVIEW WR-02, WR-04, WR-08) на устройстве: настоящий слой,
// настоящие xkeen и opkg, файлы теста во временном каталоге. Ядро тест не читает и не
// переключает — метки ядра нет.

// rtJobsJobNow — текущее задание сервиса (под s.mu).
func rtJobsJobNow(s *Service) (running bool, job string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.job
}

// TestRouterGeodataJobSlot — пока идёт установка jq, «Обновить сейчас» и «Довести
// удаление» отказывают geodata_busy и не держат замок слоя; фоновый -ug ждёт конца
// установки, не ставит waiting_apply и не идёт вместе с opkg.
func TestRouterGeodataJobSlot(t *testing.T) {
	tg := routertest.Current(t)
	for _, bin := range []string{rtRunXKeenBin, opkgBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("нет %s", bin)
		}
	}
	rtGeoSkipWindow(t, 25*time.Minute)

	dir := rtGeoTempDir(t, tg.WorkDir)
	layer := rtGeoLayer(t, dir)
	store := rtGeoStore(t, "/opt/etc/xkeen")
	index := lists.NewIndex(lists.DefaultDatDir)
	svc := rtGeoService(t, layer, store, index, dir)
	svc.Start()
	ctx := context.Background()

	kernels := map[string]bool{}
	for _, name := range []string{"xray", "mihomo"} {
		if rtRunPid(name) != "" {
			kernels[name] = true
		}
	}

	if err := svc.InstallJQ(); err != nil {
		t.Fatalf("InstallJQ: %v", err)
	}
	if !svc.Output(jobJQ, 0).Running {
		t.Fatal("установка jq закончилась раньше проверок: задание не удержано")
	}
	if err := svc.RunNow(true); lists.ErrorCode(err) != "geodata_busy" {
		t.Errorf("RunNow во время установки jq: %v, ожидался geodata_busy", err)
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("замок слоя держит %q после отказа RunNow во время установки jq", h)
	}
	if err := svc.FinishDelete(ctx, "xcp-none.dat", "", false); lists.ErrorCode(err) != "geodata_busy" {
		t.Errorf("FinishDelete во время установки jq: %v, ожидался geodata_busy", err)
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("замок слоя держит %q после отказа FinishDelete во время установки jq", h)
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	runErr := make(chan error, 1)
	go func() { runErr <- svc.run(runCtx, false, "rt-jobs") }()

	lastJQ := time.Now()
	for svc.Output(jobJQ, 0).Running {
		lastJQ = time.Now()
		if _, job := rtJobsJobNow(svc); job == jobUG {
			t.Error("-ug пошёл во время установки jq")
		}
		if v, err := svc.Snapshot(); err != nil {
			t.Fatalf("Snapshot: %v", err)
		} else if v.Run.WaitingApply {
			t.Error("waiting_apply во время ожидания jq")
		}
		if h := layer.BusyHolder(); h != "" && svc.Output(jobJQ, 0).Running {
			t.Errorf("замок слоя держит %q, пока фоновый -ug ждёт конца установки jq", h)
		}
		time.Sleep(200 * time.Millisecond)
	}

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("run после установки jq: %v", err)
		}
	case <-time.After(20 * time.Minute):
		t.Fatal("фоновый -ug не завершился за 20 минут")
	}
	svc.mu.Lock()
	lr := svc.st.LastRun
	svc.mu.Unlock()
	switch {
	case lr == nil:
		t.Error("итога запуска нет")
	case lr.Trigger != "rt-jobs":
		t.Errorf("триггер запуска %q, ожидался rt-jobs", lr.Trigger)
	case lr.At.Before(lastJQ):
		t.Errorf("-ug начат %v, раньше конца установки jq (%v)", lr.At, lastJQ)
	}
	for name := range kernels {
		if !rtRunWaitKernel(name, 60*time.Second) {
			t.Errorf("ядро %s не вернулось за 60 с после -ug", name)
		}
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("замок слоя не снят: %q", h)
	}
}

// TestRouterGeodataAfterApplyStop — Stop дожидается шага удаления после «Применить»
// (он ждёт замок слоя) и отменяет его: после Stop файл dat, пометка удаления и
// состояние менеджера не меняются.
func TestRouterGeodataAfterApplyStop(t *testing.T) {
	tg := routertest.Current(t)
	src, _, _ := rtRemovePick(t)
	dir := rtGeoTempDir(t, tg.WorkDir)
	datDir := filepath.Join(dir, "dat")
	xkeenDir := filepath.Join(dir, "xkeen")
	for _, d := range []string{datDir, xkeenDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const file = "xcp-stop.dat"
	rtGeoCopy(t, src, filepath.Join(datDir, file))
	rtRemoveWrite(t, filepath.Join(xkeenDir, "xkeen.json"), rtRemoveXKeenJSON())

	layer := rtGeoLayer(t, dir)
	svc, err := New(Deps{
		DataDir: filepath.Join(dir, "panel"), Layer: layer, Store: rtGeoStore(t, xkeenDir),
		Index: lists.NewIndex(datDir), XrayConfDir: filepath.Join(dir, "xray"), MihomoDir: filepath.Join(dir, "mihomo"),
	})
	if err != nil {
		t.Fatalf("geodata.New: %v", err)
	}
	t.Cleanup(svc.Stop)

	// Пометка удаления в применённом состоянии без «Применить».
	if err := layer.SettleSection(routing.GeodataSection, func(_, draft json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		applied, err := json.Marshal(routing.GeodataDraft{Drop: []routing.GeodataDrop{{File: file, Mode: routing.DropDelete}}})
		return applied, draft, err
	}); err != nil {
		t.Fatalf("SettleSection: %v", err)
	}

	ctx := context.Background()
	hold, err := layer.BeginExclusive(ctx, false, configlayer.HolderGeodataUpdate)
	if err != nil {
		t.Fatalf("BeginExclusive: %v", err)
	}
	defer hold()

	done := make(chan struct{})
	go func() {
		svc.AfterApply(context.Background(), configlayer.AfterApplyInfo{})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("AfterApply не ждал замок слоя")
	case <-time.After(500 * time.Millisecond):
	}

	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop не вернулся")
	}
	select {
	case <-done:
	default:
		t.Error("AfterApply ещё идёт после возврата Stop")
	}

	hold()
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(filepath.Join(datDir, file)); err != nil {
		t.Errorf("файл удалён после Stop: %v", err)
	}
	st, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if _, ok := st.Applied[routing.GeodataSection]; !ok {
		t.Error("пометка удаления снята после Stop")
	}
	svc.mu.Lock()
	pending := len(svc.st.PendingDeletes)
	svc.mu.Unlock()
	if pending != 0 {
		t.Errorf("PendingDeletes после Stop: %d, ожидалось 0", pending)
	}
}

// rtJobsConfig — xkeen.json теста записи: обновление включено, записей нет.
const rtJobsConfig = `{
  // xcp router test: комментарий владельца
  "xkeen": {
    "geofile_update": true,
    "gh_proxy": "",
    "xray": {
      "geodata": []
    }
  }
}
`

// rtJobsGeodataHolder — есть ли среди событий geodata за срок wait событие с держателем
// holder. Остальные события отбрасываются.
func rtJobsGeodataHolder(events <-chan configlayer.Event, holder string, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return false
			}
			if ev.Type != configlayer.EventGeodata {
				continue
			}
			if data, ok := ev.Data.(configlayer.GeodataEvent); ok && data.Holder == holder {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

// TestRouterGeodataLockedWrite — запись xkeen.json по кнопке и уборка временной записи
// тиком идут под исключительным замком слоя: чужой замок даёт отказ без записи; тик
// берёт замок только ради готового к уборке файла без отметки include_stuck.
func TestRouterGeodataLockedWrite(t *testing.T) {
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
	rtRemoveWrite(t, filepath.Join(xkDir, "xkeen.json"), rtJobsConfig)
	store := rtGeoStore(t, xkDir)
	if st, err := store.Status(); err != nil {
		t.Fatalf("Status: %v", err)
	} else if !st.JQ {
		t.Skip("стенд: нет jq")
	}
	layer := rtGeoLayer(t, dir)
	svc, err := New(Deps{
		DataDir: dir, Layer: layer, Store: store, Index: lists.NewIndex(datDir),
		CronFile: filepath.Join(dir, "crontab"), XrayConfDir: filepath.Join(dir, "xray"), MihomoDir: filepath.Join(dir, "mihomo"),
	})
	if err != nil {
		t.Fatalf("geodata.New: %v", err)
	}
	t.Cleanup(svc.Stop)
	if err := svc.staticCheck(); err != nil {
		t.Skipf("XKeen старее 2.0.1: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Сервер: проверка источника (HEAD) стоит на воротах, пока тест не возьмёт замок.
	data, datKind := rtProbeDat(t)
	started := make(chan struct{}, 1)
	gate := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow.dat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "slow.dat", time.Time{}, bytes.NewReader(data))
	})
	addr := rtProbeServer(t, mux)

	// 1. Запись под чужим замком: отказ, xkeen.json не меняется.
	const own = "xcp-lock.dat"
	op := WriteOp{Kind: WriteAdd, File: own, URL: "http://" + addr + "/slow.dat"}
	pv, err := svc.Preview(op)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	writeErr := make(chan error, 1)
	go func() { writeErr <- svc.Write(ctx, op, pv.Base, true, false) }()
	select {
	case <-started:
	case err := <-writeErr:
		t.Fatalf("Write не дошёл до сетевой проверки: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("сетевая проверка источника не началась")
	}
	hold, err := layer.BeginExclusive(ctx, false, configlayer.HolderGeodataRemove)
	if err != nil {
		t.Fatalf("BeginExclusive: %v", err)
	}
	defer hold()
	close(gate)
	select {
	case err := <-writeErr:
		if !errors.Is(err, configlayer.ErrGeodataBusy) {
			t.Errorf("запись xkeen.json под чужим замком: %v, ожидалась ErrGeodataBusy", err)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("Write не вернулся")
	}
	if rtOpsEntry(t, store, own, "") {
		t.Error("запись xkeen.json под чужим замком изменила файл")
	}
	hold()
	// Замок свободен: повторная запись тех же значений (проверка из кэша) проходит.
	if err := svc.Write(ctx, op, pv.Base, true, false); err != nil {
		t.Fatalf("повторный Write: %v", err)
	}
	if !rtOpsEntry(t, store, own, "") {
		t.Fatalf("записи %s нет в xkeen.json после повторной записи", own)
	}

	// 2. Уборка тиком. Имена стандартных наборов: готовый файл лежит под первым,
	// ожидание без файла — под вторым.
	ready, absent := "geosite_v2fly.dat", "geoip_v2fly.dat"
	if datKind == lists.SniffGeoIP {
		ready, absent = absent, ready
	}
	writeDat := func(path string) {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("запись %s: %v", path, err)
		}
	}
	events, unsubscribe, err := layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()
	ticks := func() {
		for range 3 {
			svc.tick()
		}
	}

	// (а) Ожидание без файла: тик замок не берёт.
	absentURL := "https://example.invalid/" + absent
	svc.mu.Lock()
	svc.st.PendingEnables = append(svc.st.PendingEnables, PendingEnable{File: absent, URL: absentURL, Since: time.Now()})
	svc.mu.Unlock()
	ticks()
	if rtJobsGeodataHolder(events, configlayer.HolderGeodataWrite, time.Second) {
		t.Error("тик берёт замок без готового файла")
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("тик держит замок %q без готового файла", h)
	}

	// (б) Готовый файл с отметкой include_stuck: тик замок тоже не берёт.
	readyURL := "https://example.invalid/" + ready
	writeDat(filepath.Join(datDir, ready))
	if err := store.Apply(lists.GeodataOp{Kind: lists.OpAdd, File: ready, URL: readyURL}, "", true); err != nil {
		t.Fatalf("Apply(add %s): %v", ready, err)
	}
	svc.mu.Lock()
	svc.st.PendingEnables = append(svc.st.PendingEnables, PendingEnable{File: ready, URL: readyURL, Since: time.Now()})
	svc.st.IncludeStuck[ready] = lists.ErrCommentsLost.Code
	svc.mu.Unlock()
	ticks()
	if rtJobsGeodataHolder(events, configlayer.HolderGeodataWrite, time.Second) {
		t.Error("тик берёт замок без готового файла: застрявшее ожидание")
	}
	if !rtOpsEntry(t, store, ready, "") {
		t.Error("тик убрал запись застрявшего ожидания")
	}
	svc.mu.Lock()
	delete(svc.st.IncludeStuck, ready)
	svc.mu.Unlock()

	// (в) Готовый файл, чужой замок: запись не трогается; замок свободен: запись убрана.
	hold2, err := layer.BeginExclusive(ctx, false, configlayer.HolderGeodataRemove)
	if err != nil {
		t.Fatalf("BeginExclusive: %v", err)
	}
	defer hold2()
	svc.tick()
	if !rtOpsEntry(t, store, ready, "") {
		t.Error("уборка временной записи под чужим замком")
	}
	hold2()
	svc.tick()
	if rtOpsEntry(t, store, ready, "") {
		t.Error("запись готового набора не убрана тиком при свободном замке")
	}
	svc.mu.Lock()
	cleaned := !svc.st.Cleaned[ready].IsZero()
	_, absentKept := func() (PendingEnable, bool) {
		for _, p := range svc.st.PendingEnables {
			if p.File == absent {
				return p, true
			}
		}
		return PendingEnable{}, false
	}()
	svc.mu.Unlock()
	if !cleaned {
		t.Error("в Cleaned нет отметки об уборке")
	}
	if !absentKept {
		t.Error("ожидание без файла пропало после уборки")
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("замок не снят после тика: %q", h)
	}
}
