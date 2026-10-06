package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envValue(env []string, key string) (string, bool) {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v, true
		}
	}
	return "", false
}

func TestXrayAssetEnv(t *testing.T) {
	t.Run("добавляет переменную с существующим каталогом", func(t *testing.T) {
		configDir := t.TempDir()
		base := []string{"PATH=/usr/bin", "HOME=/root"}
		got := XrayAssetEnv(base, configDir)

		val, ok := envValue(got, "XRAY_LOCATION_ASSET")
		if !ok {
			t.Fatalf("XRAY_LOCATION_ASSET не добавлена: %v", got)
		}
		if st, err := os.Stat(val); err != nil || !st.IsDir() {
			t.Fatalf("XRAY_LOCATION_ASSET=%q не указывает на каталог: %v", val, err)
		}
		if v, _ := envValue(got, "PATH"); v != "/usr/bin" {
			t.Errorf("исходные переменные потеряны: %v", got)
		}
		if len(base) != 2 {
			t.Errorf("исходный срез изменён: %v", base)
		}
	})

	t.Run("кандидат от configDir, когда системных каталогов нет", func(t *testing.T) {
		parent := t.TempDir()
		configDir := filepath.Join(parent, "configs")
		if err := os.Mkdir(configDir, 0o755); err != nil {
			t.Fatal(err)
		}
		val, _ := envValue(XrayAssetEnv(nil, configDir), "XRAY_LOCATION_ASSET")
		if val == "" {
			t.Fatal("переменная не выставлена")
		}
	})

	t.Run("уже заданная переменная не трогается", func(t *testing.T) {
		base := []string{"A=1", "XRAY_LOCATION_ASSET=/custom/dat"}
		got := XrayAssetEnv(base, t.TempDir())
		if len(got) != len(base) {
			t.Fatalf("окружение изменено: %v", got)
		}
		if v, _ := envValue(got, "XRAY_LOCATION_ASSET"); v != "/custom/dat" {
			t.Errorf("XRAY_LOCATION_ASSET = %q, want /custom/dat", v)
		}
	})

	t.Run("нет подходящего каталога — окружение как есть", func(t *testing.T) {
		for _, sys := range []string{"/opt/etc/xray/dat", "/opt/share/xray", "/opt/etc/xray"} {
			if _, err := os.Stat(sys); err == nil {
				t.Skipf("на машине есть системный каталог %s", sys)
			}
		}
		missing := filepath.Join(t.TempDir(), "no", "such", "dir")
		got := XrayAssetEnv([]string{"A=1"}, missing)
		if _, ok := envValue(got, "XRAY_LOCATION_ASSET"); ok {
			t.Errorf("переменная выставлена без существующего каталога: %v", got)
		}
	})
}
