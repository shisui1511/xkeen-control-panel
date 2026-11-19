//go:build router

// router-area: routing

package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
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
	// rtFltKillPoll — шаг чтения журнала панели горутинами убийства и выбора по
	// строке журнала.
	rtFltKillPoll = 20 * time.Millisecond
	// rtFltHotMark — строка журнала после горячей замены (routing/hotswap.go).
	rtFltHotMark = "[routing] горячая замена Xray: тегов"
	// rtFltNoAPIReason — причина неприменимой горячей замены: порт API не отвечает.
	rtFltNoAPIReason = "xray_hot_no_api"
	// rtFltNoAPIMark — строка конвейера о неприменимой горячей замене (rtFltNoAPIReason).
	rtFltNoAPIMark = "горячая замена Xray: причина=" + rtFltNoAPIReason
	// rtFltRollingNote — пояснение шага restart при откате файлов
	// (NoteRestartFailedRolling): событие уходит до отката и подъёма ядра.
	rtFltRollingNote = "restart_failed_rolling_back"
	// rtFltXrayWait — предел ожидания работающего Xray после запуска.
	rtFltXrayWait = 2 * time.Minute
	// rtFltHoldWait — предел ожидания строки rtFltNoAPIMark прохода выбора,
	// держащего ход перед первой пачкой (fallback_batch_return).
	rtFltHoldWait = time.Minute
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

