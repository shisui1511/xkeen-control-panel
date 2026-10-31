//go:build router

package cores

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Сервис секций ядер на настоящем слое и ядрах устройства: вид с владельцем,
// предпросмотр, взятие с согласием на потерю комментариев, правка формой и
// «Дополнительно», пресет, отпускание, разбор сырого блока. Слой работает во
// временном каталоге рабочего каталога теста, работающие ядра стенда не трогаются
// (Applier не задан), активное ядро тест не читает и не переключает — метка ядра не
// нужна.

const (
	rtCoresSvcDNSFile = "02_dns.json"
	// Секция dns с комментарием JSONC и полем dns.tag, которого форма не знает.
	rtCoresSvcDNSBody      = "// комментарий\n" + `{"dns": {"tag": "dns-in", "servers": ["127.0.0.1"], "queryStrategy": "UseIPv4"}}` + "\n"
	rtCoresSvcOutboundBody = `{"outbounds": [{"tag": "direct", "protocol": "freedom"}]}` + "\n"
	rtCoresSvcRoutingBody  = `{"routing": {"rules": []}}` + "\n"
)

// rtCoresSvcEnv — слой с чужими файлами Xray и профилем Mihomo во временном
// каталоге и сервис секций поверх него.
type rtCoresSvcEnv struct {
	layer  *configlayer.Layer
	svc    *Service
	xray   string
	mihomo string
}

