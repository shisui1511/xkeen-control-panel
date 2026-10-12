//go:build router

package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Обновление подписок по расписанию и по «Обновить»: сроки и повторы, статусы и
// уведомления по ядрам, отложенная запись при дрейфе, сборка после установки ядра.
// Сеть настоящая: клиент панели с наблюдателем заголовков (ответов он не подменяет).
// Сценарии с живым провайдером идут при заданном XCP_RT_SUBSCRIPTION_URL и Xray
// устройства; сетевые отказы воспроизводятся на домене зоны .invalid, который не
// разрешается ни одним резолвером. Вердикт — файлы на диске, живые данные слоя и
// число настоящих запросов.

const rtDeadURL = "https://xcp-provider.invalid/sub"

// rtSubURL — адрес настоящей подписки из окружения запуска; пусто — сценарий
// пропускается.
func rtSubURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("XCP_RT_SUBSCRIPTION_URL")
	if u == "" {
		t.Skip("нет XCP_RT_SUBSCRIPTION_URL: настоящая подписка не задана")
	}
	return u
}

// rtSeedApplied кладёт подписку в применённое состояние вместе с живыми данными
// (снимки по ядрам заданы в src): правка черновика и «Применить».
func rtSeedApplied(t *testing.T, e *rtEnv, id, subURL string, interval Interval, enabled bool, src *LiveSource) {
	t.Helper()
	section, err := json.Marshal(SubscriptionConfig{
		Kind: KindSubscription, ID: id, Name: "Провайдер", URL: subURL, Enabled: enabled,
		Exclude: []string{}, Interval: interval, AddedAt: e.clock.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	src.Meta.UpdatedAt = e.clock.Now()
	_, err = e.layer.EditDraftBatch(e.rev(t), map[string]json.RawMessage{sectionName(id): section}, func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		cur[id] = src
		return writeLive(l, cur)
	})
	if err != nil {
		t.Fatalf("EditDraftBatch: %v", err)
	}
	if done := e.applyAndWait(t); done.Result == nil || !done.Result.OK {
		t.Fatalf("применение подписки: %+v", done.Result)
	}
}

// rtAppliedRef — живые данные ядра с применённым снимком и статусом ok.
func rtAppliedRef(e *rtEnv, ref *Ref) *KernelLive {
	return &KernelLive{Applied: ref, Status: Status{Code: StatusOK, Since: e.clock.Now()}}
}

// rtLiveOf читает живые данные источника.
func rtLiveOf(t *testing.T, e *rtEnv, id string) *LiveSource {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	live, err := readLive(st.Live)
	if err != nil {
		t.Fatal(err)
	}
	src := live[id]
	if src == nil {
		t.Fatalf("живых данных источника %s нет", id)
	}
	return src
}

// rtNoticeIDs — идентификаторы уведомлений источников.
func rtNoticeIDs(e *rtEnv) map[string]configlayer.NoticeView {
	out := map[string]configlayer.NoticeView{}
	for _, n := range e.layer.Snapshot().Notices {
		if strings.HasPrefix(n.ID, noticePrefix) {
			out[n.ID] = n
		}
	}
	return out
}

// Срок обновления: интервал провайдера, свой в часах, 12 часов по умолчанию,
// выключено — никогда; отказ провайдера чаще интервала не повторяется, сетевая ошибка
// повторяется по задержке; статус неустановленного ядра срок не двигает.
func TestRouterSchedulerDue(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = configlayer.InstalledKernels{Xray: true}
	t0 := e.clock.Now()
	live := func(meta Meta, st Status) *LiveSource {
		meta.CheckedAt = t0
		return &LiveSource{Meta: meta, Kernels: map[string]*KernelLive{kernelXray: {Status: st, UpdatedAt: t0}}}
	}
	ok := Status{Code: StatusOK}
	cases := []struct {
		name string
		cfg  Interval
		src  *LiveSource
		due  time.Duration
		want bool
	}{
		{"интервал провайдера 6 ч", Interval{Mode: IntervalProvider}, live(Meta{IntervalH: 6}, ok), 6 * time.Hour, true},
		{"без интервала провайдера — 12 ч", Interval{Mode: IntervalProvider}, live(Meta{}, ok), 12 * time.Hour, true},
		{"свой интервал 3 ч", Interval{Mode: IntervalHours, Hours: 3}, live(Meta{IntervalH: 6}, ok), 3 * time.Hour, true},
		{"выключено", Interval{Mode: IntervalOff}, live(Meta{IntervalH: 6}, ok), 0, false},
		{"отказ провайдера не ускоряет", Interval{Mode: IntervalProvider}, live(Meta{IntervalH: 6}, Status{Code: StatusRejected, ErrorCode: nodes.CodeAllStubs, Attempts: 9}), 6 * time.Hour, true},
		{"tls_error ждёт интервала", Interval{Mode: IntervalProvider}, live(Meta{IntervalH: 6}, Status{Code: StatusNetwork, ErrorCode: nodes.CodeTLSError, Attempts: 1}), 6 * time.Hour, true},
		{"первый повтор через 5 минут", Interval{Mode: IntervalProvider}, live(Meta{IntervalH: 6}, Status{Code: StatusNetwork, ErrorCode: nodes.CodeTimeout, Attempts: 1}), 5 * time.Minute, true},
		{"третий повтор через 60 минут", Interval{Mode: IntervalProvider}, live(Meta{IntervalH: 6}, Status{Code: StatusNetwork, ErrorCode: nodes.CodeDNSFailed, Attempts: 3}), time.Hour, true},
		{"после последней задержки — интервал", Interval{Mode: IntervalProvider}, live(Meta{IntervalH: 6}, Status{Code: StatusNetwork, ErrorCode: nodes.CodeDNSFailed, Attempts: 4}), 6 * time.Hour, true},
		{"повтор раньше интервала в 1 час не позже интервала", Interval{Mode: IntervalHours, Hours: 1}, live(Meta{}, Status{Code: StatusNetwork, ErrorCode: nodes.CodeServerError, Attempts: 3}), time.Hour, true},
	}
	for _, c := range cases {
		cfg := SubscriptionConfig{Enabled: true, Interval: c.cfg}
		due, ok := e.svc.nextDue(cfg, c.src)
		if ok != c.want {
			t.Errorf("%s: расписание включено %v, ждали %v", c.name, ok, c.want)
			continue
		}
		if ok && due.Sub(t0) != c.due {
			t.Errorf("%s: срок через %v, ждали %v", c.name, due.Sub(t0), c.due)
		}
	}

	// Срок отсчитывается от времени добавления, пока проверок не было; подписка без
	// единой отметки времени обновляется сразу.
	added := &LiveSource{Meta: Meta{UpdatedAt: t0}, Kernels: map[string]*KernelLive{}}
	if due, _ := e.svc.nextDue(SubscriptionConfig{Interval: Interval{Mode: IntervalProvider}}, added); due.Sub(t0) != 12*time.Hour {
		t.Errorf("срок от времени добавления: %v", due.Sub(t0))
	}
	if due, ok := e.svc.nextDue(SubscriptionConfig{Interval: Interval{Mode: IntervalProvider}}, nil); !ok || !due.IsZero() {
		t.Errorf("подписка без отметок времени: срок %v, включено %v", due, ok)
	}
	// Ядро снято: его старый срок повтора не вызывает обновление.
	stale := live(Meta{IntervalH: 6}, Status{Code: StatusNetwork, ErrorCode: nodes.CodeTimeout, Attempts: 1})
	stale.Kernels[kernelMihomo] = &KernelLive{Status: Status{Code: StatusNetwork, ErrorCode: nodes.CodeTimeout, Attempts: 1, RetryAt: t0.Add(time.Minute)}}
	if due, _ := e.svc.nextDue(SubscriptionConfig{Interval: Interval{Mode: IntervalProvider}}, stale); due.Sub(t0) != 5*time.Minute {
		t.Errorf("срок со статусом неустановленного ядра: %v", due.Sub(t0))
	}
}

// Повторы D-30 на настоящей сети: домен зоны .invalid не разрешается, повторы идут
// через 5, 15 и 60 минут по часам сервиса, затем по обычному интервалу; прежний
// снимок остаётся, статус и уведомление по ядру обновляются.
func TestRouterSchedulerNetworkRetries(t *testing.T) {
	e, cap := rtSubscriptionEnv(t, true, false, false)
	const id = "a1b2c3d4"
	ref := rtPut(t, e, "vless_ws", "trojan_grpc")
	rtSeedApplied(t, e, id, rtDeadURL, Interval{Mode: IntervalProvider}, true,
		&LiveSource{Kernels: map[string]*KernelLive{kernelXray: rtAppliedRef(e, ref)}})
	ctx := context.Background()
	requests := func() int { return len(cap.all()) }

	tickAfter := func(d time.Duration) {
		e.clock.Advance(d)
		e.svc.tick(ctx)
	}
	// Срок ещё не наступил: запроса нет.
	tickAfter(time.Hour)
	if requests() != 0 {
		t.Fatalf("запрос до срока: %d", requests())
	}
	// 12 часов от добавления: первый запрос, отказ сети.
	tickAfter(11 * time.Hour)
	if requests() != 1 {
		t.Fatalf("запросов в срок: %d", requests())
	}
	check := func(attempts int, retry time.Duration) {
		t.Helper()
		src := rtLiveOf(t, e, id)
		kl := src.Kernels[kernelXray]
		if kl.Status.Code != StatusNetwork || kl.Status.ErrorCode != nodes.CodeDNSFailed || kl.Status.Attempts != attempts {
			t.Fatalf("статус: %+v, ждали network/dns_failed, попыток %d", kl.Status, attempts)
		}
		if retry == 0 && !kl.Status.RetryAt.IsZero() {
			t.Errorf("срок повтора после последней задержки: %v", kl.Status.RetryAt)
		}
		if retry != 0 && !kl.Status.RetryAt.Equal(e.clock.Now().Add(retry)) {
			t.Errorf("срок повтора: через %v, ждали %v", kl.Status.RetryAt.Sub(e.clock.Now()), retry)
		}
		if kl.Applied == nil || kl.Applied.Hash != ref.Hash {
			t.Errorf("применённый снимок изменился при отказе сети: %+v", kl.Applied)
		}
		if !src.Meta.CheckedAt.Equal(e.clock.Now()) {
			t.Errorf("время проверки: %v, часы %v", src.Meta.CheckedAt, e.clock.Now())
		}
	}
	check(1, 5*time.Minute)
	n := rtNoticeIDs(e)["source_network:"+id+":xray"]
	if n.Kind != "warning" || n.Params["in"] != "5" || n.Params["code"] != nodes.CodeDNSFailed || n.Params["name"] != "Провайдер" {
		t.Errorf("уведомление о сети: %+v", n)
	}
	// Абсолютный срок повтора: остаток до него считает клиент.
	if at, err := time.Parse(time.RFC3339, n.Params["retry_at"]); err != nil || !at.Equal(e.clock.Now().Add(5*time.Minute).Truncate(time.Second)) {
		t.Errorf("срок повтора в уведомлении: %q (%v)", n.Params["retry_at"], err)
	}

	tickAfter(4 * time.Minute)
	if requests() != 1 {
		t.Fatalf("повтор раньше 5 минут: запросов %d", requests())
	}
	tickAfter(time.Minute)
	if requests() != 2 {
		t.Fatalf("повтор через 5 минут: запросов %d", requests())
	}
	check(2, 15*time.Minute)
	tickAfter(15 * time.Minute)
	if requests() != 3 {
		t.Fatalf("повтор через 15 минут: запросов %d", requests())
	}
	check(3, time.Hour)
	tickAfter(time.Hour)
	if requests() != 4 {
		t.Fatalf("повтор через 60 минут: запросов %d", requests())
	}
	check(4, 0)

	// После последней задержки — обычный интервал (12 часов от последней проверки).
	tickAfter(11 * time.Hour)
	if requests() != 4 {
		t.Fatalf("запрос раньше интервала: %d", requests())
	}
	tickAfter(time.Hour)
	if requests() != 5 {
		t.Fatalf("запрос по интервалу: %d", requests())
	}
	check(5, 0)
}

// Выключенный источник не обновляется: ни одного запроса к провайдеру; неизвестный
// источник — ErrSourceNotFound; «Мои узлы» не подписка.
func TestRouterRefreshDisabledAndUnknown(t *testing.T) {
	e, cap := rtSubscriptionEnv(t, true, true, false)
	const id = "a1b2c3d4"
	ref := rtPut(t, e, "vless_ws")
	rtSeedApplied(t, e, id, rtDeadURL, Interval{Mode: IntervalProvider}, false,
		&LiveSource{Kernels: map[string]*KernelLive{kernelXray: rtAppliedRef(e, ref)}})
	ctx := context.Background()
	if _, err := e.svc.Refresh(ctx, id); !errors.Is(err, ErrSourceDisabled) {
		t.Errorf("выключенный источник: %v", err)
	}
	if _, err := e.svc.Refresh(ctx, "deadbeef"); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("неизвестный источник: %v", err)
	}
	if _, err := e.svc.Refresh(ctx, MySourceID); !errors.Is(err, ErrSourceNotFound) {
		t.Errorf("«Мои узлы»: %v", err)
	}
	// Планировщик выключенный источник пропускает.
	e.clock.Advance(100 * time.Hour)
	e.svc.tick(ctx)
	if got := len(cap.all()); got != 0 {
		t.Errorf("запросов к провайдеру для выключенного источника: %d", got)
	}
}

