//go:build router

package configlayer

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Исключительный замок менеджера geodata на устройстве: настоящий слой, настоящее
// хранилище и настоящий `xray run -test` роутера. Тест ядро не читает и не
// переключает — метка ядра не нужна.

// rtGeodataEventHolder ждёт событие geodata и возвращает держателя из него.
func rtGeodataEventHolder(t *testing.T, ch <-chan Event, what string) string {
	t.Helper()
	ev, ok := rtNextEvent(ch, EventGeodata, 5*time.Second)
	if !ok {
		t.Fatalf("%s: события geodata нет", what)
	}
	data, ok := ev.Data.(GeodataEvent)
	if !ok {
		t.Fatalf("%s: данные события geodata — %T", what, ev.Data)
	}
	return data.Holder
}

func TestRouterExclusiveGeodata(t *testing.T) {
	layer, _ := rtNewCheckLayer(t, func(*Layer) {})
	ctx := context.Background()
	events, unsubscribe, err := layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	release, err := layer.BeginExclusive(ctx, false, HolderGeodataUpdate)
	if err != nil {
		t.Fatalf("BeginExclusive: %v", err)
	}
	if h := rtGeodataEventHolder(t, events, "взятие замка"); h != HolderGeodataUpdate {
		t.Errorf("держатель в событии при взятии: %q", h)
	}

	// «Применить» по кнопке получает отказ с понятной причиной, а не apply_busy.
	if err := layer.StartApply(true); !errors.Is(err, ErrGeodataBusy) {
		t.Errorf("StartApply при замке менеджера: %v, ожидалась ErrGeodataBusy", err)
	}
	if h := layer.Snapshot().BusyHolder; h != HolderGeodataUpdate {
		t.Errorf("Snapshot().BusyHolder = %q", h)
	}
	if h := layer.BusyHolder(); h != HolderGeodataUpdate {
		t.Errorf("BusyHolder() = %q", h)
	}
	// Второй -ug по кнопке и выключение слоя — тоже geodata_busy.
	if _, err := layer.BeginExclusive(ctx, true, HolderGeodataUpdate); !errors.Is(err, ErrGeodataBusy) {
		t.Errorf("второй BeginExclusive по кнопке: %v", err)
	}
	committed := false
	if err := layer.Disable(ctx, func() error { committed = true; return nil }); !errors.Is(err, ErrGeodataBusy) {
		t.Errorf("Disable при замке менеджера: %v", err)
	}
	if committed {
		t.Error("Disable вызвал commit при занятом замке")
	}

	release()
	release() // повторный вызов безопасен
	if h := rtGeodataEventHolder(t, events, "снятие замка"); h != "" {
		t.Errorf("держатель в событии после снятия: %q", h)
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("BusyHolder() после release = %q", h)
	}
	if h := layer.Snapshot().BusyHolder; h != "" {
		t.Errorf("Snapshot().BusyHolder после release = %q", h)
	}

	// Замок свободен: «Применить» проходит.
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("StartApply после release: %v", err)
	}
	rtWaitUserApply(t, layer)

	// Фоновый вызов ждёт конца идущего применения.
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("второй StartApply: %v", err)
	}
	sawRunning := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if layer.pipeline.Current().Running {
			sawRunning = true
			break
		}
	}
	var acquired atomic.Bool
	var runningAtAcquire atomic.Bool
	var holderAtAcquire atomic.Value
	errCh := make(chan error, 1)
	go func() {
		rel, err := layer.BeginExclusive(ctx, false, HolderGeodataRemove)
		if err != nil {
			errCh <- err
			return
		}
		runningAtAcquire.Store(layer.pipeline.Current().Running)
		holderAtAcquire.Store(layer.BusyHolder())
		acquired.Store(true)
		rel()
		errCh <- nil
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("фоновый BeginExclusive: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("фоновый BeginExclusive не получил замок после применения")
	}
	if !acquired.Load() {
		t.Fatal("замок не взят")
	}
	if sawRunning && runningAtAcquire.Load() {
		t.Error("фоновый BeginExclusive взял замок, пока применение ещё шло")
	}
	if h, _ := holderAtAcquire.Load().(string); h != HolderGeodataRemove {
		t.Errorf("держатель в замке фонового вызова: %q", h)
	}
	if !sawRunning {
		t.Log("применение закончилось раньше, чем тест увидел Running: порядок ожидания не проверен")
	}
	rtWaitUserApply(t, layer)
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("BusyHolder() в конце теста = %q", h)
	}
}

