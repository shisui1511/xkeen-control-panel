package configlayer

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsPanelFileName(t *testing.T) {
	tests := []struct {
		kernel string
		rel    string
		want   bool
	}{
		// файлы панели
		{KernelXray, "04_outbounds.xcp-diag.tail.json", true},
		{KernelXray, "xcp-groups.json", true},
		{KernelXray, "04_xcp-a.json", true},
		{KernelXray, "05_routing.xcp-main.json", true},
		{KernelMihomo, "proxy_providers/xcp-diag.yaml", true},
		{KernelMihomo, "rule_providers/xcp-ru.yaml", true},
		{KernelMihomo, "profiles/xcp-panel.yaml", true},
		// старый слой и чужие файлы
		{KernelXray, "04_outbounds.zz_xcp_selected.tail.json", false},
		{KernelXray, "04_outbounds.zz_xcp_default.json", false},
		{KernelXray, "04_outbounds.sub_123.tail.json", false},
		{KernelXray, "xcp-diag.json.obsolete", false},
		{KernelXray, "atomic-123", false},
		{KernelXray, "xcp_hwid.txt", false},
		{KernelXray, "sub/xcp-a.json", false},
		{KernelXray, "XCP-A.json", false},
		{KernelMihomo, "config.yaml.xcp-link", false},
		{KernelMihomo, "xcp-a.yaml", false},
		{KernelMihomo, "proxy_providers/sub.yaml", false},
		{KernelMihomo, "cache/xcp-a.yaml", false},
		{KernelMihomo, "proxy_providers/xcp-a.yml", false},
		{"nope", "xcp-a.json", false},
	}
	for _, tc := range tests {
		t.Run(tc.kernel+"/"+tc.rel, func(t *testing.T) {
			if got := IsPanelFileName(tc.kernel, tc.rel); got != tc.want {
				t.Fatalf("IsPanelFileName(%q, %q) = %v, want %v", tc.kernel, tc.rel, got, tc.want)
			}
		})
	}
}

func TestValidatePanelName_Stoplist(t *testing.T) {
	err := ValidatePanelName(KernelXray, "04_outbounds.xcp-folder.tail.json")
	if !errors.Is(err, ErrStoplistName) {
		t.Fatalf("folder: err = %v, want ErrStoplistName", err)
	}
	if !strings.Contains(err.Error(), "old") {
		t.Fatalf("folder: текст ошибки %q не содержит слово стоп-списка old", err.Error())
	}

	err = ValidatePanelName(KernelMihomo, "proxy_providers/xcp-tmp-a.yaml")
	if !errors.Is(err, ErrStoplistName) {
		t.Fatalf("tmp: err = %v, want ErrStoplistName", err)
	}
	if !strings.Contains(err.Error(), "tmp") {
		t.Fatalf("tmp: текст ошибки %q не содержит слово tmp", err.Error())
	}

	if err := ValidatePanelName(KernelXray, "04_outbounds.xcp-diag.tail.json"); err != nil {
		t.Fatalf("допустимое имя: err = %v, want nil", err)
	}
	if err := ValidatePanelName(KernelXray, "zz_xcp_x.json"); !errors.Is(err, ErrInvalidPanelName) {
		t.Fatalf("zz_xcp_x.json: err = %v, want ErrInvalidPanelName", err)
	}
}

func TestHasForbiddenTransport(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"верхний уровень", `{"transport":{}}`, true},
		{"пробел до двоеточия во вложенном объекте", `{"a":{"transport" : 1}}`, true},
		{"табуляция и перевод строки до двоеточия", "\"transport\"\t\n:", true},
		{"похожий ключ", `{"transportX":1}`, false},
		{"слово как значение", `{"note":"transport"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasForbiddenTransport([]byte(tc.in)); got != tc.want {
				t.Fatalf("HasForbiddenTransport(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRoots_Abs(t *testing.T) {
	roots := Roots{Xray: filepath.Join(t.TempDir(), "configs"), Mihomo: filepath.Join(t.TempDir(), "mihomo")}

	got, err := roots.Abs(KernelXray, "04_outbounds.xcp-a.tail.json")
	if err != nil {
		t.Fatalf("Abs xray: %v", err)
	}
	if want := filepath.Join(roots.Xray, "04_outbounds.xcp-a.tail.json"); got != want {
		t.Fatalf("Abs xray = %q, want %q", got, want)
	}
	got, err = roots.Abs(KernelMihomo, "proxy_providers/xcp-a.yaml")
	if err != nil {
		t.Fatalf("Abs mihomo: %v", err)
	}
	if want := filepath.Join(roots.Mihomo, "proxy_providers", "xcp-a.yaml"); got != want {
		t.Fatalf("Abs mihomo = %q, want %q", got, want)
	}

	bad := []struct{ kernel, rel string }{
		{KernelXray, "../x.json"},
		{KernelXray, "/etc/passwd"},
		{KernelXray, "sub/xcp-a.json"},
		{KernelXray, ""},
		{KernelMihomo, "proxy_providers/../../x"},
		{KernelMihomo, "/proxy_providers/xcp-a.yaml"},
		{"nope", "x"},
	}
	for _, tc := range bad {
		if _, err := roots.Abs(tc.kernel, tc.rel); !errors.Is(err, ErrInvalidPanelPath) {
			t.Errorf("Abs(%q, %q) err = %v, want ErrInvalidPanelPath", tc.kernel, tc.rel, err)
		}
	}
}
