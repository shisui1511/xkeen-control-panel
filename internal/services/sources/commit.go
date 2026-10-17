package sources

import (
	"log"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// CommitLive — хук фиксации живых данных для Layer.SetLiveCommitter. Конвейер
// вызывает его внутри той же записи файла состояния, что и манифест, после
// успешной записи файлов и перезапуска (и когда план пуст), поэтому состояние
// снимков меняется вместе с применёнными файлами и не раньше них.
//
//   - «Применить» из черновика: ожидающий снимок (pending), по которому собраны
//     файлы этого запуска, становится применённым (applied). Если с тех пор
//     ожидающий снимок заменился более свежим, он остаётся ждать (Open Question 2).
//   - Фоновое обновление (TriggerSourceRefresh): кандидаты оверлея становятся
//     применёнными, ожидающий снимок ядра сбрасывается. Кандидат, чей отпечаток
//     конфигурации не совпадает с секцией запуска, не фиксируется (readCandidates):
//     файлы собраны без него.
//
// После фиксации Live очищается от источников, которых нет ни в черновике, ни в
// применённом состоянии, а ключи candidates и candidate_prints в состояние не
// попадают. Повреждённые живые данные не блокируют «Применить»: фиксация
// пропускается с записью в журнал.
func (s *Service) CommitLive(st *configlayer.State, req configlayer.ApplyRequest, used configlayer.Input) error {
	if st.Live == nil {
		st.Live = configlayer.Sections{}
	}
	cur, err := readLive(st.Live)
	if err != nil {
		log.Printf("[sources] фиксация пропущена: живые данные источников не читаются")
		delete(st.Live, LiveKeyCandidates)
		delete(st.Live, LiveKeyCandidatePrints)
		return nil
	}
	now := s.d.Now()

	switch {
	case req.Trigger == configlayer.TriggerSourceRefresh:
		cands, err := readCandidates(req.LiveOverlay, used.Sections)
		if err != nil {
			log.Printf("[sources] кандидаты обновления не читаются: фиксация пропущена")
			break
		}
		for id, byKernel := range cands {
			src := cur[id]
			if src == nil {
				continue
			}
			for kernel, ref := range byKernel {
				if req.Kernel != "" && req.Kernel != kernel {
					continue
				}
				ref := ref
				s.promote(id, src.kernelLive(kernel), &ref, true, now)
			}
		}
	case req.Source == configlayer.SourceDraft && len(req.Only) == 0:
		usedLive, err := readLive(used.Live)
		if err != nil {
			break
		}
		for id, usedSrc := range usedLive {
			src := cur[id]
			if src == nil {
				continue
			}
			for kernel, usedKL := range usedSrc.Kernels {
				if req.Kernel != "" && req.Kernel != kernel {
					continue
				}
				if usedKL == nil || usedKL.Pending == nil {
					continue
				}
				kl := src.Kernels[kernel]
				// Фиксируется ровно тот снимок, по которому собраны файлы запуска.
				if kl == nil || kl.Pending == nil || kl.Pending.Hash != usedKL.Pending.Hash {
					continue
				}
				ref := *usedKL.Pending
				s.promote(id, kl, &ref, false, now)
			}
		}
	}

	// Источники без секции в черновике и применённом состоянии больше не нужны.
	for id := range cur {
		if _, inDraft := st.Draft[sectionName(id)]; inDraft {
			continue
		}
		if _, inApplied := st.Applied[sectionName(id)]; inApplied {
			continue
		}
		delete(cur, id)
	}
	if err := writeLive(st.Live, cur); err != nil {
		return err
	}
	delete(st.Live, LiveKeyCandidates)
	delete(st.Live, LiveKeyCandidatePrints)
	// Статусы могли измениться («Применить» делает снимки применёнными): уведомления
	// пересобираются после записи состояния, из горутины — слой держит замок хранилища.
	// Туда же уходит уборка: прежние применённые снимки и диагностика удалённых
	// источников больше не нужны, а читать состояние под замком нельзя.
	s.spawn(func() {
		s.rebuildNotices()
		s.collectGarbage()
	})
	return nil
}

// promote делает снимок применённым: разница составов считается по множествам
// ключей старого и нового наборов, ожидающий снимок сбрасывается, статус — ok.
// clearPending — сбросить ожидающий снимок даже другого хэша (обновление пришло
// после него и свежее).
func (s *Service) promote(id string, kl *KernelLive, ref *Ref, clearPending bool, now time.Time) {
	if kl.Applied != nil && kl.Applied.Hash == ref.Hash {
		// Состав не изменился: меняются только время запроса и статус.
		kl.Applied.FetchedAt = ref.FetchedAt
		kl.Delta = Delta{}
	} else {
		kl.Delta = s.delta(id, kl.Applied, ref)
		kl.Applied = ref
	}
	if kl.Pending != nil && (clearPending || kl.Pending.Hash == ref.Hash) {
		kl.Pending = nil
	}
	kl.setStatus(Status{Code: StatusOK}, now)
}

// delta считает, сколько узлов добавилось и убралось между наборами; нечитаемый
// снимок считается пустым набором.
func (s *Service) delta(id string, old, cur *Ref) Delta {
	oldKeys := s.keySet(id, old)
	curKeys := s.keySet(id, cur)
	var d Delta
	for k := range curKeys {
		if !oldKeys[k] {
			d.Added++
		}
	}
	for k := range oldKeys {
		if !curKeys[k] {
			d.Removed++
		}
	}
	return d
}

// keySet — ключи узлов снимка (с учётом схлопывания полных дублей).
func (s *Service) keySet(id string, ref *Ref) map[string]bool {
	if ref == nil {
		return map[string]bool{}
	}
	list, err := s.blobs.get(ref.Hash)
	if err != nil {
		log.Printf("[sources] снимок источника %s не прочитан при подсчёте разницы", id)
		return map[string]bool{}
	}
	uniq, _ := nodes.Uniq(id, list)
	out := make(map[string]bool, len(uniq))
	for _, k := range uniq {
		out[k.Key] = true
	}
	return out
}
