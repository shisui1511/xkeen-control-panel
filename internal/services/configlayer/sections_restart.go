package configlayer

import (
	"bytes"
	"encoding/json"
)

// Объявление способа перезапуска по изменениям секций ядер (D-16, SEC-02).
// Один расчёт служит и снимку слоя (полоса черновика показывает объявления до
// «Применить»), и плану запуска (признак полного перезапуска Mihomo).

// Причины перезапуска: смена порта прозрачного входящего и смена tun.
const (
	RestartReasonPort = "port"
	RestartReasonTun  = "tun"
)

// RestartReasons возвращает объявления перезапуска по разнице между
// применённым состоянием и черновиком. Сравниваются только секции, которые
// управляются панелью и там, и там: взятие, отпускание или правка секции без
// смены порта прозрачного входящего и tun ничего не объявляют. Порядок — xray,
// затем mihomo, не больше одной записи на ядро; у Mihomo tun важнее port. Пустой
// результат — пустой срез (не nil).
func RestartReasons(applied, draft Sections) []RestartNote {
	out := []RestartNote{}
	appliedCores, draftCores := DesiredCores(applied), DesiredCores(draft)

	if xrayInboundsPortChanged(appliedCores[SectionXrayInbounds], draftCores[SectionXrayInbounds]) {
		out = append(out, RestartNote{Kernel: KernelXray, Reason: RestartReasonPort})
	}
	if reason := mihomoRestartReason(appliedCores, draftCores); reason != "" {
		out = append(out, RestartNote{Kernel: KernelMihomo, Reason: reason})
	}
	return out
}

// fullRestartOf переводит объявления в карту «ядро → причина», которую несёт
// Plan.FullRestart. Пустые объявления дают nil.
func fullRestartOf(notes []RestartNote) map[string]string {
	if len(notes) == 0 {
		return nil
	}
	out := make(map[string]string, len(notes))
	for _, n := range notes {
		if n.Kernel != "" && n.Reason != "" {
			out[n.Kernel] = n.Reason
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// xrayInboundsPortChanged — порт хотя бы одного входящего, который читает XKeen,
// в черновике иной, чем в применённом. Читаемые входящие — теги из Keep
// применённой секции; без Keep — входящие, подходящие XKeen как прозрачные.
// Входящий, пропавший из черновика, считается изменившимся. Значение секции
// пусто (будет пресет) или не разбирается — объявления нет.
func xrayInboundsPortChanged(applied, draft CoreSection) bool {
	if !applied.Managed || !draft.Managed {
		return false
	}
	was, ok := inboundPorts(applied.Value)
	if !ok {
		return false
	}
	now, ok := inboundPorts(draft.Value)
	if !ok {
		return false
	}
	tags := applied.Keep
	if len(tags) == 0 {
		for tag, in := range was {
			if _, transparent := IsTransparentInbound(in.raw); transparent {
				tags = append(tags, tag)
			}
		}
	}
	for _, tag := range tags {
		before, had := was[tag]
		if !had {
			continue
		}
		after, has := now[tag]
		if !has || before.port != after.port {
			return true
		}
	}
	return false
}

// inboundInfo — тело входящего и его порт в сжатой записи JSON.
type inboundInfo struct {
	raw  json.RawMessage
	port string
}

// inboundPorts разбирает массив входящих Xray: тег → тело и порт. Пустое
// значение, не массив или входящее не объект — ok=false. Входящие без тега в
// карту не попадают.
func inboundPorts(value json.RawMessage) (map[string]inboundInfo, bool) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, false
	}
	out := make(map[string]inboundInfo, len(items))
	for _, it := range items {
		var in struct {
			Tag  string          `json:"tag"`
			Port json.RawMessage `json:"port"`
		}
		if json.Unmarshal(it, &in) != nil {
			return nil, false
		}
		if in.Tag == "" {
			continue
		}
		out[in.Tag] = inboundInfo{raw: it, port: compactJSON(in.Port)}
	}
	return out, true
}

// compactJSON возвращает сжатую запись значения; пустое и ошибочное значение —
// "null" (ключа нет).
func compactJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "null"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return string(trimmed)
	}
	return buf.String()
}

// mihomoRestartReason находит причину полного перезапуска Mihomo: tun в
// «Входящих/TUN» важнее порта; порт — redir-port или tproxy-port «Основного»,
// либо порт listener типа redir/tproxy. Пустая строка — перезапуск не нужен.
func mihomoRestartReason(applied, draft map[string]CoreSection) string {
	inbWas, inbNow := applied[SectionMihomoInbounds], draft[SectionMihomoInbounds]
	var wasObj, nowObj *OrderedObject
	if inbWas.Managed && inbNow.Managed {
		wasObj, nowObj = parseSectionObject(inbWas.Value), parseSectionObject(inbNow.Value)
	}
	if wasObj != nil && nowObj != nil && objectKey(wasObj, "tun") != objectKey(nowObj, "tun") {
		return RestartReasonTun
	}

	mainWas, mainNow := applied[SectionMihomoMain], draft[SectionMihomoMain]
	if mainWas.Managed && mainNow.Managed {
		w, n := parseSectionObject(mainWas.Value), parseSectionObject(mainNow.Value)
		if w != nil && n != nil {
			for _, key := range []string{"redir-port", "tproxy-port"} {
				if objectKey(w, key) != objectKey(n, key) {
					return RestartReasonPort
				}
			}
		}
	}

	if wasObj != nil && nowObj != nil {
		was, now := transparentListeners(wasObj), transparentListeners(nowObj)
		if len(was) != len(now) {
			return RestartReasonPort
		}
		for name, port := range was {
			if p, ok := now[name]; !ok || p != port {
				return RestartReasonPort
			}
		}
	}
	return ""
}

// parseSectionObject разбирает значение секции Mihomo как объект; пустое
// значение (будет пресет) и не объект — nil.
func parseSectionObject(value json.RawMessage) *OrderedObject {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	obj, err := ParseOrderedObject(trimmed)
	if err != nil {
		return nil
	}
	return obj
}

// objectKey возвращает сжатую запись значения ключа; ключа нет — "null".
func objectKey(obj *OrderedObject, key string) string {
	raw, ok := obj.Get(key)
	if !ok {
		return "null"
	}
	return compactJSON(raw)
}

// transparentListeners собирает listeners типа redir и tproxy: имя → порт в
// сжатой записи. Записи без имени и не объекты пропускаются.
func transparentListeners(obj *OrderedObject) map[string]string {
	out := map[string]string{}
	raw, ok := obj.Get("listeners")
	if !ok {
		return out
	}
	var items []struct {
		Name string          `json:"name"`
		Type string          `json:"type"`
		Port json.RawMessage `json:"port"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return out
	}
	for _, it := range items {
		if it.Name == "" || (it.Type != "redir" && it.Type != "tproxy") {
			continue
		}
		out[it.Name] = compactJSON(it.Port)
	}
	return out
}
