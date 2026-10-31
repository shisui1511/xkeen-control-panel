//go:build router

package configlayer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Взятие и возврат секций Xray на устройстве (SEC-01, XCFG-03): настоящий слой,
// хранилище и `xray run -test` роутера, временный каталог в рабочем каталоге
// теста. Работающие ядра стенда не трогаются (Applier не задан), тест ядро не
// читает и не переключает — метка ядра не нужна.

const (
	rtCoDNSFile      = "02_dns.json"
	rtCoMixedFile    = "00_mixed.json"
	rtCoOutboundFile = "04_outbounds.json"

	// Файл секции dns с комментарием JSONC: при переносе комментарий пропадает,
	// поэтому нужно согласие владельца (D-02).
	rtCoDNSBody = "// комментарий\n" + `{"dns": {"servers": ["127.0.0.1"], "queryStrategy": "UseIPv4"}}` + "\n"
	// Смешанный файл: log и stats — разные секции, файл остаётся на месте.
	rtCoMixedBody    = `{"log": {"loglevel": "warning"}, "stats": {}}` + "\n"
	rtCoOutboundBody = `{"outbounds": [{"tag": "direct", "protocol": "freedom"}]}` + "\n"
)

// rtCoresGen выдаёт для каждой managed секции Xray с непустым значением файл
// панели: {"<ключ>": value}, для «Прочих ключей» — объект ключей как есть.
type rtCoresGen struct{}

func (g *rtCoresGen) ID() string { return "rt-cores" }

func (g *rtCoresGen) Generate(in Input, installed InstalledKernels) ([]GeneratedFile, error) {
	if !installed.Xray {
		return nil, nil
	}
	desired := DesiredCores(in.Sections)
	var out []GeneratedFile
	for _, id := range XraySectionIDs {
		cs := desired[CoreSectionName(KernelXray, id)]
		if !cs.Managed || len(cs.Value) == 0 {
			continue
		}
		body := []byte(cs.Value)
		if id != xraySectionOther {
			var err error
			if body, err = json.Marshal(map[string]json.RawMessage{id: cs.Value}); err != nil {
				return nil, err
			}
		}
		out = append(out, GeneratedFile{
			Kernel: KernelXray, RelPath: PanelXraySectionFile(id), Kind: KindXrayJSON,
			Content: append(body, '\n'),
		})
	}
	return out, nil
}

// rtCoresEnv — слой с чужими файлами в каталоге Xray.
type rtCoresEnv struct {
	layer  *Layer
	xray   string
	rev    int64
	hashes map[string]string
}

