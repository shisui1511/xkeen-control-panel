package services

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// fixedPanelFileNames — имена файлов панели в корне каталога Xray, которые
// строятся вне пакета services или не зависят от ID подписки.
var fixedPanelFileNames = []string{
	"00_api.json",
	"05_routing.json",
	"04_outbounds.json",
	"01_log.json",
	"02_dns.json",
	"03_inbounds.json",
	"06_policy.json",
	selectionDefaultFileName,
	selectionTailFileName,
}

func TestPanelGeneratedNames_NeverMatchStoplist(t *testing.T) {
	svc := NewSubscriptionService(t.TempDir(), t.TempDir(), t.TempDir())

	ids := []string{"safe_id", "sub_1700000000_0", "zz_xcp_selected", "zz_xcp_default"}
	for i := 0; i < 200; i++ {
		ids = append(ids, svc.generateIDLocked())
	}

	names := append([]string{}, fixedPanelFileNames...)
	for _, id := range ids {
		sub := &Subscription{ID: id}
		names = append(names,
			filepath.Base(svc.getFragmentPath(sub)),
			filepath.Base(svc.getRoutingFragmentPath(sub)),
		)
		if legacy := svc.legacyFragmentPath(sub); legacy != "" {
			names = append(names, filepath.Base(legacy))
		}
	}

	for _, name := range names {
		if word, hit := utils.XKeenStoplistMatch(name); hit {
			t.Errorf("panel generates %q, which matches the XKeen stop-list word %q", name, word)
		}
	}
}

func TestAdd_ClientIDFromStoplistReplaced(t *testing.T) {
	generated := regexp.MustCompile(`^sub_\d+$`)

	for _, clientID := range []string{"bak", "Gold", "tmp-list", "my_old", "copy1", "orig"} {
		t.Run(clientID, func(t *testing.T) {
			svc := NewSubscriptionService(t.TempDir(), t.TempDir(), t.TempDir())
			sub := Subscription{ID: clientID, Name: "S", URL: "https://example.com/sub", Enabled: true, EnableXray: true}
			if err := svc.Add(&sub); err != nil {
				t.Fatalf("Add must not fail for a stop-list ID, got: %v", err)
			}
			if !generated.MatchString(sub.ID) {
				t.Fatalf("ID %q must be replaced by sub_<unixnano>, got %q", clientID, sub.ID)
			}
			if svc.Get(sub.ID) == nil {
				t.Errorf("subscription must be saved under the replaced ID %q", sub.ID)
			}
		})
	}

	for _, clientID := range []string{"home", "backup", "work-2"} {
		t.Run("keeps_"+clientID, func(t *testing.T) {
			svc := NewSubscriptionService(t.TempDir(), t.TempDir(), t.TempDir())
			sub := Subscription{ID: clientID, Name: "S", URL: "https://example.com/sub", Enabled: true, EnableXray: true}
			if err := svc.Add(&sub); err != nil {
				t.Fatalf("Add: %v", err)
			}
			if sub.ID != clientID {
				t.Errorf("safe client ID %q must be kept, got %q", clientID, sub.ID)
			}
		})
	}
}

func TestGuardXrayRootName_Names(t *testing.T) {
	dir := t.TempDir()
	if err := guardXrayRootName(filepath.Join(dir, "04_outbounds.bak.tail.json")); !errors.Is(err, ErrXKeenStoplistName) {
		t.Errorf("stop-list name must give ErrXKeenStoplistName, got %v", err)
	}
	if err := guardXrayRootName(filepath.Join(dir, "04_outbounds.sub_1.tail.json")); err != nil {
		t.Errorf("safe name must pass, got %v", err)
	}
}

// injectSubscription кладёт подписку в срез в обход Add — так проверяется
// защита записи для ID, созданных до появления правила (Add такой ID заменил бы).
func injectSubscription(svc *SubscriptionService, sub Subscription) *Subscription {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.subscriptions = append(svc.subscriptions, sub)
	return &svc.subscriptions[len(svc.subscriptions)-1]
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file %s must not be created (stat err: %v)", filepath.Base(path), err)
	}
}

