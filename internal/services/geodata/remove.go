package geodata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Причины незавершённого удаления (PendingDelete.Reason), которые ставит панель;
// остальные причины — коды lists (comments_lost, geodata_changed, xkeen_json_invalid).
const (
	// removeReasonFailed — файл dat не удалён (ошибка файловой системы).
	removeReasonFailed = "remove_failed"
	// removeReasonWrite — запись в xkeen.json не удалась без кода причины.
	removeReasonWrite = "write_failed"
)

// AfterApply — шаг «убрать запись geodata и файл» после «Применить» (D-18, D-37).
// Регистрируется в слое как Hooks.AfterApply. К моменту вызова конфиги обоих ядер уже
// записаны без ссылок на файл; шаг идёт под исключительным замком слоя (держатель
// geodata_remove) и заданием менеджера, поэтому `-ug` не успевает вклиниться между его
// началом и удалением файла, а установка jq ждётся. Пометки читаются из применённого
// состояния внутри замка: за время ожидания замка состояние могло смениться.
//
// Шаг учитывается в горутинах сервиса (track): Stop дожидается его конца, контекст шага
// отменяется остановкой сервиса, поэтому после возврата Stop xkeen.json, файлы dat и
// пометка удаления не меняются.
func (s *Service) AfterApply(ctx context.Context, _ configlayer.AfterApplyInfo) {
	if !s.track() {
		return
	}
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(s.ctx, cancel)()
	if !s.hasAppliedDrops() {
		return
	}
	release, err := s.beginExclusiveJob(ctx, false, configlayer.HolderGeodataRemove, jobRemove)
	if err == nil {
		err = s.removeApplied(ctx)
		s.changed()
		release()
	}
	if err != nil && !errors.Is(err, configlayer.ErrDisabled) && !errors.Is(err, configlayer.ErrStopped) &&
		s.ctx.Err() == nil {
		log.Printf("[geodata] шаг удаления источников после «Применить»: %v", err)
	}
}

// appliedDrops читает пометки удаления из применённого состояния слоя.
func (s *Service) appliedDrops() (routing.GeodataDraft, error) {
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return routing.GeodataDraft{}, err
	}
	return routing.ParseGeodataDraft(st.Applied[routing.GeodataSection])
}

// hasAppliedDrops — быстрая проверка до замка: в применённом состоянии есть пометки.
func (s *Service) hasAppliedDrops() bool {
	d, err := s.appliedDrops()
	return err == nil && len(d.Drop) > 0
}

// removeApplied убирает запись и файл для каждой применённой пометки, затем снимает
// обработанные пометки из применённого состояния и черновика.
func (s *Service) removeApplied(ctx context.Context) error {
	d, err := s.appliedDrops()
	if err != nil {
		return err
	}
	if len(d.Drop) == 0 {
		return nil
	}
	done := make(map[string]bool, len(d.Drop))
	for _, drop := range d.Drop {
		if ctx.Err() != nil {
			break
		}
		s.removeOne(drop.File)
		done[drop.File] = true
	}
	if len(done) == 0 {
		return ctx.Err()
	}
	settle := func(raw json.RawMessage) (json.RawMessage, error) {
		cur, err := routing.ParseGeodataDraft(raw)
		if err != nil {
			return nil, err
		}
		keep := make([]routing.GeodataDrop, 0, len(cur.Drop))
		for _, x := range cur.Drop {
			if !done[x.File] {
				keep = append(keep, x)
			}
		}
		if len(keep) == 0 {
			return nil, nil
		}
		cur.Drop = keep
		return json.Marshal(cur)
	}
	if err := s.d.Layer.SettleSection(routing.GeodataSection, func(applied, draft json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		a, err := settle(applied)
		if err != nil {
			return nil, nil, err
		}
		dr, err := settle(draft)
		if err != nil {
			return nil, nil, err
		}
		return a, dr, nil
	}); err != nil {
		return fmt.Errorf("снятие пометки удаления: %w", err)
	}
	s.d.Layer.InvalidateChecks()
	return ctx.Err()
}