// rtFltPutBadBare — rtFltPutBad без t.*: для горутины наблюдения за потоком
// событий; true — файл записан.
func rtFltPutBadBare() bool {
	return os.WriteFile(rtFltBadPath(), rtFltBadBody, 0o600) == nil
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
// вызов безопасен); t.Cleanup снимает его при любом исходе. Прошивка пересобирает
// таблицу filter по своим событиям (NDM netfilter rewrite: обновление правил UPnP
// после DHCP клиента и т. п.) и снимает чужое правило; ensure (без t.*) проверяет
// правило и возвращает его, если оно пропало: true — правило возвращено.
func rtFltBlockAPI(t *testing.T, port string) (unblock func(), ensure func() bool) {
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
	ensure = func() bool {
		if rtFltIptables(append([]string{"-C"}, rule...)...) == 0 {
			return false
		}
		return rtFltIptables(append([]string{"-I"}, rule...)...) == 0
	}
	t.Cleanup(unblock)
	return unblock, ensure
}

// rtFltHotReasonRe — строка конвейера с причиной исхода горячей замены Xray.
var rtFltHotReasonRe = regexp.MustCompile(`configlayer: горячая замена Xray: причина=(\S+)`)

// rtFltHotReasons — причины горячей замены Xray по порядку строк окна журнала.
func rtFltHotReasons(window string) []string {
	var out []string
	for _, m := range rtFltHotReasonRe.FindAllStringSubmatch(window, -1) {
		out = append(out, m[1])
	}
	return out
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

// rtFltSelectBare — rtFltSelect без t.*: для одновременных выборов из горутин.
func (e *rtSrc) rtFltSelectBare(gid, member string) (rtFltSel, error) {
	status, raw, err := e.p.CallSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": member})
	if err != nil {
		return rtFltSel{}, err
	}
	r := e.decode(status, raw)
	s := rtFltSel{status: r.Status, code: r.Code}
	if r.Status != http.StatusOK {
		return s, nil
	}
	var obj struct {
		Kernels       map[string]string `json:"kernels"`
		RestartReason string            `json:"restart_reason"`
		Reverted      bool              `json:"reverted"`
		Unconfirmed   []any             `json:"unconfirmed"`
	}
	if err := json.Unmarshal(r.Data, &obj); err != nil {
		return s, fmt.Errorf("data ответа select не объект: %w", err)
	}
	s.xray, s.reason, s.reverted, s.unconfirmed = obj.Kernels["xray"], obj.RestartReason, obj.Reverted, len(obj.Unconfirmed)
	return s, nil
}

// rtFltBatchName — имя i-й независимой группы возврата пачки.
func rtFltBatchName(i int) string { return fmt.Sprintf("Пачка %d", i+1) }

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

// rtFltKill — итог горутины убийства: процесс убит по строке горячей замены.
type rtFltKill struct {
	killed bool
	err    error
}

// rtFltKillOnHot читает журнал панели с offset каждые rtFltKillPoll и при первой
// строке горячей замены убивает процесс pid (SIGKILL). Горутина без t.*: итог —
// через канал, выход — по ctx.
func rtFltKillOnHot(ctx context.Context, offset int64, pid int) <-chan rtFltKill {
	out := make(chan rtFltKill, 1)
	go func() {
		tick := time.NewTicker(rtFltKillPoll)
		defer tick.Stop()
		for {
			if strings.Contains(rtFltLogSince(offset), rtFltHotMark) {
				err := syscall.Kill(pid, syscall.SIGKILL)
				out <- rtFltKill{killed: err == nil, err: err}
				return
			}
			select {
			case <-ctx.Done():
				out <- rtFltKill{}
				return
			case <-tick.C:
			}
		}
	}()
	return out
}

// rtFltNext — итог выбора, отправленного из горутины по сигналу: отправлен ли он,
// моменты отправки и ответа, ответ.
type rtFltNext struct {
	sent     bool
	at, done time.Time
	s        rtFltSel
	err      error
}

// rtFltSelectOn отправляет выбор member в группе gid (rtFltSelectBare), как только
// закрыт go. Закрытый раньше late — окно прошло, выбор не отправляется. Горутина
// без t.*: итог — через канал.
func (e *rtSrc) rtFltSelectOn(goCh, late <-chan struct{}, gid, member string) <-chan rtFltNext {
	out := make(chan rtFltNext, 1)
	go func() {
		select {
		case <-goCh:
		case <-late:
			out <- rtFltNext{}
			return
		}
		r := rtFltNext{sent: true, at: time.Now()}
		r.s, r.err = e.rtFltSelectBare(gid, member)
		r.done = time.Now()
		out <- r
	}()
	return out
}

// rtFltHoldDone — что сделано по концу перезапуска прохода, держащего ход:
// чужой файл положен (put), PID Xray после перезапуска (pid), правило блока порта
// API пропало за время прохода и возвращено (blockRestored).
type rtFltHoldDone struct {
	put           bool
	pid           int
	blockRestored bool
}

// rtFltRollback — итог наблюдения за потоком событий слоя: действия по концу
// перезапуска прохода-держателя (hold, если наблюдение шло с onHold). Событие шага
// restart с пояснением restart_failed_rolling_back пришло, и чужой файл снят по
// нему; итог этого прохода — первое apply_done после события отката (код итога,
// откат файлов, исход Xray): GET state к концу наблюдения может нести уже
// следующий проход.
type rtFltRollback struct {
	hold    rtFltHoldDone
	seen    bool
	removed bool
	// done — apply_done прохода с откатом пришло.
	done       bool
	code       string
	rolledBack bool
	xray       string
}

// rtFltWatchRollback открывает поток событий слоя и ждёт его первого события
// (подписка заведена). Горутина без t.*: на первое событие шага restart с
// пояснением restart_failed_rolling_back сразу удаляет чужой файл — до отката
// файлов и подъёма Xray на прежних; следующее apply_done — итог этого прохода.
// onHold (не nil): первый итог шага restart прохода выбора (trigger select) —
// проход, держащий ход; если это done (Xray перезапущен и подтверждён, каталог им
// уже прочитан), горутина вызывает onHold и сохраняет его итог. События отката до
// этого итога (откат самого держателя) файл не снимают. Итог — в канал после
// закрытия потока по ctx.
func (e *rtSrc) rtFltWatchRollback(ctx context.Context, onHold func() rtFltHoldDone) <-chan rtFltRollback {
	e.t.Helper()
	const path = "/api/configlayer/events"
	resp, err := e.p.Stream(ctx, path)
	if err != nil {
		e.t.Fatalf("поток %s не открылся: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		e.t.Fatalf("поток %s: http %d", path, resp.StatusCode)
	}
	first := make(chan struct{})
	out := make(chan rtFltRollback, 1)
	go func() {
		defer resp.Body.Close()
		var res rtFltRollback
		defer func() { out <- res }()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 16<<20)
		event, got := "", false
		armed := onHold == nil
		for sc.Scan() {
			line := sc.Text()
			if !got {
				got = true
				close(first)
			}
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "apply_step" && !res.seen:
				var sv struct {
					ID       string `json:"id"`
					State    string `json:"state"`
					NoteCode string `json:"note_code"`
					Trigger  string `json:"trigger"`
				}
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &sv) != nil || sv.ID != "restart" {
					continue
				}
				switch {
				case !armed:
					if sv.Trigger == "select" && (sv.State == "done" || sv.State == "failed") {
						armed = true
						if sv.State == "done" {
							res.hold = onHold()
						}
					}
				case sv.NoteCode == rtFltRollingNote:
					res.seen = true
					res.removed = rtFltDropBad()
				}
			case strings.HasPrefix(line, "data: ") && event == "apply_done" && res.seen && !res.done:
				var av struct {
					Restart []struct {
						Kernel  string `json:"kernel"`
						Outcome string `json:"outcome"`
					} `json:"restart"`
					Result *struct {
						Code       string `json:"code"`
						RolledBack bool   `json:"rolled_back"`
					} `json:"result"`
				}
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &av) == nil {
					res.done = true
					if av.Result != nil {
						res.code, res.rolledBack = av.Result.Code, av.Result.RolledBack
					}
					for _, r := range av.Restart {
						if r.Kernel == "xray" {
							res.xray = r.Outcome
						}
					}
				}
			case line == "":
				event = ""
			}
		}
	}()
	select {
	case <-first:
	case <-time.After(30 * time.Second):
		e.t.Fatalf("поток %s не отдал первое событие за 30 с", path)
	}
	return out
}

