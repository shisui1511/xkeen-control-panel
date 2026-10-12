package sources

import (
	"context"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// maxParallelURLs — сколько подписок из одной вставки запрашивается одновременно:
// каждая стоит до двух запросов, а роутер слабый.
const maxParallelURLs = 3

// plausibleClock — самая ранняя дата, которой доверяют часы роутера. Без NTP
// время может быть 1970-м, и «подписка истекла» было бы ложным (Pitfall 13).
var plausibleClock = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// previewKernels — ядра в порядке опроса и вывода.
var previewKernels = []string{kernelXray, kernelMihomo}

// subCandidate — подписка из предпросмотра: ответы провайдера по установленным
// ядрам и представление для клиента. Хранится под токеном.
type subCandidate struct {
	line int
	url  string
	name string
	meta Meta
	// kernels — только установленные ядра; неустановленное не запрашивалось.
	kernels map[string]*kernelOutcome
	view    PreviewSubscription
	// diagID — идентификатор, под которым записана диагностика ответов; при
	// «Добавить» он становится идентификатором источника, если свободен.
	diagID string
	// releaseDiag снимает удержание диагностики «в полёте».
	releaseDiag func()
}

// hashes — хэши снимков кандидата по ядрам.
func (c *subCandidate) hashes() []string {
	var out []string
	for _, oc := range c.kernels {
		if oc != nil && oc.Ref != nil {
			out = append(out, oc.Ref.Hash)
		}
	}
	return out
}

// release отпускает удержание снимков и диагностики: после регистрации токена их
// держит токен, при отброшенном результате — никто. Повторный вызов безопасен.
func (c *subCandidate) release() {
	if c == nil {
		return
	}
	for _, oc := range c.kernels {
		oc.free()
	}
	if c.releaseDiag != nil {
		c.releaseDiag()
	}
}

// releaseAll отпускает удержание всех кандидатов.
func releaseAll(list []*subCandidate) {
	for _, c := range list {
		c.release()
	}
}

// previewSubscriptions запрашивает каждый URL подписки под установленные ядра.
// HWID один на все запросы панели: у провайдера занят один слот устройства (D-06).
func (s *Service) previewSubscriptions(ctx context.Context, urls []nodes.URLItem) ([]*subCandidate, error) {
	if len(urls) == 0 {
		return nil, nil
	}
	installed := s.d.Installed()
	var kernels []string
	for _, k := range previewKernels {
		if installed.Has(k) {
			kernels = append(kernels, k)
		}
	}
	hwid := ""
	if len(kernels) > 0 {
		hwid = s.d.HWID()
	}
	out := make([]*subCandidate, len(urls))
	errs := make([]error, len(urls))
	sem := make(chan struct{}, maxParallelURLs)
	var wg sync.WaitGroup
	for i, item := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i], errs[i] = s.previewOne(ctx, item, kernels, hwid)
		}()
	}
	wg.Wait()
	err := ctx.Err()
	for _, e := range errs {
		if e != nil {
			err = e
			break
		}
	}
	if err != nil {
		releaseAll(out)
		return nil, err
	}
	return out, nil
}

// previewOne запрашивает одну подписку и строит кандидата.
func (s *Service) previewOne(ctx context.Context, item nodes.URLItem, kernels []string, hwid string) (*subCandidate, error) {
	c := &subCandidate{line: item.Line, url: item.URL, kernels: map[string]*kernelOutcome{}}
	if len(kernels) > 0 {
		res, err := s.fetchAll(ctx, item.URL, hwid, false, kernels)
		if err != nil {
			return nil, err
		}
		c.kernels = res
		// Диагностика ответа по каждому ядру пишется под идентификатором кандидата;
		// он удержан, пока токен не зарегистрирован.
		if c.diagID, err = s.newID(); err != nil {
			c.release()
			return nil, err
		}
		c.releaseDiag = s.blobs.holdDiag(c.diagID)
		for _, k := range kernels {
			if oc := res[k]; oc != nil {
				s.writeDiag(c.diagID, k, oc.fetch, oc.report)
				oc.dropBody()
			}
		}
	}
	var metas []Meta
	for _, k := range previewKernels {
		if oc := c.kernels[k]; oc != nil {
			metas = append(metas, oc.Meta)
		}
	}
	c.meta = mergeMeta(s.d.Now(), metas...)
	c.name = c.meta.Title
	if c.name == "" {
		c.name = hostOf(item.URL)
	}
	c.view = s.subscriptionView(c)
	return c, nil
}

// hostOf — домен адреса; неразбираемый адрес даёт «…».
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "…"
}

