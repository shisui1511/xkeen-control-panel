//go:build router

package configlayer

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Проверка уведомлений сервиса источников, события sources и порогов версий
// функций узлов на устройстве: настоящие хранилище, шина событий и таблица порогов.

func rtNewNoticeLayer(t *testing.T, enabled bool) *Layer {
	t.Helper()
	dir := rtLiveDir(t)
	layer, err := New(Options{
		DataDir: dir,
		Roots:   Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")},
		Enabled: func() bool { return enabled },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	return layer
}

// rtNextEvent ждёт событие заданного типа до таймаута; прочие события пропускает.
func rtNextEvent(ch <-chan Event, typ string, wait time.Duration) (Event, bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return Event{}, false
			}
			if ev.Type == typ {
				return ev, true
			}
		case <-timer.C:
			return Event{}, false
		}
	}
}

func rtNoticeIDs(views []NoticeView) []string {
	ids := make([]string, 0, len(views))
	for _, n := range views {
		ids = append(ids, n.ID)
	}
	return ids
}

func rtHasNotice(views []NoticeView, id string) (NoticeView, bool) {
	for _, n := range views {
		if n.ID == id {
			return n, true
		}
	}
	return NoticeView{}, false
}

func TestRouterSourceNotices(t *testing.T) {
	layer := rtNewNoticeLayer(t, true)
	events, cancel, err := layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	const id = "source_rejected:ab12cd34:mihomo"
	note := NoticeView{ID: id, Kind: noticeWarning, Kernel: KernelMihomo, Params: map[string]string{"name": "Имя", "date": "2026-10-05"}}

	layer.SetNotices("source_", []NoticeView{note})
	ev, ok := rtNextEvent(events, EventNotices, 2*time.Second)
	if !ok {
		t.Fatal("SetNotices не опубликовал событие notices")
	}
	if data, _ := ev.Data.(NoticesEvent); len(data.Notices) != 1 || data.Notices[0].Params["name"] != "Имя" {
		t.Errorf("событие notices: %+v", ev.Data)
	}
	got, found := rtHasNotice(layer.Snapshot().Notices, id)
	if !found || got.Params["date"] != "2026-10-05" || got.Kernel != KernelMihomo {
		t.Fatalf("уведомление в снимке слоя: %+v (есть: %v)", got, found)
	}

	// Тот же список — события нет.
	layer.SetNotices("source_", []NoticeView{note})
	if _, again := rtNextEvent(events, EventNotices, 300*time.Millisecond); again {
		t.Error("повтор того же списка опубликовал событие")
	}

	// Закрытие: уведомление пропадает и не возвращается тем же списком.
	views, err := layer.DismissNotice(id)
	if err != nil {
		t.Fatalf("DismissNotice: %v", err)
	}
	if _, still := rtHasNotice(views, id); still {
		t.Error("закрытое уведомление осталось в ответе")
	}
	if _, ok := rtNextEvent(events, EventNotices, 2*time.Second); !ok {
		t.Error("закрытие не опубликовало событие notices")
	}
	layer.SetNotices("source_", []NoticeView{note})
	if _, back := rtHasNotice(layer.Snapshot().Notices, id); back {
		t.Error("закрытое уведомление вернулось, пока условие не исчезло")
	}
	if _, again := rtNextEvent(events, EventNotices, 300*time.Millisecond); again {
		t.Error("повтор закрытого уведомления опубликовал событие")
	}

	// Условие исчезло и появилось снова — уведомление видно.
	layer.SetNotices("source_", nil)
	layer.SetNotices("source_", []NoticeView{note})
	if _, back := rtHasNotice(layer.Snapshot().Notices, id); !back {
		t.Error("уведомление не вернулось после исчезновения и повторного появления условия")
	}

	// Пустой список убирает уведомление.
	layer.SetNotices("source_", nil)
	if ids := rtNoticeIDs(layer.Snapshot().Notices); len(ids) != 0 {
		t.Errorf("после пустого списка остались уведомления: %v", ids)
	}

	// Неизвестный идентификатор без внешнего источника.
	if _, err := layer.DismissNotice("source_rejected:00000000:xray"); !errors.Is(err, ErrUnknownNotice) {
		t.Errorf("неизвестный идентификатор: %v", err)
	}
	if _, err := layer.DismissNotice("no_such_notice"); !errors.Is(err, ErrUnknownNotice) {
		t.Errorf("идентификатор без префикса: %v", err)
	}
}

