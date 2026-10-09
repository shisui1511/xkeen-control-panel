//go:build router

package nodes

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка матрицы возможностей на устройстве. Вердикт — свойства модели:
// Support обязан совпадать с тем, что умеют эмиттеры, и с порогами версий из
// Caps. Ядра здесь не запускаются: их вердикт для тех же узлов даёт
// TestRouterNodeLinks и TestRouterParse*. Эталонных файлов нет.

// rtCapLink разбирает ссылку из файла данных по имени схемы.
func rtCapLink(t *testing.T, scheme string) Node {
	t.Helper()
	n, err := ParseLink(rtFirstLink(t, filepath.Join(routertest.LinksDir(t), scheme+".txt")))
	if err != nil {
		t.Fatalf("ParseLink(%s): %v", scheme, err)
	}
	return n
}

// rtCapInput разбирает входной файл и возвращает его узлы.
func rtCapInput(t *testing.T, name string) []Node {
	t.Helper()
	res, err := ParseBody(rtInput(t, name))
	if err != nil {
		t.Fatalf("ParseBody(%s): %v", name, err)
	}
	return res.Nodes
}

// rtCapAll — все узлы данных проверки: ссылки и входные тела.
func rtCapAll(t *testing.T) []Node {
	t.Helper()
	var out []Node
	links, _ := filepath.Glob(filepath.Join(routertest.LinksDir(t), "*.txt"))
	sort.Strings(links)
	for _, f := range links {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("файл данных не читается: %v", err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			if n, err := ParseLink(line); err == nil {
				out = append(out, n)
			}
		}
	}
	inputs, _ := filepath.Glob(filepath.Join(routertest.InputsDir(t), "*"))
	sort.Strings(inputs)
	for _, f := range inputs {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("входной файл не читается: %v", err)
		}
		if res, err := ParseBody(raw); err == nil {
			out = append(out, res.Nodes...)
		}
	}
	if len(out) == 0 {
		t.Fatal("в данных проверки нет узлов")
	}
	return out
}

func rtCapExpect(t *testing.T, what string, v Verdict, ok bool, code string, params map[string]string) {
	t.Helper()
	if v.OK != ok || v.Code != code {
		t.Errorf("%s: вердикт ok=%v code=%q, ожидалось ok=%v code=%q", what, v.OK, v.Code, ok, code)
		return
	}
	for k, want := range params {
		if v.Params[k] != want {
			t.Errorf("%s: параметр %s=%q, ожидалось %q", what, k, v.Params[k], want)
		}
	}
}

