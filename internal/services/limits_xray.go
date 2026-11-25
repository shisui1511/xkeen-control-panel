package services

import (
	"context"
	"log"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/xrayapi"
)

// Учёт лимитов на Xray (D-02, D-03): счётчики исходящих StatsService
// (outbound>>>tag>>>traffic>>>uplink/downlink), запрос с шаблоном outbound>>> без
// сброса. Расход — дельты между опросами:
//   - первое чтение после старта панели или смены активного ядра — только базовая
//     линия, без прибавления;
//   - значение меньше прежнего — сброс счётчика ядра (перезапуск Xray): дельта равна
//     текущему значению;
//   - тег, которого нет в ответе (счётчик ещё не создан), пропускается без сдвига
//     базовой линии; тег, появившийся после базовой линии, считается с нуля.
//
// Ручная группа считается по своему тегу xcp-g-<id>, «авто»/«резерв» — по тегам
// узлов балансировщика (цели — TrafficTargets маршрутизации). Нет счётчиков (нет
// блока API панели, чужой api, ошибка gRPC, пустой ответ) — состояние no_counters,
// а не ложный ноль.

// xrayPollInterval — период опроса счётчиков Xray (не чаще, только при активном Xray).
const xrayPollInterval = 10 * time.Second

// xrayPollTimeout — предел одного опроса: подключение и запрос (сам запрос — 3 с).
const xrayPollTimeout = 5 * time.Second

// XrayCounterSource — источник клиента gRPC Xray (его реализует *XrayGRPCService).
// release вызывается после запроса.
type XrayCounterSource interface {
	Acquire(ctx context.Context) (*xrayapi.Client, func(), error)
}

var _ XrayCounterSource = (*XrayGRPCService)(nil)

// SetXrayCounters задаёт источник счётчиков Xray. nil — опрос не идёт, при активном
// Xray состояние данных no_counters.
func (s *LimitsService) SetXrayCounters(src XrayCounterSource) {
	s.mu.Lock()
	s.xraySrc = src
	s.mu.Unlock()
}

// xrayPollState — базовая линия опроса; принадлежит циклу опроса.
type xrayPollState struct {
	// base — последнее значение uplink+downlink по тегу исходящего.
	base map[string]int64
	// primed — базовая линия снята: следующее чтение даёт дельты.
	primed bool
	// lastData — последнее записанное состояние данных (для журнала переходов).
	lastData string
}

// reset сбрасывает базовую линию: следующее чтение — только базовая линия.
func (p *xrayPollState) reset() {
	p.base = nil
	p.primed = false
}

// xrayLoop опрашивает счётчики Xray раз в xrayPollInterval до Stop.
func (s *LimitsService) xrayLoop() {
	defer close(s.xrayDone)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	t := time.NewTicker(xrayPollInterval)
	defer t.Stop()
	var p xrayPollState
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.pollXray(ctx, &p)
		}
	}
}

// pollXray — один опрос. Активное ядро не Xray — базовая линия сбрасывается, данные
// не трогаются (состояние списка при другом ядре считает List).
func (s *LimitsService) pollXray(ctx context.Context, p *xrayPollState) {
	if s.activeKernel() != "xray" {
		p.reset()
		return
	}
	t, ok := s.readTargets()
	if !ok {
		// Цели неизвестны: прибавлять некуда. Базовая линия остаётся — накопленное
		// посчитается следующим удачным опросом.
		s.xrayData(p, LimitDataNoCounters)
		return
	}
	if !t.XrayManaged {
		// Маршрутизацией Xray не управляет панель: тегов групп нет.
		p.reset()
		s.xrayData(p, LimitDataNoCounters)
		return
	}
	cur, err := s.readXrayCounters(ctx)
	if err != nil || len(cur) == 0 {
		s.xrayData(p, LimitDataNoCounters)
		return
	}
	if !p.primed {
		p.base, p.primed = cur, true
		s.xrayData(p, LimitDataOK)
		return
	}

	delta := make(map[string]int64, len(cur))
	for tag, v := range cur {
		last := p.base[tag]
		if v >= last {
			delta[tag] = v - last
		} else {
			// Счётчик уменьшился — ядро сбросило его (перезапуск Xray).
			delta[tag] = v
		}
		p.base[tag] = v
	}

	groupBytes := map[string]int64{}
	for gid, g := range t.Groups {
		if g.Direct {
			continue
		}
		var sum int64
		for _, tag := range g.XrayTags {
			sum += delta[tag]
		}
		if sum > 0 {
			groupBytes[gid] = sum
		}
	}
	var all int64
	for _, tag := range t.XrayGlobalTags {
		all += delta[tag]
	}
	s.xrayData(p, LimitDataOK)
	if all > 0 || len(groupBytes) > 0 {
		s.AddUsage(groupBytes, all)
	}
}

// readXrayCounters читает счётчики исходящих: тег → uplink+downlink.
func (s *LimitsService) readXrayCounters(ctx context.Context) (map[string]int64, error) {
	s.mu.RLock()
	src := s.xraySrc
	s.mu.RUnlock()
	if src == nil {
		return nil, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, xrayPollTimeout)
	defer cancel()
	client, release, err := src.Acquire(callCtx)
	if err != nil {
		return nil, err
	}
	defer release()
	pairs, err := client.OutboundCounters(callCtx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(pairs))
	for tag, p := range pairs {
		out[tag] = p.Uplink + p.Downlink
	}
	return out, nil
}

// xrayData записывает состояние данных опроса; переход в no_counters и обратно —
// одна строка журнала без подробностей ошибки.
func (s *LimitsService) xrayData(p *xrayPollState, state string) {
	s.setDataState(state)
	if p.lastData == state {
		return
	}
	switch {
	case state == LimitDataNoCounters:
		log.Printf("limits: счётчики Xray недоступны, учёт Xray приостановлен")
	case p.lastData == LimitDataNoCounters:
		log.Printf("limits: счётчики Xray снова доступны")
	}
	p.lastData = state
}