// rtNewCoresEnv раскладывает files в каталог Xray и поднимает слой с одним
// ядром (Xray роутера).
func rtNewCoresEnv(t *testing.T, files map[string]string) *rtCoresEnv {
	t.Helper()
	if _, err := os.Stat(rtRefreshXrayBin); err != nil {
		t.Skipf("ядро не установлено на устройстве: %v", err)
	}
	dir := rtLiveDir(t)
	roots := Roots{Xray: filepath.Join(dir, "xray"), Mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{roots.Xray, roots.Mihomo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e := &rtCoresEnv{xray: roots.Xray, hashes: map[string]string{}}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(roots.Xray, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		e.hashes[name] = HashContent([]byte(body))
	}
	layer, err := New(Options{
		DataDir:       dir,
		Roots:         roots,
		Enabled:       func() bool { return true },
		Binaries:      func() Binaries { return Binaries{Xray: rtRefreshXrayBin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(layer.Stop)
	if err := layer.RegisterGenerator(&rtCoresGen{}); err != nil {
		t.Fatal(err)
	}
	layer.Start()
	e.layer = layer
	return e
}

func (e *rtCoresEnv) path(name string) string { return filepath.Join(e.xray, name) }

// editSections меняет секции черновика одной правкой (nil — удаляет).
func (e *rtCoresEnv) editSections(t *testing.T, sections map[string]json.RawMessage) {
	t.Helper()
	ev, err := e.layer.EditDraftBatch(e.rev, sections, nil)
	if err != nil {
		t.Fatalf("EditDraftBatch: %v", err)
	}
	e.rev = ev.DraftRevision
}

// takeDraft — секция черновика «Управлять в панели» для секции id: значение и
// источники — по скану настоящего каталога, как их присылает интерфейс.
func (e *rtCoresEnv) takeDraft(t *testing.T, id string, ack bool) json.RawMessage {
	t.Helper()
	scans, _, err := e.layer.ScanXraySections()
	if err != nil {
		t.Fatalf("ScanXraySections: %v", err)
	}
	sc := scans[id]
	return rtCoreSection(t, CoreSection{Managed: true, Value: sc.Effective, From: sc.Sources, CommentsAck: ack})
}

// rtCoreSection кодирует секцию черновика.
func rtCoreSection(t *testing.T, cs CoreSection) json.RawMessage {
	t.Helper()
	raw, err := EncodeCoreSection(cs)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rtCoresApply запускает «Применить» и ждёт итог.
func rtCoresApply(t *testing.T, layer *Layer) *ResultView {
	t.Helper()
	events, cancel, err := layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("подписка на события закрыта до итога применения")
			}
			if ev.Type == EventApplyDone {
				view, ok := ev.Data.(ApplyView)
				if !ok || view.Result == nil {
					t.Fatalf("apply_done без итога: %+v", ev.Data)
				}
				return view.Result
			}
		case <-deadline:
			t.Fatal("применение не завершилось")
		}
	}
}

func (e *rtCoresEnv) apply(t *testing.T) *ResultView { return rtCoresApply(t, e.layer) }

// assertFileIs проверяет, что файл Xray лежит на месте и совпадает побайтно с
// исходным.
func (e *rtCoresEnv) assertFileIs(t *testing.T, name string) {
	t.Helper()
	if got := hashOf(e.path(name)); got != e.hashes[name] {
		t.Errorf("%s не на месте или изменён: %q, ожидался %q", name, got, e.hashes[name])
	}
}

// assertAbsent проверяет, что файла нет в каталоге Xray.
func (e *rtCoresEnv) assertAbsent(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(e.path(name)); !os.IsNotExist(err) {
		t.Errorf("%s остался в каталоге Xray: %v", name, err)
	}
}

func TestRouterCoresTakeoverApplyAndRelease(t *testing.T) {
	e := rtNewCoresEnv(t, map[string]string{
		rtCoDNSFile:      rtCoDNSBody,
		rtCoMixedFile:    rtCoMixedBody,
		rtCoOutboundFile: rtCoOutboundBody,
	})

	// Взятие: желаемое managed ∧ нет записи в State.Cores — переход состояния
	// внутри обычного «Применить».
	e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true)})
	res := e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("взятие: %+v", res)
	}

	// Исходный файл ушёл в набор копий побайтно, файл панели записан, смешанный
	// файл не тронут.
	e.assertAbsent(t, rtCoDNSFile)
	e.assertFileIs(t, rtCoMixedFile)
	e.assertFileIs(t, rtCoOutboundFile)
	if _, err := os.Stat(e.path(PanelXrayDNSFile)); err != nil {
		t.Fatalf("файл панели секции не записан: %v", err)
	}
	st := e.layer.store.Snapshot()
	if st.Journal != nil {
		t.Errorf("журнал не снят после применения: %+v", st.Journal)
	}
	sec := st.Cores.Xray[xraySectionDNS]
	if sec == nil || len(sec.Moved) != 1 || sec.Moved[0].Name != rtCoDNSFile {
		t.Fatalf("State.Cores.Xray[dns]: %+v", sec)
	}
	if sec.File != PanelXrayDNSFile || len(sec.Left) != 0 {
		t.Errorf("State.Cores.Xray[dns]: файл панели %q, оставленные %v", sec.File, sec.Left)
	}
	if sec.Moved[0].Hash != e.hashes[rtCoDNSFile] {
		t.Errorf("хэш перенесённого файла %s, ожидался %s", sec.Moved[0].Hash, e.hashes[rtCoDNSFile])
	}
	if got := hashOf(movedCopyPath(sec.Moved[0])); got != e.hashes[rtCoDNSFile] {
		t.Errorf("копия перенесённого файла в наборе не совпадает: %q", got)
	}
	if len(st.Cores.Xray) != 1 {
		t.Errorf("взята не только секция dns: %+v", st.Cores.Xray)
	}

	// Возврат: желаемое managed=false проходит тем же «Применить».
	e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: rtCoreSection(t, CoreSection{Managed: false})})
	res = e.apply(t)
	if !res.OK || res.Code != ResultApplied {
		t.Fatalf("возврат: %+v", res)
	}
	e.assertFileIs(t, rtCoDNSFile)
	e.assertFileIs(t, rtCoMixedFile)
	e.assertAbsent(t, PanelXrayDNSFile)
	st = e.layer.store.Snapshot()
	if !st.Cores.isEmpty() {
		t.Errorf("State.Cores не пуст после возврата: %+v", st.Cores)
	}
	if st.Journal != nil {
		t.Errorf("журнал не снят после возврата: %+v", st.Journal)
	}
}