// Одновременные обновления одного источника сливаются: провайдер получает по одному
// запросу на ядро, оба вызова возвращают один итог (D-08). Очередь источников
// удерживается самим тестом, поэтому второй вызов гарантированно приходит, пока
// первый идёт.
func TestRouterRefreshSingleflight(t *testing.T) {
	e, cap := rtSubscriptionEnv(t, true, true, false)
	const id = "a1b2c3d4"
	ref := rtPut(t, e, "vless_ws", "trojan_grpc")
	rtSeedApplied(t, e, id, rtDeadURL, Interval{Mode: IntervalProvider}, true,
		&LiveSource{Kernels: map[string]*KernelLive{kernelXray: rtAppliedRef(e, ref), kernelMihomo: rtAppliedRef(e, ref)}})

	type result struct {
		out RefreshOutcome
		err error
	}
	results := make(chan result, 2)
	e.svc.refreshMu.Lock()
	go func() {
		out, err := e.svc.Refresh(context.Background(), id)
		results <- result{out, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.svc.cmu.Lock()
		_, registered := e.svc.calls[id]
		e.svc.cmu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			e.svc.refreshMu.Unlock()
			t.Fatal("первое обновление не зарегистрировалось")
		}
		time.Sleep(time.Millisecond)
	}
	go func() {
		out, err := e.svc.Refresh(context.Background(), id)
		results <- result{out, err}
	}()
	time.Sleep(100 * time.Millisecond)
	e.svc.refreshMu.Unlock()

	var got [2]result
	for i := range got {
		select {
		case got[i] = <-results:
		case <-time.After(2 * time.Minute):
			t.Fatal("обновление не завершилось")
		}
	}
	if got[0].err != nil || got[1].err != nil {
		t.Fatalf("ошибки обновления: %v, %v", got[0].err, got[1].err)
	}
	if !reflect.DeepEqual(got[0].out, got[1].out) {
		t.Errorf("итоги различаются: %+v и %+v", got[0].out, got[1].out)
	}
	if got[0].out.Outcome != OutcomeNetwork || got[0].out.Error == nil || got[0].out.Error.Code != nodes.CodeDNSFailed {
		t.Errorf("итог при недоступном провайдере: %+v", got[0].out)
	}
	if len(got[0].out.Kernels) != 2 {
		t.Errorf("итог по ядрам: %+v", got[0].out.Kernels)
	}
	if reqs := len(cap.all()); reqs != 2 {
		t.Errorf("запросов к провайдеру: %d, нужно по одному на ядро", reqs)
	}
}

