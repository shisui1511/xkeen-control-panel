//go:build router

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Профиль из шаблона на настоящей панели устройства (PART-02, D-01…D-03): панель
// дописывает недостающие ключи, подставляет файлы источников и проверяет итог тем же
// `mihomo -t` в зеркале корня, что и сохранение. Вердикт — ответ панели, байты файла
// профиля на диске и повторный запуск настоящего mihomo -t по корню Mihomo. Файлы
// rt-tpl-* убираются в t.Cleanup, ядро стенда не перезапускается самим сценарием
// (перезапуск идёт только от применения узла «Моих узлов» слоя).

const rtTplPrefix = "rt-tpl-"

// rtTplProfilePath — путь файла профиля в корне Mihomo устройства.
func rtTplProfilePath(name string) string {
	return filepath.Join(rtRtMihomoDir, "profiles", name+".yaml")
}

// rtTplRemoveProfiles удаляет профили rt-tpl-* (остатки прошлого прогона и свои).
func rtTplRemoveProfiles() {
	files, _ := filepath.Glob(filepath.Join(rtRtMihomoDir, "profiles", rtTplPrefix+"*.yaml"))
	for _, f := range files {
		_ = os.Remove(f)
	}
}

// rtTplContents читает содержимое шаблонов каталога панели (файл → текст).
func (e *rtSrc) rtTplContents() map[string]string {
	e.t.Helper()
	status, raw, err := e.p.Call(http.MethodGet, "/api/templates/list", nil)
	if err != nil || status != http.StatusOK {
		e.t.Fatalf("GET /api/templates/list: %d %v", status, err)
	}
	var list []struct {
		Type     string `json:"type"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		e.t.Fatalf("каталог шаблонов не разобран: %v", err)
	}
	out := map[string]string{}
	for _, t := range list {
		if t.Type == "mihomo" {
			out[t.Filename] = t.Content
		}
	}
	return out
}

// rtTplCreate создаёт профиль из шаблона (запрос идёт вместе с `mihomo -t`).
func (e *rtSrc) rtTplCreate(name, template string) rtSrcResp {
	e.t.Helper()
	return e.callSlow(http.MethodPost, "/api/mihomo/profiles/create", map[string]string{"name": name, "template": template})
}

// rtTplRealCheck — повторный запуск настоящего `mihomo -t` по корню Mihomo устройства.
func rtTplRealCheck(t *testing.T, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, rtSaveBinary, "-t", "-d", rtRtMihomoDir, "-f", rtTplProfilePath(name))
	if code != 0 {
		t.Errorf("mihomo -t отверг %s: код %d\n%s%s", name, code, stdout, stderr)
	}
}

// rtTplReadProfile читает файл профиля с диска.
func rtTplReadProfile(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(rtTplProfilePath(name))
	if err != nil {
		t.Fatalf("профиль %s не читается: %v", name, err)
	}
	return string(raw)
}

// rtTplHasKey — ключ верхнего уровня есть в тексте профиля.
func rtTplHasKey(content, key string) bool {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:`).MatchString(content)
}

// rtTplPrepare включает слой, убирает узел прошлого прогона из «Моих узлов» и
// применяет узел из ссылки: файл провайдера xcp-my.yaml оказывается на диске.
func (e *rtSrc) rtTplPrepare(link string) {
	e.t.Helper()
	e.enableLayer()
	// Узел «Моих узлов» из прогона не остаётся в применённом состоянии: уборка идёт
	// до сброса черновика и возврата флага слоя.
	e.t.Cleanup(e.rtTplDropNode)
	e.rtRstIdle()
	e.rtTplDropNode()
	e.rtRstAddMyNode(link)
	before := e.rtRstStartApply()
	rtRstExpectApplied(e.t, e.rtRstWaitApply(before))
	if p := e.rtRstLayerFile("proxy_providers/xcp-my.yaml"); p == "" || !rtRstExists(p) {
		e.t.Fatalf("файл провайдера xcp-my.yaml не записан на диск (путь %q)", p)
	}
}

// rtTplDropNode убирает узел «Моих узлов» из применённого состояния.
func (e *rtSrc) rtTplDropNode() {
	e.t.Helper()
	if id := e.rtRstMyEntry(); id != "" {
		e.rtRstDeleteMyNode(id)
		before := e.rtRstStartApply()
		e.rtRstWaitApply(before)
		e.rtRstIdle()
	}
}

