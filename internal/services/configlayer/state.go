package configlayer

import "encoding/json"

// Store — заглушка RED-фазы.
type Store struct{ st State }

// OpenStore — заглушка RED-фазы.
func OpenStore(dataDir string, broker *Broker) (*Store, error) {
	return &Store{st: emptyState()}, nil
}

// Snapshot — заглушка RED-фазы.
func (s *Store) Snapshot() State { return s.st.clone() }

// DraftRevision — заглушка RED-фазы.
func (s *Store) DraftRevision() int64 { return s.st.DraftRevision }

// EditDraft — заглушка RED-фазы.
func (s *Store) EditDraft(baseRev int64, section string, value json.RawMessage) (DraftEvent, error) {
	return DraftEvent{}, nil
}
