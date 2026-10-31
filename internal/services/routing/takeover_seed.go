package routing

import (
	"encoding/json"
	"fmt"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
)

// Перенос настроек прежней конфигурации при явном «Управлять в панели»
// (Service.Take): ключи активного профиля Mihomo уходят в секции mihomo_*
// (D-06, MIHO-01). Всё пишется в черновик той же правкой, что и желание взять
// маршрутизацию. Автовзятие конвейера (установка ядра, включение слоя) идёт мимо
// сервиса и ничего не переносит: профиль без секций собирается из пресетов.

// mihomoDraftManaged сообщает, что в черновике есть хотя бы одна секция Mihomo
// под управлением.
func mihomoDraftManaged(draft configlayer.Sections) bool {
	desired := configlayer.DesiredCores(draft)
	for _, id := range configlayer.MihomoSectionIDs {
		if desired[configlayer.CoreSectionName(configlayer.KernelMihomo, id)].Managed {
			return true
		}
	}
	return false
}

// seedMihomoSections раскладывает активный профиль Mihomo по секциям mihomo_*:
// значения как в профиле, from — имя и md5 прежнего профиля. Нет профиля
// (config.yaml не читается) — секции из встроенной базы с preset и без from.
// Нечего делать, если Mihomo не установлен, маршрутизация Mihomo уже взята или
// в черновике уже есть секция Mihomo под управлением. Результат: имя секции
// черновика → её значение.
func (s *Service) seedMihomoSections(st configlayer.StateView) (map[string]json.RawMessage, error) {
	if !s.d.Installed().Mihomo {
		return nil, nil
	}
	if m := s.d.Layer.Snapshot().Routing.Mihomo; m != nil && m.Taken {
		return nil, nil
	}
	if mihomoDraftManaged(st.Draft) {
		return nil, nil
	}
	// Основа секций: с прежним профилем — from с его именем и md5; без него —
	// значения встроенной базы с пометкой preset и без from.
	base := configlayer.CoreSection{V: 1, Managed: true, Preset: true}
	var profile []byte
	if name, data, hash, err := s.d.Layer.MihomoActiveProfile(); err == nil {
		profile = data
		base = configlayer.CoreSection{V: 1, Managed: true, From: []configlayer.SourceFile{{Name: name, Hash: hash}}}
	}
	values, err := cores.SeedMihomo(profile, configlayer.MihomoPorts{})
	if err != nil {
		// Профиль есть, но не разбирается: взять его молча без ключей нельзя.
		return nil, fmt.Errorf("routing: перенос профиля Mihomo: %w", err)
	}
	out := make(map[string]json.RawMessage, len(configlayer.MihomoSectionIDs))
	for _, id := range configlayer.MihomoSectionIDs {
		cs := base
		cs.Value = values[id]
		raw, err := configlayer.EncodeCoreSection(cs)
		if err != nil {
			return nil, err
		}
		out[configlayer.CoreSectionName(configlayer.KernelMihomo, id)] = raw
	}
	return out, nil
}

// releaseMihomoSections снимает секции Mihomo, которые сейчас под управлением
// (D-05): после «Применить» config.yaml возвращается к прежнему профилю, правки
// панели в секциях не остаются. Секции, которых нет или которые не под
// управлением, черновик не получает.
func releaseMihomoSections(draft configlayer.Sections) (map[string]json.RawMessage, error) {
	desired := configlayer.DesiredCores(draft)
	out := map[string]json.RawMessage{}
	for _, id := range configlayer.MihomoSectionIDs {
		name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
		if !desired[name].Managed {
			continue
		}
		raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{V: 1, Managed: false})
		if err != nil {
			return nil, err
		}
		out[name] = raw
	}
	return out, nil
}
