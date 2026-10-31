package routing

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/cores"
)

// Перенос настроек прежней конфигурации при явном «Управлять в панели»
// (Service.Take): ключи активного профиля Mihomo уходят в секции mihomo_*
// (D-06, MIHO-01), поля чужого burstObservatory — в настройки проб (D-27,
// XCFG-03). Всё пишется в черновик той же правкой, что и желание взять
// маршрутизацию. Автовзятие конвейера (установка ядра, включение слоя) идёт мимо
// сервиса и ничего не переносит: профиль без секций собирается из пресетов.

// takeSeeds собирает секции, которые взятие маршрутизации переносит из прежней
// конфигурации: секции Mihomo и пробы. Его зовут оба пути взятия: Take и
// TakeWithPreset.
func (s *Service) takeSeeds(st configlayer.StateView) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, seed := range []func(configlayer.StateView) (map[string]json.RawMessage, error){
		s.seedMihomoSections, s.seedProbeFromForeign,
	} {
		seeded, err := seed(st)
		if err != nil {
			return nil, err
		}
		for name, v := range seeded {
			out[name] = v
		}
	}
	return out, nil
}

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

// seedProbeFromForeign заполняет настройки проб из чужого burstObservatory
// (D-27, XCFG-03). Только если Xray установлен, его маршрутизация ещё не взята и
// секция probe в черновике не задана или равна значению по умолчанию: правки
// пользователя не перезаписываются. Чужого burstObservatory нет — ничего.
func (s *Service) seedProbeFromForeign(st configlayer.StateView) (map[string]json.RawMessage, error) {
	if !s.d.Installed().Xray {
		return nil, nil
	}
	if x := s.d.Layer.Snapshot().Routing.Xray; x != nil && x.Taken {
		return nil, nil
	}
	def := defaultProbe(s.d.GOARCH)
	cur, err := parseProbe(st.Draft[ProbeSection], s.d.GOARCH)
	if err != nil || !reflect.DeepEqual(cur, def) {
		return nil, nil
	}
	_, files, err := s.d.Layer.ScanXraySections()
	if err != nil {
		// Сбой скана сообщит «Применить»; перенос проб из-за него не блокируется.
		return nil, nil
	}
	burst := configlayer.XrayFacts(files).BurstObservatory
	if burst == nil {
		return nil, nil
	}
	probe, ok := probeFromBurst(burst, def)
	if !ok || reflect.DeepEqual(probe, def) {
		return nil, nil
	}
	raw, err := encodeProbe(probe)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{ProbeSection: raw}, nil
}

// secondsCeil переводит длительность в целые секунды, округляя вверх.
func secondsCeil(d time.Duration) int {
	return int((d + time.Second - 1) / time.Second)
}

// durationSeconds читает длительность Xray ("30s", "1m") в секундах вверх;
// ok=false — значения нет, оно не строка или не длительность.
func durationSeconds(raw json.RawMessage, present bool) (int, bool) {
	if !present {
		return 0, false
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, false
	}
	d, err := time.ParseDuration(text)
	if err != nil || d <= 0 {
		return 0, false
	}
	return secondsCeil(d), true
}

// probeFromBurst переносит значение burstObservatory в настройки проб:
// pingConfig.destination → URL; interval и timeout → секунды вверх (интервал не
// меньше minProbeInterval; timeout вне [1, interval) и другие недопустимые
// значения — из def); connectivity; sampling (целое 1–100); остальные ключи
// pingConfig → Extra в исходном порядке. subjectSelector в пробы не входит: его
// строит генератор из групп. false — pingConfig в значении нет.
func probeFromBurst(raw json.RawMessage, def ProbeConfig) (ProbeConfig, bool) {
	obs, err := configlayer.ParseOrderedObject(raw)
	if err != nil {
		return def, false
	}
	pingRaw, has := obs.Get("pingConfig")
	if !has {
		return def, false
	}
	ping, err := configlayer.ParseOrderedObject(pingRaw)
	if err != nil {
		return def, false
	}
	out := def

	if v, ok := ping.Get("destination"); ok {
		var u string
		if json.Unmarshal(v, &u) == nil && validProbeURL(u) {
			out.URL = u
		}
	}
	rawInterval, hasInterval := ping.Get("interval")
	if sec, ok := durationSeconds(rawInterval, hasInterval); ok {
		if sec < minProbeInterval {
			sec = minProbeInterval
		}
		if sec <= maxProbeInterval {
			out.IntervalS = sec
		}
	}
	rawTimeout, hasTimeout := ping.Get("timeout")
	if sec, ok := durationSeconds(rawTimeout, hasTimeout); ok && sec >= 1 && sec < out.IntervalS {
		out.TimeoutS = sec
	}
	if out.TimeoutS < 1 || out.TimeoutS >= out.IntervalS {
		out.TimeoutS = def.TimeoutS
	}
	if v, ok := ping.Get("connectivity"); ok {
		var u string
		if json.Unmarshal(v, &u) == nil && validProbeURL(u) {
			out.Connectivity = u
		}
	}
	if v, ok := ping.Get("sampling"); ok {
		var n float64
		if json.Unmarshal(v, &n) == nil && n == float64(int(n)) && int(n) >= 1 && int(n) <= maxProbeSampling {
			out.Sampling = int(n)
		}
	}

	reserved := map[string]bool{}
	for _, k := range probeReservedKeys {
		reserved[k] = true
	}
	extra := configlayer.NewOrderedObject()
	for _, k := range ping.Keys() {
		if reserved[k] {
			continue
		}
		v, _ := ping.Get(k)
		extra.Set(k, v)
	}
	if extra.Len() > 0 {
		if b, err := extra.MarshalJSON(); err == nil && len(b) <= maxProbeExtraBytes {
			out.Extra = b
		}
	}
	out.V = sectionVersion
	if validateProbe(out) != nil {
		return def, false
	}
	return out, true
}