// rtTplPreview читает GET /api/mihomo/profiles/preview для файла шаблона.
func (e *rtSrc) rtTplPreview(template string) rtSrcResp {
	e.t.Helper()
	return e.call(http.MethodGet, "/api/mihomo/profiles/preview?template="+url.QueryEscape(template), nil)
}

// rtTplProfileCount — число файлов в profiles/ (предпросмотр ничего не пишет).
func rtTplProfileCount() int {
	files, _ := filepath.Glob(filepath.Join(rtRtMihomoDir, "profiles", "*"))
	return len(files)
}

// Трасса: «Мои узлы» с узлом применены → POST create по каждому из восьми шаблонов →
// профиль на диске с файлом источника, ключами для XKeen, комментариями шаблона и без
// заглушки подписки, настоящий mihomo -t принял; затем предпросмотр, занятое имя (409),
// шаблон вне списка (400) и профиль без единого источника.
func TestRouterProfileTemplates(t *testing.T) {
	routertest.Current(t)
	if _, err := os.Stat(rtSaveBinary); err != nil {
		t.Skip("Mihomo не установлен на стенде")
	}
	root := rtNewSrc(t)
	link := rtLinkText(t, "vless_ws")
	rtTplRemoveProfiles()
	t.Cleanup(rtTplRemoveProfiles)
	root.rtTplPrepare(link)
	templates := root.rtTplContents()

	for _, file := range services.ProfileTemplateFiles {
		t.Run(file, func(t *testing.T) {
			e := root.with(t)
			tpl := templates[file]
			if tpl == "" {
				t.Fatalf("шаблон %s не найден в каталоге панели", file)
			}
			name := rtTplPrefix + strings.TrimSuffix(file, ".yaml")
			r := e.rtTplCreate(name, file)
			if r.Status != http.StatusOK {
				t.Fatalf("создание из %s: %d %s %s", file, r.Status, r.Code, r.Detail)
			}
			got := rtTplReadProfile(t, name)
			rtTplRealCheck(t, name)

			if !strings.Contains(got, "xcp-my:") || !strings.Contains(got, "./proxy_providers/xcp-my.yaml") {
				t.Errorf("в профиле из %s нет файла источника xcp-my", file)
			}
			if strings.Contains(got, "https://example.com") {
				t.Errorf("в профиле из %s осталась заглушка подписки", file)
			}
			for _, key := range []string{"routing-mark", "redir-port", "tproxy-port"} {
				if !rtTplHasKey(got, key) {
					t.Errorf("в профиле из %s нет ключа %s", file, key)
				}
			}
			if !rtTplHasKey(got, "external-controller-unix") {
				t.Errorf("в профиле из %s нет external-controller-unix", file)
			}
			if first := strings.SplitN(tpl, "\n", 2)[0]; !strings.Contains(got, first) {
				t.Errorf("первая строка шаблона %s пропала из профиля", file)
			}
			switch file {
			case "rule-based.yaml":
				if !strings.Contains(got, "routing-mark: 255") {
					t.Errorf("в профиле из %s нет routing-mark: 255", file)
				}
			case "zkeen.yaml":
				if a, b := strings.Count(got, "&"), strings.Count(tpl, "&"); a != b {
					t.Errorf("zkeen: якорей %d, в шаблоне %d", a, b)
				}
				if a, b := strings.Count(got, "<<: *"), strings.Count(tpl, "<<: *"); a != b {
					t.Errorf("zkeen: слияний %d, в шаблоне %d", a, b)
				}
			case "smart-antifilter.yaml", "split-services.yaml":
				if !strings.Contains(tpl, "https://example.com") {
					t.Errorf("шаблон %s больше не содержит заглушки: сценарий устарел", file)
				}
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		e := root.with(t)
		if r := e.rtTplCreate(rtTplPrefix+"rule-based", "rule-based.yaml"); r.Status != http.StatusConflict || r.Code != "profile_exists" {
			t.Errorf("повторное создание: %d %q, ожидалось 409 profile_exists", r.Status, r.Code)
		}
		for _, bad := range []string{"awg-antifilter.yaml", "multi-sub-hwid.yaml", "../rule-based.yaml"} {
			name := rtTplPrefix + "bad"
			if r := e.rtTplCreate(name, bad); r.Status != http.StatusBadRequest || r.Code != "template_unknown" {
				t.Errorf("шаблон %s: %d %q, ожидалось 400 template_unknown", bad, r.Status, r.Code)
			}
			if _, err := os.Stat(rtTplProfilePath(name)); err == nil {
				t.Errorf("отказанный шаблон %s оставил файл профиля", bad)
			}
		}
	})

	t.Run("preview", func(t *testing.T) {
		e := root.with(t)
		before := rtTplProfileCount()
		r := e.rtTplPreview("rule-based.yaml")
		if r.Status != http.StatusOK {
			t.Fatalf("предпросмотр rule-based.yaml: %d %s", r.Status, r.Code)
		}
		o := r.object(t)
		added, _ := o["added"].([]any)
		if !rtTplContainsKey(added, "external-controller-unix") {
			t.Errorf("предпросмотр rule-based.yaml: added без external-controller-unix: %v", added)
		}
		if o["kind"] != "template" || o["no_sources"] != false {
			t.Errorf("предпросмотр rule-based.yaml: kind=%v no_sources=%v", o["kind"], o["no_sources"])
		}
		if n, _ := o["sources"].(float64); n < 1 {
			t.Errorf("предпросмотр: источников %v, ожидался хотя бы один", o["sources"])
		}
		if n, _ := o["nodes"].(float64); n < 1 {
			t.Errorf("предпросмотр: узлов %v, ожидался хотя бы один", o["nodes"])
		}
		for _, key := range []string{"redir", "tproxy"} {
			if n, _ := o[key].(float64); n <= 0 {
				t.Errorf("предпросмотр: порт %s = %v", key, o[key])
			}
		}
		if _, ok := o["unix_unsupported"].(bool); !ok {
			t.Errorf("предпросмотр: нет булева unix_unsupported: %.200s", r.Data)
		}

		z := e.rtTplPreview("zkeen.yaml")
		if z.Status != http.StatusOK {
			t.Fatalf("предпросмотр zkeen.yaml: %d %s", z.Status, z.Code)
		}
		zAdded, _ := z.object(t)["added"].([]any)
		if rtTplContainsKey(zAdded, "external-controller-unix") {
			t.Errorf("предпросмотр zkeen.yaml: сокет уже есть в шаблоне, added = %v", zAdded)
		}

		if bad := e.rtTplPreview("awg-antifilter.yaml"); bad.Status != http.StatusBadRequest || bad.Code != "template_unknown" {
			t.Errorf("предпросмотр шаблона вне списка: %d %q, ожидалось 400 template_unknown", bad.Status, bad.Code)
		}
		if after := rtTplProfileCount(); after != before {
			t.Errorf("предпросмотр изменил profiles/: было %d файлов, стало %d", before, after)
		}
	})

	// Без источников профиль создаётся с пустым proxy-providers. Если ядро отвергает
	// группы include-all без единого провайдера, это находка по шаблону.
	t.Run("no_sources", func(t *testing.T) {
		e := root.with(t)
		e.rtTplDropNode()
		const name = rtTplPrefix + "no-sources"
		r := e.rtTplCreate(name, "rule-based.yaml")
		if r.Status != http.StatusOK {
			t.Fatalf("шаблон rule-based.yaml без источников отвергнут: %d %s %s", r.Status, r.Code, r.Detail)
		}
		got := rtTplReadProfile(t, name)
		if !strings.Contains(got, "proxy-providers: {}") {
			t.Errorf("без источников в профиле ожидался proxy-providers: {}")
		}
		rtTplRealCheck(t, name)
		p := e.rtTplPreview("rule-based.yaml")
		if p.Status != http.StatusOK {
			t.Fatalf("предпросмотр без источников: %d %s", p.Status, p.Code)
		}
		if o := p.object(t); o["no_sources"] != true || o["sources"] != float64(0) {
			t.Errorf("предпросмотр без источников: no_sources=%v sources=%v", o["no_sources"], o["sources"])
		}
	})
}

// rtTplContainsKey — среди added есть ключ.
func rtTplContainsKey(added []any, key string) bool {
	for _, v := range added {
		if s, _ := v.(string); s == key {
			return true
		}
	}
	return false
}

// --- копия профиля панели (D-04, D-11) -----------------------------------------------

const (
	rtCpCopy = "rt-copy"
	rtCpBad  = "rt-copy-bad"
	rtCpUser = "rt-copy-user"
	rtCpConf = "rt-copy-conf"
	rtCpSrc  = "rt-cp-src"
)

// rtCpRemove убирает профили и копии списков сценария (остатки прошлого прогона и свои).
func rtCpRemove() {
	for _, pat := range []string{
		filepath.Join(rtRtMihomoDir, "profiles", "rt-copy*.yaml"),
		filepath.Join(rtRtMihomoDir, "profiles", rtCpSrc+".yaml"),
		filepath.Join(rtRtMihomoDir, "rule_providers", "rt-copy*.txt"),
	} {
		files, _ := filepath.Glob(pat)
		for _, f := range files {
			_ = os.Remove(f)
		}
	}
}

// rtCpListPaths — пути списков правил профиля панели вида ./rule_providers/xcp-<имя>.txt.
func rtCpListPaths(text string) []string {
	return regexp.MustCompile(`\./rule_providers/xcp-[a-z0-9][a-z0-9-]*\.txt`).FindAllString(text, -1)
}

// rtCpDuplicateKey — строка верхнего уровня, которую ядро считает известным ключом:
// её повтор в конце файла ядро отвергает (поле задано дважды).
func rtCpDuplicateKey(t *testing.T, content string) string {
	t.Helper()
	for _, key := range []string{"mode", "log-level", "mixed-port", "allow-lan", "ipv6"} {
		if m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]+\S.*$`).FindString(content); m != "" {
			return m
		}
	}
	t.Fatal("в профиле панели нет известного скалярного ключа верхнего уровня для повтора")
	return ""
}

// rtCpExists — файл есть на диске.
func rtCpExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Трасса: маршрутизация Mihomo взята (группа и правило со списком dat, поэтому у профиля
// панели есть свои файлы списков) → копия xcp-main: ссылок на rule_providers/xcp- нет,
// рядом rule_providers/rt-copy-*.txt побайтно равные оригиналам, узлы по-прежнему из
// proxy_providers/xcp-*; отказ `mihomo -t` не оставляет ни профиля, ни копий; отпускание
// удаляет файлы xcp-* списков, копия и её списки остаются и принимаются mihomo -t;
// при отпущенном — 409 panel_absent; копия профиля пользователя и обычного config.yaml
// побайтная. Метки ядра нет: взятие идёт при любом активном ядре.
func TestRouterProfileCopyPanel(t *testing.T) {
	tg := routertest.Current(t)
	if _, err := os.Stat(rtSaveBinary); err != nil {
		t.Skip("Mihomo не установлен на стенде")
	}
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	rtCpRemove()
	t.Cleanup(rtCpRemove)

	// Список dat: самый короткий доменный список из каталога панели.
	lr := e.call(http.MethodGet, rtRoutingBase+"/lists", nil)
	if lr.Status != http.StatusOK {
		t.Fatalf("GET lists: %d %s", lr.Status, lr.Code)
	}
	var ref map[string]any
	best := 0.0
	entries, _ := lr.object(t)["lists"].([]any)
	for _, it := range entries {
		m, _ := it.(map[string]any)
		n, _ := m["count"].(float64)
		if m != nil && m["kind"] == "site" && n > 0 && (ref == nil || n < best) {
			ref, best = m, n
		}
	}
	if ref == nil {
		t.Fatal("в каталоге списков нет доменного списка dat")
	}

	m := rtCoresLiveMihomoTakeDraft(t, e, tg)
	save := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group": map[string]any{
			"name": "rt-copy-group", "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{{"kind": "direct"}},
		},
	})
	if save.Status != http.StatusOK {
		t.Fatalf("groups/save: %d %s", save.Status, save.Code)
	}
	gid, _ := save.object(t)["group"].(map[string]any)["id"].(string)
	rule := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{
		"revision": e.rtRtRevision(),
		"rule": map[string]any{
			"enabled": true,
			"conds": []map[string]any{{
				"type": "geosite", "values": []string{},
				"lists": []map[string]any{{"file": ref["file"], "tag": ref["tag"], "kind": ref["kind"]}},
			}},
			"target": map[string]string{"kind": "group", "id": gid},
		},
	})
	if rule.Status != http.StatusOK {
		t.Fatalf("rules/save со списком: %d %s", rule.Status, rule.Code)
	}
	e.rtRtApplyOK("взятие маршрутизации Mihomo со списком")

	panelFile := rtTplProfilePath("xcp-main")
	panel, err := os.ReadFile(panelFile)
	if err != nil {
		t.Fatalf("профиль панели не записан: %v", err)
	}
	panelSum := rtRtSum(panel)
	paths := rtCpListPaths(string(panel))
	if len(paths) == 0 {
		t.Fatal("в профиле панели нет файлов списков rule_providers/xcp-*: сценарию нечего копировать")
	}
	originals := map[string][]byte{}
	for _, p := range paths {
		list := strings.TrimSuffix(strings.TrimPrefix(p, "./rule_providers/xcp-"), ".txt")
		raw, rerr := os.ReadFile(filepath.Join(rtRtMihomoDir, "rule_providers", "xcp-"+list+".txt"))
		if rerr != nil {
			t.Fatalf("файл списка %s не записан панелью: %v", p, rerr)
		}
		originals[list] = raw
	}

	// Предпросмотр и копия при профиле панели на диске.
	if pv := e.call(http.MethodGet, "/api/mihomo/profiles/preview?from=xcp-main", nil); pv.Status != http.StatusOK || pv.object(t)["kind"] != "copy_panel" {
		t.Errorf("предпросмотр xcp-main: %d %q, ожидалось kind copy_panel", pv.Status, pv.Code)
	}
	r := e.callSlow(http.MethodPost, "/api/mihomo/profiles/create", map[string]string{"name": rtCpCopy, "from": "xcp-main"})
	if r.Status != http.StatusOK {
		t.Fatalf("копия xcp-main: %d %s %s", r.Status, r.Code, r.Detail)
	}
	got := rtTplReadProfile(t, rtCpCopy)
	if strings.Contains(got, "rule_providers/xcp-") {
		t.Error("в копии остались ссылки на rule_providers/xcp-*")
	}
	if !strings.Contains(got, "proxy_providers/xcp-my.yaml") {
		t.Error("в копии нет ссылки на proxy_providers/xcp-my.yaml: узлы должны оставаться ссылками")
	}
	for list, want := range originals {
		path := filepath.Join(rtRtMihomoDir, "rule_providers", rtCpCopy+"-"+list+".txt")
		have, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Errorf("копия списка %s не записана: %v", list, rerr)
			continue
		}
		if !bytes.Equal(have, want) {
			t.Errorf("копия списка %s отличается от оригинала", list)
		}
		if !strings.Contains(got, "./rule_providers/"+rtCpCopy+"-"+list+".txt") {
			t.Errorf("в копии не переписан путь списка %s", list)
		}
	}
	rtTplRealCheck(t, rtCpCopy)
	if after, _ := os.ReadFile(panelFile); rtRtSum(after) != panelSum {
		t.Error("копирование изменило профиль панели")
	}

	// Отказ mihomo -t: у настоящего файла панели его не воспроизвести, поэтому в файл
	// дописывается повтор известного ключа; панельный файл возвращается сразу же.
	info, err := os.Stat(panelFile)
	if err != nil {
		t.Fatal(err)
	}
	restore := func() { _ = os.WriteFile(panelFile, panel, info.Mode().Perm()) }
	t.Cleanup(restore)
	broken := append(append([]byte{}, panel...), []byte("\n"+rtCpDuplicateKey(t, string(panel))+"\n")...)
	if err := os.WriteFile(panelFile, broken, info.Mode().Perm()); err != nil {
		t.Fatalf("профиль панели не изменён: %v", err)
	}
	bad := e.callSlow(http.MethodPost, "/api/mihomo/profiles/create", map[string]string{"name": rtCpBad, "from": "xcp-main"})
	restore()
	if bad.Status != http.StatusUnprocessableEntity || bad.Code != "profile_invalid" {
		t.Errorf("копия с отказом ядра: %d %q, ожидалось 422 profile_invalid", bad.Status, bad.Code)
	}
	if rtCpExists(rtTplProfilePath(rtCpBad)) {
		t.Error("отказ ядра оставил файл профиля")
	}
	if left, _ := filepath.Glob(filepath.Join(rtRtMihomoDir, "rule_providers", rtCpBad+"-*.txt")); len(left) > 0 {
		t.Errorf("отказ ядра оставил копий списков: %d", len(left))
	}

	// Отпустить и применить: файлы списков панели удалены, копия самостоятельна.
	if !m.rtCoresLiveRelease(t) {
		t.FailNow()
	}
	for _, p := range paths {
		if rtCpExists(filepath.Join(rtRtMihomoDir, strings.TrimPrefix(p, "./"))) {
			t.Errorf("после отпускания остался файл списка панели %s", p)
		}
	}
	for list, want := range originals {
		have, rerr := os.ReadFile(filepath.Join(rtRtMihomoDir, "rule_providers", rtCpCopy+"-"+list+".txt"))
		if rerr != nil || !bytes.Equal(have, want) {
			t.Errorf("после отпускания копия списка %s потеряна или изменена", list)
		}
	}
	rtTplRealCheck(t, rtCpCopy)

	// Профиля панели нет: копия и предпросмотр отвечают 409 panel_absent.
	if rtCpExists(panelFile) {
		t.Fatal("после отпускания файл профиля панели остался")
	}
	if r := e.callSlow(http.MethodPost, "/api/mihomo/profiles/create", map[string]string{"name": rtCpBad, "from": "xcp-main"}); r.Status != http.StatusConflict || r.Code != "panel_absent" {
		t.Errorf("копия xcp-main без файла: %d %q, ожидалось 409 panel_absent", r.Status, r.Code)
	}
	if r := e.call(http.MethodGet, "/api/mihomo/profiles/preview?from=xcp-main", nil); r.Status != http.StatusConflict || r.Code != "panel_absent" {
		t.Errorf("предпросмотр xcp-main без файла: %d %q, ожидалось 409 panel_absent", r.Status, r.Code)
	}

	// Копия профиля пользователя и обычного config.yaml — побайтная.
	srcText := "mixed-port: 7891\nmode: rule\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(rtTplProfilePath(rtCpSrc), []byte(srcText), 0o600); err != nil {
		t.Fatalf("профиль-источник не записан: %v", err)
	}
	if pv := e.call(http.MethodGet, "/api/mihomo/profiles/preview?from="+rtCpSrc, nil); pv.Status != http.StatusOK || pv.object(t)["kind"] != "copy_user" {
		t.Errorf("предпросмотр профиля пользователя: %d %q, ожидалось kind copy_user", pv.Status, pv.Code)
	}
	if r := e.callSlow(http.MethodPost, "/api/mihomo/profiles/create", map[string]string{"name": rtCpUser, "from": rtCpSrc}); r.Status != http.StatusOK {
		t.Fatalf("копия профиля пользователя: %d %s", r.Status, r.Code)
	}
	if rtTplReadProfile(t, rtCpUser) != srcText || rtTplReadProfile(t, rtCpSrc) != srcText {
		t.Error("копия профиля пользователя не побайтная или источник изменён")
	}
	conf, err := os.ReadFile(filepath.Join(rtRtMihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml не читается: %v", err)
	}
	if pv := e.call(http.MethodGet, "/api/mihomo/profiles/preview?from=config.yaml", nil); pv.Status != http.StatusOK || pv.object(t)["kind"] != "copy_user" {
		t.Errorf("предпросмотр config.yaml: %d %q, ожидалось kind copy_user", pv.Status, pv.Code)
	}
	if r := e.callSlow(http.MethodPost, "/api/mihomo/profiles/create", map[string]string{"name": rtCpConf, "from": "config.yaml"}); r.Status != http.StatusOK {
		t.Fatalf("копия config.yaml: %d %s", r.Status, r.Code)
	}
	if rtTplReadProfile(t, rtCpConf) != string(conf) {
		t.Error("копия config.yaml не побайтная")
	}
}
