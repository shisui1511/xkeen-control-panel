//go:build router

// router-area: routing

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Выбор узла Xray при настоящих сбоях на устройстве: порт API Xray закрыт
// правилом iptables, в каталоге Xray лежит чужой файл, который отвергает
// `xray run -test`, процесс Xray убивается сразу после горячей замены. Вердикт —
// ответ POST routing/select, итог прохода в GET /api/configlayer/state, строки
// журнала панели, процессы Xray в /proc (pidof, независимо от панели), исходящие
// групп в файлах каталога Xray и выбор в представлении панели. Моков и подмены
// хуков нет. В журнал теста — только коды, PID и числа: подписи узлов, адреса и
// содержимое файлов не печатаются.

const (
	// rtFltBadFile — чужой файл Xray с исходящим неизвестного протокола: `xray run
	// -test` его отвергает, а секцией панели ключ outbounds не является, поэтому
	// дрейфом взятой секции файл не считается.
	rtFltBadFile = "98_rt_fault_bad.json"
	// rtFltAPIFile — управляемый файл блока API панели в каталоге Xray.
	rtFltAPIFile = "99_xcp-api.json"
	// rtFltAttempts — попыток поймать окно подтверждения горячей замены.
	rtFltAttempts = 3
	// rtFltKillPoll — шаг чтения журнала панели горутиной убийства.
	rtFltKillPoll = 20 * time.Millisecond
	// rtFltHotMark — строка журнала после горячей замены (routing/hotswap.go).
	rtFltHotMark = "[routing] горячая замена Xray: тегов"
	// rtFltNoAPIMark — строка конвейера о неприменимой горячей замене: порт API
	// не отвечает (причина xray_hot_no_api).
	rtFltNoAPIMark = "горячая замена Xray: причина=xray_hot_no_api"
	// rtFltRollingNote — пояснение шага restart при откате файлов
	// (NoteRestartFailedRolling): событие уходит до отката и подъёма ядра.
	rtFltRollingNote = "restart_failed_rolling_back"
	// rtFltXrayWait — предел ожидания работающего Xray после запуска.
	rtFltXrayWait = 2 * time.Minute
)

// rtFltBadBody — содержимое чужого отвергаемого файла.
var rtFltBadBody = []byte(`{"outbounds":[{"tag":"rt-fault-bad","protocol":"no-such-protocol"}]}` + "\n")

// rtFltBadPath — путь чужого файла в каталоге Xray.
func rtFltBadPath() string { return filepath.Join(rtRtXrayDir, rtFltBadFile) }

// rtFltPutBad кладёт чужой отвергаемый файл.
func rtFltPutBad(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(rtFltBadPath(), rtFltBadBody, 0o600); err != nil {
		t.Fatalf("чужой файл Xray не записан: %v", err)
	}
}

// rtFltDropBad удаляет чужой файл, если он есть; true — файла больше нет.
func rtFltDropBad() bool {
	err := os.Remove(rtFltBadPath())
	return err == nil || os.IsNotExist(err)
}

// rtFltBadPresent сообщает, что чужой файл лежит в каталоге.
func rtFltBadPresent() bool {
	_, err := os.Stat(rtFltBadPath())
	return err == nil
}

