package configlayer

import "testing"

// Трассер: сырая строка версии ядра → разбор → порог функции → доступность.
func TestVersions_TracerHotSwap(t *testing.T) {
	cases := []struct {
		version string
		want    Availability
	}{
		{"1.8.11", Unavailable},
		{"1.8.13", Available},
		{"26.9.9", Available},
	}
	for _, tc := range cases {
		got := FeatureAvailable("xray", tc.version, FeatureXrayHotSwapAPI)
		if got != tc.want {
			t.Errorf("FeatureAvailable(xray, %q, hot_swap_api) = %q, ожидалось %q", tc.version, got, tc.want)
		}
	}
}

func TestParseKernelVersion(t *testing.T) {
	cases := []struct {
		raw    string
		want   Version
		status VersionStatus
	}{
		{"26.9.9", Version{26, 9, 9}, VersionOK},
		{"v1.19.31", Version{1, 19, 31}, VersionOK},
		{"1.19.31", Version{1, 19, 31}, VersionOK},
		{"2.0", Version{2, 0, 0}, VersionOK},
		{"2.0.1 Beta", Version{2, 0, 1}, VersionOK},
		{"  1.8.13\n", Version{1, 8, 13}, VersionOK},
		{"alpha-f103639", Version{}, VersionUndetermined},
		{"Alpha-smart", Version{}, VersionUndetermined},
		{"unknown", Version{}, VersionUndetermined},
		{"error", Version{}, VersionUndetermined},
		{"", Version{}, VersionUndetermined},
		{"not installed", Version{}, VersionUndetermined},
	}
	for _, tc := range cases {
		got, st := ParseKernelVersion(tc.raw)
		if st != tc.status || got != tc.want {
			t.Errorf("ParseKernelVersion(%q) = %+v, %q; ожидалось %+v, %q", tc.raw, got, st, tc.want, tc.status)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b Version
		want int
	}{
		{Version{1, 8, 13}, Version{1, 8, 13}, 0},
		{Version{1, 8, 11}, Version{1, 8, 13}, -1},
		{Version{1, 9, 0}, Version{1, 8, 13}, 1},
		{Version{26, 0, 0}, Version{1, 99, 99}, 1},
		{Version{2, 0, 0}, Version{2, 0, 1}, -1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%+v, %+v) = %d, ожидалось %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestFeatureAvailable_Table(t *testing.T) {
	cases := []struct {
		kernel  string
		feature Feature
		below   string
		at      string
		above   string
	}{
		{"xray", FeatureXrayOutboundsTail, "1.8.5", "1.8.6", "1.8.24"},
		{"xray", FeatureXrayAutoGroup, "1.8.7", "1.8.8", "1.8.24"},
		{"xray", FeatureXrayFallbackGroup, "1.8.9", "1.8.10", "26.9.9"},
		{"xray", FeatureXrayHotSwapAPI, "1.8.12", "1.8.13", "26.9.9"},
		{"mihomo", FeatureMihomoUnixSocket, "1.18.3", "1.18.4", "v1.19.31"},
		{"mihomo", FeatureMihomoInlineProvider, "1.19.0", "1.19.1", "v1.19.31"},
		{"xkeen", FeatureXKeenManagedInbounds, "1.1.3", "2.0", "2.0.1 Beta"},
	}
	for _, tc := range cases {
		if got := FeatureAvailable(tc.kernel, tc.below, tc.feature); got != Unavailable {
			t.Errorf("%s %q ниже порога: %q, ожидалось unavailable", tc.feature, tc.below, got)
		}
		if got := FeatureAvailable(tc.kernel, tc.at, tc.feature); got != Available {
			t.Errorf("%s %q на пороге: %q, ожидалось available", tc.feature, tc.at, got)
		}
		if got := FeatureAvailable(tc.kernel, tc.above, tc.feature); got != Available {
			t.Errorf("%s %q выше порога: %q, ожидалось available", tc.feature, tc.above, got)
		}
	}

	if got := FeatureAvailable("mihomo", "alpha-f103639", FeatureMihomoUnixSocket); got != Undetermined {
		t.Errorf("Alpha Mihomo: %q, ожидалось undetermined", got)
	}
	if got := FeatureAvailable("xray", "26.9.9", FeatureMihomoUnixSocket); got != Unavailable {
		t.Errorf("функция чужого ядра: %q, ожидалось unavailable", got)
	}
	if got := FeatureAvailable("xray", "26.9.9", Feature("nope")); got != Unavailable {
		t.Errorf("неизвестная функция: %q, ожидалось unavailable", got)
	}
}

func TestKernelVersionViews(t *testing.T) {
	views := KernelVersionViews([]KernelVersionInput{
		{Name: "xray", Installed: true, Version: "26.9.9"},
		{Name: "mihomo", Installed: true, Version: "alpha-f103639"},
	})
	want := []KernelVersionView{
		{Name: "xkeen", Installed: false, Version: "", Status: "not_installed", MinVersion: "2.0"},
		{Name: "xray", Installed: true, Version: "26.9.9", Status: "ok", MinVersion: "1.8.13"},
		{Name: "mihomo", Installed: true, Version: "alpha-f103639", Status: "undetermined", MinVersion: "1.18.4"},
	}
	if len(views) != len(want) {
		t.Fatalf("строк %d, ожидалось %d: %+v", len(views), len(want), views)
	}
	for i := range want {
		if views[i] != want[i] {
			t.Errorf("строка %d: %+v, ожидалось %+v", i, views[i], want[i])
		}
	}

	below := KernelVersionViews([]KernelVersionInput{{Name: "xray", Installed: true, Version: "1.8.11"}})
	if below[1].Name != "xray" || below[1].Status != "below_min" {
		t.Errorf("xray 1.8.11: %+v, ожидалось below_min", below[1])
	}

	unk := KernelVersionViews([]KernelVersionInput{{Name: "xray", Installed: true, Version: "unknown"}})
	if unk[1].Status != "undetermined" {
		t.Errorf("xray unknown: %+v, ожидалось undetermined", unk[1])
	}

	notInst := KernelVersionViews([]KernelVersionInput{{Name: "mihomo", Installed: false, Version: "1.19.31"}})
	if notInst[2].Status != "not_installed" {
		t.Errorf("mihomo не установлен: %+v, ожидалось not_installed", notInst[2])
	}
}

func TestFeatureMap(t *testing.T) {
	inputs := []KernelVersionInput{
		{Name: "xray", Installed: true, Version: "26.9.9"},
		{Name: "mihomo", Installed: true, Version: "1.19.31"},
		{Name: "xkeen", Installed: true, Version: "2.0.1 Beta"},
	}
	got := FeatureMap(inputs)
	features := []Feature{
		FeatureXrayOutboundsTail, FeatureXrayAutoGroup, FeatureXrayFallbackGroup, FeatureXrayHotSwapAPI,
		FeatureMihomoUnixSocket, FeatureMihomoInlineProvider, FeatureXKeenManagedInbounds,
	}
	if len(got) != len(features) {
		t.Errorf("в карте %d функций, ожидалось %d", len(got), len(features))
	}
	for _, f := range features {
		if got[f] != Available {
			t.Errorf("%s: %q, ожидалось available", f, got[f])
		}
	}

	none := FeatureMap([]KernelVersionInput{{Name: "xray", Installed: false, Version: "26.9.9"}})
	if none[FeatureXrayHotSwapAPI] != Unavailable {
		t.Errorf("ядро не установлено: %q, ожидалось unavailable", none[FeatureXrayHotSwapAPI])
	}
}
