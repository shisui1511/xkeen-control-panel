package routing

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// LiveKeyGeodata — ключ State.Live с применёнными сигнатурами dat-файлов, на
// которые ссылаются правила: {"<файл>": "<размер>:<mtime>"}. Сигнатуры пишет
// наблюдатель dat (D-22); по ним он узнаёт, что XKeen обновил базы.
const LiveKeyGeodata = "geodata"

// Параметры наблюдателя dat.
const (
	// noticeListMissingPrefix — префикс уведомлений о пропавшем списке
	// (routing_list_missing:<файл>:<тег>, D-21).
	noticeListMissingPrefix = "routing_list_missing:"
	noticeListMissingKind   = "routing_list_missing"
	// watchRetryMin и watchRetryMax — задержки повторов при занятом применении и
	// сбое связи с Mihomo: 30, 60, 120 … до 600 с (Pitfall 6).
	watchRetryMin = 30 * time.Second
	watchRetryMax = 600 * time.Second
	// watchNoteAfter — сколько раз подряд запуск может закончиться отказом
	// перезапуска, прежде чем наблюдатель сообщит о нём: несовпадение числа
	// правил сразу после `xkeen -ug` (Mihomo поднялся на старых файлах) — не отказ
	// продукта, следующий цикл повторит (Pitfall 7).
	watchNoteAfter = 3
)

// datRef — список dat, на который ссылается включённое применённое правило.
// mihomo — правило пишется в профиль Mihomo (по нему строится провайдер).
type datRef struct {
	file, tag string
	mihomo    bool
}

func (r datRef) key() string { return listKey(r.file, r.tag) }

// appliedRefs собирает ссылки на списки включённых правил без «своих правил
// ядра» (в них списков нет), без повторов.
func appliedRefs(items []Rule) []datRef {
	var out []datRef
	idx := map[string]int{}
	for _, r := range items {
		if !r.Enabled || r.Raw != nil {
			continue
		}
		for _, c := range r.Conds {
			if c.Type != CondGeosite && c.Type != CondGeoip {
				continue
			}
			for _, l := range c.Lists {
				ref := datRef{file: l.File, tag: l.Tag, mihomo: mihomoApplicable(r)}
				if i, ok := idx[ref.key()]; ok {
					out[i].mihomo = out[i].mihomo || ref.mihomo
					continue
				}
				idx[ref.key()] = len(out)
				out = append(out, ref)
			}
		}
	}
	return out
}

// datWatcher — наблюдатель dat-файлов XKeen (D-21, D-22). Раз в Deps.WatchInterval
// сверяет размер и время изменения файлов, на которые ссылаются применённые
// правила, с Live[geodata] (только stat, файлы целиком не читаются). Файл
// изменился — проверяет, на месте ли теги (нет — уведомление
// routing_list_missing, записанные файлы не трогаются), и если содержимое списков
// Mihomo изменилось, пересобирает провайдеры в обход черновика: RunLive с
// триггером dat_refresh, точечное обновление провайдеров и сверка числа правил.
// Xray читает dat при старте, а `xkeen -ug` сам перезапускает ядро, поэтому для
// Xray пересборка не нужна. Состояние трогает только горутина run.
type datWatcher struct {
	s *Service

	first bool
	// hashes — хэши выгрузок категорий, записанные в файлы Mihomo последним
	// успешным запуском. Нет записи — состав неизвестен, запуск сам решит, есть ли
	// что писать (конвейер без изменений файлов ничего не пишет).
	hashes map[string]string
	// missing — пары файл|ТЕГ, о которых показано уведомление.
	missing map[string]bool
	// retryAt и delay — откат повторов запуска конвейера.
	retryAt time.Time
	delay   time.Duration
	// restartFails — подряд отказов перезапуска.
	restartFails int
	// draftSigs — сигнатуры файлов dat, на которые ссылаются правила черновика,
	// по прошлому проходу (пусто — файла нет). Нужны, чтобы сбросить мемо проверок
	// черновика, когда список появился или файл изменился: применённые правила
	// это не покрывает.
	draftSigs map[string]string
}

func newDatWatcher(s *Service) *datWatcher {
	return &datWatcher{s: s, first: true, hashes: map[string]string{}, missing: map[string]bool{}, draftSigs: map[string]string{}}
}

// startDatWatcher запускает наблюдателя под учётом Stop сервиса.
func (s *Service) startDatWatcher() {
	w := newDatWatcher(s)
	s.Go(func() { w.run(s.ctx) })
}

