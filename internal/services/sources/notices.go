package sources

import (
	"sort"
	"strconv"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Префикс уведомлений сервиса источников в слое.
const noticePrefix = "source_"

// Виды уведомлений слоя.
const (
	noticeWarning = "warning"
	noticeError   = "error"
)

// rebuildNotices собирает уведомления из живых данных применённых подписок и
// передаёт слою (Layer.SetNotices): отказ провайдера по ядру, сетевая ошибка с
// временем повтора, отложенная запись, отказ ядра. Статус ok уведомления не даёт.
// Выключенный слой не читается. Закрытое пользователем уведомление слой не
// возвращает, пока условие не исчезнет и не появится снова.
func (s *Service) rebuildNotices() {
	// Сборки идут по очереди: состояние читается под замком, поэтому последняя
	// сборка всегда видит самое свежее.
	s.nmu.Lock()
	defer s.nmu.Unlock()
	if !s.d.Layer.Enabled() {
		return
	}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return
	}
	live, err := readLive(st.Live)
	if err != nil {
		return
	}
	now := s.d.Now()
	var out []configlayer.NoticeView
	for _, id := range subscriptionIDs(st, true) {
		src := live[id]
		cfg, _, err := subscriptionConfig(st, id)
		if src == nil || err != nil {
			continue
		}
		name := cfg.Name
		if name == "" {
			name = src.Meta.Title
		}
		if name == "" {
			name = id
		}
		deferred := false
		for _, kernel := range previewKernels {
			kl := src.Kernels[kernel]
			if kl == nil {
				continue
			}
			switch kl.Status.Code {
			case StatusRejected:
				params := map[string]string{"name": name, "code": kl.Status.ErrorCode}
				if d := previousDate(kl); d != "" {
					params["date"] = d
				}
				// Ссылка кабинета проверена parseMeta (только http/https): здесь она
				// передаётся как есть, без повторного построения.
				if src.Meta.WebURL != "" {
					params["web_url"] = src.Meta.WebURL
				}
				out = append(out, configlayer.NoticeView{
					ID: "source_rejected:" + id + ":" + kernel, Kind: noticeWarning, Kernel: kernel, Params: params,
				})
			case StatusNetwork:
				params := map[string]string{
					"name": name,
					"in":   strconv.Itoa(s.minutesUntilNext(cfg, src, now)),
					"code": kl.Status.ErrorCode,
				}
				// Абсолютный срок повтора: остаток считает клиент, иначе «через 5 мин»
				// застывал бы на момент сборки уведомлений.
				if due, ok := s.nextDue(cfg, src); !ok {
					// Обновление выключено: повтора не будет, уведомление не обещает его.
					params["no_retry"] = "1"
				} else if !due.IsZero() {
					params["retry_at"] = due.UTC().Format(time.RFC3339)
				}
				out = append(out, configlayer.NoticeView{
					ID: "source_network:" + id + ":" + kernel, Kind: noticeWarning, Kernel: kernel,
					Params: params,
				})
			case StatusDeferred:
				deferred = true
			case StatusRefused:
				reason := kl.Status.Params["reason"]
				out = append(out, configlayer.NoticeView{
					ID: "source_kernel_refused:" + id + ":" + kernel, Kind: noticeError, Kernel: kernel, Reason: reason,
					Params: map[string]string{"name": name, "kernel": kernel, "reason": reason},
				})
			}
		}
		if deferred {
			out = append(out, configlayer.NoticeView{
				ID: "source_deferred:" + id, Kind: noticeWarning, Params: map[string]string{"name": name},
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	s.d.Layer.SetNotices("source_", out)
}

// previousDate — дата прежнего рабочего снимка ядра (применённого, иначе
// ожидающего); пусто, если снимка не было.
func previousDate(kl *KernelLive) string {
	ref := kl.Applied
	if ref == nil {
		ref = kl.Pending
	}
	if ref == nil || ref.FetchedAt.IsZero() {
		return ""
	}
	return ref.FetchedAt.UTC().Format("2006-01-02")
}

// minutesUntilNext — сколько минут до следующего запроса подписки, не меньше одной.
func (s *Service) minutesUntilNext(cfg SubscriptionConfig, src *LiveSource, now time.Time) int {
	due, ok := s.nextDue(cfg, src)
	if !ok {
		return 0
	}
	d := due.Sub(now)
	if d <= 0 {
		return 1
	}
	return int((d + time.Minute - 1) / time.Minute)
}
