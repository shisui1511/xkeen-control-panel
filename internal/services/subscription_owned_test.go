package services

import "testing"

func TestSubscriptionOwnedFileNames(t *testing.T) {
	t.Run("фрагменты подписки с ID xcp-foo", func(t *testing.T) {
		svc := NewSubscriptionService(t.TempDir(), t.TempDir(), t.TempDir())
		svc.mu.Lock()
		svc.subscriptions = []Subscription{{ID: "xcp-foo", Name: "foo"}}
		svc.mu.Unlock()

		got := svc.OwnedFileNames()
		for _, want := range []string{
			"04_outbounds.xcp-foo.tail.json",
			"04_outbounds.xcp-foo.json",
			"05_routing.xcp-foo.json",
		} {
			if !got[want] {
				t.Errorf("в наборе нет %q: %v", want, got)
			}
		}
		if got["01_log.json"] {
			t.Errorf("набор содержит чужое имя: %v", got)
		}
	})

	t.Run("пустой сервис — пустой набор", func(t *testing.T) {
		svc := NewSubscriptionService(t.TempDir(), t.TempDir(), t.TempDir())
		got := svc.OwnedFileNames()
		if len(got) != 0 {
			t.Errorf("набор не пуст: %v", got)
		}
	})

	t.Run("прежнее имя, совпавшее с файлом дефолта выбора, не берётся", func(t *testing.T) {
		svc := NewSubscriptionService(t.TempDir(), t.TempDir(), t.TempDir())
		svc.mu.Lock()
		svc.subscriptions = []Subscription{{ID: "zz_xcp_default", Name: "x"}}
		svc.mu.Unlock()

		got := svc.OwnedFileNames()
		if got[selectionDefaultFileName] {
			t.Errorf("файл дефолта выбора принят за фрагмент подписки: %v", got)
		}
		if !got["04_outbounds.zz_xcp_default.tail.json"] {
			t.Errorf("в наборе нет tail-фрагмента: %v", got)
		}
		if got[""] || got["."] {
			t.Errorf("в наборе пустое имя: %v", got)
		}
	})
}