// TestRouterCapabilityTracer: tuic и xhttp не пропадают молча — умеющее ядро
// получает узел, другое — причину с кодом и порогом версии.
func TestRouterCapabilityTracer(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()

	tuic := rtCapLink(t, "tuic")
	rtCapExpect(t, "tuic/xray", Support(tuic, "xray", latest), false, CodeTUICXray, nil)
	rtCapExpect(t, "tuic/mihomo", Support(tuic, "mihomo", latest), true, "", nil)
	if px, err := ClashProxy(tuic, tuic.Name); err != nil || px["type"] != "tuic" {
		t.Errorf("tuic в Clash: type %v, ошибка %v", px["type"], err)
	}

	xh := rtCapLink(t, "vless_xhttp")
	if xh.Transport.Network != "xhttp" {
		t.Fatalf("данные vless_xhttp: транспорт %q", xh.Transport.Network)
	}
	rtCapExpect(t, "xhttp/xray/latest", Support(xh, "xray", latest), true, "", nil)
	rtCapExpect(t, "xhttp/mihomo/latest", Support(xh, "mihomo", latest), true, "", nil)

	old := latest
	old.MihomoXHTTP = Gate{OK: false, Min: "1.19.22"}
	rtCapExpect(t, "xhttp/mihomo/old", Support(xh, "mihomo", old), false, CodeNeedsMihomo, map[string]string{"kernel_min": "1.19.22"})
	rtCapExpect(t, "xhttp/xray/old-mihomo", Support(xh, "xray", old), true, "", nil)

	old = latest
	old.XrayXHTTP = Gate{OK: false, Min: "24.11.21"}
	rtCapExpect(t, "xhttp/xray/old", Support(xh, "xray", old), false, CodeNeedsXray, map[string]string{"kernel_min": "24.11.21"})
	rtCapExpect(t, "xhttp/mihomo/old-xray", Support(xh, "mihomo", old), true, "", nil)

	// reuse-settings (xmux) требует Mihomo не ниже 1.19.23, sessionID* — Xray не ниже 26.6.22.
	plain := xh
	plain.Transport.XHTTP = nil
	withMux := xh
	withMux.Transport.XHTTP = map[string]any{"xmux": map[string]any{"maxConcurrency": "16-32"}, "sessionIDKey": "sid"}
	old = latest
	old.MihomoXHTTPReuse = Gate{OK: false, Min: "1.19.23"}
	rtCapExpect(t, "xhttp-reuse/mihomo", Support(withMux, "mihomo", old), false, CodeNeedsMihomo, map[string]string{"kernel_min": "1.19.23"})
	rtCapExpect(t, "xhttp-reuse/mihomo-no-mux", Support(plain, "mihomo", old), true, "", nil)
	old = latest
	old.XraySessionID = Gate{OK: false, Min: "26.6.22"}
	rtCapExpect(t, "xhttp-session/xray", Support(withMux, "xray", old), false, CodeNeedsXray, map[string]string{"kernel_min": "26.6.22"})
	rtCapExpect(t, "xhttp-session/xray-plain", Support(plain, "xray", old), true, "", nil)
}

// TestRouterCapabilityConsistent: вердикт ядра совпадает с эмиттером — если
// XrayOutbound или ClashProxy отказали, Support отдаёт тот же код и параметры;
// вердикт OK возможен только при успешной эмиссии.
func TestRouterCapabilityConsistent(t *testing.T) {
	routertest.Current(t)
	latest := CapsLatest()
	for i, n := range rtCapAll(t) {
		_, xerr := XrayOutbound(n, "xcp-probe")
		rtCapSame(t, i, n, "xray", Support(n, "xray", latest), xerr)
		_, merr := ClashProxy(n, "probe")
		rtCapSame(t, i, n, "mihomo", Support(n, "mihomo", latest), merr)
	}
}

func rtCapSame(t *testing.T, i int, n Node, kernel string, v Verdict, err error) {
	t.Helper()
	what := kernel + "/" + n.Protocol
	if err == nil {
		if !v.OK {
			t.Errorf("узел %d (%s): эмиттер принял, а вердикт не OK: %s", i, what, v.Code)
		}
		return
	}
	var se *SourceError
	if v.OK {
		t.Errorf("узел %d (%s): эмиттер отказал (%v), а вердикт OK", i, what, err)
		return
	}
	if !errors.As(err, &se) || v.Code != se.Code {
		t.Errorf("узел %d (%s): код вердикта %q не совпал с ошибкой эмиттера %v", i, what, v.Code, err)
		return
	}
	for k, want := range se.Params {
		if v.Params[k] != want {
			t.Errorf("узел %d (%s): параметр %s=%q, у эмиттера %q", i, what, k, v.Params[k], want)
		}
	}
}

// TestRouterVerdictJSON: ключи вердикта — контракт клиента.
func TestRouterVerdictJSON(t *testing.T) {
	routertest.Current(t)
	keys := func(v Verdict) []string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("вердикт не объект: %v", err)
		}
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	full := keys(Verdict{OK: false, Code: CodeNeedsMihomo, Params: map[string]string{"kernel_min": "1.19.22"}, Notes: []string{CodeNoteUDPDropped}})
	if strings.Join(full, ",") != "code,notes,ok,params" {
		t.Errorf("ключи вердикта с отказом: %v", full)
	}
	if got := keys(Verdict{OK: true}); strings.Join(got, ",") != "ok" {
		t.Errorf("ключи вердикта OK: %v", got)
	}
}