// rtHasIssue — в итоге есть проблема с кодом code по секции key и файлом file.
func rtHasIssue(res *ResultView, code, key, file string) bool {
	for _, is := range res.Issues {
		if is.Code == code && is.Key == key && is.Params["file"] == file {
			return true
		}
	}
	return false
}

// assertKept — после отказанного применения каталог Xray побайтно как до него,
// файлов панели нет, State.Cores пуст, журнал снят.
func (e *rtCoresEnv) assertKept(t *testing.T, before map[string]string) {
	t.Helper()
	after := rtDirSnapshot(t, e.xray)
	if len(after) != len(before) {
		t.Errorf("состав каталога Xray изменился: было %d, стало %d", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s изменён при отказе", k)
		}
	}
	st := e.layer.store.Snapshot()
	if !st.Cores.isEmpty() {
		t.Errorf("State.Cores изменён при отказе: %+v", st.Cores)
	}
	if st.Journal != nil {
		t.Errorf("журнал остался после отказа: %+v", st.Journal)
	}
}

func TestRouterCoresTakeoverGuards(t *testing.T) {
	const (
		mixedInbounds = "05_mixed.json"
		shadowFile    = "99_zz.json"
	)

	t.Run("comments", func(t *testing.T) {
		e := rtNewCoresEnv(t, map[string]string{rtCoDNSFile: rtCoDNSBody, rtCoOutboundFile: rtCoOutboundBody})
		// Согласия на потерю комментариев нет: сервер не доверяет клиенту (D-02).
		e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: e.takeDraft(t, xraySectionDNS, false)})
		before := rtDirSnapshot(t, e.xray)
		res := e.apply(t)
		if res.OK || res.Code != ResultBuildFailed || !rtHasIssue(res, IssueCommentsNotAck, SectionXrayDNS, rtCoDNSFile) {
			t.Fatalf("перенос файла с комментариями без согласия: %+v", res)
		}
		e.assertKept(t, before)
	})

	t.Run("from", func(t *testing.T) {
		e := rtNewCoresEnv(t, map[string]string{rtCoDNSFile: rtCoDNSBody, rtCoOutboundFile: rtCoOutboundBody})
		e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true)})
		// Файл изменился между нажатием «Управлять в панели» и записью.
		if err := os.WriteFile(e.path(rtCoDNSFile), []byte(rtCoDNSBody+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := rtDirSnapshot(t, e.xray)
		res := e.apply(t)
		if res.OK || res.Code != ResultBuildFailed || !rtHasIssue(res, IssueSectionTakeoverFailed, SectionXrayDNS, rtCoDNSFile) {
			t.Fatalf("изменённый файл секции: %+v", res)
		}
		e.assertKept(t, before)

		// Новый файл с ключом секции появился после проверки: from не совпадает.
		if err := os.WriteFile(e.path(rtCoDNSFile), []byte(rtCoDNSBody), 0o644); err != nil {
			t.Fatal(err)
		}
		e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true)})
		if err := os.WriteFile(e.path(shadowFile), []byte(`{"dns": {"servers": ["8.8.8.8"]}}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before = rtDirSnapshot(t, e.xray)
		res = e.apply(t)
		if res.OK || res.Code != ResultBuildFailed || !rtHasIssue(res, IssueSectionTakeoverFailed, SectionXrayDNS, shadowFile) {
			t.Fatalf("новый файл секции: %+v", res)
		}
		e.assertKept(t, before)
	})

	t.Run("inbounds_mixed", func(t *testing.T) {
		e := rtNewCoresEnv(t, map[string]string{
			mixedInbounds: `{"inbounds": [{"tag": "redirect", "port": 61219, "protocol": "dokodemo-door", ` +
				`"settings": {"network": "tcp", "followRedirect": true}}], "stats": {}}` + "\n",
			rtCoOutboundFile: rtCoOutboundBody,
		})
		e.editSections(t, map[string]json.RawMessage{SectionXrayInbounds: e.takeDraft(t, xraySectionInbounds, true)})
		before := rtDirSnapshot(t, e.xray)
		res := e.apply(t)
		if res.OK || res.Code != ResultBuildFailed || !rtHasIssue(res, IssueInboundsMixed, SectionXrayInbounds, mixedInbounds) {
			t.Fatalf("входящие в смешанном файле: %+v", res)
		}
		// Чужой файл не тронут.
		e.assertFileIs(t, mixedInbounds)
		e.assertKept(t, before)
	})

	t.Run("shadowed", func(t *testing.T) {
		e := rtNewCoresEnv(t, map[string]string{
			rtCoDNSFile:      rtCoDNSBody,
			shadowFile:       `{"dns": {"servers": ["8.8.8.8"]}}` + "\n",
			rtCoOutboundFile: rtCoOutboundBody,
		})
		e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: e.takeDraft(t, xraySectionDNS, true)})
		before := rtDirSnapshot(t, e.xray)
		res := e.apply(t)
		if res.OK || res.Code != ResultBuildFailed || !rtHasIssue(res, IssueSectionShadowed, SectionXrayDNS, shadowFile) {
			t.Fatalf("перекрывающий файл секции: %+v", res)
		}
		e.assertKept(t, before)
	})

	t.Run("validation", func(t *testing.T) {
		e := rtNewCoresEnv(t, map[string]string{rtCoDNSFile: rtCoDNSBody, rtCoOutboundFile: rtCoOutboundBody})
		scans, _, err := e.layer.ScanXraySections()
		if err != nil {
			t.Fatal(err)
		}
		// Тестовый генератор пишет в файл панели dns с неверным типом поля: ядро
		// отвергает итоговую раскладку, чужой файл остаётся на месте.
		bad := rtCoreSection(t, CoreSection{
			Managed: true, Value: json.RawMessage(`{"servers": 5}`),
			From: scans[xraySectionDNS].Sources, CommentsAck: true,
		})
		e.editSections(t, map[string]json.RawMessage{SectionXrayDNS: bad})
		before := rtDirSnapshot(t, e.xray)
		res := e.apply(t)
		if res.OK || res.Code != CodeValidationFailed || res.Kernel != KernelXray {
			t.Fatalf("итог при невалидном dns: %+v", res)
		}
		e.assertFileIs(t, rtCoDNSFile)
		e.assertKept(t, before)
	})
}
