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