// Ключи JSON итога обновления — контракт клиента.
func TestRouterRefreshOutcomeJSONKeys(t *testing.T) {
	raw, err := json.Marshal(RefreshOutcome{
		Outcome: OutcomeApplied, Added: 1, Removed: 2,
		Kernels: map[string]KernelOutcome{"xray": {Status: StatusOK, Count: 3}, "mihomo": {Status: StatusRejected, Count: 2, ErrorCode: nodes.CodeAllStubs}},
		Error:   nodes.NewError(nodes.CodeAllStubs),
	})
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"outcome", "added", "removed", "kernels", "error"} {
		if _, ok := top[k]; !ok {
			t.Errorf("в итоге нет ключа %s: %s", k, raw)
		}
	}
	var kernels map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["kernels"], &kernels); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"status", "count"} {
		if _, ok := kernels["xray"][k]; !ok {
			t.Errorf("в итоге по ядру нет ключа %s", k)
		}
	}
	if _, ok := kernels["xray"]["error_code"]; ok {
		t.Error("error_code без отказа не выводится")
	}
	if _, ok := kernels["mihomo"]["error_code"]; !ok {
		t.Error("error_code при отказе не выведен")
	}
}

// Уведомления собираются из живых данных по ядрам: отказ провайдера со ссылкой
// кабинета и датой прежнего снимка, сеть с минутами до повтора, отложенная запись,
// отказ ядра с причиной; статус ok уведомления не даёт; без ссылки кабинета ключа
// web_url нет.
func TestRouterNoticesRebuildFromLive(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	ref := rtPut(t, e, "vless_ws")
	ref.FetchedAt = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	now := e.clock.Now()

	const a, b, c, d = "a1b2c3d4", "b1b2c3d4", "c1b2c3d4", "d1b2c3d4"
	rtSeedApplied(t, e, a, rtDeadURL, Interval{Mode: IntervalProvider}, true, &LiveSource{
		Meta: Meta{WebURL: "https://cab.example/account"},
		Kernels: map[string]*KernelLive{
			kernelXray: {Applied: ref, Status: Status{Code: StatusRejected, ErrorCode: nodes.CodeAllStubs, Since: now, Attempts: 2}},
			kernelMihomo: {Applied: ref, Status: Status{
				Code: StatusNetwork, ErrorCode: nodes.CodeDNSFailed, Since: now, Attempts: 1, RetryAt: now.Add(5 * time.Minute),
			}, UpdatedAt: now},
		},
	})
	rtSeedApplied(t, e, b, rtDeadURL, Interval{Mode: IntervalProvider}, true, &LiveSource{
		Kernels: map[string]*KernelLive{
			kernelXray: {Applied: ref, Status: Status{Code: StatusRejected, ErrorCode: nodes.CodeHTTPAuth, Since: now}},
		},
	})
	rtSeedApplied(t, e, c, rtDeadURL, Interval{Mode: IntervalProvider}, true, &LiveSource{
		Kernels: map[string]*KernelLive{kernelXray: rtAppliedRef(e, ref), kernelMihomo: rtAppliedRef(e, ref)},
	})
	rtSeedApplied(t, e, d, rtDeadURL, Interval{Mode: IntervalProvider}, true, &LiveSource{
		Kernels: map[string]*KernelLive{
			kernelXray:   {Applied: ref, Status: Status{Code: StatusRefused, Since: now, Params: map[string]string{"reason": "порт занят"}}},
			kernelMihomo: rtAppliedRef(e, ref),
		},
	})
	// Ожидающий снимок при «Применить» становится применённым, поэтому отложенная
	// запись выставляется после применения.
	if err := e.layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		for _, kl := range cur[c].Kernels {
			kl.Pending = ref
			kl.setStatus(Status{Code: StatusDeferred}, now)
		}
		return writeLive(l, cur)
	}); err != nil {
		t.Fatal(err)
	}
	e.svc.rebuildNotices()
	got := rtNoticeIDs(e)
	want := []string{
		"source_rejected:" + a + ":xray", "source_network:" + a + ":mihomo",
		"source_rejected:" + b + ":xray", "source_deferred:" + c, "source_kernel_refused:" + d + ":xray",
	}
	if len(got) != len(want) {
		t.Fatalf("уведомлений %d, ждали %d: %+v", len(got), len(want), got)
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("нет уведомления %s", id)
		}
	}
	rej := got["source_rejected:"+a+":xray"]
	if rej.Kind != "warning" || rej.Params["name"] != "Провайдер" || rej.Params["date"] != "2026-10-01" ||
		rej.Params["code"] != nodes.CodeAllStubs || rej.Params["web_url"] != "https://cab.example/account" {
		t.Errorf("отказ провайдера: %+v", rej)
	}
	if _, has := got["source_rejected:"+b+":xray"].Params["web_url"]; has {
		t.Error("web_url у уведомления без известной ссылки кабинета")
	}
	if net := got["source_network:"+a+":mihomo"]; net.Params["in"] != "5" || net.Params["code"] != nodes.CodeDNSFailed {
		t.Errorf("сеть: %+v", net)
	}
	if ref := got["source_kernel_refused:"+d+":xray"]; ref.Kind != "error" || ref.Params["reason"] != "порт занят" ||
		ref.Params["kernel"] != "xray" || ref.Params["name"] != "Провайдер" {
		t.Errorf("отказ ядра: %+v", ref)
	}

	// Статус стал ok — уведомление снято.
	if err := e.layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		cur[a].Kernels[kernelXray].setStatus(Status{Code: StatusOK}, now)
		return writeLive(l, cur)
	}); err != nil {
		t.Fatal(err)
	}
	e.svc.rebuildNotices()
	if _, ok := rtNoticeIDs(e)["source_rejected:"+a+":xray"]; ok {
		t.Error("уведомление об отказе осталось при статусе ok")
	}
}

