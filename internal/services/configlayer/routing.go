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
type RoutingDesired struct {
	V       int  `json:"v"`
	Managed bool `json:"managed"`
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
