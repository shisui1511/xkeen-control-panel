package routing

import (
	"context"
	"errors"

	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

// GeodataPreview — предпросмотр «Добавить в XKeen»: файл настроек XKeen до и
// после вставки набора и признак сохранения комментариев.
type GeodataPreview struct {
	Before string `json:"before"`
	After  string `json:"after"`
	// CommentsKept — false: вставка без потерь невозможна, запись удалит
	// комментарии и требует подтверждения (accept_comment_loss).
	CommentsKept bool `json:"comments_kept"`
}

// Catalog возвращает каталог списков роутера с состоянием geodata XKeen (поле
// geodata). Индекс списков не читается — catalog_unavailable (InvalidInputError).
// Состояние geodata при ошибке чтения xkeen.json остаётся пустым с причиной из
// кодов пакета lists (invalid_json, invalid_structure): каталог при этом полезен.
func (s *Service) Catalog() (lists.CatalogView, error) {
	if s.d.Lists == nil {
		return lists.CatalogView{}, invalid("catalog", reasonCatalogUnavailable)
	}
	status := lists.GeodataStatus{Entries: []lists.GeodataEntry{}, GeofileUpdate: true}
	if s.d.Geodata != nil {
		st, err := s.d.Geodata.Status()
		if err != nil && st.Reason == "" {
			st.Reason = lists.ErrorCode(err)
		}
		if st.Entries == nil {
			st.Entries = []lists.GeodataEntry{}
		}
		status = st
	}
	cat, err := lists.Resolve(s.d.Lists, status.Entries)
	if err != nil {
		return lists.CatalogView{}, invalid("catalog", reasonCatalogUnavailable)
	}
	cat.Geodata = status
	return cat, nil
}

// GeodataPreview строит предпросмотр записи набора из встроенного каталога в
// xkeen.json. Файл не меняется. Набор задаётся только идентификатором каталога.
func (s *Service) GeodataPreview(setID string) (GeodataPreview, error) {
	if s.d.Geodata == nil {
		return GeodataPreview{}, lists.ErrUpdateUnavail
	}
	before, after, kept, err := s.d.Geodata.PreviewAdd(setID)
	if err != nil {
		return GeodataPreview{}, err
	}
	return GeodataPreview{Before: before, After: after, CommentsKept: kept}, nil
}

// GeodataAdd дописывает набор из встроенного каталога в xkeen.json (с резервной
// копией) и при updateNow запускает xkeen -ug. Состояние geodata после записи
// возвращается и вместе с ошибкой lists.ErrUpdateFailed: запись к этому моменту
// уже сделана.
func (s *Service) GeodataAdd(ctx context.Context, setID string, acceptCommentLoss, updateNow bool) (lists.GeodataStatus, error) {
	if s.d.Geodata == nil {
		return lists.GeodataStatus{}, lists.ErrUpdateUnavail
	}
	// Набор вне каталога — ошибка запроса и отвечает раньше условий окружения
	// (jq, обновление geo-файлов): адрес от клиента сюда не доходит никогда.
	if _, _, _, err := s.d.Geodata.PreviewAdd(setID); errors.Is(err, lists.ErrSetUnknown) {
		return lists.GeodataStatus{}, err
	}
	addErr := s.d.Geodata.Add(ctx, setID, acceptCommentLoss, updateNow)
	if addErr != nil && lists.ErrorCode(addErr) != lists.ErrUpdateFailed.Code {
		return lists.GeodataStatus{}, addErr
	}
	// Запись сделана: списки набора могли появиться на роутере, мемо проверок
	// черновика (list_not_found) устарело.
	s.d.Layer.InvalidateChecks()
	st, err := s.d.Geodata.Status()
	if err != nil && addErr == nil {
		return lists.GeodataStatus{}, err
	}
	if st.Entries == nil {
		st.Entries = []lists.GeodataEntry{}
	}
	return st, addErr
}
