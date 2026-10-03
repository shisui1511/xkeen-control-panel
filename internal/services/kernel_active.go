package services

import (
	"strings"
	"time"
)

// Переменные, а не константы: тесты подменяют их (с восстановлением через Cleanup).
var (
	// activeStateTTL — сколько ActiveState отдаёт кэш, не перечитывая /proc.
	// Меньше 3-секундного кэша capabilities; после start/stop/restart/switch
	// кэш сбрасывается lifecycle-хуком (InvalidateActiveState).
	activeStateTTL = 2 * time.Second
	// conflictMinAge — минимальный возраст процесса, с которого он считается
	// установившимся ядром. Процесс моложе (ядро только что стартовало при
	// переключении) рядом с установившимся ядром конфликтом не считается.
	conflictMinAge = 2 * time.Second
)

// ActiveKernelState — единое определение активного ядра: что запущено и нет ли
// конфликта. Конфликт выводится только из процессов (/proc), а не из текста
// `xkeen -status`: в нём оба слова встречаются и в нормальном состоянии.
type ActiveKernelState struct {
	// Kernel — "xray" | "mihomo" | "none"; при конфликте пуст (ядро не выбирается).
	Kernel string `json:"kernel"`
	// Conflict — запущены оба ядра.
	Conflict bool `json:"conflict"`
	// Running — запущенные ядра в порядке [xray, mihomo].
	Running []string `json:"running"`
}

// Label — значение active_kernel в API: "both" при конфликте, иначе
// "xray" | "mihomo" | "none".
func (st ActiveKernelState) Label() string {
	if st.Conflict {
		return "both"
	}
	if st.Kernel == "xray" || st.Kernel == "mihomo" {
		return st.Kernel
	}
	return "none"
}

// ResolveActiveState сводит состояния процессов к активному ядру. Без запущенных
// ядер активным считается то, что подтверждает свежий снимок `xkeen -status`
// (ровно одно ядро в тексте), затем ядро из name_client init-скрипта, иначе none.
// nil-функции допустимы и пропускаются.
func ResolveActiveState(states []KernelProcessState, freshRaw func() (string, bool), configured func() string) ActiveKernelState {
	var running, established []string
	for _, st := range states {
		if st.Status != "running" {
			continue
		}
		running = append(running, st.Name)
		// Неизвестный возраст считается установившимся (fail-closed: лучше
		// показать конфликт, чем скрыть его).
		if !st.AgeKnown || st.Age >= conflictMinAge {
			established = append(established, st.Name)
		}
	}
	switch len(running) {
	case 0:
		return ActiveKernelState{Kernel: idleKernel(freshRaw, configured)}
	case 1:
		return ActiveKernelState{Kernel: running[0], Running: running}
	}
	// Два процесса: ровно одно установившееся ядро и новичок младше
	// conflictMinAge — это переключение в ходе, а не конфликт. Оба молодые или оба
	// установившиеся — конфликт.
	if len(established) == 1 {
		return ActiveKernelState{Kernel: established[0], Running: running}
	}
	return ActiveKernelState{Conflict: true, Running: running}
}

// idleKernel — активное ядро, когда ни один процесс не запущен.
func idleKernel(freshRaw func() (string, bool), configured func() string) string {
	if freshRaw != nil {
		// Только свежий снимок: устаревший хранит прежнее ядро (после switch_kernel
		// или при зависшем опросе). Оба слова в тексте — ядро по нему не выбирается.
		if raw, fresh := freshRaw(); fresh && raw != "" {
			lower := strings.ToLower(raw)
			hasXray := strings.Contains(lower, "xray")
			hasMihomo := strings.Contains(lower, "mihomo")
			if hasXray && !hasMihomo {
				return "xray"
			}
			if hasMihomo && !hasXray {
				return "mihomo"
			}
		}
	}
	if configured != nil {
		if k := configured(); k == "xray" || k == "mihomo" {
			return k
		}
	}
	return "none"
}

// SetActiveFallbacks подключает запасные источники активного ядра для случая,
// когда ни один процесс не запущен: свежий снимок `xkeen -status` и ядро из
// name_client init-скрипта. KernelService не знает про кэш статуса и XKeen —
// источники вставляет main/обработчики.
func (s *KernelService) SetActiveFallbacks(freshRaw func() (string, bool), configured func() string) {
	s.activeMu.Lock()
	s.freshRawFn = freshRaw
	s.configuredFn = configured
	s.activeFresh = false
	s.activeGen++
	s.activeMu.Unlock()
}

// InvalidateActiveState сбрасывает кэш ActiveState; зовётся после действий
// жизненного цикла и при ClearCapabilitiesCache. nil-безопасен.
func (s *KernelService) InvalidateActiveState() {
	if s == nil {
		return
	}
	s.activeMu.Lock()
	s.activeFresh = false
	s.activeGen++
	s.activeMu.Unlock()
}

// ActiveState — текущее активное ядро и признак конфликта. Результат кэшируется
// на activeStateTTL: на горячем пути (capabilities, гейты API) /proc на MIPS
// читать на каждый запрос дорого. nil-безопасен.
func (s *KernelService) ActiveState() ActiveKernelState {
	if s == nil {
		return ActiveKernelState{Kernel: "none"}
	}
	s.activeMu.Lock()
	if s.activeFresh && time.Since(s.activeAt) < activeStateTTL {
		st := copyActiveState(s.activeCache)
		s.activeMu.Unlock()
		return st
	}
	freshRaw, configured, gen := s.freshRawFn, s.configuredFn, s.activeGen
	s.activeMu.Unlock()

	// Расчёт вне activeMu: он читает /proc и может запускать `xkeen -status`;
	// замок на это время блокировал бы InvalidateActiveState из lifecycle-хука.
	st := ResolveActiveState(s.ProcessStates(), freshRaw, configured)

	s.activeMu.Lock()
	if s.activeGen == gen {
		s.activeCache = copyActiveState(st)
		s.activeAt = time.Now()
		s.activeFresh = true
	}
	s.activeMu.Unlock()
	return st
}

// copyActiveState копирует срез Running: кэш не отдаётся наружу по ссылке.
func copyActiveState(st ActiveKernelState) ActiveKernelState {
	if st.Running != nil {
		st.Running = append([]string(nil), st.Running...)
	}
	return st
}