// Причина отказа ядра: секреты узлов кандидата и части адреса подписки маскируются,
// текст не длиннее 200 символов.
func TestRouterMaskReason(t *testing.T) {
	e := rtNewEnv(t, false, false)
	list := rtNodes(t, "vless_ws")
	hash, err := e.svc.blobs.put(list)
	if err != nil {
		t.Fatal(err)
	}
	secrets := nodes.Secrets(list[0])
	if len(secrets) == 0 {
		t.Fatal("в узле нет секретов для проверки")
	}
	cfg := SubscriptionConfig{URL: "https://provider.invalid/path/tokensecret123?key=querysecret456"}
	msg := "ошибка: " + secrets[0] + " token tokensecret123 key querysecret456 " + strings.Repeat("я", 400)
	got := e.svc.maskReason(msg, "build_failed", cfg, map[string]Ref{kernelXray: {Hash: hash}})
	for _, s := range append(secrets, "tokensecret123", "querysecret456") {
		if len(s) >= 6 && strings.Contains(got, s) {
			t.Errorf("секрет %q не замаскирован: %s", s[:2]+"…", got)
		}
	}
	if n := len([]rune(got)); n > maxReasonRunes {
		t.Errorf("причина длиннее предела: %d", n)
	}
	if empty := e.svc.maskReason("  ", "write_failed", cfg, nil); empty != "write_failed" {
		t.Errorf("пустая причина заменяется кодом: %q", empty)
	}
}

