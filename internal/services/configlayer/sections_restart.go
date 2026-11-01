package configlayer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Объявление способа перезапуска по изменениям секций ядер (D-16, SEC-02).
// Один расчёт служит и снимку слоя (полоса черновика показывает объявления до
// «Применить»), и плану запуска (признак полного перезапуска Mihomo).

// Причины перезапуска: смена порта прозрачного входящего и смена tun.
const (
	RestartReasonPort = "port"
	RestartReasonTun  = "tun"
)

// RestartBase — сторона перехода владения, которую RestartReasons по двум
// наборам секций увидеть не может: при отпускании это то, что вернётся на диск
// (прежний профиль Mihomo), при взятии — то, что действует сейчас. Пустое
// значение — перехода нет, сравниваются только секции. Строится один раз на
// снимок и на план (restartBase), чтобы объявление и признак полного перезапуска
// считались одним расчётом.
type RestartBase struct {
	xray   *xrayTransition
	mihomo *mihomoTransition
}

// xrayTransition — переход владения «Входящими» Xray: входящие по ту сторону
// перехода. unread — сторону прочитать не удалось.
type xrayTransition struct {
	// taking — взятие: сторона панели — черновик; иначе отпускание: сторона
	// панели — применённое состояние.
	taking bool
	// other — Effective секции inbounds по ту сторону (nil — входящих там нет).
	other  json.RawMessage
	unread bool
}

// mihomoTransition — переход владения маршрутизацией Mihomo: факты профиля до и
// после него. unread — одну из сторон прочитать не удалось.
type mihomoTransition struct {
	before, after mihomoRestartFacts
	unread        bool
}

// RestartReasons возвращает объявления перезапуска по разнице между
// применённым состоянием и черновиком. Секции, которые управляются панелью и
// там, и там, сравниваются между собой: правка секции без смены порта
// прозрачного входящего и tun ничего не объявляет. На переходе владения
// (base) сравнивается значение по ту сторону перехода: если порт или tun
// действительно меняются, объявляется перезапуск; сторона не прочиталась —
// port. Порядок — xray, затем mihomo, не больше одной записи на ядро; у Mihomo
// tun важнее port. Пустой результат — пустой срез (не nil).
func RestartReasons(applied, draft Sections, base RestartBase) []RestartNote {
	out := []RestartNote{}
	appliedCores, draftCores := DesiredCores(applied), DesiredCores(draft)

	var xrayPort bool
	if tr := base.xray; tr != nil {
		panel := appliedCores[SectionXrayInbounds]
		if tr.taking {
			panel = draftCores[SectionXrayInbounds]
		}
		xrayPort = xrayTransitionPortChanged(panel, tr)
	} else {
		xrayPort = xrayInboundsPortChanged(appliedCores[SectionXrayInbounds], draftCores[SectionXrayInbounds])
	}
	if xrayPort {
		out = append(out, RestartNote{Kernel: KernelXray, Reason: RestartReasonPort})
	}
	reason := ""
	switch tr := base.mihomo; {
	case tr == nil:
		reason = mihomoRestartReason(appliedCores, draftCores)
	case tr.unread:
		reason = RestartReasonPort
	default:
		reason = mihomoFactsReason(tr.before, tr.after)
	}
	if reason != "" {
		out = append(out, RestartNote{Kernel: KernelMihomo, Reason: reason})
	}
	return out
}

// restartBase строит сторону перехода владения для снимка слоя и плана запуска.
// st — состояние слоя (фактическое владение), draft — желаемое. Диск читается
// только при переходе; любая ошибка чтения даёт «не прочиталось», а не отказ.
func restartBase(roots Roots, st State, draft Sections, installed InstalledKernels, foreignOwned func(kernel, rel string) bool) RestartBase {
	var base RestartBase
	if installed.Xray {
		base.xray = xrayInboundsTransition(roots, st, draft, foreignOwned)
	}
	if installed.Mihomo {
		base.mihomo = mihomoRoutingTransition(roots, st, draft)
	}
	return base
}

