package services

import (
	"errors"
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
