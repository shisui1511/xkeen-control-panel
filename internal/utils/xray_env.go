package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// XrayAssetEnv возвращает окружение для запуска xray, в котором задана
// XRAY_LOCATION_ASSET: без неё xray не находит geodata (.dat) при проверке
// конфигурации. Если переменная уже есть в environ, окружение возвращается как
// есть; иначе к копии environ добавляется первый существующий каталог из
// системных мест Entware, родителя configDir и самого configDir. Нет ни одного
// подходящего каталога — environ возвращается без изменений. Исходный срез не
// меняется.
func XrayAssetEnv(environ []string, configDir string) []string {
	for _, e := range environ {
		if strings.HasPrefix(e, "XRAY_LOCATION_ASSET=") {
			return environ
		}
	}
	candidates := []string{
		"/opt/etc/xray/dat",
		"/opt/share/xray",
		"/opt/etc/xray",
	}
	if configDir != "" {
		candidates = append(candidates, filepath.Dir(configDir), configDir)
	}
	for _, dir := range candidates {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out := make([]string, 0, len(environ)+1)
			out = append(out, environ...)
			return append(out, "XRAY_LOCATION_ASSET="+dir)
		}
	}
	return environ
}