// rtCoresSvcNew поднимает слой с зарегистрированными генератором и проверкой
// сервиса.
func rtCoresSvcNew(t *testing.T) *rtCoresSvcEnv {
	t.Helper()
	bin := rtXraySecBin(t)
	tg := routertest.Current(t)
	dir, err := os.MkdirTemp(tg.WorkDir, "cores-svc-")
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	e := &rtCoresSvcEnv{xray: filepath.Join(dir, "xray"), mihomo: filepath.Join(dir, "mihomo")}
	for _, d := range []string{e.xray, filepath.Join(e.mihomo, "profiles")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		rtCoresSvcDNSFile:   rtCoresSvcDNSBody,
		"04_outbounds.json": rtCoresSvcOutboundBody,
		"05_routing.json":   rtCoresSvcRoutingBody,
	} {
		if err := os.WriteFile(filepath.Join(e.xray, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	layer, err := configlayer.New(configlayer.Options{
		DataDir:       dir,
		Roots:         configlayer.Roots{Xray: e.xray, Mihomo: e.mihomo},
		Enabled:       func() bool { return true },
		Binaries:      func() configlayer.Binaries { return configlayer.Binaries{Xray: bin} },
		DriftInterval: time.Hour,
		DebounceDelay: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("configlayer.New: %v", err)
	}
	t.Cleanup(layer.Stop)
	svc, err := New(Deps{
		Layer:     layer,
		Installed: func() configlayer.InstalledKernels { return configlayer.InstalledKernels{Xray: true, Mihomo: true} },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(svc.Stop)
	if err := layer.Register(configlayer.Hooks{Generators: svc.Generators(), Checker: svc.Checker()}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	layer.Start()
	svc.Start()
	e.layer, e.svc = layer, svc
	return e
}

// rev — текущая ревизия черновика.
func (e *rtCoresSvcEnv) rev(t *testing.T) int64 {
	t.Helper()
	st, err := e.layer.ReadState()
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	return st.DraftRevision
}

// view возвращает вид ядра.
func (e *rtCoresSvcEnv) view(t *testing.T, kernel string) KernelView {
	t.Helper()
	v, err := e.svc.View(kernel)
	if err != nil {
		t.Fatalf("View(%s): %v", kernel, err)
	}
	return v
}

// section возвращает секцию из вида ядра.
func (e *rtCoresSvcEnv) section(t *testing.T, kernel, id string) SectionView {
	t.Helper()
	for _, s := range e.view(t, kernel).Sections {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("в виде %s нет секции %s", kernel, id)
	return SectionView{}
}

// apply запускает «Применить» и ждёт итог.
func (e *rtCoresSvcEnv) apply(t *testing.T) *configlayer.ResultView {
	t.Helper()
	events, cancel, err := e.layer.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := e.layer.StartApply(true); err != nil {
		t.Fatalf("StartApply: %v", err)
	}
	deadline := time.After(120 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("подписка на события закрыта до итога применения")
			}
			if ev.Type == configlayer.EventApplyDone {
				view, ok := ev.Data.(configlayer.ApplyView)
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

// mustApply применяет черновик и требует успеха.
func (e *rtCoresSvcEnv) mustApply(t *testing.T, what string) {
	t.Helper()
	if res := e.apply(t); !res.OK || res.Code != configlayer.ResultApplied {
		t.Fatalf("%s: применение: %+v", what, res)
	}
}

// readFile читает файл каталога Xray.
func (e *rtCoresSvcEnv) readFile(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(e.xray, name))
	return string(b), err
}

// rtCoresSvcCode достаёт код ошибки сервиса; другая ошибка или её отсутствие —
// провал.
func rtCoresSvcCode(t *testing.T, err error) *Error {
	t.Helper()
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("ожидалась ошибка сервиса, получено: %v", err)
	}
	return ce
}

// rtCoresSvcObject разбирает значение секции в карту.
func rtCoresSvcObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("значение секции не объект: %v (%s)", err, raw)
	}
	return m
}

// rtCoresSvcStrings приводит значение JSON-массива строк к срезу.
func rtCoresSvcStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, it := range list {
		s, _ := it.(string)
		out = append(out, s)
	}
	return out
}

func TestRouterCoresServiceFlow(t *testing.T) {
	e := rtCoresSvcNew(t)

	t.Run("взятие dns с комментарием", func(t *testing.T) {
		// Вид: секция dns за XKeen, действующее значение читается из чужого файла.
		v := e.view(t, "xray")
		if !v.Installed || !v.TakeAll {
			t.Errorf("вид Xray: installed=%v take_all=%v", v.Installed, v.TakeAll)
		}
		dns := e.section(t, "xray", "dns")
		if dns.Owner != "xkeen" || !slices.Equal(dns.Files, []string{rtCoresSvcDNSFile}) {
			t.Fatalf("dns: владелец %q, файлы %v", dns.Owner, dns.Files)
		}
		if !strings.Contains(string(dns.Value), `"dns-in"`) {
			t.Errorf("значение dns не содержит dns.tag: %s", dns.Value)
		}
		if !slices.Equal(dns.Comments, []string{rtCoresSvcDNSFile}) {
			t.Errorf("комментарии dns: %v", dns.Comments)
		}
		if dns.Dirty || len(dns.Pinned) != 0 || dns.PanelFile != "" {
			t.Errorf("dns до взятия: dirty=%v pinned=%v panel_file=%q", dns.Dirty, dns.Pinned, dns.PanelFile)
		}

		// Предпросмотр: ровно запрошенная секция (D-03).
		pv, err := e.svc.Preview("xray", []string{"dns"})
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		if len(pv.Sections) != 1 || pv.Sections[0].ID != "dns" {
			t.Fatalf("предпросмотр вышел за запрошенную секцию: %+v", pv.Sections)
		}
		sp := pv.Sections[0]
		if !slices.Equal(sp.Move, []string{rtCoresSvcDNSFile}) || !slices.Equal(sp.Comments, []string{rtCoresSvcDNSFile}) || sp.Preset || len(sp.Left) != 0 {
			t.Errorf("предпросмотр dns: %+v", sp)
		}
		if _, err := e.svc.Preview("mihomo", []string{"dns"}); err == nil || rtCoresSvcCode(t, err).Code != "mihomo_with_profile" {
			t.Errorf("Preview mihomo: %v", err)
		}

		// Без согласия комментарии не теряются.
		_, err = e.svc.Take(e.rev(t), "xray", []string{"dns"}, false)
		if ce := rtCoresSvcCode(t, err); ce.Code != "comments_not_acknowledged" || ce.Params["file"] != rtCoresSvcDNSFile {
			t.Fatalf("Take без ack: %+v", ce)
		}
		if _, err := e.svc.Take(e.rev(t), "xray", []string{"dns", "nope"}, true); err == nil || rtCoresSvcCode(t, err).Code != "section_unknown" {
			t.Fatalf("Take с неизвестной секцией: %v", err)
		}
		if _, err := e.svc.Take(e.rev(t)-1, "xray", []string{"dns"}, true); !errors.Is(err, configlayer.ErrDraftConflict) {
			t.Fatalf("Take на устаревшей ревизии: %v", err)
		}

		// С согласием: значение в черновике, диск не тронут до «Применить».
		if _, err := e.svc.Take(e.rev(t), "xray", []string{"dns"}, true); err != nil {
			t.Fatalf("Take: %v", err)
		}
		if body, err := e.readFile(rtCoresSvcDNSFile); err != nil || body != rtCoresSvcDNSBody {
			t.Fatalf("Take изменил чужой файл: %v", err)
		}
		if _, err := os.Stat(filepath.Join(e.xray, configlayer.PanelXrayDNSFile)); !os.IsNotExist(err) {
			t.Fatalf("Take записал файл панели до «Применить»: %v", err)
		}
		dns = e.section(t, "xray", "dns")
		if dns.Owner != "pending" || !dns.Dirty || dns.PanelFile != configlayer.PanelXrayDNSFile {
			t.Errorf("dns после Take: владелец %q dirty=%v panel_file=%q", dns.Owner, dns.Dirty, dns.PanelFile)
		}
		for _, id := range []string{"log", "inbounds", "policy", "other"} {
			if sec := e.section(t, "xray", id); sec.Owner == "pending" || sec.Owner == "panel" {
				t.Errorf("взятие dns захватило секцию %s", id)
			}
		}

		e.mustApply(t, "взятие dns")
		dns = e.section(t, "xray", "dns")
		if dns.Owner != "panel" || !slices.Equal(dns.Files, []string{configlayer.PanelXrayDNSFile}) || dns.Dirty {
			t.Fatalf("dns после «Применить»: владелец %q файлы %v dirty=%v", dns.Owner, dns.Files, dns.Dirty)
		}
		if _, err := e.readFile(rtCoresSvcDNSFile); !os.IsNotExist(err) {
			t.Errorf("чужой файл остался в каталоге Xray: %v", err)
		}
		// XCFG-01: поле dns.tag, которого форма не знает, дошло до файла панели.
		body, err := e.readFile(configlayer.PanelXrayDNSFile)
		if err != nil {
			t.Fatalf("файл панели: %v", err)
		}
		var file struct {
			DNS map[string]any `json:"dns"`
		}
		if err := json.Unmarshal([]byte(body), &file); err != nil {
			t.Fatalf("файл панели не JSON: %v", err)
		}
		if file.DNS["tag"] != "dns-in" || file.DNS["queryStrategy"] != "UseIPv4" || !slices.Equal(rtCoresSvcStrings(file.DNS["servers"]), []string{"127.0.0.1"}) {
			t.Errorf("файл панели потерял поля dns: %v", file.DNS)
		}
	})

	t.Run("правка формой и Дополнительно", func(t *testing.T) {
		dns := e.section(t, "xray", "dns")
		if !strings.Contains(dns.Advanced, `"tag"`) || strings.Contains(dns.Advanced, "servers") {
			t.Fatalf("Дополнительно dns: %q", dns.Advanced)
		}
		form := json.RawMessage(`{"servers":["127.0.0.3"],"queryStrategy":"UseIPv4"}`)
		if _, err := e.svc.Save(e.rev(t), "xray", "dns", form, dns.Advanced); err != nil {
			t.Fatalf("Save dns: %v", err)
		}
		dns = e.section(t, "xray", "dns")
		obj := rtCoresSvcObject(t, dns.Value)
		if obj["tag"] != "dns-in" || obj["queryStrategy"] != "UseIPv4" || !slices.Equal(rtCoresSvcStrings(obj["servers"]), []string{"127.0.0.3"}) || !dns.Dirty {
			t.Errorf("значение dns после Save: %v dirty=%v", obj, dns.Dirty)
		}

		// Очистка поля формы снимает ключ, остаток остаётся.
		if _, err := e.svc.Save(e.rev(t), "xray", "dns", json.RawMessage(`{"servers":["127.0.0.3"]}`), dns.Advanced); err != nil {
			t.Fatalf("Save dns без queryStrategy: %v", err)
		}
		obj = rtCoresSvcObject(t, e.section(t, "xray", "dns").Value)
		if _, has := obj["queryStrategy"]; has || obj["tag"] != "dns-in" {
			t.Errorf("очистка поля формы: %v", obj)
		}

		// Ключ «Дополнительно», совпадающий с полем формы, и синтаксис.
		_, err := e.svc.Save(e.rev(t), "xray", "dns", form, `{"servers":["127.0.0.9"]}`)
		if ce := rtCoresSvcCode(t, err); ce.Code != "raw_dup_key" || ce.Params["key"] != "servers" {
			t.Errorf("повтор ключа формы: %+v", ce)
		}
		_, err = e.svc.Save(e.rev(t), "xray", "dns", form, "{\n \"a\": }")
		if ce := rtCoresSvcCode(t, err); ce.Code != "raw_syntax" || ce.Params["line"] != "2" {
			t.Errorf("синтаксис Дополнительно: %+v", ce)
		}

		// Взятие dns не захватило «Прочие ключи» (D-03): править их рано.
		_, err = e.svc.Save(e.rev(t), "xray", "other", nil, `{"stats":{}}`)
		if ce := rtCoresSvcCode(t, err); ce.Code != "section_not_managed" {
			t.Errorf("правка не взятой секции: %+v", ce)
		}
		if _, err := e.svc.Take(e.rev(t), "xray", []string{"other", "policy"}, false); err != nil {
			t.Fatalf("Take other, policy: %v", err)
		}
		if sec := e.section(t, "xray", "dns"); sec.Owner != "panel" {
			t.Errorf("взятие других секций изменило dns: %q", sec.Owner)
		}

		_, err = e.svc.Save(e.rev(t), "xray", "other", nil, `{"routing": {}}`)
		if ce := rtCoresSvcCode(t, err); ce.Code != "raw_reserved" || ce.Params["key"] != "routing" {
			t.Errorf("ключ панели в «Прочих ключах»: %+v", ce)
		}
		_, err = e.svc.Save(e.rev(t), "xray", "policy", nil, `{"system":{"statsOutboundUplink":false}}`)
		if ce := rtCoresSvcCode(t, err); ce.Code != "raw_pinned" {
			t.Errorf("закреплённый счётчик policy: %+v", ce)
		}
		if _, err := e.svc.Save(e.rev(t), "xray", "other", nil, `{"stats":{}}`); err != nil {
			t.Fatalf("Save other: %v", err)
		}
		if got := string(e.section(t, "xray", "other").Value); got != `{"stats":{}}` {
			t.Errorf("значение other: %s", got)
		}

		// Пресет заменяет значение секции в черновике.
		if _, err := e.svc.Preset(e.rev(t), "xray", "dns"); err != nil {
			t.Fatalf("Preset dns: %v", err)
		}
		if got := string(e.section(t, "xray", "dns").Value); got != `{"servers":["localhost"]}` {
			t.Errorf("значение dns после пресета: %s", got)
		}
	})

	t.Run("отпускание одной секции", func(t *testing.T) {
		if _, err := e.svc.Release(e.rev(t), "xray", "dns"); err != nil {
			t.Fatalf("Release dns: %v", err)
		}
		for _, id := range []string{"other", "policy"} {
			if sec := e.section(t, "xray", id); sec.Owner != "pending" {
				t.Errorf("отпускание dns изменило секцию %s: %q", id, sec.Owner)
			}
		}
		if _, err := e.svc.Release(e.rev(t), "mihomo", "main"); err == nil || rtCoresSvcCode(t, err).Code != "mihomo_with_profile" {
			t.Errorf("Release mihomo: %v", err)
		}

		// Файл возвращается побайтно, с комментарием, правки панели снимаются.
		e.mustApply(t, "отпускание dns, взятие other и policy")
		if body, err := e.readFile(rtCoresSvcDNSFile); err != nil || body != rtCoresSvcDNSBody {
			t.Errorf("02_dns.json не вернулся побайтно: %v", err)
		}
		if _, err := e.readFile(configlayer.PanelXrayDNSFile); !os.IsNotExist(err) {
			t.Errorf("файл панели dns остался: %v", err)
		}
		if sec := e.section(t, "xray", "dns"); sec.Owner != "xkeen" {
			t.Errorf("dns после отпускания: %q", sec.Owner)
		}
		for _, id := range []string{"other", "policy"} {
			if sec := e.section(t, "xray", id); sec.Owner != "panel" {
				t.Errorf("секция %s после «Применить»: %q", id, sec.Owner)
			}
		}
		if len(e.section(t, "xray", "policy").Pinned) != 4 {
			t.Errorf("закреплённые счётчики policy: %v", e.section(t, "xray", "policy").Pinned)
		}

		// Остальные секции отпускаются своим Release.
		for _, id := range []string{"other", "policy"} {
			if _, err := e.svc.Release(e.rev(t), "xray", id); err != nil {
				t.Fatalf("Release %s: %v", id, err)
			}
		}
		e.mustApply(t, "отпускание other и policy")
		for _, name := range []string{configlayer.PanelXrayOtherFile, configlayer.PanelXrayPolicyFile} {
			if _, err := e.readFile(name); !os.IsNotExist(err) {
				t.Errorf("файл панели %s остался: %v", name, err)
			}
		}
		if body, err := e.readFile(rtCoresSvcDNSFile); err != nil || body != rtCoresSvcDNSBody {
			t.Errorf("02_dns.json изменился: %v", err)
		}
	})

	t.Run("группа force-proxy", func(t *testing.T) {
		if _, err := e.layer.EditDraft(e.rev(t), "groups", json.RawMessage(`{"items":[{"id":"g1"}]}`)); err != nil {
			t.Fatalf("EditDraft groups: %v", err)
		}
		// Секция «Входящие» за XKeen — группа всё равно сохраняется (D-12).
		_, err := e.svc.SetForceProxyGroup(e.rev(t), "xray", "nope")
		if ce := rtCoresSvcCode(t, err); ce.Code != "group_not_found" {
			t.Errorf("неизвестная группа: %+v", ce)
		}
		if _, err := e.svc.SetForceProxyGroup(e.rev(t), "xray", "g1"); err != nil {
			t.Fatalf("SetForceProxyGroup: %v", err)
		}
		st, err := e.layer.ReadState()
		if err != nil {
			t.Fatal(err)
		}
		cs, err := configlayer.ParseCoreSection(st.Draft[configlayer.SectionXrayInbounds])
		if err != nil || cs.ForceProxyGroup != "g1" || cs.Managed {
			t.Errorf("секция «Входящие»: %+v, %v", cs, err)
		}
		if _, err := e.svc.SetForceProxyGroup(e.rev(t), "xray", ""); err != nil {
			t.Fatalf("сброс группы: %v", err)
		}
	})
}
