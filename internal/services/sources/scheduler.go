package sources

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Start запускает фоновый цикл обновления подписок. Сервис создаётся один раз в
// cmd/xcp/main.go, Start вызывается после запуска слоя; повторный вызов безопасен.
// Цикл работает и при выключенном флаге (его включают без перезапуска), но на
// каждом тике сначала спрашивает Layer.Enabled(): выключенный слой не даёт ни
// запросов к провайдерам, ни чтения состояния.
func (s *Service) Start() {
	s.startOnce.Do(func() {
		s.spawn(s.loop)
	})
}

// Stop останавливает сервис: отменяет контекст, дожидается цикла и фоновых
// горутин. Безопасен при повторном вызове; вызывается через defer в main().
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		s.lmu.Lock()
		s.stopped = true
		s.lmu.Unlock()
		s.cancel()
	})
	s.wg.Wait()
}

// loop — цикл тиков: стартовые действия при включённом слое, затем проверка сроков
// по тикеру TickInterval.
func (s *Service) loop() {
	s.ensureStartup(s.ctx)
	ticker := time.NewTicker(s.d.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.tick(s.ctx)
		}
	}
}

// ensureStartup выполняет стартовые действия один раз, как только слой включён:
// уборка снимков, уведомления по сохранённым статусам, отложенные записи.
func (s *Service) ensureStartup(ctx context.Context) {
	if !s.d.Layer.Enabled() {
		return
	}
	if !s.startupDone.CompareAndSwap(false, true) {
		return
	}
	s.collectGarbage()
	s.rebuildNotices()
	s.retryDeferred(ctx)
}

// tick обновляет применённые включённые подписки, у которых наступил срок.
// Источники идут по одному. Выключенный слой — ничего не делает до чтения
// состояния.
func (s *Service) tick(ctx context.Context) {
	if !s.d.Layer.Enabled() {
		return
	}
	s.ensureStartup(ctx)
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	live, err := readLive(st.Live)
	if err != nil {
		return
	}
	now := s.d.Now()
	ids := subscriptionIDs(st, true)
	s.pruneBackoffs(ids)
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		cfg, _, err := subscriptionConfig(st, id)
		if err != nil {
			continue
		}
		due, ok := s.nextDue(cfg, live[id])
		if !ok || now.Before(due) || s.backedOff(id, now) {
			continue
		}
		// Удачное обновление снимает откат в общей работе Refresh.
		_, err = s.Refresh(ctx, id)
		switch {
		case err == nil, ctx.Err() != nil, errors.Is(err, ErrSourceDisabled), errors.Is(err, ErrSourceNotFound),
			errors.Is(err, ErrSourceChanged), errors.Is(err, configlayer.ErrDisabled):
		default:
			log.Printf("[sources] фоновое обновление источника %s не выполнено: %v", id, err)
			s.backOff(id, now)
		}
	}
}

// backoff — откат планировщика для источника после внутреннего сбоя обновления
// (запись снимка или состояния): until — до какого времени источник не
// запрашивается, n — сбоев подряд.
type backoff struct {
	until time.Time
	n     int
}

// backOff откладывает следующее фоновое обновление источника после внутреннего
// сбоя: 5, 15, затем 60 минут (задержки повторов D-30). Откат держится в памяти:
// переполненный флеш или USB, из-за которого не записался снимок, не даёт
// записать на диск и время проверки, и без отката провайдер запрашивался бы на
// каждом тике. После перезапуска панели откат сбрасывается.
func (s *Service) backOff(id string, now time.Time) {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	b := s.backoffs[id]
	delay := 5 * time.Minute
	if n := len(s.d.RetryDelays); n > 0 {
		delay = s.d.RetryDelays[min(b.n, n-1)]
	}
	b.n++
	b.until = now.Add(delay)
	s.backoffs[id] = b
}

// backedOff — фоновое обновление источника отложено откатом.
func (s *Service) backedOff(id string, now time.Time) bool {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	b, ok := s.backoffs[id]
	return ok && now.Before(b.until)
}

// pruneBackoffs убирает откаты источников, которых планировщик больше не
// обновляет (удалены, выключены): ids — текущие применённые включённые подписки.
func (s *Service) pruneBackoffs(ids []string) {
	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	s.cmu.Lock()
	for id := range s.backoffs {
		if !keep[id] {
			delete(s.backoffs, id)
		}
	}
	s.cmu.Unlock()
}

// intervalOf — интервал обновления подписки (D-05): свой в часах; иначе
// profile-update-interval провайдера; иначе 12 часов. Выключено — false.
func intervalOf(cfg SubscriptionConfig, meta Meta) (time.Duration, bool) {
	hours := 0
	switch cfg.Interval.Mode {
	case IntervalOff:
		return 0, false
	case IntervalHours:
		hours = cfg.Interval.Hours
	}
	if hours <= 0 {
		hours = meta.IntervalH
	}
	if hours <= 0 {
		hours = defaultIntervalHours
	}
	return time.Duration(hours) * time.Hour, true
}

// nextDue — когда подписка должна обновиться в следующий раз. Срок считается от
// последней проверки (Meta.CheckedAt, иначе время добавления) по интервалу. После
// сетевой ошибки, которую стоит повторять (D-30), срок наступает раньше — по
// задержке повтора; отказ провайдера чаще интервала не повторяется. false —
// автоматическое обновление выключено.
func (s *Service) nextDue(cfg SubscriptionConfig, src *LiveSource) (time.Time, bool) {
	var meta Meta
	if src != nil {
		meta = src.Meta
	}
	interval, ok := intervalOf(cfg, meta)
	if !ok {
		return time.Time{}, false
	}
	base := meta.CheckedAt
	if base.IsZero() {
		base = meta.UpdatedAt
	}
	if base.IsZero() {
		// Не проверялась никогда: срок наступил.
		return time.Time{}, true
	}
	due := base.Add(interval)
	if src == nil {
		return due, true
	}
	installed := s.d.Installed()
	for kernel, kl := range src.Kernels {
		// Статус неустановленного ядра не обновляется запросами: его срок повтора
		// вызывал бы обновление на каждом тике.
		if !installed.Has(kernel) {
			continue
		}
		retry := kl.Status.RetryAt
		if retry.IsZero() {
			// Статус записан без срока (подписка добавлена с отказом сети): срок
			// повтора выводится из номера попытки.
			retry = s.retryAt(kl, kl.UpdatedAt)
		}
		if !retry.IsZero() && retry.Before(due) {
			due = retry
		}
	}
	return due, true
}