func TestRouterPublishSources(t *testing.T) {
	layer := rtNewNoticeLayer(t, true)
	events, cancel, err := layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	if _, err := layer.EditDraft(0, "other", rtRaw(`{"a":1}`)); err != nil {
		t.Fatalf("EditDraft: %v", err)
	}
	layer.PublishSources()
	ev, ok := rtNextEvent(events, EventSources, 2*time.Second)
	if !ok {
		t.Fatal("событие sources не получено")
	}
	if data, _ := ev.Data.(SourcesEvent); data.Revision != 1 {
		t.Errorf("ревизия в событии sources: %+v", ev.Data)
	}

	off := rtNewNoticeLayer(t, false)
	offEvents, offCancel, err := off.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer offCancel()
	off.PublishSources()
	if _, got := rtNextEvent(offEvents, EventSources, 300*time.Millisecond); got {
		t.Error("выключенный слой опубликовал событие sources")
	}
}

func TestRouterFeatureThresholdsNodes(t *testing.T) {
	cases := []struct {
		kernel, version string
		f               Feature
		want            Availability
	}{
		{"mihomo", "v1.19.21", FeatureMihomoXHTTP, Unavailable},
		{"mihomo", "v1.19.22", FeatureMihomoXHTTP, Available},
		{"mihomo", "alpha-abc", FeatureMihomoXHTTP, Undetermined},
		{"xray", "24.11.20", FeatureXrayXHTTP, Unavailable},
		{"xray", "24.11.21", FeatureXrayXHTTP, Available},
		{"xray", "26.6.21", FeatureXraySessionIDFields, Unavailable},
		{"xray", "26.6.22", FeatureXraySessionIDFields, Available},
		{"mihomo", "1.19.22", FeatureMihomoXHTTPReuse, Unavailable},
		{"mihomo", "1.19.23", FeatureMihomoXHTTPReuse, Available},
		{"mihomo", "1.18.8", FeatureMihomoAWG, Unavailable},
		{"mihomo", "1.18.9", FeatureMihomoAWG, Available},
		{"mihomo", "1.19.12", FeatureMihomoAWG15, Unavailable},
		{"mihomo", "1.19.13", FeatureMihomoAWG15, Available},
		{"mihomo", "1.19.13", FeatureMihomoAWG20, Unavailable},
		{"mihomo", "1.19.14", FeatureMihomoAWG20, Available},
		{"mihomo", "1.19.29", FeatureMihomoAWG3, Unavailable},
		{"mihomo", "1.19.30", FeatureMihomoAWG3, Available},
		// Функция ядра, которого здесь нет, недоступна.
		{"xray", "26.9.9", FeatureMihomoXHTTP, Unavailable},
	}
	for _, c := range cases {
		if got := FeatureAvailable(c.kernel, c.version, c.f); got != c.want {
			t.Errorf("FeatureAvailable(%s, %s, %s) = %s, нужно %s", c.kernel, c.version, c.f, got, c.want)
		}
	}
	if got := FeatureMinVersion(FeatureMihomoAWG3); got != "1.19.30" {
		t.Errorf("FeatureMinVersion(AWG 3.x) = %q", got)
	}
	if got := FeatureMinVersion(Feature("no_such_feature")); got != "" {
		t.Errorf("FeatureMinVersion неизвестной функции = %q", got)
	}

	features := FeatureMap([]KernelVersionInput{
		{Name: "xray", Installed: true, Version: "26.9.9"},
		{Name: "mihomo", Installed: true, Version: "1.19.31"},
	})
	for _, f := range []Feature{
		FeatureXrayXHTTP, FeatureXraySessionIDFields, FeatureMihomoXHTTP, FeatureMihomoXHTTPReuse,
		FeatureMihomoAWG, FeatureMihomoAWG15, FeatureMihomoAWG20, FeatureMihomoAWG3,
	} {
		if features[f] != Available {
			t.Errorf("FeatureMap[%s] = %q на свежих ядрах", f, features[f])
		}
	}
}