// Выключенный флаг: планировщик, установка ядра и стартовые действия не делают ни
// одного запроса к провайдеру, не запрашивают HWID и не создают каталогов; уже
// сохранённая применённая подписка тоже не обновляется.
func TestRouterSchedulerFlagOffZeroRequests(t *testing.T) {
	var hwid atomic.Int64
	cap := &rtCapture{}
	mutate := func(d *Deps) {
		d.Client = rtObservedClient(cap)
		d.HWID = func() string { hwid.Add(1); return "A1B2C3D4E5F6" }
		d.Device = rtDevice
		d.TickInterval = 5 * time.Millisecond
	}

	// Флаг выключен с самого начала: каталога слоя нет.
	fresh := rtNewEnvOpts(t, false, false, false, mutate)
	fresh.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	fresh.svc.Start()
	fresh.clock.Advance(100 * time.Hour)
	time.Sleep(150 * time.Millisecond)
	fresh.svc.OnKernelInstalled("mihomo")
	fresh.svc.Stop()
	if _, err := os.Stat(filepath.Join(fresh.dir, "config_layer")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("каталог config_layer создан при выключенном флаге: %v", err)
	}
	if len(cap.all()) != 0 || hwid.Load() != 0 {
		t.Errorf("при выключенном флаге: запросов %d, вызовов HWID %d", len(cap.all()), hwid.Load())
	}

	// Применённая подписка есть, флаг выключили: тишина.
	e := rtNewEnvWith(t, false, false, mutate)
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	ref := rtPut(t, e, "vless_ws")
	rtSeedApplied(t, e, "a1b2c3d4", rtDeadURL, Interval{Mode: IntervalProvider}, true,
		&LiveSource{Kernels: map[string]*KernelLive{kernelXray: rtAppliedRef(e, ref)}})
	files := func() int {
		list, _ := os.ReadDir(e.svc.blobs.dir)
		return len(list)
	}
	filesBefore, reqBefore, hwBefore := files(), len(cap.all()), hwid.Load()
	e.enabled.Store(false)
	e.svc.Start()
	e.clock.Advance(100 * time.Hour)
	for range 20 {
		e.svc.tick(context.Background())
	}
	time.Sleep(100 * time.Millisecond)
	e.svc.OnKernelInstalled("mihomo")
	e.svc.OnDriftResolved()
	e.svc.Stop()
	if got := len(cap.all()); got != reqBefore {
		t.Errorf("запросов при выключенном флаге: %d", got-reqBefore)
	}
	if hwid.Load() != hwBefore {
		t.Error("HWID запрошен при выключенном флаге")
	}
	if files() != filesBefore {
		t.Errorf("снимки изменились при выключенном флаге: было %d, стало %d", filesBefore, files())
	}
}

// Stop дожидается цикла и фоновых горутин, повторный Stop безопасен; после Stop
// горутины не запускаются.
func TestRouterServiceStopWaits(t *testing.T) {
	e := rtNewEnvWith(t, false, false, func(d *Deps) { d.TickInterval = 5 * time.Millisecond })
	e.svc.Start()
	e.svc.Start()
	time.Sleep(50 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		e.svc.Stop()
		e.svc.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop не вернулся за 5 секунд")
	}
	if e.svc.spawn(func() {}) {
		t.Error("горутина запущена после Stop")
	}
}

// --- сценарии с настоящим провайдером и Xray устройства ---

// rtApplyProviderSub добавляет настоящую подписку через предпросмотр и применяет её
// с набором, подготовленным mutate (nil — полный набор). Возвращает идентификатор и
// число рабочих узлов в полном ответе провайдера.
func rtApplyProviderSub(t *testing.T, e *rtEnv, subURL string, mutate func([]nodes.Node) []nodes.Node) (string, int) {
	t.Helper()
	view, err := e.svc.Preview(context.Background(), inputOf(subURL))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(view.Subscriptions) != 1 || view.Token == "" || view.Subscriptions[0].Kernels[kernelXray].Error != nil {
		t.Fatalf("предпросмотр подписки: %+v", view.Subscriptions)
	}
	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add: %v", err)
	}
	id, _, _ := rtDraftSubscription(t, e)
	full := 0
	if kl := rtLiveOf(t, e, id).Kernels[kernelXray]; kl != nil && kl.Pending != nil {
		full = kl.Pending.Usable
	}
	if mutate != nil {
		err := e.layer.UpdateLive(func(l configlayer.Sections) error {
			cur, err := readLive(l)
			if err != nil {
				return err
			}
			for _, kl := range cur[id].Kernels {
				if kl.Pending == nil {
					continue
				}
				list, err := e.svc.blobs.get(kl.Pending.Hash)
				if err != nil {
					return err
				}
				list = mutate(list)
				hash, err := e.svc.blobs.put(list)
				if err != nil {
					return err
				}
				ref := *kl.Pending
				ref.Hash, ref.Usable, ref.Total = hash, len(list), len(list)
				kl.Pending = &ref
			}
			return writeLive(l, cur)
		})
		if err != nil {
			t.Fatalf("подмена ожидающего набора: %v", err)
		}
	}
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение: %+v", done.Result)
	}
	return id, full
}

