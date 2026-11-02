//go:build router

package configlayer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Разделение владения маршрутизацией по ядрам на устройстве (147.1, D-06, D-07):
// настоящий слой, хранилище, `xray run -test` и `mihomo -t` роутера, временный
// каталог в рабочем каталоге теста. Работающие ядра стенда не трогаются, ядро
// тест не читает и не переключает — метка ядра не нужна.

// rtSplitGen выдаёт файлы по ядрам: routing Xray — когда панель держит Xray,
// профиль Mihomo с портами прежнего профиля — когда панель держит Mihomo.
type rtSplitGen struct{}

func (rtSplitGen) ID() string { return "rt-split" }

func (rtSplitGen) Generate(in Input, _ InstalledKernels) ([]GeneratedFile, error) {
	desired := DesiredRouting(in.Sections)
	var out []GeneratedFile
	if desired.ManagedFor(KernelXray) {
		route, err := json.Marshal(map[string]any{"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          []map[string]any{{"type": "field", "network": "tcp,udp", "outboundTag": "xcp-block"}},
		}})
		if err != nil {
			return nil, err
		}
		tail, err := json.Marshal(map[string]any{"outbounds": []map[string]any{{"tag": "xcp-block", "protocol": "blackhole"}}})
		if err != nil {
			return nil, err
		}
		out = append(out,
			GeneratedFile{Kernel: KernelXray, RelPath: rtTkPanelRoute, Kind: KindXrayJSON, Content: route},
			GeneratedFile{Kernel: KernelXray, RelPath: rtTkPanelTail, Kind: KindXrayJSON, Content: tail},
		)
	}
	if desired.ManagedFor(KernelMihomo) {
		profile := fmt.Sprintf("mode: rule\nredir-port: %d\ntproxy-port: %d\nrules:\n  - MATCH,DIRECT\n",
			in.Routing.Mihomo.Redir, in.Routing.Mihomo.TProxy)
		out = append(out, GeneratedFile{Kernel: KernelMihomo, RelPath: PanelMihomoProfile, Kind: KindMihomoProfile, Content: []byte(profile)})
	}
	return out, nil
}

// rtNewSplitEnv — слой с раскладкой rtWriteTakeoverLayout и генератором rtSplitGen.
func rtNewSplitEnv(t *testing.T) *rtTakeoverEnv { return rtNewSplitEnvKind(t, false) }

// rtNewSplitEnvKind — то же; plain делает config.yaml обычным файлом.
func rtNewSplitEnvKind(t *testing.T, plain bool) *rtTakeoverEnv {
	t.Helper()
	for _, bin := range []string{rtRefreshXrayBin, rtMhBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("ядро не установлено на устройстве: %v", err)
		}
	}
	dir := rtLiveDir(t)
	roots, hashes, link := rtWriteTakeoverLayout(t, dir, plain)
	e := &rtTakeoverEnv{gen: &rtTakeoverGen{}, roots: roots, hashes: hashes, link: link}
	e.enabled.Store(true)
	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       e.enabled.Load,
		Binaries:      func() Binaries { return Binaries{Xray: rtRefreshXrayBin, Mihomo: rtMhBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	if err := layer.RegisterGenerator(rtSplitGen{}); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	e.layer = layer
	return e
}

// setRouting записывает секцию routing как есть и применяет черновик.
func (e *rtTakeoverEnv) setRouting(t *testing.T, raw string) *ResultView {
	t.Helper()
	e.edit(t, RoutingSection, json.RawMessage(raw))
	res := e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("применение секции %s: %+v", raw, res)
	}
	return res
}

