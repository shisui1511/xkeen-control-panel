//go:build router

package configlayer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