// Сбои при выборе узла Xray: CR-02 (сокращённая проверка выбора, неприменимая
// горячая замена и отвергаемый каталог — откат без остановки Xray), D-15
// (падение Xray сразу после горячей замены: доводящий перезапуск; при
// отвергаемом каталоге — откат с подъёмом Xray на прежних файлах или маркер
// D-16) и CR-01 (возврат пачки одновременных выборов при отказе полной проверки).
// Подготовка: три узла «Моих узлов», ручная группа «Сбой» со всеми
// узлами, вложенная ручная «Внешняя» с участником-группой «Сбой» и три
// независимые ручные группы «Пачка» со всеми узлами, маршрутизация Xray взята,
// «Применить», в «Сбой» выбран узел A. Метка ядра:
// горячая замена и каталог есть только у Xray, на Mihomo тест пропускается.
func TestRouterSelectXrayFaults(t *testing.T) {
	routertest.Tier(t, routertest.Full, "внесение сбоя: закрытый порт API Xray, отвергаемый каталог и падение процесса Xray посреди горячей замены")
	tg := routertest.CoreSpecific(t)
	if tg.Core != "xray" {
		t.Skip("ядро ветви не Xray: горячей замены исходящих и каталога Xray нет")
	}
	e := rtNewSrc(t)
	e.freshDraft()
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
	// Независимые ручные группы возврата пачки (fallback_batch_return): применяются
	// вместе со взятием, отдельного «Применить» в подтесте нет.
	var batch [3]string
	for i := range batch {
		batch[i] = saveGroup(rtFltBatchName(i), map[string]any{"all": true}, []map[string]string{})
	}

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
		unblock, _ := rtFltBlockAPI(t, port)
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

	// CR-01, возврат пачки и следующая пачка во время неудачного прохода: порт API
	// закрыт, горячая замена неприменима (no_api). При свободном ходе первый
	// пришедший выбор запечатывает пачку сразу и один, поэтому ход сначала занимает
	// выбор в «Сбой» при каталоге без чужого файла: его проход проходит полную
	// проверку и перезапускает Xray с ожиданием устойчивости — ход занят секунды.
	// По строке no_api этого прохода одновременно уходят выборы узла B в «Пачка 1»
	// и «Пачка 2» и собираются одной следующей пачкой. Чужой отвергаемый файл
	// кладётся по концу шага перезапуска прохода «Сбой» (Xray уже прочитал каталог),
	// поэтому проход первой пачки, как в fallback_full_validation, отвергается
	// полной проверкой: оба выбора получают reverted, Live и xcp-groups обеих групп
	// возвращаются к узлу A, Xray работает с PID после перезапуска «Сбой». Выбор B в
	// «Пачка 3» уходит по ответу «Сбой»: ход к этому моменту передан первой пачке,
	// третья группа собирается следующей пачкой и ждёт ход, пока неудачный проход
	// возвращает выборы (settleXrayBatch). Чужой файл снимается по событию отката
	// (restart_failed_rolling_back) первой пачки; проход третьей группы проходит
	// полную проверку, перезапускает Xray (причина no_api) и не несёт в xcp-groups и
	// ядро отвергнутых выборов первых двух. Попадание доказывается после факта:
	// строка no_api «Сбой» видна и файл положен по концу его перезапуска (правило
	// блока порта API к этому моменту возвращается, если прошивка сняла его
	// пересборкой таблицы filter); первые две причины горячей замены окна — проходы
	// «Сбой» и первой пачки — no_api, иначе первая пачка дошла до API Xray; строки
	// этапов «Пачка 1» и «Пачка 2» — пачка из двух (batch=2) с ожиданием хода
	// (wait > 0), а других выборов, кроме «Сбой» и третьей группы, нет, значит, это
	// одна пачка; строка этапов третьей группы — отдельная пачка (batch=1) с
	// ожиданием хода, её выбор отправлен раньше обоих ответов первой пачки и не
	// reverted. Иначе попытка не засчитывается, стенд возвращается и попытка
	// повторяется (до rtFltAttempts).
	// Код до исправления e8f68073 сценарий не отличает: там окно гонки — время между
	// снятием хода и возвратом выбора той же горутиной.
	t.Run("fallback_batch_return", func(t *testing.T) {
		c := e.with(t)
		t.Cleanup(func() {
			if !rtFltDropBad() {
				t.Error("очистка: чужой файл Xray не удалён")
			}
			c.rtFltEnsureXray()
			for i, id := range batch {
				c.rtFltConfirmIfMarked(rtFltBatchName(i), id)
			}
			c.rtFltConfirmIfMarked("Сбой", gid)
		})
		third := rtFltBatchName(2)
		port := rtFltAPIPort(t)

		// ready — исходный выбор узла A во всех трёх группах горячей заменой при
		// открытом API: у третьей группы цель B отличается от текущего выбора.
		prev := map[string]map[string]any{}
		ready := func() {
			t.Helper()
			for i, id := range batch {
				name := rtFltBatchName(i)
				if c.rtFltCurrent(name) != nodeA {
					if s := c.rtFltSelect(id, nodeA); s.status != http.StatusOK || s.xray != "applied" {
						t.Fatalf("исходный выбор узла A в «%s»: %s", name, s)
					}
				}
				ob, ok := rtRtGroupOutbound(t, id)
				if !ok || !reflect.DeepEqual(ob, obs[nodeA]) {
					t.Fatalf("Xray: исходящий «%s» до сбоя не совпадает с узлом A (найден=%t)", name, ok)
				}
				prev[id] = ob
			}
		}
		// restored проверяет, что выбор и исходящий групп первой пачки — прежние (узел A).
		restored := func(label string) {
			t.Helper()
			for i, id := range batch[:2] {
				name := rtFltBatchName(i)
				if c.rtFltCurrent(name) != nodeA {
					t.Errorf("%s: выбор «%s» в Live не узел A", label, name)
				}
				if cur, marked := c.rtFltRuntime(name); cur != nodeA || marked {
					t.Errorf("%s: вид «Прокси» «%s»: выбор A=%t, маркер selection_unconfirmed=%t", label, name, cur == nodeA, marked)
				}
				if ob, ok := rtRtGroupOutbound(t, id); !ok || !reflect.DeepEqual(ob, prev[id]) {
					t.Errorf("%s: исходящий «%s» в xcp-groups не прежний (найден=%t)", label, name, ok)
				}
			}
		}

		// selRes — ответ выбора первой пачки, момент ответа и PID Xray сразу после него.
		type selRes struct {
			s    rtFltSel
			err  error
			done time.Time
			pid  int
		}
		var (
			hit      bool
			attempts int
			results  [2]selRes
			next     rtFltNext
			st       rtRtStage
			pair     [2]rtRtStage
		)
		for attempts < rtFltAttempts && !hit {
			attempts++
			ready()
			// Выбор-держатель хода: узел «Сбой», отличный от текущего.
			holdTo := nodeB
			if c.rtFltCurrent("Сбой") == nodeB {
				holdTo = nodeA
			}
			if rtFltXrayPID() == 0 {
				t.Fatal("до выбора нет единственного процесса Xray")
			}
			unblock, ensureBlock := rtFltBlockAPI(t, port)

			offset := rtRtLogOffset(t)
			var (
				rb       rtFltRollback
				hold     rtFltNext
				holdSeen bool
			)
			results, next, st, pair = [2]selRes{}, rtFltNext{}, rtRtStage{}, [2]rtRtStage{}
			func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				// По концу перезапуска «Сбой»: правило блока порта API на месте (прошивка
				// могла снять его пересборкой таблицы filter), чужой файл положен, PID
				// Xray запомнен; файл снимается по откату первой пачки.
				rolled := c.rtFltWatchRollback(ctx, func() rtFltHoldDone {
					restored := ensureBlock()
					return rtFltHoldDone{blockRestored: restored, put: rtFltPutBadBare(), pid: rtFltXrayPID()}
				})
				holdCh := make(chan rtFltNext, 1)
				// Ответ «Сбой»: ход уже передан первой пачке — выбор третьей группы уходит сразу.
				holdOK := make(chan struct{})
				go func() {
					r := rtFltNext{sent: true, at: time.Now()}
					r.s, r.err = c.rtFltSelectBare(gid, holdTo)
					r.done = time.Now()
					if r.err == nil && r.s.status == http.StatusOK {
						close(holdOK)
					}
					holdCh <- r
				}()
				late := make(chan struct{})
				nextCh := c.rtFltSelectOn(holdOK, late, batch[2], nodeB)
				// Строка no_api прохода «Сбой»: ход занят, впереди полная проверка и перезапуск Xray.
				holdSeen, _ = routertest.Poll(rtFltHoldWait, rtFltKillPoll, func() (bool, string) {
					return strings.Contains(rtFltLogSince(offset), rtFltNoAPIMark), ""
				})
				if !holdSeen {
					close(late)
					hold = <-holdCh
					next = <-nextCh
					cancel()
					rb = <-rolled
					return
				}
				var wg sync.WaitGroup
				start := make(chan struct{})
				for i, id := range batch[:2] {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						r := &results[i]
						r.s, r.err = c.rtFltSelectBare(id, nodeB)
						r.done = time.Now()
						r.pid = rtFltXrayPID()
					}()
				}
				close(start)
				wg.Wait()
				hold = <-holdCh
				close(late)
				next = <-nextCh
				cancel()
				rb = <-rolled
			}()
			window := rtFltLogSince(offset)

			// Первая пачка — одна: строки этапов обеих групп — пачка из двух с ожиданием
			// хода. Кроме «Сбой» (его пачка запечатана до отправки первой) и третьей
			// группы, выборов нет: две строки batch=2 — одна и та же пачка.
			var stages map[string]rtRtStage
			if holdSeen {
				groups := []string{batch[0], batch[1]}
				if next.sent {
					groups = append(groups, batch[2])
				}
				stages, _, _ = rtRtWaitStages(offset, groups, 0, rtRtStagesWait("xray"))
			}
			var staged12 [2]bool
			for i := range pair {
				pair[i], staged12[i] = stages[batch[i]]
			}
			paired := staged12[0] && staged12[1]
			for _, p := range pair {
				paired = paired && p.num("batch") == 2 && p.num("wait") > 0
			}
			// Попадание в окно — по строке этапов третьей группы и порядку отправки и ответов.
			var staged bool
			st, staged = stages[batch[2]]
			before := next.sent && next.at.Before(results[0].done) && next.at.Before(results[1].done)
			inWindow := staged && st.num("batch") == 1 && st.num("wait") > 0 && before
			// Порт API закрыт для прохода «Сбой» и прохода первой пачки: первые две
			// причины горячей замены окна — no_api. Прошивка снимает правило блока
			// пересборкой таблицы filter в любой момент; тогда первая пачка доходит до
			// API Xray, и сценарий CR-01 не исполнен.
			reasons := rtFltHotReasons(window)
			closed := len(reasons) >= 2 && reasons[0] == rtFltNoAPIReason && reasons[1] == rtFltNoAPIReason
			switch {
			case !holdSeen || !rb.hold.put:
				t.Logf("cr01-next: попытка %d мимо окна: проход «Сбой» не дошёл до конца перезапуска (hold_seen=%t put=%t hold=%s)",
					attempts, holdSeen, rb.hold.put, hold.s)
			case !closed:
				// lost_after — правило пропало и после конца перезапуска «Сбой».
				t.Logf("cr01-next: попытка %d мимо окна: порт API открыт для прохода «Сбой» или первой пачки (причины=%s block_restored=%t lost_after=%t)",
					attempts, strings.Join(reasons, ","), rb.hold.blockRestored, ensureBlock())
			case !paired:
				t.Logf("cr01-next: попытка %d мимо окна: первая пачка разошлась (staged12=%t/%t batch12=%d/%d wait12=%d/%d out12=%s/%s reverted12=%t/%t)",
					attempts, staged12[0], staged12[1], pair[0].num("batch"), pair[1].num("batch"), pair[0].num("wait"), pair[1].num("wait"),
					results[0].s.xray, results[1].s.xray, results[0].s.reverted, results[1].s.reverted)
			case !inWindow || next.err != nil || next.s.status != http.StatusOK || next.s.reverted:
				t.Logf("cr01-next: попытка %d мимо окна: sent=%t staged=%t batch3=%d wait3=%d before=%t status3=%d out3=%s reverted3=%t reverted12=%t/%t rollback_event=%t",
					attempts, next.sent, staged, st.num("batch"), st.num("wait"), before, next.s.status, next.s.xray, next.s.reverted, results[0].s.reverted, results[1].s.reverted, rb.seen)
			default:
				hit = true
			}
			if !hit {
				// Стенд возвращается: файл снят, API открыт, Xray работает, маркеров нет.
				if !rtFltDropBad() {
					t.Fatal("чужой файл Xray не удалён")
				}
				unblock()
				c.rtFltEnsureXray()
				for i, id := range batch {
					c.rtFltConfirmIfMarked(rtFltBatchName(i), id)
				}
				c.rtFltConfirmIfMarked("Сбой", gid)
				c.rtRstIdle()
				continue
			}

			// Держатель хода: проход «Сбой» при каталоге без чужого файла — полная
			// проверка и перезапуск Xray (причина no_api), выбор не возвращён.
			if hold.err != nil || hold.s.status != http.StatusOK || hold.s.reverted || hold.s.xray != "restarted" || hold.s.reason != "no_api" {
				t.Errorf("CR-01: выбор в «Сбой», держащий ход перед первой пачкой: %s (ошибка=%t), ожидалось 200 restarted с причиной no_api", hold.s, hold.err != nil)
			}

			// Первая пачка: оба выбора возвращены, проход — отказ полной проверки с
			// откатом без перезапуска (PID к ответам — PID после перезапуска «Сбой»).
			pidSame := results[0].pid == rb.hold.pid && results[1].pid == rb.hold.pid
			t.Logf("cr01: reverted=%t/%t pid_same=%t code=%s block_restored=%t", results[0].s.reverted, results[1].s.reverted, pidSame, rb.code, rb.hold.blockRestored)
			for i, r := range results {
				if r.err != nil {
					t.Errorf("CR-01: выбор в «%s»: %v", rtFltBatchName(i), r.err)
					continue
				}
				if r.s.status != http.StatusOK || !r.s.reverted {
					t.Errorf("CR-01: выбор в «%s» при закрытом API и отвергаемом каталоге: %s, ожидалось 200 reverted", rtFltBatchName(i), r.s)
				}
			}
			switch {
			case !rb.done:
				t.Error("CR-01: в потоке событий слоя нет итога прохода с откатом (apply_done после restart_failed_rolling_back)")
			case rb.code != "restart_failed" || !rb.rolledBack || rb.xray != "failed_rolled_back":
				t.Errorf("CR-01: итог прохода code=%q rolled_back=%t xray=%q, ожидалось restart_failed с откатом и failed_rolled_back", rb.code, rb.rolledBack, rb.xray)
			}
			if !rb.removed {
				t.Error("CR-01: чужой файл не снят по событию отката")
			}
			if !strings.Contains(window, rtFltNoAPIMark) {
				t.Error("CR-01: в журнале панели нет строки горячей замены с причиной no_api")
			}
			if !pidSame {
				t.Errorf("CR-01: Xray перезапущен или остановлен при отказе полной проверки (PID к ответам прежний: %t/%t)", results[0].pid == rb.hold.pid, results[1].pid == rb.hold.pid)
			}

			// Следующая пачка: API открыт, Xray поднят после прохода третьей группы.
			unblock()
			newPID := c.rtFltEnsureXray()
			restored("CR-01, после следующей пачки")
			s3 := next.s
			if s3.unconfirmed > 0 || !(s3.xray == "applied" || (s3.xray == "restarted" && s3.reason == "no_api")) {
				t.Errorf("CR-01: выбор B в «%s» следующей пачкой: %s, ожидалось restarted с причиной no_api или applied", third, s3)
			}
			if s3.xray == "restarted" && newPID == rb.hold.pid {
				t.Errorf("CR-01: исход restarted, но Xray работает с PID первой пачки: ядро не загрузило xcp-groups следующей пачки")
			}
			if c.rtFltCurrent(third) != nodeB {
				t.Errorf("CR-01: выбор «%s» в Live не узел B", third)
			}
			if cur, marked := c.rtFltRuntime(third); cur != nodeB || marked {
				t.Errorf("CR-01: вид «Прокси» «%s»: выбор B=%t, маркер selection_unconfirmed=%t", third, cur == nodeB, marked)
			}
			ob, ok := rtRtGroupOutbound(t, batch[2])
			if !ok || reflect.DeepEqual(ob, obs[nodeA]) || !rtRtMatchesNode(t, ob) {
				t.Errorf("CR-01: исходящий «%s» не сменился на узел «Моих узлов», отличный от A (найден=%t)", third, ok)
			}
			if want, known := obs[nodeB]; known && !reflect.DeepEqual(ob, want) {
				t.Errorf("CR-01: исходящий «%s» не совпадает с узлом B", third)
			}
		}
		window, out3 := "missed", next.s.xray
		if hit {
			window = "hit"
		}
		if next.s.reason != "" {
			out3 += ":" + next.s.reason
		}
		t.Logf("cr01-next: window=%s attempts=%d batch12=%d/%d wait3=%d out3=%s reverted12=%t/%t", window, attempts, pair[0].num("batch"), pair[1].num("batch"), st.num("wait"), out3, results[0].s.reverted, results[1].s.reverted)
		if !hit {
			t.Errorf("cr01-next: окно не поймано за %d попыток: выбор «%s» не собран следующей пачкой во время неудачного прохода первой", attempts, third)
		}
	})

	// other — узел «Сбой», отличный от текущего выбора.
	other := func(c *rtSrc) (prev, target string) {
		prev = c.rtFltCurrent("Сбой")
		if prev == nodeB {
			return prev, nodeA
		}
		return prev, nodeB
	}

	// D-15, доводящий перезапуск: Xray убит сразу после горячей замены (строка
	// журнала панели), опрос PID после хука видит частичный сбой, Xray
	// перезапускается на новых файлах. Попадание в окно подтверждения — по исходу
	// ответа: restarted с причиной hot_failed; applied — убийство пришло после
	// подтверждения, попытка повторяется.
	t.Run("hot_kill_restart", func(t *testing.T) {
		c := e.with(t)
		t.Cleanup(func() {
			c.rtFltEnsureXray()
			c.rtFltConfirmIfMarked("Сбой", gid)
		})
		hit, failed, attempts := false, false, 0
		for attempts < rtFltAttempts && !hit && !failed {
			attempts++
			pid := c.rtFltEnsureXray()
			_, target := other(c)
			prevIn, _ := rtFltOutbounds(t, gid, outerID)
			var s rtFltSel
			var k rtFltKill
			func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				killed := rtFltKillOnHot(ctx, rtRtLogOffset(t), pid)
				s = c.rtFltSelect(gid, target)
				cancel()
				k = <-killed
			}()
			switch {
			case s.status == http.StatusOK && s.xray == "restarted" && s.reason == "hot_failed":
				hit = true
				newPID := routertest.WaitKernelReady(t, "xray", rtFltXrayWait)
				t.Logf("d15: window=hit attempts=%d outcome=%s reason=%s killed=%t pid_changed=%t", attempts, s.xray, s.reason, k.killed, newPID != pid)
				if newPID == pid {
					t.Error("D-15: после доводящего перезапуска Xray работает с убитым PID")
				}
				if s.reverted || s.unconfirmed > 0 {
					t.Errorf("D-15: доводящий перезапуск вернул выбор или поставил маркер: %s", s)
				}
				if cur := c.rtFltCurrent("Сбой"); cur != target {
					t.Error("D-15: выбор «Сбой» в Live не равен новому узлу")
				}
				if cur, marked := c.rtFltRuntime("Сбой"); cur != target || marked {
					t.Errorf("D-15: вид «Прокси»: новый узел=%t, маркер selection_unconfirmed=%t", cur == target, marked)
				}
				in, out := rtFltOutbounds(t, gid, outerID)
				switch {
				case reflect.DeepEqual(in, prevIn):
					t.Error("D-15: исходящий «Сбой» в xcp-groups не сменился")
				case !rtRtMatchesNode(t, in):
					t.Error("D-15: исходящий «Сбой» не совпадает ни с одним узлом «Моих узлов»")
				case obs[target] != nil && !reflect.DeepEqual(in, obs[target]):
					t.Error("D-15: исходящий «Сбой» не совпадает с выбранным узлом")
				}
				if !reflect.DeepEqual(out, in) {
					t.Error("D-15: исходящий «Внешняя» не совпадает с новым исходящим «Сбой»")
				}
			case s.status == http.StatusOK && s.xray == "applied" && !s.reverted && s.unconfirmed == 0:
				// Убийство пришло после подтверждения (или строки замены не было):
				// Xray поднимается и попытка повторяется.
				t.Logf("d15: попытка %d мимо окна: %s killed=%t", attempts, s, k.killed)
				c.rtFltEnsureXray()
			default:
				failed = true
				t.Errorf("D-15: доводящий перезапуск не сработал: %s killed=%t (ожидалось restarted с причиной hot_failed)", s, k.killed)
			}
		}
		if !hit && !failed {
			t.Logf("d15: window=missed attempts=%d", attempts)
			t.Errorf("d15: окно подтверждения не поймано")
		}
	})

	// D-15, «перезапуск не помог», и D-16: чужой отвергаемый файл в каталоге и
	// Xray, убитый сразу после горячей замены. Частичный сбой ведёт к полной
	// проверке перед перезапуском (CR-02), она отказывает, откат файлов поднимает
	// Xray на прежних файлах (hotPartial). Чужой файл снимается по событию шага
	// отката (apply_step, restart, restart_failed_rolling_back): успел —
	// reverted с работающим Xray и прежним выбором (rollback_up); не успел — маркер
	// selection_unconfirmed с остановленным Xray (rollback_down), затем
	// «Повторить». reverted при остановленном Xray без маркера — откат без
	// перезапуска (hotPartial не присвоена): ошибка.
	t.Run("hot_kill_rejected", func(t *testing.T) {
		c := e.with(t)
		t.Cleanup(func() {
			if !rtFltDropBad() {
				t.Error("очистка: чужой файл Xray не удалён")
			}
			c.rtFltEnsureXray()
			c.rtFltConfirmIfMarked("Сбой", gid)
		})
		hit, failed, attempts := false, false, 0
		for attempts < rtFltAttempts && !hit && !failed {
			attempts++
			pid := c.rtFltEnsureXray()
			prev, target := other(c)
			prevIn, _ := rtFltOutbounds(t, gid, outerID)
			rtFltPutBad(t)
			var (
				s  rtFltSel
				k  rtFltKill
				rb rtFltRollback
			)
			func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				rolled := c.rtFltWatchRollback(ctx, nil)
				killed := rtFltKillOnHot(ctx, rtRtLogOffset(t), pid)
				s = c.rtFltSelect(gid, target)
				cancel()
				k, rb = <-killed, <-rolled
			}()
			xrayPID := rtFltXrayPID()
			marker := s.unconfirmed > 0
			report := func(window, branch string) {
				t.Logf("d15b: window=%s attempts=%d branch=%s outcome=%s marker=%t xray_up=%t rollback_event=%t", window, attempts, branch, s.xray, marker, xrayPID > 0, rb.seen)
			}
			switch {
			case s.status == http.StatusOK && s.xray == "applied" && !s.reverted && !marker:
				// Убийство после подтверждения: чужой файл не дал бы поднять Xray —
				// файл снимается, ядро запускается, попытка повторяется.
				t.Logf("d15b: попытка %d мимо окна: %s killed=%t", attempts, s, k.killed)
				if !rtFltDropBad() {
					t.Fatal("чужой файл Xray не удалён")
				}
				c.rtFltEnsureXray()
			case s.status == http.StatusOK && s.reverted && !marker && xrayPID > 0 && xrayPID != pid:
				hit = true
				report("hit", "rollback_up")
				if !rb.seen {
					t.Error("D-15: исход reverted без события отката restart_failed_rolling_back")
				}
				if rtFltBadPresent() {
					t.Error("D-15: чужой файл остался в каталоге после события отката")
				}
				if cur := c.rtFltCurrent("Сбой"); cur != prev {
					t.Error("D-15: выбор «Сбой» в Live не вернулся к прежнему узлу")
				}
				if cur, marked := c.rtFltRuntime("Сбой"); cur != prev || marked {
					t.Errorf("D-15: вид «Прокси»: прежний узел=%t, маркер selection_unconfirmed=%t", cur == prev, marked)
				}
				in, out := rtFltOutbounds(t, gid, outerID)
				if !reflect.DeepEqual(in, prevIn) || !reflect.DeepEqual(out, prevIn) {
					t.Errorf("D-15: исходящие групп в xcp-groups не прежние: «Сбой»=%t «Внешняя»=%t", reflect.DeepEqual(in, prevIn), reflect.DeepEqual(out, prevIn))
				}
				routertest.WaitKernelReady(t, "xray", rtFltXrayWait)
			case s.status == http.StatusOK && marker && xrayPID == 0:
				hit = true
				report("hit", "rollback_down")
				if !rb.seen {
					t.Error("D-16: маркер без события отката restart_failed_rolling_back")
				}
				if !rtFltDropBad() {
					t.Fatal("чужой файл Xray не удалён")
				}
				c.rtFltEnsureXray()
				before := c.rtRstApplyView()
				r := c.callSlow(http.MethodPost, "/api/configlayer/proxies/confirm", map[string]string{"group": gid})
				if r.Status != http.StatusOK {
					t.Fatalf("D-16: «Повторить»: %d %s", r.Status, r.Code)
				}
				if ok, _ := r.object(t)["confirmed"].(bool); !ok {
					t.Error("D-16: «Повторить» не подтвердило выбор")
				}
				if ok, code := rtRstResult(c.rtRstWaitApply(before)); !ok {
					t.Errorf("D-16: итог «Повторить» %q", code)
				}
				if _, marked := c.rtFltRuntime("Сбой"); marked {
					t.Error("D-16: маркер selection_unconfirmed остался после «Повторить»")
				}
				routertest.WaitKernelReady(t, "xray", rtFltXrayWait)
			case s.status == http.StatusOK && s.reverted && !marker && xrayPID == 0:
				failed = true
				report("hit", "rollback_none")
				t.Errorf("D-15: откат после частичного сбоя замены не перезапустил Xray (hotPartial): %s", s)
			default:
				failed = true
				t.Errorf("D-15: исход выбора при отвергаемом каталоге и падении Xray: %s killed=%t xray_up=%t rollback_event=%t (полная проверка не видела отвергаемый каталог или исход неизвестен)", s, k.killed, xrayPID > 0, rb.seen)
			}
		}
		if !hit && !failed {
			t.Logf("d15b: window=missed attempts=%d", attempts)
			t.Errorf("d15b: окно подтверждения не поймано")
		}
	})
}
