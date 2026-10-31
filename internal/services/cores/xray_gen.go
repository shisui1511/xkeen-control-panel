package cores

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Идентификатор генератора секций Xray в реестре слоя.
const xrayGenID = "cores_xray"

// xrayIndent — отступ файлов панели (как у остальных генераторов Xray).
const xrayIndent = "  "

// isEmptyValue — значения нет: пусто или null.
func isEmptyValue(v json.RawMessage) bool {
	t := bytes.TrimSpace(v)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// wrapXrayKey собирает файл {"<ключ>": значение}: значение — сырые байты,
// порядок вложенных ключей не меняется; отступ два пробела, перевод строки в
// конце.
func wrapXrayKey(key string, value json.RawMessage) ([]byte, error) {
	o := configlayer.NewOrderedObject()
	o.Set(key, value)
	return indentXray(o)
}

// indentXray сериализует объект файла панели.
func indentXray(o *configlayer.OrderedObject) ([]byte, error) {
	b, err := o.MarshalIndent(xrayIndent)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// policyWithCounters дописывает в policy.system четыре закреплённых счётчика
// (D-10): остальные ключи system и levels, а также порядок ключей сохраняются.
func policyWithCounters(value json.RawMessage) (json.RawMessage, error) {
	policy, err := configlayer.ParseOrderedObject(value)
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	system := configlayer.NewOrderedObject()
	if raw, ok := policy.Get("system"); ok && !isEmptyValue(raw) {
		if system, err = configlayer.ParseOrderedObject(raw); err != nil {
			return nil, fmt.Errorf("policy.system: %w", err)
		}
	}
	for _, c := range xrayStatsCounters {
		system.Set(c, json.RawMessage("true"))
	}
	sys, err := system.MarshalJSON()
	if err != nil {
		return nil, err
	}
	policy.Set("system", sys)
	return policy.MarshalJSON()
}

// XraySectionFile возвращает содержимое файла панели секции id из её значения.
// Пустое значение заменяется пресетом секции. Для log, dns, policy файл —
// {"<ключ>": значение}, для inbounds — {"inbounds": [...]}, для «Прочих
// ключей» — сам объект значения; ok=false — файла нет (пустые «Прочие ключи»).
// В policy дописываются закреплённые счётчики статистики. Поля, которых форма не
// знает, и порядок ключей переносятся как есть.
func XraySectionFile(id string, value json.RawMessage) (content []byte, ok bool, err error) {
	spec, known := xraySpecByID(id)
	if !known {
		return nil, false, fmt.Errorf("секция Xray %q неизвестна", id)
	}
	if isEmptyValue(value) {
		value = XrayPreset(id)
	}
	switch id {
	case "log", "dns":
		content, err = wrapXrayKey(spec.Key, value)
	case "policy":
		if value, err = policyWithCounters(value); err == nil {
			content, err = wrapXrayKey(spec.Key, value)
		}
	case "inbounds":
		if _, isList := xrayInbounds(value); !isList {
			return nil, false, fmt.Errorf("секция Xray %q: ожидался массив объектов", id)
		}
		content, err = wrapXrayKey(spec.Key, value)
	case "other":
		var obj *configlayer.OrderedObject
		if obj, err = configlayer.ParseOrderedObject(value); err != nil {
			return nil, false, fmt.Errorf("секция Xray %q: %w", id, err)
		}
		if obj.Len() == 0 {
			return nil, false, nil
		}
		content, err = indentXray(obj)
	}
	if err != nil {
		return nil, false, fmt.Errorf("секция Xray %q: %w", id, err)
	}
	return content, true, nil
}

// xrayGenerator — генератор файлов секций Xray (cores_xray).
type xrayGenerator struct{}

// XrayGenerator возвращает генератор файлов секций Xray под управлением панели.
func XrayGenerator() configlayer.Generator { return xrayGenerator{} }

// ID — идентификатор генератора в реестре.
func (xrayGenerator) ID() string { return xrayGenID }

// Generate выдаёт файл панели для каждой секции Xray с managed=true. Xray не
// установлен — файлов нет. Служебные поля секции черновика (managed, from, keep,
// …) в файл не попадают: берётся только Value. Protocol, network, followRedirect
// и теги входящих генератор не меняет — только переносит значение.
func (xrayGenerator) Generate(in configlayer.Input, installed configlayer.InstalledKernels) ([]configlayer.GeneratedFile, error) {
	if !installed.Xray {
		return nil, nil
	}
	cores := configlayer.DesiredCores(in.Sections)
	var out []configlayer.GeneratedFile
	for _, spec := range XraySpecs {
		cs := cores[spec.Section]
		if !cs.Managed {
			continue
		}
		content, ok, err := XraySectionFile(spec.ID, cs.Value)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, configlayer.GeneratedFile{
			Kernel:  configlayer.KernelXray,
			RelPath: spec.File,
			Kind:    configlayer.KindXrayJSON,
			Content: content,
		})
	}
	return out, nil
}