// xrayInboundsTransition считает переход владения «Входящими» Xray (D-01, D-05,
// D-28). Взятие — секция управляется в черновике, записи в State.Cores нет:
// другая сторона — Effective секции по configs/. Отпускание — секция не
// управляется, запись есть: другая сторона — configs/ плюс копии из резервного
// набора, то есть то, что вернётся на диск. nil — перехода нет, либо объявлять
// нечего (значение секции панели пусто — будет пресет), и диск не читается.
func xrayInboundsTransition(roots Roots, st State, draft Sections, foreignOwned func(kernel, rel string) bool) *xrayTransition {
	if roots.Xray == "" {
		return nil
	}
	entry := st.Cores.Xray[xraySectionInbounds]
	cs := DesiredCores(draft)[SectionXrayInbounds]
	var panel CoreSection
	tr := &xrayTransition{}
	switch {
	case cs.Managed && entry == nil:
		panel, tr.taking = cs, true
	case !cs.Managed && entry != nil:
		panel = DesiredCores(st.Applied)[SectionXrayInbounds]
	default:
		return nil
	}
	if _, ok := inboundPorts(panel.Value); !panel.Managed || !ok {
		return nil
	}
	files, err := ScanXrayKeyFiles(roots.Xray, foreignOwned)
	if err != nil {
		tr.unread = true
		return tr
	}
	if !tr.taking {
		have := make(map[string]bool, len(files))
		for _, f := range files {
			have[f.Name] = true
		}
		for _, mf := range entry.Moved {
			if !safeRoutingName(mf.Name) || have[mf.Name] {
				tr.unread = true
				return tr
			}
			data, err := readLimited(movedCopyPath(mf), maxXrayKeyFileBytes)
			if err != nil || HashContent(data) != mf.Hash {
				tr.unread = true
				return tr
			}
			f := parseXrayKeyFile(mf.Name, data)
			if f.Broken {
				tr.unread = true
				return tr
			}
			files = append(files, f)
		}
	}
	tr.other = ScanXraySections(files)[xraySectionInbounds].Effective
	return tr
}

// xrayTransitionPortChanged — на переходе владения порт читаемого XKeen входящего
// у панели и по ту сторону разный. Читаемые входящие — Keep секции панели и
// прозрачные входящие обеих сторон; тег есть только с одной стороны — порт
// изменился. Значение секции панели пусто — объявления нет; сторона не
// прочиталась — port.
func xrayTransitionPortChanged(panel CoreSection, tr *xrayTransition) bool {
	if !panel.Managed {
		return false
	}
	mine, ok := inboundPorts(panel.Value)
	if !ok {
		return false
	}
	if tr.unread {
		return true
	}
	theirs := map[string]inboundInfo{}
	if len(bytes.TrimSpace(tr.other)) > 0 {
		if theirs, ok = inboundPorts(tr.other); !ok {
			return true
		}
	}
	tags := map[string]bool{}
	for _, tag := range panel.Keep {
		tags[tag] = true
	}
	for _, side := range []map[string]inboundInfo{mine, theirs} {
		for tag, in := range side {
			if _, transparent := IsTransparentInbound(in.raw); transparent {
				tags[tag] = true
			}
		}
	}
	for tag := range tags {
		a, hasA := mine[tag]
		b, hasB := theirs[tag]
		if hasA != hasB || a.port != b.port {
			return true
		}
	}
	return false
}

// mihomoRoutingTransition считает переход владения маршрутизацией Mihomo.
// Взятие: до — активный профиль, после — секции черновика. Отпускание: до —
// действующий профиль панели, после — прежний профиль, к которому вернётся
// config.yaml. nil — перехода для расчёта нет.
func mihomoRoutingTransition(roots Roots, st State, draft Sections) *mihomoTransition {
	prev := st.Routing.Mihomo
	managed := DesiredRouting(draft).Managed
	switch {
	case managed && prev == nil:
		return mihomoTakeTransition(roots, draft)
	case !managed && prev != nil:
		return mihomoReleaseTransition(roots, prev)
	}
	return nil
}

// mihomoTakeTransition — взятие маршрутизации: активный профиль против секций
// черновика. Секции без значения (будет пресет) и отсутствие профиля перехода для
// расчёта не дают.
func mihomoTakeTransition(roots Roots, draft Sections) *mihomoTransition {
	after, ok := mihomoFactsFromSections(DesiredCores(draft))
	if !ok {
		return nil
	}
	_, data, err := ReadMihomoActive(roots.Mihomo)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	tr := &mihomoTransition{after: after}
	if err != nil {
		tr.unread = true
		return tr
	}
	before, ok := mihomoFactsFromYAML(data)
	if !ok {
		tr.unread = true
		return tr
	}
	tr.before = before
	return tr
}

// mihomoReleaseTransition — отпускание маршрутизации: профиль панели против
// прежнего профиля по виду прежнего config.yaml.
func mihomoReleaseTransition(roots Roots, prev *MihomoRoutingState) *mihomoTransition {
	var data []byte
	var err error
	switch prev.PrevKind {
	case configKindSymlink:
		link := prev.PrevLink
		if link == "" {
			link = filepath.Join(roots.Mihomo, filepath.FromSlash(prev.PrevProfile))
		}
		_, data, err = readMihomoLink(roots.Mihomo, link)
	case configKindFile:
		data, err = readLimited(filepath.Join(prev.PrevBackup, KernelMihomo, "config.yaml"), maxXrayKeyFileBytes)
	default:
		return nil
	}
	tr := &mihomoTransition{}
	if err != nil {
		tr.unread = true
		return tr
	}
	after, ok := mihomoFactsFromYAML(data)
	if !ok {
		tr.unread = true
		return tr
	}
	_, current, err := ReadMihomoActive(roots.Mihomo)
	if err != nil {
		tr.unread = true
		return tr
	}
	before, ok := mihomoFactsFromYAML(current)
	if !ok {
		tr.unread = true
		return tr
	}
	tr.before, tr.after = before, after
	return tr
}

