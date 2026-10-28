//go:build router

package geodata

import (
	"context"
	"encoding/json"
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