// rtDropLastXray убирает из набора последний по ключу узел, который пишется в файл
// Xray: набор до его появления у провайдера. Ключи считаются по фиктивному
// источнику — важна только уникальность узлов.
func rtDropLastXray(e *rtEnv) func([]nodes.Node) []nodes.Node {
	return func(list []nodes.Node) []nodes.Node {
		caps := e.svc.caps(configlayer.InstalledKernels{Xray: true})
		uniq, _ := nodes.Uniq("00000000", list)
		drop := -1
		for i, k := range uniq {
			if nodes.Support(k.Node, kernelXray, caps).OK {
				drop = i
			}
		}
		out := make([]nodes.Node, 0, len(uniq))
		for i, k := range uniq {
			if i != drop {
				out = append(out, k.Node)
			}
		}
		return out
	}
}

func rtXrayFile(e *rtEnv, id string) string {
	return filepath.Join(e.roots.Xray, "04_outbounds.xcp-"+id+".tail.json")
}

// rtApplyDoneCount считает события apply_done с запуском обновления источника.
type rtApplyWatcher struct {
	events <-chan configlayer.Event
	cancel func()
}

func rtWatchApply(t *testing.T, e *rtEnv) *rtApplyWatcher {
	t.Helper()
	ch, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return &rtApplyWatcher{events: ch, cancel: cancel}
}

// refreshRuns — сколько запусков конвейера обновления источника завершилось с тех пор.
func (w *rtApplyWatcher) refreshRuns(wait time.Duration) int {
	n := 0
	timeout := time.After(wait)
	for {
		select {
		case ev, ok := <-w.events:
			if !ok {
				return n
			}
			if v, ok := ev.Data.(configlayer.ApplyView); ev.Type == configlayer.EventApplyDone && ok && v.Trigger == configlayer.TriggerSourceRefresh {
				n++
			}
		case <-timeout:
			return n
		}
	}
}

// Сквозной сценарий FND-06: провайдер отдал новый состав → «Обновить» → хэш набора
// Xray изменился → конвейер из применённого состояния → новый файл; открытая правка
// черновика не тронута; повторное обновление без изменений — ни одного запуска
// конвейера, файл и его время записи прежние.
func TestRouterRefreshChangedSetTracer(t *testing.T) {
	subURL := rtSubURL(t)
	rtNeedKernels(t, true, false)
	e, cap := rtSubscriptionEnv(t, true, false, true)
	ctx := context.Background()

	// Применённый набор — без одного узла: как если бы провайдер добавил его позже.
	id, full := rtApplyProviderSub(t, e, subURL, rtDropLastXray(e))
	if full < 2 {
		t.Skipf("в подписке меньше двух рабочих узлов: %d", full)
	}
	hash := rtLiveOf(t, e, id).Kernels[kernelXray].Applied.Hash
	before, err := os.ReadFile(rtXrayFile(e, id))
	if err != nil {
		t.Fatal(err)
	}

	// Открытая правка другой секции черновика.
	e.addLinks(t, rtLink(t, "vless_ws"))
	snap := e.layer.Snapshot()
	rev, changes := snap.DraftRevision, snap.DraftChanges
	if changes == 0 {
		t.Fatal("правка черновика не открыта")
	}

	reqBefore := len(cap.all())
	out, err := e.svc.Refresh(ctx, id)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := len(cap.all()) - reqBefore; got != 1 {
		t.Errorf("запросов на обновление: %d, нужен 1 (установлен один Xray)", got)
	}
	if out.Outcome != OutcomeApplied || out.Added != 1 || out.Removed != 0 {
		t.Fatalf("итог обновления: %+v", out)
	}
	after, err := os.ReadFile(rtXrayFile(e, id))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, after) {
		t.Error("файл Xray не изменился после смены состава")
	}
	snap = e.layer.Snapshot()
	if snap.DraftRevision != rev || snap.DraftChanges != changes {
		t.Errorf("черновик тронут: ревизия %d → %d, правок %d → %d", rev, snap.DraftRevision, changes, snap.DraftChanges)
	}
	kl := rtLiveOf(t, e, id).Kernels[kernelXray]
	if kl.Applied == nil || kl.Applied.Hash == hash || kl.Delta.Added != 1 || kl.Status.Code != StatusOK {
		t.Errorf("живые данные после обновления: %+v", kl)
	}

	// Повтор без изменений: конвейер не запускается, файл не переписывается.
	st, err := os.Stat(rtXrayFile(e, id))
	if err != nil {
		t.Fatal(err)
	}
	w := rtWatchApply(t, e)
	out, err = e.svc.Refresh(ctx, id)
	if err != nil {
		t.Fatalf("повторный Refresh: %v", err)
	}
	if out.Outcome != OutcomeUnchanged {
		t.Skipf("состав у провайдера изменился между запросами: %s", out.Outcome)
	}
	if runs := w.refreshRuns(2 * time.Second); runs != 0 {
		t.Errorf("конвейер запущен без смены состава: %d", runs)
	}
	st2, err := os.Stat(rtXrayFile(e, id))
	if err != nil || !st2.ModTime().Equal(st.ModTime()) {
		t.Errorf("файл Xray переписан без смены состава: %v", err)
	}
	if !rtLiveOf(t, e, id).Meta.CheckedAt.Equal(e.clock.Now()) {
		t.Error("время проверки не обновлено")
	}
}

