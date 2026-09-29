package services

// KernelProcessState — лёгкое состояние процесса ядра: без определения версии
// (запуск бинарника) и без чтения каталога бэкапов.
type KernelProcessState struct {
	Name   string
	Status string
	PID    int
	Uptime string
}

// ProcessStates возвращает состояния процессов ядер в порядке [xray, mihomo].
// В отличие от List не запускает бинарники ядер: только проверка файла и /proc,
// поэтому безопасен на горячем пути статуса под нагрузкой роутера.
func (s *KernelService) ProcessStates() []KernelProcessState {
	type target struct {
		name, path string
	}
	order := []string{"xray", "mihomo"}
	targets := make([]target, 0, len(order))

	s.mu.Lock()
	for _, name := range order {
		if k, ok := s.kernels[name]; ok {
			s.resolveBinaryPath(k)
			targets = append(targets, target{name: k.Name, path: k.BinaryPath})
		}
	}
	s.mu.Unlock()

	// Процессы читаются вне общего замка, чтобы не задерживать Install/CheckLatest
	states := make([]KernelProcessState, 0, len(targets))
	for _, t := range targets {
		status, pid, uptime := kernelProcessStatusDetailed(t.path)
		states = append(states, KernelProcessState{Name: t.name, Status: status, PID: pid, Uptime: uptime})
	}
	return states
}
