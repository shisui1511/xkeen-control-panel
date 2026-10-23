//go:build router

package configlayer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Возврат маршрутизации при выключении флага, дрейф маршрутизации и автоматическое
// взятие у позже установленного ядра на устройстве: окружение и раскладка каталогов
// те же, что у сценариев взятия (router_takeover_test.go). Работающие ядра стенда не
// трогаются, тест ядро не читает и не переключает — метка ядра не нужна.

// flagOffSets возвращает наборы копий с триггером flag_off каталога данных окружения.
func (e *rtTakeoverEnv) flagOffSets(t *testing.T) []*BackupSet {
	t.Helper()
	metas, err := filepath.Glob(filepath.Join(filepath.Dir(e.roots.Xray), backupDirName, backupLayerName, backupSetPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}
	var out []*BackupSet
	for _, dir := range metas {
		set, err := LoadBackupSet(dir)
		if err != nil {
			continue
		}
		if set.Meta.Trigger == string(TriggerFlagOff) {
			out = append(out, set)
		}
	}
	return out
}

func (e *rtTakeoverEnv) disable(t *testing.T) error {
	t.Helper()
	return e.layer.Disable(e.layer.Context(), func() error {
		e.enabled.Store(false)
		return nil
	})
}

// enableAndWaitTaken включает флаг и ждёт, пока фоновая сборка снова возьмёт
// маршрутизацию обоих ядер.
func (e *rtTakeoverEnv) enableAndWaitTaken(t *testing.T) {
	t.Helper()
	e.enabled.Store(true)
	e.layer.Enable()
	rtWaitFor(t, "повторное взятие после включения флага", func() bool {
		st := e.layer.store.Snapshot()
		return st.Routing.Xray != nil && st.Routing.Mihomo != nil && st.Journal == nil
	})
}

func TestRouterDisableReleasesRouting(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	e.setManaged(t, true)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Fatalf("после взятия файл routing остался в configs/: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// Занятое применение: Disable отказывает, диск не тронут.
	release, err := e.layer.pipeline.TryBegin(e.layer.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	flagCalled := false
	err = e.layer.Disable(e.layer.Context(), func() error { flagCalled = true; return nil })
	release()
	if !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("Disable при занятом применении: %v, ожидался ErrApplyBusy", err)
	}
	if flagCalled {
		t.Error("флаг снят при занятом применении")
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл панели тронут при занятом применении: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("чужой файл вернулся при занятом применении: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// Выключение: чужой routing вернулся побайтно, config.yaml на прежнем профиле,
	// файлов панели нет, они в наборе flag_off.
	if err := e.disable(t); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("%s вернулся не побайтно: %q", rtTkRoutingFile, got)
	}
	e.assertUntouched(t)
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)

	st := e.layer.store.Snapshot()
	if !st.Routing.isEmpty() {
		t.Errorf("State.Routing не очищено: %+v", st.Routing)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят: %+v", st.Journal)
	}
	for key, entry := range st.Manifest {
		if entry.Status == StatusManaged {
			t.Errorf("в манифесте остался managed-файл %s", key)
		}
	}
	sets := e.flagOffSets(t)
	if len(sets) != 1 {
		t.Fatalf("наборов flag_off: %d, ожидался 1", len(sets))
	}
	inSet := map[string]bool{}
	for _, m := range sets[0].Meta.Files {
		inSet[m.Key] = true
	}
	for _, key := range []string{ManifestKey(KernelXray, rtTkPanelRoute), ManifestKey(KernelXray, rtTkPanelTail), ManifestKey(KernelMihomo, PanelMihomoProfile)} {
		if !inSet[key] {
			t.Errorf("в наборе flag_off нет файла панели %s", key)
		}
	}

	// Включение: применённое состояние с routing.managed собирает всё заново,
	// включая взятие (TriggerFlagOn).
	e.enableAndWaitTaken(t)
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("после повторного взятия %s остался в configs/: %v", rtTkRoutingFile, err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл routing панели не собран после включения: %v", err)
	}

	// release_conflict: пользователь вернул файл с тем же именем сам — выключение
	// его не перезаписывает, остальное возвращается.
	const own = `{"routing": {"rules": []}}` + "\n"
	if err := os.WriteFile(e.xray(rtTkRoutingFile), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.disable(t); err != nil {
		t.Fatalf("Disable при занятом имени: %v", err)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != HashContent([]byte(own)) {
		t.Errorf("файл пользователя перезаписан при возврате: %q", got)
	}
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)
	e.assertUntouched(t)
	if st := e.layer.store.Snapshot(); !st.Routing.isEmpty() || st.Journal != nil {
		t.Errorf("состояние после второго выключения: routing %+v, журнал %+v", st.Routing, st.Journal)
	}
}

const (
	rtRelForeignBody = `{"routing": {"rules": [{"type": "field", "network": "tcp,udp", "outboundTag": "direct"}]}}` + "\n"
	rtRelMixedBody   = `{"routing": {"rules": []}, "outbounds": [{"tag": "zz-out", "protocol": "freedom"}]}` + "\n"
	rtRelMixedFile   = "zz_user.json"
)

// waitRoutingEvent ждёт события routing из подписки.
func waitRoutingEvent(t *testing.T, events <-chan Event) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("подписка закрыта до события routing")
			}
			if ev.Type == EventRouting {
				if _, ok := ev.Data.(RoutingEvent); !ok {
					t.Fatalf("данные события routing: %T", ev.Data)
				}
				return
			}
		case <-deadline:
			t.Fatal("события routing нет")
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRouterRoutingDrift(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	// Предпросмотр до взятия: 05_routing.json уйдёт, смешанный 00_api.json останется.
	pv, err := e.layer.RoutingPreview()
	if err != nil {
		t.Fatalf("RoutingPreview: %v", err)
	}
	if pv.Xray == nil || !containsString(pv.Xray.Move, rtTkRoutingFile) || !containsString(pv.Xray.Left, rtTkAPIFile) {
		t.Fatalf("предпросмотр Xray: %+v", pv.Xray)
	}
	if pv.Mihomo == nil || pv.Mihomo.PrevProfile != "profiles/user.yaml" {
		t.Fatalf("предпросмотр Mihomo: %+v", pv.Mihomo)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Fatalf("предпросмотр изменил %s", rtTkRoutingFile)
	}

	e.setManaged(t, true)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}
	snap := e.layer.Snapshot()
	if !snap.Routing.Desired || snap.Routing.Xray == nil || snap.Routing.Mihomo == nil {
		t.Fatalf("раздел routing снимка: %+v", snap.Routing)
	}
	x, m := snap.Routing.Xray, snap.Routing.Mihomo
	if !x.Taken || !equalStrings(x.Moved, []string{rtTkRoutingFile}) || !containsString(x.Left, rtTkAPIFile) || len(x.Drift) != 0 {
		t.Errorf("Xray в снимке: %+v", x)
	}
	if !m.Taken || m.PrevProfile != "profiles/user.yaml" || len(m.Drift) != 0 || snap.DriftCount != 0 {
		t.Errorf("Mihomo в снимке: %+v, drift_count %d", m, snap.DriftCount)
	}

	// Дрейф профиля: config.yaml переложили снаружи на прежний профиль.
	cfg := e.mihomo("config.yaml")
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.link, cfg); err != nil {
		t.Fatal(err)
	}
	snap = e.layer.Snapshot()
	if !equalStrings(snap.Routing.Mihomo.Drift, []string{"config.yaml"}) || snap.DriftCount == 0 {
		t.Fatalf("дрейф профиля не виден: %+v, drift_count %d", snap.Routing.Mihomo, snap.DriftCount)
	}
	if err := e.layer.StartApply(true); !errors.Is(err, ErrDriftBlocked) {
		t.Fatalf("StartApply при дрейфе профиля: %v, ожидался ErrDriftBlocked", err)
	}
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.ReclaimRouting(e.layer.Context(), KernelMihomo); err != nil {
		t.Fatalf("ReclaimRouting mihomo: %v", err)
	}
	waitRoutingEvent(t, events)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	snap = e.layer.Snapshot()
	if len(snap.Routing.Mihomo.Drift) != 0 || snap.DriftCount != 0 || snap.Routing.Mihomo.PrevProfile != "profiles/user.yaml" {
		t.Errorf("после возврата профиля: %+v, drift_count %d", snap.Routing.Mihomo, snap.DriftCount)
	}
	if err := e.layer.ReclaimRouting(e.layer.Context(), KernelMihomo); !errors.Is(err, ErrNothingToReclaim) {
		t.Errorf("повторное «вернуть профиль»: %v, ожидался ErrNothingToReclaim", err)
	}

	// Чужой routing вернулся (05_routing.json от XKeen): дрейф, «вернуть» уносит его в копию.
	if err := os.WriteFile(e.xray(rtTkRoutingFile), []byte(rtRelForeignBody), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = e.layer.Snapshot()
	if !equalStrings(snap.Routing.Xray.Drift, []string{rtTkRoutingFile}) || snap.DriftCount == 0 {
		t.Fatalf("дрейф routing не виден: %+v, drift_count %d", snap.Routing.Xray, snap.DriftCount)
	}
	if err := e.layer.ReclaimRouting(e.layer.Context(), KernelXray); err != nil {
		t.Fatalf("ReclaimRouting xray: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("чужой файл остался в configs/: %v", err)
	}
	st := e.layer.store.Snapshot()
	var got *MovedFile
	for i := range st.Routing.Xray.Moved {
		if st.Routing.Xray.Moved[i].Name == rtTkRoutingFile {
			got = &st.Routing.Xray.Moved[i]
		}
	}
	if got == nil || got.Hash != HashContent([]byte(rtRelForeignBody)) || hashOf(movedCopyPath(*got)) != got.Hash {
		t.Fatalf("перенесённый файл не в копии: %+v", st.Routing.Xray.Moved)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят: %+v", st.Journal)
	}
	if snap = e.layer.Snapshot(); len(snap.Routing.Xray.Drift) != 0 || snap.DriftCount != 0 {
		t.Errorf("дрейф не пропал: %+v", snap.Routing.Xray)
	}
	e.assertUntouched(t)

	// Смешанный чужой файл с routing после файла панели: дрейф, «вернуть» отказывает.
	if err := os.WriteFile(e.xray(rtRelMixedFile), []byte(rtRelMixedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = e.layer.Snapshot()
	if !equalStrings(snap.Routing.Xray.Drift, []string{rtRelMixedFile}) {
		t.Fatalf("дрейф смешанного файла не виден: %+v", snap.Routing.Xray)
	}
	err = e.layer.ReclaimRouting(e.layer.Context(), KernelXray)
	var fm *ForeignMixedError
	if !errors.Is(err, ErrForeignMixed) || !errors.As(err, &fm) || fm.File != rtRelMixedFile {
		t.Fatalf("ReclaimRouting смешанного файла: %v", err)
	}
	if hashOf(e.xray(rtRelMixedFile)) != HashContent([]byte(rtRelMixedBody)) {
		t.Error("смешанный файл изменён")
	}
	if err := e.layer.StartApply(true); !errors.Is(err, ErrDriftBlocked) {
		t.Fatalf("StartApply при чужом routing: %v, ожидался ErrDriftBlocked", err)
	}

	// «Отпустить»: желаемое managed=false проходит несмотря на дрейф и возвращает файлы.
	e.setManaged(t, false)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("возврат при дрейфе: %+v", res)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != HashContent([]byte(rtRelForeignBody)) {
		t.Errorf("вернулась не последняя копия %s: %q", rtTkRoutingFile, got)
	}
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)
	if hashOf(e.xray(rtRelMixedFile)) != HashContent([]byte(rtRelMixedBody)) {
		t.Error("смешанный файл тронут при возврате")
	}
	if st := e.layer.store.Snapshot(); !st.Routing.isEmpty() || st.Journal != nil {
		t.Errorf("состояние после возврата: routing %+v, журнал %+v", st.Routing, st.Journal)
	}
}
