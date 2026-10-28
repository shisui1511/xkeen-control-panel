package geodata

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	// opkgBin — менеджер пакетов Entware.
	opkgBin = "/opt/bin/opkg"
	// jqTimeout — срок всей установки jq (обновление списков и установка).
	jqTimeout = 5 * time.Minute
)

// Причины сбоя установки jq (State.JQError).
const (
	jqReasonTimeout = "timeout"
	jqReasonExit    = "exit"
)

// beginJob отмечает начало задания в RunView. false — уже идёт другое задание.
func (s *Service) beginJob(job string) bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.job = job
	s.started = s.d.Now()
	s.mu.Unlock()
	s.touch()
	return true
}

// endJob отмечает конец задания. Если RunView уже занято другим заданием, оно не
// трогается.
func (s *Service) endJob(job string) {
	s.mu.Lock()
	if !s.running || s.job != job {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.job = ""
	s.started = time.Time{}
	s.mu.Unlock()
	s.touch()
}

// InstallJQ ставит jq из ленты Entware: `opkg update`, затем `opkg install jq`
// (фиксированные аргументы, без пользовательского ввода, живой вывод в буфере job jq,
// таймаут 5 минут). Замок применения не берётся: ядра не затрагиваются. Уже идёт
// установка или другое задание менеджера — ErrBusy (geodata_busy).
func (s *Service) InstallJQ() error {
	if !s.ops.jqMu.TryLock() {
		return ErrBusy
	}
	if !s.beginJob(jobJQ) {
		s.ops.jqMu.Unlock()
		return ErrBusy
	}
	out := NewOutput(0)
	s.setOutput(jobJQ, out)
	s.mu.Lock()
	s.st.JQError = ""
	s.mu.Unlock()
	if !s.Go(func() {
		defer s.ops.jqMu.Unlock()
		s.runJQ(out)
	}) {
		s.ops.jqMu.Unlock()
		s.endJob(jobJQ)
		return ErrStopped
	}
	return nil
}

// runJQ выполняет установку и записывает итог: jq найден — JQError пуст.
func (s *Service) runJQ(out *Output) {
	ctx, cancel := context.WithTimeout(s.ctx, jqTimeout)
	defer cancel()
	r := Runner{Bin: opkgBin, Timeout: jqTimeout}

	// Сбой обновления списков не мешает установке из уже загруженных.
	if _, err := r.Run(ctx, []string{"update"}, out); err != nil {
		log.Printf("[geodata] opkg update: %v", err)
	}
	_, err := r.Run(ctx, []string{"install", "jq"}, out)

	// Итог — по факту: jq найден тем же поиском, что у хранилища записей. Статус
	// возвращает JQ и при нечитаемом xkeen.json.
	reason := ""
	if st, _ := s.d.Store.Status(); !st.JQ {
		reason = jqReasonExit
		if errors.Is(err, ErrRunTimeout) || errors.Is(err, context.DeadlineExceeded) {
			reason = jqReasonTimeout
		}
	}
	s.mu.Lock()
	s.st.JQError = reason
	s.mu.Unlock()
	s.endJob(jobJQ)
	s.changed()
}