// Переименование узла у провайдера при прежнем ключе: файл Xray побайтно прежний (тег
// от ключа), конвейер даёт «применено» без добавленных и убранных узлов.
func TestRouterRefreshRenameOnlyXrayFileUntouched(t *testing.T) {
	subURL := rtSubURL(t)
	rtNeedKernels(t, true, false)
	e, _ := rtSubscriptionEnv(t, true, false, true)
	rename := func(list []nodes.Node) []nodes.Node {
		out := append([]nodes.Node(nil), list...)
		out[0].Name = "xcp-rename-check"
		return out
	}
	id, _ := rtApplyProviderSub(t, e, subURL, rename)
	before, err := os.ReadFile(rtXrayFile(e, id))
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(rtXrayFile(e, id))
	if err != nil {
		t.Fatal(err)
	}
	out, err := e.svc.Refresh(context.Background(), id)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if out.Outcome != OutcomeApplied || out.Added != 0 || out.Removed != 0 {
		t.Fatalf("итог при переименовании: %+v", out)
	}
	after, err := os.ReadFile(rtXrayFile(e, id))
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("файл Xray изменился при переименовании: %v", err)
	}
	if st2, err := os.Stat(rtXrayFile(e, id)); err != nil || !st2.ModTime().Equal(st.ModTime()) {
		t.Errorf("файл Xray переписан при переименовании: %v", err)
	}
}

