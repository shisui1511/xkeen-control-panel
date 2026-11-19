//go:build router

// router-area: configlayer

package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Проверка признака мастера первого запуска на устройстве (149.1, CFG-04):
// настоящее хранилище слоя во временном каталоге рабочего каталога теста, шина
// событий и перечитывание файла состояния новым слоем. Ядра не запускаются.

// rtNewWizardLayer — слой на каталоге dir; останавливается по окончании теста.
func rtNewWizardLayer(t *testing.T, dir string, dev bool) *Layer {
	t.Helper()
	layer, err := New(Options{
		DataDir: dir,
		Roots:   Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")},
		DevMode: func() bool { return dev },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	return layer
}

// rtWizardSubscribe подписывается на шину слоя; отписка — по окончании теста.
func rtWizardSubscribe(t *testing.T, layer *Layer) <-chan Event {
	t.Helper()
	ch, cancel, err := layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(cancel)
	return ch
}

// rtWizardFile читает файл состояния каталога dir как объект верхнего уровня.
func rtWizardFile(t *testing.T, dir string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, StateFileName))
	if err != nil {
		t.Fatalf("файл состояния: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("файл состояния не разбирается: %v", err)
	}
	return raw
}

// rtWizardRewrite правит файл состояния каталога dir снаружи слоя.
func rtWizardRewrite(t *testing.T, dir string, edit func(raw map[string]json.RawMessage)) {
	t.Helper()
	raw := rtWizardFile(t, dir)
	edit(raw)
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("сериализация файла состояния: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, StateFileName), data, 0o600); err != nil {
		t.Fatalf("запись файла состояния: %v", err)
	}
}

// rtWizardMark ставит статус и сверяет признак изменения.
func rtWizardMark(t *testing.T, layer *Layer, status WizardStatus, want bool) {
	t.Helper()
	changed, err := layer.MarkWizard(status)
	if err != nil {
		t.Fatalf("MarkWizard(%s): %v", status, err)
	}
	if changed != want {
		t.Fatalf("MarkWizard(%s): изменено %v, ожидалось %v", status, changed, want)
	}
}

func TestRouterWizardState(t *testing.T) {
	t.Run("monotonic", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		events := rtWizardSubscribe(t, layer)

		if s := layer.WizardStatus(); s != "" {
			t.Fatalf("статус нового слоя %q, ожидался пустой", s)
		}
		rtWizardMark(t, layer, WizardClosed, true)
		ev, ok := rtNextEvent(events, EventWizard, 2*time.Second)
		if !ok {
			t.Fatal("нет события wizard после closed")
		}
		first, ok := ev.Data.(WizardEvent)
		if !ok {
			t.Fatalf("данные события wizard: %T", ev.Data)
		}

		// Повтор того же статуса ничего не пишет и не будит клиентов.
		rtWizardMark(t, layer, WizardClosed, false)
		if _, ok := rtNextEvent(events, EventWizard, 300*time.Millisecond); ok {
			t.Error("событие wizard без смены статуса")
		}

		rtWizardMark(t, layer, WizardDone, true)
		ev, ok = rtNextEvent(events, EventWizard, 2*time.Second)
		if !ok {
			t.Fatal("нет события wizard после done")
		}
		if next := ev.Data.(WizardEvent); next.Seq <= first.Seq {
			t.Errorf("счётчик событий не растёт: %d -> %d", first.Seq, next.Seq)
		}

		// «Пройдено» не понижается и не меняется на «скрыто» (одинаковый ранг).
		rtWizardMark(t, layer, WizardClosed, false)
		rtWizardMark(t, layer, WizardHidden, false)
		if s := layer.WizardStatus(); s != WizardDone {
			t.Errorf("статус после closed и hidden поверх done: %q", s)
		}
		if _, err := layer.MarkWizard(WizardStatus("bogus")); !errors.Is(err, ErrWizardStatus) {
			t.Errorf("неизвестный статус: %v, ожидалась ErrWizardStatus", err)
		}

		// Признак пишется в файл в фоне, как правки черновика: дождаться записи.
		layer.store.Flush()
		var onDisk WizardState
		raw, ok := rtWizardFile(t, dir)["wizard"]
		if !ok {
			t.Fatal("в файле состояния нет признака мастера")
		}
		if err := json.Unmarshal(raw, &onDisk); err != nil || onDisk.Status != WizardDone || onDisk.At.IsZero() {
			t.Errorf("признак в файле: статус %q, время задано %v, ошибка %v", onDisk.Status, !onDisk.At.IsZero(), err)
		}
	})

	t.Run("persisted", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		rtWizardMark(t, layer, WizardDone, true)
		layer.Stop()

		again := rtNewWizardLayer(t, dir, false)
		if s := again.WizardStatus(); s != WizardDone {
			t.Errorf("статус после нового слоя %q, ожидался done", s)
		}
		rtWizardMark(t, again, WizardClosed, false)
	})

	t.Run("file_without_field", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		if _, err := layer.EditDraft(0, "other", rtRaw(`{"a":1}`)); err != nil {
			t.Fatalf("EditDraft: %v", err)
		}
		layer.Stop()
		if _, ok := rtWizardFile(t, dir)["wizard"]; ok {
			t.Fatal("слой без мастера записал признак в файл")
		}
		before, err := os.ReadFile(filepath.Join(dir, StateFileName))
		if err != nil {
			t.Fatalf("файл состояния: %v", err)
		}

		again := rtNewWizardLayer(t, dir, false)
		if s := again.WizardStatus(); s != "" {
			t.Errorf("статус файла без признака %q, ожидался пустой", s)
		}
		_ = again.WizardFacts(WizardProbe{})
		after, err := os.ReadFile(filepath.Join(dir, StateFileName))
		if err != nil {
			t.Fatalf("файл состояния: %v", err)
		}
		if !bytes.Equal(before, after) {
			t.Error("чтение признака переписало файл того, кто мастер не трогал")
		}

		prev := again.store.Snapshot()
		rtWizardMark(t, again, WizardClosed, true)
		cur := again.store.Snapshot()
		if cur.DraftRevision != prev.DraftRevision || !SectionEqual(cur.Draft["other"], prev.Draft["other"]) || len(cur.Draft) != len(prev.Draft) {
			t.Errorf("признак мастера изменил черновик: ревизия %d -> %d, секций %d -> %d",
				prev.DraftRevision, cur.DraftRevision, len(prev.Draft), len(cur.Draft))
		}
		again.store.Flush() // признак пишется в файл в фоне
		file := rtWizardFile(t, dir)
		if string(file["schema_version"]) != "1" {
			t.Errorf("schema_version в файле %s, ожидалась 1", file["schema_version"])
		}
		if _, ok := file["wizard"]; !ok {
			t.Error("признак closed не записан в файл")
		}
	})

	t.Run("garbage_status", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		rtWizardMark(t, layer, WizardDone, true)
		layer.Stop()
		rtWizardRewrite(t, dir, func(raw map[string]json.RawMessage) {
			raw["wizard"] = rtRaw(`{"status":"bogus","at":"2026-01-01T00:00:00Z"}`)
		})

		again := rtNewWizardLayer(t, dir, false)
		if s := again.WizardStatus(); s != "" {
			t.Errorf("мусорный статус прочитан как %q, ожидался пустой", s)
		}
		if again.store.Snapshot().Notices.SchemaReset {
			t.Error("мусорный статус сбросил схему состояния")
		}
		rtWizardMark(t, again, WizardClosed, true)
	})

	t.Run("schema_reset", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		rtWizardMark(t, layer, WizardHidden, true)
		layer.Stop()
		rtWizardRewrite(t, dir, func(raw map[string]json.RawMessage) {
			raw["schema_version"] = rtRaw(`999`)
		})

		again := rtNewWizardLayer(t, dir, false)
		if s := again.WizardStatus(); s != "" {
			t.Errorf("статус после сброса схемы %q, ожидался пустой (мастер снова виден)", s)
		}
		if !again.store.Snapshot().Notices.SchemaReset {
			t.Error("чужая схема не сбросила состояние")
		}
		if _, ok := rtWizardFile(t, dir)["wizard"]; ok {
			t.Error("после сброса схемы в файле остался признак мастера")
		}
	})

	t.Run("reset_dev_only", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		rtWizardMark(t, layer, WizardDone, true)
		if err := layer.ResetWizard(); !errors.Is(err, ErrDevModeRequired) {
			t.Errorf("сброс без режима разработчика: %v, ожидалась ErrDevModeRequired", err)
		}
		if s := layer.WizardStatus(); s != WizardDone {
			t.Errorf("статус после отказа в сбросе %q, ожидался done", s)
		}
		layer.Stop()

		dev := rtNewWizardLayer(t, dir, true)
		events := rtWizardSubscribe(t, dev)
		if err := dev.ResetWizard(); err != nil {
			t.Fatalf("сброс в режиме разработчика: %v", err)
		}
		if s := dev.WizardStatus(); s != "" {
			t.Errorf("статус после сброса %q, ожидался пустой", s)
		}
		if _, ok := rtNextEvent(events, EventWizard, 2*time.Second); !ok {
			t.Error("нет события wizard после сброса")
		}
		dev.store.Flush() // снятие признака пишется в файл в фоне
		if _, ok := rtWizardFile(t, dir)["wizard"]; ok {
			t.Error("после сброса в файле остался признак мастера")
		}
		if err := dev.ResetWizard(); err != nil {
			t.Errorf("повторный сброс: %v", err)
		}
		if _, ok := rtNextEvent(events, EventWizard, 300*time.Millisecond); ok {
			t.Error("событие wizard при сбросе без признака")
		}

	})

	t.Run("facts_empty", func(t *testing.T) {
		dir := rtLiveDir(t)
		layer := rtNewWizardLayer(t, dir, false)
		f := layer.WizardFacts(WizardProbe{})
		if !f.Empty || !f.AppliedEmpty || f.DraftChanges != 0 || f.RoutingAny || f.XrayTaken != 0 || f.ApplyRunning {
			t.Errorf("факты пустого слоя: %+v", f)
		}

		if _, err := layer.EditDraft(layer.store.DraftRevision(), RoutingSection, rtRaw(`{"v":1,"managed":true}`)); err != nil {
			t.Fatalf("EditDraft routing: %v", err)
		}
		f = layer.WizardFacts(WizardProbe{})
		if !f.RoutingAny || !f.RoutingMihomo {
			t.Errorf("маршрутизация взята в черновике, а факты: any=%v mihomo=%v", f.RoutingAny, f.RoutingMihomo)
		}
		if f.Empty {
			t.Error("черновик со взятой маршрутизацией считается пустым")
		}
		if !f.AppliedEmpty {
			t.Error("применённое без изменений считается непустым")
		}
		if f.DraftChanges == 0 {
			t.Error("правка черновика не дала изменений")
		}
	})
}
