package routing

import (
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
)

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
	// Списки файлов с пометкой drop в черновике скрыты в выборе списка (D-18):
	// выбрать их в правило нельзя, пока удаление не сброшено. Не читается черновик —
	// каталог остаётся полным.
	if st, err := s.d.Layer.ReadState(); err == nil {
		if gd, err := ParseGeodataDraft(st.Draft[GeodataSection]); err == nil && len(gd.Drop) > 0 {
			cat = hideDropped(cat, dropNames(datFilesOf(s.d.Lists), gd))
		}
	}
	return cat, nil
}
