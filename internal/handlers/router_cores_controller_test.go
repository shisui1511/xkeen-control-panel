//go:build router

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services/configlayer"
)

// Регрессия G-147-4 / T-147-19 (D-11): TCP external-controller и
// external-controller-tls — только с непустым secret, в любой секции Mihomo.
// secret берётся из собранного профиля: «Основное», «Входящие», «Прочие ключи».
// Вердикт — ответы панели (save, raw/parse, checks снимка, 409 apply_blocked) и
// профиль на диске после настоящего «Применить» с mihomo -t. Стенд возвращается в
// t.Cleanup. В сообщения попадают только коды, ключи и имена секций; значение
// secret не печатается. Метка ядра: при активном Mihomo профиль с контроллером
// идёт перезагрузкой ядра, при Xray пишутся только файлы, а помощник взятия
// сверяет возврат по ядру цели.

// rtCoresCtlSecret — непустая строка-фикстура secret.
const rtCoresCtlSecret = "xcp-ctl-fixture"

// rtCoresCtlKeys — ключи, открывающие API ядра по TCP.
var rtCoresCtlKeys = []string{"external-controller", "external-controller-tls"}

// rtCoresCtlSections — секции Mihomo, ключи которых ложатся в корень профиля.
var rtCoresCtlSections = []string{"main", "inbounds", "other"}

// rtCoresCtlTitle — имя секции в сообщениях.
func rtCoresCtlTitle(section string) string {
	switch section {
	case "main":
		return "Основное"
	case "inbounds":
		return "Входящие"
	}
	return "Прочие ключи"
}

// rtCoresCtlAccepted сообщает, что ключ контроллера без secret принят.
func rtCoresCtlAccepted(t *testing.T, section, key, stage string) {
	t.Helper()
	t.Errorf("%s: %s без secret принят (%s)", section, key, stage)
}

// rtCoresCtlAdvanced разбирает «Дополнительно» как YAML-отображение, снимает drop
// и выставляет set.
func rtCoresCtlAdvanced(t *testing.T, advanced string, set map[string]any, drop ...string) string {
	t.Helper()
	keys := map[string]any{}
	if strings.TrimSpace(advanced) != "" {
		if err := yaml.Unmarshal([]byte(advanced), &keys); err != nil {
			t.Fatalf("«Дополнительно» не YAML: %v", err)
		}
		if keys == nil {
			keys = map[string]any{}
		}
	}
	for _, k := range drop {
		delete(keys, k)
	}
	for k, v := range set {
		keys[k] = v
	}
	out, err := yaml.Marshal(keys)
	if err != nil {
		t.Fatalf("«Дополнительно» не собирается в YAML: %v", err)
	}
	if len(keys) == 0 {
		return ""
	}
	return string(out)
}

// rtCoresCtlHasAnyKey сообщает, что в «Дополнительно» есть хотя бы один из ключей.
func rtCoresCtlHasAnyKey(t *testing.T, advanced string, keys []string) bool {
	t.Helper()
	cur := map[string]any{}
	if err := yaml.Unmarshal([]byte(advanced), &cur); err != nil {
		t.Fatalf("«Дополнительно» не YAML: %v", err)
	}
	for _, k := range keys {
		if _, ok := cur[k]; ok {
			return true
		}
	}
	return false
}

// rtCoresCtlPart — форма и «Дополнительно» одной секции.
type rtCoresCtlPart struct {
	form     any
	advanced string
}

// rtCoresCtlBase — исходные значения секций main, inbounds, other.
type rtCoresCtlBase struct {
	e     *rtSrc
	parts map[string]rtCoresCtlPart
}

// rtCoresCtlRead читает форму и «Дополнительно» секции из вида Mihomo.
func rtCoresCtlRead(t *testing.T, e *rtSrc, section string) rtCoresCtlPart {
	t.Helper()
	sec := rtCoresSection(e.rtCoresView("mihomo"), section)
	if sec == nil {
		t.Fatalf("в виде Mihomo нет секции %s", section)
	}
	form := sec["form"]
	if form == nil {
		form = map[string]any{}
	}
	adv, _ := sec["advanced"].(string)
	return rtCoresCtlPart{form: form, advanced: adv}
}

