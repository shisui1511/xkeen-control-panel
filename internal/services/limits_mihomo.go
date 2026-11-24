package services

import "time"

// Учёт лимитов на Mihomo (D-02): дельты соединений потока /connections. Конечный
// исходящий соединения — chains[0] (spike фазы 152: mihomo_chains_final=first),
// последний элемент chains — внешняя группа правила. Соединение на DIRECT или REJECT
// расходом не считается. Расход группы — дельты соединений, в chains которых есть
// имя группы в профиле (внешней и вложенной: mihomo_group_in_chains=yes).

// limitsTargetsTTL — как долго приём дельт Mihomo пользуется прочитанными целями:
// чтение целей строит модель маршрутизации, а снимки идут раз в секунду.
const limitsTargetsTTL = 5 * time.Second

// Конечные исходящие Mihomo, трафик которых расходом лимита не считается.
const (
	mihomoOutDirect = "DIRECT"
	mihomoOutReject = "REJECT"
)

// SetMihomoState задаёт источник состояния потока /connections Mihomo
// (TrafficQuotaService.StreamState): no_socket, stopped, down, failing или ok.
func (s *LimitsService) SetMihomoState(fn func() string) {
	s.mu.Lock()
	s.mihomoStateF = fn
	s.mu.Unlock()
}

// mihomoDataState — состояние данных учёта при активном Mihomo: нет сокета
// контроллера или ядро остановлено — соответствующее состояние; идёт полоса отказов
// подключения потока — no_stream (учёт стоит, данных нет); поток подключён или
// кратко переподключается — ok. Caller не держит s.mu.
func (s *LimitsService) mihomoDataState() string {
	s.mu.RLock()
	fn := s.mihomoStateF
	s.mu.RUnlock()
	if fn == nil {
		return LimitDataOK
	}
	switch fn() {
	case StreamStateNoSocket:
		return LimitDataNoSocket
	case StreamStateStopped:
		return LimitDataStopped
	case StreamStateFailing:
		return LimitDataNoStream
	}
	return LimitDataOK
}

// observeTargets — цели учёта для частого приёма дельт: прочитанные не раньше
// limitsTargetsTTL назад или свежие. ok=false — цели неизвестны.
func (s *LimitsService) observeTargets(now time.Time) (TrafficTargets, bool) {
	s.mu.RLock()
	t, at := s.obsTargets, s.obsTargetsAt
	s.mu.RUnlock()
	if !at.IsZero() && now.Sub(at) < limitsTargetsTTL && now.Sub(at) >= 0 {
		return t, true
	}
	t, ok := s.readTargets()
	if !ok {
		return TrafficTargets{}, false
	}
	s.mu.Lock()
	s.obsTargets, s.obsTargetsAt = t, now
	s.mu.Unlock()
	return t, true
}

// ObserveMihomo принимает дельты соединений снимка /connections (хук
// TrafficQuotaService.SetConnDeltaHook) и прибавляет расход лимитам. Активное ядро не
// Mihomo или цели неизвестны — дельты отбрасываются: расход по группам без целей
// был бы ложным нулём.
func (s *LimitsService) ObserveMihomo(deltas []ConnDelta) {
	if len(deltas) == 0 || s.activeKernel() != "mihomo" {
		return
	}
	t, ok := s.observeTargets(time.Now())
	if !ok {
		return
	}
	// Имя группы в профиле → ID групп панели, без групп на DIRECT/REJECT.
	byName := map[string][]string{}
	for gid, g := range t.Groups {
		if g.Direct {
			continue
		}
		for _, name := range g.MihomoNames {
			if name != "" {
				byName[name] = append(byName[name], gid)
			}
		}
	}

	var all int64
	groupBytes := map[string]int64{}
	for _, d := range deltas {
		if len(d.Chains) == 0 || d.Bytes <= 0 {
			continue
		}
		switch d.Chains[0] {
		case "", mihomoOutDirect, mihomoOutReject:
			continue
		}
		all += d.Bytes
		if len(byName) == 0 {
			continue
		}
		// Группа считается один раз на соединение, даже если её имена (основное и
		// скрытая группа «резерва») встречаются в цепочке оба.
		var seen map[string]bool
		for _, name := range d.Chains {
			for _, gid := range byName[name] {
				if seen == nil {
					seen = map[string]bool{}
				}
				if !seen[gid] {
					seen[gid] = true
					groupBytes[gid] += d.Bytes
				}
			}
		}
	}
	if all == 0 {
		return
	}
	s.AddUsage(groupBytes, all)
}
