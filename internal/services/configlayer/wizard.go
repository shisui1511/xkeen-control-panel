package configlayer

import (
	"bytes"
	"encoding/json"
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

// MarkWizard ставит признак мастера, если его ранг выше текущего. Ранг не выше
// текущего — ничего не меняется (false). Под замком хранилища: две вкладки и
// наблюдатель службы мастера не понизят признак друг другу.
//
// Признак пишется по схеме правок черновика (EditDraft): состояние меняется в
// памяти, файл пишет фоновая запись без fsync (flushLoop) с досинхронизацией
// (scheduleSync), остаток при остановке слоя и перед самообновлением — FlushSync.
// Ответ на «Закрыть» и «Скрыть» и отметка «пройдено» не ждут носителя: fsync и
// подмена переименованием на флешке роутера под чужой записью стоят секунды —
// десятки секунд (G-149.1-6). Окно риска при обрыве питания то же, что у правок
// черновика: последний признак может не дойти до файла — closed означает ещё одно
// автооткрытие, done наблюдатель поставит снова.
func (s *Store) MarkWizard(status WizardStatus, now time.Time) (bool, error) {
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
	s.commitLocked(next)
	s.scheduleFlushLocked()
	return true, nil
}

// MarkWizard ставит признак мастера (closed — «Закрыть», hidden — «Скрыть»,
// done — отметка службы мастера). Признак монотонен: false — статус не изменился.
// Смена публикует событие wizard.
func (l *Layer) MarkWizard(status WizardStatus) (bool, error) {
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
func (l *Layer) WizardStatus() WizardStatus {
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
// /api/configlayer/wizard.
func (l *Layer) PublishWizard() {
	l.broker.Publish(Event{Type: EventWizard, Data: WizardEvent{Seq: l.wizardSeq.Add(1)}})
}

// ResetWizard снимает признак мастера. Признака нет — ничего не меняется (false).
// Запись — как у MarkWizard: в памяти и фоновой записью файла, ответ на сброс не
// ждёт носителя (G-149.1-6); окно риска при обрыве питания — как у правок
// черновика.
func (s *Store) ResetWizard() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	if s.st.Wizard == nil {
		return false, nil
	}

	next := s.st.clone()
	next.Wizard = nil
	s.commitLocked(next)
	s.scheduleFlushLocked()
	return true, nil
}

// CheckWizardReset — проверка сброса признака мастера без обращения к
// хранилищу: включён режим разработчика (ErrDevModeRequired). Служба мастера
// зовёт её до очереди записей, чтобы код 403 не ждал чужих записей признака.
func (l *Layer) CheckWizardReset() error {
	if !l.devMode() {
		return ErrDevModeRequired
	}
	return nil
}

// ResetWizard снимает признак мастера: мастер снова считается непройденным.
// Только в режиме разработчика — ручка проверок карточки и автооткрытия на одном
// стенде; иначе признак стирает лишь сброс схемы состояния (149 D-26).
func (l *Layer) ResetWizard() error {
	if err := l.CheckWizardReset(); err != nil {
		return err
	}
	changed, err := l.store.ResetWizard()
	if err != nil {
		return err
	}
	if changed {
		l.PublishWizard()
	}
	return nil
}

// wizardRulesSection — имя секции правил маршрутизации в черновике (то же, что
// routing.RulesSection: пакет routing импортирует configlayer, обратный импорт —
// цикл).
const wizardRulesSection = "rules"

// WizardProbe — счётчики, которые подключает служба мастера: пакет источников
// импортирует configlayer, поэтому слой зовёт его через функцию.
type WizardProbe struct {
	// Nodes — рабочие узлы и источники включённых источников по набору секций и
	// живым данным. Вызывается под замком хранилища: без ввода-вывода, секции и
	// живые данные после возврата не удерживать.
	Nodes func(sections, live Sections) (nodes, sources int)
}

// WizardFacts — факты шагов 2–5 мастера одним чтением состояния (149 D-22, D-04).
type WizardFacts struct {
	// Status — признак мастера; пусто — мастер не тронут.
	Status WizardStatus
	// XrayTaken — секции Xray черновика под панелью (из XraySectionIDs).
	XrayTaken int
	// RoutingAny — панель держит маршрутизацию хотя бы одного ядра (черновик).
	RoutingAny bool
	// RoutingMihomo — панель держит маршрутизацию Mihomo (профиль панели).
	RoutingMihomo bool
	// Nodes и Sources — рабочие узлы и источники черновика по WizardProbe.
	Nodes   int
	Sources int
	// Rules — копия байтов секции rules черновика (nil — секции нет): её
	// разбирает служба мастера пакетом routing.
	Rules json.RawMessage
	// DraftChanges — число неприменённых изменений черновика.
	DraftChanges int
	// Empty и AppliedEmpty — набор секций пуст (149 D-22): ни одного источника,
	// ни одной секции Xray под панелью, маршрутизация не взята. Empty — по
	// черновику (автооткрытие), AppliedEmpty — по применённому (D-04.5).
	Empty        bool
	AppliedEmpty bool
	// ApplyRunning — идёт применение.
	ApplyRunning bool
}

// xrayTakenCount — число секций Xray под панелью в наборе секций.
func xrayTakenCount(s Sections) int {
	cores := DesiredCores(s)
	n := 0
	for _, id := range XraySectionIDs {
		if cores[CoreSectionName(KernelXray, id)].Managed {
			n++
		}
	}
	return n
}

// WizardFacts читает факты шагов мастера одним чтением под замком хранилища, без
// глубоких копий: наблюдатель службы мастера зовёт её на каждом тике.
func (l *Layer) WizardFacts(p WizardProbe) WizardFacts {
	var f WizardFacts
	l.store.read(func(st State) {
		if st.Wizard != nil {
			f.Status = st.Wizard.Status
		}
		f.XrayTaken = xrayTakenCount(st.Draft)
		rd := DesiredRouting(st.Draft)
		f.RoutingAny = rd.AnyManaged()
		f.RoutingMihomo = rd.ManagedFor(KernelMihomo)
		appliedSources := 0
		if p.Nodes != nil {
			f.Nodes, f.Sources = p.Nodes(st.Draft, st.Live)
			_, appliedSources = p.Nodes(st.Applied, st.Live)
		}
		if raw, ok := st.Draft[wizardRulesSection]; ok {
			f.Rules = bytes.Clone(raw)
		}
		f.DraftChanges = l.store.changeCount(st)
		f.Empty = f.Sources == 0 && f.XrayTaken == 0 && !f.RoutingAny
		f.AppliedEmpty = appliedSources == 0 && xrayTakenCount(st.Applied) == 0 &&
			!DesiredRouting(st.Applied).AnyManaged()
	})
	f.ApplyRunning = l.pipeline.Current().Running
	return f
}
