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
	t := time.NewTicker(s.interval())
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
// слоя и состояние ядер: вид секций ядра и копии состояния здесь не нужны.
//
// Проверки идут по одной под checkMu. Установленность, факты слоя и состояние
// ядер (один вызов) читаются вне tickMu и без предела — это фон; итог кладётся в
// кэш наблюдения (счёт секций Xray тик не читает, кэшированный остаётся). Замок
// записи tickMu берётся только для done: под ним факты слоя перечитываются и
// done ставится, только если шаги 1–6 по ним выполнены (WR-03); ядра под ним не
// перечитываются — сброс и «Закрыть» ядро не меняют.
func (s *Service) tick() {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	layer := s.d.Layer

	start := time.Now()
	seq := s.startSeq()
	inst := s.d.Installed()
	xkeen := s.d.XKeenInstalled()
	t := time.Now()
	f := layer.WizardFacts(probe(inst))
	factsTook := time.Since(t)
	t = time.Now()
	ks := s.d.Kernels()
	kernelsTook := time.Since(t)
	s.storeObservation(observation{seq: seq, inst: inst, xkeen: xkeen, facts: f, ks: ks})
	ev := evaluate(f, inst, xkeen, ks)

	sig := ev.signature()
	if s.lastSig != "" && sig != s.lastSig {
		layer.PublishWizard()
	}
	s.lastSig = sig

	var writeTook time.Duration
	if f.Status != configlayer.WizardDone && f.Status != configlayer.WizardHidden && ev.allDone() {
		t = time.Now()
		// Между чтением фактов и записью признака черновик могли поправить, а
		// признак — сбросить или поставить: done — только если факты, перечитанные
		// под замком записи, его подтверждают. Сброс, «Закрыть» и «Скрыть» пишут
		// под тем же замком и сюда не вклиниваются.
		s.tickMu.Lock()
		inst = s.d.Installed()
		fresh := layer.WizardFacts(probe(inst))
		if fresh.Status != configlayer.WizardDone && fresh.Status != configlayer.WizardHidden &&
			evaluate(fresh, inst, s.d.XKeenInstalled(), ks).allDone() {
			if changed, err := layer.MarkWizard(configlayer.WizardDone); err != nil {
				// Повтор — на следующем тике. В тексте ошибки записи нет данных источников.
				log.Printf("setupwizard: отметка «пройдено» не записана: %v", err)
			} else if changed {
				// Статус службы — done; номер записи растёт: наблюдение, начатое до
				// этой записи, базу не перезапишет.
				s.cacheMu.Lock()
				s.clock++
				s.lastWriteAt = s.clock
				s.base, s.baseSet = configlayer.WizardDone, true
				s.cacheMu.Unlock()
			}
		}
		s.tickMu.Unlock()
		writeTook = time.Since(t)
	}

	if took := time.Since(start); took > s.interval() {
		log.Printf("setupwizard: проверка шагов заняла %v (факты %v, ядра %v, запись %v)",
			took, factsTook, kernelsTook, writeTook)
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
