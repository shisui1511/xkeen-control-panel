//go:build router

package configlayer

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
)

// Откат зафиксированного применения к снимку (RunRestore) на устройстве: настоящий
// слой, хранилище и `xray run -test` роутера, временный каталог в рабочем каталоге
// теста. Работающие ядра стенда не трогаются (Applier не задан), тест ядро не
// читает и не переключает — метка ядра не нужна.

// rtSectionNames — отсортированные имена секций.
func rtSectionNames(s Sections) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// rtSectionsEqual сверяет две карты секций посекционно, без лишних секций.
func rtSectionsEqual(t *testing.T, what string, got, want Sections) {
	t.Helper()
	gn, wn := rtSectionNames(got), rtSectionNames(want)
	if len(gn) != len(wn) {
		t.Errorf("%s: секции %v, ожидались %v", what, gn, wn)
		return
	}
	for i := range gn {
		if gn[i] != wn[i] {
			t.Errorf("%s: секции %v, ожидались %v", what, gn, wn)
			return
		}
		if !SectionEqual(got[gn[i]], want[gn[i]]) {
			t.Errorf("%s[%s] = %s, ожидалось %s", what, gn[i], got[gn[i]], want[gn[i]])
		}
	}
}

func TestRouterLayerRestore(t *testing.T) {
	t.Run("restore_applied", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.applyDraft(t)
		// Живой ключ, которого в снимке нет, и ключ, который снимок удаляет.
		if err := e.layer.UpdateLive(func(live Sections) error {
			live["rt_keep"] = json.RawMessage(`{"k":1}`)
			live["rt_drop"] = json.RawMessage(`{"d":1}`)
			return nil
		}); err != nil {
			t.Fatalf("UpdateLive: %v", err)
		}
		stateA := e.state(t)
		fileA := hashOf(e.file)

		// Состояние B: подмена секции меняет файл и добавляет секцию, которой в A нет.
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger: TriggerDNSTakeover,
			Sections: Sections{
				"rt_log":   json.RawMessage(`{"access":"none","level":"debug"}`),
				"rt_extra": json.RawMessage(`{"y":1}`),
			},
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог запуска B: %+v", view.Result)
		}
		if hashOf(e.file) == fileA {
			t.Fatal("запуск B не изменил файл генератора")
		}
		// Правка пользователя в другой секции черновика.
		e.edit(t, "rt_other", `{"x":1}`)
		before := e.state(t)

		view, err = e.layer.RunRestore(context.Background(), RestoreRun{
			Trigger: TriggerDNSRestore,
			Applied: stateA.Applied,
			Live:    Sections{"rt_drop": json.RawMessage(`null`), "rt_new": json.RawMessage(`{"n":1}`)},
		})
		if err != nil {
			t.Fatalf("RunRestore: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог отката: %+v", view.Result)
		}
		after := e.state(t)
		rtSectionsEqual(t, "Applied", after.Applied, stateA.Applied)
		if got := hashOf(e.file); got != fileA {
			t.Errorf("файл после отката %s, ожидался файл состояния A %s", got, fileA)
		}
		rtSecEqual(t, "Draft[rt_other]", after.Draft["rt_other"], `{"x":1}`)
		rtSectionsEqual(t, "Draft", after.Draft, before.Draft)
		if after.DraftRevision != before.DraftRevision {
			t.Errorf("ревизия черновика %d -> %d", before.DraftRevision, after.DraftRevision)
		}
		rtSecEqual(t, "Live[rt_keep]", after.Live["rt_keep"], `{"k":1}`)
		rtSecEqual(t, "Live[rt_new]", after.Live["rt_new"], `{"n":1}`)
		if _, has := after.Live["rt_drop"]; has {
			t.Errorf("ключ живых данных с null в снимке остался: %s", after.Live["rt_drop"])
		}
	})

	t.Run("reset_draft", func(t *testing.T) {
		e := rtNewSecEnv(t)
		e.edit(t, "rt_log", `{"access":"none"}`)
		e.edit(t, "rt_other", `{"x":1}`)
		e.applyDraft(t)
		stateA := e.state(t)
		fileA := hashOf(e.file)

		// B: обе секции подменены, черновик получает значения подмены целиком.
		overlay := Sections{
			"rt_log":   json.RawMessage(`{"access":"none","level":"debug"}`),
			"rt_other": json.RawMessage(`{"x":2}`),
		}
		view, err := e.layer.RunSectionLive(context.Background(), SectionLiveRun{
			Trigger:  TriggerDNSTakeover,
			Sections: overlay,
			MergeDraft: func(name string, _ json.RawMessage) (json.RawMessage, error) {
				return overlay[name], nil
			},
		})
		if err != nil {
			t.Fatalf("RunSectionLive: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог запуска B: %+v", view.Result)
		}
		// Пользователь правит rt_other после B; rt_log не трогает.
		e.edit(t, "rt_other", `{"x":3}`)
		before := e.state(t)

		view, err = e.layer.RunRestore(context.Background(), RestoreRun{
			Trigger:            TriggerDNSRestore,
			Applied:            stateA.Applied,
			ResetDraftSections: []string{"rt_log", "rt_other"},
		})
		if err != nil {
			t.Fatalf("RunRestore: %v", err)
		}
		if view.Result == nil || view.Result.Code != ResultApplied {
			t.Fatalf("итог отката: %+v", view.Result)
		}
		after := e.state(t)
		rtSectionsEqual(t, "Applied", after.Applied, stateA.Applied)
		if got := hashOf(e.file); got != fileA {
			t.Errorf("файл после отката %s, ожидался файл состояния A %s", got, fileA)
		}
		rtSecEqual(t, "Draft[rt_log]", after.Draft["rt_log"], `{"access":"none"}`)
		rtSecEqual(t, "Draft[rt_other]", after.Draft["rt_other"], `{"x":3}`)
		if after.DraftRevision != before.DraftRevision+1 {
			t.Errorf("ревизия %d -> %d, ожидался +1", before.DraftRevision, after.DraftRevision)
		}
	})
}
