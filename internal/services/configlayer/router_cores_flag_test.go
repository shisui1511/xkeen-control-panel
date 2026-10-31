//go:build router

package configlayer

import (
	"context"
	"encoding/json"
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