// rtCoresCtlSave сохраняет секцию Mihomo без проверки статуса.
func rtCoresCtlSave(t *testing.T, e *rtSrc, section string, form any, advanced string) rtSrcResp {
	t.Helper()
	return e.rtCoresPost("/save", map[string]any{
		"kernel": "mihomo", "section": section, "form": form, "advanced": advanced,
	})
}

// rtCoresCtlRestore возвращает секции main, inbounds, other к исходным значениям
// и применяет, если черновик отличается. Save делает секцию управляемой, поэтому
// возвращает и секцию, снятую с управления через общий черновик.
func (b *rtCoresCtlBase) rtCoresCtlRestore(t *testing.T) {
	t.Helper()
	c := b.e.with(t)
	for _, section := range rtCoresCtlSections {
		p := b.parts[section]
		if r := rtCoresCtlSave(t, c, section, p.form, p.advanced); r.Status != http.StatusOK {
			t.Errorf("возврат секции %s: %d %s", rtCoresCtlTitle(section), r.Status, r.Code)
		}
	}
	if c.rtRtDraftChanges() > 0 {
		c.rtRtApplyOK("возврат секций Mihomo")
	}
}

// rtCoresCtlExpectKey требует отказ save с controller_no_secret для ключа; иначе
// сообщает о принятом ключе и возвращает false при ответе 200.
func rtCoresCtlExpectKey(t *testing.T, r rtSrcResp, section, key, stage string) (accepted bool) {
	t.Helper()
	switch {
	case r.Status == http.StatusOK:
		rtCoresCtlAccepted(t, rtCoresCtlTitle(section), key, stage)
		return true
	case r.Status != http.StatusBadRequest || r.Code != configlayer.IssueControllerNoSecret || r.Params["key"] != key:
		t.Errorf("%s: %s (%s): %d %q params.key=%q, ожидалось 400 controller_no_secret с этим ключом",
			rtCoresCtlTitle(section), key, stage, r.Status, r.Code, r.Params["key"])
	}
	return false
}

// rtCoresCtlParse вызывает raw/parse и возвращает rejected как список «ключ:код».
func rtCoresCtlParse(t *testing.T, e *rtSrc, section, text string) []string {
	t.Helper()
	r := e.call(http.MethodPost, rtCoresBase+"/raw/parse", map[string]any{
		"kernel": "mihomo", "section": section, "text": text,
	})
	if r.Status != http.StatusOK {
		t.Fatalf("raw/parse %s: %d %s", section, r.Status, r.Code)
	}
	var out []string
	rej, _ := r.object(t)["rejected"].([]any)
	for _, it := range rej {
		m, _ := it.(map[string]any)
		out = append(out, fmt.Sprintf("%v:%v", m["key"], m["code"]))
	}
	return out
}

// rtCoresCtlRejected сообщает, что в итоге raw/parse есть отказ controller_no_secret для ключа.
func rtCoresCtlRejected(rejected []string, key string) bool {
	return slices.Contains(rejected, key+":"+configlayer.IssueControllerNoSecret)
}

// rtCoresCtlChecked сообщает, что в checks снимка есть controller_no_secret секции
// с ключом.
func rtCoresCtlChecked(t *testing.T, e *rtSrc, section, key string) bool {
	t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	checks, _ := r.object(t)["checks"].([]any)
	return rtCoresCtlHasIssue(checks, section, key)
}

// rtCoresCtlHasIssue ищет в списке проблем {key: секция, code: controller_no_secret, params.key: ключ}.
func rtCoresCtlHasIssue(list []any, section, key string) bool {
	for _, it := range list {
		m, _ := it.(map[string]any)
		params, _ := m["params"].(map[string]any)
		if m["key"] == section && m["code"] == configlayer.IssueControllerNoSecret && params["key"] == key {
			return true
		}
	}
	return false
}

