package nodes

import "strings"

// Таблицы отпечатков uTLS обоих ядер и перевод между ними.
//
// Источники (при обновлении ядер таблицы сверяются заново):
//   - Xray: transport/internet/tls/tls.go — PresetFingerprints, ModernFingerprints,
//     OtherFingerprints; infra/conf/transport_security.go. Имя приводится к нижнему
//     регистру, пустое — chrome, неизвестное — ошибка конфигурации. В Reality
//     unsafe и hellogolang запрещены.
//   - Mihomo: component/tls/utls.go — fingerprints; transport/vmess/tls.go.
//     Регистр не приводится; пустое, none или неизвестное имя — TLS Go без uTLS,
//     а у Reality каждое подключение падает («REALITY is based on uTLS»).
//
// Сервер отпечаток клиента не проверяет: замена меняет маскировку, а не
// подключение. Поэтому перевод точный там, где аналог есть, иначе — ближайший
// из семейства.

// xrayFingerprints — все имена, принимаемые Xray: Preset ∪ Modern ∪ Other.
var xrayFingerprints = setOf(
	// PresetFingerprints
	"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq",
	"random", "randomized", "randomizednoalpn", "unsafe",
	// ModernFingerprints
	"hellochrome_120", "hellochrome_131", "hellochrome_133",
	"hellofirefox_120", "hellofirefox_148",
	"helloios_13", "helloios_14", "helloedge_106", "hellosafari_26_3",
	"hello360_11_0", "helloqq_11_1",
	// OtherFingerprints
	"hellogolang", "hellorandomized", "hellorandomizedalpn", "hellorandomizednoalpn",
	"hellochrome_auto", "hellofirefox_auto", "hellosafari_auto", "helloios_auto",
	"helloandroid_11_okhttp", "helloedge_auto", "hello360_auto", "helloqq_auto",
	"hellochrome_58", "hellochrome_62", "hellochrome_70", "hellochrome_72",
	"hellochrome_83", "hellochrome_87", "hellochrome_96", "hellochrome_100",
	"hellochrome_102", "hellochrome_100_psk", "hellochrome_106_shuffle",
	"hellochrome_112_psk_shuf", "hellochrome_114_padding_psk_shuf",
	"hellochrome_115_pq", "hellochrome_115_pq_psk", "hellochrome_120_pq",
	"hellofirefox_55", "hellofirefox_56", "hellofirefox_63", "hellofirefox_65",
	"hellofirefox_99", "hellofirefox_102", "hellofirefox_105",
	"helloios_11_1", "helloios_12_1", "helloedge_85", "hellosafari_16_0",
	"hello360_7_5",
)

// mihomoFingerprints — имена, принимаемые Mihomo (none — без uTLS).
var mihomoFingerprints = setOf(
	"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq",
	"random", "randomized",
	"chrome120", "firefox120", "safari16",
	"chrome_psk", "chrome_psk_shuffle", "chrome_padding_psk_shuffle",
	"chrome_pq", "chrome_pq_psk",
	"none",
)

// mihomoToXray — имя Mihomo → тот же ClientHelloID в Xray (без потерь).
var mihomoToXray = map[string]string{
	"chrome120":                  "hellochrome_120",
	"firefox120":                 "hellofirefox_120",
	"safari16":                   "hellosafari_16_0",
	"chrome_psk":                 "hellochrome_100_psk",
	"chrome_psk_shuffle":         "hellochrome_106_shuffle",
	"chrome_padding_psk_shuffle": "hellochrome_114_padding_psk_shuf",
	"chrome_pq":                  "hellochrome_115_pq",
	"chrome_pq_psk":              "hellochrome_115_pq_psk",
	"none":                       "unsafe",
}

// xrayToMihomo — точные аналоги Xray → Mihomo.
var xrayToMihomo = map[string]string{
	"hellochrome_auto":                 "chrome",
	"hellofirefox_auto":                "firefox",
	"hellosafari_auto":                 "safari",
	"helloios_auto":                    "ios",
	"helloandroid_11_okhttp":           "android",
	"helloedge_auto":                   "edge",
	"hello360_auto":                    "360",
	"helloqq_auto":                     "qq",
	"hellochrome_120":                  "chrome120",
	"hellofirefox_120":                 "firefox120",
	"hellosafari_16_0":                 "safari16",
	"hellochrome_100_psk":              "chrome_psk",
	"hellochrome_106_shuffle":          "chrome_psk_shuffle",
	"hellochrome_114_padding_psk_shuf": "chrome_padding_psk_shuffle",
	"hellochrome_115_pq":               "chrome_pq",
	"hellochrome_115_pq_psk":           "chrome_pq_psk",
	"hellorandomized":                  "randomized",
	"unsafe":                           "none",
	"hellogolang":                      "none",
}

// xrayFamilies — префикс имени Xray → семейство Mihomo для имён без точного аналога.
var xrayFamilies = []struct{ prefix, family string }{
	{"hellochrome_", "chrome"},
	{"hellofirefox_", "firefox"},
	{"helloios_", "ios"},
	{"hellosafari_", "safari"},
	{"helloedge_", "edge"},
	{"hello360_", "360"},
	{"helloqq_", "qq"},
	{"hellorandomized", "randomized"},
	{"randomized", "randomized"},
}

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func normFingerprint(fp string) string { return strings.ToLower(strings.TrimSpace(fp)) }

// validXrayFingerprint сообщает, что имя принимает Xray; в Reality unsafe и
// hellogolang запрещены.
func validXrayFingerprint(fp string, reality bool) bool {
	fp = normFingerprint(fp)
	if !xrayFingerprints[fp] {
		return false
	}
	return !reality || (fp != "unsafe" && fp != "hellogolang")
}

// XrayFingerprint переводит отпечаток uTLS в имя Xray. replaced == true только
// при замене значения на другое (не при точном переводе имени); пустой out —
// «ключ не писать» (Xray подставит chrome).
func XrayFingerprint(fp string, reality bool) (out string, replaced bool) {
	fp = normFingerprint(fp)
	if fp == "" {
		return "", false
	}
	if x, ok := mihomoToXray[fp]; ok {
		fp = x
	}
	if !validXrayFingerprint(fp, reality) {
		return "", true
	}
	return fp, false
}

// MihomoFingerprint переводит отпечаток uTLS в имя Mihomo. Пустой отпечаток —
// chrome (так ведёт себя Xray). Без uTLS Mihomo не работает с Reality, поэтому
// none и аналоги в Reality заменяются на chrome.
func MihomoFingerprint(fp string, reality bool) (out string, replaced bool) {
	fp = normFingerprint(fp)
	if fp == "" {
		return "chrome", false
	}
	if !mihomoFingerprints[fp] {
		if m, ok := xrayToMihomo[fp]; ok {
			fp = m
		} else {
			return mihomoFamily(fp), true
		}
	}
	if fp == "none" && reality {
		return "chrome", true
	}
	return fp, false
}

// mihomoFamily подбирает ближайшее имя Mihomo из семейства; без семейства — chrome.
func mihomoFamily(fp string) string {
	for _, f := range xrayFamilies {
		if strings.HasPrefix(fp, f.prefix) {
			return f.family
		}
	}
	return "chrome"
}