// rtFltAPIPort — порт API Xray из управляемого файла блока API панели.
func rtFltAPIPort(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rtRtXrayDir, rtFltAPIFile))
	if err != nil {
		t.Fatalf("нет файла блока API панели: горячая замена на стенде невозможна: %v", err)
	}
	var doc struct {
		API struct {
			Listen string `json:"listen"`
		} `json:"api"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("файл блока API панели не разбирается: %v", err)
	}
	_, port, err := net.SplitHostPort(doc.API.Listen)
	if err != nil || port == "" {
		t.Fatalf("в файле блока API панели нет адреса api.listen с портом (длина %d)", len(doc.API.Listen))
	}
	return port
}

// rtFltIptables выполняет iptables -w с аргументами; код выхода.
func rtFltIptables(args ...string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _, code := routertest.Exec(ctx, "iptables", append([]string{"-w"}, args...)...)
	return code
}

// rtFltFilterRules — строки правил (-A) таблицы filter из iptables-save.
func rtFltFilterRules(t *testing.T) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, _, code := routertest.Exec(ctx, "iptables-save", "-t", "filter")
	if code != 0 {
		t.Fatalf("iptables-save -t filter: код %d", code)
	}
	var rules []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-A ") {
			rules = append(rules, strings.TrimSpace(line))
		}
	}
	return rules
}

// rtFltPortRules — правила из списка, которые касаются порта port.
func rtFltPortRules(rules []string, port string) []string {
	var out []string
	for _, r := range rules {
		if strings.Contains(r, "--dport "+port+" ") || strings.HasSuffix(r, "--dport "+port) {
			out = append(out, r)
		}
	}
	return out
}

// rtFltBlockAPI закрывает порт API Xray на loopback: REJECT с tcp-reset во
// входящей цепочке (без цели REJECT — DROP). unblock снимает правило (повторный
// вызов безопасен); t.Cleanup снимает его при любом исходе.
func rtFltBlockAPI(t *testing.T, port string) (unblock func()) {
	t.Helper()
	rule := []string{"INPUT", "-i", "lo", "-p", "tcp", "--dport", port, "-j", "REJECT", "--reject-with", "tcp-reset"}
	if rtFltIptables(append([]string{"-I"}, rule...)...) != 0 {
		rule = []string{"INPUT", "-i", "lo", "-p", "tcp", "--dport", port, "-j", "DROP"}
		if rtFltIptables(append([]string{"-I"}, rule...)...) != 0 {
			t.Fatal("правило iptables на порт API Xray не поставлено (REJECT и DROP)")
		}
		t.Log("cr02: цель REJECT недоступна, порт API закрыт правилом DROP")
	}
	if rtFltIptables(append([]string{"-C"}, rule...)...) != 0 {
		t.Fatal("правило iptables на порт API Xray не найдено после вставки")
	}
	done := false
	unblock = func() {
		if done {
			return
		}
		done = true
		for i := 0; i < 5 && rtFltIptables(append([]string{"-C"}, rule...)...) == 0; i++ {
			rtFltIptables(append([]string{"-D"}, rule...)...)
		}
		if rtFltIptables(append([]string{"-C"}, rule...)...) == 0 {
			t.Error("правило iptables на порт API Xray не снято")
		}
	}
	t.Cleanup(unblock)
	return unblock
}

// rtFltSel — разобранный ответ POST routing/select: только коды и числа.
type rtFltSel struct {
	status      int
	code        string
	xray        string
	reason      string
	reverted    bool
	unconfirmed int
}

func (s rtFltSel) String() string {
	return fmt.Sprintf("status=%d code=%q xray=%q reason=%q reverted=%t unconfirmed=%d",
		s.status, s.code, s.xray, s.reason, s.reverted, s.unconfirmed)
}

// rtFltSelect выбирает участника ручной группы через API панели.
func (e *rtSrc) rtFltSelect(gid, member string) rtFltSel {
	e.t.Helper()
	r := e.callSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": member})
	s := rtFltSel{status: r.Status, code: r.Code}
	if r.Status != http.StatusOK {
		return s
	}
	obj := r.object(e.t)
	ks, _ := obj["kernels"].(map[string]any)
	s.xray, _ = ks["xray"].(string)
	s.reason, _ = obj["restart_reason"].(string)
	s.reverted, _ = obj["reverted"].(bool)
	un, _ := obj["unconfirmed"].([]any)
	s.unconfirmed = len(un)
	return s
}

// rtFltCurrent — выбор группы в представлении маршрутизации (Live).
func (e *rtSrc) rtFltCurrent(name string) string {
	e.t.Helper()
	g := rtRtFindGroup(e.rtRtView(), name)
	if g == nil {
		e.t.Fatalf("представление: нет группы %s", name)
	}
	cur, _ := g["current"].(string)
	return cur
}

// rtFltRuntime — группа вида «Прокси»: выбор и маркер неподтверждённого выбора.
func (e *rtSrc) rtFltRuntime(name string) (current string, unconfirmed bool) {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/proxies", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("proxies: %d %s", r.Status, r.Code)
	}
	g := rtRtFindGroup(r.object(e.t), name)
	if g == nil {
		e.t.Fatalf("proxies: нет группы %s", name)
	}
	current, _ = g["current"].(string)
	unconfirmed, _ = g["unconfirmed"].(bool)
	return current, unconfirmed
}

// rtFltXrayPID — PID единственного процесса Xray (0 — процесса нет или их несколько).
func rtFltXrayPID() int {
	pids, _ := rtRstKernelPIDs("xray")
	if len(pids) != 1 {
		return 0
	}
	return pids[0]
}

// rtFltStartXray запускает ядро тем же путём, что «Запустить» на «Службах».
func (e *rtSrc) rtFltStartXray() {
	e.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		r := e.callSlow(http.MethodPost, "/api/service/control?action=start", nil)
		if r.Status == http.StatusOK {
			return
		}
		busy := r.Status == http.StatusConflict && (r.Code == "kernel_op_in_progress" || r.Code == "apply_busy")
		if !busy || time.Now().After(deadline) {
			e.t.Fatalf("запуск ядра через API служб: %d %s", r.Status, r.Code)
		}
		time.Sleep(time.Second)
	}
}

// rtFltEnsureXray ждёт работающий Xray; нет процесса 15 с — запускает ядро через
// API панели и ждёт готовности. Возвращает PID.
func (e *rtSrc) rtFltEnsureXray() int {
	e.t.Helper()
	up, _ := routertest.Poll(15*time.Second, time.Second, func() (bool, string) { return rtFltXrayPID() > 0, "" })
	if !up {
		e.rtFltStartXray()
	}
	return routertest.WaitKernelReady(e.t, "xray", rtFltXrayWait)
}

// rtFltConfirmIfMarked снимает маркер неподтверждённого выбора группы
// «Повторить» (POST /api/configlayer/proxies/confirm), если он есть.
func (e *rtSrc) rtFltConfirmIfMarked(name, gid string) {
	e.t.Helper()
	if _, marked := e.rtFltRuntime(name); !marked {
		return
	}
	r := e.callSlow(http.MethodPost, "/api/configlayer/proxies/confirm", map[string]string{"group": gid})
	if r.Status != http.StatusOK {
		e.t.Errorf("«Повторить» группы %s: %d %s", gid, r.Status, r.Code)
	}
}

// rtFltOutbounds — исходящие внутренней и внешней групп в файлах Xray (без тега).
func rtFltOutbounds(t *testing.T, gid, outerID string) (inner, outer map[string]any) {
	t.Helper()
	inner, ok := rtRtGroupOutbound(t, gid)
	if !ok {
		t.Fatalf("Xray: нет исходящего группы %s", gid)
	}
	outer, ok = rtRtGroupOutbound(t, outerID)
	if !ok {
		t.Fatalf("Xray: нет исходящего группы %s", outerID)
	}
	return inner, outer
}

// rtFltApplyCode — код итога, признак отката файлов и исход Xray последнего
// прохода конвейера.
func (e *rtSrc) rtFltApplyCode() (code string, rolledBack bool, xray string) {
	e.t.Helper()
	a := e.rtRstApplyView()
	_, code = rtRstResult(a)
	res, _ := a["result"].(map[string]any)
	rolledBack, _ = res["rolled_back"].(bool)
	return code, rolledBack, rtRstRestartOutcome(a, "xray")
}

// rtFltLogSince — окно журнала панели с offset (журнал ротирован — с начала).
func rtFltLogSince(offset int64) string {
	text, err := rtReadLogFrom(rtRtLogPath(), offset)
	if err != nil {
		text, _ = rtReadLogFrom(rtRtLogPath(), 0)
	}
	return text
}

// Сбои при выборе узла Xray: CR-02 (сокращённая проверка выбора, неприменимая
// горячая замена и отвергаемый каталог — откат без остановки Xray) и D-15
// (падение Xray сразу после горячей замены: доводящий перезапуск; при
// отвергаемом каталоге — откат с подъёмом Xray на прежних файлах или маркер
// D-16). Подготовка: слой включён, три узла «Моих узлов», ручная группа «Сбой»
// со всеми узлами и вложенная ручная «Внешняя» с участником-группой «Сбой»,
// маршрутизация Xray взята, «Применить», в «Сбой» выбран узел A. Метка ядра:
// горячая замена и каталог есть только у Xray, на Mihomo тест пропускается.
func TestRouterSelectXrayFaults(t *testing.T) {
	routertest.Tier(t, routertest.Full, "внесение сбоя: закрытый порт API Xray, отвергаемый каталог и падение процесса Xray посреди горячей замены")
	tg := routertest.CoreSpecific(t)
	if tg.Core != "xray" {
		t.Skip("ядро ветви не Xray: горячей замены исходящих и каталога Xray нет")
	}
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	e.rtRtPurge()
	taken := false

	// Очистка при любом исходе: release → «Применить», затем узлы и секции.
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

	hole := rtRtBlackhole(t)
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_ws", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_grpc", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_tls_alpn", hole))

	saveGroup := func(name string, sources map[string]any, members []map[string]string) string {
		r := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
			"revision": e.rtRtRevision(),
			"group": map[string]any{
				"name": name, "mode": "manual",
				"sources": sources,
				"members": members,
			},
		})
		if r.Status != http.StatusOK {
			t.Fatalf("groups/save %s: %d %s", name, r.Status, r.Code)
		}
		id, _ := r.object(t)["group"].(map[string]any)["id"].(string)
		if len(id) != 8 {
			t.Fatalf("groups/save %s: идентификатор группы длиной %d, ожидалось 8", name, len(id))
		}
		return id
	}
	gid := saveGroup("Сбой", map[string]any{"all": true}, []map[string]string{})
	outerID := saveGroup("Внешняя", map[string]any{"all": false, "ids": []string{}}, []map[string]string{{"kind": "group", "id": gid}})

	if tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()}); tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	taken = true
	e.rtRtApplyOK("взятие")
	e.rtRtRebuildReleased()

	// Стенд после теста: чужого файла нет, Xray работает, маркеров нет.
	// Регистрируется после общей очистки и выполняется раньше неё.
	t.Cleanup(func() {
		c := e.with(t)
		if !rtFltDropBad() {
			t.Error("очистка: чужой файл Xray не удалён")
		}
		c.rtFltEnsureXray()
		c.rtFltConfirmIfMarked("Сбой", gid)
	})

	// Узлы A и B — узлы Xray «Сбой» с различными подписями.
	r := e.call(http.MethodGet, "/api/configlayer/proxies", nil)
	if r.Status != http.StatusOK {
		t.Fatalf("proxies: %d %s", r.Status, r.Code)
	}
	rg := rtRtFindGroup(r.object(t), "Сбой")
	if rg == nil {
		t.Fatal("proxies: нет группы «Сбой»")
	}
	var nodeA, nodeB, labelA string
	members, _ := rg["members"].([]any)
	for _, it := range members {
		m, _ := it.(map[string]any)
		if m["kind"] != "node" || m["xray"] != true {
			continue
		}
		member, _ := m["member"].(string)
		label, _ := m["name"].(string)
		switch {
		case nodeA == "":
			nodeA, labelA = member, label
		case nodeB == "" && label != labelA:
			nodeB = member
		}
	}
	if nodeA == "" || nodeB == "" {
		t.Fatal("в «Сбой» нет двух узлов Xray с различными подписями")
	}

	// obs — исходящий группы «Сбой» при подтверждённом горячем выборе узла.
	obs := map[string]map[string]any{}
	if e.rtFltCurrent("Сбой") != nodeA {
		if s := e.rtFltSelect(gid, nodeA); s.status != http.StatusOK || s.xray != "applied" {
			t.Fatalf("выбор узла A: %s", s)
		}
	}
	inner, outer := rtFltOutbounds(t, gid, outerID)
	if !rtRtMatchesNode(t, inner) {
		t.Fatal("Xray: исходящий «Сбой» не совпадает ни с одним узлом «Моих узлов»")
	}
	if !reflect.DeepEqual(inner, outer) {
		t.Fatal("Xray: исходящий «Внешняя» не совпадает с исходящим «Сбой»: вложенная группа не указывает на «Сбой»")
	}
	obs[nodeA] = inner

	// CR-02: порт API закрыт — горячая замена неприменима (no_api); сокращённая
	// проверка выбора не видит чужой файл, а полная проверка перед перезапуском
	// его отвергает: откат файлов без перезапуска, Xray работает с прежним PID.
	t.Run("fallback_full_validation", func(t *testing.T) {
		c := e.with(t)
		pid := rtFltXrayPID()
		if pid == 0 {
			t.Fatal("до выбора нет единственного процесса Xray")
		}
		port := rtFltAPIPort(t)
		before := rtFltFilterRules(t)
		unblock := rtFltBlockAPI(t, port)
		t.Cleanup(func() {
			if !rtFltDropBad() {
				t.Error("очистка: чужой файл Xray не удалён")
			}
		})
		rtFltPutBad(t)

		offset := rtRtLogOffset(t)
		s := c.rtFltSelect(gid, nodeB)
		code, rolledBack, xrayOut := c.rtFltApplyCode()
		window := rtFltLogSince(offset)
		pidSame := rtFltXrayPID() == pid
		t.Logf("cr02: reverted=%t pid_same=%t code=%s", s.reverted, pidSame, code)
		if s.status != http.StatusOK || !s.reverted {
			t.Errorf("CR-02: выбор при закрытом API и отвергаемом каталоге: %s, ожидалось 200 reverted", s)
		}
		if code == "drift_blocked" {
			t.Errorf("CR-02: проход остановлен дрейфом (drift_blocked): чужой файл подобран неверно")
		}
		if code != "restart_failed" || !rolledBack || xrayOut != "failed_rolled_back" {
			t.Errorf("CR-02: итог прохода code=%q rolled_back=%t xray=%q, ожидалось restart_failed с откатом и failed_rolled_back", code, rolledBack, xrayOut)
		}
		if !strings.Contains(window, rtFltNoAPIMark) {
			t.Errorf("CR-02: в журнале панели нет строки горячей замены с причиной no_api: путь сокращённой проверки и неприменимой замены не исполнен")
		}
		if !pidSame {
			t.Errorf("CR-02: Xray перезапущен или остановлен при отказе полной проверки (PID прежний=%t, процесс есть=%t)", pidSame, rtFltXrayPID() > 0)
		}
		if cur := c.rtFltCurrent("Сбой"); cur != nodeA {
			t.Error("CR-02: выбор «Сбой» в Live не вернулся к узлу A")
		}
		if cur, marked := c.rtFltRuntime("Сбой"); cur != nodeA || marked {
			t.Errorf("CR-02: вид «Прокси»: выбор A=%t, маркер selection_unconfirmed=%t", cur == nodeA, marked)
		}
		in, out := rtFltOutbounds(t, gid, outerID)
		if !reflect.DeepEqual(in, obs[nodeA]) || !reflect.DeepEqual(out, obs[nodeA]) {
			t.Errorf("CR-02: исходящие групп в xcp-groups не прежние: «Сбой»=%t «Внешняя»=%t", reflect.DeepEqual(in, obs[nodeA]), reflect.DeepEqual(out, obs[nodeA]))
		}

		// Сбои сняты: выбор B проходит горячей заменой без смены PID.
		if !rtFltDropBad() {
			t.Fatal("чужой файл Xray не удалён")
		}
		unblock()
		after := rtFltFilterRules(t)
		if b, a := rtFltPortRules(before, port), rtFltPortRules(after, port); !reflect.DeepEqual(b, a) {
			t.Errorf("правила фильтра на порт API до (%d) и после (%d) теста различаются", len(b), len(a))
		}
		if len(before) != len(after) {
			t.Logf("cr02: правил фильтра до %d, после %d (правила прошивки меняются независимо от теста)", len(before), len(after))
		}
		s = c.rtFltSelect(gid, nodeB)
		if s.status != http.StatusOK || s.xray != "applied" || s.reverted {
			t.Fatalf("CR-02: выбор B после снятия сбоев: %s, ожидалось applied", s)
		}
		if rtFltXrayPID() != pid {
			t.Error("CR-02: выбор B после снятия сбоев сменил PID Xray")
		}
		in, _ = rtFltOutbounds(t, gid, outerID)
		if reflect.DeepEqual(in, obs[nodeA]) || !rtRtMatchesNode(t, in) {
			t.Error("CR-02: исходящий «Сбой» не сменился на узел B")
		}
		obs[nodeB] = in
	})
}
