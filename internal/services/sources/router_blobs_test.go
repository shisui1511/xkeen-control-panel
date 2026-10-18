//go:build router

package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Защита снимков подписок: удержание «в полёте», уборка без гонки с обновлением,
// пропавший снимок как ошибка сборки. Данные узлов — ссылки testdata/links; вердикт —
// файлы на диске и ответы слоя «Конфигурация».

// rtSubNamed — секция подписки с заданным именем и исключениями.
func rtSubNamed(t *testing.T, id, name string, exclude []string) json.RawMessage {
	t.Helper()
	if exclude == nil {
		exclude = []string{}
	}
	raw, err := json.Marshal(SubscriptionConfig{
		Kind: KindSubscription, ID: id, Name: name, URL: "https://provider.invalid/sub", Enabled: true,
		Exclude: exclude, Interval: Interval{Mode: IntervalProvider},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rtAddSub кладёт в черновик секцию подписки и живые данные: по ядру либо ожидающий
// снимок, либо (applied=true) применённый.
func rtAddSub(t *testing.T, e *rtEnv, section json.RawMessage, id string, applied bool, refs map[string]*Ref) {
	t.Helper()
	src := &LiveSource{Kernels: map[string]*KernelLive{}}
	for kernel, ref := range refs {
		kl := &KernelLive{Status: Status{Code: StatusOK, Since: e.clock.Now()}}
		if applied {
			kl.Applied = ref
		} else {
			kl.Pending = ref
		}
		src.Kernels[kernel] = kl
	}
	_, err := e.layer.EditDraftBatch(e.rev(t), map[string]json.RawMessage{sectionName(id): section}, func(l configlayer.Sections) error {
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
}

func rtExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func rtBlobPath(e *rtEnv, hash string) string {
	return filepath.Join(e.svc.blobs.dir, hash+".json")
}

func rtRefOf(e *rtEnv, hash string, list []nodes.Node) *Ref {
	return &Ref{Hash: hash, FetchedAt: e.clock.Now(), Format: "links", Total: len(list), Usable: len(list)}
}

// Кандидат обновления записан через putHeld: уборка посреди обновления его не
// трогает, «Обновить» пишет файл ядра из снимка, после фиксации прежний
// применённый снимок убирается, новый остаётся.
func TestRouterGCKeepsInflightCandidate(t *testing.T) {
	rtNeedKernels(t, true, false)
	e := rtNewEnv(t, true, false)
	const id = "a1b2c3d4"
	first := rtPut(t, e, "vless_ws", "trojan_grpc")
	rtAddSub(t, e, rtSubNamed(t, id, "Провайдер", nil), id, false, map[string]*Ref{kernelXray: first})
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("первое применение: %+v", done.Result)
	}

	list := rtNodes(t, "vless_ws", "trojan_grpc", "ss_plain")
	hash, release, err := e.svc.blobs.putHeld(list)
	if err != nil {
		t.Fatalf("putHeld: %v", err)
	}
	defer release()
	e.svc.collectGarbage()
	if !rtExists(rtBlobPath(e, hash)) {
		t.Fatal("уборка удалила снимок кандидата в полёте")
	}
	if !rtExists(rtBlobPath(e, first.Hash)) {
		t.Fatal("уборка удалила применённый снимок")
	}

	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := subscriptionConfig(st, id)
	if err != nil {
		t.Fatal(err)
	}
	overlay := configlayer.Sections{}
	if err := writeCandidates(overlay, Candidates{id: {kernelXray: *rtRefOf(e, hash, list)}}, map[string]SubscriptionConfig{id: cfg}); err != nil {
		t.Fatal(err)
	}
	view, err := e.layer.RunSourceRefresh(context.Background(), overlay)
	if err != nil {
		t.Fatalf("RunSourceRefresh: %v", err)
	}
	if view.Result == nil || view.Result.Code != configlayer.ResultApplied {
		t.Fatalf("обновление: %+v", view.Result)
	}
	data, err := os.ReadFile(filepath.Join(e.roots.Xray, "04_outbounds.xcp-"+id+".tail.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &f); err != nil || len(f.Outbounds) != 3 {
		t.Fatalf("файл Xray после обновления: outbound %d, ошибка %v", len(f.Outbounds), err)
	}
	_, _, live := rtDraftSubscription(t, e)
	if kl := live[id].Kernels[kernelXray]; kl == nil || kl.Applied == nil || kl.Applied.Hash != hash {
		t.Fatalf("применённый снимок после обновления: %+v", kl)
	}

	release()
	e.svc.collectGarbage()
	if !rtExists(rtBlobPath(e, hash)) {
		t.Error("после release применённый снимок удалён")
	}
	if rtExists(rtBlobPath(e, first.Hash)) {
		t.Error("прежний применённый снимок остался после фиксации нового")
	}
}

// Снимок в полёте переживает уборку; после последнего release он подчиняется
// обычным правилам. Ссылки из Live и токены предпросмотра держат снимок сами.
func TestRouterGCKeepRules(t *testing.T) {
	e := rtNewEnv(t, false, false)
	e.installed = configlayer.InstalledKernels{Xray: true, Mihomo: true}
	const id = "a1b2c3d4"

	applied := rtPut(t, e, "vless_ws")
	pending := rtPut(t, e, "vless_ws", "trojan_ws")
	tokenRef := rtPut(t, e, "tuic")
	orphan := rtPut(t, e, "ss_plain")
	rtAddSub(t, e, rtSubNamed(t, id, "Провайдер", nil), id, true, map[string]*Ref{kernelXray: applied})
	// Ожидающий снимок второго ядра.
	if err := e.layer.UpdateLive(func(l configlayer.Sections) error {
		cur, err := readLive(l)
		if err != nil {
			return err
		}
		cur[id].Kernels[kernelMihomo] = &KernelLive{Pending: pending, Status: Status{Code: StatusOK}}
		return writeLive(l, cur)
	}); err != nil {
		t.Fatal(err)
	}
	// Токен предпросмотра с кандидатом подписки.
	cand := &subCandidate{kernels: map[string]*kernelOutcome{kernelMihomo: {Ref: tokenRef}}, diagID: "0f0f0f0f"}
	if _, _, err := e.svc.putPreview(&previewEntry{subs: []*subCandidate{cand}}); err != nil {
		t.Fatal(err)
	}
	// Мусорный файл вида снимка и посторонний файл в каталоге.
	stray := filepath.Join(e.svc.blobs.dir, "notes.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	e.svc.collectGarbage()
	for name, ref := range map[string]*Ref{"applied": applied, "pending": pending, "токен": tokenRef} {
		if !rtExists(rtBlobPath(e, ref.Hash)) {
			t.Errorf("снимок %s удалён уборкой", name)
		}
	}
	if rtExists(rtBlobPath(e, orphan.Hash)) {
		t.Error("снимок без ссылок остался")
	}
	if !rtExists(stray) {
		t.Error("уборка удалила посторонний файл каталога")
	}

	// Токен истёк: его снимок больше никому не нужен.
	e.clock.Advance(previewTTL + time.Second)
	e.svc.collectGarbage()
	if rtExists(rtBlobPath(e, tokenRef.Hash)) {
		t.Error("снимок истёкшего токена остался")
	}
}

// Гонка уборки с удержанием: 50 горутин пишут и удерживают снимки, пока идёт
// двадцать уборок. Ни один удержанный в момент уборки снимок не удаляется; при двух
// hold одного хэша снимок освобождается после второго release.
func TestRouterBlobsHoldReleaseRace(t *testing.T) {
	e := rtNewEnv(t, false, false)
	b := e.svc.blobs
	names := [][]string{{"vless_ws"}, {"trojan_ws"}, {"vless_reality"}, {"tuic"}, {"ss_plain"}, {"vmess_ws_tls"}}
	sets := make([][]nodes.Node, len(names))
	for i, n := range names {
		sets[i] = rtNodes(t, n...)
	}

	var wg sync.WaitGroup
	errs := make(chan string, 200)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			list := sets[i%len(sets)]
			hash, release, err := b.putHeld(list)
			if err != nil {
				errs <- "putHeld: " + err.Error()
				return
			}
			rel2 := b.hold(hash)
			for range 5 {
				if !rtExists(rtBlobPath(e, hash)) {
					errs <- "удержанный снимок удалён уборкой: " + hash[:8]
					break
				}
				time.Sleep(time.Millisecond)
			}
			release()
			release() // повторный release безопасен
			if !rtExists(rtBlobPath(e, hash)) {
				errs <- "снимок удалён, пока не отпущен второй hold: " + hash[:8]
			}
			rel2()
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.svc.collectGarbage()
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}

	// Всё отпущено: уборка убирает снимки, ссылок на них нет.
	e.svc.collectGarbage()
	for _, set := range sets {
		data, hash, err := b.encode(set)
		_ = data
		if err != nil {
			t.Fatal(err)
		}
		if rtExists(rtBlobPath(e, hash)) {
			t.Errorf("после последнего release снимок %s остался", hash[:8])
		}
	}

	// Два hold одного хэша: снимок живёт до второго release.
	hash, rel1, err := b.putHeld(sets[0])
	if err != nil {
		t.Fatal(err)
	}
	rel2 := b.hold(hash)
	rel1()
	e.svc.collectGarbage()
	if !rtExists(rtBlobPath(e, hash)) {
		t.Fatal("снимок удалён при втором hold")
	}
	rel2()
	rel2()
	e.svc.collectGarbage()
	if rtExists(rtBlobPath(e, hash)) {
		t.Error("снимок остался после последнего release")
	}
}

// putHeld пишет файл 0600 в каталоге 0700, повторная запись того же набора даёт тот
// же хэш и один файл; изменённый или пропавший файл — ошибка чтения.
func TestRouterBlobsPutHeldIntegrity(t *testing.T) {
	e := rtNewEnv(t, false, false)
	b := e.svc.blobs
	list := rtNodes(t, "vless_ws", "trojan_ws")
	h1, rel1, err := b.putHeld(list)
	if err != nil {
		t.Fatal(err)
	}
	defer rel1()
	h2, rel2, err := b.putHeld(rtNodes(t, "trojan_ws", "vless_ws"))
	if err != nil {
		t.Fatal(err)
	}
	defer rel2()
	if h1 != h2 {
		t.Fatalf("хэши одного набора различаются: %s и %s", h1, h2)
	}
	entries, err := os.ReadDir(b.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("файлов снимков: %d, %v", len(entries), err)
	}
	if st, err := os.Stat(b.dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("права каталога: %v %v", st, err)
	}
	path := rtBlobPath(e, h1)
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("права снимка: %v %v", st, err)
	}
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.get(h1); err == nil {
		t.Error("подменённый снимок прочитан")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := b.get(h1); err == nil {
		t.Error("пропавший снимок прочитан")
	}
}

// Пропавший снимок — ошибка сборки, а не пустой файл ядра: «Применить» завершается
// build_failed (generator_failed у sources_xray), файлы источника на диске прежние.
func TestRouterGenerateMissingBlobIsError(t *testing.T) {
	rtNeedKernels(t, true, false)
	e := rtNewEnv(t, true, false)
	const id = "a1b2c3d4"
	ref := rtPut(t, e, "vless_ws", "trojan_grpc")
	rtAddSub(t, e, rtSubNamed(t, id, "Провайдер", nil), id, false, map[string]*Ref{kernelXray: ref})
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("первое применение: %+v", done.Result)
	}
	before := rtTree(t, e.roots.Xray)
	if len(before) == 0 {
		t.Fatal("после применения файлов Xray нет")
	}

	if err := os.Remove(rtBlobPath(e, ref.Hash)); err != nil {
		t.Fatal(err)
	}
	e.svc.mmu.Lock()
	e.svc.gens = map[string]memoFile{}
	e.svc.genOrder = nil
	e.svc.mmu.Unlock()
	// Правка черновика, чтобы «Применить» было что собирать.
	e.addLinks(t, rtLink(t, "vless_reality"))

	done := e.applyAndWait(t)
	if done.Result == nil || done.Result.Code != configlayer.ResultBuildFailed {
		t.Fatalf("применение с пропавшим снимком: %+v", done.Result)
	}
	found := false
	for _, is := range done.Result.Issues {
		if is.Code == configlayer.IssueGeneratorFailed && is.Key == xrayGenID {
			found = true
		}
	}
	if !found {
		t.Errorf("нет проблемы generator_failed у %s: %+v", xrayGenID, done.Result.Issues)
	}
	if changed, removed, added := rtDiff(before, rtTree(t, e.roots.Xray)); len(changed)+len(removed)+len(added) != 0 {
		t.Errorf("файлы Xray изменились: %v %v %v", changed, removed, added)
	}
}

// Источник, удалённый из черновика и применённый, уходит из Live; ключ candidates
// в Live не сохраняется.
func TestRouterCommitLiveRemovedSourceDropsLive(t *testing.T) {
	rtNeedKernels(t, true, false)
	e := rtNewEnv(t, true, false)
	// Как на устройстве, каталог Xray не пуст: пустой каталог `xray run -test -confdir`
	// не читает, а ждёт конфиг на stdin и падает на EOF после удаления последнего файла.
	if err := os.WriteFile(filepath.Join(e.roots.Xray, "01_log.json"), []byte("{\"log\":{\"loglevel\":\"none\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const id = "a1b2c3d4"
	ref := rtPut(t, e, "vless_ws")
	rtAddSub(t, e, rtSubNamed(t, id, "Провайдер", nil), id, false, map[string]*Ref{kernelXray: ref})
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("первое применение: %+v", done.Result)
	}
	if _, err := e.layer.EditDraft(e.rev(t), sectionName(id), nil); err != nil {
		t.Fatalf("удаление секции: %v", err)
	}
	if done := e.applyAndWait(t); done.Result == nil || done.Result.Code != configlayer.ResultApplied {
		t.Fatalf("применение удаления: %+v", done.Result)
	}
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	live, err := readLive(st.Live)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := live[id]; ok {
		t.Error("удалённый источник остался в Live")
	}
	if _, ok := st.Live[LiveKeyCandidates]; ok {
		t.Error("ключ candidates сохранён в Live")
	}
}
