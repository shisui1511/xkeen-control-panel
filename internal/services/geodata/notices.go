package geodata

import (
	"sort"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Виды уведомлений слоя (как у остальных сервисов).
const (
	noticeWarning = "warning"
	noticeError   = "error"
)

// cleanedNoticeTTL — сколько висит сообщение «запись убрана» (D-01).
const cleanedNoticeTTL = 7 * 24 * time.Hour

// buildNotices собирает уведомления раздела по состоянию менеджера:
//   - geodata_update:<файл> — файл не обновлён (kept или failed), причина в Reason;
//   - geodata_update:geoipset — сбой списков GeoIPSET;
//   - geodata_update:run — сорван весь запуск (с временем повтора, если он назначен);
//   - geodata_update:retry — повтор тоже не удался;
//   - geodata_cleanup:<файл> — панель убрала временную запись набора;
//   - geodata_include:<файл> — запись не убрана после скачивания, нужно довести;
//   - geodata_delete:<файл> — удаление источника не завершено;
//   - geodata_cron_two — базы обновляют и панель, и cron XKeen.
func buildNotices(st State, now time.Time) []configlayer.NoticeView {
	var out []configlayer.NoticeView
	for file, r := range st.Results {
		if r.Status != StatusKept && r.Status != StatusFailed {
			continue
		}
		out = append(out, configlayer.NoticeView{
			ID: "geodata_update:" + file, Kind: noticeWarning, Reason: r.Reason,
			Params: map[string]string{"file": file, "reason": r.Reason},
		})
	}
	if g := st.GeoIPSet; g != nil && (g.Status == StatusKept || g.Status == StatusFailed) {
		out = append(out, configlayer.NoticeView{
			ID: "geodata_update:geoipset", Kind: noticeWarning, Reason: g.Reason,
			Params: map[string]string{"file": "GeoIPSET", "reason": g.Reason},
		})
	}
	if lr := st.LastRun; lr != nil && lr.Outcome == outcomeFailed {
		params := map[string]string{"reason": lr.Reason}
		if st.RetryAt.After(now) {
			params["retry_at"] = st.RetryAt.UTC().Format(time.RFC3339)
		}
		out = append(out, configlayer.NoticeView{
			ID: "geodata_update:run", Kind: noticeError, Reason: lr.Reason, Params: params,
		})
	}
	if st.RetryFailed {
		reason := ""
		if st.LastRun != nil {
			reason = st.LastRun.Reason
		}
		out = append(out, configlayer.NoticeView{
			ID: "geodata_update:retry", Kind: noticeWarning, Reason: reason,
			Params: map[string]string{"reason": reason},
		})
	}
	for file, at := range st.Cleaned {
		if now.Sub(at) >= cleanedNoticeTTL {
			continue
		}
		out = append(out, configlayer.NoticeView{
			ID: "geodata_cleanup:" + file, Kind: noticeWarning, Params: map[string]string{"file": file},
		})
	}
	for file, reason := range st.IncludeStuck {
		out = append(out, configlayer.NoticeView{
			ID: "geodata_include:" + file, Kind: noticeWarning, Reason: reason,
			Params: map[string]string{"file": file, "reason": reason},
		})
	}
	for _, d := range st.PendingDeletes {
		out = append(out, configlayer.NoticeView{
			ID: "geodata_delete:" + d.File, Kind: noticeWarning, Reason: d.Reason,
			Params: map[string]string{"file": d.File, "reason": d.Reason},
		})
	}
	if st.CronTwo {
		out = append(out, configlayer.NoticeView{
			ID: "geodata_cron_two", Kind: noticeWarning, Params: map[string]string{"schedule": st.CronSchedule},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// rebuildNotices пересобирает весь набор уведомлений раздела и передаёт его слою:
// набор с префиксом geodata_ целиком принадлежит этому сервису. Выключенный слой не
// трогается (его уведомления никто не читает; включение вызовет пересборку).
func (s *Service) rebuildNotices() {
	// Сборки идут по очереди: последняя всегда видит самое свежее состояние.
	s.nmu.Lock()
	defer s.nmu.Unlock()
	if !s.d.Layer.Enabled() {
		return
	}
	s.mu.Lock()
	st := s.st.clone()
	s.mu.Unlock()
	s.d.Layer.SetNotices("geodata_", buildNotices(st, s.d.Now()))
}