// subscriptionView строит представление подписки: счётчики по ядрам, предупреждение
// «истекла» и общая ошибка, когда отказали все установленные ядра.
func (s *Service) subscriptionView(c *subCandidate) PreviewSubscription {
	v := PreviewSubscription{
		Line:      c.line,
		Name:      c.name,
		URLMasked: nodes.MaskURL(c.url),
		Kernels:   make(map[string]PreviewKernel, len(previewKernels)),
		Warnings:  []*nodes.SourceError{},
	}
	var failed []*nodes.SourceError
	for _, k := range previewKernels {
		oc := c.kernels[k]
		pk := PreviewKernel{Installed: oc != nil}
		if oc != nil {
			pk.Stubs, pk.Skipped, pk.Error = oc.Stubs, oc.Skipped, oc.Err
			if oc.Ref != nil {
				pk.Count = oc.Ref.Usable
			}
			if oc.Err != nil {
				failed = append(failed, oc.Err)
			}
		}
		v.Kernels[k] = pk
	}
	if len(failed) > 0 && len(failed) == len(c.kernels) {
		v.Error = failed[0]
	}
	if w := expiredWarning(c.meta, s.d.Now()); w != nil {
		v.Warnings = append(v.Warnings, w)
	}
	return v
}

// expiredWarning — предупреждение «подписка истекла»: только при правдоподобных
// часах роутера (не раньше 2026-01-01). Снимок при этом остаётся рабочим.
func expiredWarning(m Meta, now time.Time) *nodes.SourceError {
	if m.Expire <= 0 || now.Before(plausibleClock) {
		return nil
	}
	exp := time.Unix(m.Expire, 0).UTC()
	if !exp.Before(now) {
		return nil
	}
	return nodes.NewError(nodes.CodeExpired, "date", exp.Format("2006-01-02"))
}

// mergeMeta объединяет метаданные ответов под разные ядра: каждое поле берётся из
// первого ответа, где оно есть; HWIDLocked — если его объявил любой ответ.
func mergeMeta(now time.Time, metas ...Meta) Meta {
	out := Meta{UpdatedAt: now}
	for _, m := range metas {
		if out.Title == "" {
			out.Title = m.Title
		}
		if out.Total == 0 && out.Upload == 0 && out.Download == 0 && out.Expire == 0 {
			out.Upload, out.Download, out.Total, out.Expire = m.Upload, m.Download, m.Total, m.Expire
		}
		if out.IntervalH == 0 {
			out.IntervalH = m.IntervalH
		}
		if out.SupportURL == "" {
			out.SupportURL = m.SupportURL
		}
		if out.WebURL == "" {
			out.WebURL = m.WebURL
		}
		if out.Announce == "" {
			out.Announce = m.Announce
		}
		out.HWIDLocked = out.HWIDLocked || m.HWIDLocked
	}
	return out
}

// liveSource — живое состояние новой подписки: метаданные и по ядру либо ожидающий
// снимок (статус ok), либо причина отказа без снимков. Неустановленные ядра в
// состояние не попадают.
func (c *subCandidate) liveSource(now time.Time) *LiveSource {
	src := &LiveSource{Meta: c.meta, Kernels: map[string]*KernelLive{}}
	for _, k := range previewKernels {
		oc := c.kernels[k]
		if oc == nil {
			continue
		}
		kl := &KernelLive{}
		switch {
		case oc.Ref != nil:
			ref := *oc.Ref
			kl.Pending = &ref
			kl.setStatus(Status{Code: StatusOK}, now)
		case oc.Err != nil:
			kl.setStatus(errorStatus(oc.Err), now)
		default:
			kl.setStatus(Status{Code: StatusNone}, now)
		}
		src.Kernels[k] = kl
	}
	return src
}

// subscriptionIndex — идентичность узла → имя подписки, набор которой его содержит
// (D-14: «есть в подписке X»). Берётся применённый набор ядра, иначе ожидающий;
// подписки — из черновика и применённого состояния. Нечитаемый снимок подписку
// пропускает: пометка — подсказка, а не условие добавления.
func (s *Service) subscriptionIndex() map[string]string {
	idx := map[string]string{}
	st, err := s.d.Layer.ReadState()
	if err != nil {
		return idx
	}
	live, err := readLive(st.Live)
	if err != nil {
		return idx
	}
	names := map[string]string{}
	for _, secs := range []configlayer.Sections{st.Applied, st.Draft} {
		for section, raw := range secs {
			id, ok := sourceID(section)
			if !ok || kindOf(raw) != KindSubscription {
				continue
			}
			cfg, err := parseSubscription(raw)
			if err != nil {
				continue
			}
			names[id] = cfg.Name
		}
	}
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		src := live[id]
		if src == nil || names[id] == "" {
			continue
		}
		for _, kernel := range previewKernels {
			kl := src.Kernels[kernel]
			if kl == nil {
				continue
			}
			ref := kl.Applied
			if ref == nil {
				ref = kl.Pending
			}
			if ref == nil {
				continue
			}
			list, err := s.blobs.get(ref.Hash)
			if err != nil {
				continue
			}
			for _, n := range list {
				if _, seen := idx[n.Identity()]; !seen {
					idx[n.Identity()] = names[id]
				}
			}
		}
	}
	return idx
}
