package configlayer

import (
	"regexp"
	"strconv"
	"strings"
)

// Минимальные версии ядер и XKeen (D-20). Единственное место в Go, где они
// объявлены; UI номера не сравнивает, а получает готовые статусы от API.
// Источник — 143-FINDINGS «Минимальные версии (SPK-02)».
const (
	MinXrayVersion   = "1.8.13"
	MinMihomoVersion = "1.18.4"
	MinXKeenVersion  = "2.0"
)

// Имена ядер в таблице порогов и во входе KernelVersionViews.
const (
	kernelXKeen  = "xkeen"
	kernelXray   = "xray"
	kernelMihomo = "mihomo"
)

// Feature — функция модели слоя, у которой есть порог версии ядра.
type Feature string

const (
	FeatureXrayOutboundsTail    Feature = "xray_outbounds_tail"
	FeatureXrayAutoGroup        Feature = "xray_auto_group"
	FeatureXrayFallbackGroup    Feature = "xray_fallback_group"
	FeatureXrayHotSwapAPI       Feature = "xray_hot_swap_api"
	FeatureMihomoUnixSocket     Feature = "mihomo_unix_socket"
	FeatureMihomoInlineProvider Feature = "mihomo_inline_provider"
	FeatureXKeenManagedInbounds Feature = "xkeen_managed_inbounds"
)

// featureThreshold — ядро, к которому относится функция, и первая версия,
// на которой она работает.
type featureThreshold struct {
	kernel string
	min    string
}

// featureThresholds — таблица «функция → ядро → порог». Источник —
// 143-FINDINGS «Минимальные версии (SPK-02)». Пометки у конкретных функций
// появляются в фазах, где появляются сами функции (D-07 из 143).
// «Применить» из-за версии не блокируется (D-20): таблица только сообщает,
// что функция недоступна или не определена.
var featureThresholds = map[Feature]featureThreshold{
	FeatureXrayOutboundsTail:    {kernelXray, "1.8.6"},
	FeatureXrayAutoGroup:        {kernelXray, "1.8.8"},
	FeatureXrayFallbackGroup:    {kernelXray, "1.8.10"},
	FeatureXrayHotSwapAPI:       {kernelXray, "1.8.13"},
	FeatureMihomoUnixSocket:     {kernelMihomo, "1.18.4"},
	FeatureMihomoInlineProvider: {kernelMihomo, "1.19.1"},
	FeatureXKeenManagedInbounds: {kernelXKeen, "2.0"},
}

// Version — числовая версия ядра.
type Version struct {
	Major, Minor, Patch int
}

// VersionStatus — результат разбора строки версии.
type VersionStatus string

const (
	VersionOK           VersionStatus = "ok"
	VersionUndetermined VersionStatus = "undetermined"
)

// Availability — доступность функции на установленной версии ядра.
type Availability string

const (
	Available    Availability = "available"
	Unavailable  Availability = "unavailable"
	Undetermined Availability = "undetermined"
)

// versionRe берёт числа с начала строки: хвост вроде " Beta" отбрасывается,
// отсутствующая патч-часть читается как 0 («2.0» → 2.0.0).
var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseKernelVersion разбирает сырую строку версии. Пустая строка, Alpha-сборка
// Mihomo («alpha-<хэш>») и всё нераспознанное («unknown», «error») дают
// VersionUndetermined: такая версия не сравнивается с релизными номерами и
// не должна выключать функции ложно (T-144-05).
func ParseKernelVersion(raw string) (Version, VersionStatus) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(strings.ToLower(s), "alpha") {
		return Version{}, VersionUndetermined
	}
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, VersionUndetermined
	}
	major, err1 := strconv.Atoi(m[1])
	minor, err2 := strconv.Atoi(m[2])
	patch := 0
	var err3 error
	if m[3] != "" {
		patch, err3 = strconv.Atoi(m[3])
	}
	if err1 != nil || err2 != nil || err3 != nil {
		return Version{}, VersionUndetermined
	}
	return Version{Major: major, Minor: minor, Patch: patch}, VersionOK
}

// CompareVersions сравнивает версии по трём числам: -1, 0 или 1.
func CompareVersions(a, b Version) int {
	switch {
	case a.Major != b.Major:
		return cmpInt(a.Major, b.Major)
	case a.Minor != b.Minor:
		return cmpInt(a.Minor, b.Minor)
	default:
		return cmpInt(a.Patch, b.Patch)
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// mustParseConst разбирает константу-порог из таблицы; константы заданы в
// этом файле и всегда разбираются.
func mustParseConst(raw string) Version {
	v, st := ParseKernelVersion(raw)
	if st != VersionOK {
		panic("configlayer: неразбираемая константа версии " + raw)
	}
	return v
}

// FeatureAvailable отвечает, доступна ли функция на версии ядра. Неизвестная
// функция или ядро, не совпадающее с ядром функции, — unavailable; версия не
// определена — undetermined.
func FeatureAvailable(kernel, rawVersion string, f Feature) Availability {
	th, ok := featureThresholds[f]
	if !ok || th.kernel != kernel {
		return Unavailable
	}
	v, st := ParseKernelVersion(rawVersion)
	if st != VersionOK {
		return Undetermined
	}
	if CompareVersions(v, mustParseConst(th.min)) < 0 {
		return Unavailable
	}
	return Available
}