// removeOne доводит удаление одного источника: чужие ссылки блокируют (Pitfall 13:
// сканирование повторяется прямо перед шагом), затем убирается запись geodata, затем
// файл. Не вышло — запись в pending_deletes с причиной, пометка всё равно снимается:
// дальше удаление доводит кнопка «Довести удаление».
func (s *Service) removeOne(file string) {
	names := s.removeNames(file)
	refs, _ := s.ForeignRefs(file)
	hadEntry, reason := s.entryNames(names)
	if reason != "" {
		s.setPendingDelete(file, reason, true)
		return
	}
	if len(refs) > 0 {
		s.setPendingDelete(file, ErrForeignRef.Code, len(hadEntry) > 0)
		return
	}
	if reason, ok := s.removeEntries(hadEntry, "", false); !ok {
		s.setPendingDelete(file, reason, true)
		return
	}
	if err := removeDatFiles(s.d.Index.Dir(), names, s.d.AllowedRoots); err != nil {
		log.Printf("[geodata] удаление файла %s: %v", file, err)
		s.setPendingDelete(file, removeReasonFailed, false)
		return
	}
	s.finishRemoved(file, names)
}

// entryNames — имена из names, для которых в xkeen.json есть запись geodata. Нет
// файла xkeen.json — записей нет. Файл не прочитан или не разобран — причина
// xkeen_json_invalid: запись, которую не видно, убрать нельзя, а файл при ней
// удалять нельзя (XKeen скачал бы его заново).
func (s *Service) entryNames(names []string) ([]string, string) {
	st, err := s.d.Store.Status()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ""
		}
		log.Printf("[geodata] чтение записей xkeen.json: %v", err)
		return nil, lists.ErrXKeenJSONInvalid.Code
	}
	var out []string
	for _, n := range names {
		if anyEntryNamed(st, n) {
			out = append(out, n)
		}
	}
	return out, ""
}

// removeEntries убирает записи geodata с этими именами. base относится к первой правке
// (отпечаток показанного предпросмотра), остальные идут без привязки. Не удалось —
// код причины и false.
func (s *Service) removeEntries(names []string, base string, acceptCommentLoss bool) (string, bool) {
	for i, n := range names {
		b := ""
		if i == 0 {
			b = base
		}
		err := s.d.Store.Apply(lists.GeodataOp{Kind: lists.OpRemove, File: n}, b, acceptCommentLoss)
		if err == nil {
			continue
		}
		if lists.ErrorCode(err) == lists.ErrEntryNotFound.Code {
			// Запись убрали вручную между проверкой и правкой: цель достигнута.
			continue
		}
		code := lists.ErrorCode(err)
		if code == "" {
			log.Printf("[geodata] удаление записи %s из xkeen.json: %v", n, err)
			code = removeReasonWrite
		}
		return code, false
	}
	return "", true
}

// setPendingDelete запоминает незавершённое удаление (или обновляет причину у того же
// файла) и пересобирает уведомления на ближайшем changed.
func (s *Service) setPendingDelete(file, reason string, entry bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pd := PendingDelete{File: file, Reason: reason, Entry: entry, At: s.d.Now()}
	for i, p := range s.st.PendingDeletes {
		if p.File == file {
			s.st.PendingDeletes[i] = pd
			return
		}
	}
	s.st.PendingDeletes = append(s.st.PendingDeletes, pd)
}

// pendingDelete — незавершённое удаление по имени файла.
func (s *Service) pendingDelete(file string) (PendingDelete, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.st.PendingDeletes {
		if p.File == file {
			return p, true
		}
	}
	return PendingDelete{}, false
}

// finishRemoved убирает следы удалённого источника из состояния менеджера: ожидание
// удаления, ожидание включения, застрявшую уборку, итог запуска, отметку «вне
// панели», хэш и сигнатуру.
func (s *Service) finishRemoved(file string, names []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gone := map[string]bool{file: true}
	for _, n := range names {
		gone[n] = true
	}
	kept := s.st.PendingDeletes[:0:0]
	for _, p := range s.st.PendingDeletes {
		if !gone[p.File] {
			kept = append(kept, p)
		}
	}
	s.st.PendingDeletes = kept
	for n := range gone {
		s.dropPendingLocked(n)
		delete(s.st.IncludeStuck, n)
		delete(s.st.Results, n)
		delete(s.st.Outside, n)
		delete(s.st.Hashes, n)
		delete(s.st.Sigs, n)
	}
}

