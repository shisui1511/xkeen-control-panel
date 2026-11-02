package configlayer

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// RoutingSection — имя секции черновика с желаемым владением маршрутизацией.
// Имя подходит под sectionNameRE.
const RoutingSection = "routing"

// routingSectionVersion — версия формата секции routing.
const routingSectionVersion = 1

// RoutingDesired — содержимое секции routing черновика: желаемое состояние
// «маршрутизацией управляет панель» (D-01). Фактическое владение — State.Routing
// (146-06): оно меняется только применением, а секция — правкой черновика.
//
// Решение по ядрам (147.1, D-06): Managed — владение Xray, и Mihomo, пока поле
// Mihomo не задано. Mihomo=false при Managed=true — Mihomo на своём профиле, Xray
// у панели; Managed=false при Mihomo=true — только Mihomo у панели, Xray за XKeen.
// Читать решение — только через ManagedFor и AnyManaged.
type RoutingDesired struct {
	V       int   `json:"v"`
	Managed bool  `json:"managed"`
	Mihomo  *bool `json:"mihomo,omitempty"`
	// MihomoProfile — имя своего профиля, на котором остаётся Mihomo при Mihomo=false.
	MihomoProfile string `json:"mihomo_profile,omitempty"`
}

// ManagedFor — управляет ли панель маршрутизацией ядра. Mihomo с заданным полем
// Mihomo следует ему, иначе — Managed; Xray и прочие — Managed.
func (d RoutingDesired) ManagedFor(kernel string) bool {
	if kernel == KernelMihomo && d.Mihomo != nil {
		return *d.Mihomo
	}
	return d.Managed
}

// AnyManaged — панель держит хотя бы одно ядро.
func (d RoutingDesired) AnyManaged() bool {
	return d.ManagedFor(KernelXray) || d.ManagedFor(KernelMihomo)
}

// WithMihomo — копия с решением по Mihomo. on при Managed снимает поле (Mihomo
// следует за Managed); on без Managed — &true. off — всегда &false: решение
// «Mihomo на своём профиле» переживает «Отпустить» и «Взять» на других вкладках
// (D-07). profile пишется только при off.
func (d RoutingDesired) WithMihomo(on bool, profile string) RoutingDesired {
	out := d
	out.MihomoProfile = ""
	switch {
	case on && d.Managed:
		out.Mihomo = nil
	case on:
		v := true
		out.Mihomo = &v
	default:
		v := false
		out.Mihomo = &v
		out.MihomoProfile = profile
	}
	return out
}

// ParseRoutingDesired разбирает секцию routing. Пустой или null raw — секции
// нет: {V: 1, Managed: false}. Не объект или неизвестная версия — ошибка.
func ParseRoutingDesired(raw json.RawMessage) (RoutingDesired, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return RoutingDesired{V: routingSectionVersion}, nil
	}
	if trimmed[0] != '{' {
		return RoutingDesired{}, fmt.Errorf("секция %s: ожидался объект", RoutingSection)
	}
	var out RoutingDesired
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return RoutingDesired{}, fmt.Errorf("секция %s: %w", RoutingSection, err)
	}
	if out.V != routingSectionVersion {
		return RoutingDesired{}, fmt.Errorf("секция %s: неизвестная версия %d", RoutingSection, out.V)
	}
	return out, nil
}

// DesiredRouting читает секцию routing из секций черновика или применённого
// состояния. Секции нет или она не разбирается — Managed=false: сообщить о порче
// секции отдельно обязаны генераторы и проверка 146-05 (через ParseRoutingDesired).
func DesiredRouting(s Sections) RoutingDesired {
	d, err := ParseRoutingDesired(s[RoutingSection])
	if err != nil {
		return RoutingDesired{V: routingSectionVersion}
	}
	return d
}