// Нерешённый дрейф: «Обновить» не трогает ни файл, ни ядро, новый снимок ждёт как
// ожидающий со статусом deferred и уведомлением; после «Пересобрать» запись проходит
// сама, без нового запроса к провайдеру, уведомление снимается.
func TestRouterRefreshDeferredOnDrift(t *testing.T) {
	subURL := rtSubURL(t)
	rtNeedKernels(t, true, false)
	e, cap := rtSubscriptionEnv(t, true, false, true)
	ctx := context.Background()
	id, full := rtApplyProviderSub(t, e, subURL, rtDropLastXray(e))
	if full < 2 {
		t.Skipf("в подписке меньше двух рабочих узлов: %d", full)
	}
	path := rtXrayFile(e, id)
	hash := rtLiveOf(t, e, id).Kernels[kernelXray].Applied.Hash
	key := "xray:04_outbounds.xcp-" + id + ".tail.json"

	// Ручная правка файла панели.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manual := append(append([]byte(nil), data...), '\n', '\n')
	if err := os.WriteFile(path, manual, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := e.svc.Refresh(ctx, id)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if out.Outcome != OutcomeDeferred {
		t.Fatalf("итог при дрейфе: %+v", out)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, manual) {
		t.Error("файл с ручной правкой перезаписан")
	}
	kl := rtLiveOf(t, e, id).Kernels[kernelXray]
	if kl.Status.Code != StatusDeferred || kl.Pending == nil || kl.Applied == nil || kl.Applied.Hash != hash {
		t.Fatalf("живые данные при дрейфе: %+v", kl)
	}
	pending := kl.Pending.Hash
	if _, ok := rtNoticeIDs(e)["source_deferred:"+id]; !ok {
		t.Error("нет уведомления source_deferred")
	}

	// «Пересобрать»: дрейф решён, отложенная запись проходит сама.
	reqs := len(cap.all())
	if err := e.layer.Rebuild([]string{key}, false); err != nil {
		t.Fatalf("Rebuild при дрейфе: %v", err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for {
		kl = rtLiveOf(t, e, id).Kernels[kernelXray]
		if kl.Status.Code == StatusOK && kl.Pending == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("отложенная запись не прошла за 3 минуты: %+v", kl)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if kl.Applied == nil || kl.Applied.Hash != pending {
		t.Errorf("применён не отложенный набор: %+v", kl.Applied)
	}
	if got := len(cap.all()); got != reqs {
		t.Errorf("отложенная запись сделала запросы к провайдеру: %d", got-reqs)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, ok := rtNoticeIDs(e)["source_deferred:"+id]; !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("уведомление source_deferred осталось после записи")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Отложенный набор устаревает: применён полный ответ провайдера, ожидающим ждёт
// другой набор (отложен при дрейфе), а следующее «Обновить» возвращает прежний
// применённый состав. Ожидающий снимок снимается, статус ok: черновик больше не
// собирается из устаревшего набора, и «Применить» его не применит.
func TestRouterRefreshUnchangedDropsStalePending(t *testing.T) {
	subURL := rtSubURL(t)
	rtNeedKernels(t, true, false)
	e, _ := rtSubscriptionEnv(t, true, false, true)
	id, full := rtApplyProviderSub(t, e, subURL, nil)
	if full < 2 {
		t.Skipf("в подписке меньше двух рабочих узлов: %d", full)
	}
	applied := rtLiveOf(t, e, id).Kernels[kernelXray].Applied.Hash

	// Отложенный набор без одного узла: как если бы провайдер на время убрал узел,
	// а запись упёрлась в дрейф.
	err := e.layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		kl := cur[id].Kernels[kernelXray]
		list, err := e.svc.blobs.get(kl.Applied.Hash)
		if err != nil {
			return err
		}
		list = rtDropLastXray(e)(list)
		hash, err := e.svc.blobs.put(list)
		if err != nil {
			return err
		}
		ref := *kl.Applied
		ref.Hash, ref.Usable, ref.Total = hash, len(list), len(list)
		kl.Pending = &ref
		kl.setStatus(Status{Code: StatusDeferred}, e.clock.Now())
		return writeLive(l, cur)
	})
	if err != nil {
		t.Fatalf("отложенный набор: %v", err)
	}

	out, err := e.svc.Refresh(context.Background(), id)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if out.Outcome != OutcomeUnchanged {
		t.Skipf("состав у провайдера изменился между запросами: %s", out.Outcome)
	}
	kl := rtLiveOf(t, e, id).Kernels[kernelXray]
	if kl.Pending != nil {
		t.Errorf("устаревший отложенный набор остался: %+v", kl.Pending)
	}
	if kl.Status.Code != StatusOK || kl.Applied == nil || kl.Applied.Hash != applied {
		t.Errorf("живые данные после обновления: %+v", kl)
	}
}

// Подписку изменили, пока шёл запрос: ответ прежней конфигурации отбрасывается, и
// общая работа запрашивает провайдера заново с новой. Подписка только в черновике
// (планировщик её не обновляет) получает ожидающий набор без второго «Обновить», а
// итог — не поддельное «без изменений».
func TestRouterRefreshRetriesWhenChangedDuringFetch(t *testing.T) {
	subURL := rtSubURL(t)
	rtNeedKernels(t, true, false)
	e, cap := rtSubscriptionEnv(t, true, false, true)
	view, err := e.svc.Preview(context.Background(), inputOf(subURL))
	if err != nil || len(view.Subscriptions) != 1 || view.Token == "" {
		t.Fatalf("предпросмотр подписки: %v", err)
	}
	if _, err := e.svc.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add: %v", err)
	}
	id, _, _ := rtDraftSubscription(t, e)
	if err := e.layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		for _, kl := range cur[id].Kernels {
			kl.Pending = nil
		}
		return writeLive(l, cur)
	}); err != nil {
		t.Fatalf("сброс ожидающего набора: %v", err)
	}

	// Свой HWID, равный HWID панели: конфигурация подписки меняется, а запросы к
	// провайдеру остаются прежними.
	hwid := e.svc.d.HWID()
	var (
		once      sync.Once
		updateErr error
	)
	cap.mu.Lock()
	cap.before = func() {
		once.Do(func() {
			st, err := e.layer.ReadState()
			if err != nil {
				updateErr = err
				return
			}
			_, updateErr = e.svc.Update(st.DraftRevision, id, Patch{HWID: &hwid})
		})
	}
	cap.mu.Unlock()
	reqs := len(cap.all())

	out, err := e.svc.Refresh(context.Background(), id)
	if updateErr != nil {
		t.Fatalf("смена HWID во время запроса: %v", updateErr)
	}
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := len(cap.all()) - reqs; got != 2 {
		t.Errorf("запросов к провайдеру %d, ожидалось 2: отброшенный и повтор", got)
	}
	if out.Outcome != OutcomePendingUpdated {
		t.Errorf("итог %q, ожидалось %q", out.Outcome, OutcomePendingUpdated)
	}
	if kl := rtLiveOf(t, e, id).Kernels[kernelXray]; kl == nil || kl.Pending == nil {
		t.Errorf("ожидающий набор новой конфигурации не записан: %+v", kl)
	}
	_, cfg, _ := rtDraftSubscription(t, e)
	if cfg.HWID != hwid {
		t.Errorf("HWID подписки после смены: %q", cfg.HWID)
	}
}

// Сборка после установки ядра (D-01): применённая подписка Xray получает файл Mihomo
// без «Обновить» — один запрос под Mihomo и файл провайдера на диске.
func TestRouterOnKernelInstalledBuildsFiles(t *testing.T) {
	subURL := rtSubURL(t)
	rtNeedKernels(t, true, true)
	e, cap := rtSubscriptionEnv(t, true, true, true)
	e.installed = configlayer.InstalledKernels{Xray: true}
	id, _ := rtApplyProviderSub(t, e, subURL, nil)
	mihomoFile := filepath.Join(e.roots.Mihomo, "proxy_providers", "xcp-"+id+".yaml")
	if _, err := os.Stat(mihomoFile); err == nil {
		t.Fatal("файл Mihomo записан до установки ядра")
	}
	reqs := len(cap.all())

	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	e.svc.OnKernelInstalled("mihomo")
	// Stop отменяет контекст сервиса, а не ждёт сборку: сначала ждём запрос к
	// провайдеру и запись снимка, иначе отмена обрывает сборку до запроса.
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if len(cap.all()) > reqs {
			if kl := rtLiveOf(t, e, id).Kernels[kernelMihomo]; kl != nil && kl.Applied != nil {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	e.svc.Stop() // дожидается завершения фоновой сборки
	got := cap.all()[reqs:]
	if len(got) != 1 || !strings.HasPrefix(got[0].UA, "ClashMeta/") {
		t.Fatalf("запросы после установки ядра: %+v", got)
	}
	if st, err := os.Stat(mihomoFile); err != nil || st.Size() == 0 {
		t.Errorf("файл Mihomo не собран: %v", err)
	}
	if kl := rtLiveOf(t, e, id).Kernels[kernelMihomo]; kl == nil || kl.Applied == nil || kl.Status.Code != StatusOK {
		t.Errorf("живые данные Mihomo: %+v", kl)
	}
}
