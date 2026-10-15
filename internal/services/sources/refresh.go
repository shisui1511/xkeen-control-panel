package sources

import (
	"context"
	"errors"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// ErrSourceDisabled — источник выключен: он не обновляется (D-17).
var ErrSourceDisabled = errors.New("sources: источник выключен")

// ErrSourceChanged — подписку изменили, пока шёл запрос, и после повтора с новой
// конфигурацией тоже: ответ относится к прежней конфигурации и отброшен.
var ErrSourceChanged = errors.New("sources: конфигурация подписки изменилась во время обновления")

// Итоги обновления источника (RefreshOutcome.Outcome).
const (
	OutcomeUnchanged      = "unchanged"
	OutcomeApplied        = "applied"
	OutcomeRejected       = "rejected"
	OutcomeNetwork        = "network"
	OutcomeDeferred       = "deferred"
	OutcomeFailed         = "failed"
	OutcomePendingUpdated = "pending_updated"
)

// maxReasonRunes — длина причины отказа ядра в состоянии и уведомлении.
const maxReasonRunes = 200

// KernelOutcome — итог обновления по одному ядру: статус после обновления, число
// рабочих узлов набора и код причины отказа.
type KernelOutcome struct {
	Status    string `json:"status"`
	Count     int    `json:"count"`
	ErrorCode string `json:"error_code,omitempty"`
}

// RefreshOutcome — итог обновления источника. Added и Removed — разница состава
// при применении (наибольшая среди обновлённых ядер: узлы ядер считаются из их
// собственных ответов, сумма удвоила бы общие узлы).
//
// Outcome applied или deferred относится к ядрам, которые новый состав приняли:
// если одно ядро его отвергло, конвейер повторяется без него, и отказ виден только
// в Kernels[ядро].Status = refused (у такого ядра действует прежний набор). failed —
// новый состав отвергли все ядра с кандидатами. Клиент проверяет Kernels, а не
// только Outcome.
type RefreshOutcome struct {
	Outcome string                   `json:"outcome"`
	Added   int                      `json:"added"`
	Removed int                      `json:"removed"`
	Kernels map[string]KernelOutcome `json:"kernels"`
	Error   *nodes.SourceError       `json:"error,omitempty"`
}

// refreshCall — идущее обновление источника: повторные вызовы ждут его итог.
type refreshCall struct {
	done chan struct{}
	out  RefreshOutcome
	err  error
}

// Refresh обновляет подписку: запрашивает провайдера под каждое установленное
// ядро без замков слоя, сравнивает хэш рабочего набора с применённым и запускает
// конвейер только при смене состава (FND-06). Одновременные вызовы по одному
// источнику сливаются в один запрос и получают один итог (D-08). Выключенный
// источник — ErrSourceDisabled, неизвестный — ErrSourceNotFound. «Обновить»
// пользователя и тик планировщика идут одинаково: срок и повторы решает
// планировщик по статусам.
//
// Общая работа идёт в контексте сервиса и учитывается Stop: оборванный HTTP-запрос
// инициатора не прерывает запросы к провайдеру и запись итога, а остальные
// ожидающие не получают чужую отмену. ctx ограничивает только ожидание итога.
// Остановленный сервис — configlayer.ErrStopped.
func (s *Service) Refresh(ctx context.Context, id string) (RefreshOutcome, error) {
	s.cmu.Lock()
	c, ok := s.calls[id]
	if !ok {
		c = &refreshCall{done: make(chan struct{})}
		s.calls[id] = c
		started := s.spawn(func() {
			out, err := s.refreshCurrent(s.ctx, id, nil)
			s.cmu.Lock()
			c.out, c.err = out, err
			delete(s.calls, id)
			if err == nil {
				// Удачное обновление (и ручное «Обновить») снимает откат планировщика:
				// следующий сбой начнёт задержки заново с 5 минут.
				delete(s.backoffs, id)
			}
			s.cmu.Unlock()
			close(c.done)
		})
		if !started {
			delete(s.calls, id)
			s.cmu.Unlock()
			return RefreshOutcome{}, configlayer.ErrStopped
		}
	}
	s.cmu.Unlock()
	select {
	case <-c.done:
		return c.out, c.err
	case <-ctx.Done():
		return RefreshOutcome{}, ctx.Err()
	}
}

// refreshCurrent — refreshWork с одним повтором, если подписку изменили, пока шёл
// запрос: ответ прежней конфигурации отброшен, и запрос идёт заново с новой. Так
// вызов, присоединившийся к идущему обновлению после смены адреса, получает итог
// по новому адресу, а подписка только в черновике (её не обновляет планировщик)
// получает набор без второго «Обновить». Повторная смена за время повтора —
// ErrSourceChanged.
func (s *Service) refreshCurrent(ctx context.Context, id string, only []string) (RefreshOutcome, error) {
	out, err := s.refreshWork(ctx, id, only)
	if errors.Is(err, ErrSourceChanged) && ctx.Err() == nil {
		out, err = s.refreshWork(ctx, id, only)
	}
	return out, err
}

// subscriptionConfig находит конфигурацию подписки: применённое состояние
// важнее черновика (конвейер обновления идёт из применённого). inApplied — секция
// есть в применённом состоянии.
func subscriptionConfig(st configlayer.StateView, id string) (cfg SubscriptionConfig, inApplied bool, err error) {
	if sid, ok := sourceID(sectionName(id)); !ok || sid == MySourceID {
		return cfg, false, ErrSourceNotFound
	}
	raw, ok := st.Applied[sectionName(id)]
	inApplied = ok
	if !ok {
		raw, ok = st.Draft[sectionName(id)]
	}
	if !ok || kindOf(raw) != KindSubscription {
		return cfg, false, ErrSourceNotFound
	}
	if cfg, err = parseSubscription(raw); err != nil {
		return cfg, inApplied, err
	}
	if !cfg.Enabled {
		return cfg, inApplied, ErrSourceDisabled
	}
	return cfg, inApplied, nil
}

// sameSubscription сверяет конфигурацию подписки в st с той, под которой шёл
// запрос: другой адрес, HWID или место (применена или только в черновике) —
// ErrSourceChanged; выключенная или удалённая подписка — ошибка subscriptionConfig.
func sameSubscription(st configlayer.StateView, id string, cfg SubscriptionConfig, inApplied bool) error {
	cur, curInApplied, err := subscriptionConfig(st, id)
	if err != nil {
		return err
	}
	if curInApplied != inApplied || cur.URL != cfg.URL || cur.HWID != cfg.HWID {
		return ErrSourceChanged
	}
	return nil
}

// subscriptionIDs — идентификаторы включённых подписок: применённых, а при
// appliedOnly=false ещё и тех, что есть только в черновике. Порядок стабильный.
func subscriptionIDs(st configlayer.StateView, appliedOnly bool) []string {
	seen := map[string]bool{}
	var ids []string
	groups := []configlayer.Sections{st.Applied}
	if !appliedOnly {
		groups = append(groups, st.Draft)
	}
	for _, secs := range groups {
		for name, raw := range secs {
			id, ok := sourceID(name)
			if !ok || id == MySourceID || seen[id] || kindOf(raw) != KindSubscription {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	// Применённая секция важнее: подписку, выключенную там, не обновляем, даже если
	// в черновике она включена.
	out := ids[:0]
	for _, id := range ids {
		if _, _, err := subscriptionConfig(st, id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// kernelFetch — результат запроса и его разбор по ядру.
type kernelFetch struct {
	kernel string
	oc     *kernelOutcome
	// unchanged — набор совпал с применённым; candidate — набор отличается и идёт в
	// конвейер (или в pending для подписки только в черновике).
	unchanged bool
	candidate bool
}

// refreshWork — обновление источника под замком очереди. only — ограничить ядра
// (сборка после установки ядра); nil — все установленные.
func (s *Service) refreshWork(ctx context.Context, id string, only []string) (RefreshOutcome, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	st, err := s.d.Layer.ReadState()
	if err != nil {
		return RefreshOutcome{}, err
	}
	cfg, inApplied, err := subscriptionConfig(st, id)
	if err != nil {
		return RefreshOutcome{}, err
	}
	live, err := readLive(st.Live)
	if err != nil {
		return RefreshOutcome{}, err
	}
	prev := live[id]

	installed := s.d.Installed()
	var kernels []string
	for _, k := range previewKernels {
		if !installed.Has(k) {
			continue
		}
		if only != nil && !containsString(only, k) {
			continue
		}
		kernels = append(kernels, k)
	}
	if len(kernels) == 0 {
		return RefreshOutcome{Outcome: OutcomeUnchanged, Kernels: map[string]KernelOutcome{}}, nil
	}

	meta := metaFor(cfg, prev)
	hwid, locked := s.sourceHWID(cfg, meta)
	res, err := s.fetchAll(ctx, cfg.URL, hwid, locked, kernels)
	if err != nil {
		if ctx.Err() == nil {
			// Сбой записи снимка (переполненный флеш или USB): без отметки проверки
			// срок остался бы наступившим. Отметка — дополнительная мера: на том же
			// носителе она может не записаться, а прошедший срок повтора сетевого
			// сбоя она не сдвигает. Частоту запросов ограничивает откат планировщика
			// в памяти (backOff).
			s.markChecked(id, cfg, s.d.Now())
		}
		return RefreshOutcome{}, err
	}
	// Снимки кандидатов удержаны с момента записи; отпускаются после того, как они
	// попали в Live (применены или ждут как pending) или результат отброшен.
	defer func() {
		for _, oc := range res {
			oc.free()
		}
		// Прежний применённый снимок, отвергнутые и неиспользованные кандидаты после
		// отпускания больше не нужны: каждое обновление с изменившимся составом пишет
		// новый снимок, и без уборки они копились бы на носителе до перезапуска.
		s.spawn(s.collectGarbage)
	}()
	now := s.d.Now()

	fetches := make([]*kernelFetch, 0, len(kernels))
	for _, k := range kernels {
		oc := res[k]
		if oc == nil {
			continue
		}
		s.writeDiag(id, k, oc.fetch, oc.report)
		oc.dropBody()
		kf := &kernelFetch{kernel: k, oc: oc}
		if oc.Ref != nil {
			if inApplied && prev != nil && prev.Kernels[k] != nil && prev.Kernels[k].Applied != nil &&
				prev.Kernels[k].Applied.Hash == oc.Ref.Hash {
				kf.unchanged = true
			} else {
				kf.candidate = true
			}
		}
		fetches = append(fetches, kf)
	}

	freshMeta := s.freshMeta(now, meta, locked, fetches)
	out := RefreshOutcome{Kernels: map[string]KernelOutcome{}}

	var (
		settled settleResult
		cands   = map[string]Ref{}
	)
	if inApplied {
		for _, kf := range fetches {
			if kf.candidate {
				cands[kf.kernel] = *kf.oc.Ref
			}
		}
		if len(cands) > 0 {
			// Пока шёл запрос, подписку могли применить с другим адресом или HWID:
			// конвейер собрал бы файлы из нового применённого состояния, но с набором
			// прежнего адреса. Сверка до запуска; окно между ней и замком конвейера
			// закрывает отпечаток конфигурации у кандидатов (readCandidates).
			cur, err := s.d.Layer.ReadState()
			if err != nil {
				return RefreshOutcome{}, err
			}
			if err := sameSubscription(cur, id, cfg, inApplied); err != nil {
				return RefreshOutcome{}, err
			}
			if settled, err = s.settle(ctx, id, cfg, cands); err != nil {
				return RefreshOutcome{}, err
			}
		}
	}

	// Итоговая запись живых данных: метаданные, время проверки, статусы по ядрам.
	// Пока шёл запрос (на MIPS до минуты), подписку могли изменить: адрес, HWID,
	// выключение или удаление. Тогда ответ относится к прежней конфигурации и
	// статусы по нему не пишутся — иначе набор старого адреса стал бы ожидающим под
	// новым и ушёл бы в «Применить».
	var delta Delta
	err = s.d.Layer.UpdateLiveWith(func(view configlayer.StateView) error {
		if err := sameSubscription(view, id, cfg, inApplied); err != nil {
			return err
		}
		l := view.Live
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		src := cur[id]
		if src == nil {
			src = &LiveSource{Kernels: map[string]*KernelLive{}}
			cur[id] = src
		}
		src.Meta = freshMeta
		src.Meta.Print = candidatePrint(cfg)
		src.Meta.CheckedAt = now
		for _, kf := range fetches {
			kl := src.kernelLive(kf.kernel)
			switch {
			case kf.oc.Err != nil:
				s.setFailure(kl, kf.oc.Err, now)
			case !inApplied:
				// Подписка только в черновике: свежий набор ждёт «Применить».
				ref := *kf.oc.Ref
				kl.Pending = &ref
				kl.setStatus(Status{Code: StatusOK}, now)
			case kf.unchanged:
				if kl.Applied != nil {
					kl.Applied.FetchedAt = kf.oc.Ref.FetchedAt
				}
				// Свежий ответ совпал с применённым: отложенный ранее набор устарел и не
				// должен попасть ни в черновик, ни в следующее «Применить».
				kl.Pending = nil
				kl.Delta = Delta{}
				kl.setStatus(Status{Code: StatusOK}, now)
			case kf.candidate:
				s.settleKernel(kl, kf.kernel, *kf.oc.Ref, settled, now)
				if _, refused := settled.Refused[kf.kernel]; !refused && settled.Outcome == OutcomeApplied {
					delta.Added = max(delta.Added, kl.Delta.Added)
					delta.Removed = max(delta.Removed, kl.Delta.Removed)
				}
			}
		}
		return writeLive(l, cur)
	})
	if err != nil {
		// ErrSourceChanged: живые данные не записаны, срок проверки не сдвинут; повтор
		// с новой конфигурацией делает refreshCurrent. Если смена пришла, пока шёл
		// конвейер, он уже мог отработать: кандидаты прежней конфигурации в нём
		// отброшены по отпечатку, и файлы собраны из применённых снимков. Если смена
		// пришла после конвейера, набор применён под той конфигурацией, которая
		// действовала при запуске.
		return RefreshOutcome{}, err
	}

	s.fillOutcome(&out, fetches, inApplied, settled, delta, prev)
	s.d.Layer.PublishSources()
	s.rebuildNotices()
	return out, nil
}

// markChecked отмечает время проверки источника без изменения снимков и статусов.
// Источник без живых данных не создаётся: его могли удалить, пока шёл запрос
// (Delete подписки только в черновике убирает и её Live). Сбой записи — только в
// журнал: вызывающий уже возвращает исходную ошибку.
func (s *Service) markChecked(id string, cfg SubscriptionConfig, now time.Time) {
	err := s.d.Layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		src := cur[id]
		if src == nil {
			return nil
		}
		if src.Meta.Print != candidatePrint(cfg) {
			// Метаданные прежнего адреса или HWID: отметка относится к новым.
			src.Meta = Meta{Print: candidatePrint(cfg)}
		}
		src.Meta.CheckedAt = now
		return writeLive(l, cur)
	})
	if err != nil {
		log.Printf("[sources] время проверки источника %s не записано: %v", id, err)
	}
}

// freshMeta — метаданные после запроса. Источников три, по порядку:
//  1. удавшиеся ответы: свежие метаданные из них, как и раньше;
//  2. ни один запрос не удался, но провайдер отказал (all_stubs, http_auth), а своих
//     метаданных у подписки под текущими адресом и HWID ещё нет (сменили HWID или
//     адрес — отпечаток не совпал, metaFor пуст): метаданные ответов с отказом. Это
//     ответ самого провайдера на текущие адрес и HWID, с ним же приходят ссылка
//     кабинета, срок и объявление; без него уведомление об отказе осталось бы без
//     ссылки кабинета, а карточка — без данных провайдера;
//  3. иначе прежние. Метаданные той же конфигурации при отказе не перезаписываются.
//
// Сетевые сбои (5xx, TLS, таймаут) и provider_error (HTML, пустой ответ) метаданных не
// дают: заголовки там могут быть от промежуточного узла, а не от провайдера.
// HWIDLocked не снимается, пока запросы идут без HWID: без x-hwid провайдер заголовка
// не повторит.
func (s *Service) freshMeta(now time.Time, prev Meta, locked bool, fetches []*kernelFetch) Meta {
	var metas, rejected []Meta
	for _, kf := range fetches {
		switch {
		case kf.oc.Err == nil:
			metas = append(metas, kf.oc.Meta)
		case statusGroup(kf.oc.Err.Code) == StatusRejected:
			rejected = append(rejected, kf.oc.Meta)
		}
	}
	if len(metas) == 0 {
		if len(rejected) == 0 || !noProviderData(prev) {
			return prev
		}
		metas = rejected
	}
	m := mergeMeta(now, metas...)
	m.HWIDLocked = m.HWIDLocked || locked
	return m
}

// noProviderData — у метаданных нет данных провайдера: без служебных UpdatedAt,
// CheckedAt и Print остаётся пустое значение.
func noProviderData(m Meta) bool {
	m.UpdatedAt, m.CheckedAt, m.Print = time.Time{}, time.Time{}, ""
	return m == Meta{}
}

// fillOutcome собирает итог обновления из результатов по ядрам.
func (s *Service) fillOutcome(out *RefreshOutcome, fetches []*kernelFetch, inApplied bool, settled settleResult, delta Delta, prev *LiveSource) {
	var firstErr *nodes.SourceError
	failed, ok := 0, 0
	rejected := false
	for _, kf := range fetches {
		ko := KernelOutcome{}
		switch {
		case kf.oc.Err != nil:
			failed++
			ko.Status = statusGroup(kf.oc.Err.Code)
			ko.ErrorCode = kf.oc.Err.Code
			if ko.Status != StatusNetwork {
				rejected = true
			}
			if firstErr == nil {
				firstErr = kf.oc.Err
			}
			if prev != nil {
				if kl := prev.Kernels[kf.kernel]; kl != nil && kl.Applied != nil {
					ko.Count = kl.Applied.Usable
				}
			}
		default:
			ok++
			ko.Count = kf.oc.Ref.Usable
			ko.Status = StatusOK
			if kf.candidate && inApplied {
				if _, refused := settled.Refused[kf.kernel]; refused {
					// Набор отвергнут: действует прежний применённый.
					ko.Status = StatusRefused
					ko.Count = 0
					if prev != nil {
						if kl := prev.Kernels[kf.kernel]; kl != nil && kl.Applied != nil {
							ko.Count = kl.Applied.Usable
						}
					}
				} else if settled.Outcome == OutcomeDeferred {
					ko.Status = StatusDeferred
				}
			}
		}
		out.Kernels[kf.kernel] = ko
	}

	switch {
	case inApplied && settled.Outcome != "":
		out.Outcome = settled.Outcome
		if out.Outcome == OutcomeApplied {
			out.Added, out.Removed = delta.Added, delta.Removed
		}
	case failed > 0 && ok == 0:
		out.Outcome = OutcomeNetwork
		if rejected {
			out.Outcome = OutcomeRejected
		}
		out.Error = firstErr
	case !inApplied:
		out.Outcome = OutcomePendingUpdated
	default:
		out.Outcome = OutcomeUnchanged
	}
}

// settleResult — итог запуска конвейера обновления.
type settleResult struct {
	// Outcome — applied, deferred или failed для ядер вне Refused; пусто — конвейер
	// не запускался.
	Outcome string
	// Refused — ядра, отказавшиеся принять состав, с замаскированной причиной. При
	// applied и deferred в нём ядра, отвергнутые до повтора без них.
	Refused map[string]string
}

// settle запускает конвейер из применённого состояния с кандидатами в оверлее:
// сборка, проверка ядрами, запись, перезапуск запущенного ядра. Открытый черновик
// и его ревизия не меняются. Ошибка возврата — остановка сервиса или слоя.
func (s *Service) settle(ctx context.Context, id string, cfg SubscriptionConfig, cands map[string]Ref) (settleResult, error) {
	overlay := configlayer.Sections{}
	if err := writeCandidates(overlay, Candidates{id: cands}, map[string]SubscriptionConfig{id: cfg}); err != nil {
		return settleResult{}, err
	}
	view, err := s.d.Layer.RunSourceRefresh(ctx, overlay)
	switch {
	case errors.Is(err, configlayer.ErrDriftBlocked):
		return settleResult{Outcome: OutcomeDeferred}, nil
	case err != nil:
		return settleResult{}, err
	case view.Result != nil && view.Result.OK:
		return settleResult{Outcome: OutcomeApplied}, nil
	case ctx.Err() != nil:
		// Запуск прерван остановкой сервиса: отказом ядра это не считается.
		return settleResult{}, ctx.Err()
	}

	// Ядро отказалось проверить новый состав: кандидат отбрасывается, прежний набор
	// действует (D-29).
	code, message, blamed := "", "", ""
	rolledBack := false
	if view.Result != nil {
		code, message, blamed, rolledBack = view.Result.Code, view.Result.Message, view.Result.Kernel, view.Result.RolledBack
	}
	reason := s.maskReason(message, code, cfg, cands)
	// Виноватым ядро считается только при отказе проверки или перезапуска с удачным
	// откатом. В деградированных итогах (rollback_failed, kernel_not_recovered)
	// Kernel тоже заполнен, но отказ вызван не кандидатом, а повтор начался бы с
	// восстановления журнала в тех же условиях, в которых откат только что не
	// удался.
	blameable := code == configlayer.CodeValidationFailed || (code == configlayer.ResultRestartFailed && rolledBack)
	if _, ok := cands[blamed]; !ok || !blameable || len(cands) == 1 {
		refused := map[string]string{}
		for k := range cands {
			refused[k] = reason
		}
		return settleResult{Outcome: OutcomeFailed, Refused: refused}, nil
	}
	// Отказало одно названное ядро: конвейер всё-или-ничего, и кандидаты остальных
	// ядер не применены, хотя их не отвергали. Повтор без виноватого ядра: иначе
	// свежие наборы остальных ядер пропали бы, а их прежний отложенный снимок
	// применился бы позже, хотя он старше полученного.
	rest := make(map[string]Ref, len(cands)-1)
	for k, ref := range cands {
		if k != blamed {
			rest[k] = ref
		}
	}
	next, err := s.settle(ctx, id, cfg, rest)
	if err != nil {
		return settleResult{}, err
	}
	if next.Refused == nil {
		next.Refused = map[string]string{}
	}
	next.Refused[blamed] = reason
	return next, nil
}

// settleKernel отражает итог конвейера в состоянии ядра. Применённое состояние
// записал хук фиксации; здесь — отложенная запись и отказ ядра.
//
// При отказе ядра ожидающий снимок снимается: при обновлении он старше
// отвергнутого кандидата, при повторе отложенной записи он сам и есть
// отвергнутый кандидат. Иначе черновик продолжил бы собираться из него, а
// «Применить» молча применило бы непроверенный или отвергнутый набор. Отказ
// проверяется до итога: остальные ядра могли примениться повтором без него.
func (s *Service) settleKernel(kl *KernelLive, kernel string, ref Ref, sr settleResult, now time.Time) {
	if reason, ok := sr.Refused[kernel]; ok {
		kl.Pending = nil
		kl.setStatus(Status{Code: StatusRefused, Params: map[string]string{"reason": reason}}, now)
		return
	}
	if sr.Outcome == OutcomeDeferred {
		p := ref
		kl.Pending = &p
		kl.setStatus(Status{Code: StatusDeferred}, now)
	}
}

// maskReason — причина отказа ядра без секретов: учётные данные узлов
// кандидатов и части адреса подписки заменяются маской, адреса сокращаются до
// домена, текст не длиннее 200 символов (D-29, D-33).
func (s *Service) maskReason(message, code string, cfg SubscriptionConfig, cands map[string]Ref) string {
	if strings.TrimSpace(message) == "" {
		message = code
	}
	var secrets []string
	if u, err := url.Parse(cfg.URL); err == nil {
		secrets = append(secrets, urlSecrets(u)...)
	}
	for _, ref := range cands {
		list, err := s.blobs.get(ref.Hash)
		if err != nil {
			continue
		}
		for _, n := range list {
			secrets = append(secrets, nodes.Secrets(n)...)
		}
	}
	text := nodes.MaskText(message, secrets)
	if r := []rune(text); len(r) > maxReasonRunes {
		text = string(r[:maxReasonRunes])
	}
	return text
}

// setFailure записывает отказ запроса: статус, число попыток и срок повтора. Прежние
// снимки не трогаются.
func (s *Service) setFailure(kl *KernelLive, e *nodes.SourceError, now time.Time) {
	kl.setStatus(errorStatus(e), now)
	kl.Status.RetryAt = s.retryAt(kl, now)
}

// retryable — сетевые отказы, которые повторяются по D-30. tls_error и
// ssrf_blocked ждут обычного интервала: повтор их не лечит.
func retryable(code string) bool {
	switch code {
	case nodes.CodeDNSFailed, nodes.CodeTimeout, nodes.CodeUnreachableDirect, nodes.CodeServerError:
		return true
	}
	return false
}

// retryAt — срок повтора после сетевой ошибки: задержка по номеру попытки (5, 15,
// 60 минут). После последней задержки и для отказов провайдера — нулевое время.
func (s *Service) retryAt(kl *KernelLive, base time.Time) time.Time {
	st := kl.Status
	if st.Code != StatusNetwork || !retryable(st.ErrorCode) {
		return time.Time{}
	}
	if st.Attempts < 1 || st.Attempts > len(s.d.RetryDelays) {
		return time.Time{}
	}
	return base.Add(s.d.RetryDelays[st.Attempts-1])
}

// retryDeferred применяет отложенные записи: для источников со статусом deferred и
// ожидающим снимком запускает конвейер с этими снимками как кандидатами, без
// нового запроса к провайдеру (D-27). Нерешённый дрейф оставляет запись
// отложенной.
func (s *Service) retryDeferred(ctx context.Context) {
	if !s.d.Layer.Enabled() {
		return
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	live, err := readLive(st.Live)
	if err != nil {
		return
	}
	var ids []string
	for id, src := range live {
		for _, kl := range src.Kernels {
			if kl.Status.Code == StatusDeferred && kl.Pending != nil {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	changed := false
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if s.retryDeferredOne(ctx, id) {
			changed = true
		}
	}
	if changed {
		s.d.Layer.PublishSources()
		s.rebuildNotices()
	}
}

// retryDeferredOne применяет отложенные снимки одного источника; true — состояние
// изменилось.
func (s *Service) retryDeferredOne(ctx context.Context, id string) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	// Состояние перечитывается под замком очереди: пока запись ждала, источник мог
	// обновиться, примениться или исчезнуть.
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return false
	}
	cfg, inApplied, err := subscriptionConfig(st, id)
	if err != nil || !inApplied {
		return false
	}
	live, err := readLive(st.Live)
	if err != nil || live[id] == nil {
		return false
	}
	cands := map[string]Ref{}
	for k, kl := range live[id].Kernels {
		if kl.Status.Code == StatusDeferred && kl.Pending != nil {
			cands[k] = *kl.Pending
		}
	}
	if len(cands) == 0 {
		return false
	}
	sr, err := s.settle(ctx, id, cfg, cands)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, configlayer.ErrStopped) && !errors.Is(err, configlayer.ErrDisabled) {
			log.Printf("[sources] отложенная запись источника %s не выполнена: %v", id, err)
		}
		return false
	}
	if sr.Outcome == OutcomeDeferred && len(sr.Refused) == 0 {
		return false
	}
	if sr.Outcome == OutcomeApplied && len(sr.Refused) == 0 {
		// Снимки применены хуком фиксации: статус ok выставлен им.
		return true
	}
	now := s.d.Now()
	err = s.d.Layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		src := cur[id]
		if src == nil {
			return nil
		}
		for k, ref := range cands {
			if kl := src.Kernels[k]; kl != nil {
				s.settleKernel(kl, k, ref, sr, now)
			}
		}
		return writeLive(l, cur)
	})
	if err != nil {
		log.Printf("[sources] состояние источника %s не записано после отложенной записи: %v", id, err)
	}
	return true
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
