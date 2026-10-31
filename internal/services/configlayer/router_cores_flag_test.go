//go:build router

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// Выключение флага слоя и дрейф секций Xray на устройстве (SEC-01, SEC-02;
// D-04 из 144, D-05, D-07 из 146): настоящий слой, хранилище и `xray run -test`
// роутера, окружение и генератор — из router_cores_takeover_test.go. Работающие
// ядра стенда не трогаются (Applier не задан), ядро тест не читает и не
// переключает — метка ядра не нужна.

// TestRouterCoresDisableReturnsOriginals — выключение флага возвращает
// исходные файлы всех взятых секций побайтно, файлы панели уходят в набор,
// State.Cores пуст.
func TestRouterCoresDisableReturnsOriginals(t *testing.T) {
	e := rtNewCoresEnv(t, map[string]string{
		rtCoLogFile: `{"log": {"loglevel": "warning"}}` + "\n",
		rtCoDNSFile: rtCoDNSBody,
	})
	e.editSections(t, map[string]json.RawMessage{
		SectionXrayLog: e.takeDraft(t, xraySectionLog, false),
		SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true),
	})
	res := e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}
	e.assertAbsent(t, rtCoLogFile)
	e.assertAbsent(t, rtCoDNSFile)
	if got := len(e.layer.store.Snapshot().Cores.Xray); got != 2 {
		t.Fatalf("взято секций %d, ожидалось 2", got)
	}

	if err := e.layer.Disable(context.Background(), nil); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	e.assertFileIs(t, rtCoLogFile)
	e.assertFileIs(t, rtCoDNSFile)
	e.assertAbsent(t, PanelXrayLogFile)
	e.assertAbsent(t, PanelXrayDNSFile)
	st := e.layer.store.Snapshot()
	if !st.Cores.isEmpty() {
		t.Errorf("State.Cores не пуст после выключения: %+v", st.Cores)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят после выключения: %+v", st.Journal)
	}

	t.Run("уже вернул сам", func(t *testing.T) {
		e := rtNewCoresEnv(t, map[string]string{
			rtCoLogFile: `{"log": {"loglevel": "warning"}}` + "\n",
			rtCoDNSFile: rtCoDNSBody,
		})
		e.editSections(t, map[string]json.RawMessage{
			SectionXrayLog: e.takeDraft(t, xraySectionLog, false),
			SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true),
		})
		res := e.apply(t)
		if !res.OK || res.Code != ResultApplied {
			t.Fatalf("взятие: %+v", res)
		}

		// Пользователь вернул 02_dns.json сам, содержимое иное.
		const own = `{"dns": {"servers": ["10.0.0.1"]}}` + "\n"
		if err := os.WriteFile(e.path(rtCoDNSFile), []byte(own), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := e.layer.Disable(context.Background(), nil); err != nil {
			t.Fatalf("Disable: %v", err)
		}
		e.assertFileIs(t, rtCoLogFile)
		if got := hashOf(e.path(rtCoDNSFile)); got != HashContent([]byte(own)) {
			t.Errorf("файл пользователя %s перезаписан выключением", rtCoDNSFile)
		}
		e.assertAbsent(t, PanelXrayLogFile)
		e.assertAbsent(t, PanelXrayDNSFile)
		if st := e.layer.store.Snapshot(); !st.Cores.isEmpty() {
			t.Errorf("State.Cores не пуст после выключения: %+v", st.Cores)
		}
	})
}

const (
	// Смешанный чужой файл: dns вместе со stats, имя не совпадает с перенесённым
	// 02_dns.json, чтобы возврат секции не упирался в release_conflict.
	rtCoMixedDNSFile = "08_dnsstats.json"
	rtCoMixedDNSBody = `{"dns": {"servers": ["127.0.0.2"]}, "stats": {}}` + "\n"
	// Чужой «чистый» файл dns, вернувшийся после взятия (как восстановил XKeen).
	rtCoReturnedDNSBody = `{"dns": {"servers": ["8.8.8.8"]}}` + "\n"
)

// assertPanelFile проверяет, что файл панели лежит в каталоге Xray.
func (e *rtCoresEnv) assertPanelFile(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Stat(e.path(name)); err != nil {
		t.Errorf("файл панели %s пропал: %v", name, err)
	}
}

