//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
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

// Трасса: «Мои узлы» с узлом применены → POST create {template: rule-based.yaml} →
// профиль на диске с файлом источника, ключами для XKeen и комментариями шаблона,
// без заглушки подписки; занятое имя — 409, шаблон вне списка — 400.
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

	t.Run("rule_based", func(t *testing.T) {
		e := root.with(t)
		const name = rtTplPrefix + "rule-based"
		r := e.rtTplCreate(name, "rule-based.yaml")
		if r.Status != http.StatusOK {
			t.Fatalf("создание из rule-based.yaml: %d %s %s", r.Status, r.Code, r.Detail)
		}
		got := rtTplReadProfile(t, name)
		for _, want := range []string{"xcp-my:", "./proxy_providers/xcp-my.yaml", "external-controller-unix:", "routing-mark: 255"} {
			if !strings.Contains(got, want) {
				t.Errorf("в профиле нет %q", want)
			}
		}
		if strings.Contains(got, "example.com") {
			t.Errorf("в профиле осталась заглушка example.com")
		}
		tpl := templates["rule-based.yaml"]
		if first := strings.SplitN(tpl, "\n", 2)[0]; !strings.Contains(got, first) {
			t.Errorf("первая строка шаблона (комментарий) пропала из профиля")
		}
		rtTplRealCheck(t, name)
	})

	t.Run("errors", func(t *testing.T) {
		e := root.with(t)
		if r := e.rtTplCreate(rtTplPrefix+"rule-based", "rule-based.yaml"); r.Status != http.StatusConflict || r.Code != "profile_exists" {
			t.Errorf("повторное создание: %d %q, ожидалось 409 profile_exists", r.Status, r.Code)
		}
		if r := e.rtTplCreate(rtTplPrefix+"awg", "awg-antifilter.yaml"); r.Status != http.StatusBadRequest || r.Code != "template_unknown" {
			t.Errorf("шаблон вне списка: %d %q, ожидалось 400 template_unknown", r.Status, r.Code)
		}
		if _, err := os.Stat(rtTplProfilePath(rtTplPrefix + "awg")); err == nil {
			t.Errorf("отказанный шаблон оставил файл профиля")
		}
	})
}
