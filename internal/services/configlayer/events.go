package configlayer

// Имена событий слоя.
const (
	EventSnapshot  = "snapshot"
	EventDraft     = "draft"
	EventFiles     = "files"
	EventApplyStep = "apply_step"
	EventApplyDone = "apply_done"
	EventNotices   = "notices"
)

// Event — событие шины.
type Event struct {
	Type string
	Data any
}

// DraftEvent — данные события draft.
type DraftEvent struct {
	DraftRevision int64 `json:"draft_revision"`
	DraftChanges  int   `json:"draft_changes"`
}

// Broker — заглушка RED-фазы.
type Broker struct{}

// NewBroker — заглушка RED-фазы.
func NewBroker() *Broker { return &Broker{} }

// Subscribe — заглушка RED-фазы.
func (b *Broker) Subscribe() (<-chan Event, func(), error) {
	return make(chan Event), func() {}, nil
}

// Publish — заглушка RED-фазы.
func (b *Broker) Publish(ev Event) {}

// Close — заглушка RED-фазы.
func (b *Broker) Close() {}