// rtCoresCtlExpectBlocked требует, чтобы «Применить» было заблокировано
// controller_no_secret секции section по ключу key.
func rtCoresCtlExpectBlocked(t *testing.T, e *rtSrc, section, key string) {
	t.Helper()
	title := rtCoresCtlTitle(strings.TrimPrefix(section, "mihomo_"))
	before := e.rtRstApplyView()
	r := e.call(http.MethodPost, "/api/configlayer/apply", nil)
	switch {
	case r.Status == http.StatusConflict && r.Code == "apply_blocked":
		var data struct {
			Issues []any `json:"issues"`
		}
		_ = json.Unmarshal(r.Data, &data)
		if !rtCoresCtlHasIssue(data.Issues, section, key) {
			n := len(r.Data)
			if n > 300 {
				n = 300
			}
			t.Errorf("%s: apply_blocked без controller_no_secret для %s: %s", title, key, r.Data[:n])
		}
	case r.Status == http.StatusOK:
		e.rtRstWaitApply(before)
		rtCoresCtlAccepted(t, title, key, "«Применить» не заблокирован")
	default:
		t.Errorf("%s: POST /api/configlayer/apply: %d %q, ожидалось 409 apply_blocked", title, r.Status, r.Code)
	}
}

// rtCoresCtlProfileHas сообщает, что в config.yaml профиля панели external-controller
// равен адресу фикстуры, а secret — фикстуре; значения не печатаются.
func rtCoresCtlProfileHas(t *testing.T, addr string) (hasAddr, hasSecret bool) {
	t.Helper()
	top := rtCoresRetakeProfileTop(t)
	a, _ := top["external-controller"].(string)
	s, _ := top["secret"].(string)
	return a == addr, s == rtCoresCtlSecret
}

// rtCoresCtlUnmanage снимает секцию с управления через общий черновик, оставляя значение.
func rtCoresCtlUnmanage(t *testing.T, e *rtSrc, section string) {
	t.Helper()
	sec := rtCoresSection(e.rtCoresView("mihomo"), section)
	if sec == nil {
		t.Fatalf("в виде Mihomo нет секции %s", section)
	}
	r := e.call(http.MethodPost, "/api/configlayer/draft", map[string]any{
		"revision": e.revision(),
		"section":  configlayer.CoreSectionName(configlayer.KernelMihomo, section),
		"value":    map[string]any{"v": 1, "managed": false, "value": sec["value"]},
	})
	if r.Status != http.StatusOK {
		t.Fatalf("снятие секции %s с управления: %d %s", rtCoresCtlTitle(section), r.Status, r.Code)
	}
}