func TestRouterProfileSplitOps(t *testing.T) {
	e := rtNewSplitEnv(t)

	// Оба ядра у панели: секция без поля mihomo ведёт себя как раньше.
	e.setRouting(t, `{"v":1,"managed":true}`)
	st := e.layer.store.Snapshot()
	if st.Routing.Xray == nil || st.Routing.Mihomo == nil {
		t.Fatalf("оба ядра должны быть взяты: %+v", st.Routing)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// Mihomo на своём профиле, Xray у панели: нужен только возврат Mihomo.
	e.setRouting(t, `{"v":1,"managed":true,"mihomo":false}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo != nil {
		t.Errorf("State.Routing.Mihomo не снят: %+v", st.Routing.Mihomo)
	}
	if st.Routing.Xray == nil || len(st.Routing.Xray.Moved) != 1 || st.Routing.Xray.Moved[0].Name != rtTkRoutingFile {
		t.Errorf("State.Routing.Xray тронут отпусканием Mihomo: %+v", st.Routing.Xray)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); !os.IsNotExist(err) {
		t.Errorf("файл routing Xray вернулся при отпускании Mihomo: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл routing панели Xray пропал: %v", err)
	}
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); !os.IsNotExist(err) {
		t.Errorf("профиль панели Mihomo остался на диске: %v", err)
	}
	e.assertConfigLink(t, e.link)

	// Снимок слоя: владение по ядрам, активный свой профиль Mihomo.
	snap := e.layer.Snapshot().Routing
	if !snap.Desired || !snap.DesiredXray || snap.DesiredMihomo {
		t.Errorf("снимок: desired=%v desired_xray=%v desired_mihomo=%v", snap.Desired, snap.DesiredXray, snap.DesiredMihomo)
	}
	if snap.MihomoActive != "user" {
		t.Errorf("mihomo_active = %q, ожидалось user", snap.MihomoActive)
	}

	// Всё отпущено: Xray возвращается, Mihomo остаётся на своём профиле.
	e.setRouting(t, `{"v":1,"managed":false}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Xray != nil || st.Routing.Mihomo != nil {
		t.Errorf("State.Routing не пуст после отпускания: %+v", st.Routing)
	}
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("чужой файл routing вернулся не побайтно: %q", got)
	}
	e.assertConfigLink(t, e.link)

	// Только Mihomo у панели, Xray за XKeen.
	e.setRouting(t, `{"v":1,"managed":false,"mihomo":true}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo == nil || st.Routing.Xray != nil {
		t.Fatalf("ожидался взятый Mihomo без Xray: %+v", st.Routing)
	}
	if _, err := os.Lstat(e.xray(rtTkRoutingFile)); err != nil {
		t.Errorf("файл routing Xray тронут взятием Mihomo: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); !os.IsNotExist(err) {
		t.Errorf("файл routing панели Xray записан без управления: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	snap = e.layer.Snapshot().Routing
	if !snap.Desired || snap.DesiredXray || !snap.DesiredMihomo || snap.MihomoActive != "" {
		t.Errorf("снимок: %+v", snap)
	}

	// Секция без поля после этого ведёт себя как раньше: Xray берётся, Mihomo
	// остаётся у панели, затем отпускание возвращает оба.
	e.setRouting(t, `{"v":1,"managed":true}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo == nil || st.Routing.Xray == nil {
		t.Fatalf("ожидались оба ядра: %+v", st.Routing)
	}
	e.setRouting(t, `{"v":1,"managed":false}`)
	st = e.layer.store.Snapshot()
	if st.Routing.Mihomo != nil || st.Routing.Xray != nil {
		t.Errorf("State.Routing не пуст: %+v", st.Routing)
	}
	e.assertConfigLink(t, e.link)
	e.assertUntouched(t)
	if got := hashOf(e.xray(rtTkRoutingFile)); got != e.hashes[rtTkRoutingFile] {
		t.Errorf("чужой файл routing вернулся не побайтно: %q", got)
	}
}

// writeProfile кладёт профиль в profiles/ и запоминает его хэш.
func (e *rtTakeoverEnv) writeProfile(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(e.mihomo("profiles/"+name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	e.hashes["profiles/"+name+".yaml"] = HashContent([]byte(body))
}

// targetLink — текст симлинка config.yaml на профиль profiles/<name>.yaml.
func (e *rtTakeoverEnv) targetLink(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(e.roots.Mihomo)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(abs, "profiles", name+".yaml")
}

// assertProfileKept проверяет, что профиль пользователя на месте побайтно.
func (e *rtTakeoverEnv) assertProfileKept(t *testing.T, name string) {
	t.Helper()
	rel := "profiles/" + name + ".yaml"
	if got := hashOf(e.mihomo(rel)); got != e.hashes[rel] {
		t.Errorf("%s не на месте или изменён: %q", rel, got)
	}
}

// hasIssue — среди проблем сборки итога есть проблема с кодом.
func hasIssue(res *ResultView, code string) bool {
	for _, is := range res.Issues {
		if is.Code == code {
			return true
		}
	}
	return false
}

// Отпускание Mihomo с целью (147.1, D-06, D-10): одно «Применить» переводит
// config.yaml на выбранный профиль, цель проверяет настоящий `mihomo -t`, отказ
// и сбой записи оставляют всё как было.
func TestRouterProfileTargetRelease(t *testing.T) {
	e := rtNewSplitEnv(t)
	e.writeProfile(t, "other", rtTkUserProfile)
	const toOther = `{"v":1,"managed":true,"mihomo":false,"mihomo_profile":"other"}`

	// Панель держит оба ядра; затем Mihomo уходит на профиль other одним применением.
	e.setRouting(t, `{"v":1,"managed":true}`)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	e.setRouting(t, toOther)
	e.assertConfigLink(t, e.targetLink(t, "other"))
	st := e.layer.store.Snapshot()
	if st.Routing.Mihomo != nil {
		t.Errorf("State.Routing.Mihomo не снят: %+v", st.Routing.Mihomo)
	}
	if st.Routing.Xray == nil || len(st.Routing.Xray.Moved) != 1 {
		t.Errorf("Xray тронут отпусканием Mihomo: %+v", st.Routing.Xray)
	}
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); !os.IsNotExist(err) {
		t.Errorf("профиль панели остался на диске: %v", err)
	}
	if _, err := os.Lstat(e.xray(rtTkPanelRoute)); err != nil {
		t.Errorf("файл routing панели Xray пропал: %v", err)
	}
	e.assertProfileKept(t, "user")
	e.assertProfileKept(t, "other")
	if st.Journal != nil {
		t.Errorf("журнал не снят после применения: %+v", st.Journal)
	}

	// Отказ ядра: цель с неверным YAML не проходит mihomo -t, всё как было.
	e.setRouting(t, `{"v":1,"managed":true}`)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	e.writeProfile(t, "broken", "mode: rule\nrules: [unterminated\n")
	e.edit(t, RoutingSection, json.RawMessage(`{"v":1,"managed":true,"mihomo":false,"mihomo_profile":"broken"}`))
	res := e.apply(t)
	if res.OK || res.Code != CodeValidationFailed || res.Kernel != KernelMihomo {
		t.Fatalf("итог при неверной цели: %+v", res)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); err != nil {
		t.Errorf("профиль панели пропал при отказе проверки: %v", err)
	}
	e.assertProfileKept(t, "broken")
	e.assertProfileKept(t, "other")

	// Цели нет на диске: сборка не проходит, проблема mihomo_target_missing.
	e.edit(t, RoutingSection, json.RawMessage(`{"v":1,"managed":true,"mihomo":false,"mihomo_profile":"gone"}`))
	res = e.apply(t)
	if res.OK || res.Code != ResultBuildFailed || !hasIssue(res, IssueMihomoTargetMissing) {
		t.Fatalf("итог при отсутствующей цели: %+v", res)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// Профиль панели целью быть не может, хотя файл есть.
	e.edit(t, RoutingSection, json.RawMessage(`{"v":1,"managed":true,"mihomo":false,"mihomo_profile":"xcp-main"}`))
	res = e.apply(t)
	if res.OK || res.Code != ResultBuildFailed || !hasIssue(res, IssueMihomoTargetMissing) {
		t.Fatalf("итог при цели-профиле панели: %+v", res)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))

	// Сбой записи (D-23): перекладке config.yaml мешает непустой каталог на месте
	// временного симлинка. Откат возвращает ссылку на профиль панели и сам профиль.
	e.edit(t, RoutingSection, json.RawMessage(toOther))
	blocker := e.mihomo("config.yaml.xcp-link")
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	res = e.apply(t)
	if res.OK || res.Code != ResultWriteFailed || !res.RolledBack {
		t.Fatalf("итог при сбое записи: %+v", res)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); err != nil {
		t.Errorf("профиль панели не возвращён откатом: %v", err)
	}
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}

	// Без помехи то же применение проходит.
	res = e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("отпускание после отката: %+v", res)
	}
	e.assertConfigLink(t, e.targetLink(t, "other"))
	e.assertProfileKept(t, "user")
	e.assertProfileKept(t, "other")
}

// Прежний обычный файл XKeen при отпускании Mihomo с целью не теряется (D-27):
// он становится profiles/config-xkeen[-N].yaml, содержимое то же побайтно; сбой
// записи и прерванная запись возвращают всё.
func TestRouterProfileTargetKeepsXKeen(t *testing.T) {
	e := rtNewSplitEnvKind(t, true)
	e.writeProfile(t, "other", rtTkUserProfile)
	const toOther = `{"v":1,"managed":true,"mihomo":false,"mihomo_profile":"other"}`
	cfg := e.mihomo("config.yaml")
	keep := e.mihomo("profiles/config-xkeen.yaml")
	keep2 := e.mihomo("profiles/config-xkeen-2.yaml")

	// Взятие: config.yaml стал ссылкой на профиль панели, прежний файл — в наборе.
	e.setRouting(t, `{"v":1,"managed":true}`)
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	m := e.layer.store.Snapshot().Routing.Mihomo
	if m == nil || m.PrevKind != configKindFile || m.PrevBackup == "" {
		t.Fatalf("State.Routing.Mihomo: %+v", m)
	}
	copyPath := filepath.Join(m.PrevBackup, KernelMihomo, "config.yaml")
	wantHash := e.hashes["config.yaml"]

	// Сбой записи после переноса: помеха перекладке ссылки. Файла config-xkeen нет,
	// config.yaml снова на профиле панели, копия в наборе на месте.
	e.edit(t, RoutingSection, json.RawMessage(toOther))
	blocker := e.mihomo("config.yaml.xcp-link")
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := e.apply(t)
	if res.OK || res.Code != ResultWriteFailed || !res.RolledBack {
		t.Fatalf("итог при сбое записи: %+v", res)
	}
	if _, err := os.Lstat(keep); !os.IsNotExist(err) {
		t.Errorf("config-xkeen.yaml остался после отката: %v", err)
	}
	e.assertConfigLink(t, panelProfileLink(e.roots.Mihomo))
	if _, err := os.Lstat(e.mihomo(PanelMihomoProfile)); err != nil {
		t.Errorf("профиль панели не возвращён откатом: %v", err)
	}
	if got := hashOf(copyPath); got != wantHash {
		t.Errorf("копия прежнего файла в наборе изменена: %q", got)
	}
	if err := os.RemoveAll(blocker); err != nil {
		t.Fatal(err)
	}

	// Без помехи: прежний файл XKeen стал профилем побайтно, config.yaml -> other.
	res = e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("отпускание: %+v", res)
	}
	if got := hashOf(keep); got != wantHash {
		t.Errorf("config-xkeen.yaml не совпадает с прежним config.yaml: %q", got)
	}
	e.assertConfigLink(t, e.targetLink(t, "other"))
	e.assertProfileKept(t, "user")
	e.assertProfileKept(t, "other")
	if st := e.layer.store.Snapshot(); st.Routing.Mihomo != nil || st.Journal != nil {
		t.Errorf("состояние после отпускания: routing=%+v journal=%+v", st.Routing.Mihomo, st.Journal)
	}

	// Коллизия: config-xkeen.yaml занят, новый файл получает суффикс -2, прежний
	// файл не тронут. Обычный config.yaml возвращается руками, как у пользователя.
	e.setRouting(t, `{"v":1,"managed":false}`)
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	const second = rtTkUserProfile + "# вторая правка\n"
	if err := os.WriteFile(cfg, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	e.setRouting(t, `{"v":1,"managed":true}`)
	e.setRouting(t, toOther)
	if got := hashOf(keep2); got != HashContent([]byte(second)) {
		t.Errorf("config-xkeen-2.yaml не совпадает с прежним config.yaml: %q", got)
	}
	if got := hashOf(keep); got != wantHash {
		t.Errorf("прежний config-xkeen.yaml тронут: %q", got)
	}
	e.assertConfigLink(t, e.targetLink(t, "other"))

	// Прерванная запись: журнал с MihomoKept обрабатывается RecoverJournal, файл
	// убран, если его содержимое осталось записанным панелью; чужое не трогается.
	body := []byte("# записано панелью\n")
	for _, tc := range []struct {
		name     string
		onDisk   []byte
		wantGone bool
	}{{"файл панели", body, true}, {"чужое содержимое", []byte("# чужое\n"), false}} {
		path := e.mihomo("profiles/config-xkeen-9.yaml")
		if err := os.WriteFile(path, tc.onDisk, 0o644); err != nil {
			t.Fatal(err)
		}
		set, err := NewBackupSet(e.layer.opts.DataDir, time.Now(), "rt-recover")
		if err != nil {
			t.Fatal(err)
		}
		if err := set.WriteMeta(); err != nil {
			t.Fatal(err)
		}
		err = e.layer.store.Update(func(st *State) error {
			st.Journal = &Journal{
				BackupDir: set.Dir, Trigger: "rt-recover", StartedAt: time.Now().UTC(),
				Routing: &JournalRouting{MihomoKept: &MovedFile{Name: "profiles/config-xkeen-9.yaml", Hash: HashContent(body)}},
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := RecoverJournal(e.layer.store, e.roots); !ok || err != nil {
			t.Fatalf("%s: RecoverJournal: %v, %v", tc.name, ok, err)
		}
		_, statErr := os.Lstat(path)
		if gone := os.IsNotExist(statErr); gone != tc.wantGone {
			t.Errorf("%s: файл убран = %v, ожидалось %v", tc.name, gone, tc.wantGone)
		}
		_ = os.Remove(path)
	}
}