func TestGuardXrayRootName_BlocksWrite(t *testing.T) {
	newSvc := func(t *testing.T) (*SubscriptionService, string) {
		t.Helper()
		tmp := t.TempDir()
		xrayDir := filepath.Join(tmp, "xray")
		if err := os.MkdirAll(xrayDir, 0755); err != nil {
			t.Fatal(err)
		}
		return NewSubscriptionService(tmp, xrayDir, tmp), xrayDir
	}
	outbounds := []Outbound{{Tag: "n1", Protocol: "freedom"}}

	t.Run("writeFragment", func(t *testing.T) {
		svc, _ := newSvc(t)
		sub := injectSubscription(svc, Subscription{ID: "bak_x", Name: "S", EnableXray: true, Enabled: true})
		path := svc.getFragmentPath(sub)
		if _, err := svc.writeFragment(path, outbounds, sub); !errors.Is(err, ErrXKeenStoplistName) {
			t.Fatalf("want ErrXKeenStoplistName, got %v", err)
		}
		assertNoFile(t, path)
	})

	t.Run("writeRoutingFragment", func(t *testing.T) {
		svc, _ := newSvc(t)
		sub := injectSubscription(svc, Subscription{ID: "bak_x", Name: "S", EnableXray: true, Enabled: true})
		path := svc.getRoutingFragmentPath(sub)
		if err := svc.writeRoutingFragment(path, sub, []string{"n1"}); !errors.Is(err, ErrXKeenStoplistName) {
			t.Fatalf("want ErrXKeenStoplistName, got %v", err)
		}
		assertNoFile(t, path)
	})

	t.Run("refreshXrayFragmentLocked", func(t *testing.T) {
		svc, _ := newSvc(t)
		sub := injectSubscription(svc, Subscription{ID: "bak_x", Name: "S", EnableXray: true, Enabled: true})
		path := svc.getFragmentPath(sub)
		orig := []byte(`{"outbounds":[{"tag":"n1","protocol":"freedom"}]}`)
		if err := os.WriteFile(path, orig, 0600); err != nil {
			t.Fatal(err)
		}
		svc.mu.Lock()
		err := svc.refreshXrayFragmentLocked(sub)
		svc.mu.Unlock()
		if !errors.Is(err, ErrXKeenStoplistName) {
			t.Fatalf("want ErrXKeenStoplistName, got %v", err)
		}
		if got, _ := os.ReadFile(path); string(got) != string(orig) {
			t.Errorf("fragment must stay untouched, got %q", got)
		}
	})

	t.Run("selectionFileWrite.apply", func(t *testing.T) {
		_, dir := newSvc(t)
		path := filepath.Join(dir, "04_outbounds.zz_xcp_bak.json")
		w := selectionFileWrite{path: path, newData: []byte(`{}`)}
		if err := w.apply(); !errors.Is(err, ErrXKeenStoplistName) {
			t.Fatalf("want ErrXKeenStoplistName, got %v", err)
		}
		assertNoFile(t, path)

		// Удаление файла проверке не подлежит.
		if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := (selectionFileWrite{path: path}).apply(); err != nil {
			t.Fatalf("removal must not be gated: %v", err)
		}
		assertNoFile(t, path)
	})

	t.Run("migrateLegacyFragments", func(t *testing.T) {
		svc, _ := newSvc(t)
		sub := injectSubscription(svc, Subscription{ID: "bak_x", Name: "S", EnableXray: true, Enabled: true})
		legacy := svc.legacyFragmentPath(sub)
		if err := os.WriteFile(legacy, []byte(`{"outbounds":[]}`), 0600); err != nil {
			t.Fatal(err)
		}
		svc.mu.Lock()
		changed := svc.migrateLegacyFragmentsLocked()
		svc.mu.Unlock()
		if changed {
			t.Error("legacy fragment with a stop-list target name must not be renamed")
		}
		if _, err := os.Stat(legacy); err != nil {
			t.Errorf("legacy fragment must stay in place: %v", err)
		}
		assertNoFile(t, svc.getFragmentPath(sub))
	})

	t.Run("safe_id_is_written", func(t *testing.T) {
		svc, _ := newSvc(t)
		sub := injectSubscription(svc, Subscription{ID: "sub_1", Name: "S", EnableXray: true, Enabled: true})
		path := svc.getFragmentPath(sub)
		if _, err := svc.writeFragment(path, outbounds, sub); err != nil {
			t.Fatalf("safe name must be written: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("fragment must exist: %v", err)
		}
	})
}