// run — цикл: первый проход сразу (уведомления восстанавливаются после перезапуска
// панели), затем по тикеру.
func (w *datWatcher) run(ctx context.Context) {
	t := time.NewTicker(w.s.d.WatchInterval)
	defer t.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		w.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// readGeodata читает Live[geodata]; порча данных — как отсутствие.
func readGeodata(live configlayer.Sections) map[string]string {
	out := map[string]string{}
	raw, ok := live[LiveKeyGeodata]
	if !ok || json.Unmarshal(raw, &out) != nil || out == nil {
		return map[string]string{}
	}
	return out
}

// tick — один проход наблюдателя.
func (w *datWatcher) tick(ctx context.Context) {
	s := w.s
	if s.d.Lists == nil {
		return
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	w.watchDraft(st)
	var refs []datRef
	if configlayer.DesiredRouting(st.Applied).Managed {
		rules, err := parseRules(st.Applied[RulesSection])
		if err != nil {
			return
		}
		refs = appliedRefs(rules.Items)
	}
	if len(refs) == 0 {
		w.setMissing(nil)
		w.first = false
		return
	}

	var files []string
	seenFile := map[string]bool{}
	for _, r := range refs {
		if !seenFile[r.file] {
			seenFile[r.file] = true
			files = append(files, r.file)
		}
	}
	sort.Strings(files)
	sigs := s.d.Lists.Signature(files)
	live := readGeodata(st.Live)

	changed := map[string]bool{}
	gone := false
	learn := false
	for _, f := range files {
		sig, ok := sigs[f]
		switch old, had := live[f]; {
		case !ok:
			gone = true
		case !had:
			learn = true
		case old != sig:
			changed[f] = true
		}
	}
	if !w.first && len(changed) == 0 && !gone && !learn && len(w.missing) == 0 {
		return
	}
	w.first = false

	// Теги: нет файла или тега — список пропал.
	missing := map[string]datRef{}
	for _, r := range refs {
		if _, _, ok, err := s.d.Lists.Has(r.file, r.tag); err != nil || !ok {
			missing[r.key()] = r
		}
	}
	if w.setMissing(missing) || len(changed) > 0 {
		s.d.Layer.InvalidateChecks()
	}
	if len(missing) > 0 {
		// D-21: записанные файлы не трогаются и конвейер не запускается (профиль с
		// пропавшим списком Mihomo не соберёт); прежние сигнатуры остаются, и когда
		// список вернётся, изменение будет обнаружено заново.
		return
	}
	if len(changed) == 0 {
		if learn {
			w.recordLive(sigs, live)
		}
		return
	}
	if time.Now().Before(w.retryAt) {
		return
	}

	// Хэши выгрузок списков Mihomo из изменившихся файлов: запускать конвейер
	// стоит, только если содержимое изменилось (T-146-40: «дрожащий» mtime).
	installedMihomo := s.d.Installed().Mihomo
	hashChanged := false
	next := map[string]string{}
	if installedMihomo {
		for _, r := range refs {
			if !r.mihomo || !changed[r.file] {
				continue
			}
			cat, err := s.d.Lists.Extract(r.file, r.tag)
			if err != nil {
				// Слишком большой или нечитаемый список: пусть решает проверка
				// конвейера (list_too_large виден уведомлением build_failed).
				hashChanged = true
				continue
			}
			next[r.key()] = cat.Hash
			if w.hashes[r.key()] != cat.Hash {
				hashChanged = true
			}
		}
	}
	if !hashChanged {
		for k, h := range next {
			w.hashes[k] = h
		}
		w.recordLive(sigs, live)
		return
	}
	if w.runMihomo(ctx, sigs) {
		for k, h := range next {
			w.hashes[k] = h
		}
	}
}

// watchDraft следит за файлами dat правил черновика: список появился (был
// list_not_found) или файл изменился — мемо проверок сбрасывается, и «Применить»
// снимается или ставится по свежим данным. Уведомления и пересборка
// остаются за применёнными правилами.
func (w *datWatcher) watchDraft(st configlayer.StateView) {
	rules, err := parseRules(st.Draft[RulesSection])
	if err != nil {
		return
	}
	var files []string
	seen := map[string]bool{}
	for _, r := range appliedRefs(rules.Items) {
		if !seen[r.file] {
			seen[r.file] = true
			files = append(files, r.file)
		}
	}
	sort.Strings(files)
	sigs := w.s.d.Lists.Signature(files)
	next := make(map[string]string, len(files))
	changed := false
	for _, f := range files {
		sig := sigs[f]
		next[f] = sig
		if old, had := w.draftSigs[f]; had && old != sig {
			changed = true
		}
	}
	w.draftSigs = next
	if changed {
		w.s.d.Layer.InvalidateChecks()
	}
}

// setMissing заменяет набор пропавших списков и уведомления о них. Возвращает
// true, если набор изменился.
func (w *datWatcher) setMissing(missing map[string]datRef) bool {
	now := make(map[string]bool, len(missing))
	for k := range missing {
		now[k] = true
	}
	same := len(now) == len(w.missing)
	for k := range now {
		if !w.missing[k] {
			same = false
		}
	}
	list := make([]configlayer.NoticeView, 0, len(missing))
	for _, r := range missing {
		list = append(list, configlayer.NoticeView{
			ID:     noticeListMissingPrefix + r.file + ":" + r.tag,
			Kind:   noticeListMissingKind,
			Params: map[string]string{"file": r.file, "tag": r.tag},
		})
	}
	w.missing = now
	w.s.d.Layer.SetNotices(noticeListMissingPrefix, list)
	return !same
}

// recordLive записывает в Live[geodata] текущие сигнатуры, если они отличаются от
// записанных. Ревизия черновика не растёт.
func (w *datWatcher) recordLive(sigs, live map[string]string) {
	if sameStrings(sigs, live) {
		return
	}
	raw, err := json.Marshal(sigs)
	if err != nil {
		return
	}
	err = w.s.d.Layer.UpdateLive(func(l configlayer.Sections) error {
		l[LiveKeyGeodata] = raw
		return nil
	})
	if err != nil && !errors.Is(err, configlayer.ErrDisabled) {
		log.Printf("[routing] сигнатуры dat не записаны: %v", err)
	}
}

func sameStrings(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// runMihomo пересобирает файлы Mihomo в обход черновика. true — запуск прошёл,
// сигнатуры записаны. Занятое применение, нерешённый дрейф и сбой связи с
// Mihomo — повтор с нарастающей задержкой без уведомления: пользователь не
// видит «применение уже идёт» из-за фонового запуска (Pitfall 6).
func (w *datWatcher) runMihomo(ctx context.Context, sigs map[string]string) bool {
	l := w.s.d.Layer
	raw, err := json.Marshal(sigs)
	if err != nil {
		return false
	}
	view, err := l.RunLive(ctx, configlayer.LiveRun{
		Trigger: configlayer.TriggerDatRefresh,
		Kernel:  configlayer.KernelMihomo,
		Overlay: configlayer.Sections{LiveKeyGeodata: raw},
	})
	switch {
	case ctx.Err() != nil, errors.Is(err, configlayer.ErrStopped), errors.Is(err, configlayer.ErrDisabled):
		return false
	case err != nil:
		w.backOff()
		return false
	}
	res := view.Result
	switch {
	case res != nil && res.OK:
		w.delay, w.retryAt, w.restartFails = 0, time.Time{}, 0
		l.NoteBuildResult(configlayer.KernelMihomo, view)
		if err := l.UpdateLive(func(live configlayer.Sections) error {
			live[LiveKeyGeodata] = raw
			return nil
		}); err != nil && !errors.Is(err, configlayer.ErrDisabled) {
			log.Printf("[routing] сигнатуры dat не записаны: %v", err)
		}
		return true
	case res != nil && (res.Code == configlayer.ResultBuildFailed || res.Code == configlayer.ResultWriteFailed):
		// Неудача проверки или записи: прежние файлы возвращены, уведомление
		// build_failed:mihomo, повтор позже.
		l.NoteBuildResult(configlayer.KernelMihomo, view)
	case res != nil && res.Code == configlayer.ResultRestartFailed:
		w.restartFails++
		if w.restartFails >= watchNoteAfter {
			l.NoteBuildResult(configlayer.KernelMihomo, view)
		}
	}
	w.backOff()
	return false
}

// backOff откладывает следующий запуск: 30, 60, 120 … до 600 секунд.
func (w *datWatcher) backOff() {
	switch {
	case w.delay <= 0:
		w.delay = watchRetryMin
	default:
		w.delay = min(w.delay*2, watchRetryMax)
	}
	w.retryAt = time.Now().Add(w.delay)
}
