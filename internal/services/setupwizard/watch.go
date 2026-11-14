package setupwizard

import (
	"context"
	"log"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// defaultInterval — тик наблюдателя.
const defaultInterval = 10 * time.Second

// «Пройдено» ставит сервер сам (D-06): хук после успешного применения и
// наблюдатель раз в defaultInterval вызывают одну проверку шагов 1–6 и, когда все
// они впервые выполнены, — Layer.MarkWizard(WizardDone). Шаги выполняются и через
// мастер, и через раздел, а запуск ядра идёт вне слоя — поэтому нужен тик, а не
// только хук. Монотонность признака держит MarkWizard: «пройдено» не
// сбрасывается, «скрыто» не понижается. Ядро служба не запускает и черновик не
// применяет (FND-05): она только наблюдает.

// Hooks — хук после применения для регистрации в слое до его Start.
func (s *Service) Hooks() configlayer.Hooks {
	return configlayer.Hooks{AfterApply: s.afterApply}
}

// Start запускает наблюдатель. Повторный вызов ничего не делает.
func (s *Service) Start() {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	go s.run()
}

// Stop останавливает наблюдатель и ждёт выхода цикла. Идемпотентен; без Start
// не ждёт.
func (s *Service) Stop() {
	s.stopOnce.Do(s.cancel)
	if s.started.Load() {
		<-s.done
	}
}

// run — цикл наблюдателя: первая проверка сразу, затем по тикеру.
func (s *Service) run() {
	defer close(s.done)
	interval := s.d.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if s.ctx.Err() != nil {
			return
		}
		s.tick()
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// afterApply — хук слоя после успешного применения. Слой зовёт его в своей
// горутине после снятия замка применения (неуспешное применение хуки не
// вызывает), поэтому конвейер он не задерживает.
func (s *Service) afterApply(_ context.Context, _ configlayer.AfterApplyInfo) {
	s.tick()
}

// tick — одна проверка: смена вектора шагов публикует событие wizard (клиенты
// перечитывают GET), все шаги 1–6 выполнены — признак done. После done или
// hidden расчёт и событие остаются (мастер открыт по ссылке раздела, шаги на
// странице должны обновляться), не пишется только признак. Читает только факты
// слоя: вид секций ядра и копии состояния здесь не нужны.
func (s *Service) tick() {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	layer := s.d.Layer
	if !layer.Enabled() {
		return
	}

	inst := s.d.Installed()
	f := layer.WizardFacts(probe(inst))
	if !f.Enabled {
		return
	}
	ev := evaluate(f, inst, s.d.XKeenInstalled(), s.d.Kernels())

	sig := ev.signature()
	if s.lastSig != "" && sig != s.lastSig {
		layer.PublishWizard()
	}
	s.lastSig = sig

	if f.Status == configlayer.WizardDone || f.Status == configlayer.WizardHidden || !ev.allDone() {
		return
	}
	// Между чтением фактов и записью признака черновик могли поправить: done —
	// только если свежая проверка шагов 1–6 его подтверждает. Сброс и «Закрыть»
	// или «Скрыть» берут tickMu и сюда не вклиниваются.
	inst = s.d.Installed()
	if f = layer.WizardFacts(probe(inst)); !f.Enabled ||
		!evaluate(f, inst, s.d.XKeenInstalled(), s.d.Kernels()).allDone() {
		return
	}
	if _, err := layer.MarkWizard(configlayer.WizardDone); err != nil {
		// Повтор — на следующем тике. В тексте ошибки записи нет данных источников.
		log.Printf("setupwizard: отметка «пройдено» не записана: %v", err)
	}
}

// signature — вектор выполненных шагов и номер текущего: по смене подписи
// наблюдатель сообщает клиентам, что статус шагов изменился.
func (e evaluation) signature() string {
	b := make([]byte, 0, TotalSteps+1)
	for _, st := range e.steps {
		if st.Done {
			b = append(b, '1')
		} else {
			b = append(b, '0')
		}
	}
	return string(append(b, byte('0'+e.current)))
}

// allDone — выполнены все шаги 1–6.
func (e evaluation) allDone() bool { return e.doneCount == TotalSteps }
