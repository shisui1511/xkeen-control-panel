//go:build router

// router-area: routing

package handlers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Проверка API маршрутизации на настоящей панели устройства: маршруты
// /api/configlayer/routing* вызываются по HTTPS с сессией, созданной на ПК
// (routertest.Panel). Вердикт — коды и тела ответов панели, файлы ядер на диске
// устройства и Clash API Mihomo через панель. Маршрутизация возвращается
// «Отпустить» + «Применить», черновик после сценария сбрасывается. В сообщения
// попадают только коды и числа.

const rtRoutingBase = "/api/configlayer/routing"

const (
	// rtRtXrayDir — каталог конфигов Xray по умолчанию.
	rtRtXrayDir = "/opt/etc/xray/configs"
	// rtRtMihomoDir — каталог Mihomo по умолчанию.
	rtRtMihomoDir = "/opt/etc/mihomo"
	// rtRtPanelProfile — профиль панели внутри каталога Mihomo.
	rtRtPanelProfile = "profiles/xcp-main.yaml"
	// rtRtProviderWait — сколько ждать провайдера «Моих узлов» в Clash API после
	// перезапуска Mihomo.
	rtRtProviderWait = 90 * time.Second
)

// rtRtDiskState — слепок файлов маршрутизации ядер на диске устройства.
type rtRtDiskState struct {
	// xray — файлы каталога конфигов Xray с ключом routing верхнего уровня: имя → sha256.
	xray map[string]string
	// xrayPure — из них «чистые»: в файле нет ничего, кроме routing.
	xrayPure map[string]bool
	// mihomoTarget — путь, на который указывает config.yaml Mihomo ("" — файла нет).
	mihomoTarget string
	// mihomoSum — sha256 содержимого config.yaml.
	mihomoSum string
}

func rtRtSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// rtRtScanDisk снимает слепок файлов routing Xray и config.yaml Mihomo.
func rtRtScanDisk(t *testing.T) rtRtDiskState {
	t.Helper()
	st := rtRtDiskState{xray: map[string]string{}, xrayPure: map[string]bool{}}
	entries, err := os.ReadDir(rtRtXrayDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("каталог конфигов Xray не читается: %v", err)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rtRtXrayDir, ent.Name()))
		if err != nil {
			continue
		}
		var top map[string]json.RawMessage
		if json.Unmarshal(data, &top) != nil {
			continue
		}
		if _, ok := top["routing"]; !ok {
			continue
		}
		st.xray[ent.Name()] = rtRtSum(data)
		st.xrayPure[ent.Name()] = len(top) == 1
	}
	cfg := filepath.Join(rtRtMihomoDir, "config.yaml")
	if target, err := filepath.EvalSymlinks(cfg); err == nil {
		st.mihomoTarget = target
		if data, err := os.ReadFile(cfg); err == nil {
			st.mihomoSum = rtRtSum(data)
		}
	}
	return st
}

// rtRtUniqueLink берёт ссылку фикстуры и подставляет порт, которого нет у других
// источников стенда.
func rtRtUniqueLink(t *testing.T) string {
	t.Helper()
	return rtRtUniqueLinkOf(t, "vless_ws", 0)
}

// rtRtUniqueLinkOf — как rtRtUniqueLink для заданной фикстуры; salt разводит порты
// нескольких ссылок, взятых подряд.
func rtRtUniqueLinkOf(t *testing.T, fixture string, salt int) string {
	t.Helper()
	link := rtLinkText(t, fixture)
	re := regexp.MustCompile(`(@[^:/?#]+):\d+`)
	port := 20000 + int((time.Now().UnixNano()+int64(salt)*7919)%30000)
	if !re.MatchString(link) {
		t.Fatal("в ссылке фикстуры нет порта")
	}
	return re.ReplaceAllString(link, "${1}:"+strconv.Itoa(port))
}

// rtRtLinkAt — ссылка фикстуры с адресом 127.0.0.1:port.
func rtRtLinkAt(t *testing.T, fixture string, port int) string {
	t.Helper()
	link := rtLinkText(t, fixture)
	re := regexp.MustCompile(`@[^:/?#]+:\d+`)
	if !re.MatchString(link) {
		t.Fatal("в ссылке фикстуры нет адреса с портом")
	}
	return re.ReplaceAllString(link, "@127.0.0.1:"+strconv.Itoa(port))
}

// rtRtBlackhole — TCP на loopback, который принимает соединения и не отвечает
// до конца теста. Возвращает порт.
func rtRtBlackhole(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("чёрная дыра: %v", err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().(*net.TCPAddr).Port
}

// rtRtView читает представление маршрутизации.
func (e *rtSrc) rtRtView() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, rtRoutingBase, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtRoutingBase, r.Status, r.Code)
	}
	return r.object(e.t)
}

// rtRtRevision — ревизия черновика из представления маршрутизации.
func (e *rtSrc) rtRtRevision() int64 {
	e.t.Helper()
	rev, _ := e.rtRtView()["revision"].(float64)
	return int64(rev)
}

// rtRtFindGroup ищет группу представления по имени.
func rtRtFindGroup(view map[string]any, name string) map[string]any {
	groups, _ := view["groups"].([]any)
	for _, g := range groups {
		if m, _ := g.(map[string]any); m != nil && m["name"] == name {
			return m
		}
	}
	return nil
}

// rtRtExpect требует от ответа статус и код ошибки.
func (e *rtSrc) rtRtExpect(label string, r rtSrcResp, status int, code string) {
	e.t.Helper()
	if r.Status != status || r.Code != code {
		e.t.Errorf("%s: %d %q, ожидалось %d %q", label, r.Status, r.Code, status, code)
	}
}

// rtRtApplyOK запускает «Применить», ждёт итог и требует успех.
func (e *rtSrc) rtRtApplyOK(label string) {
	e.t.Helper()
	before := e.rtRstStartApply()
	a := e.rtRstWaitApply(before)
	if ok, code := rtRstResult(a); !ok {
		e.t.Fatalf("«Применить» (%s): код %q: %s", label, code, rtRstDescribe(a))
	}
}

// rtRtDraftChanges — число изменений черновика относительно применённого.
func (e *rtSrc) rtRtDraftChanges() int {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	n, _ := r.object(e.t)["draft_changes"].(float64)
	return int(n)
}

// rtRtPurge убирает следы маршрутизации и узлов: удаляет узлы «Моих узлов» и
// секции dns_takeover, groups, rules, probe и routing, затем применяет, если черновик отличается
// от применённого. Состояние стенда после этого — как до первого узла: группы по
// умолчанию создадутся заново вместе с первым источником.
func (e *rtSrc) rtRtPurge() {
	e.t.Helper()
	for id := e.rtRstMyEntry(); id != ""; id = e.rtRstMyEntry() {
		e.rtRstDeleteMyNode(id)
	}
	// Секция dns_takeover ссылается на группу-цель по id: без неё удаление групп
	// оставляет осиротевшую ссылку, и проверка слоя отвергает «Применить» очистки
	// (dns_group_missing), например после прохода Playwright с перехватом.
	for _, section := range []string{"dns_takeover", "groups", "rules", "probe", "routing"} {
		r := e.call(http.MethodPost, "/api/configlayer/draft", map[string]any{"revision": e.revision(), "section": section, "value": nil})
		if r.Status != http.StatusOK {
			e.t.Fatalf("удаление секции №%d черновика: %d %s", len(section), r.Status, r.Code)
		}
	}
	if e.rtRtDraftChanges() > 0 {
		e.rtRtApplyOK("очистка")
	}
	e.rtRstIdle()
}

// rtRtRebuildReleased возвращает панели файлы в состоянии released («Принять
// правку» другого сценария или пользователя): «Пересобрать» по их ключам и итог
// применения. Зовётся после взятия маршрутизации: до него профиля Mihomo среди
// файлов панели нет, а released-профиль слой не пишет, и группы панели в Mihomo
// не попадают. В журнал — только число файлов.
func (e *rtSrc) rtRtRebuildReleased() {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	files, _ := r.object(e.t)["files"].([]any)
	var keys []string
	for _, f := range files {
		m, _ := f.(map[string]any)
		if key, _ := m["key"].(string); key != "" && m["state"] == "released" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return
	}
	e.t.Logf("файлов панели в состоянии released: %d — пересборка", len(keys))
	before := e.rtRstApplyView()
	rb := e.call(http.MethodPost, "/api/configlayer/files/rebuild", map[string]any{"keys": keys})
	if rb.Status != http.StatusOK {
		e.t.Fatalf("files/rebuild: %d %s", rb.Status, rb.Code)
	}
	if ok, code := rtRstResult(e.rtRstWaitApply(before)); !ok {
		e.t.Fatalf("files/rebuild: итог %q", code)
	}
}

// rtRtOwnership читает владение маршрутизацией ядра из GET /api/configlayer/state.
func (e *rtSrc) rtRtOwnership(kernel string) (desired, taken bool) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	routing, _ := r.object(e.t)["routing"].(map[string]any)
	desired, _ = routing["desired"].(bool)
	if k, _ := routing[kernel].(map[string]any); k != nil {
		taken, _ = k["taken"].(bool)
	}
	return desired, taken
}

// rtRtProviderNodes ждёт ответа Clash API (через панель) по провайдеру xcp-my и
// возвращает число его узлов; -1 — провайдера нет за отведённое время.
func (e *rtSrc) rtRtProviderNodes() int {
	e.t.Helper()
	deadline := time.Now().Add(rtRtProviderWait)
	for {
		r := e.call(http.MethodGet, "/api/mihomo/proxy/providers/proxies/xcp-my", nil)
		if r.Status == http.StatusOK {
			var body struct {
				Proxies []json.RawMessage `json:"proxies"`
			}
			if json.Unmarshal(r.Raw, &body) == nil && body.Proxies != nil {
				return len(body.Proxies)
			}
		}
		if time.Now().After(deadline) {
			return -1
		}
		time.Sleep(2 * time.Second)
	}
}

// rtRtExpectTaken сверяет диск и Clash API с состоянием «маршрутизацией управляет
// панель» для активного ядра.
func (e *rtSrc) rtRtExpectTaken(tg routertest.Target, before rtRtDiskState, nodes int) {
	e.t.Helper()
	switch tg.Core {
	case "xray":
		if _, err := os.Stat(filepath.Join(rtRtXrayDir, "99_xcp-routing.json")); err != nil {
			e.t.Errorf("Xray: нет 99_xcp-routing.json после «Применить»: %v", err)
		}
		now := rtRtScanDisk(e.t)
		for name := range before.xray {
			if before.xrayPure[name] {
				if _, still := now.xray[name]; still {
					e.t.Errorf("Xray: «чистый» файл routing №%d остался в configs/", len(name))
				}
			}
		}
	case "mihomo":
		now := rtRtScanDisk(e.t)
		if !strings.HasSuffix(now.mihomoTarget, rtRtPanelProfile) {
			e.t.Errorf("Mihomo: config.yaml не указывает на %s", rtRtPanelProfile)
		}
		if got := e.rtRtProviderNodes(); got != nodes {
			e.t.Errorf("Mihomo: в провайдере xcp-my узлов %d, ожидалось %d", got, nodes)
		}
	}
}

// rtRtExpectReleased сверяет диск с состоянием до взятия.
func (e *rtSrc) rtRtExpectReleased(tg routertest.Target, before rtRtDiskState) {
	e.t.Helper()
	now := rtRtScanDisk(e.t)
	switch tg.Core {
	case "xray":
		for name, sum := range before.xray {
			got, ok := now.xray[name]
			if !ok {
				e.t.Errorf("Xray: файл routing (длина имени %d) не вернулся после «Отпустить»", len(name))
			} else if got != sum {
				e.t.Errorf("Xray: файл routing (длина имени %d) вернулся с другим содержимым", len(name))
			}
		}
	case "mihomo":
		if now.mihomoTarget != before.mihomoTarget {
			e.t.Error("Mihomo: config.yaml не вернулся на прежний профиль")
		}
		if now.mihomoSum != before.mihomoSum {
			e.t.Error("Mihomo: содержимое config.yaml после «Отпустить» отличается от исходного")
		}
	}
}

// rtRtTailGroup — ручная группа сценария только из «Источников» (сверка хвоста).
const rtRtTailGroup = "Хвост"

