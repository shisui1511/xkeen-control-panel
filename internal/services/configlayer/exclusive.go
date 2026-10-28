package configlayer

import (
	"context"
	"errors"
	"sync"
)

// Держатели исключительного замка применения: кто сейчас занимает его вне конвейера.
const (
	// HolderGeodataUpdate — менеджер geodata обновляет базы (`xkeen -ug`).
	HolderGeodataUpdate = "geodata_update"
	// HolderGeodataRemove — менеджер geodata убирает запись и файл базы после «Применить».
	HolderGeodataRemove = "geodata_remove"
)

// ErrGeodataBusy — замок применения держит менеджер geodata: «Применить», выключение
// слоя и второе обновление баз по кнопке отказывают этой ошибкой вместо ErrApplyBusy.
var ErrGeodataBusy = errors.New("geodata_busy")

// BeginExclusive берёт тот же замок, что и «Применить» (applyMu, затем замок жизненного
// цикла: порядок такой же, как у Pipeline.TryBegin), и записывает держателя.
//
// user=true — запуск по кнопке: занятый замок даёт отказ сразу (ErrGeodataBusy, если
// замок держит менеджер geodata, иначе ErrApplyBusy или ErrKernelBusy). user=false —
// фоновый запуск (шаг после «Применить»): ждёт конца идущего применения, ожидание
// прерывается контекстом ctx и остановкой слоя.
//
// release снимает держателя и оба замка, затем публикует событие geodata; повторный
// вызов безопасен. Слой выключен — ErrDisabled. Пустой holder отклоняется: по нему
// остальные пути узнают причину занятости.
func (l *Layer) BeginExclusive(ctx context.Context, user bool, holder string) (release func(), err error) {
	if !l.Enabled() {
		return nil, ErrDisabled
	}
	if holder == "" {
		return nil, errors.New("configlayer: не задан держатель исключительного замка")
	}
	if user && l.BusyHolder() != "" {
		return nil, ErrGeodataBusy
	}
	// Ожидание фонового запуска прерывается и остановкой слоя.
	waitCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(l.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	unlock, err := l.pipeline.TryBegin(waitCtx, user)
	if err != nil {
		if l.ctx.Err() != nil {
			return nil, ErrStopped
		}
		return nil, l.mapBusy(err)
	}
	l.mu.Lock()
	l.holder = holder
	l.mu.Unlock()
	l.PublishGeodata(0)
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.holder = ""
			l.mu.Unlock()
			unlock()
			l.PublishGeodata(0)
		})
	}, nil
}

// RunExclusive выполняет fn под исключительным замком (BeginExclusive) и снимает его
// по возвращении. Ошибку fn возвращает как есть.
func (l *Layer) RunExclusive(ctx context.Context, user bool, holder string, fn func(ctx context.Context) error) error {
	release, err := l.BeginExclusive(ctx, user, holder)
	if err != nil {
		return err
	}
	defer release()
	return fn(ctx)
}

// BusyHolder — кто держит исключительный замок (HolderGeodata*); пусто — никто.
func (l *Layer) BusyHolder() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.holder
}

// mapBusy превращает отказ занятого замка в ErrGeodataBusy, если замок держит менеджер
// geodata: пользователь видит причину («идёт обновление баз»), а не «применение уже
// идёт». Прочие ошибки возвращаются как есть.
func (l *Layer) mapBusy(err error) error {
	if errors.Is(err, ErrApplyBusy) && l.BusyHolder() != "" {
		return ErrGeodataBusy
	}
	return err
}

// PublishGeodata сообщает клиентам о состоянии менеджера geodata: счётчик событий и
// держатель замка. Построчный вывод -ug по шине слоя не идёт. Выключенный слой молчит.
func (l *Layer) PublishGeodata(seq uint64) {
	if !l.Enabled() {
		return
	}
	l.broker.Publish(Event{Type: EventGeodata, Data: GeodataEvent{Seq: seq, Holder: l.BusyHolder()}})
}
