package configlayer

import (
	"errors"
	"sync"
)

// Имена событий слоя.
const (
	EventSnapshot  = "snapshot"
	EventDraft     = "draft"
	EventFiles     = "files"
	EventApplyStep = "apply_step"
	EventApplyDone = "apply_done"
	EventNotices   = "notices"
)

const (
	// maxSubscribers — предел одновременных подписчиков шины (по вкладке на
	// подписчика; на роутере держать больше смысла нет).
	maxSubscribers = 16
	// subscriberBuffer — буфер канала подписчика. Переполнение означает, что
	// клиент не успевает читать, и он отключается.
	subscriberBuffer = 32
)

// ErrTooManySubscribers — достигнут предел подписчиков шины.
var ErrTooManySubscribers = errors.New("configlayer: too many event subscribers")

// ErrBrokerClosed — шина уже закрыта.
var ErrBrokerClosed = errors.New("configlayer: event broker closed")

// Event — событие шины.
type Event struct {
	Type string
	Data any
}

// DraftEvent — данные события draft: новая ревизия черновика и число
// неприменённых изменений (считает сервер).
type DraftEvent struct {
	DraftRevision int64 `json:"draft_revision"`
	DraftChanges  int   `json:"draft_changes"`
}

// Broker — шина событий слоя для SSE-подписчиков.
//
// Publish не блокируется: подписчик с полным буфером отключается (его канал
// закрывается), остальные продолжают получать события. Отключённый клиент
// переподключается и перечитывает состояние целиком (D-06). Все операции идут
// под одним мьютексом, вложенных замков нет.
type Broker struct {
	mu     sync.Mutex
	subs   map[uint64]chan Event
	next   uint64
	closed bool
}

// NewBroker создаёт пустую шину.
func NewBroker() *Broker {
	return &Broker{subs: make(map[uint64]chan Event)}
}

// Subscribe регистрирует подписчика. Возвращает канал событий и
// идемпотентную функцию отписки, которая закрывает канал.
func (b *Broker) Subscribe() (<-chan Event, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, nil, ErrBrokerClosed
	}
	if len(b.subs) >= maxSubscribers {
		return nil, nil, ErrTooManySubscribers
	}

	id := b.next
	b.next++
	ch := make(chan Event, subscriberBuffer)
	b.subs[id] = ch

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
	return ch, cancel, nil
}

// Publish рассылает событие всем подписчикам без блокировки. Вызов на nil
// шине безопасен (Store работает и без шины).
func (b *Broker) Publish(ev Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	for id, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Буфер полон: клиент отстал и будет переподключён.
			delete(b.subs, id)
			close(ch)
		}
	}
}

// Close закрывает шину и каналы всех подписчиков. Повторный вызов безопасен.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		delete(b.subs, id)
		close(ch)
	}
}
