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
}
