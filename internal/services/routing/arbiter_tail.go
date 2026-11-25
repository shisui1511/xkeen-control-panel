package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Сверка служебного хвоста профиля Mihomo (D-13, гибрид по Open Question 1; D-05,
// D-14). Группе только из провайдера служебные DIRECT/REJECT пишутся в профиль,
// только пока на неё действует включённый лимит block/direct; снимок таких групп
// лежит в Live hidden_tail (LiveKeyHiddenTail) и меняется только здесь. Лимиты
// живут вне черновика, поэтому их правка кнопку «Применить» не показывает: профиль
// догоняет лимиты сам. Арбитр сравнивает желаемый снимок (Holds().Tail/TailAll) с
// Live и при расхождении пересобирает профиль Mihomo из применённого состояния
// запуском RunSectionLive с одним Live-ключом: черновик, его ревизия и число
// изменений не меняются, неприменённые правки пользователя в профиль не попадают.

const (
	// tailRunLimit — предел одного запуска сверки вместе с ожиданием замков.
	tailRunLimit = 3 * time.Minute
	// tailRetryMin и tailRetryMax — пауза повтора после неудачи: 30, 60, 120 … до
	// 600 секунд; успешное применение любого вида её сбрасывает (kickAfterApply).
	tailRetryMin = 30 * time.Second
	tailRetryMax = 600 * time.Second
)

// tailRetry — пауза повтора сверки хвоста. failed — желаемый снимок неудачной
// попытки: другой желаемый снимок (новая правка лимита) паузу не ждёт.
type tailRetry struct {
	mu      sync.Mutex
	failed  json.RawMessage
	delay   time.Duration
	retryAt time.Time
}

// reset снимает паузу.
func (r *tailRetry) reset() {
	r.mu.Lock()
	r.failed, r.delay, r.retryAt = nil, 0, time.Time{}
	r.mu.Unlock()
}

// paused — прошлая попытка с тем же желаемым снимком не удалась, пауза не истекла.
func (r *tailRetry) paused(want json.RawMessage) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed != nil && bytes.Equal(r.failed, tailOrNull(want)) && time.Now().Before(r.retryAt)
}

// backOff откладывает повтор попытки со снимком want.
func (r *tailRetry) backOff(want json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.delay <= 0 {
		r.delay = tailRetryMin
	} else {
		r.delay = min(r.delay*2, tailRetryMax)
	}
	r.failed = tailOrNull(want)
	r.retryAt = time.Now().Add(r.delay)
}

// marshalHiddenTail — снимок хвоста в виде Live hidden_tail: ID групп по
// возрастанию без повторов; пустой снимок — nil (ключа нет).
func marshalHiddenTail(t hiddenTail) json.RawMessage {
	if !t.All && len(t.Groups) == 0 {
		return nil
	}
	groups := append([]string{}, t.Groups...)
	sort.Strings(groups)
	out := hiddenTail{All: t.All, Groups: groups}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}

// tailWant — желаемый снимок хвоста по удержаниям лимитов: группы Tail (только
// со значением true) и TailAll. Пустой снимок — nil.
func tailWant(h services.LimitHolds) json.RawMessage {
	t := hiddenTail{All: h.TailAll}
	for gid, on := range h.Tail {
		if on {
			t.Groups = append(t.Groups, gid)
		}
	}
	return marshalHiddenTail(t)
}

// tailOrNull — значение Live-ключа запуска: пустой снимок — JSON null (ключ
// удаляется).
func tailOrNull(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return json.RawMessage("null")
	}
	return raw
}

// liveTail — снимок хвоста в Live st в том же виде, что tailWant: значение
// разбирается и сериализуется заново, так что сравнение побайтное и не зависит от
// записи значения слоем.
func liveTail(live configlayer.Sections) json.RawMessage {
	return marshalHiddenTail(readHiddenTail(live))
}

// syncTail сверяет служебный хвост профиля Mihomo с удержаниями лимитов; true —
// запускался конвейер (состояние изменилось или могло измениться). Вызывается из
// тика без selMu: клик пользователя не ждёт конвейер.
func (a *Arbiter) syncTail(ctx context.Context, st configlayer.StateView) (ran bool) {
	a.mu.Lock()
	src := a.holds
	a.mu.Unlock()
	if src == nil {
		// Источник удержаний не подключён: сверка сняла бы хвост, который нужен
		// лимитам, уже прочитанным с диска.
		return false
	}
	want := tailWant(src.Holds())
	if bytes.Equal(want, liveTail(st.Live)) {
		a.tail.reset()
		return false
	}
	if a.tail.paused(want) {
		return false
	}
	rctx, cancel := context.WithTimeout(ctx, tailRunLimit)
	defer cancel()
	view, err := a.s.d.Layer.RunSectionLive(rctx, configlayer.SectionLiveRun{
		Trigger: configlayer.TriggerLimitTail,
		Kernel:  configlayer.KernelMihomo,
		Live:    configlayer.Sections{LiveKeyHiddenTail: tailOrNull(want)},
		Wait:    true,
	})
	a.settleTail(ctx, want, view, err)
	return true
}

// settleTail разбирает исход запуска сверки по образцу datWatcher.runMihomo:
// успех снимает прежнее уведомление build_failed:mihomo, сбой проверки, записи или
// перезапуска его ставит; дрейф, занятость и остановка — без уведомления. Любая
// неудача — пауза повтора. В журнал — одна строка с кодом исхода, без имён групп и
// узлов.
func (a *Arbiter) settleTail(ctx context.Context, want json.RawMessage, view configlayer.ApplyView, err error) {
	l := a.s.d.Layer
	code := ""
	switch {
	case ctx.Err() != nil, errors.Is(err, configlayer.ErrStopped):
		// Остановка панели: повторять некому.
		return
	case errors.Is(err, configlayer.ErrDriftBlocked):
		code = configlayer.ResultDriftBlocked
	case errors.Is(err, configlayer.ErrApplyBusy), errors.Is(err, configlayer.ErrKernelBusy):
		code = "busy"
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	case err != nil:
		code = "error"
	case view.Result == nil:
		code = configlayer.ResultInterrupted
	case view.Result.OK:
		st, rerr := l.ReadState()
		if rerr == nil && !bytes.Equal(want, liveTail(st.Live)) {
			// Запуск прошёл, а снимок не зафиксирован: повтор с паузой, иначе
			// каждый тик пересобирал бы профиль.
			code = "not_committed"
			break
		}
		a.tail.reset()
		l.NoteBuildResult(configlayer.KernelMihomo, view)
		log.Printf("[routing] арбитр: сверка служебного хвоста Mihomo: ok")
		return
	default:
		code = view.Result.Code
		switch code {
		case configlayer.ResultBuildFailed, configlayer.ResultWriteFailed, configlayer.ResultRestartFailed:
			// Уведомление build_failed:mihomo на «Конфигурации».
			l.NoteBuildResult(configlayer.KernelMihomo, view)
		}
	}
	a.tail.backOff(want)
	log.Printf("[routing] арбитр: сверка служебного хвоста Mihomo не прошла: %s", code)
}
