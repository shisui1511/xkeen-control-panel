//go:build router

package geodata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing"
	"github.com/shisui1511/xkeen-control-panel/internal/services/routing/lists"
	"github.com/shisui1511/xkeen-control-panel/internal/services/sources"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Сценарии удаления источников dat (D-18, D-19, D-21, D-22, D-37) на устройстве:
// настоящий слой «Конфигурация» во временном каталоге теста, настоящие `xray run
// -test` и `mihomo -t` в «Применить», настоящий сервис маршрутизации и сервис
// менеджера с хуком AfterApply. Файлы dat — копии настоящих dat стенда во временном
// каталоге; xkeen.json — файл во временном каталоге. Каталог dat, xkeen.json и ядро
// стенда тест не трогает и не переключает — метки ядра нет.

const (
	rtRemoveXrayBin   = "/opt/sbin/xray"
	rtRemoveMihomoBin = "/opt/sbin/mihomo"
	// Имена копий настоящих dat в каталоге dat теста.
	rtRemoveFile  = "xcp-rm.dat"
	rtRemoveFile2 = "xcp-rm2.dat"
	rtRemoveKeep  = "xcp-keep.dat"
	rtRemoveZkeen = "geosite_zkeen.dat"
	rtRemoveAlias = "zkeen.dat"
)

// rtRemoveUserProfile — прежний активный профиль Mihomo (как у пользователя XKeen).
const rtRemoveUserProfile = "mode: rule\n" +
	"redir-port: 7000\n" +
	"tproxy-port: 7001\n" +
	"allow-lan: true\n" +
	"proxy-groups:\n" +
	"  - name: PROXY\n" +
	"    type: select\n" +
	"    proxies:\n" +
	"      - DIRECT\n" +
	"rules:\n" +
	"  - MATCH,PROXY\n"

// rtRemoveEnv — окружение сценария: слой, сервисы и каталоги теста.
type rtRemoveEnv struct {
	dir, datDir, xkeenDir, xrayDir, mihomoDir string
	layer                                     *configlayer.Layer
	rt                                        *routing.Service
	geo                                       *Service
	store                                     *lists.GeodataStore
	index                                     *lists.Index
	// kind и tag — вид и тег списка, на который ссылаются правила теста.
	kind, tag string
}

// rtRemovePick выбирает на стенде самый маленький dat (geosite или geoip) с тегом,
// пригодным для правила. Нет такого — тест пропускается.
func rtRemovePick(t *testing.T) (src, kind, tag string) {
	t.Helper()
	ix := lists.NewIndex(lists.DefaultDatDir)
	files, err := ix.Files()
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Size < files[j].Size })
	for _, f := range files {
		if (f.Kind != lists.KindSite && f.Kind != lists.KindIP) || !datNameRe.MatchString(f.Name) {
			continue
		}
		tags, err := ix.Tags(f.Name)
		if err != nil {
			continue
		}
		for _, tg := range tags {
			if tg.Count >= 3 && tg.Count <= 3000 && !strings.ContainsAny(tg.Tag, ":,\" ") {
				return filepath.Join(ix.Dir(), f.Name), f.Kind, tg.Tag
			}
		}
	}
	t.Skip("в каталоге dat стенда нет файла с подходящим списком")
	return "", "", ""
}

// rtRemoveWrite пишет файл теста.
func rtRemoveWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// rtRemoveHasEntry — есть ли в тексте xkeen.json запись источника с этим файлом.
// Комментарии владельца остаются при правке, поэтому по имени подстрокой искать нельзя.
func rtRemoveHasEntry(raw, file string) bool {
	return strings.Contains(raw, `"file": "`+file+`"`) || strings.Contains(raw, `"file":"`+file+`"`)
}

// rtRemoveRead читает файл теста.
func rtRemoveRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(data)
}

// rtRemoveLink читает ссылку узла из данных testdata/links.
func rtRemoveLink(t *testing.T, name string) string {
	t.Helper()
	return strings.TrimSpace(rtRemoveRead(t, filepath.Join(routertest.LinksDir(t), name+".txt")))
}

