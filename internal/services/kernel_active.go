package services

import "strings"

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
	var running []string
	for _, st := range states {
		if st.Status == "running" {
			running = append(running, st.Name)
		}
	}
	switch len(running) {
	case 0:
		return ActiveKernelState{Kernel: idleKernel(freshRaw, configured)}
	case 1:
		return ActiveKernelState{Kernel: running[0], Running: running}
	default:
		return ActiveKernelState{Conflict: true, Running: running}
	}
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
	s.activeMu.Unlock()
}

// ActiveState — текущее активное ядро и признак конфликта. nil-безопасен.
func (s *KernelService) ActiveState() ActiveKernelState {
	if s == nil {
		return ActiveKernelState{Kernel: "none"}
	}
	s.activeMu.Lock()
	freshRaw, configured := s.freshRawFn, s.configuredFn
	s.activeMu.Unlock()
	return ResolveActiveState(s.ProcessStates(), freshRaw, configured)
}