// PreviewFinishDelete — предпросмотр правки xkeen.json для «Довести удаление»: у
// источника нет незавершённого удаления — not_pending. Убираются записи всех имён
// пары, как в FinishDelete. Записи уже нет (или она не нужна) — пустая правка, до и
// после совпадают.
func (s *Service) PreviewFinishDelete(file string) (PreviewView, error) {
	p, ok := s.pendingDelete(file)
	if !ok {
		return PreviewView{}, ErrNotPending
	}
	names := s.removeNames(p.File)
	entries, _ := s.entryNames(names)
	if len(entries) == 0 {
		entries = []string{p.File}
	}
	prev, err := s.d.Store.PreviewRemoveAll(entries)
	if err != nil {
		// Записи уже нет: правки нет, показывается файл как есть.
		if lists.ErrorCode(err) == lists.ErrEntryNotFound.Code && prev.Before != "" {
			return PreviewView{Before: prev.Before, After: prev.Before, CommentsKept: true, Base: prev.Base}, nil
		}
		return PreviewView{}, err
	}
	return PreviewView{Before: prev.Before, After: prev.After, CommentsKept: prev.CommentsKept, Base: prev.Base}, nil
}

// FinishDelete — «Довести удаление»: работает без черновика, под исключительным
// замком по кнопке (занят — geodata_busy). Чужие ссылки по-прежнему блокируют
// (сканирование повторяется, Pitfall 13). Запись убирается с base из предпросмотра и
// подтверждением потери комментариев; файлы — после записи. Ошибка оставляет
// удаление в pending_deletes с новой причиной.
func (s *Service) FinishDelete(ctx context.Context, file, base string, acceptCommentLoss bool) error {
	release, err := s.beginExclusiveJob(ctx, true, configlayer.HolderGeodataRemove, jobRemove)
	if err != nil {
		return err
	}
	defer func() {
		s.changed()
		release()
	}()
	p, ok := s.pendingDelete(file)
	if !ok {
		return ErrNotPending
	}
	names := s.removeNames(p.File)
	refs, _ := s.ForeignRefs(p.File)
	if len(refs) > 0 {
		return &ForeignRefError{Refs: refs}
	}
	entries, reason := s.entryNames(names)
	if reason != "" {
		s.setPendingDelete(p.File, reason, true)
		return &lists.CodedError{Code: reason}
	}
	if reason, ok := s.removeEntries(entries, base, acceptCommentLoss); !ok {
		s.setPendingDelete(p.File, reason, true)
		return &lists.CodedError{Code: reason}
	}
	if err := removeDatFiles(s.d.Index.Dir(), names, s.d.AllowedRoots); err != nil {
		s.setPendingDelete(p.File, removeReasonFailed, false)
		return fmt.Errorf("%w: %v", &lists.CodedError{Code: removeReasonFailed}, err)
	}
	s.finishRemoved(p.File, names)
	s.d.Layer.InvalidateChecks()
	return nil
}

// removeDatFiles удаляет файлы dat по именам; первая ошибка прерывает обход, но
// остальные имена всё равно пробуются (оба имени пары убираются, если возможно).
// roots — корни путей панели, внутри которых должен лежать каталог dat.
func removeDatFiles(dir string, names []string, roots []string) error {
	var first error
	for _, n := range names {
		if err := removeDatFile(dir, n, roots); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// removeDatFile удаляет один именованный файл dat или симлинк в каталоге dir (D-19,
// T-146.1-21, T-146.1-52). Имя проходит `^[A-Za-z0-9._-]+\.dat$`, каталог — проверку
// utils.PathValidator по корням путей панели (roots, cfg.AllowedRoots).
//
// Проверяется именно каталог, а не путь файла: Validate разворачивает симлинки, и по
// пути симлинка-файла он вернул бы путь его цели, то есть os.Remove удалил бы цель вместо
// ссылки. Развёрнутый путь каталога — основа пути файла. Обычный файл или симлинк
// удаляется одним os.Remove: симлинк удаляется сам, цель за пределами каталога не
// затрагивается; каталог и прочее — отказ, рекурсивного удаления нет. Нет файла — не
// ошибка.
//
// Ограничение: каталог dat, вынесенный симлинком за корни путей панели (например на
// другой раздел), удаления не даёт — отказ, пометка остаётся в pending_deletes с причиной
// remove_failed, файл на месте. Выход — добавить целевой каталог в allowed_roots настроек
// панели.
func removeDatFile(dir, name string, roots []string) error {
	if !datNameRe.MatchString(name) {
		return fmt.Errorf("недопустимое имя файла %q", name)
	}
	root, err := utils.NewPathValidator(roots).Validate(dir)
	if err != nil {
		return fmt.Errorf("удаление вне допустимых корней: %w", err)
	}
	path := filepath.Join(root, name)
	if filepath.Dir(path) != root {
		return fmt.Errorf("файл %q вне каталога dat", name)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%q не файл и не симлинк", name)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