// mihomoRestartFacts — то, от чего зависит способ перезапуска Mihomo, в
// каноническом виде: значение, разобранное в any и записанное json.Marshal (ключи
// отображений сортируются). Так YAML профиля и JSON секций сравниваются без учёта
// порядка ключей и вида числа.
type mihomoRestartFacts struct {
	tun       string            // значение ключа tun; ключа нет — "null"
	redir     string            // redir-port
	tproxy    string            // tproxy-port
	listeners map[string]string // listeners типа redir и tproxy: имя → порт
}

// mihomoFactsFromYAML достаёт факты из профиля Mihomo. Ошибка разбора, корень не
// отображение или значение, которое не записывается в JSON, — false.
func mihomoFactsFromYAML(data []byte) (mihomoRestartFacts, bool) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil || doc == nil {
		return mihomoRestartFacts{}, false
	}
	return mihomoFactsFromMap(doc)
}

// mihomoFactsFromMap достаёт факты из корня профиля, разобранного в отображение.
func mihomoFactsFromMap(doc map[string]any) (mihomoRestartFacts, bool) {
	f := mihomoRestartFacts{listeners: map[string]string{}}
	var ok bool
	if f.tun, ok = canonicalJSON(doc["tun"]); !ok {
		return mihomoRestartFacts{}, false
	}
	if f.redir, ok = canonicalJSON(doc["redir-port"]); !ok {
		return mihomoRestartFacts{}, false
	}
	if f.tproxy, ok = canonicalJSON(doc["tproxy-port"]); !ok {
		return mihomoRestartFacts{}, false
	}
	items, _ := doc["listeners"].([]any)
	for _, it := range items {
		l, _ := it.(map[string]any)
		name, _ := l["name"].(string)
		kind, _ := l["type"].(string)
		if name == "" || (kind != "redir" && kind != "tproxy") {
			continue
		}
		if f.listeners[name], ok = canonicalJSON(l["port"]); !ok {
			return mihomoRestartFacts{}, false
		}
	}
	return f, true
}

// mihomoFactsFromSections достаёт факты из секций Mihomo черновика: redir-port и
// tproxy-port — из «Основного», tun и listeners — из «Входящих/TUN». false — одна
// из секций не управляется панелью, пуста (будет пресет) или не разбирается.
func mihomoFactsFromSections(cores map[string]CoreSection) (mihomoRestartFacts, bool) {
	main, inb := cores[SectionMihomoMain], cores[SectionMihomoInbounds]
	if !main.Managed || !inb.Managed {
		return mihomoRestartFacts{}, false
	}
	mainDoc, inbDoc := sectionDoc(main.Value), sectionDoc(inb.Value)
	if mainDoc == nil || inbDoc == nil {
		return mihomoRestartFacts{}, false
	}
	doc := map[string]any{}
	for _, key := range []string{"redir-port", "tproxy-port"} {
		if v, has := mainDoc[key]; has {
			doc[key] = v
		}
	}
	for _, key := range []string{"tun", "listeners"} {
		if v, has := inbDoc[key]; has {
			doc[key] = v
		}
	}
	return mihomoFactsFromMap(doc)
}

// sectionDoc разбирает значение секции Mihomo в отображение; пустое значение и
// не объект — nil.
func sectionDoc(value json.RawMessage) map[string]any {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var doc map[string]any
	if json.Unmarshal(trimmed, &doc) != nil {
		return nil
	}
	return doc
}

// canonicalJSON — каноническая запись значения; nil (ключа нет) — "null".
func canonicalJSON(v any) (string, bool) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// mihomoFactsReason сравнивает факты до и после: tun важнее port; пустая
// строка — причины нет.
func mihomoFactsReason(before, after mihomoRestartFacts) string {
	if before.tun != after.tun {
		return RestartReasonTun
	}
	if before.redir != after.redir || before.tproxy != after.tproxy || len(before.listeners) != len(after.listeners) {
		return RestartReasonPort
	}
	for name, port := range before.listeners {
		if p, ok := after.listeners[name]; !ok || p != port {
			return RestartReasonPort
		}
	}
	return ""
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
