package configlayer

import (
	"errors"
	"time"
)

// WizardStatus — признак мастера первого запуска (149.1, D-06, D-10, D-11).
// Монотонен: ранг только растёт, «пройдено» и «скрыто» не сбрасываются ничем,
// кроме сброса схемы состояния и ручки режима разработчика (ResetWizard).
type WizardStatus string

// Статусы мастера первого запуска.
const (
	// WizardClosed — мастер закрыт: автооткрытие отменено, карточка дашборда
	// остаётся, пока шаги не выполнены (D-10).
	WizardClosed WizardStatus = "closed"
	// WizardDone — шаги 1–6 впервые выполнены; ставит сервер (D-06).
	WizardDone WizardStatus = "done"
	// WizardHidden — карточка скрыта владельцем, действует как done (D-11).
	WizardHidden WizardStatus = "hidden"
)

// ErrWizardStatus — неизвестный статус мастера на входе.
var ErrWizardStatus = errors.New("неизвестный статус мастера настройки")

// WizardState — признак мастера в файле состояния: статус и время его постановки.
type WizardState struct {
	Status WizardStatus `json:"status"`
	At     time.Time    `json:"at"`
}

// validWizardStatus — статус из известного набора closed|done|hidden.
func validWizardStatus(s WizardStatus) bool {
	switch s {
	case WizardClosed, WizardDone, WizardHidden:
		return true
	}
	return false
}

// wizardRank — ранг признака для монотонной записи: nil и неизвестный статус — 0,
// closed — 1, done и hidden — 2.
func wizardRank(w *WizardState) int {
	if w == nil {
		return 0
	}
	switch w.Status {
	case WizardClosed:
		return 1
	case WizardDone, WizardHidden:
		return 2
	}
	return 0
}

// MarkWizard ставит признак мастера, если его ранг выше текущего, и сохраняет
// состояние (fsync). Ранг не выше текущего — ничего не меняется и файл не пишется
// (false). Под замком хранилища: две вкладки и наблюдатель службы мастера не
// понизят признак друг другу.
func (s *Store) MarkWizard(status WizardStatus, now time.Time) (bool, error) {
	s.Flush() // без mu: см. Update
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	if wizardRank(&WizardState{Status: status}) <= wizardRank(s.st.Wizard) {
		return false, nil
	}

	next := s.st.clone()
	next.Wizard = &WizardState{Status: status, At: now.UTC()}
	if err := s.persist(next); err != nil {
		return false, err
	}
	s.commitLocked(next)
	return true, nil
}

// MarkWizard ставит признак мастера (closed — «Закрыть», hidden — «Скрыть»,
// done — отметка службы мастера). Признак монотонен: false — статус не изменился.
// Смена публикует событие wizard.
func (l *Layer) MarkWizard(status WizardStatus) (bool, error) {
	if !l.Enabled() {
		return false, ErrDisabled
	}
	if !validWizardStatus(status) {
		return false, ErrWizardStatus
	}
	changed, err := l.store.MarkWizard(status, time.Now())
	if err != nil {
		return false, err
	}
	if changed {
		l.PublishWizard()
	}
	return changed, nil
}

// WizardStatus — текущий статус мастера; признака нет — пустая строка.
// Выключенный слой диск не опрашивает.
func (l *Layer) WizardStatus() WizardStatus {
	if !l.Enabled() {
		return ""
	}
	var status WizardStatus
	l.store.read(func(st State) {
		if st.Wizard != nil {
			status = st.Wizard.Status
		}
	})
	return status
}

// PublishWizard сообщает клиентам, что состояние мастера изменилось (событие
// wizard со сквозным счётчиком): клиент по нему перечитывает GET
// /api/configlayer/wizard. Выключенный слой молчит.
func (l *Layer) PublishWizard() {
	if !l.Enabled() {
		return
	}
	l.broker.Publish(Event{Type: EventWizard, Data: WizardEvent{Seq: l.wizardSeq.Add(1)}})
}
