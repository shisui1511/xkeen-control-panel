package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
// конфигурации: секции Mihomo и пробы. Его зовут оба пути взятия: TakeScoped и
// TakeWithPresetScoped. Секции Mihomo переносятся, только если Mihomo после
// взятия под панелью; пробы Xray — если под панелью Xray и область не mihomo.
func (s *Service) takeSeeds(st configlayer.StateView, scope TakeScope, next configlayer.RoutingDesired) (map[string]json.RawMessage, error) {
	var seeds []func(configlayer.StateView) (map[string]json.RawMessage, error)
	if next.ManagedFor(configlayer.KernelMihomo) {
		seeds = append(seeds, s.seedMihomoSections)
	}
	if next.ManagedFor(configlayer.KernelXray) && scope != ScopeMihomo {
		seeds = append(seeds, s.seedProbeFromForeign)
	}
	out := map[string]json.RawMessage{}
	for _, seed := range seeds {
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
// Если маршрутизация Mihomo уже взята, ничего не переносится: секции, снятые
// только в черновике, возвращаются из применённого состояния. Нечего делать и
// когда Mihomo не установлен или в черновике уже есть секция Mihomo под
// управлением. Если после отпускания в черновике сохранены значения секций
// (D-09), они возвращаются как есть, а ключи активного профиля не переносятся:
// перенос D-06 идёт только при первом взятии. Результат: имя секции черновика →
// её значение.
func (s *Service) seedMihomoSections(st configlayer.StateView) (map[string]json.RawMessage, error) {
	if !s.d.Installed().Mihomo {
		return nil, nil
	}
	if m := s.d.Layer.Snapshot().Routing.Mihomo; m != nil && m.Taken {
		// Отпускание не применено: «Управлять» отменяет его и возвращает прежние
		// значения панели из применённого состояния, а не встроенную базу.
		return restoreAppliedMihomoSections(st)
	}
	if mihomoDraftManaged(st.Draft) {
		return nil, nil
	}
	if configlayer.MihomoSectionsSaved(st.Draft) {
		return restoreSavedMihomoSections(st.Draft)
	}
	// Основа секций: с прежним профилем — from с его именем и md5; без него —
	// значения встроенной базы с пометкой preset и без from.
	base := configlayer.CoreSection{V: 1, Managed: true, Preset: true}
	var profile []byte
	name, data, hash, err := s.d.Layer.MihomoActiveProfile()
	switch {
	case err == nil:
		profile = data
		base = configlayer.CoreSection{V: 1, Managed: true, From: []configlayer.SourceFile{{Name: name, Hash: hash}}}
	case errors.Is(err, fs.ErrNotExist):
		// Профиля нет (config.yaml не существует) — секции из встроенной базы.
	default:
		// Профиль есть, но не читается (симлинк вне корня, нет прав): молча взять
		// базу нельзя, «Применить» заменил бы профиль пользователя без сверки md5.
		return nil, fmt.Errorf("routing: чтение профиля Mihomo: %w", err)
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

// restoreAppliedMihomoSections возвращает в черновик секции Mihomo, которые в
// применённом состоянии под управлением, а в черновике сняты (отпускание ещё не
// применено). Без этого повторное взятие оставило бы секции неуправляемыми, и
// профиль панели после «Применить» собрался бы из встроенной базы, потеряв
// hosts, ntp, sub-rules и прочие ключи секций.
func restoreAppliedMihomoSections(st configlayer.StateView) (map[string]json.RawMessage, error) {
	applied := configlayer.DesiredCores(st.Applied)
	draft := configlayer.DesiredCores(st.Draft)
	out := map[string]json.RawMessage{}
	for _, id := range configlayer.MihomoSectionIDs {
		name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
		ac := applied[name]
		if !ac.Managed || draft[name].Managed {
			continue
		}
		raw, err := configlayer.EncodeCoreSection(ac)
		if err != nil {
			return nil, err
		}
		out[name] = raw
	}
	return out, nil
}

// restoreSavedMihomoSections возвращает под управление секции Mihomo, значения
// которых сохранены отпусканием (Managed=false и непустой Value, D-09): From пуст,
// сверять значения панели с чужим файлом не с чем. Остальные секции черновика не
// трогаются.
func restoreSavedMihomoSections(draft configlayer.Sections) (map[string]json.RawMessage, error) {
	desired := configlayer.DesiredCores(draft)
	out := map[string]json.RawMessage{}
	for _, id := range configlayer.MihomoSectionIDs {
		name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
		cs := desired[name]
		if cs.Managed || len(bytes.TrimSpace(cs.Value)) == 0 {
			continue
		}
		cs.V = 1
		cs.Managed = true
		cs.From = nil
		raw, err := configlayer.EncodeCoreSection(cs)
		if err != nil {
			return nil, err
		}
		out[name] = raw
	}
	return out, nil
}

// releaseMihomoSections снимает управление секциями Mihomo, которые сейчас под
// управлением (D-05, D-09): после «Применить» config.yaml возвращается к прежнему
// профилю, а значения секций остаются в черновике и возвращаются повторным
// взятием. From не сохраняется: значения теперь панельные. Секции, которых нет
// или которые не под управлением, черновик не получает.
func releaseMihomoSections(draft configlayer.Sections) (map[string]json.RawMessage, error) {
	desired := configlayer.DesiredCores(draft)
	out := map[string]json.RawMessage{}
	for _, id := range configlayer.MihomoSectionIDs {
		name := configlayer.CoreSectionName(configlayer.KernelMihomo, id)
		cs := desired[name]
		if !cs.Managed {
			continue
		}
		raw, err := configlayer.EncodeCoreSection(configlayer.CoreSection{
			V: 1, Managed: false, Value: cs.Value, Preset: cs.Preset, ForceProxyGroup: cs.ForceProxyGroup,
		})
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
