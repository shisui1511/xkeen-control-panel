//go:build router

package routing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// rtpbConnectivity — адрес проверки связи роутера в сценарии: на него никто не
// слушает, значение только пишется в файл и разбирается ядром при проверке.
const rtpbConnectivity = "http://127.0.0.1:1/generate_204"

// rtpbPing читает pingConfig и subjectSelector файла наблюдения панели.
func rtpbPing(t *testing.T, e *rtEnv) (ping map[string]any, subjects []string) {
	t.Helper()
	obs, _ := e.xrayDoc(t, xrayObservatoryFile)["burstObservatory"].(map[string]any)
	if obs == nil {
		t.Fatal("в файле observatory нет burstObservatory")
	}
	ping, _ = obs["pingConfig"].(map[string]any)
	if ping == nil {
		t.Fatal("в burstObservatory нет pingConfig")
	}
	return ping, rtxStrings(obs["subjectSelector"])
}

// burstObservatory правится целиком (D-13): connectivity, sampling и остальные
// ключи pingConfig из настроек проб доходят до 99_xcp-observatory.json и
// принимаются настоящим xray run -test конвейера «Применить»; границы значений
// проверяются до записи; пустые новые поля дают прежний вид файла. Ядро стенда
// не трогается (Applier не задан), ядро тест не читает и не переключает —
// метка ядра не нужна.
func TestRouterXrayProbeBurstFields(t *testing.T) {
	e := rtNewEnv(t, rtWithKernels(rtXrayOnly))
	e.addMyLinks(t, "vless_ws", "trojan_grpc", "ss_plain")
	wantSel := rtxXrayTags(e, t)

	// Шаг 1: все поля заданы — они в pingConfig, subjectSelector из групп.
	full := ProbeConfig{
		URL: rtxDefaultProbeURL, IntervalS: 20, TimeoutS: 5,
		Connectivity: rtpbConnectivity, Sampling: 5, Extra: json.RawMessage(`{"httpMethod":"GET"}`),
	}
	if _, err := e.svc.SaveProbe(e.rev(t), full); err != nil {
		t.Fatalf("SaveProbe (все поля): %v", err)
	}
	if got, err := e.svc.Probe(); err != nil || got.Connectivity != full.Connectivity || got.Sampling != 5 ||
		string(got.Extra) != string(full.Extra) {
		t.Fatalf("настройки проб после сохранения: %+v, %v", got, err)
	}
	e.takeAndApply(t)

	ping, sub := rtpbPing(t, e)
	if ping["connectivity"] != rtpbConnectivity || ping["sampling"] != float64(5) || ping["httpMethod"] != "GET" {
		t.Errorf("pingConfig с новыми полями: %v", ping)
	}
	if ping["destination"] != rtxDefaultProbeURL || ping["interval"] != "20s" || ping["timeout"] != "5s" {
		t.Errorf("pingConfig: поля формы потеряны: %v", ping)
	}
	if !rtxEqualStrings(sub, wantSel) {
		t.Errorf("subjectSelector %v, ожидались теги группы %v", sub, wantSel)
	}

	// Шаг 2: недопустимые значения отвергаются до записи, черновик не меняется.
	bad := []struct {
		name  string
		mod   func(p *ProbeConfig)
		field string
	}{
		{"sampling больше 100", func(p *ProbeConfig) { p.Sampling = 101 }, "sampling"},
		{"sampling отрицательный", func(p *ProbeConfig) { p.Sampling = -1 }, "sampling"},
		{"connectivity не http", func(p *ProbeConfig) { p.Connectivity = "ftp://x" }, "connectivity"},
		{"connectivity с пробелом", func(p *ProbeConfig) { p.Connectivity = "http://x/a b" }, "connectivity"},
		{"extra с interval", func(p *ProbeConfig) { p.Extra = json.RawMessage(`{"interval":"5s"}`) }, "extra"},
		{"extra с destination", func(p *ProbeConfig) { p.Extra = json.RawMessage(`{"destination":"http://x"}`) }, "extra"},
		{"extra с subjectSelector", func(p *ProbeConfig) { p.Extra = json.RawMessage(`{"subjectSelector":["x"]}`) }, "extra"},
		{"extra не объект", func(p *ProbeConfig) { p.Extra = json.RawMessage(`[1]`) }, "extra"},
		{"extra больше 16 КиБ", func(p *ProbeConfig) {
			p.Extra = json.RawMessage(`{"httpMethod":"` + strings.Repeat("a", maxProbeExtraBytes) + `"}`)
		}, "extra"},
	}
	for _, c := range bad {
		p := full
		c.mod(&p)
		_, err := e.svc.SaveProbe(e.rev(t), p)
		rtWantInvalid(t, c.name, err, c.field, reasonProbeInvalid)
	}
	want := full
	want.V = sectionVersion
	if got, err := e.svc.Probe(); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("после отказов в черновике не прежние пробы: %+v, %v", got, err)
	}

	// Шаг 3: sampling 0 — значение панели; без новых полей файл прежнего вида.
	plain := ProbeConfig{URL: rtxDefaultProbeURL, IntervalS: 20, TimeoutS: 5}
	if _, err := e.svc.SaveProbe(e.rev(t), plain); err != nil {
		t.Fatalf("SaveProbe (sampling 0, без новых полей): %v", err)
	}
	e.applyOK(t)
	ping, sub = rtpbPing(t, e)
	if ping["connectivity"] != "" || ping["sampling"] != float64(ProbeSampling) {
		t.Errorf("pingConfig без новых полей: connectivity=%v sampling=%v, ожидались \"\" и %d", ping["connectivity"], ping["sampling"], ProbeSampling)
	}
	if _, has := ping["httpMethod"]; has {
		t.Errorf("httpMethod остался в pingConfig после очистки extra: %v", ping)
	}
	if !rtxEqualStrings(sub, wantSel) {
		t.Errorf("subjectSelector %v, ожидались теги группы %v", sub, wantSel)
	}
}