// Трасса: маршрутизация Mihomo взята → из секций убраны secret и контроллеры →
// main_without_secret: контроллер в «Основном» без secret отклонён в raw/parse и
// save → inbounds_with_and_without_secret: то же во «Входящих», с secret в той же
// секции применяется → secret_in_other_section: secret в «Прочих ключах»
// засчитывается, «Применить» проходит mihomo -t; после снятия secret «Применить»
// заблокирован, xcp-main.yaml не переписан → secret_edge_cases:
// external-controller-tls, пустой secret, secret секции вне управления.
func TestRouterCoresMihomoControllerSecret(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	rtCoresLiveMihomoTake(t, e, tg)
	addr := fmt.Sprintf("127.0.0.1:%d", rtCoresLiveFreePort(t))

	// Профиль стенда мог принести свой контроллер с secret, а сценарию нужен
	// профиль без них.
	dirty := false
	for _, section := range rtCoresCtlSections {
		p := rtCoresCtlRead(t, e, section)
		drop := []string{"secret", "external-controller", "external-controller-tls"}
		if !rtCoresCtlHasAnyKey(t, p.advanced, drop) {
			continue
		}
		clean := rtCoresCtlAdvanced(t, p.advanced, nil, drop...)
		if r := rtCoresCtlSave(t, e, section, p.form, clean); r.Status != http.StatusOK {
			t.Fatalf("очистка секции %s: %d %s", rtCoresCtlTitle(section), r.Status, r.Code)
		}
		dirty = true
	}
	if dirty {
		e.rtRtApplyOK("очистка контроллера и secret")
	}

	base := &rtCoresCtlBase{e: e, parts: map[string]rtCoresCtlPart{}}
	for _, section := range rtCoresCtlSections {
		base.parts[section] = rtCoresCtlRead(t, e, section)
	}
	// Регистрируется после взятия, поэтому выполнится до отпускания маршрутизации.
	t.Cleanup(func() { base.rtCoresCtlRestore(t) })

	t.Run("main_without_secret", func(t *testing.T) {
		t.Cleanup(func() { base.rtCoresCtlRestore(t) })
		s := e.with(t)
		p := base.parts["main"]
		for _, key := range rtCoresCtlKeys {
			text := rtCoresCtlAdvanced(t, p.advanced, map[string]any{key: addr})
			if !rtCoresCtlRejected(rtCoresCtlParse(t, s, "main", text), key) {
				rtCoresCtlAccepted(t, "Основное", key, "raw/parse")
			}
			r := rtCoresCtlSave(t, s, "main", p.form, text)
			if rtCoresCtlExpectKey(t, r, "main", key, "save") {
				base.rtCoresCtlRestore(t)
			}
		}
	})

	t.Run("inbounds_with_and_without_secret", func(t *testing.T) {
		t.Cleanup(func() { base.rtCoresCtlRestore(t) })
		s := e.with(t)
		p := base.parts["inbounds"]
		for _, key := range rtCoresCtlKeys {
			text := rtCoresCtlAdvanced(t, p.advanced, map[string]any{key: addr})
			if !rtCoresCtlRejected(rtCoresCtlParse(t, s, "inbounds", text), key) {
				rtCoresCtlAccepted(t, "Входящие", key, "raw/parse")
			}
			r := rtCoresCtlSave(t, s, "inbounds", p.form, text)
			if rtCoresCtlExpectKey(t, r, "inbounds", key, "save") {
				base.rtCoresCtlRestore(t)
			}
		}
		// С secret в той же секции контроллер применяется. external-controller-tls
		// в профиль не применяется: для настоящего TLS ядру нужен сертификат.
		text := rtCoresCtlAdvanced(t, p.advanced, map[string]any{"external-controller": addr, "secret": rtCoresCtlSecret})
		if r := rtCoresCtlSave(t, s, "inbounds", p.form, text); r.Status != http.StatusOK {
			t.Fatalf("Входящие: external-controller с secret в той же секции: %d %s", r.Status, r.Code)
		}
		s.rtRtApplyOK("контроллер с secret во «Входящих»")
		if hasAddr, hasSecret := rtCoresCtlProfileHas(t, addr); !hasAddr || !hasSecret {
			t.Errorf("Входящие: в xcp-main.yaml external-controller=%v, secret=%v (ожидались оба)", hasAddr, hasSecret)
		}
	})

	t.Run("secret_in_other_section", func(t *testing.T) {
		t.Cleanup(func() { base.rtCoresCtlRestore(t) })
		s := e.with(t)
		mainP, otherP := base.parts["main"], base.parts["other"]

		withSecret := rtCoresCtlAdvanced(t, otherP.advanced, map[string]any{"secret": rtCoresCtlSecret})
		if r := rtCoresCtlSave(t, s, "other", otherP.form, withSecret); r.Status != http.StatusOK {
			t.Fatalf("Прочие ключи: secret: %d %s", r.Status, r.Code)
		}
		mainText := rtCoresCtlAdvanced(t, mainP.advanced, map[string]any{"external-controller": addr})
		if slices.Contains(rtCoresCtlParse(t, s, "main", mainText), "external-controller:"+configlayer.IssueControllerNoSecret) {
			t.Error("secret из «Прочих ключей» не учтён (raw/parse)")
		}
		if r := rtCoresCtlSave(t, s, "main", mainP.form, mainText); r.Status != http.StatusOK {
			t.Fatalf("secret из «Прочих ключей» не учтён (save %d %s)", r.Status, r.Code)
		}
		s.rtRtApplyOK("контроллер в «Основном», secret в «Прочих ключах»")
		if hasAddr, hasSecret := rtCoresCtlProfileHas(t, addr); !hasAddr || !hasSecret {
			t.Errorf("в xcp-main.yaml external-controller=%v, secret=%v (ожидались оба)", hasAddr, hasSecret)
		}
		sum := rtRtScanDisk(t).mihomoSum

		// Secret снят в другой секции: save пропускает, «Применить» блокируется.
		if r := rtCoresCtlSave(t, s, "other", otherP.form, otherP.advanced); r.Status != http.StatusOK {
			t.Fatalf("Прочие ключи: снятие secret: %d %s", r.Status, r.Code)
		}
		if !rtCoresCtlChecked(t, s, configlayer.SectionMihomoMain, "external-controller") {
			rtCoresCtlAccepted(t, "Основное", "external-controller", "нет в checks снимка")
		}
		rtCoresCtlExpectBlocked(t, s, configlayer.SectionMihomoMain, "external-controller")
		if got := rtRtScanDisk(t).mihomoSum; got != sum {
			t.Error("xcp-main.yaml переписан при заблокированном «Применить»")
		}
	})

	t.Run("secret_edge_cases", func(t *testing.T) {
		t.Cleanup(func() { base.rtCoresCtlRestore(t) })
		s := e.with(t)
		mainP, otherP := base.parts["main"], base.parts["other"]

		// 1. external-controller-tls в «Прочих ключах»: прежняя ветка other видела
		// только external-controller.
		tls := rtCoresCtlAdvanced(t, otherP.advanced, map[string]any{"external-controller-tls": addr})
		if !rtCoresCtlRejected(rtCoresCtlParse(t, s, "other", tls), "external-controller-tls") {
			rtCoresCtlAccepted(t, "Прочие ключи", "external-controller-tls", "raw/parse")
		}
		if rtCoresCtlExpectKey(t, rtCoresCtlSave(t, s, "other", otherP.form, tls), "other", "external-controller-tls", "save") {
			base.rtCoresCtlRestore(t)
		}

		// 2. Пустой secret рядом с непустым в другой секции: корень получил бы
		// пустой и задвоенный secret.
		withSecret := rtCoresCtlAdvanced(t, otherP.advanced, map[string]any{"secret": rtCoresCtlSecret})
		if r := rtCoresCtlSave(t, s, "other", otherP.form, withSecret); r.Status != http.StatusOK {
			t.Fatalf("Прочие ключи: secret: %d %s", r.Status, r.Code)
		}
		empty := rtCoresCtlAdvanced(t, mainP.advanced, map[string]any{"external-controller": addr, "secret": ""})
		if !rtCoresCtlRejected(rtCoresCtlParse(t, s, "main", empty), "external-controller") {
			rtCoresCtlAccepted(t, "Основное", "external-controller", "raw/parse, пустой secret")
		}
		r := rtCoresCtlSave(t, s, "main", mainP.form, empty)
		if rtCoresCtlExpectKey(t, r, "main", "external-controller", "save, пустой secret") {
			if back := rtCoresCtlSave(t, s, "main", mainP.form, mainP.advanced); back.Status != http.StatusOK {
				t.Fatalf("Основное: возврат секции: %d %s", back.Status, back.Code)
			}
		}

		// 3. secret секции, которая не попадёт в профиль: неуправляемая секция
		// уходит в профиль пресетом без secret.
		plain := rtCoresCtlAdvanced(t, mainP.advanced, map[string]any{"external-controller": addr})
		if r := rtCoresCtlSave(t, s, "main", mainP.form, plain); r.Status != http.StatusOK {
			t.Fatalf("Основное: external-controller с secret в «Прочих ключах»: %d %s", r.Status, r.Code)
		}
		s.rtRtApplyOK("контроллер в «Основном», secret в «Прочих ключах»")
		sum := rtRtScanDisk(t).mihomoSum
		rtCoresCtlUnmanage(t, s, "other")
		if !rtCoresCtlChecked(t, s, configlayer.SectionMihomoMain, "external-controller") {
			rtCoresCtlAccepted(t, "Основное", "external-controller", "secret неуправляемой секции, нет в checks снимка")
		}
		rtCoresCtlExpectBlocked(t, s, configlayer.SectionMihomoMain, "external-controller")
		if got := rtRtScanDisk(t).mihomoSum; got != sum {
			t.Error("xcp-main.yaml переписан при заблокированном «Применить»")
		}
	})
}
