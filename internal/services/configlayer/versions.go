package configlayer

// Каркас контракта версий (D-20). Реализация — в следующем коммите.

// Feature — функция модели слоя, у которой есть порог версии ядра.
type Feature string

const FeatureXrayHotSwapAPI Feature = "xray_hot_swap_api"

// Availability — доступность функции на установленной версии ядра.
type Availability string

const (
	Available    Availability = "available"
	Unavailable  Availability = "unavailable"
	Undetermined Availability = "undetermined"
)

// FeatureAvailable отвечает, доступна ли функция на версии ядра.
func FeatureAvailable(kernel, rawVersion string, f Feature) Availability {
	return Undetermined
}
