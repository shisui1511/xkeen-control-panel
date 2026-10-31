package cores

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// xrayIndent — отступ файлов панели (как у остальных генераторов Xray).
const xrayIndent = "  "

// xrayDNSPreset — значение секции DNS без значения в черновике: как у Xray без
// секции DNS.
const xrayDNSPreset = `{"servers":["localhost"]}`

// xrayDefaultValue возвращает значение секции, когда в черновике его нет.
func xrayDefaultValue(id string) json.RawMessage {
	if id == "dns" {
		return json.RawMessage(xrayDNSPreset)
	}
	return nil
}

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

// XraySectionFile возвращает содержимое файла панели секции id из её значения.
// Пустое значение заменяется пресетом секции. ok=false — файла нет.
func XraySectionFile(id string, value json.RawMessage) (content []byte, ok bool, err error) {
	spec, known := xraySpecByID(id)
	if !known {
		return nil, false, fmt.Errorf("секция Xray %q неизвестна", id)
	}
	if isEmptyValue(value) {
		value = xrayDefaultValue(id)
	}
	switch id {
	case "dns":
		content, err = wrapXrayKey(spec.Key, value)
	default:
		return nil, false, fmt.Errorf("секция Xray %q: файл не поддержан", id)
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
// …) в файл не попадают: берётся только Value.
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