// TestRouterCoresSectionDrift — чужой файл с ключом взятой секции даёт дрейф
// section_foreign: он виден в снимке, блокирует «Применить» и решается «Вернуть
// файл панели» (чистый файл) или «Отпустить» (смешанный остаётся на месте).
func TestRouterCoresSectionDrift(t *testing.T) {
	e := rtNewCoresEnv(t, map[string]string{rtCoDNSFile: rtCoDNSBody})
	e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true)})
	res := e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}
	if n := e.layer.Snapshot().DriftCount; n != 0 {
		t.Fatalf("дрейф сразу после взятия: %d", n)
	}

	// XKeen вернул 02_dns.json: дрейф виден в снимке и блокирует «Применить».
	if err := os.WriteFile(e.path(rtCoDNSFile), []byte(rtCoReturnedDNSBody), 0o644); err != nil {
		t.Fatal(err)
	}
	fe := e.layer.evaluate(e.layer.store.Snapshot())
	if len(fe.Sections) != 1 || fe.Sections[0].Code != DriftSectionForeign ||
		fe.Sections[0].Section != xraySectionDNS || fe.Sections[0].File != rtCoDNSFile {
		t.Fatalf("дрейф секций: %+v", fe.Sections)
	}
	if n := e.layer.Snapshot().DriftCount; n < 1 {
		t.Errorf("DriftCount не учёл дрейф секции: %d", n)
	}
	if err := e.layer.StartApply(true); !errors.Is(err, ErrDriftBlocked) {
		t.Fatalf("«Применить» при дрейфе секции: %v, ожидалась ErrDriftBlocked", err)
	}
	if got := hashOf(e.path(rtCoDNSFile)); got != HashContent([]byte(rtCoReturnedDNSBody)) {
		t.Errorf("чужой файл изменён блокированным применением")
	}
	e.assertPanelFile(t, PanelXrayDNSFile)

	// «Вернуть файл панели»: чистый чужой файл уходит в копии, дрейфа нет.
	if err := e.layer.ReclaimSection(context.Background(), xraySectionDNS); err != nil {
		t.Fatalf("ReclaimSection: %v", err)
	}
	e.assertAbsent(t, rtCoDNSFile)
	e.assertPanelFile(t, PanelXrayDNSFile)
	sec := e.layer.store.Snapshot().Cores.Xray[xraySectionDNS]
	if sec == nil || len(sec.Moved) != 1 || sec.Moved[0].Name != rtCoDNSFile {
		t.Fatalf("State.Cores.Xray[dns] после возврата файла панели: %+v", sec)
	}
	if want := HashContent([]byte(rtCoReturnedDNSBody)); sec.Moved[0].Hash != want || hashOf(movedCopyPath(sec.Moved[0])) != want {
		t.Errorf("копия вернувшегося файла в наборе не совпадает: %+v", sec.Moved[0])
	}
	if n := e.layer.Snapshot().DriftCount; n != 0 {
		t.Errorf("дрейф после возврата файла панели: %d", n)
	}
	if err := e.layer.ReclaimSection(context.Background(), xraySectionDNS); !errors.Is(err, ErrNothingToReclaim) {
		t.Errorf("ReclaimSection без дрейфа: %v, ожидалась ErrNothingToReclaim", err)
	}

	// Смешанный файл (dns и stats): отказ foreign_mixed, файл на месте.
	if err := os.WriteFile(e.path(rtCoMixedDNSFile), []byte(rtCoMixedDNSBody), 0o644); err != nil {
		t.Fatal(err)
	}
	var mixed *ForeignMixedError
	if err := e.layer.ReclaimSection(context.Background(), xraySectionDNS); !errors.As(err, &mixed) || mixed.File != rtCoMixedDNSFile {
		t.Fatalf("ReclaimSection со смешанным файлом: %v, ожидалась ForeignMixedError(%s)", err, rtCoMixedDNSFile)
	}
	if got := hashOf(e.path(rtCoMixedDNSFile)); got != HashContent([]byte(rtCoMixedDNSBody)) {
		t.Errorf("смешанный файл изменён отказом")
	}
	if st := e.layer.store.Snapshot(); st.Journal != nil || len(st.Cores.Xray[xraySectionDNS].Moved) != 1 {
		t.Errorf("отказ оставил следы: журнал %+v, перенесённые %+v", st.Journal, st.Cores.Xray[xraySectionDNS].Moved)
	}

	// «Отпустить»: managed=false не блокируется дрейфом, исходный файл возвращается,
	// смешанный файл не тронут (имена разные — release_conflict нет).
	e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: rtCoreSection(t, CoreSection{Managed: false})})
	res = e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("«Отпустить» при дрейфе секции: %+v", res)
	}
	if got := hashOf(e.path(rtCoDNSFile)); got != HashContent([]byte(rtCoReturnedDNSBody)) {
		t.Errorf("возвращённый файл %s не совпадает с перенесённым", rtCoDNSFile)
	}
	if got := hashOf(e.path(rtCoMixedDNSFile)); got != HashContent([]byte(rtCoMixedDNSBody)) {
		t.Errorf("смешанный файл тронут при «Отпустить»")
	}
	e.assertAbsent(t, PanelXrayDNSFile)
	if st := e.layer.store.Snapshot(); !st.Cores.isEmpty() || st.Journal != nil {
		t.Errorf("после «Отпустить»: cores %+v, журнал %+v", st.Cores, st.Journal)
	}
}
