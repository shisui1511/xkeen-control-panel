package geodata

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
)

// MinDatSize — наименьший размер dat-файла, который XKeen принимает как
// настоящую базу (fetch_with_mirrors вызывается с порогом 24576 байт).
const MinDatSize = 24576

// DefaultXKeenDir — каталог служебных скриптов установленного XKeen.
const DefaultXKeenDir = "/opt/sbin/.xkeen"

// xkVariablesFile — файл с адресами наборов относительно каталога XKeen.
const xkVariablesFile = "01_info/01_info_variable.sh"

// Запасные значения — как в 01_info_variable.sh XKeen 2.0.1: используются, когда
// XKeen не установлен или переменная в его файле не найдена.
const (
	xkFallbackRefilterURL   = "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/geosite.dat"
	xkFallbackRefilterIPURL = "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/geoip.dat"
	xkFallbackV2flyURL      = "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat"
	xkFallbackV2flyIPURL    = "https://github.com/loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
	xkFallbackZkeenURL      = "https://github.com/jameszeroX/zkeen-domains/releases/latest/download/zkeen.dat"
	xkFallbackZkeenIPURL    = "https://github.com/jameszeroX/zkeen-ip/releases/latest/download/zkeenip.dat"
	xkFallbackProxy1        = "https://gh-proxy.com"
	xkFallbackProxy2        = "https://ghfast.top"
	xkFallbackTarURL        = "https://github.com/jameszeroX/XKeen/releases/latest/download/xkeen.tar.gz"
	xkFallbackDevURL        = "https://raw.githubusercontent.com/jameszeroX/xkeen/main/test/xkeen.tar.gz"
)

// StandardSet — один из шести стандартных наборов XKeen. File — основное имя
// файла в каталоге dat, Alias — второе имя (симлинк), которое XKeen держит рядом
// для zkeen; Display — название блока в выводе `xkeen -ug` дословно.
type StandardSet struct {
	ID, Provider, Kind, File, Alias, Display, URLKey string
}

// StandardSets — стандартные наборы XKeen 2.0.1 (02_install/04_install_geofile.sh).
var StandardSets = []StandardSet{
	{ID: "refilter_geosite", Provider: "refilter", Kind: "geosite", File: "geosite_refilter.dat", Display: "GeoSite Re:filter", URLKey: "refilter_url"},
	{ID: "refilter_geoip", Provider: "refilter", Kind: "geoip", File: "geoip_refilter.dat", Display: "GeoIP Re:filter", URLKey: "refilterip_url"},
	{ID: "v2fly_geosite", Provider: "v2fly", Kind: "geosite", File: "geosite_v2fly.dat", Display: "GeoSite V2Fly", URLKey: "v2fly_url"},
	{ID: "v2fly_geoip", Provider: "v2fly", Kind: "geoip", File: "geoip_v2fly.dat", Display: "GeoIP V2Fly", URLKey: "v2flyip_url"},
	{ID: "zkeen_geosite", Provider: "zkeen", Kind: "geosite", File: "geosite_zkeen.dat", Alias: "zkeen.dat", Display: "GeoSite ZKeen", URLKey: "zkeen_url"},
	{ID: "zkeen_geoip", Provider: "zkeen", Kind: "geoip", File: "geoip_zkeenip.dat", Alias: "zkeenip.dat", Display: "GeoIP ZKeenIP", URLKey: "zkeenip_url"},
}

// XKeenVars — адреса из установленного XKeen: URL наборов по имени переменной,
// зеркала gh_proxy1/gh_proxy2 и адреса, по которым XKeen проверяет доступность GitHub.
type XKeenVars struct {
	URLs       map[string]string
	Mirrors    []string
	GitHubURLs []string
}

var xkAssignRe = regexp.MustCompile(`^\s*([a-z0-9_]+)="([^"]*)"`)

// xkSetKeys — переменные наборов; значение по умолчанию на случай отсутствия.
var xkSetKeys = map[string]string{
	"refilter_url":   xkFallbackRefilterURL,
	"refilterip_url": xkFallbackRefilterIPURL,
	"v2fly_url":      xkFallbackV2flyURL,
	"v2flyip_url":    xkFallbackV2flyIPURL,
	"zkeen_url":      xkFallbackZkeenURL,
	"zkeenip_url":    xkFallbackZkeenIPURL,
}

// xkExtraKeys — прочие читаемые переменные.
var xkExtraKeys = map[string]string{
	"gh_proxy1":     xkFallbackProxy1,
	"gh_proxy2":     xkFallbackProxy2,
	"xkeen_tar_url": xkFallbackTarURL,
	"xkeen_dev_url": xkFallbackDevURL,
}

// LoadXKeenVars читает 01_info/01_info_variable.sh из dir (пусто — DefaultXKeenDir).
// Файл не исполняется: берутся только присваивания вида имя="значение" для известных
// ключей. Нет файла или ключа — запасное значение XKeen 2.0.1.
func LoadXKeenVars(dir string) XKeenVars {
	if dir == "" {
		dir = DefaultXKeenDir
	}
	vals := make(map[string]string, len(xkSetKeys)+len(xkExtraKeys))
	for k, v := range xkSetKeys {
		vals[k] = v
	}
	for k, v := range xkExtraKeys {
		vals[k] = v
	}
	if f, err := os.Open(filepath.Join(dir, xkVariablesFile)); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			m := xkAssignRe.FindStringSubmatch(sc.Text())
			if m == nil || m[2] == "" {
				continue
			}
			if _, ok := vals[m[1]]; ok {
				vals[m[1]] = m[2]
			}
		}
	}
	v := XKeenVars{URLs: make(map[string]string, len(xkSetKeys))}
	for k := range xkSetKeys {
		v.URLs[k] = vals[k]
	}
	v.Mirrors = []string{vals["gh_proxy1"], vals["gh_proxy2"]}
	v.GitHubURLs = []string{vals["xkeen_tar_url"], vals["xkeen_dev_url"]}
	return v
}

// URL — адрес стандартного набора; неизвестный ключ (или пустая структура) —
// запасное значение XKeen 2.0.1.
func (v XKeenVars) URL(set StandardSet) string {
	if u := v.URLs[set.URLKey]; u != "" {
		return u
	}
	return xkSetKeys[set.URLKey]
}