// rtRemoveNewEnv поднимает окружение: каталог dat с копиями настоящего dat (xcp-rm.dat,
// xcp-rm2.dat, xcp-keep.dat и пара geosite_zkeen.dat с симлинком zkeen.dat), xkeen.json
// с записями xcp-rm.dat и xcp-keep.dat, слой с обоими ядрами, маршрутизация взята,
// есть узел и прежний профиль Mihomo. Хук AfterApply сервиса менеджера зарегистрирован
// в слое до его запуска, как это делает cmd/xcp/main.go.
func rtRemoveNewEnv(t *testing.T) *rtRemoveEnv {
	t.Helper()
	for _, bin := range []string{rtRemoveXrayBin, rtRemoveMihomoBin} {
		if _, err := os.Stat(bin); err != nil {
			t.Skipf("нет %s: ядро не установлено", bin)
		}
	}
	tg := routertest.Current(t)
	src, kind, tag := rtRemovePick(t)
	e := &rtRemoveEnv{dir: rtGeoTempDir(t, tg.WorkDir), kind: kind, tag: tag}
	e.datDir = filepath.Join(e.dir, "dat")
	e.xkeenDir = filepath.Join(e.dir, "xkeen")
	e.xrayDir = filepath.Join(e.dir, "xray")
	e.mihomoDir = filepath.Join(e.dir, "mihomo")
	for _, d := range []string{e.datDir, e.xkeenDir, e.xrayDir, e.mihomoDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{rtRemoveFile, rtRemoveFile2, rtRemoveKeep, rtRemoveZkeen} {
		rtGeoCopy(t, src, filepath.Join(e.datDir, name))
	}
	if err := os.Symlink(rtRemoveZkeen, filepath.Join(e.datDir, rtRemoveAlias)); err != nil {
		t.Fatalf("симлинк пары zkeen: %v", err)
	}
	rtRemoveWrite(t, filepath.Join(e.xkeenDir, "xkeen.json"), rtRemoveXKeenJSON(rtRemoveFile, rtRemoveKeep))
	// Прежний профиль Mihomo и config.yaml на него.
	rtRemoveWrite(t, filepath.Join(e.mihomoDir, "profiles", "user.yaml"), rtRemoveUserProfile)
	if err := os.Symlink(filepath.Join(e.mihomoDir, "profiles", "user.yaml"), filepath.Join(e.mihomoDir, "config.yaml")); err != nil {
		t.Fatal(err)
	}

	e.index = lists.NewIndex(e.datDir)
	e.store = rtGeoStore(t, e.xkeenDir)

	layer, err := configlayer.New(configlayer.Options{
		DataDir: e.dir,
		Roots:   configlayer.Roots{Xray: e.xrayDir, Mihomo: e.mihomoDir},
		Enabled: func() bool { return true },
		Binaries: func() configlayer.Binaries {
			return configlayer.Binaries{Xray: rtRemoveXrayBin, Mihomo: rtRemoveMihomoBin}
		},
		XrayEnv:       func(string) []string { return utils.XrayAssetEnv(os.Environ(), "") },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)
	e.layer = layer

	installed := func() configlayer.InstalledKernels { return configlayer.InstalledKernels{Xray: true, Mihomo: true} }
	src2, err := sources.New(sources.Deps{DataDir: e.dir, Layer: layer, Installed: installed})
	if err != nil {
		t.Fatalf("sources.New: %v", err)
	}
	t.Cleanup(src2.Stop)
	rt, err := routing.New(routing.Deps{DataDir: e.dir, Layer: layer, Sources: src2, Lists: e.index, Geodata: e.store, Installed: installed})
	if err != nil {
		t.Fatalf("routing.New: %v", err)
	}
	t.Cleanup(rt.Stop)
	rt.Attach()
	e.rt = rt

	e.geo, err = New(Deps{
		DataDir: filepath.Join(e.dir, "panel"), Layer: layer, Store: e.store, Index: e.index, Routing: rt,
		XrayConfDir: e.xrayDir, MihomoDir: e.mihomoDir, AllowedRoots: []string{e.dir},
	})
	if err != nil {
		t.Fatalf("geodata.New: %v", err)
	}
	t.Cleanup(e.geo.Stop)

	for _, gens := range [][]configlayer.Generator{src2.Generators(), rt.Generators()} {
		for _, g := range gens {
			if err := layer.RegisterGenerator(g); err != nil {
				t.Fatalf("RegisterGenerator %s: %v", g.ID(), err)
			}
		}
	}
	if err := layer.AddLiveCommitter(src2.CommitLive); err != nil {
		t.Fatalf("AddLiveCommitter: %v", err)
	}
	if err := layer.OnDriftResolved(src2.OnDriftResolved); err != nil {
		t.Fatalf("OnDriftResolved: %v", err)
	}
	if err := layer.RegisterChecker(rt.Checker()); err != nil {
		t.Fatalf("RegisterChecker: %v", err)
	}
	if err := layer.Register(configlayer.Hooks{AfterApply: e.geo.AfterApply}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	layer.Start()
	rt.Start()

	view, err := src2.Preview(context.Background(), nodes.Input{Text: rtRemoveLink(t, "vless_ws")})
	if err != nil || view.Token == "" {
		t.Fatalf("Preview узла: %v (токен %q)", err, view.Token)
	}
	if _, err := src2.Add(e.rev(t), view.Token); err != nil {
		t.Fatalf("Add узла: %v", err)
	}
	if _, err := rt.Take(e.rev(t)); err != nil {
		t.Fatalf("Take: %v", err)
	}
	return e
}

// rtRemoveXKeenJSON — xkeen.json с комментариями владельца и записями для файлов
// files (адреса недоступны намеренно: панель сети не касается).
func rtRemoveXKeenJSON(files ...string) string {
	var b strings.Builder
	b.WriteString("{\n  // комментарий владельца\n  \"xkeen\": {\n    \"xray\": {\"geodata\": [\n")
	for i, f := range files {
		b.WriteString("      // про " + f + "\n      {\"file\": \"" + f + "\", \"url\": \"https://example.invalid/" + f + "\"}")
		if i < len(files)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("    ]}\n  }\n}\n")
	return b.String()
}

// rev — текущая ревизия черновика.
func (e *rtRemoveEnv) rev(t *testing.T) int64 {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return st.DraftRevision
}

// state — текущее состояние слоя.
func (e *rtRemoveEnv) state(t *testing.T) configlayer.StateView {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// apply запускает «Применить» и ждёт apply_done: применено или нечего применять.
func (e *rtRemoveEnv) apply(t *testing.T) {
	t.Helper()
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.StartApply(true); err != nil {
		t.Fatalf("«Применить» отклонено: %v", err)
	}
	deadline := time.After(3 * time.Minute)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("шина событий закрыта до apply_done")
			}
			if ev.Type != configlayer.EventApplyDone {
				continue
			}
			view, ok := ev.Data.(configlayer.ApplyView)
			if !ok || view.Result == nil {
				t.Fatalf("apply_done с данными %T", ev.Data)
			}
			res := view.Result
			if !res.OK || (res.Code != configlayer.ResultApplied && res.Code != configlayer.ResultNothingToApply) {
				t.Fatalf("«Применить»: ok=%v code=%q kernel=%q rolled_back=%v issues=%+v", res.OK, res.Code, res.Kernel, res.RolledBack, res.Issues)
			}
			return
		case <-deadline:
			t.Fatal("«Применить» не завершилось за 3 минуты")
		}
	}
}

// waitStep ждёт конца шага удаления после «Применить»: замок свободен, пометки нет в
// применённом состоянии и cond истинно.
func (e *rtRemoveEnv) waitStep(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		st, err := e.layer.ReadState()
		if err != nil {
			t.Fatal(err)
		}
		_, applied := st.Applied[routing.GeodataSection]
		if !applied && e.layer.BusyHolder() == "" && cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("шаг удаления не завершился за 2 минуты: %s (замок: %q)", what, e.layer.BusyHolder())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// pending — незавершённые удаления из состояния менеджера.
func (e *rtRemoveEnv) pending() []PendingDelete {
	e.geo.mu.Lock()
	defer e.geo.mu.Unlock()
	return append([]PendingDelete(nil), e.geo.st.PendingDeletes...)
}

// notice сообщает, показывает ли слой уведомление с этим ID.
func (e *rtRemoveEnv) notice(id string) bool {
	for _, n := range e.layer.Snapshot().Notices {
		if n.ID == id {
			return true
		}
	}
	return false
}

// exists — файл или симлинк есть в каталоге dat теста.
func (e *rtRemoveEnv) exists(name string) bool {
	_, err := os.Lstat(filepath.Join(e.datDir, name))
	return err == nil
}

// requireNoGeodataSection требует, чтобы секции geodata не было ни в применённом
// состоянии, ни в черновике.
func (e *rtRemoveEnv) requireNoGeodataSection(t *testing.T) {
	t.Helper()
	st := e.state(t)
	if _, ok := st.Applied[routing.GeodataSection]; ok {
		t.Errorf("секция geodata осталась в применённом состоянии: %s", st.Applied[routing.GeodataSection])
	}
	if _, ok := st.Draft[routing.GeodataSection]; ok {
		t.Errorf("секция geodata осталась в черновике: %s", st.Draft[routing.GeodataSection])
	}
}

// rtRemoveRequireNoRef требует, чтобы в файлах каталога dir не было подстроки sub.
func rtRemoveRequireNoRef(t *testing.T, dir, sub string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if en.IsDir() {
			continue
		}
		if strings.Contains(rtRemoveRead(t, filepath.Join(dir, en.Name())), sub) {
			t.Errorf("в %s осталась ссылка %q", en.Name(), sub)
		}
	}
}

// cond — условие «список» вида, которому принадлежит копия dat.
func (e *rtRemoveEnv) cond(file string) routing.Cond {
	typ := routing.CondGeosite
	if e.kind == lists.KindIP {
		typ = routing.CondGeoip
	}
	return routing.Cond{Type: typ, Lists: []lists.ListRef{{File: file, Tag: e.tag, Kind: e.kind}}}
}

// saveRule сохраняет правило на DIRECT со списком файла file.
func (e *rtRemoveEnv) saveRule(t *testing.T, file string) routing.Rule {
	t.Helper()
	_, r, err := e.rt.SaveRule(e.rev(t), routing.Rule{Enabled: true, Target: routing.Target{Kind: routing.KindDirect}, Conds: []routing.Cond{e.cond(file)}})
	if err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	return r
}

// TestRouterGeodataRemoveAfterApply — «Применить» с пометкой удаления: конвейер пишет
// конфиги без ссылок на файл, затем хук под замком geodata_remove убирает запись в
// xkeen.json, файл и парный симлинк; пометка снимается из применённого состояния и
// черновика. Сбой последнего шага (xkeen.json отвергнут проверкой) не откатывает
// конфиги: у источника «удаление не завершено», файл лежит, «Довести удаление» после
// исправления xkeen.json доводит дело до конца.
func TestRouterGeodataRemoveAfterApply(t *testing.T) {
	e := rtRemoveNewEnv(t)
	xkeenJSON := filepath.Join(e.xkeenDir, "xkeen.json")

	// Правило со списком файла: без пометки на него ссылаются конфиги.
	e.saveRule(t, rtRemoveFile)
	imp, err := e.rt.GeodataImpact(rtRemoveFile)
	if err != nil {
		t.Fatalf("GeodataImpact: %v", err)
	}
	if len(imp.Lists) != 1 || !strings.EqualFold(imp.Lists[0].Tag, e.tag) {
		t.Fatalf("до пометки на файл не ссылается правило: %+v", imp.Lists)
	}

	// Пометки: удалить свой источник (с записью в xkeen.json) и выключить стандартный
	// набор без записи (файл и симлинк пары).
	if _, err := e.rt.GeodataDrop(e.rev(t), rtRemoveFile, routing.DropDelete, []routing.GeodataAction{{Tag: e.tag, Action: routing.ActionRemove}}); err != nil {
		t.Fatalf("GeodataDrop %s: %v", rtRemoveFile, err)
	}
	if _, err := e.rt.GeodataDrop(e.rev(t), rtRemoveZkeen, routing.DropOff, nil); err != nil {
		t.Fatalf("GeodataDrop %s: %v", rtRemoveZkeen, err)
	}
	// Пометка пока ничего не удалила: файлы и запись на месте до «Применить».
	for _, n := range []string{rtRemoveFile, rtRemoveKeep, rtRemoveZkeen, rtRemoveAlias} {
		if !e.exists(n) {
			t.Fatalf("до «Применить» пропал файл %s", n)
		}
	}

	e.apply(t)
	e.waitStep(t, "файлы и запись убраны", func() bool {
		return !e.exists(rtRemoveFile) && !e.exists(rtRemoveZkeen) && !e.exists(rtRemoveAlias) && len(e.pending()) == 0
	})

	rtRemoveRequireNoRef(t, e.xrayDir, "ext:"+rtRemoveFile+":")
	disk := rtRemoveRead(t, xkeenJSON)
	if rtRemoveHasEntry(disk, rtRemoveFile) {
		t.Errorf("запись %s осталась в xkeen.json:\n%s", rtRemoveFile, disk)
	}
	if !strings.Contains(disk, "// комментарий владельца") || !strings.Contains(disk, "// про "+rtRemoveKeep) || !strings.Contains(disk, rtRemoveKeep) {
		t.Errorf("комментарии или чужая запись пропали из xkeen.json:\n%s", disk)
	}
	backups, _ := filepath.Glob(filepath.Join(routertest.Current(t).WorkDir, "rt-geodata-*", "backup", "xkeen", "xkeen.json.*"))
	if len(backups) == 0 {
		t.Error("резервной копии xkeen.json нет")
	}
	for _, n := range []string{rtRemoveFile2, rtRemoveKeep} {
		if !e.exists(n) {
			t.Errorf("чужой файл %s удалён", n)
		}
	}
	e.requireNoGeodataSection(t)

	// Сбой последнего шага: запись с нарушением, которое отвергает проверка xkeen.json.
	if _, err := e.rt.GeodataDrop(e.rev(t), rtRemoveFile2, routing.DropDelete, nil); err != nil {
		t.Fatalf("GeodataDrop %s: %v", rtRemoveFile2, err)
	}
	valid := rtRemoveXKeenJSON(rtRemoveFile2)
	rtRemoveWrite(t, xkeenJSON, strings.Replace(valid, "\"xkeen\": {", "\"xkeen\": {\"policy\": \"x\",", 1))
	e.apply(t)
	e.waitStep(t, "удаление перенесено в незавершённые", func() bool { return len(e.pending()) == 1 })

	p := e.pending()[0]
	if p.File != rtRemoveFile2 || p.Reason != lists.ErrXKeenJSONInvalid.Code || !p.Entry {
		t.Errorf("незавершённое удаление: %+v, ожидалось %s / %s / запись остаётся", p, rtRemoveFile2, lists.ErrXKeenJSONInvalid.Code)
	}
	if !e.exists(rtRemoveFile2) {
		t.Error("файл удалён при неудавшейся правке xkeen.json")
	}
	if !e.notice("geodata_delete:" + rtRemoveFile2) {
		t.Errorf("нет уведомления geodata_delete:%s: %+v", rtRemoveFile2, e.layer.Snapshot().Notices)
	}
	e.requireNoGeodataSection(t)

	// Исправили xkeen.json: «Довести удаление» без черновика, с предпросмотром правки.
	rtRemoveWrite(t, xkeenJSON, valid)
	pv, err := e.geo.PreviewFinishDelete(rtRemoveFile2)
	if err != nil {
		t.Fatalf("PreviewFinishDelete: %v", err)
	}
	if !rtRemoveHasEntry(pv.Before, rtRemoveFile2) || rtRemoveHasEntry(pv.After, rtRemoveFile2) || pv.Base == "" {
		t.Errorf("предпросмотр: before с записью=%v, after без записи=%v, base=%q", rtRemoveHasEntry(pv.Before, rtRemoveFile2), !rtRemoveHasEntry(pv.After, rtRemoveFile2), pv.Base)
	}
	if _, err := e.geo.PreviewFinishDelete(rtRemoveKeep); !errors.Is(err, ErrNotPending) {
		t.Errorf("PreviewFinishDelete чужого файла: %v, ожидалась not_pending", err)
	}
	if err := e.geo.FinishDelete(context.Background(), rtRemoveFile2, pv.Base, true); err != nil {
		t.Fatalf("FinishDelete: %v", err)
	}
	if e.exists(rtRemoveFile2) || rtRemoveHasEntry(rtRemoveRead(t, xkeenJSON), rtRemoveFile2) {
		t.Error("после «Довести удаление» остались файл или запись")
	}
	if len(e.pending()) != 0 || e.notice("geodata_delete:"+rtRemoveFile2) {
		t.Errorf("после «Довести удаление» осталось незавершённое удаление: %+v", e.pending())
	}
}

// TestRouterGeodataForeignRefs — чужие конфиги со ссылкой на файл блокируют пометку
// удаления (D-21): ссылка в файле Xray вне манифеста панели находится по подстроке
// ext:<имя>:, файл манифеста панели с той же подстрокой не считается, пара zkeen
// находится по второму имени, профили Mihomo вне манифеста — по имени файла. Пометка
// отклоняется ошибкой с местами ссылок, черновик не меняется; после удаления чужого
// файла пометка проходит, «Вернуть источник» её снимает.
func TestRouterGeodataForeignRefs(t *testing.T) {
	e := rtRemoveNewEnv(t)
	// Применение без ссылок на dat: файлы панели попадают в манифест.
	e.apply(t)

	managed := filepath.Join(e.xrayDir, "99_xcp-routing.json")
	if !e.layer.IsManagedPath(managed) {
		t.Fatalf("файл %s не в манифесте панели: проверка пропуска файлов панели невозможна", managed)
	}
	rtRemoveWrite(t, managed, "{\"x\": \"ext:"+rtRemoveFile+":"+e.tag+"\"}\n")

	foreign := filepath.Join(e.xrayDir, "05_routing.json")
	rtRemoveWrite(t, foreign, "{\n  \"routing\": {\n    \"rules\": [\n      {\n        \"type\": \"field\",\n        \"outboundTag\": \"direct\",\n"+
		"        \"domain\": [\"ext:"+rtRemoveFile+":"+e.tag+"\"]\n      }\n    ]\n  }\n}\n")

	refs, err := e.geo.ForeignRefs(rtRemoveFile)
	if err != nil {
		t.Fatalf("ForeignRefs: %v", err)
	}
	if len(refs) != 1 || refs[0].File != "05_routing.json" || refs[0].Line != 7 || refs[0].Editor != foreign {
		t.Fatalf("ссылки на %s: %+v, ожидалось одно место {05_routing.json, 7}", rtRemoveFile, refs)
	}
	imp, err := e.geo.Impact(rtRemoveFile)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(imp.Foreign) != 1 || imp.File != rtRemoveFile || imp.Lists == nil {
		t.Errorf("влияние: файл %q, чужих ссылок %d", imp.File, len(imp.Foreign))
	}

	// Пометка отклоняется, черновик не меняется.
	before := e.rev(t)
	_, err = e.geo.Drop(before, rtRemoveFile, routing.DropDelete, nil)
	var fe *ForeignRefError
	if !errors.As(err, &fe) || !errors.Is(err, ErrForeignRef) || lists.ErrorCode(err) != "foreign_ref" {
		t.Fatalf("Drop при чужой ссылке: %v, ожидалась foreign_ref", err)
	}
	if len(fe.Refs) != 1 || fe.Refs[0].Line != 7 {
		t.Errorf("места в ошибке: %+v", fe.Refs)
	}
	if got := e.rev(t); got != before {
		t.Errorf("ревизия черновика %d, ожидалась прежняя %d", got, before)
	}
	e.requireNoGeodataSection(t)

	// Пара zkeen: ссылка на второе имя блокирует удаление первого.
	rtRemoveWrite(t, filepath.Join(e.xrayDir, "06_other.json"), "{\"domain\": [\"ext:"+rtRemoveAlias+":X\"]}\n")
	refs, err = e.geo.ForeignRefs(rtRemoveZkeen)
	if err != nil {
		t.Fatalf("ForeignRefs пары: %v", err)
	}
	if len(refs) != 1 || refs[0].File != "06_other.json" || refs[0].Line != 1 {
		t.Errorf("ссылки на пару zkeen: %+v, ожидалось одно место {06_other.json, 1}", refs)
	}
	if _, err := e.geo.Drop(e.rev(t), rtRemoveZkeen, routing.DropOff, nil); !errors.Is(err, ErrForeignRef) {
		t.Errorf("Drop пары при ссылке на второе имя: %v", err)
	}
	// Имя файла внутри другого имени ссылкой не считается.
	rtRemoveWrite(t, filepath.Join(e.xrayDir, "07_name.json"), "{\"domain\": [\"ext:my"+rtRemoveFile+":X\"]}\n")
	if refs, _ = e.geo.ForeignRefs(rtRemoveFile); len(refs) != 1 {
		t.Errorf("ссылка на другое имя засчитана: %+v", refs)
	}

	// Профиль Mihomo вне манифеста: вхождение имени файла.
	rtRemoveWrite(t, filepath.Join(e.mihomoDir, "profiles", "mix.yaml"), "# список из "+rtRemoveKeep+"\nrules: []\n")
	refs, err = e.geo.ForeignRefs(rtRemoveKeep)
	if err != nil {
		t.Fatalf("ForeignRefs профиля: %v", err)
	}
	if len(refs) != 1 || refs[0].File != "profiles/mix.yaml" || refs[0].Line != 1 {
		t.Errorf("ссылки на %s в профилях Mihomo: %+v", rtRemoveKeep, refs)
	}

	// Чужой файл удалён — пометка проходит; «Вернуть источник» снимает её.
	for _, n := range []string{"05_routing.json", "07_name.json"} {
		if err := os.Remove(filepath.Join(e.xrayDir, n)); err != nil {
			t.Fatal(err)
		}
	}
	if refs, _ = e.geo.ForeignRefs(rtRemoveFile); len(refs) != 0 {
		t.Fatalf("после удаления чужого файла остались ссылки: %+v", refs)
	}
	if _, err := e.geo.Drop(e.rev(t), rtRemoveFile, routing.DropDelete, nil); err != nil {
		t.Fatalf("Drop после удаления чужого файла: %v", err)
	}
	g, err := routing.ParseGeodataDraft(e.state(t).Draft[routing.GeodataSection])
	if err != nil || len(g.Drop) != 1 || g.Drop[0] != (routing.GeodataDrop{File: rtRemoveFile, Mode: routing.DropDelete}) {
		t.Errorf("секция geodata после пометки: %+v (%v)", g, err)
	}
	if _, err := e.geo.Restore(e.rev(t), rtRemoveFile); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	e.requireNoGeodataSection(t)
}

// rtRemoveWantExists проверяет, что путь на месте (Lstat: симлинк не разворачивается).
func rtRemoveWantExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if want && err != nil {
		t.Errorf("%s должен остаться на месте: %v", path, err)
	}
	if !want && err == nil {
		t.Errorf("%s должен быть удалён", path)
	}
}

// TestRouterGeodataRemoveRoots — удаление dat-файла проверяет каталог по корням путей
// панели (WR-07, T-146.1-52) на файловой системе роутера: каталог внутри корня — файл
// и симлинк удаляются (цель симлинка остаётся), каталог вне корней или симлинк каталога
// наружу, недопустимое имя и каталог вместо файла — отказ без удаления.
func TestRouterGeodataRemoveRoots(t *testing.T) {
	tg := routertest.Current(t)
	base := rtGeoTempDir(t, tg.WorkDir)
	root := filepath.Join(base, "root")
	datDir := filepath.Join(root, "dat")
	outside := filepath.Join(base, "outside")
	other := filepath.Join(base, "other")
	for _, d := range []string{datDir, outside, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const wantMsg = "удаление вне допустимых корней"

	// (1) Корень содержит каталог dat: обычный файл удаляется; симлинк удаляется сам,
	// цель за пределами каталога остаётся.
	rtRemoveWrite(t, filepath.Join(datDir, "a.dat"), "a")
	target := filepath.Join(outside, "real.dat")
	rtRemoveWrite(t, target, "real")
	if err := os.Symlink(target, filepath.Join(datDir, "link.dat")); err != nil {
		t.Fatalf("симлинк link.dat: %v", err)
	}
	if err := removeDatFile(datDir, "a.dat", []string{root}); err != nil {
		t.Errorf("удаление a.dat внутри корня: %v", err)
	}
	if err := removeDatFile(datDir, "link.dat", []string{root}); err != nil {
		t.Errorf("удаление симлинка link.dat: %v", err)
	}
	rtRemoveWantExists(t, filepath.Join(datDir, "a.dat"), false)
	rtRemoveWantExists(t, filepath.Join(datDir, "link.dat"), false)
	rtRemoveWantExists(t, target, true)
	if err := removeDatFile(datDir, "a.dat", []string{root}); err != nil {
		t.Errorf("повторное удаление несуществующего файла не ошибка: %v", err)
	}

	// (2) Корень — другой каталог: отказ, файл на месте.
	rtRemoveWrite(t, filepath.Join(datDir, "b.dat"), "b")
	if err := removeDatFile(datDir, "b.dat", []string{other}); err == nil || !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("каталог вне корней: %v, ожидалось %q", err, wantMsg)
	}
	rtRemoveWantExists(t, filepath.Join(datDir, "b.dat"), true)

	// (3) Каталог dat — симлинк на каталог вне корней: отказ, файл цели на месте.
	rtRemoveWrite(t, filepath.Join(outside, "c.dat"), "c")
	linkDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(outside, linkDir); err != nil {
		t.Fatalf("симлинк каталога: %v", err)
	}
	if err := removeDatFile(linkDir, "c.dat", []string{root}); err == nil || !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("симлинк каталога наружу: %v, ожидалось %q", err, wantMsg)
	}
	rtRemoveWantExists(t, filepath.Join(outside, "c.dat"), true)

	// (4) Имя с выходом из каталога: отказ, файл рядом с каталогом на месте.
	rtRemoveWrite(t, filepath.Join(root, "x.dat"), "x")
	if err := removeDatFile(datDir, "../x.dat", []string{root}); err == nil {
		t.Errorf("имя ../x.dat принято")
	}
	rtRemoveWantExists(t, filepath.Join(root, "x.dat"), true)

	// (5) Каталог с именем dat-файла: отказ, каталог на месте.
	if err := os.Mkdir(filepath.Join(datDir, "d.dat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeDatFile(datDir, "d.dat", []string{root}); err == nil {
		t.Errorf("каталог d.dat удалён как файл")
	}
	rtRemoveWantExists(t, filepath.Join(datDir, "d.dat"), true)
}
