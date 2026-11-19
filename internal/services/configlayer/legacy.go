package configlayer

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

var ErrLegacyState = errors.New("не удалось сохранить закрытие окна старого слоя")

// LegacyResetState хранит серверное закрытие окна в прежней схеме состояния.
type LegacyResetState struct {
	ClosedAt time.Time `json:"closed_at"`
}

// MarkLegacyClosed фиксирует закрытие на диске до публикации в памяти.
func (s *Store) MarkLegacyClosed(now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return false, err
	}
	if s.st.Legacy != nil {
		return false, nil
	}
	next := s.st.clone()
	next.Legacy = &LegacyResetState{ClosedAt: now.UTC()}
	err := s.writeVersion(next, s.ver+1, func(path string, data []byte, perm os.FileMode) error {
		if err := utils.AtomicWriteFile(path, data, perm); err != nil {
			return err
		}
		dir, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	})
	if err != nil {
		return false, err
	}
	s.commitLocked(next)
	return true, nil
}

// LegacyArchive — доступ к приватным данным, подключаемый при сборке панели.
// Status читает только метаданные. URLs вызывается исключительно для GET legacy.
type LegacyArchive interface {
	Status() LegacySummary
	URLs() ([]string, error)
	ForgetURLs() error
}

func (l *Layer) legacySummary(closed bool) LegacySummary {
	if l.opts.LegacyArchive == nil {
		return LegacySummary{}
	}
	summary := l.opts.LegacyArchive.Status()
	if closed {
		summary.Pending, summary.URLsAvailable = false, false
	}
	return summary
}

// MarkLegacyClosed сохраняет признак и после этого убирает временный список.
// При ошибке уборки признак уже надёжен, а следующий запрос или сверка повторит её.
func (l *Layer) MarkLegacyClosed() (LegacySummary, error) {
	l.legacyMu.Lock()
	defer l.legacyMu.Unlock()
	closed := l.store.Snapshot().Legacy != nil
	summary := l.legacySummary(closed)
	if !summary.Pending && !closed {
		return summary, nil
	}
	changed, err := l.store.MarkLegacyClosed(l.opts.Now())
	if err != nil {
		// Ошибка чтения или записи состояния может содержать данные декодера.
		return LegacySummary{}, ErrLegacyState
	}
	if l.opts.LegacyArchive != nil {
		if err := l.opts.LegacyArchive.ForgetURLs(); err != nil {
			log.Print("[configlayer] список адресов старых подписок пока не удалён")
		}
	}
	summary.Pending, summary.URLsAvailable = false, false
	if changed {
		l.PublishLegacy()
	}
	return summary, nil
}

// Legacy показывает адреса только пока окно ожидает закрытия.
func (l *Layer) Legacy() LegacyView {
	l.legacyMu.Lock()
	defer l.legacyMu.Unlock()
	summary := l.legacySummary(l.store.Snapshot().Legacy != nil)
	view := LegacyView{LegacySummary: summary, URLs: []string{}}
	if summary.Pending && summary.URLsAvailable {
		urls, err := l.opts.LegacyArchive.URLs()
		if err != nil {
			view.URLsAvailable = false
		} else {
			view.URLs = urls
		}
	}
	return view
}

// PublishLegacy передаёт только сигнал перечитать окно.
func (l *Layer) PublishLegacy() {
	l.broker.Publish(Event{Type: EventLegacy, Data: LegacyEvent{Seq: l.legacySeq.Add(1)}})
}

func (l *Layer) retryLegacyURLCleanup() {
	if l.opts.LegacyArchive != nil && l.store.Snapshot().Legacy != nil {
		_ = l.opts.LegacyArchive.ForgetURLs()
	}
}