// Флаг слоя выключили, пока фоновый вызов ждал замок: он получает ErrDisabled, замок
// сразу свободен и держателя нет.
func TestRouterExclusiveFlagRecheck(t *testing.T) {
	var on atomic.Bool
	on.Store(true)
	layer, _ := rtNewCheckLayer(t, func(l *Layer) {
		l.opts.Enabled = func() bool { return on.Load() }
	})
	ctx := context.Background()

	hold, err := layer.BeginExclusive(ctx, false, HolderGeodataUpdate)
	if err != nil {
		t.Fatalf("BeginExclusive: %v", err)
	}
	released := false
	defer func() {
		if !released {
			hold()
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		rel, err := layer.BeginExclusive(ctx, false, HolderGeodataRemove)
		if err == nil {
			rel()
		}
		errCh <- err
	}()
	select {
	case err := <-errCh:
		t.Fatalf("фоновый вызов не ждал замок: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	on.Store(false)
	hold()
	released = true
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrDisabled) {
			t.Errorf("BeginExclusive после выключения флага: %v, ожидалась ErrDisabled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("BeginExclusive после выключения флага не вернулся")
	}
	if h := layer.BusyHolder(); h != "" {
		t.Errorf("BusyHolder() после отказа = %q", h)
	}

	on.Store(true)
	rel, err := layer.BeginExclusive(ctx, true, HolderGeodataWrite)
	if err != nil {
		t.Fatalf("BeginExclusive по кнопке после отказа: %v (замок не отпущен)", err)
	}
	if h := layer.BusyHolder(); h != HolderGeodataWrite {
		t.Errorf("BusyHolder() = %q, ожидался %q", h, HolderGeodataWrite)
	}
	rel()
}

func TestRouterSettleSection(t *testing.T) {
	layer, _ := rtNewCheckLayer(t, func(*Layer) {})
	ctx := context.Background()

	// Секция с пометкой удаления попадает в применённое состояние.
	ev, err := layer.EditDraft(0, "geodata", rtRaw(`{"v":1,"drop":[{"file":"a.dat","mode":"delete"}]}`))
	if err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	rtWaitUserApply(t, layer)
	before, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if _, ok := before.Applied["geodata"]; !ok {
		t.Fatalf("секции geodata нет в Applied после применения: %v", before.Applied)
	}
	if before.DraftRevision < ev.DraftRevision {
		t.Fatalf("ревизия %d меньше ревизии правки %d", before.DraftRevision, ev.DraftRevision)
	}

	// Шаг «убрать запись и файл» закончен: пометка снимается под исключительным замком.
	events, unsubscribe, err := layer.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()
	calls := 0
	settle := func(applied, draft json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		calls++
		return nil, nil, nil
	}
	err = layer.RunExclusive(ctx, false, HolderGeodataRemove, func(context.Context) error {
		return layer.SettleSection("geodata", settle)
	})
	if err != nil {
		t.Fatalf("SettleSection под замком: %v", err)
	}
	after, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState после снятия: %v", err)
	}
	if _, ok := after.Applied["geodata"]; ok {
		t.Error("секция geodata осталась в Applied")
	}
	if _, ok := after.Draft["geodata"]; ok {
		t.Error("секция geodata осталась в черновике")
	}
	if after.DraftRevision != before.DraftRevision+1 {
		t.Errorf("ревизия %d, ожидалась %d", after.DraftRevision, before.DraftRevision+1)
	}
	// Хвост применения мог опубликовать draft со старой ревизией: ищем событие новой.
	gotDraft := false
	for deadline := time.Now().Add(5 * time.Second); !gotDraft && time.Now().Before(deadline); {
		dev, ok := rtNextEvent(events, EventDraft, time.Until(deadline))
		if !ok {
			break
		}
		if de, ok := dev.Data.(DraftEvent); ok && de.DraftRevision == after.DraftRevision {
			gotDraft = true
		}
	}
	if !gotDraft {
		t.Error("события draft с новой ревизией после снятия пометки нет")
	}
	// Клиент читает состояние сразу после снятия — правка по новой ревизии без конфликта.
	if _, err := layer.EditDraft(after.DraftRevision, "geodata_probe", rtRaw(`{"v":1}`)); err != nil {
		t.Errorf("EditDraft по ревизии после снятия: %v", err)
	}
	// Клиент со старой ревизией получает конфликт.
	if _, err := layer.EditDraft(before.DraftRevision, "geodata_probe", rtRaw(`{"v":2}`)); !errors.Is(err, ErrDraftConflict) {
		t.Errorf("EditDraft по старой ревизии: %v", err)
	}
	if _, err := layer.EditDraft(after.DraftRevision+1, "geodata_probe", nil); err != nil {
		t.Errorf("удаление пробной секции: %v", err)
	}

	// Повторное снятие ничего не меняет: ревизия прежняя.
	mid, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if err := layer.SettleSection("geodata", settle); err != nil {
		t.Fatalf("повторный SettleSection: %v", err)
	}
	again, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if again.DraftRevision != mid.DraftRevision {
		t.Errorf("повторное снятие сдвинуло ревизию: %d -> %d", mid.DraftRevision, again.DraftRevision)
	}
	if calls != 2 {
		t.Errorf("функция снятия вызвана %d раз, ожидалось 2", calls)
	}
	if err := layer.SettleSection("Не-секция", settle); !errors.Is(err, ErrInvalidSection) {
		t.Errorf("недопустимое имя секции: %v", err)
	}

	// Изменился только Applied: ревизия прежняя, но событие draft с новым числом
	// изменений черновика публикуется.
	probe := rtRaw(`{"v":3}`)
	if _, err := layer.EditDraft(again.DraftRevision, "geodata_settle", probe); err != nil {
		t.Fatalf("EditDraft пробной секции: %v", err)
	}
	pre, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	wantChanges := draftChanges(State{Draft: pre.Draft, Applied: pre.Applied}) - 1
	if err := layer.SettleSection("geodata_settle", func(applied, draft json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		return draft, draft, nil
	}); err != nil {
		t.Fatalf("SettleSection только Applied: %v", err)
	}
	post, err := layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if post.DraftRevision != pre.DraftRevision {
		t.Errorf("изменение только Applied сдвинуло ревизию: %d -> %d", pre.DraftRevision, post.DraftRevision)
	}
	gotApplied := false
	for deadline := time.Now().Add(5 * time.Second); !gotApplied && time.Now().Before(deadline); {
		dev, ok := rtNextEvent(events, EventDraft, time.Until(deadline))
		if !ok {
			break
		}
		if de, ok := dev.Data.(DraftEvent); ok && de.DraftRevision == post.DraftRevision && de.DraftChanges == wantChanges {
			gotApplied = true
		}
	}
	if !gotApplied {
		t.Errorf("нет события draft с ревизией %d и числом изменений %d после изменения только Applied", post.DraftRevision, wantChanges)
	}
	if err := layer.SettleSection("geodata_settle", func(json.RawMessage, json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		return nil, nil, nil
	}); err != nil {
		t.Errorf("уборка пробной секции: %v", err)
	}

	// Порог пользовательских geodata читается через слой без сравнения номеров в UI.
	switch a := layer.FeatureAvailability(FeatureXKeenUserGeodata); a {
	case Available, Unavailable, Undetermined:
		t.Logf("FeatureAvailability(xkeen_user_geodata) = %s", a)
	default:
		t.Errorf("неизвестное значение доступности: %q", a)
	}
	for _, k := range layer.Snapshot().Kernels {
		if k.Name == kernelXKeen && k.Installed && layer.FeatureAvailability(FeatureXKeenUserGeodata) != Available {
			t.Logf("XKeen %s: пользовательские geodata недоступны (порог %s)", k.Version, FeatureMinVersion(FeatureXKeenUserGeodata))
		}
	}
}
