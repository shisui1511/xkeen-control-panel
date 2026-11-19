//go:build router

// router-area: configlayer

package configlayer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Возврат маршрутизации обычным «Применить», взятие на старте слоя, дрейф
// маршрутизации и автоматическое взятие у позже установленного ядра на устройстве:
// окружение и раскладка каталогов те же, что у сценариев взятия
// (router_takeover_test.go). Работающие ядра стенда не трогаются, тест ядро не
// читает и не переключает — метка ядра не нужна.

// restart останавливает слой окружения и поднимает новый на том же каталоге
// данных, как перезапуск панели: генератор регистрируется заново, Start собирает
// файлы из применённого состояния. Возвращает итог стартовой сборки.
func (e *rtTakeoverEnv) restart(t *testing.T) *ResultView {
	t.Helper()
	e.layer.Stop()
	layer, err := New(Options{
		DataDir: filepath.Dir(e.roots.Xray),
		Roots:   e.roots,
		Binaries: func() Binaries {
			if e.noMihomo.Load() {
				return Binaries{Xray: rtRefreshXrayBin}
			}
			return Binaries{Xray: rtRefreshXrayBin, Mihomo: rtMhBin}
		},
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New после остановки: %v", err)
	}
	t.Cleanup(layer.Stop)
	if err := layer.RegisterGenerator(e.gen); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	e.layer = layer
	rtWaitFor(t, "стартовая сборка применённого состояния", func() bool {
		v := layer.pipeline.Current()
		return !v.Running && v.Trigger == TriggerStartup && v.Result != nil
	})
	e.rev = layer.store.Snapshot().DraftRevision
	return layer.pipeline.Current().Result
}

// TestRouterRoutingReleaseLifecycle — жизненный цикл владения маршрутизацией:
// Start берёт маршрутизацию ядра, которого не было при «Применить»;
// «Отпустить» при занятом применении отказывает без записи; файл с тем же именем,
// который пользователь вернул сам, не перезаписывается (release_conflict); после
// уборки возврат проходит побайтно, а повторное взятие снова работает.
func TestRouterRoutingReleaseLifecycle(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	// «Применить» при одном Xray: Mihomo не взят, config.yaml на прежнем профиле.
	e.noMihomo.Store(true)
	e.setManaged(t, true)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие при одном Xray: %+v", res)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Fatalf("после взятия файл routing остался в configs/: %v", err)
	}
	if st := e.layer.store.Snapshot(); st.Routing.Xray == nil || st.Routing.Mihomo != nil {
		t.Fatalf("State.Routing после взятия Xray: %+v", st.Routing)
	}
	e.assertConfigLink(t, e.link)
	xrayRoute := hashOf(e.xray(rtTkPanelRoute))
	xrayState, err := json.Marshal(e.layer.store.Snapshot().Routing.Xray)
	if err != nil {
		t.Fatal(err)
	}

	// Перезапуск с установленным Mihomo: стартовая сборка применённого состояния
	// (routing.managed) берёт и его, Xray остаётся как был.
	e.noMihomo.Store(false)
	if res := e.restart(t); !res.OK {
		t.Fatalf("стартовая сборка: %+v", res)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); err != nil {
		t.Errorf("профиль панели не записан на старте: %v", err)
	}
	st := e.layer.store.Snapshot()
	if st.Routing.Mihomo == nil || st.Routing.Mihomo.PrevProfile != "profiles/user.yaml" {
		t.Fatalf("State.Routing.Mihomo после старта: %+v", st.Routing.Mihomo)
	}
	if after, err := json.Marshal(st.Routing.Xray); err != nil || string(after) != string(xrayState) {
		t.Errorf("State.Routing.Xray изменено стартом (%v):\n%s\n%s", err, xrayState, after)
	}
	if got := hashOf(e.xray(rtTkPanelRoute)); got != xrayRoute {
		t.Errorf("файл routing панели изменён стартом: %q, ожидался %q", got, xrayRoute)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят после старта: %+v", st.Journal)
	}
	if n := e.layer.Snapshot().DriftCount; n != 0 {
		t.Errorf("дрейф после стартовой сборки: %d", n)
	}

	// Занятое применение: «Отпустить» по кнопке отказывает, диск не тронут. Замок
	// берётся как фоновым запуском: apply_done публикуется ещё под замком
	// «Применить», и запуск по кнопке сразу после итога ждал бы его сам.
	e.setManaged(t, false)
	release, err := e.layer.pipeline.TryBegin(e.layer.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	err = e.layer.StartApply(true)
	release()
	if !errors.Is(err, ErrApplyBusy) {
		t.Fatalf("«Отпустить» при занятом применении: %v, ожидался ErrApplyBusy", err)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл панели тронут при занятом применении: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("чужой файл вернулся при занятом применении: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// release_conflict: пользователь вернул файл с тем же именем сам — возврат его
	// не перезаписывает и отказывает целиком, владение прежнее.
	const own = `{"routing": {"rules": []}}` + "\n"
	if err := os.WriteFile(e.xray(rtTkRoutingFile), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	res := e.apply(t)
	if res.OK || res.Code != ResultBuildFailed || !rtHasIssue(res, IssueReleaseConflict, ManifestKey(KernelXray, rtTkRoutingFile), rtTkRoutingFile) {
		t.Fatalf("возврат при занятом имени: %+v, ожидался release_conflict по %s", res, rtTkRoutingFile)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != HashContent([]byte(own)) {
		t.Errorf("файл пользователя перезаписан при возврате: %q", got)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл панели тронут отказанным возвратом: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if st := e.layer.store.Snapshot(); st.Routing.Xray == nil || st.Routing.Mihomo == nil || st.Journal != nil {
		t.Errorf("отказ изменил владение: routing %+v, журнал %+v", st.Routing, st.Journal)
	}

	// Пользователь убрал свой файл: возврат проходит, чужой routing вернулся
	// побайтно, config.yaml на прежнем профиле, файлов панели нет.
	if err := os.Remove(e.xray(rtTkRoutingFile)); err != nil {
		t.Fatal(err)
	}
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("возврат после уборки своего файла: %+v", res)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("%s вернулся не побайтно: %q", rtTkRoutingFile, got)
	}
	e.assertUntouched(t)
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)
	st = e.layer.store.Snapshot()
	if !st.Routing.isEmpty() {
		t.Errorf("State.Routing не очищено: %+v", st.Routing)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят: %+v", st.Journal)
	}
	for key, entry := range st.Manifest {
		if entry.Status == StatusManaged {
			t.Errorf("в манифесте остался managed-файл %s", key)
		}
	}

	// Повторное взятие после возврата: тот же цикл снова уносит routing и
	// переводит config.yaml на профиль панели.
	e.setManaged(t, true)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("повторное взятие: %+v", res)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("после повторного взятия %s остался в configs/: %v", rtTkRoutingFile, err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл routing панели не собран повторным взятием: %v", err)
	}
}

const (
	rtRelForeignBody = `{"routing": {"rules": [{"type": "field", "network": "tcp,udp", "outboundTag": "direct"}]}}` + "\n"
	rtRelMixedBody   = `{"routing": {"rules": []}, "outbounds": [{"tag": "zz-out", "protocol": "freedom"}]}` + "\n"
	rtRelMixedFile   = "zz_user.json"
)

// waitRoutingEvent ждёт события routing из подписки.
func waitRoutingEvent(t *testing.T, events <-chan Event) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("подписка закрыта до события routing")
			}
			if ev.Type == EventRouting {
				if _, ok := ev.Data.(RoutingEvent); !ok {
					t.Fatalf("данные события routing: %T", ev.Data)
				}
				return
			}
		case <-deadline:
			t.Fatal("события routing нет")
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRouterRoutingDrift(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	// Предпросмотр до взятия: 05_routing.json уйдёт, смешанный 00_api.json останется.
	pv, err := e.layer.RoutingPreview()
	if err != nil {
		t.Fatalf("RoutingPreview: %v", err)
	}
	if pv.Xray == nil || !containsString(pv.Xray.Move, rtTkRoutingFile) || !containsString(pv.Xray.Left, rtTkAPIFile) {
		t.Fatalf("предпросмотр Xray: %+v", pv.Xray)
	}
	if pv.Mihomo == nil || pv.Mihomo.PrevProfile != "profiles/user.yaml" {
		t.Fatalf("предпросмотр Mihomo: %+v", pv.Mihomo)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Fatalf("предпросмотр изменил %s", rtTkRoutingFile)
	}

	e.setManaged(t, true)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}
	snap := e.layer.Snapshot()
	if !snap.Routing.Desired || snap.Routing.Xray == nil || snap.Routing.Mihomo == nil {
		t.Fatalf("раздел routing снимка: %+v", snap.Routing)
	}
	x, m := snap.Routing.Xray, snap.Routing.Mihomo
	if !x.Taken || !equalStrings(x.Moved, []string{rtTkRoutingFile}) || !containsString(x.Left, rtTkAPIFile) || len(x.Drift) != 0 {
		t.Errorf("Xray в снимке: %+v", x)
	}
	if !m.Taken || m.PrevProfile != "profiles/user.yaml" || len(m.Drift) != 0 || snap.DriftCount != 0 {
		t.Errorf("Mihomo в снимке: %+v, drift_count %d", m, snap.DriftCount)
	}

	// Дрейф профиля: config.yaml переложили снаружи на прежний профиль.
	cfg := e.mihomo("config.yaml")
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.link, cfg); err != nil {
		t.Fatal(err)
	}
	snap = e.layer.Snapshot()
	if !equalStrings(snap.Routing.Mihomo.Drift, []string{"config.yaml"}) || snap.DriftCount == 0 {
		t.Fatalf("дрейф профиля не виден: %+v, drift_count %d", snap.Routing.Mihomo, snap.DriftCount)
	}
	if err := e.layer.StartApply(true); !errors.Is(err, ErrDriftBlocked) {
		t.Fatalf("StartApply при дрейфе профиля: %v, ожидался ErrDriftBlocked", err)
	}
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.ReclaimRouting(e.layer.Context(), KernelMihomo); err != nil {
		t.Fatalf("ReclaimRouting mihomo: %v", err)
	}
	waitRoutingEvent(t, events)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	snap = e.layer.Snapshot()
	if len(snap.Routing.Mihomo.Drift) != 0 || snap.DriftCount != 0 || snap.Routing.Mihomo.PrevProfile != "profiles/user.yaml" {
		t.Errorf("после возврата профиля: %+v, drift_count %d", snap.Routing.Mihomo, snap.DriftCount)
	}
	if err := e.layer.ReclaimRouting(e.layer.Context(), KernelMihomo); !errors.Is(err, ErrNothingToReclaim) {
		t.Errorf("повторное «вернуть профиль»: %v, ожидался ErrNothingToReclaim", err)
	}

	// Чужой routing вернулся (05_routing.json от XKeen): дрейф, «вернуть» уносит его в копию.
	if err := os.WriteFile(e.xray(rtTkRoutingFile), []byte(rtRelForeignBody), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = e.layer.Snapshot()
	if !equalStrings(snap.Routing.Xray.Drift, []string{rtTkRoutingFile}) || snap.DriftCount == 0 {
		t.Fatalf("дрейф routing не виден: %+v, drift_count %d", snap.Routing.Xray, snap.DriftCount)
	}
	if err := e.layer.ReclaimRouting(e.layer.Context(), KernelXray); err != nil {
		t.Fatalf("ReclaimRouting xray: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("чужой файл остался в configs/: %v", err)
	}
	st := e.layer.store.Snapshot()
	var got *MovedFile
	for i := range st.Routing.Xray.Moved {
		if st.Routing.Xray.Moved[i].Name == rtTkRoutingFile {
			got = &st.Routing.Xray.Moved[i]
		}
	}
	if got == nil || got.Hash != HashContent([]byte(rtRelForeignBody)) || hashOf(movedCopyPath(*got)) != got.Hash {
		t.Fatalf("перенесённый файл не в копии: %+v", st.Routing.Xray.Moved)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят: %+v", st.Journal)
	}
	if snap = e.layer.Snapshot(); len(snap.Routing.Xray.Drift) != 0 || snap.DriftCount != 0 {
		t.Errorf("дрейф не пропал: %+v", snap.Routing.Xray)
	}
	e.assertUntouched(t)

	// Смешанный чужой файл с routing после файла панели: дрейф, «вернуть» отказывает.
	if err := os.WriteFile(e.xray(rtRelMixedFile), []byte(rtRelMixedBody), 0o644); err != nil {
		t.Fatal(err)
	}
	snap = e.layer.Snapshot()
	if !equalStrings(snap.Routing.Xray.Drift, []string{rtRelMixedFile}) {
		t.Fatalf("дрейф смешанного файла не виден: %+v", snap.Routing.Xray)
	}
	err = e.layer.ReclaimRouting(e.layer.Context(), KernelXray)
	var fm *ForeignMixedError
	if !errors.Is(err, ErrForeignMixed) || !errors.As(err, &fm) || fm.File != rtRelMixedFile {
		t.Fatalf("ReclaimRouting смешанного файла: %v", err)
	}
	if hashOf(e.xray(rtRelMixedFile)) != HashContent([]byte(rtRelMixedBody)) {
		t.Error("смешанный файл изменён")
	}
	if err := e.layer.StartApply(true); !errors.Is(err, ErrDriftBlocked) {
		t.Fatalf("StartApply при чужом routing: %v, ожидался ErrDriftBlocked", err)
	}

	// «Отпустить»: желаемое managed=false проходит несмотря на дрейф и возвращает файлы.
	e.setManaged(t, false)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("возврат при дрейфе: %+v", res)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != HashContent([]byte(rtRelForeignBody)) {
		t.Errorf("вернулась не последняя копия %s: %q", rtTkRoutingFile, got)
	}
	e.assertConfigLink(t, e.link)
	e.assertNoPanelFiles(t)
	if hashOf(e.xray(rtRelMixedFile)) != HashContent([]byte(rtRelMixedBody)) {
		t.Error("смешанный файл тронут при возврате")
	}
	if st := e.layer.store.Snapshot(); !st.Routing.isEmpty() || st.Journal != nil {
		t.Errorf("состояние после возврата: routing %+v, журнал %+v", st.Routing, st.Journal)
	}
}

func TestRouterKernelInstalledAutoTake(t *testing.T) {
	e := rtNewTakeoverEnv(t)

	// Установлен только Xray: взятие касается его одного.
	e.noMihomo.Store(true)
	e.setManaged(t, true)
	if res := e.apply(t); !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие при одном Xray: %+v", res)
	}
	st := e.layer.store.Snapshot()
	if st.Routing.Xray == nil || st.Routing.Mihomo != nil {
		t.Fatalf("State.Routing после взятия Xray: %+v", st.Routing)
	}
	e.assertConfigLink(t, e.link)
	xrayHashes := map[string]string{
		rtTkPanelRoute: hashOf(e.xray(rtTkPanelRoute)),
		rtTkPanelTail:  hashOf(e.xray(rtTkPanelTail)),
	}
	xrayState, err := json.Marshal(st.Routing.Xray)
	if err != nil {
		t.Fatal(err)
	}

	// Mihomo установили позже: его маршрутизация берётся в фоне той же сборкой.
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	e.noMihomo.Store(false)
	e.layer.OnKernelInstalled(KernelMihomo)
	rtWaitFor(t, "фоновое взятие Mihomo после установки", func() bool {
		s := e.layer.store.Snapshot()
		return s.Routing.Mihomo != nil && s.Journal == nil
	})
	waitRoutingEvent(t, events)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); err != nil {
		t.Errorf("профиль панели не записан: %v", err)
	}
	if m := e.layer.store.Snapshot().Routing.Mihomo; m == nil || m.PrevProfile != "profiles/user.yaml" {
		t.Errorf("State.Routing.Mihomo: %+v", m)
	}
	rtWaitFor(t, "уведомление routing_auto_taken:mihomo", func() bool {
		for _, n := range e.layer.Snapshot().Notices {
			if n.ID == "routing_auto_taken:mihomo" && n.Kernel == KernelMihomo {
				return true
			}
		}
		return false
	})

	// Xray не тронут: файлы панели и запись о владении прежние.
	for name, want := range xrayHashes {
		if got := hashOf(e.xray(name)); got != want {
			t.Errorf("%s изменён при установке Mihomo: %q, ожидался %q", name, got, want)
		}
	}
	after, err := json.Marshal(e.layer.store.Snapshot().Routing.Xray)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(xrayState) {
		t.Errorf("State.Routing.Xray изменено установкой Mihomo:\n%s\n%s", xrayState, after)
	}
}

// waitApplyDone ждёт итога применения из подписки.
func waitApplyDone(t *testing.T, events <-chan Event) ApplyView {
	t.Helper()
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("подписка закрыта до apply_done")
			}
			if ev.Type == EventApplyDone {
				view, ok := ev.Data.(ApplyView)
				if !ok || view.Result == nil {
					t.Fatalf("apply_done без итога: %+v", ev.Data)
				}
				return view
			}
		case <-deadline:
			t.Fatal("применение не завершилось")
		}
	}
}

// «Применить» по кнопке сразу после apply_done не получает «применение уже идёт»:
// итог публикуется ещё под замком применения (дальше идёт хвост запуска —
// уведомления и событие routing), и запуск, пришедший в этот хвост, ждёт замок.
func TestRouterApplyRightAfterDone(t *testing.T) {
	e := rtNewTakeoverEnv(t)
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	for i := 1; i <= 200; i++ {
		if err := e.layer.StartApply(true); err != nil {
			t.Fatalf("запуск %d сразу после итога прошлого: %v", i, err)
		}
		if view := waitApplyDone(t, events); !view.Result.OK {
			t.Fatalf("запуск %d: %+v", i, view.Result)
		}
	}
}
