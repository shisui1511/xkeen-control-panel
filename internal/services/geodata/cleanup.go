package geodata

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// datReady — файл набора появился: лежит под своим именем или вторым, размером не
// меньше MinDatSize и разбирается как dat (критерий RESEARCH «Двухшаговое включение»).
func (s *Service) datReady(file string) bool {
	path := s.datPath(file)
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() < MinDatSize {
		return false
	}
	_, err = s.d.Index.Tags(filepath.Base(path))
	return err == nil
}

// cleanupPending убирает временные записи стандартных наборов, файлы которых уже
// скачаны (D-01). Запись ищется по паре файл и адрес: запись с тем же именем и другим
// адресом (правка вручную) не трогается. Не удалось убрать — файл xkeen.json остаётся
// как есть, у источника include_stuck с кодом причины (D-01, Pitfall 9) и уведомление
// geodata_include; «Довести включение» делает владелец. После успеха следующий
// `xkeen -ug` качает файл один раз (стандартная ветка XKeen).
//
// Вызывается сразу после запуска `-ug` из панели (под замком слоя) и тиком — для
// запусков по cron и SSH.
func (s *Service) cleanupPending(ctx context.Context) {
	s.ops.cleanMu.Lock()
	defer s.ops.cleanMu.Unlock()

	s.mu.Lock()
	pending := append([]PendingEnable(nil), s.st.PendingEnables...)
	s.mu.Unlock()
	if len(pending) == 0 {
		return
	}

	dirty := false
	for _, p := range pending {
		if ctx.Err() != nil {
			break
		}
		if !s.datReady(p.File) {
			continue
		}
		err := s.d.Store.Apply(lists.GeodataOp{Kind: lists.OpRemove, File: p.File, MatchURL: p.URL}, "", false)
		code := lists.ErrorCode(err)
		switch {
		case err == nil:
			s.mu.Lock()
			s.dropPendingLocked(p.File)
			s.st.Cleaned[p.File] = s.d.Now()
			delete(s.st.IncludeStuck, p.File)
			s.mu.Unlock()
			dirty = true
		case code == lists.ErrEntryNotFound.Code:
			// Записи с этой парой нет. Нет и записи с этим именем — убирать нечего
			// (её убрали вручную), ожидание закрывается без уведомления; есть запись с
			// другим адресом — файл не трогается.
			if st, serr := s.d.Store.Status(); serr == nil && !hasEntryNamed(st.Entries, p.File) {
				s.mu.Lock()
				s.dropPendingLocked(p.File)
				delete(s.st.IncludeStuck, p.File)
				s.mu.Unlock()
				dirty = true
			} else if s.markStuck(p.File, code) {
				dirty = true
			}
		case code == lists.ErrGeodataChanged.Code, code == lists.ErrCommentsLost.Code, code == lists.ErrXKeenJSONInvalid.Code:
			if s.markStuck(p.File, code) {
				dirty = true
			}
		default:
			log.Printf("[geodata] уборка временной записи %s: %v", p.File, err)
		}
	}
	if dirty {
		s.changed()
	}
}

// markStuck отмечает у файла застрявшую уборку; true — состояние изменилось.
func (s *Service) markStuck(file, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.IncludeStuck[file] == code {
		return false
	}
	s.st.IncludeStuck[file] = code
	return true
}