// rtRtPoll опрашивает cond раз в секунду до wait; true — условие выполнилось.
func rtRtPoll(wait time.Duration, cond func() bool) bool {
	for end := time.Now().Add(wait); ; time.Sleep(time.Second) {
		if cond() {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

// rtRtProfileText — профиль панели Mihomo на диске устройства ("" — не прочитан).
func rtRtProfileText() string {
	data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, rtRtPanelProfile))
	if err != nil {
		return ""
	}
	return string(data)
}

// rtRtProfileGroup — группа name профиля панели Mihomo на диске: use — группа берёт
// узлы провайдеров, proxies — её proxies; found=false — профиль не разобран или
// группы нет.
func rtRtProfileGroup(name string) (use bool, proxies []string, found bool) {
	var doc struct {
		Groups []struct {
			Name    string   `yaml:"name"`
			Use     []string `yaml:"use"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if yaml.Unmarshal([]byte(rtRtProfileText()), &doc) != nil {
		return false, nil, false
	}
	for _, g := range doc.Groups {
		if g.Name == name {
			return len(g.Use) > 0, g.Proxies, true
		}
	}
	return false, nil, false
}

// rtRtHiddenTail — proxies оканчивается служебными DIRECT и REJECT.
func rtRtHiddenTail(proxies []string) bool {
	n := len(proxies)
	return n >= 2 && proxies[n-2] == "DIRECT" && proxies[n-1] == "REJECT"
}

// rtRtMihomoAll — варианты группы в Clash API Mihomo через панель (nil — не прочитаны).
func (e *rtSrc) rtRtMihomoAll(group string) []string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/mihomo/proxy/proxies/"+url.PathEscape(group), nil)
	if r.Status != http.StatusOK {
		return nil
	}
	var body struct {
		All []string `json:"all"`
	}
	_ = json.Unmarshal(r.Raw, &body)
	return body.All
}

// rtRtLimitTail — подтест limit_tail_reconcile: лимит block, сохранённый через
// POST /api/traffic/limits/save без «Применить», сам меняет профиль Mihomo на диске
// (служебный хвост и копии узлов у группы только из «Источников») и группу в Clash
// API (REJECT в all); неприменённая правка черновика в профиль не попадает, ревизия
// и число изменений черновика те же; удаление лимита возвращает use без хвоста.
// Сообщения — только коды, да/нет и числа.
func (e *rtSrc) rtRtLimitTail(name, gid string) {
	t := e.t
	t.Helper()
	if use, proxies, found := rtRtProfileGroup(name); !found || !use || slices.Contains(proxies, "REJECT") {
		t.Fatalf("до лимита: группа в профиле=%t, use=%t, REJECT в proxies=%t", found, use, slices.Contains(proxies, "REJECT"))
	}

	// Правка черновика без «Применить»: сверка хвоста её применять не должна.
	const draftDomain = "tail-draft.invalid"
	rs := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{"revision": e.rtRtRevision(), "rule": map[string]any{
		"enabled": true,
		"conds":   []map[string]any{{"type": "domain", "values": []string{draftDomain}}},
		"target":  map[string]string{"kind": "direct"},
	}})
	if rs.Status != http.StatusOK {
		t.Fatalf("rules/save: %d %s", rs.Status, rs.Code)
	}
	rule, _ := rs.object(t)["rule"].(map[string]any)
	rid, _ := rule["id"].(string)
	rev, changes := e.rtRtRevision(), e.rtRtDraftChanges()
	if changes == 0 {
		t.Error("после rules/save черновик не отличается от применённого")
	}

	ls := e.call(http.MethodPost, "/api/traffic/limits/save", map[string]any{
		"name": "rt хвост", "target_type": "group", "target_id": gid, "limit_bytes": int64(1) << 40,
		"period": "monthly", "threshold": 80, "action": "block", "enabled": true,
	})
	if ls.Status != http.StatusOK {
		t.Fatalf("limits/save: %d %s", ls.Status, ls.Code)
	}
	lid, _ := ls.object(t)["id"].(string)
	if lid == "" {
		t.Fatal("limits/save: в ответе нет id лимита")
	}
	deleted := false
	t.Cleanup(func() {
		if !deleted {
			_ = e.call(http.MethodPost, "/api/traffic/limits/delete", map[string]string{"id": lid})
		}
	})

	// Без «Применить»: сверка арбитра пересобирает профиль, Mihomo его перечитывает.
	if !rtRtPoll(30*time.Second, func() bool {
		use, proxies, found := rtRtProfileGroup(name)
		return found && !use && rtRtHiddenTail(proxies) && slices.Contains(e.rtRtMihomoAll(name), "REJECT")
	}) {
		use, proxies, found := rtRtProfileGroup(name)
		t.Fatalf("сверка хвоста за 30 с: группа в профиле=%t, use=%t, хвост=%t, REJECT в Clash API=%t",
			found, use, rtRtHiddenTail(proxies), slices.Contains(e.rtRtMihomoAll(name), "REJECT"))
	}
	if gotRev, gotChanges := e.rtRtRevision(), e.rtRtDraftChanges(); gotRev != rev || gotChanges != changes {
		t.Errorf("сверка хвоста тронула черновик: ревизия %d → %d, изменений %d → %d", rev, gotRev, changes, gotChanges)
	}
	if strings.Contains(rtRtProfileText(), draftDomain) {
		t.Error("сверка хвоста применила неприменённую правку черновика")
	}

	if r := e.call(http.MethodPost, "/api/traffic/limits/delete", map[string]string{"id": lid}); r.Status != http.StatusOK {
		t.Fatalf("limits/delete: %d %s", r.Status, r.Code)
	}
	deleted = true
	if !rtRtPoll(30*time.Second, func() bool {
		use, proxies, found := rtRtProfileGroup(name)
		return found && use && !slices.Contains(proxies, "REJECT")
	}) {
		use, proxies, _ := rtRtProfileGroup(name)
		t.Errorf("удаление лимита за 30 с: use=%t, REJECT в proxies=%t", use, slices.Contains(proxies, "REJECT"))
	}

	// Фоновая сверка не должна столкнуться со следующим «Применить» сценария.
	e.rtRstIdle()
	if r := e.call(http.MethodPost, rtRoutingBase+"/rules/delete", map[string]any{"revision": e.rtRtRevision(), "id": rid}); r.Status != http.StatusOK {
		t.Errorf("rules/delete: %d %s", r.Status, r.Code)
	}
}

// Трасса: «Мои узлы» → группы по умолчанию → группа → take → «Применить» →
// маршрутизация активного ядра у панели → release → «Применить» → всё как было.
// Метка ядра: тест читает активное ядро и меняет его маршрутизацию.
func TestRouterRoutingAPIFlow(t *testing.T) {
	routertest.Slow(t, "серия «Применить» по API маршрутизации с перезапуском ядра")
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.freshDraft()
	e.rtRstIdle()
	e.rtRtPurge()
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	before := rtRtScanDisk(t)
	taken := false
	// proxyID и winID — окно расписания сценария, которое ещё не удалено.
	var proxyID, winID string

	// Маршрутизация и следы сценария убираются при любом исходе: окно расписания,
	// release → «Применить», затем очистка узлов и секций. Cleanup регистрируется
	// после freshDraft и потому идёт раньше сброса черновика.
	t.Cleanup(func() {
		c := e.with(t)
		if winID != "" {
			_ = c.call(http.MethodPost, rtRoutingBase+"/schedule/delete", map[string]string{"group": proxyID, "id": winID})
		}
		if taken {
			r := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
			if r.Status != http.StatusOK {
				t.Errorf("очистка: release: %d %s", r.Status, r.Code)
				return
			}
			c.rtRtApplyOK("очистка: возврат")
		}
		c.rtRtPurge()
	})

	// «Мои узлы»: одна ссылка с уникальным портом.
	link := rtRtUniqueLink(t)
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": link})
	if pv.Status != http.StatusOK {
		t.Fatalf("sources/preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(t)["token"].(string)
	if add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token}); add.Status != http.StatusOK {
		t.Fatalf("sources/add: %d %s", add.Status, add.Code)
	}

	// Представление: группы по умолчанию появились вместе с первым источником.
	view := e.rtRtView()
	if hs, _ := view["has_sources"].(bool); !hs {
		t.Error("представление: has_sources=false после добавления узла")
	}
	rtAssertContractKeys(t, contract, view, "RoutingView", "routing")
	if done, _ := view["defaults_done"].(bool); !done {
		t.Error("представление: defaults_done=false после добавления первого источника")
	}
	for _, name := range []string{"Авто", "Прокси"} {
		if rtRtFindGroup(view, name) == nil {
			t.Errorf("представление: нет группы по умолчанию %q", name)
		}
	}

	// groups/save: новая ручная группа получает идентификатор из 8 hex.
	save := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group": map[string]any{
			"name": "Проверка", "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{{"kind": "direct"}},
		},
	})
	if save.Status != http.StatusOK {
		t.Fatalf("groups/save: %d %s", save.Status, save.Code)
	}
	saved, _ := save.object(t)["group"].(map[string]any)
	if id, _ := saved["id"].(string); len(id) != 8 {
		t.Errorf("groups/save: идентификатор группы длиной %d, ожидалось 8", len(id))
	}
	if ev, _ := save.object(t)["event"].(map[string]any); ev == nil || ev["draft_revision"] == nil {
		t.Errorf("groups/save: в ответе нет event.draft_revision")
	}
	if rtRtFindGroup(e.rtRtView(), "Проверка") == nil {
		t.Error("представление: сохранённой группы нет")
	}
	gid, _ := saved["id"].(string)
	manual := func(name string) map[string]any {
		return map[string]any{
			"name": name, "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{{"kind": "direct"}},
		}
	}

	// Коды ошибок записи группы.
	e.rtRtExpect("groups/save: то же имя", e.call(http.MethodPost, rtRoutingBase+"/groups/save",
		map[string]any{"revision": e.rtRtRevision(), "group": manual("проверка")}), http.StatusConflict, "group_name_taken")
	bad := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{"revision": e.rtRtRevision(), "group": manual("")})
	e.rtRtExpect("groups/save: пустое имя", bad, http.StatusBadRequest, "invalid_input")
	if bad.Params["field"] != "name" || bad.Params["reason"] != "name_empty" {
		t.Errorf("groups/save: пустое имя: params %v, ожидались field=name reason=name_empty", bad.Params)
	}
	e.rtRtExpect("groups/save: старая ревизия", e.call(http.MethodPost, rtRoutingBase+"/groups/save",
		map[string]any{"revision": 0, "group": manual("Другая")}), http.StatusConflict, "draft_conflict")
	ghost := manual("Другая")
	ghost["id"] = "ffffffff"
	e.rtRtExpect("groups/save: неизвестная группа", e.call(http.MethodPost, rtRoutingBase+"/groups/save",
		map[string]any{"revision": e.rtRtRevision(), "group": ghost}), http.StatusNotFound, "group_not_found")

	// groups/preview: ответ по установленным ядрам, группа не сохраняется.
	gp := e.call(http.MethodPost, rtRoutingBase+"/groups/preview", map[string]any{"group": map[string]any{
		"name": "Предпросмотр", "mode": "auto", "sources": map[string]any{"all": true},
	}})
	if gp.Status != http.StatusOK {
		t.Errorf("groups/preview: %d %s", gp.Status, gp.Code)
	} else {
		rtAssertContractKeys(t, contract, gp.object(t), "GroupPreview", "groups/preview")
	}
	if rtRtFindGroup(e.rtRtView(), "Предпросмотр") != nil {
		t.Error("groups/preview сохранил группу")
	}

	// Правила: сохранить, выключить, включить, переставить, удалить; коды ошибок.
	ruleBody := func(domain string) map[string]any {
		return map[string]any{
			"enabled": true,
			"conds":   []map[string]any{{"type": "domain", "values": []string{domain}}},
			"target":  map[string]string{"kind": "group", "id": gid},
		}
	}
	rs := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{"revision": e.rtRtRevision(), "rule": ruleBody("example.com")})
	if rs.Status != http.StatusOK {
		t.Fatalf("rules/save: %d %s", rs.Status, rs.Code)
	}
	savedRule, _ := rs.object(t)["rule"].(map[string]any)
	rid, _ := savedRule["id"].(string)
	if len(rid) != 8 {
		t.Fatalf("rules/save: идентификатор правила длиной %d, ожидалось 8", len(rid))
	}
	rs2 := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{"revision": e.rtRtRevision(), "rule": ruleBody("example.org")})
	if rs2.Status != http.StatusOK {
		t.Fatalf("rules/save (второе): %d %s", rs2.Status, rs2.Code)
	}
	secondRule, _ := rs2.object(t)["rule"].(map[string]any)
	rid2, _ := secondRule["id"].(string)
	for _, enabled := range []bool{false, true} {
		r := e.call(http.MethodPost, rtRoutingBase+"/rules/enable", map[string]any{"revision": e.rtRtRevision(), "id": rid, "enabled": enabled})
		if r.Status != http.StatusOK {
			t.Errorf("rules/enable enabled=%v: %d %s", enabled, r.Status, r.Code)
		}
	}
	if r := e.call(http.MethodPost, rtRoutingBase+"/rules/move", map[string]any{"revision": e.rtRtRevision(), "id": rid2, "to": 0}); r.Status != http.StatusOK {
		t.Errorf("rules/move: %d %s", r.Status, r.Code)
	}
	if rules, _ := e.rtRtView()["rules"].([]any); len(rules) < 2 || rules[0].(map[string]any)["id"] != rid2 {
		t.Error("rules/move: второе правило не стало первым")
	}
	mv := e.call(http.MethodPost, rtRoutingBase+"/rules/move", map[string]any{"revision": e.rtRtRevision(), "id": rid, "to": 99})
	e.rtRtExpect("rules/move: вне диапазона", mv, http.StatusBadRequest, "invalid_input")
	if mv.Params["reason"] != "out_of_range" {
		t.Errorf("rules/move: вне диапазона: reason %q", mv.Params["reason"])
	}
	e.rtRtExpect("rules/save: без условий", e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{
		"revision": e.rtRtRevision(),
		"rule":     map[string]any{"enabled": true, "conds": []any{}, "target": map[string]string{"kind": "direct"}},
	}), http.StatusBadRequest, "invalid_input")
	e.rtRtExpect("rules/delete: неизвестное правило", e.call(http.MethodPost, rtRoutingBase+"/rules/delete",
		map[string]any{"revision": e.rtRtRevision(), "id": "ffffffff"}), http.StatusNotFound, "rule_not_found")
	e.rtRtExpect("rules/enable: неизвестное правило", e.call(http.MethodPost, rtRoutingBase+"/rules/enable",
		map[string]any{"revision": e.rtRtRevision(), "id": "ffffffff", "enabled": true}), http.StatusNotFound, "rule_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/rules/delete", map[string]any{"revision": e.rtRtRevision(), "id": rid2}); r.Status != http.StatusOK {
		t.Errorf("rules/delete: %d %s", r.Status, r.Code)
	}

	// probe: параметры из представления сохраняются, недопустимые отклоняются.
	probe, _ := e.rtRtView()["probe"].(map[string]any)
	if r := e.call(http.MethodPost, rtRoutingBase+"/probe", map[string]any{"revision": e.rtRtRevision(), "probe": probe}); r.Status != http.StatusOK {
		t.Errorf("probe: %d %s", r.Status, r.Code)
	}
	badProbe := map[string]any{"url": "ftp://x", "interval_s": 30, "timeout_s": 5}
	e.rtRtExpect("probe: недопустимый адрес", e.call(http.MethodPost, rtRoutingBase+"/probe",
		map[string]any{"revision": e.rtRtRevision(), "probe": badProbe}), http.StatusBadRequest, "invalid_input")

	// impact и каскад удаления группы: на группу ссылаются правило и «Остальное».
	imp := e.call(http.MethodPost, rtRoutingBase+"/impact", map[string]string{"kind": "group", "id": gid})
	if imp.Status != http.StatusOK {
		t.Errorf("impact: %d %s", imp.Status, imp.Code)
	} else {
		rtAssertContractKeys(t, contract, imp.object(t), "Impact", "impact")
		if refs, _ := imp.object(t)["rules"].([]any); len(refs) != 1 {
			t.Errorf("impact: правил с целью на группу %d, ожидалось 1", len(refs))
		}
	}
	e.rtRtExpect("impact: неизвестный вид", e.call(http.MethodPost, rtRoutingBase+"/impact",
		map[string]string{"kind": "bogus", "id": gid}), http.StatusBadRequest, "invalid_input")
	e.rtRtExpect("impact: неизвестное правило", e.call(http.MethodPost, rtRoutingBase+"/impact",
		map[string]string{"kind": "rule", "id": "ffffffff"}), http.StatusNotFound, "rule_not_found")
	e.rtRtExpect("impact: неизвестный источник", e.call(http.MethodPost, rtRoutingBase+"/impact",
		map[string]string{"kind": "source", "id": "ffffffff"}), http.StatusNotFound, "source_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/final", map[string]any{"revision": e.rtRtRevision(), "target": map[string]string{"kind": "group", "id": gid}}); r.Status != http.StatusOK {
		t.Fatalf("final: %d %s", r.Status, r.Code)
	}
	ref := e.call(http.MethodPost, rtRoutingBase+"/groups/delete", map[string]any{"revision": e.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": ""}})
	e.rtRtExpect("groups/delete: есть ссылки", ref, http.StatusConflict, "group_referenced")
	if impact, _ := ref.object(t)["impact"].(map[string]any); impact == nil {
		t.Error("groups/delete: в ответе group_referenced нет impact")
	} else {
		rtAssertContractKeys(t, contract, impact, "Impact", "groups/delete.impact")
		if refs, _ := impact["rules"].([]any); len(refs) != 1 {
			t.Errorf("groups/delete: в impact правил %d, ожидалось 1", len(refs))
		}
	}
	e.rtRtExpect("groups/delete: «Остальное» без замены", e.call(http.MethodPost, rtRoutingBase+"/groups/delete",
		map[string]any{"revision": e.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": "drop"}}), http.StatusBadRequest, "final_requires_redirect")
	e.rtRtExpect("groups/delete: группа не найдена", e.call(http.MethodPost, rtRoutingBase+"/groups/delete",
		map[string]any{"revision": e.rtRtRevision(), "id": "ffffffff", "cascade": map[string]any{"mode": ""}}), http.StatusNotFound, "group_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/groups/delete", map[string]any{
		"revision": e.rtRtRevision(), "id": gid, "cascade": map[string]any{"mode": "redirect", "to": map[string]string{"kind": "direct"}},
	}); r.Status != http.StatusOK {
		t.Fatalf("groups/delete с заменой: %d %s", r.Status, r.Code)
	}
	view = e.rtRtView()
	if rtRtFindGroup(view, "Проверка") != nil {
		t.Error("groups/delete: группа осталась в представлении")
	}
	if fin, _ := view["final"].(map[string]any); fin["kind"] != "direct" {
		t.Errorf("groups/delete: «Остальное» после замены: %v", fin["kind"])
	}
	if rules, _ := view["rules"].([]any); len(rules) != 1 || rules[0].(map[string]any)["target"].(map[string]any)["kind"] != "direct" {
		t.Error("groups/delete: правило не направлено на DIRECT")
	}

	// Ручная группа «Хвост» только из «Источников» (без участников): её применяет
	// то же «взятие»; на проходе Mihomo на ней проверяется сверка служебного хвоста.
	ts := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group":    map[string]any{"name": rtRtTailGroup, "mode": "manual", "sources": map[string]any{"all": true}},
	})
	if ts.Status != http.StatusOK {
		t.Fatalf("groups/save «Хвост»: %d %s", ts.Status, ts.Code)
	}
	tailGroup, _ := ts.object(t)["group"].(map[string]any)
	tailID, _ := tailGroup["id"].(string)

	// Предпросмотр взятия и reclaim с неизвестным ядром.
	tp := e.call(http.MethodGet, rtRoutingBase+"/takeover", nil)
	if tp.Status != http.StatusOK {
		t.Errorf("takeover: %d %s", tp.Status, tp.Code)
	} else {
		rtAssertContractKeys(t, contract, tp.object(t), "TakeoverPreview", "takeover")
		if m, _ := tp.object(t)[tg.Core].(map[string]any); m == nil {
			t.Errorf("takeover: для активного ядра %s нет предпросмотра", tg.Core)
		}
	}
	e.rtRtExpect("reclaim: неизвестное ядро", e.call(http.MethodPost, rtRoutingBase+"/reclaim",
		map[string]string{"kernel": "bogus"}), http.StatusBadRequest, "invalid_input")

	// take: желание управлять маршрутизацией в черновике, затем «Применить».
	tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()})
	if tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	if on, _ := e.rtRtView()["desired"].(bool); !on {
		t.Error("представление: desired=false после take")
	}
	taken = true
	e.rtRtApplyOK("взятие")
	if desired, got := e.rtRtOwnership(tg.Core); !desired || !got {
		t.Errorf("снимок слоя: желаемое=%v, взято %s=%v", desired, tg.Core, got)
	}
	if st := e.call(http.MethodGet, "/api/configlayer/state", nil); st.Status == http.StatusOK {
		snap, _ := st.object(t)["routing"].(map[string]any)
		rtAssertContractKeys(t, contract, snap, "RoutingSnapshot", "state.routing")
	}
	e.rtRtExpectTaken(tg, before, 1)
	// Взято и применено: расхождений нет, возвращать нечего.
	e.rtRtExpect("reclaim: нет расхождений", e.call(http.MethodPost, rtRoutingBase+"/reclaim",
		map[string]string{"kernel": tg.Core}), http.StatusConflict, "nothing_to_reclaim")

	// Окна расписания на живом ядре (D-10, D-11): окно ручной группы «Прокси» на
	// её узел — 200 и ID; пересекающееся — 409 schedule_overlap с ID первого окна;
	// окно группы «авто» — 409 group_not_applied; удаление неизвестного окна — 404.
	// Окна — через 2 ч от часов роутера: тест не ждёт, пока арбитр сменит узел.
	view = e.rtRtView()
	proxyGroup := rtRtFindGroup(view, "Прокси")
	if proxyGroup == nil {
		t.Fatal("представление: нет группы «Прокси» после взятия")
	}
	proxyID, _ = proxyGroup["id"].(string)
	proxyNodes, _ := rtRtNodeOptions(proxyGroup)
	if len(proxyNodes) == 0 {
		t.Fatal("представление: у группы «Прокси» нет узлов среди вариантов")
	}
	now := time.Now()
	window := func(from, to time.Duration) map[string]any {
		return map[string]any{
			"days": []int{0, 1, 2, 3, 4, 5, 6}, "from": now.Add(from).Format("15:04"), "to": now.Add(to).Format("15:04"),
			"member": proxyNodes[0], "enabled": true,
		}
	}
	ws := e.call(http.MethodPost, rtRoutingBase+"/schedule/save", map[string]any{"group": proxyID, "window": window(2*time.Hour, 3*time.Hour)})
	if ws.Status != http.StatusOK {
		t.Fatalf("schedule/save: %d %s", ws.Status, ws.Code)
	}
	winID, _ = ws.object(t)["id"].(string)
	if winID == "" {
		t.Fatal("schedule/save: в ответе нет id окна")
	}
	ov := e.call(http.MethodPost, rtRoutingBase+"/schedule/save", map[string]any{"group": proxyID, "window": window(2*time.Hour+30*time.Minute, 4*time.Hour)})
	e.rtRtExpect("schedule/save: пересечение", ov, http.StatusConflict, "schedule_overlap")
	if ov.Params["window"] != winID {
		t.Errorf("schedule/save: пересечение: params.window совпал с первым окном=%t", ov.Params["window"] == winID)
	}
	if auto := rtRtFindGroup(view, "Авто"); auto != nil {
		autoID, _ := auto["id"].(string)
		e.rtRtExpect("schedule/save: группа «авто»", e.call(http.MethodPost, rtRoutingBase+"/schedule/save",
			map[string]any{"group": autoID, "window": window(5*time.Hour, 6*time.Hour)}), http.StatusConflict, "group_not_applied")
	}
	e.rtRtExpect("schedule/delete: неизвестное окно", e.call(http.MethodPost, rtRoutingBase+"/schedule/delete",
		map[string]string{"group": proxyID, "id": "ffffffff"}), http.StatusNotFound, "window_not_found")
	if r := e.call(http.MethodPost, rtRoutingBase+"/schedule/delete", map[string]string{"group": proxyID, "id": winID}); r.Status != http.StatusOK {
		t.Errorf("schedule/delete: %d %s", r.Status, r.Code)
	} else {
		winID = ""
	}

	// Сверка служебного хвоста через HTTP (D-13, 152-09) — путь пользователя:
	// сохранение лимита без «Применить». Хвост — форма профиля Mihomo, на проходе
	// Xray подтест не вызывается.
	if tg.Core == "mihomo" {
		t.Run("limit_tail_reconcile", func(t *testing.T) { e.with(t).rtRtLimitTail(rtRtTailGroup, tailID) })
	}

	// release → «Применить»: файлы ядра как были.
	rl := e.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": e.rtRtRevision()})
	if rl.Status != http.StatusOK {
		t.Fatalf("release: %d %s", rl.Status, rl.Code)
	}
	e.rtRtApplyOK("возврат")
	taken = false
	if _, got := e.rtRtOwnership(tg.Core); got {
		t.Errorf("снимок слоя: %s остаётся взятым после «Отпустить»", tg.Core)
	}
	e.rtRtExpectReleased(tg, before)
}

// --- выбор участника ----------------------------------------------------------

// rtRtXrayOutbounds читает исходящие всех файлов каталога конфигов Xray: тег → объект.
func rtRtXrayOutbounds(t *testing.T) map[string]map[string]any {
	t.Helper()
	entries, err := os.ReadDir(rtRtXrayDir)
	if err != nil {
		t.Fatalf("каталог конфигов Xray не читается: %v", err)
	}
	out := map[string]map[string]any{}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rtRtXrayDir, ent.Name()))
		if err != nil {
			continue
		}
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		for _, ob := range doc.Outbounds {
			if tag, _ := ob["tag"].(string); tag != "" {
				out[tag] = ob
			}
		}
	}
	return out
}

// rtRtWithoutTag — исходящий без тега: сравнение копии узла с самим узлом.
func rtRtWithoutTag(ob map[string]any) map[string]any {
	cp := make(map[string]any, len(ob))
	for k, v := range ob {
		if k != "tag" {
			cp[k] = v
		}
	}
	return cp
}

// rtRtGroupOutbound — исходящий группы xcp-g-<id> без тега и признак «найден».
func rtRtGroupOutbound(t *testing.T, gid string) (map[string]any, bool) {
	t.Helper()
	ob, ok := rtRtXrayOutbounds(t)["xcp-g-"+gid]
	if !ok {
		return nil, false
	}
	return rtRtWithoutTag(ob), true
}

// rtRtMatchesNode сообщает, что исходящий (без тега) совпадает с исходящим одного
// из узлов «Моих узлов».
func rtRtMatchesNode(t *testing.T, ob map[string]any) bool {
	t.Helper()
	for tag, cand := range rtRtXrayOutbounds(t) {
		if strings.HasPrefix(tag, "xcp-my-") && reflect.DeepEqual(rtRtWithoutTag(cand), ob) {
			return true
		}
	}
	return false
}

// rtRtMihomoNow читает выбранного участника группы Mihomo через панель (Clash API).
func (e *rtSrc) rtRtMihomoNow(group string) string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/mihomo/proxy/proxies/"+url.PathEscape(group), nil)
	if r.Status != http.StatusOK {
		return ""
	}
	var body struct {
		Now string `json:"now"`
	}
	_ = json.Unmarshal(r.Raw, &body)
	return body.Now
}

// rtRtWaitNow ждёт, пока выбранный участник группы Mihomo станет want.
func (e *rtSrc) rtRtWaitNow(group, want string) string {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		now := e.rtRtMihomoNow(group)
		if now == want || time.Now().After(deadline) {
			return now
		}
		time.Sleep(time.Second)
	}
}

// rtRtNodeOptions — варианты участника группы представления, которые являются
// узлами: список member и подпись каждого.
func rtRtNodeOptions(group map[string]any) (members []string, labels map[string]string) {
	labels = map[string]string{}
	opts, _ := group["member_options"].([]any)
	for _, o := range opts {
		m, _ := o.(map[string]any)
		member, _ := m["member"].(string)
		if !strings.HasPrefix(member, "node:") {
			continue
		}
		members = append(members, member)
		labels[member], _ = m["label"].(string)
	}
	return members, labels
}

// rtRtSelectBusyWait — selectBusyWait сервиса маршрутизации (2 с): столько выбор
// Xray ждёт замок применения перед apply_busy. Отказ select_busy связанной группы
// приходит до любого ожидания замка, поэтому быстрее этого срока.
const rtRtSelectBusyWait = 2 * time.Second

// rtRtSnapPoll — шаг опроса вида «Прокси» во время параллельных выборов.
const rtRtSnapPoll = 25 * time.Millisecond

// rtRtMihomoSelectBudget — граница медианы total строк этапов выбора на ядре mihomo
// (одиночный выбор и выборы барьера D-17), объявленная планом 150-19 до замера и
// по его итогу не пересматриваемая. При запущенном Mihomo и остановленном Xray
// ответ приходит после Clash API, проход файла Xray идёт следом, поэтому total —
// время до ответа. 1000 мс — верхняя граница θ из правил 150-17 («этап заметен
// пользователю»). Медиана, а не каждый выбор: PUT Clash API сам пишет cache.db
// Mihomo на флешку (store-selected не выключается), и редкая остановка носителя
// одной строки не должна делать тест плавающим; медиана четырёх строк одну
// остановку в итог не пропускает.
const rtRtMihomoSelectBudget = 1000 * time.Millisecond

// rtRtMihomoHTTPBudget — граница медианы http тех же выборов на ядре mihomo:
// rtRtMihomoSelectBudget плюс 1000 мс допуска времени вне Select из правила 150-16.
const rtRtMihomoHTTPBudget = 2000 * time.Millisecond

// rtRtMihomoPanelTheta — θM, порог правил плана 150-38 для составляющих времени
// выбора на ядре mihomo (10 % rtRtMihomoSelectBudget): объявлен до замера и в этом
// тесте не проверка — строка mihomo-select-parts: только печатает его рядом с
// медианами (P1: медиана clash_plan ≥ θM — цель раннего PUT из каталога Select;
// медиана clash_wait, model или pre ≥ θM — todo с цифрами; clash_put — время
// самого Mihomo, без правила).
const rtRtMihomoPanelTheta = 100 * time.Millisecond

// rtRtMihomoParts — составляющие времени строк этапов выбора на ядре mihomo (те
// же строки, что mihomoTotals): model, pre, части раннего PUT и накладные расходы
// панели panel = total − clash_put.
type rtRtMihomoParts struct {
	model, pre, wait, plan, put, panel []int64
}

// add берёт составляющие из строки этапов st.
func (p *rtRtMihomoParts) add(st rtRtStage) {
	p.model = append(p.model, st.num("model"))
	p.pre = append(p.pre, st.num("pre"))
	p.wait = append(p.wait, st.num("clash_wait"))
	p.plan = append(p.plan, st.num("clash_plan"))
	p.put = append(p.put, st.num("clash_put"))
	p.panel = append(p.panel, st.num("total")-st.num("clash_put"))
}

// rtRtStagesWait — срок ожидания строк этапов выбора: на ядре xray строка пишется до
// ответа (5 с на запись журнала), на ядре mihomo — в конце фонового прохода файла
// Xray после ответа (до 90 с).
func rtRtStagesWait(core string) time.Duration {
	if core == "mihomo" {
		return 90 * time.Second
	}
	return 5 * time.Second
}

// rtRtMedian — медиана выборки (пустая — 0).
func rtRtMedian(vals []int64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]int64(nil), vals...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return float64(s[n/2-1]+s[n/2]) / 2
}

// rtRtRunLiveNum — целое значение ключа строки «configlayer: RunLive …» (нет — -1).
func rtRtRunLiveNum(line, key string) int64 {
	for _, f := range strings.Fields(line) {
		if k, v, ok := strings.Cut(f, "="); ok && k == key {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return n
			}
		}
	}
	return -1
}

// rtRtLogPath — журнал панели на устройстве.
func rtRtLogPath() string {
	if p := config.Default().XCPLogPath; p != "" {
		return p
	}
	return "/opt/var/log/xcp.log"
}

// rtRtLogOffset — текущий размер журнала панели: начало окна строк выбора.
func rtRtLogOffset(t *testing.T) int64 {
	t.Helper()
	st, err := os.Stat(rtRtLogPath())
	if err != nil {
		t.Fatalf("журнал панели: %v", err)
	}
	return st.Size()
}

// rtRtStage — строка этапов выбора Xray («[routing] выбор Xray: этапы group=…»):
// ID группы, пары ключ=значение без group и сами пары строкой. Строка несёт только
// ID группы, числа и код исхода.
type rtRtStage struct {
	group string
	kv    map[string]string
	pairs string
}

// num — целое значение ключа строки этапов (нет ключа — 0).
func (s rtRtStage) num(key string) int64 {
	n, _ := strconv.ParseInt(s.kv[key], 10, 64)
	return n
}

var (
	rtRtStageRe   = regexp.MustCompile(`выбор Xray: этапы group=(\S+)((?: \S+=\S*)*)`)
	rtRtRunLiveRe = regexp.MustCompile(`configlayer: RunLive (trigger=select(?: \S+=\S*)*)`)
)

// rtRtSelectOK — успешные исходы выбора в поле outcome строки этапов.
var rtRtSelectOK = map[string]bool{"applied": true, "restarted": true, "pending_stopped": true}

// rtRtLogLines — строки журнала панели с offset; журнал, ротированный за время
// окна, читается с начала нового файла.
func rtRtLogLines(offset int64) []string {
	text, err := rtReadLogFrom(rtRtLogPath(), offset)
	if err != nil {
		text, _ = rtReadLogFrom(rtRtLogPath(), 0)
	}
	return strings.Split(text, "\n")
}

// rtRtCountLines — число строк lines с подстрокой phrase и ID любой группы из
// groups. В журнал теста попадает только число.
func rtRtCountLines(lines, groups []string, phrase string) int {
	n := 0
	for _, l := range lines {
		if !strings.Contains(l, phrase) {
			continue
		}
		for _, g := range groups {
			if strings.Contains(l, g) {
				n++
				break
			}
		}
	}
	return n
}

// rtRtParseStages разбирает окно журнала: строки этапов выбора Xray по порядку и
// пары строк «configlayer: RunLive trigger=select».
func rtRtParseStages(text string) (stages []rtRtStage, runlive []string) {
	for _, m := range rtRtStageRe.FindAllStringSubmatch(text, -1) {
		st := rtRtStage{group: m[1], kv: map[string]string{}, pairs: strings.TrimSpace(m[2])}
		for _, f := range strings.Fields(m[2]) {
			if k, v, ok := strings.Cut(f, "="); ok {
				st.kv[k] = v
			}
		}
		stages = append(stages, st)
	}
	for _, m := range rtRtRunLiveRe.FindAllStringSubmatch(text, -1) {
		runlive = append(runlive, m[1])
	}
	return stages, runlive
}

// rtRtWaitStages ждёт не дольше wait (rtRtStagesWait) в окне журнала с offset строк
// этапов групп groups (у каждой — последняя строка окна) и не меньше minRunLive
// строк RunLive выбора. Журнал, ротированный за время окна, читается с начала
// нового файла.
func rtRtWaitStages(offset int64, groups []string, minRunLive int, wait time.Duration) (map[string]rtRtStage, []string, bool) {
	var (
		byGroup map[string]rtRtStage
		runlive []string
	)
	ok, _ := routertest.Poll(wait, 200*time.Millisecond, func() (bool, string) {
		text, err := rtReadLogFrom(rtRtLogPath(), offset)
		if err != nil {
			text, _ = rtReadLogFrom(rtRtLogPath(), 0)
		}
		stages, rl := rtRtParseStages(text)
		byGroup, runlive = map[string]rtRtStage{}, rl
		for _, st := range stages {
			byGroup[st.group] = st
		}
		for _, g := range groups {
			if _, ok := byGroup[g]; !ok {
				return false, ""
			}
		}
		return len(runlive) >= minRunLive, ""
	})
	return byGroup, runlive, ok
}

// rtRtSelRes — ответ выбора, отправленного из горутины: только данные, разбор и
// вердикт — в горутине теста.
type rtRtSelRes struct {
	job        int
	status     int
	raw        []byte
	err        error
	sent, done time.Time
}

// rtRtSelectAsync отправляет POST select после закрытия start и кладёт ответ в out.
// Вызывается в горутине: без t.* и без e.call (они зовут t.Fatalf).
func (e *rtSrc) rtRtSelectAsync(job int, gid, member string, start <-chan struct{}, out chan<- rtRtSelRes) {
	<-start
	sent := time.Now()
	status, raw, err := e.p.CallSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": member})
	out <- rtRtSelRes{job: job, status: status, raw: raw, err: err, sent: sent, done: time.Now()}
}

// rtRtSnap — снимок вида «Прокси» во время выборов: признаки групп по ID.
type rtRtSnap struct {
	at        time.Time
	dur       time.Duration
	switching map[string]bool
	unconf    map[string]bool
}

// rtRtTakeSnap читает вид «Прокси» без t.Fatalf (ошибка — снимка нет).
func (e *rtSrc) rtRtTakeSnap() (rtRtSnap, bool) {
	start := time.Now()
	status, raw, err := e.p.Call(http.MethodGet, "/api/configlayer/proxies", nil)
	s := rtRtSnap{at: start, dur: time.Since(start), switching: map[string]bool{}, unconf: map[string]bool{}}
	if err != nil || status != http.StatusOK {
		return s, false
	}
	var body struct {
		Data struct {
			Groups []struct {
				ID          string `json:"id"`
				Switching   bool   `json:"switching"`
				Unconfirmed bool   `json:"unconfirmed"`
			} `json:"groups"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return s, false
	}
	for _, g := range body.Data.Groups {
		s.switching[g.ID], s.unconf[g.ID] = g.Switching, g.Unconfirmed
	}
	return s, true
}

// rtRtGroupNow читает current, switching и unconfirmed группы вида «Прокси» без
// t.Fatalf (ошибка или нет группы — ok=false).
func (e *rtSrc) rtRtGroupNow(gid string) (current string, switching, unconfirmed, ok bool) {
	status, raw, err := e.p.Call(http.MethodGet, "/api/configlayer/proxies", nil)
	if err != nil || status != http.StatusOK {
		return "", false, false, false
	}
	var body struct {
		Data struct {
			Groups []struct {
				ID          string `json:"id"`
				Current     string `json:"current"`
				Switching   bool   `json:"switching"`
				Unconfirmed bool   `json:"unconfirmed"`
			} `json:"groups"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return "", false, false, false
	}
	for _, g := range body.Data.Groups {
		if g.ID == gid {
			return g.Current, g.Switching, g.Unconfirmed, true
		}
	}
	return "", false, false, false
}

// rtRtProxiesEvents открывает поток событий слоя (вторая вкладка) и отдаёт время
// прихода каждого события proxies. Горутина чтения — без t.*; поток закрывает
// отмена контекста (stop и t.Cleanup).
func (e *rtSrc) rtRtProxiesEvents(t *testing.T) (<-chan time.Time, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	resp, err := e.p.Stream(ctx, "/api/configlayer/events")
	if err != nil {
		cancel()
		t.Fatalf("поток событий слоя не открылся: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		t.Fatalf("поток событий слоя: http %d", resp.StatusCode)
	}
	out := make(chan time.Time, 256)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "event: proxies" {
				select {
				case out <- time.Now():
				default:
				}
			}
		}
	}()
	return out, cancel
}

// rtRtSelectNotices — уведомления routing_select: группы gid в снимке слоя:
// идентификаторы и параметры code и reason (без имён узлов).
func (e *rtSrc) rtRtSelectNotices(gid string) (ids []string, code, reason string) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	list, _ := r.object(e.t)["notices"].([]any)
	for _, it := range list {
		n, _ := it.(map[string]any)
		id, _ := n["id"].(string)
		if !strings.HasPrefix(id, "routing_select:"+gid+":") {
			continue
		}
		ids = append(ids, id)
		params, _ := n["params"].(map[string]any)
		code, _ = params["code"].(string)
		reason, _ = params["reason"].(string)
	}
	return ids, code, reason
}

// rtRtWaitSettled ждёт конца переключения группы в виде «Прокси» (switching=false)
// не дольше 90 с: на ядре mihomo фоновый проход файла Xray идёт после ответа.
func (e *rtSrc) rtRtWaitSettled(gid string) bool {
	ok, _ := routertest.Poll(90*time.Second, 200*time.Millisecond, func() (bool, string) {
		_, sw, _, got := e.rtRtGroupNow(gid)
		return got && !sw, ""
	})
	return ok
}

// rtRtDriftFile — управляемый панелью файл Xray в каталоге конфигов, кроме
// xcp-groups, в состоянии ok: его правка — дрейф, на котором фоновый проход выбора
// отказывает. Пусто — такого файла нет.
func (e *rtSrc) rtRtDriftFile() string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	files, _ := r.object(e.t)["files"].([]any)
	for _, f := range files {
		m, _ := f.(map[string]any)
		p, _ := m["path"].(string)
		if m["kernel"] != "xray" || m["owner"] != "panel" || m["state"] != "ok" || filepath.Dir(p) != rtRtXrayDir ||
			strings.Contains(filepath.Base(p), "xcp-groups") {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// rtRtBackgroundFail — фоновый отказ быстрого пути на ядре mihomo (план 150-19,
// D-11): дрейф управляемого файла Xray (не xcp-groups) при остановленном Xray.
// Два выбора подряд отвечают 200 с kernels.mihomo=applied, после каждого current
// и now группы возвращаются к прежнему узлу, у группы ровно одно уведомление
// routing_select:<gid>:, идентификатор второго отличается от первого, маркера нет.
// После возврата файла тот же выбор применяется, уведомление снято. В журнал —
// только ID группы, коды и числа.
func (e *rtSrc) rtRtBackgroundFail(gid, groupName string, nodeMembers []string, labels map[string]string) {
	t := e.t
	t.Helper()
	cur, _, _, ok := e.rtRtGroupNow(gid)
	if !ok || cur == "" {
		t.Fatalf("фоновый отказ: нет группы %s в виде «Прокси»", gid)
	}
	other := ""
	for _, m := range nodeMembers {
		if labels[m] != labels[cur] {
			other = m
			break
		}
	}
	if other == "" {
		t.Fatal("фоновый отказ: нет узла с подписью, отличной от текущей")
	}
	path := e.rtRtDriftFile()
	if path == "" {
		t.Fatal("фоновый отказ: нет управляемого файла Xray вне xcp-groups в состоянии ok")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("фоновый отказ: файл Xray: %v", err)
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("фоновый отказ: файл Xray не читается: %v", err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		if err := os.WriteFile(path, orig, info.Mode().Perm()); err != nil {
			t.Errorf("фоновый отказ: файл Xray не возвращён: %v", err)
			return
		}
		restored = true
	}
	t.Cleanup(restore)
	if err := os.WriteFile(path, append(append([]byte(nil), orig...), '\n'), info.Mode().Perm()); err != nil {
		t.Fatalf("фоновый отказ: файл Xray не изменён: %v", err)
	}
	selectMihomo := func(label string) {
		r := e.callSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": other})
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", label, r.Status, r.Code)
		}
		ks, _ := r.object(t)["kernels"].(map[string]any)
		if out, _ := ks["mihomo"].(string); out != "applied" {
			t.Errorf("%s: исход Mihomo %q, ожидалось applied", label, out)
		}
		if !e.rtRtWaitSettled(gid) {
			t.Fatalf("%s: группа %s не вышла из переключения за 90 с", label, gid)
		}
	}
	firstID := ""
	for n := 1; n <= 2; n++ {
		label := "фоновый отказ №" + strconv.Itoa(n)
		selectMihomo(label)
		now, _, unconf, _ := e.rtRtGroupNow(gid)
		if now != cur {
			t.Errorf("%s: current группы %s не вернулся к прежнему узлу (цель осталась=%t)", label, gid, now == other)
		}
		if unconf {
			t.Errorf("%s: у группы %s маркер selection_unconfirmed", label, gid)
		}
		if mnow := e.rtRtWaitNow(groupName, labels[cur]); mnow != labels[cur] {
			t.Errorf("%s: now группы %s в Mihomo не вернулся к прежнему узлу (длина now %d)", label, gid, len(mnow))
		}
		ids, code, reason := e.rtRtSelectNotices(gid)
		if len(ids) != 1 {
			t.Errorf("%s: уведомлений routing_select группы %s %d, ожидалось одно", label, gid, len(ids))
		}
		fresh := false
		if len(ids) == 1 {
			fresh = ids[0] != firstID
			if n == 1 {
				firstID = ids[0]
			} else if !fresh {
				t.Errorf("%s: идентификатор уведомления группы %s не сменился (D-11: каждый отказ — новое уведомление)", label, gid)
			}
		}
		t.Logf("mihomo-first: fail=%d code=%s reason=%s notice_new=%t", n, code, reason, fresh)
	}
	restore()
	selectMihomo("выбор после возврата файла")
	if now, _, unconf, _ := e.rtRtGroupNow(gid); now != other || unconf {
		t.Errorf("выбор после возврата файла: current группы %s равен цели=%t, маркер=%t", gid, now == other, unconf)
	}
	if mnow := e.rtRtWaitNow(groupName, labels[other]); mnow != labels[other] {
		t.Errorf("выбор после возврата файла: now группы %s в Mihomo не равен цели (длина now %d)", gid, len(mnow))
	}
	if ids, _, _ := e.rtRtSelectNotices(gid); len(ids) != 0 {
		t.Errorf("выбор после возврата файла: уведомлений routing_select группы %s %d, ожидалось 0", gid, len(ids))
	}
}

// rtRtSourcesCount — число источников вида не «Мои узлы» (подписок): только число.
func (e *rtSrc) rtRtSourcesCount() int {
	e.t.Helper()
	view := e.call(http.MethodGet, rtSourcesBase, nil)
	if view.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtSourcesBase, view.Status, view.Code)
	}
	srcs, _ := view.object(e.t)["sources"].([]any)
	n := 0
	for _, it := range srcs {
		if src, _ := it.(map[string]any); src != nil && src["kind"] != "my" {
			n++
		}
	}
	return n
}

// rtRtFindGroupID ищет группу представления по ID.
func rtRtFindGroupID(view map[string]any, id string) map[string]any {
	groups, _ := view["groups"].([]any)
	for _, g := range groups {
		if m, _ := g.(map[string]any); m != nil && m["id"] == id {
			return m
		}
	}
	return nil
}

// rtRtLongwaitPair — пара ожидания (условие отказа G-150-3, D-17): выбор A группы
// gid и выбор B независимой группы secondID, отправленный, когда gid уже switching
// (или пришёл ответ A). Цели — узлы ядра core с подписью, отличной от текущей
// (вид «Прокси» из runtimeView). Внутри — только проверки правильности: оба ответа
// 200 без apply_busy, исход ядра core applied (на Xray — без restart_reason),
// current обеих групп — цели; ошибки начинаются с «корректность:». Порог времени —
// только через check (nil — без порога). Строки select-stages: и runlive-stages:
// несут метку kind. Возвращает наибольший wait строк этапов ответов 200 и wait
// строки secondID (-1 — строки нет). В журнал теста — только ID групп, коды и числа.
func (e *rtSrc) rtRtLongwaitPair(core, kind, gid, secondID string, check func(t *testing.T, kind string, st rtRtStage), runtimeView func() map[string]any) (maxWait, secondWait int64) {
	t := e.t
	t.Helper()
	secondWait = -1
	rv := runtimeView()
	target := func(id string) string {
		g := rtRtFindGroupID(rv, id)
		if g == nil {
			t.Fatalf("пара ожидания (%s): нет группы %s в виде «Прокси»", kind, id)
		}
		curLabel, _ := g["current_label"].(string)
		ms, _ := g["members"].([]any)
		for _, it := range ms {
			m, _ := it.(map[string]any)
			if m["kind"] == "node" && m[core] == true && m["name"] != curLabel {
				member, _ := m["member"].(string)
				return member
			}
		}
		t.Fatalf("пара ожидания (%s): у группы %s нет узла ядра %s с подписью, отличной от текущей", kind, id, core)
		return ""
	}
	pair := [2]struct{ id, member string }{{gid, target(gid)}, {secondID, target(secondID)}}
	offset := rtRtLogOffset(t)
	startA, startB := make(chan struct{}), make(chan struct{})
	out := make(chan rtRtSelRes, 2)
	go e.rtRtSelectAsync(0, pair[0].id, pair[0].member, startA, out)
	go e.rtRtSelectAsync(1, pair[1].id, pair[1].member, startB, out)
	close(startA)
	var res [2]rtRtSelRes
	got, switching := 0, false
	for got == 0 && !switching {
		if s, ok := e.rtRtTakeSnap(); ok && s.switching[gid] {
			switching = true
			break
		}
		select {
		case r := <-out:
			res[r.job] = r
			got++
		case <-time.After(rtRtSnapPoll):
		}
	}
	close(startB)
	for ; got < 2; got++ {
		r := <-out
		res[r.job] = r
	}
	var okGroups []string
	for i, r := range res {
		if r.err != nil {
			t.Fatalf("пара ожидания (%s), выбор %s: %v", kind, pair[i].id, r.err)
		}
		resp := e.decode(r.status, r.raw)
		httpMS := r.done.Sub(r.sent).Milliseconds()
		switch {
		case resp.Code == "apply_busy":
			t.Errorf("корректность: G-150-3 (%s): независимый выбор получил apply_busy во время прохода (группа %s, ответ через %d мс)", kind, pair[i].id, httpMS)
		case r.status != http.StatusOK:
			t.Errorf("корректность: пара ожидания (%s), выбор %s: %d %s, ожидалось 200", kind, pair[i].id, resp.Status, resp.Code)
		default:
			okGroups = append(okGroups, pair[i].id)
			obj := resp.object(t)
			ks, _ := obj["kernels"].(map[string]any)
			if o, _ := ks[core].(string); o != "applied" {
				t.Errorf("корректность: пара ожидания (%s), выбор %s: исход ядра %s %q, ожидалось applied", kind, pair[i].id, core, o)
			}
			if reason, present := obj["restart_reason"]; present && core == "xray" {
				t.Errorf("корректность: пара ожидания (%s), выбор %s: горячая смена потребовала перезапуск: restart_reason=%v", kind, pair[i].id, reason)
			}
		}
	}
	// Строки этапов обеих групп (у отказа apply_busy она тоже есть — с его wait).
	stages, runlive, ok := rtRtWaitStages(offset, []string{gid, secondID}, 1, rtRtStagesWait(core))
	if !ok {
		t.Errorf("пара ожидания (%s): в журнале панели нет строк этапов обеих групп или RunLive (строк RunLive %d)", kind, len(runlive))
	}
	for i, p := range pair {
		st, present := stages[p.id]
		if !present {
			continue
		}
		t.Logf("select-stages: core=%s kind=%s group=%s http=%d %s", core, kind, p.id, res[i].done.Sub(res[i].sent).Milliseconds(), st.pairs)
		if p.id == secondID {
			secondWait = st.num("wait")
		}
		for _, g := range okGroups {
			if g == p.id {
				maxWait = max(maxWait, st.num("wait"))
				if check != nil {
					check(t, "пара ожидания "+p.id, st)
				}
			}
		}
	}
	for _, rl := range runlive {
		t.Logf("runlive-stages: core=%s kind=%s %s", core, kind, rl)
	}
	t.Logf("%s: B отправлен после switching группы %s=%t", kind, gid, switching)
	view := e.rtRtView()
	for _, p := range pair {
		g := rtRtFindGroupID(view, p.id)
		if cur, _ := g["current"].(string); g == nil || cur != p.member {
			t.Errorf("корректность: пара ожидания (%s): current группы %s не равен цели", kind, p.id)
		}
	}
	return maxWait, secondWait
}

// Выбор участника ручной группы: два узла → ручная группа → take → «Применить»
// → вид работающих прокси → POST select → «Сейчас» в представлении, now группы в
// Mihomo или исходящий группы в Xray (горячая замена без смены PID Xray) → вид и
// «Сейчас» страницы «Прокси» → пачка пинга с присоединением и отменой →
// подтверждение выбора → правка правила и «Применить» не сбрасывают выбор. Метка
// ядра: тест читает активное ядро и ставит выбор в нём.
func TestRouterRoutingSelectLive(t *testing.T) {
	routertest.Slow(t, "серия «Применить» и переключение группы на живом ядре")
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.freshDraft()
	e.rtRstIdle()
	e.rtRtPurge()
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	taken := false

	// Набор источников стенда: подписка, оставшаяся от прерванного прогона, иначе
	// попала бы в «Выбор» (sources.all) и в цели блока D-17. Число подписок до
	// удаления и после всех очисток сверяют план-сессии; печатаются только числа.
	t.Logf("sources-start: core=%s subscriptions=%d", tg.Core, e.rtRtSourcesCount())
	for i := 0; i < 5; i++ {
		id := e.rtDNSSubscriptionID()
		if id == "" {
			break
		}
		if r := e.call(http.MethodPost, rtSourcesBase+"/delete", map[string]any{"revision": e.revision(), "id": id}); r.Status != http.StatusOK {
			t.Fatalf("удаление оставшейся подписки: %d %s", r.Status, r.Code)
		}
	}
	if e.rtRtDraftChanges() > 0 {
		e.rtRtApplyOK("удаление оставшейся подписки")
	}
	// Регистрируется раньше общей очистки и выполняется после неё: последний шаг —
	// число подписок, ожидается 0.
	t.Cleanup(func() {
		n := e.with(t).rtRtSourcesCount()
		t.Logf("sources-end: core=%s subscriptions=%d", tg.Core, n)
		if n > 0 {
			t.Errorf("очистка: подписка осталась в источниках (%d)", n)
		}
	})

	// Очистка при любом исходе: release → «Применить», затем узлы и секции. Выбор в
	// Live уходит вместе с группой при «Применить».
	t.Cleanup(func() {
		c := e.with(t)
		if taken {
			r := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
			if r.Status != http.StatusOK {
				t.Errorf("очистка: release: %d %s", r.Status, r.Code)
				return
			}
			c.rtRtApplyOK("очистка: возврат")
		}
		c.rtRtPurge()
	})

	// Все четыре узла выражаются в обоих ядрах: выбор узла, которого активное ядро
	// не умеет (trojan_ws в Xray — xray_no_insecure), на его файлы не влияет. Адрес
	// узлов — «чёрная дыра» на loopback: соединение принимается и молчит, замер
	// идёт до таймаута пробы, и повтор пинга успевает присоединиться к пачке
	// (отказ соединения закончил бы пачку Mihomo раньше повтора). Узлов четыре с
	// различными подписями («VLESS WS», «Trojan gRPC», «VLESS gRPC», «VLESS TLS»):
	// блоку D-17 нужны две цели «Выбор», отличные по подписи и от текущего выбора,
	// и от исходного startMember.
	hole := rtRtBlackhole(t)
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_ws", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "trojan_grpc", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_grpc", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_tls_alpn", hole))

	// До взятия и «Применить» группа не применена: select отвечает group_not_applied.
	save := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
		"revision": e.rtRtRevision(),
		"group": map[string]any{
			"name": "Выбор", "mode": "manual",
			"sources": map[string]any{"all": true},
			"members": []map[string]string{},
		},
	})
	if save.Status != http.StatusOK {
		t.Fatalf("groups/save: %d %s", save.Status, save.Code)
	}
	gid, _ := save.object(t)["group"].(map[string]any)["id"].(string)
	if len(gid) != 8 {
		t.Fatalf("groups/save: идентификатор группы длиной %d, ожидалось 8", len(gid))
	}
	e.rtRtExpect("select: группа не применена", e.call(http.MethodPost, rtRoutingBase+"/select",
		map[string]string{"group": gid, "member": "direct"}), http.StatusConflict, "group_not_applied")

	// Группы блока D-17: «Связанная» содержит «Выбор» (одна компонента с ним),
	// «Вторая» и «Третья» без участников-групп — независимые.
	saveGroup := func(name string, members []map[string]string) string {
		r := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
			"revision": e.rtRtRevision(),
			"group": map[string]any{
				"name": name, "mode": "manual",
				"sources": map[string]any{"all": true},
				"members": members,
			},
		})
		if r.Status != http.StatusOK {
			t.Fatalf("groups/save (D-17): %d %s", r.Status, r.Code)
		}
		id, _ := r.object(t)["group"].(map[string]any)["id"].(string)
		if len(id) != 8 {
			t.Fatalf("groups/save (D-17): идентификатор группы длиной %d, ожидалось 8", len(id))
		}
		return id
	}
	linkedID := saveGroup("Связанная", []map[string]string{{"kind": "group", "id": gid}})
	secondID := saveGroup("Вторая", []map[string]string{})
	thirdID := saveGroup("Третья", []map[string]string{})

	if tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()}); tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	taken = true
	e.rtRtApplyOK("взятие")
	e.rtRtRebuildReleased()

	view := e.rtRtView()
	rtAssertContractKeys(t, contract, view, "RoutingView", "routing")
	group := rtRtFindGroup(view, "Выбор")
	if group == nil {
		t.Fatal("представление: нет группы «Выбор»")
	}
	nodeMembers, labels := rtRtNodeOptions(group)
	distinct := map[string]bool{}
	for _, m := range nodeMembers {
		distinct[labels[m]] = true
	}
	if len(distinct) < 4 {
		t.Fatalf("представление: различных подписей узлов среди вариантов группы %d, ожидалось не меньше 4", len(distinct))
	}
	startMember, _ := group["current"].(string)
	if !strings.HasPrefix(startMember, "node:") {
		t.Fatalf("представление: current до выбора не узел (длина %d)", len(startMember))
	}
	if label, _ := group["current_label"].(string); label == "" || label != labels[startMember] {
		t.Errorf("представление: current_label до выбора не совпадает с подписью варианта")
	}
	target := ""
	for _, m := range nodeMembers {
		if m != startMember && labels[m] != labels[startMember] {
			target = m
			break
		}
	}
	if target == "" {
		t.Fatal("среди вариантов нет узла, отличного от текущего")
	}
	runtimeView := func() map[string]any {
		r := e.call(http.MethodGet, "/api/configlayer/proxies", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("proxies: %d %s", r.Status, r.Code)
		}
		v := r.object(t)
		rtAssertContractKeys(t, contract, v, "RuntimeView", "proxies")
		return v
	}
	runtimeBefore := runtimeView()
	modes, _ := runtimeBefore["modes"].(map[string]any)
	if runtimeBefore["state"] != "panel" || modes[tg.Core] != "panel" {
		t.Errorf("proxies: состояние panel=%t, управление panel=%t", runtimeBefore["state"] == "panel", modes[tg.Core] == "panel")
	}
	runtimeGroup := rtRtFindGroup(runtimeBefore, "Выбор")
	if runtimeGroup == nil {
		t.Fatal("proxies: нет ручной группы")
	}
	if runtimeGroup["mode"] != "manual" || runtimeGroup["selectable"] != true || runtimeGroup["current"] != startMember {
		t.Errorf("proxies: manual=%t selectable=%t current=%t", runtimeGroup["mode"] == "manual", runtimeGroup["selectable"] == true, runtimeGroup["current"] == startMember)
	}
	runtimeMembers, _ := runtimeGroup["members"].([]any)
	nodeCount := 0
	for _, member := range runtimeMembers {
		m, _ := member.(map[string]any)
		if m["kind"] == "node" {
			nodeCount++
			if m[tg.Core] != true {
				t.Error("proxies: узел несовместим с активным ядром")
			}
		}
	}
	if nodeCount < 4 {
		t.Errorf("proxies: узлов %d, ожидалось не меньше 4", nodeCount)
	}
	draftBefore := e.rtRtDraftChanges()

	// Коды отказа выбора.
	e.rtRtExpect("select: неизвестный участник", e.call(http.MethodPost, rtRoutingBase+"/select",
		map[string]string{"group": gid, "member": "node:my/00000000"}), http.StatusBadRequest, "member_invalid")
	e.rtRtExpect("select: неизвестная группа", e.call(http.MethodPost, rtRoutingBase+"/select",
		map[string]string{"group": "ffffffff", "member": target}), http.StatusConflict, "group_not_applied")

	var xrayBefore map[string]any
	var pidsBefore []int
	if tg.Core == "xray" {
		var ok bool
		if xrayBefore, ok = rtRtGroupOutbound(t, gid); !ok {
			t.Fatal("Xray: в файлах нет исходящего группы после «Применить»")
		}
		pidsBefore, _ = rtRstKernelPIDs("xray")
		if len(pidsBefore) != 1 {
			t.Fatalf("Xray: до выбора PID %d, ожидался один", len(pidsBefore))
		}
	}

	// Выбор участника через API. maxWait — наибольшее ожидание чужого прохода (wait
	// строк этапов ответов 200) за весь тест: строка d17-longwait в конце.
	var maxWait int64
	// mihomoTotals и mihomoHTTP — total строк этапов и http выборов на ядре mihomo
	// (одиночный и барьер D-17): медианы сверяются с rtRtMihomoSelectBudget и
	// rtRtMihomoHTTPBudget после блока D-17.
	var mihomoTotals, mihomoHTTP []int64
	// mihomoParts — составляющие времени тех же строк для разбора пробела 2 (строка
	// mihomo-select-parts:, без проверки).
	var mihomoParts rtRtMihomoParts
	// Вторая вкладка (ядро mihomo): поток событий слоя открыт до отправки выбора.
	var proxiesEv <-chan time.Time
	stopEvents := func() {}
	if tg.Core == "mihomo" {
		proxiesEv, stopEvents = e.rtRtProxiesEvents(t)
	}
	logOffset := rtRtLogOffset(t)
	selStart := time.Now()
	sel := e.callSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": target})
	selHTTP := time.Since(selStart).Milliseconds()
	if sel.Status != http.StatusOK {
		t.Fatalf("select: %d %s", sel.Status, sel.Code)
	}
	// Своя вкладка: вид «Прокси» сразу после ответа (до ожидания строк этапов).
	ownCur, ownSwitching, _, ownRead := e.rtRtGroupNow(gid)
	rtAssertContractKeys(t, contract, sel.object(t), "SelectResult", "select")
	kernels, _ := sel.object(t)["kernels"].(map[string]any)
	outcome, _ := kernels[tg.Core].(string)
	// Путь Xray: выбор пишет xcp-groups общим проходом (исход Xray не not_installed).
	xrayOutcome, _ := kernels["xray"].(string)
	xrayPath := xrayOutcome != "not_installed"
	if tg.Core == "mihomo" && xrayPath {
		// Быстрый путь (план 150-19, D-14): ответ после Clash API, файл Xray следом.
		// Своя вкладка видит выбранный узел сразу; вторая — по событию proxies,
		// пришедшему до конца фонового прохода (switching=true). Кончился ли проход
		// к чтению своей вкладки — тогда вторая исход не различает (executed=false).
		ownOK := ownRead && ownCur == target
		eventOK, eventSwitching, eventMS := false, false, int64(-1)
		deadline := time.After(5 * time.Second)
	events:
		for {
			select {
			case at := <-proxiesEv:
				if at.Before(selStart) {
					continue
				}
				cur, sw, _, ok := e.rtRtGroupNow(gid)
				if ok && cur == target {
					eventOK, eventSwitching, eventMS = true, sw, at.Sub(selStart).Milliseconds()
					break events
				}
			case <-deadline:
				break events
			}
		}
		stopEvents()
		executed := ownSwitching
		t.Logf("mihomo-view: own_ok=%t own_switching=%t event_ok=%t event_switching=%t event_ms=%d executed=%t",
			ownOK, ownSwitching, eventOK, eventSwitching, eventMS, executed)
		if !ownOK || !eventOK || (executed && !eventSwitching) {
			t.Errorf("вид после ответа выбора Mihomo: own_ok=%t own_switching=%t event_ok=%t event_switching=%t executed=%t",
				ownOK, ownSwitching, eventOK, eventSwitching, executed)
		}
	} else {
		stopEvents()
	}
	if xrayPath {
		// На ядре mihomo строка этапов пишется в конце фонового прохода файла Xray.
		stages, _, ok := rtRtWaitStages(logOffset, []string{gid}, 0, rtRtStagesWait(tg.Core))
		if !ok {
			t.Errorf("журнал панели: нет строки этапов выбора Xray группы %s", gid)
		} else {
			st := stages[gid]
			t.Logf("select-stages: core=%s kind=single nodes=%d group=%s http=%d %s", tg.Core, len(distinct), gid, selHTTP, st.pairs)
			maxWait = max(maxWait, st.num("wait"))
			// Порог времени Xray — в TestRouterRoutingSelectScale (карантин);
			// здесь строка этапов только пишется в журнал.
			if tg.Core == "mihomo" {
				mihomoTotals = append(mihomoTotals, st.num("total"))
				mihomoHTTP = append(mihomoHTTP, selHTTP)
				mihomoParts.add(st)
			}
		}
	}
	if outcome != "applied" && outcome != "restarted" {
		t.Errorf("select: исход для активного ядра %q, ожидалось applied или restarted", outcome)
	}
	if tg.Core == "xray" {
		if outcome != "applied" {
			t.Errorf("Xray: горячая смена applied=%t", outcome == "applied")
		}
		if reason, present := sel.object(t)["restart_reason"]; present {
			t.Errorf("Xray: горячая смена потребовала перезапуск: restart_reason=%v", reason)
		}
	}

	// «Сейчас» в представлении.
	group = rtRtFindGroup(e.rtRtView(), "Выбор")
	if group == nil || group["current"] != target {
		t.Errorf("представление после выбора: current не равен выбранному участнику")
	}
	if group != nil {
		if label, _ := group["current_label"].(string); label == "" || label != labels[target] {
			t.Errorf("представление после выбора: current_label не совпадает с подписью варианта")
		}
	}

	// Ядро: now группы в Mihomo, исходящий группы в Xray.
	switch tg.Core {
	case "mihomo":
		if now := e.rtRtWaitNow("Выбор", labels[target]); now != labels[target] {
			t.Errorf("Mihomo: now группы не равен имени выбранного узла (длина now %d)", len(now))
		}
	case "xray":
		after, ok := rtRtGroupOutbound(t, gid)
		switch {
		case !ok:
			t.Error("Xray: исходящий группы пропал после выбора")
		case reflect.DeepEqual(after, xrayBefore):
			t.Error("Xray: исходящий группы не сменился после выбора")
		case !rtRtMatchesNode(t, after):
			t.Error("Xray: исходящий группы не совпадает ни с одним узлом «Моих узлов»")
		}
		pidsAfter, _ := rtRstKernelPIDs("xray")
		if len(pidsAfter) != 1 || pidsAfter[0] != pidsBefore[0] {
			t.Errorf("Xray: горячая смена сохранила PID=%t, процессов %d", reflect.DeepEqual(pidsAfter, pidsBefore), len(pidsAfter))
		}
	}
	runtimeGroup = rtRtFindGroup(runtimeView(), "Выбор")
	if runtimeGroup == nil || runtimeGroup["current"] != target {
		t.Error("proxies: current после выбора не равен выбранному участнику")
	}
	now := e.call(http.MethodGet, "/api/configlayer/proxies/now", nil)
	if now.Status == http.StatusConflict && now.Code == "kernel_restarting" {
		t.Logf("proxies/now: %d %s", now.Status, now.Code)
	} else if now.Status != http.StatusOK {
		t.Errorf("proxies/now: %d %s", now.Status, now.Code)
	} else {
		rtAssertContractKeys(t, contract, now.object(t), "RuntimeNow", "proxies/now")
	}
	// Фоновый отказ быстрого пути (ядро mihomo, Xray установлен и остановлен): два
	// отказа подряд — два уведомления с разными идентификаторами (D-11).
	if tg.Core == "mihomo" && xrayPath {
		e.rtRtBackgroundFail(gid, "Выбор", nodeMembers, labels)
	}

	// Очистка пинга регистрируется после очистки маршрутизации и выполняется раньше
	// неё даже при Fatal. Отмена отвечает только после уборки пачки (раннер ждёт
	// завершения), поэтому идущей пачки и временного процесса после сценария нет.
	t.Cleanup(func() {
		c := e.with(t)
		r := c.callSlow(http.MethodPost, "/api/configlayer/proxies/ping/cancel", nil)
		if r.Status != http.StatusOK {
			t.Errorf("очистка пинга: %d %s", r.Status, r.Code)
			return
		}
		if state, _ := r.object(t)["state"].(string); state == "running" {
			t.Error("очистка пинга: пачка осталась в работе после отмены")
		}
	})
	readResults := func(path string) map[string]any {
		r := e.call(http.MethodGet, "/api/configlayer/proxies/results", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, r.Status, r.Code)
		}
		v := r.object(t)
		rtAssertContractKeys(t, contract, v, "PingSnapshot", path)
		return v
	}
	// Результаты прежних пачек (в том числе другого ядра) хранятся в раннере;
	// проверки ниже касаются только записей, появившихся или сменившихся в этой пачке.
	prior, _ := readResults("proxies/results/prior")["results"].(map[string]any)
	pingBody := map[string]string{"scope": "group", "group": gid}
	launch := func(path string) map[string]any {
		// Пачка во время окна перезапуска XKeen (60 с после «Применить» этого теста)
		// отвечает 409 kernel_restarting: ждём конца окна, любой другой отказ — сразу.
		restartDeadline := time.Now().Add(75 * time.Second)
		r := e.call(http.MethodPost, "/api/configlayer/proxies/ping", pingBody)
		for r.Status == http.StatusConflict && r.Code == "kernel_restarting" && time.Now().Before(restartDeadline) {
			time.Sleep(time.Second)
			r = e.call(http.MethodPost, "/api/configlayer/proxies/ping", pingBody)
		}
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, r.Status, r.Code)
		}
		v := r.object(t)
		rtAssertContractKeys(t, contract, v, "PingStart", path)
		return v
	}
	// Повтор во время пачки присоединяется к ней. Повтор без joined означает, что
	// прежняя пачка уже закончилась и он сам запустил новую: к ней присоединяется
	// следующий повтор. Три подряд новые пачки — присоединение не работает.
	startData := launch("proxies/ping")
	var joinedData map[string]any
	for attempt := 0; attempt < 3; attempt++ {
		joinedData = launch("proxies/ping/join")
		if joinedData["joined"] == true {
			break
		}
		startData = joinedData
	}
	batchID, _ := startData["batch_id"].(string)
	if batchID == "" || joinedData["joined"] != true || joinedData["batch_id"] != batchID {
		t.Errorf("proxies/ping: id непустой=%t joined=%t id совпал=%t", batchID != "", joinedData["joined"] == true, joinedData["batch_id"] == batchID)
	}
	readyBefore, _ := readResults("proxies/results")["results"].(map[string]any)
	cancelled := e.callSlow(http.MethodPost, "/api/configlayer/proxies/ping/cancel", nil)
	if cancelled.Status != http.StatusOK {
		t.Fatalf("proxies/ping/cancel: %d %s", cancelled.Status, cancelled.Code)
	}
	rtAssertContractKeys(t, contract, cancelled.object(t), "PingBatch", "proxies/ping/cancel")
	var finished map[string]any
	ok, seen := routertest.Poll(15*time.Second, 100*time.Millisecond, func() (bool, string) {
		r := e.call(http.MethodGet, "/api/configlayer/proxies/results", nil)
		if r.Status != http.StatusOK {
			return false, strconv.Itoa(r.Status) + " " + r.Code
		}
		finished = r.object(t)
		batch, _ := finished["batch"].(map[string]any)
		state, _ := batch["state"].(string)
		return batch["id"] == batchID && state != "" && state != "running", state
	})
	if !ok {
		t.Fatalf("proxies/ping/cancel: пачка не завершилась: %s", seen)
	}
	rtAssertContractKeys(t, contract, finished, "PingSnapshot", "proxies/results/finished")
	finishedBatch, _ := finished["batch"].(map[string]any)
	if state, _ := finishedBatch["state"].(string); state != "cancelled" && state != "done" {
		t.Errorf("proxies/ping/cancel: состояние пачки %q, ожидалось cancelled или done", state)
	}
	readyAfter, _ := finished["results"].(map[string]any)
	for ref, result := range readyBefore {
		if reflect.DeepEqual(prior[ref], result) {
			continue
		}
		if !reflect.DeepEqual(readyAfter[ref], result) {
			t.Error("proxies/ping/cancel: готовый результат пачки потерян или изменён")
		}
	}
	for ref, value := range readyAfter {
		if reflect.DeepEqual(prior[ref], value) {
			continue
		}
		result, _ := value.(map[string]any)
		if result["kernel"] != tg.Core {
			t.Error("proxies/results: замер пачки не активным ядром")
		}
		if result["method"] != "xray" && result["method"] != "mihomo" && result["method"] != "tls" {
			t.Error("proxies/results: неизвестный способ замера")
		}
	}
	e.rtRtExpect("proxies/ping: неверный охват", e.call(http.MethodPost, "/api/configlayer/proxies/ping",
		map[string]string{"scope": "bad"}), http.StatusBadRequest, "invalid_input")
	e.rtRtExpect("proxies/ping: неизвестная группа", e.call(http.MethodPost, "/api/configlayer/proxies/ping",
		map[string]string{"scope": "group", "group": "ffffffff"}), http.StatusNotFound, "group_not_found")
	confirm := e.callSlow(http.MethodPost, "/api/configlayer/proxies/confirm", map[string]string{"group": gid})
	if confirm.Status != http.StatusOK {
		t.Fatalf("proxies/confirm: %d %s", confirm.Status, confirm.Code)
	}
	confirmed := confirm.object(t)
	rtAssertContractKeys(t, contract, confirmed, "ConfirmResult", "proxies/confirm")
	if confirmed["confirmed"] != true || confirmed["group"] != gid {
		t.Errorf("proxies/confirm: confirmed=%t group=%t", confirmed["confirmed"] == true, confirmed["group"] == gid)
	}
	if e.rtRtDraftChanges() != draftBefore {
		t.Error("выбор, пинг или подтверждение изменили черновик")
	}

	// D-17: параллельные выборы. Пять выборов уходят одновременно по общему барьеру:
	// A и B — «Выбор» (B — повторное нажатие другим узлом), C — связанная
	// «Связанная», D и E — независимые «Вторая» и «Третья». Наложение гарантирует
	// построение, а не скорость выбора: разброс отправки меньше удержания компоненты
	// проходом (total − model строки этапов), иначе попытка повторяется с новыми
	// целями. В журнал теста — только ID групп, коды и числа.
	names := []string{"Выбор", "Связанная", "Вторая", "Третья"}
	ids := map[string]string{"Выбор": gid, "Связанная": linkedID, "Вторая": secondID, "Третья": thirdID}
	// d17Groups — current и подписи вариантов-узлов групп блока по GET routing.
	d17Groups := func() (cur map[string]string, opts map[string][]string, lbl map[string]map[string]string) {
		view := e.rtRtView()
		cur, opts, lbl = map[string]string{}, map[string][]string{}, map[string]map[string]string{}
		for _, name := range names {
			g := rtRtFindGroup(view, name)
			if g == nil {
				t.Fatalf("D-17: нет группы %s в представлении", ids[name])
			}
			cur[name], _ = g["current"].(string)
			opts[name], lbl[name] = rtRtNodeOptions(g)
		}
		return cur, opts, lbl
	}
	// d17Pick — первый узел группы, подпись которого не входит в avoid.
	d17Pick := func(opts []string, lbl map[string]string, avoid ...string) string {
		for _, m := range opts {
			ok := true
			for _, a := range avoid {
				if lbl[m] == a {
					ok = false
				}
			}
			if ok {
				return m
			}
		}
		return ""
	}
	// d17Fire отправляет выборы jobs (группа, участник) одновременно по барьеру и
	// опрашивает вид «Прокси», пока не придут все ответы.
	d17Fire := func(jobs [][2]string) ([]rtRtSelRes, []rtRtSnap) {
		start := make(chan struct{})
		out := make(chan rtRtSelRes, len(jobs))
		for i, j := range jobs {
			go e.rtRtSelectAsync(i, j[0], j[1], start, out)
		}
		close(start)
		res := make([]rtRtSelRes, len(jobs))
		var snaps []rtRtSnap
		for got := 0; got < len(jobs); {
			select {
			case r := <-out:
				res[r.job] = r
				got++
				continue
			default:
			}
			if s, ok := e.rtRtTakeSnap(); ok {
				snaps = append(snaps, s)
			}
			select {
			case r := <-out:
				res[r.job] = r
				got++
			case <-time.After(rtRtSnapPoll):
			}
		}
		for _, r := range res {
			if r.err != nil {
				t.Fatalf("D-17: выбор №%d: %v", r.job+1, r.err)
			}
		}
		return res, snaps
	}

	if !xrayPath {
		// Без прохода Xray выбор Mihomo — один вызов Clash API без общего прохода:
		// наложению не на чем случиться, проверяется только приём независимых выборов.
		cur, opts, lbl := d17Groups()
		d := d17Pick(opts["Вторая"], lbl["Вторая"], lbl["Вторая"][cur["Вторая"]])
		ee := d17Pick(opts["Третья"], lbl["Третья"], lbl["Третья"][cur["Третья"]])
		if d == "" || ee == "" {
			t.Fatal("D-17: у независимых групп нет узла, отличного от текущего")
		}
		res, _ := d17Fire([][2]string{{secondID, d}, {thirdID, ee}})
		for _, r := range res {
			if r.status != http.StatusOK {
				resp := e.decode(r.status, r.raw)
				t.Errorf("D-17: независимый выбор №%d: %d %s, ожидалось 200", r.job+1, resp.Status, resp.Code)
			}
		}
		t.Logf("d17-overlap: core=%s skipped=no_xray_path", tg.Core)
	} else {
		guaranteed := false
		var lastSpread, lastHold int64
		// Окно журнала панели блока D-17 (все попытки): причина остаточного маркера
		// (d17-marker:) и положительное свидетельство пробела 3 (d17-evidence:) —
		// успешные хвосты быстрого пути по коду reput строк этапов ответов 200.
		d17Offset := rtRtLogOffset(t)
		var evFastOK, evSkip, evDone, evDoneOK int
		for attempt := 1; attempt <= 3 && !guaranteed; attempt++ {
			cur, opts, lbl := d17Groups()
			// Цели «Выбор»: узлы с подписью, отличной от текущего выбора и от
			// startMember, с различными подписями; при четырёх узлах их ровно два.
			var cands []string
			seenLabel := map[string]bool{}
			for _, m := range opts["Выбор"] {
				l := lbl["Выбор"][m]
				if l == lbl["Выбор"][cur["Выбор"]] || l == labels[startMember] || seenLabel[l] {
					continue
				}
				seenLabel[l] = true
				cands = append(cands, m)
			}
			if len(cands) < 2 {
				t.Fatalf("D-17: целей «Выбор» %d, ожидалось 2", len(cands))
			}
			a, b := cands[0], cands[1]
			// c отличается по подписи и от своего текущего выбора, и от текущего
			// выбора «Выбор»: исходящий «Связанная» при её выигрыше обязан смениться.
			c := d17Pick(opts["Связанная"], lbl["Связанная"], lbl["Связанная"][cur["Связанная"]], lbl["Выбор"][cur["Выбор"]])
			d := d17Pick(opts["Вторая"], lbl["Вторая"], lbl["Вторая"][cur["Вторая"]])
			ee := d17Pick(opts["Третья"], lbl["Третья"], lbl["Третья"][cur["Третья"]])
			if c == "" || d == "" || ee == "" {
				t.Fatalf("D-17: нет цели связанной (%t) или независимых (%t, %t) групп", c != "", d != "", ee != "")
			}
			jobs := [][2]string{{gid, a}, {gid, b}, {linkedID, c}, {secondID, d}, {thirdID, ee}}
			obBefore := map[string]map[string]any{}
			var pidsD17 []int
			if tg.Core == "xray" {
				for _, name := range names {
					ob, ok := rtRtGroupOutbound(t, ids[name])
					if !ok {
						t.Fatalf("D-17: в файлах Xray нет исходящего группы %s", ids[name])
					}
					obBefore[name] = ob
				}
				pidsD17, _ = rtRstKernelPIDs("xray")
			}
			offset := rtRtLogOffset(t)
			res, snaps := d17Fire(jobs)
			resp := make([]rtSrcResp, len(res))
			var okGroups []string
			for i, r := range res {
				resp[i] = e.decode(r.status, r.raw)
				if r.status == http.StatusOK {
					okGroups = append(okGroups, jobs[i][0])
				}
				if resp[i].Code == "apply_busy" {
					t.Errorf("D-17: выбор №%d группы %s получил apply_busy (ожидание %d мс)", i+1, jobs[i][0], r.done.Sub(r.sent).Milliseconds())
				}
				if r.status >= 500 {
					t.Errorf("D-17: выбор №%d группы %s: %d %s", i+1, jobs[i][0], resp[i].Status, resp[i].Code)
				}
			}
			stages, runlive, ok := rtRtWaitStages(offset, okGroups, 1, rtRtStagesWait(tg.Core))
			if !ok {
				t.Fatalf("D-17: в журнале панели нет строк этапов всех ответов 200 (групп %d, строк RunLive %d)", len(okGroups), len(runlive))
			}
			for i, r := range res {
				if r.status != http.StatusOK {
					continue
				}
				g := jobs[i][0]
				st := stages[g]
				httpMS := r.done.Sub(r.sent).Milliseconds()
				t.Logf("select-stages: core=%s kind=parallel group=%s http=%d %s", tg.Core, g, httpMS, st.pairs)
				maxWait = max(maxWait, st.num("wait"))
				if tg.Core == "mihomo" {
					mihomoTotals = append(mihomoTotals, st.num("total"))
					mihomoHTTP = append(mihomoHTTP, httpMS)
					mihomoParts.add(st)
					switch st.kv["reput"] {
					case "skip":
						evSkip++
					case "done":
						evDone++
					}
					if st.num("clash") > 0 && rtRtSelectOK[st.kv["outcome"]] {
						evFastOK++
						if st.kv["reput"] == "done" {
							evDoneOK++
						}
					}
				}
			}
			for _, rl := range runlive {
				t.Logf("runlive-stages: core=%s %s", tg.Core, rl)
			}

			// Гарантия наложения: все пять запросов ушли, пока первый проход держал
			// компоненту (разброс отправки меньше наименьшего удержания ответов 200).
			first, last := res[0].sent, res[0].sent
			for _, r := range res {
				if r.sent.Before(first) {
					first = r.sent
				}
				if r.sent.After(last) {
					last = r.sent
				}
			}
			spread := last.Sub(first).Milliseconds()
			// Удержание — срок отметки: на быстром пути Mihomo total фиксируется при
			// ответе, а отметка держится до конца фонового прохода (tail); на других
			// путях tail = 0.
			hold := int64(-1)
			for _, g := range okGroups {
				if h := stages[g].num("total") - stages[g].num("model") + stages[g].num("tail"); hold < 0 || h < hold {
					hold = h
				}
			}
			guaranteed = len(okGroups) > 0 && spread < hold
			lastSpread, lastHold = spread, hold
			t.Logf("d17-overlap: core=%s attempt=%d spread=%d hold=%d guaranteed=%t", tg.Core, attempt, spread, hold, guaranteed)
			if !guaranteed {
				// Без наложения два 200 в компоненте законны: проверены только
				// apply_busy и 5xx, цели следующей попытки — от текущего выбора.
				continue
			}

			// Вердикт по D-17. Компонента {A, B, C}: ровно один 200, остальные —
			// select_busy быстрее selectBusyWait от своей отправки (без очереди).
			winner, winners := -1, 0
			for i := 0; i < 3; i++ {
				if res[i].status == http.StatusOK {
					winner = i
					winners++
					continue
				}
				if resp[i].Status != http.StatusConflict || resp[i].Code != "select_busy" {
					t.Errorf("D-17: выбор №%d в компоненте: %d %q, ожидалось 409 select_busy", i+1, resp[i].Status, resp[i].Code)
				} else if wait := res[i].done.Sub(res[i].sent); wait >= rtRtSelectBusyWait {
					t.Errorf("D-17: select_busy выбора №%d через %d мс, ожидалось быстрее %d мс", i+1, wait.Milliseconds(), rtRtSelectBusyWait.Milliseconds())
				}
			}
			if winners != 1 {
				t.Fatalf("D-17: в компоненте «Выбор»+«Связанная» ответов 200 %d, ожидался один", winners)
			}
			for i, r := range res {
				if r.status != http.StatusOK {
					if i >= 3 {
						t.Errorf("D-17: независимый выбор №%d группы %s: %d %s, ожидалось 200", i+1, jobs[i][0], resp[i].Status, resp[i].Code)
					}
					continue
				}
				obj := resp[i].object(t)
				rtAssertContractKeys(t, contract, obj, "SelectResult", "select (D-17)")
				ks, _ := obj["kernels"].(map[string]any)
				if out, _ := ks[tg.Core].(string); out != "applied" {
					t.Errorf("D-17: выбор №%d группы %s: исход активного ядра %q, ожидалось applied", i+1, jobs[i][0], out)
				}
				if reason, present := obj["restart_reason"]; present && tg.Core == "xray" {
					t.Errorf("D-17: выбор №%d группы %s потребовал перезапуск Xray: restart_reason=%v", i+1, jobs[i][0], reason)
				}
			}
			// Три одновременных прохода не начинаются сразу: порядок запусков их
			// развёл (wait > 0) или собрал в пачку (batch ≥ 2).
			ordered := false
			for _, g := range okGroups {
				if stages[g].num("wait") > 0 || stages[g].num("batch") >= 2 {
					ordered = true
				}
			}
			if !ordered {
				t.Error("D-17: ни одна строка этапов ответов 200 не несёт wait > 0 или batch ≥ 2")
			}

			// Итог: выборы победителя и независимых групп применены, цель
			// проигравших нажатий — нет (очереди нет), вторая группа компоненты прежняя.
			winName := "Выбор"
			if winner == 2 {
				winName = "Связанная"
			}
			want := map[string]string{"Выбор": cur["Выбор"], "Связанная": cur["Связанная"], "Вторая": d, "Третья": ee}
			want[winName] = jobs[winner][1]
			after, _, afterLbl := d17Groups()
			for _, name := range names {
				if after[name] != want[name] {
					t.Errorf("D-17: current группы %s не равен ожидаемому (выбор применён=%t, прежний=%t)", ids[name], after[name] == jobs[winner][1], after[name] == cur[name])
				}
			}
			changed := map[string]bool{winName: true, "Вторая": true, "Третья": true}
			switch tg.Core {
			case "xray":
				for _, name := range names {
					ob, ok := rtRtGroupOutbound(t, ids[name])
					switch {
					case !ok:
						t.Errorf("D-17: исходящий группы %s пропал", ids[name])
					case changed[name] && reflect.DeepEqual(ob, obBefore[name]):
						t.Errorf("D-17: исходящий группы %s не сменился после выбора", ids[name])
					case changed[name] && !rtRtMatchesNode(t, ob):
						t.Errorf("D-17: исходящий группы %s не совпадает ни с одним узлом «Моих узлов»", ids[name])
					case !changed[name] && strings.HasPrefix(cur[name], "node:") && !reflect.DeepEqual(ob, obBefore[name]):
						// Группа, выбравшая группу, повторяет её исходящий: сравнение
						// осмысленно, только когда её выбор — узел.
						t.Errorf("D-17: исходящий неизменённой группы %s сменился", ids[name])
					}
				}
				pidsAfter, _ := rtRstKernelPIDs("xray")
				if len(pidsD17) != 1 || len(pidsAfter) != 1 || pidsAfter[0] != pidsD17[0] {
					t.Errorf("D-17: PID Xray прежний=%t, процессов до %d, после %d", reflect.DeepEqual(pidsAfter, pidsD17), len(pidsD17), len(pidsAfter))
				}
			case "mihomo":
				for name := range changed {
					l := afterLbl[name][want[name]]
					if now := e.rtRtWaitNow(name, l); now != l {
						t.Errorf("D-17: now группы %s в Mihomo не равен выбранному узлу (длина now %d)", ids[name], len(now))
					}
				}
			}
			// Потерянного обновления xcp-groups нет: после всех ответов ни одна
			// группа не переключается и не показана неподтверждённой.
			rv := runtimeView()
			for _, name := range names {
				rg := rtRtFindGroup(rv, name)
				if rg == nil {
					t.Errorf("D-17: нет группы %s в виде «Прокси»", ids[name])
					continue
				}
				if rg["switching"] != false || rg["unconfirmed"] != false {
					if rg["unconfirmed"] != false {
						// Причина остаточного маркера — по журналу панели блока: строки
						// с ID группы о сбое Clash API, незаписанном маркере Mihomo и
						// неснятом маркере Xray.
						id, lines := ids[name], rtRtLogLines(d17Offset)
						t.Logf("d17-marker: group=%s clash_fail=%d mihomo_mark_fail=%d xray_mark_left=%d", id,
							rtRtCountLines(lines, []string{id}, "выбор Mihomo не поставлен"),
							rtRtCountLines(lines, []string{id}, "маркер Mihomo не записан"),
							rtRtCountLines(lines, []string{id}, "маркер Xray не снят"))
					}
					t.Errorf("D-17: группа %s после выборов switching=%v unconfirmed=%v", ids[name], rg["switching"], rg["unconfirmed"])
				}
			}
			if e.rtRtDraftChanges() != draftBefore {
				t.Error("D-17: параллельные выборы изменили черновик")
			}

			// Снимки опроса: идущий выбор виден switching без unconfirmed у обеих
			// групп компоненты. Обязателен, когда удержание победителя не короче
			// четырёх периодов опроса.
			var maxGet time.Duration
			seen, masked := false, 0
			for _, s := range snaps {
				maxGet = max(maxGet, s.dur)
				if s.switching[gid] && s.switching[linkedID] && !s.unconf[gid] && !s.unconf[linkedID] {
					seen = true
				}
				for _, id := range ids {
					if s.switching[id] && s.unconf[id] {
						masked++
					}
				}
			}
			if masked > 0 {
				t.Errorf("D-16: идущий выбор показан неподтверждённым в %d снимках вида", masked)
			}
			winHold := stages[jobs[winner][0]].num("total") - stages[jobs[winner][0]].num("model")
			period := (rtRtSnapPoll + maxGet).Milliseconds()
			if winHold >= 4*period {
				if !seen {
					t.Errorf("D-17: ни в одном из %d снимков вида обе группы компоненты не были switching без unconfirmed (удержание %d мс, период %d мс)", len(snaps), winHold, period)
				}
			} else {
				t.Logf("D-17: окно короче опроса: удержание %d мс, период опроса %d мс, снимков %d, switching увиден=%t", winHold, period, len(snaps), seen)
			}
		}
		if !guaranteed {
			t.Errorf("D-17: наложение не обеспечено: разброс отправки не меньше прохода (spread=%d мс, hold=%d мс, попыток 3)", lastSpread, lastHold)
		}
		if tg.Core == "mihomo" {
			// Положительное свидетельство пробела 3 (истина 35): отсутствие ошибки
			// D-17 в одном прогоне не отличает исправление от удачи. fast_ok — ответы
			// 200 быстрого пути (clash > 0) с успешным исходом; reput=done у такого
			// хвоста — повтор PUT подтверждённого значения, давший остаточный маркер.
			var groups []string
			for _, id := range ids {
				groups = append(groups, id)
			}
			lines := rtRtLogLines(d17Offset)
			putFail := rtRtCountLines(lines, groups, "выбор Mihomo не поставлен")
			markFail := rtRtCountLines(lines, groups, "маркер Mihomo не записан")
			evidence := "none"
			switch {
			case evDoneOK > 0:
				evidence = "regressed"
			case evFastOK > 0:
				evidence = "fixed"
			}
			t.Logf("d17-evidence: core=%s fast_ok=%d reput_skip=%d reput_done=%d reput_done_ok=%d mihomo_put_fail=%d mihomo_mark_fail=%d evidence=%s",
				tg.Core, evFastOK, evSkip, evDone, evDoneOK, putFail, markFail, evidence)
			if evDoneOK > 0 {
				t.Errorf("D-17: успешный хвост быстрого пути повторил PUT подтверждённого значения (%d строк): путь остаточного маркера (истина 35) исполнен снова", evDoneOK)
			}
		}
	}
	// Граница времени выбора на ядре mihomo (план 150-19): ответ после Clash API,
	// без ожидания прохода файла Xray. Медианы строк одиночного выбора и барьера.
	if tg.Core == "mihomo" && xrayPath {
		medTotal, medHTTP := rtRtMedian(mihomoTotals), rtRtMedian(mihomoHTTP)
		t.Logf("mihomo-select-budget: lines=%d total_med=%.1f http_med=%.1f total_budget=%d http_budget=%d",
			len(mihomoTotals), medTotal, medHTTP, rtRtMihomoSelectBudget.Milliseconds(), rtRtMihomoHTTPBudget.Milliseconds())
		// Разбор пробела 2: какая часть времени — панель, какая — сам Mihomo. Строка
		// разбора, не проверка: порогов и меток здесь нет, θM — для правил 150-38.
		p := mihomoParts
		t.Logf("mihomo-select-parts: lines=%d model_med=%.1f pre_med=%.1f wait_med=%.1f plan_med=%.1f put_med=%.1f panel_med=%.1f theta=%d",
			len(p.panel), rtRtMedian(p.model), rtRtMedian(p.pre), rtRtMedian(p.wait), rtRtMedian(p.plan),
			rtRtMedian(p.put), rtRtMedian(p.panel), rtRtMihomoPanelTheta.Milliseconds())
		// Метка Known 150-22-mihomo-select-clash-put-slow снята по XPASS сессии 150-37;
		// граница 150-19 (пробел 2) остаётся вопросом владельца.
		mark := routertest.KnownMark{}
		if len(mihomoTotals) == 0 {
			t.Errorf("время выбора Mihomo: нет строк этапов выборов, граница %d мс", rtRtMihomoSelectBudget.Milliseconds())
		} else {
			// Обе границы — один вердикт: время плавает между прогонами (носитель), и по
			// одной из них метка снималась бы случайно.
			ok := medTotal <= float64(rtRtMihomoSelectBudget.Milliseconds()) &&
				(len(mihomoHTTP) == 0 || medHTTP <= float64(rtRtMihomoHTTPBudget.Milliseconds()))
			routertest.Verdict(t, ok, mark,
				"время выбора Mihomo: медиана total %.1f мс по %d строкам (граница %d мс), медиана http %.1f мс по %d выборам (граница %d мс)",
				medTotal, len(mihomoTotals), rtRtMihomoSelectBudget.Milliseconds(), medHTTP, len(mihomoHTTP), rtRtMihomoHTTPBudget.Milliseconds())
		}
	}

	// Последующие проверки после «Применить» идут по текущему выбору «Выбор»: он
	// отличен от startMember, поэтому сравнение с xrayBefore остаётся верным.
	if g := rtRtFindGroup(e.rtRtView(), "Выбор"); g != nil {
		target, _ = g["current"].(string)
	}
	if target == startMember || !strings.HasPrefix(target, "node:") {
		t.Fatalf("D-17: current «Выбор» после блока совпал с исходным=%t, узел=%t", target == startMember, strings.HasPrefix(target, "node:"))
	}

	// Правка правила и «Применить»: выбор остаётся.
	rs := e.call(http.MethodPost, rtRoutingBase+"/rules/save", map[string]any{
		"revision": e.rtRtRevision(),
		"rule": map[string]any{
			"enabled": true,
			"conds":   []map[string]any{{"type": "domain", "values": []string{"direct.example.test"}}},
			"target":  map[string]string{"kind": "direct"},
		},
	})
	if rs.Status != http.StatusOK {
		t.Fatalf("rules/save: %d %s", rs.Status, rs.Code)
	}
	e.rtRtApplyOK("после правки правила")
	group = rtRtFindGroup(e.rtRtView(), "Выбор")
	if group == nil || group["current"] != target {
		t.Errorf("после «Применить» current не равен выбранному участнику")
	}
	switch tg.Core {
	case "mihomo":
		if now := e.rtRtWaitNow("Выбор", labels[target]); now != labels[target] {
			t.Errorf("Mihomo: после «Применить» now группы не равен выбранному узлу (длина now %d)", len(now))
		}
	case "xray":
		again, ok := rtRtGroupOutbound(t, gid)
		if !ok || reflect.DeepEqual(again, xrayBefore) || !rtRtMatchesNode(t, again) {
			t.Error("Xray: после «Применить» исходящий группы не соответствует выбранному узлу")
		}
	}

	// Пара ожидания без порога времени (D-17, условие отказа G-150-3): выбор
	// независимой «Вторая», отправленный, пока идёт проход «Выбор», получает 200 без
	// apply_busy, current обеих групп — цели. Идёт после проверок «Применить»: цель
	// «Выбор» пары может совпасть по подписи с исходным выбором. До трёх попыток,
	// пока ожидание строки «Вторая» не станет не меньше selectBusyWait; короткий
	// проход — executed=false в строке d17-longwait, не ошибка. Без прохода Xray
	// строк этапов нет — пара не исполняется.
	if xrayPath {
		for attempt := 1; attempt <= 3; attempt++ {
			w, second := e.rtRtLongwaitPair(tg.Core, "longwait-live", gid, secondID, nil, runtimeView)
			maxWait = max(maxWait, w)
			t.Logf("longwait-live: core=%s attempt=%d second_wait=%d busy_wait=%d", tg.Core, attempt, second, rtRtSelectBusyWait.Milliseconds())
			if second >= rtRtSelectBusyWait.Milliseconds() {
				break
			}
		}
	} else {
		t.Logf("longwait-live: core=%s skipped=no_xray_path", tg.Core)
	}

	// Условие отказа G-150-3: выбор ждал чужой проход дольше selectBusyWait (2 с) и
	// принят 200 — прежняя реализация через 2 с отдала бы apply_busy. Короткий
	// проход (на loopback) — не регрессия: строка показывает, исполнено ли условие.
	t.Logf("d17-longwait: core=%s maxwait=%d busy_wait=%d executed=%t",
		tg.Core, maxWait, rtRtSelectBusyWait.Milliseconds(), maxWait >= rtRtSelectBusyWait.Milliseconds())
}

// --- каталог списков, «Добавить в XKeen» и пресеты -----------------------------

// rtRtHasRulesFromPreset сообщает, что в представлении есть правило с пометкой
// «из пресета».
func rtRtHasRulesFromPreset(view map[string]any) bool {
	rules, _ := view["rules"].([]any)
	for _, r := range rules {
		if m, _ := r.(map[string]any); m != nil && m["origin"] == "preset" {
			return true
		}
	}
	return false
}

// Каталог списков, пресеты и «Добавить в XKeen» на настоящей панели: ответы совпадают
// с контрактом, пресет без взятия правит только черновик, файл настроек XKeen
// предпросмотр не меняет, неизвестный набор и адрес от клиента отклоняются. Метки
// ядра нет: тест меняет только черновик и не читает активное ядро.
func TestRouterRoutingPresetsAndLists(t *testing.T) {
	e := rtNewSrc(t)
	e.freshDraft()
	e.rtRstIdle()
	e.rtRtPurge()
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	// Черновик с пресетом и узлом не применяется: при выходе узлы и секции
	// убираются из черновика, затем freshDraft сбрасывает его к применённому.
	t.Cleanup(func() { e.with(t).rtRtPurge() })
	e.rtRstAddMyNode(rtRtUniqueLink(t))

	// GET /routing/lists: сервисы и страны из dat стенда, ключи по контракту.
	lr := e.call(http.MethodGet, rtRoutingBase+"/lists", nil)
	if lr.Status != http.StatusOK {
		t.Fatalf("GET lists: %d %s", lr.Status, lr.Code)
	}
	catalog := lr.object(t)
	rtAssertContractKeys(t, contract, catalog, "CatalogView", "lists")
	if svc, _ := catalog["services"].([]any); len(svc) == 0 {
		t.Error("lists: services пуст при списках dat на стенде")
	}
	if cs, _ := catalog["countries"].([]any); len(cs) == 0 {
		t.Error("lists: countries пуст при списках dat на стенде")
	}
	setFile := ""
	for _, x := range catalog["extended"].([]any) {
		if m, _ := x.(map[string]any); m != nil && m["id"] == "roscomvpn_geosite" {
			setFile, _ = m["file"].(string)
		}
	}
	if setFile == "" {
		t.Fatal("lists: в extended нет набора roscomvpn_geosite")
	}

	// GET /routing/presets: есть пресеты, среди стран есть RU.
	pr := e.call(http.MethodGet, rtRoutingBase+"/presets", nil)
	if pr.Status != http.StatusOK {
		t.Fatalf("GET presets: %d %s", pr.Status, pr.Code)
	}
	presets := pr.object(t)
	rtAssertContractKeys(t, contract, presets, "PresetsView", "presets")
	hasRU := false
	for _, p := range presets["presets"].([]any) {
		cs, _ := p.(map[string]any)["countries"].([]any)
		for _, c := range cs {
			if c == "RU" {
				hasRU = true
			}
		}
	}
	if !hasRU {
		t.Error("presets: среди стран пресетов нет RU")
	}

	// presets/plan для «всё через прокси»: правила на добавление есть, черновик не меняется.
	choice := map[string]any{"id": "all_proxy", "flags": map[string]bool{"quic": false, "torrent": false, "ads": false, "service_groups": false}}
	revBefore := e.rtRtRevision()
	pl := e.call(http.MethodPost, rtRoutingBase+"/presets/plan", map[string]any{"choice": choice})
	if pl.Status != http.StatusOK {
		t.Fatalf("presets/plan: %d %s", pl.Status, pl.Code)
	}
	plan := pl.object(t)
	rtAssertContractKeys(t, contract, plan, "PresetPlan", "presets/plan")
	if add, _ := plan["add"].([]any); len(add) == 0 {
		t.Error("presets/plan: add пуст для all_proxy")
	}
	if got := e.rtRtRevision(); got != revBefore {
		t.Errorf("presets/plan изменил ревизию черновика: %d → %d", revBefore, got)
	}

	// Коды отказа пресета.
	e.rtRtExpect("presets/plan: неизвестный пресет", e.call(http.MethodPost, rtRoutingBase+"/presets/plan",
		map[string]any{"choice": map[string]any{"id": "nope"}}), http.StatusBadRequest, "preset_unknown")
	e.rtRtExpect("presets/plan: страна без списков", e.call(http.MethodPost, rtRoutingBase+"/presets/plan",
		map[string]any{"choice": map[string]any{"id": "except_country", "country": "ZZ"}}), http.StatusBadRequest, "country_unavailable")
	e.rtRtExpect("presets/apply: старая ревизия", e.call(http.MethodPost, rtRoutingBase+"/presets/apply",
		map[string]any{"revision": 0, "choice": choice, "take": false}), http.StatusConflict, "draft_conflict")

	// presets/apply без взятия: правила «из пресета» в представлении, управление не взято.
	ap := e.call(http.MethodPost, rtRoutingBase+"/presets/apply", map[string]any{"revision": e.rtRtRevision(), "choice": choice, "take": false})
	if ap.Status != http.StatusOK {
		t.Fatalf("presets/apply: %d %s", ap.Status, ap.Code)
	}
	if ev := ap.object(t); ev["draft_revision"] == nil {
		t.Error("presets/apply: в ответе нет draft_revision")
	}
	view := e.rtRtView()
	if !rtRtHasRulesFromPreset(view) {
		t.Error("представление: после presets/apply нет правил с origin=preset")
	}
	if on, _ := view["desired"].(bool); on {
		t.Error("представление: presets/apply с take=false взял маршрутизацию")
	}
	pr = e.call(http.MethodGet, rtRoutingBase+"/presets", nil)
	if cur, _ := pr.object(t)["current"].(map[string]any); cur == nil || cur["id"] != "all_proxy" {
		t.Error("presets: current не показывает применённый пресет")
	}
}

// --- маршруты -----------------------------------------------------------------

var rtRoutingRoutes = []struct{ method, path string }{
	{http.MethodGet, rtRoutingBase},
	{http.MethodPost, rtRoutingBase + "/groups/save"},
	{http.MethodPost, rtRoutingBase + "/groups/preview"},
	{http.MethodPost, rtRoutingBase + "/groups/delete"},
	{http.MethodPost, rtRoutingBase + "/impact"},
	{http.MethodPost, rtRoutingBase + "/rules/save"},
	{http.MethodPost, rtRoutingBase + "/rules/delete"},
	{http.MethodPost, rtRoutingBase + "/rules/move"},
	{http.MethodPost, rtRoutingBase + "/rules/enable"},
	{http.MethodPost, rtRoutingBase + "/final"},
	{http.MethodPost, rtRoutingBase + "/probe"},
	{http.MethodPost, rtRoutingBase + "/take"},
	{http.MethodPost, rtRoutingBase + "/release"},
	{http.MethodGet, rtRoutingBase + "/takeover"},
	{http.MethodPost, rtRoutingBase + "/reclaim"},
	{http.MethodPost, rtRoutingBase + "/mihomo"},
	{http.MethodPost, rtRoutingBase + "/select"},
	{http.MethodGet, rtRoutingBase + "/lists"},
	{http.MethodGet, rtRoutingBase + "/presets"},
	{http.MethodPost, rtRoutingBase + "/presets/plan"},
	{http.MethodPost, rtRoutingBase + "/presets/apply"},
	{http.MethodPost, rtRoutingBase + "/schedule/save"},
	{http.MethodPost, rtRoutingBase + "/schedule/delete"},
	{http.MethodPost, rtRoutingBase + "/schedule/enabled"},
	{http.MethodPost, rtRoutingBase + "/schedule/restore"},
	{http.MethodGet, "/api/configlayer/proxies"},
	{http.MethodGet, "/api/configlayer/proxies/now"},
	{http.MethodGet, "/api/configlayer/proxies/results"},
	{http.MethodPost, "/api/configlayer/proxies/ping"},
	{http.MethodPost, "/api/configlayer/proxies/ping/cancel"},
	{http.MethodPost, "/api/configlayer/proxies/confirm"},
}

// rtLimitsRoutes — маршруты лимитов трафика (152, вкладка «Трафик → Лимиты»).
var rtLimitsRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/traffic/limits"},
	{http.MethodPost, "/api/traffic/limits/save"},
	{http.MethodPost, "/api/traffic/limits/delete"},
	{http.MethodPost, "/api/traffic/limits/enabled"},
	{http.MethodPost, "/api/traffic/limits/reset"},
	{http.MethodGet, "/api/traffic/limits/alerts"},
	{http.MethodPost, "/api/traffic/limits/alerts/clear"},
}

// Без сессии маршруты отвечают 401, изменяющие без X-CSRF-Token — 403; метод не из
// контракта — 405 (маршрутизация, расписание и лимиты). Ответы читаемых маршрутов
// совпадают с routingContract.json по ключам (вид «Прокси» — с clock, schedule и
// hold). Действия лимита throttle и log_only не принимаются (SPX-04), снятые
// маршруты Smart Proxy, квот и прежних оповещений — обычный 404 (151 D-12).
func TestRouterRoutingProtected(t *testing.T) {
	e := rtNewSrc(t)
	e.freshDraft()
	routes := append(append([]struct{ method, path string }{}, rtRoutingRoutes...), rtLimitsRoutes...)
	for _, rt := range routes {
		var body []byte
		if rt.method == http.MethodPost {
			body = []byte("{}")
		}
		status, _, err := e.p.Request(rt.method, rt.path, body, routertest.AuthNone)
		if err != nil {
			t.Fatalf("%s %s без сессии: %v", rt.method, rt.path, err)
		}
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s без сессии: статус %d, ожидался 401", rt.method, rt.path, status)
		}
		if rt.method != http.MethodPost {
			continue
		}
		status, _, err = e.p.Request(rt.method, rt.path, body, routertest.AuthNoCSRF)
		if err != nil {
			t.Fatalf("%s %s без CSRF: %v", rt.method, rt.path, err)
		}
		if status != http.StatusForbidden {
			t.Errorf("%s %s без X-CSRF-Token: статус %d, ожидался 403", rt.method, rt.path, status)
		}
	}
	for _, rt := range routes {
		other := http.MethodGet
		if rt.method == http.MethodGet {
			other = http.MethodPost
		}
		r := e.call(other, rt.path, rtBody(other))
		if r.Status != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: статус %d, ожидался 405", other, rt.path, r.Status)
		}
	}

	// Ключи ответов читаемых маршрутов — по контракту.
	contract := rtLoadContractFile(t, routertest.RoutingContractFile(t))
	view := e.call(http.MethodGet, rtRoutingBase, nil)
	if view.Status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", rtRoutingBase, view.Status, view.Code)
	}
	rtAssertContractKeys(t, contract, view.object(t), "RoutingView", "routing")
	tp := e.call(http.MethodGet, rtRoutingBase+"/takeover", nil)
	if tp.Status != http.StatusOK {
		t.Fatalf("GET %s/takeover: %d %s", rtRoutingBase, tp.Status, tp.Code)
	}
	rtAssertContractKeys(t, contract, tp.object(t), "TakeoverPreview", "takeover")
	proxies := e.call(http.MethodGet, "/api/configlayer/proxies", nil)
	if proxies.Status != http.StatusOK {
		t.Fatalf("GET /api/configlayer/proxies: %d %s", proxies.Status, proxies.Code)
	}
	rtAssertContractKeys(t, contract, proxies.object(t), "RuntimeView", "proxies")
	results := e.call(http.MethodGet, "/api/configlayer/proxies/results", nil)
	if results.Status != http.StatusOK {
		t.Fatalf("GET /api/configlayer/proxies/results: %d %s", results.Status, results.Code)
	}
	rtAssertContractKeys(t, contract, results.object(t), "PingSnapshot", "proxies/results")

	// Лимиты: список отвечает при любом активном ядре; throttle и log_only — 400
	// action_invalid (SPX-04), лимит не создаётся.
	if r := e.call(http.MethodGet, "/api/traffic/limits", nil); r.Status != http.StatusOK {
		t.Errorf("GET /api/traffic/limits: %d %s", r.Status, r.Code)
	}
	for _, action := range []string{"throttle", "log_only"} {
		r := e.call(http.MethodPost, "/api/traffic/limits/save", map[string]any{
			"name": "rt " + action, "target_type": "all", "limit_bytes": 1 << 30,
			"period": "monthly", "threshold": 80, "action": action, "enabled": true,
		})
		e.rtRtExpect("limits/save: действие "+action, r, http.StatusBadRequest, "action_invalid")
	}

	// Снятые маршруты — обычный 404 общего обработчика.
	for _, path := range []string{"/api/smart-proxy/profiles", "/api/traffic/quotas", "/api/traffic/alerts"} {
		e.rtRtExpect("снятый маршрут "+path, e.call(http.MethodGet, path, nil), http.StatusNotFound, "")
	}
}
