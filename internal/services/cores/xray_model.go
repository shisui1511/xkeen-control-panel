// Package cores — секции ядер Xray и Mihomo: пресеты, закреплённые значения,
// файлы панели, проверка до записи и сервис API (Phase 147).
package cores

import (
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Идентификатор генератора секций Xray в реестре слоя.
const xrayGenID = "cores_xray"

// XraySpec — описание одной секции Xray под управлением панели.
type XraySpec struct {
	// ID — идентификатор секции внутри ядра: log, dns, inbounds, policy, other.
	ID string
	// Section — имя секции в черновике слоя (xray_log, …).
	Section string
	// Key — ключ верхнего уровня в файле Xray; у «Прочих ключей» пуст.
	Key string
	// File — файл панели секции в каталоге Xray.
	File string
}

// XraySpecs — секции Xray в порядке показа.
var XraySpecs = buildXraySpecs()

func buildXraySpecs() []XraySpec {
	out := make([]XraySpec, 0, len(configlayer.XraySectionIDs))
	for _, id := range configlayer.XraySectionIDs {
		spec := XraySpec{
			ID:      id,
			Section: configlayer.CoreSectionName(configlayer.KernelXray, id),
			Key:     id,
			File:    configlayer.PanelXraySectionFile(id),
		}
		if id == "other" {
			spec.Key = ""
		}
		out = append(out, spec)
	}
	return out
}

// XrayFormKeys — ключи, которые знает форма секции; всё остальное в значении
// секции форма не трогает и сохраняет как есть. Совпадает с FORM_KEYS
// фронтенда.
var XrayFormKeys = map[string][]string{
	"log":      {"loglevel", "access", "error", "dnsLog"},
	"dns":      {"servers", "queryStrategy", "disableCache"},
	"inbounds": {"[].tag", "[].protocol", "[].listen", "[].port"},
	"policy":   {"levels.0.handshake", "levels.0.connIdle", "levels.0.uplinkOnly", "levels.0.downlinkOnly"},
	"other":    {},
}

// xraySpecByID возвращает описание секции по идентификатору.
func xraySpecByID(id string) (XraySpec, bool) {
	for _, s := range XraySpecs {
		if s.ID == id {
			return s, true
		}
	}
	return XraySpec{}, false
}
