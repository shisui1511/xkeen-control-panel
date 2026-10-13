//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// «Применить» и выключение флага слоя при запущенном ядре на настоящей панели
// устройства. Вердикт — процессы ядра в /proc (pidof, независимо от панели), файлы
// слоя на диске и итог конвейера из GET /api/configlayer/state. Перезапуск идёт
// через настоящий XKeen: «xkeen -restart» возвращается раньше, чем завершается
// старый процесс ядра, и подтверждение перезапуска не должно засчитывать его как
// «запущен» (D-28, FND-06). Узел — обезличенная фикстура internal/nodes/testdata/links.

const (
	// rtRstApplyTimeout — предел ожидания итога «Применить»: на mipsle XKeen
	// перезапускает ядро десятки секунд, а подтверждение ждёт до 90 с на ядро.
	rtRstApplyTimeout = 6 * time.Minute
	// rtRstHold — сколько ядро обязано держаться с новым PID после итога.
	rtRstHold = 10 * time.Second
)

// with возвращает клиента, привязанного к подтесту: провал помощников относится к нему.
func (e *rtSrc) with(t *testing.T) *rtSrc {
	c := *e
	c.t = t
	return &c
}

// rtRstKernelPIDs читает PID процессов ядра независимо от панели.
func rtRstKernelPIDs(name string) (pids []int, out string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, "pidof", name)
	out = strings.TrimSpace(stdout + " " + stderr)
	if code != 0 {
		return nil, out
	}
	for _, f := range strings.Fields(stdout) {
		if n, err := strconv.Atoi(f); err == nil && n > 0 && !rtRstHelperProcess(n, name) {
			pids = append(pids, n)
		}
	}
	return pids, out
}

// rtRstHelperProcess — короткоживущий вспомогательный запуск бинарника ядра, а не само
// ядро: проверка конфигурации («-test»/«-t») и запрос версии, которые панель и XKeen
// запускают рядом с перезапуском. Различается по аргументам из /proc/<pid>/cmdline;
// процесс, исчезнувший к моменту чтения, и процесс, ещё не сменивший образ после
// fork (argv[0] не бинарник ядра), тоже не считаются ядром.
func rtRstHelperProcess(pid int, name string) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return true
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if filepath.Base(args[0]) != name {
		return true
	}
	for _, a := range args[1:] {
		switch a {
		case "-test", "--test", "-t", "version", "-v", "-V", "--version":
			return true
		}
	}
	return false
}

// rtRstKernelPID — PID работающего ядра; не ровно один процесс — провал.
func rtRstKernelPID(t *testing.T, name string) int {
	t.Helper()
	pids, out := rtRstKernelPIDs(name)
	if len(pids) != 1 {
		t.Fatalf("процессов %s: %d (pidof: %q), ожидался ровно один", name, len(pids), out)
	}
	return pids[0]
}

// rtRstHoldPID требует, чтобы ядро держалось с одним PID rtRstHold подряд.
func rtRstHoldPID(t *testing.T, name string, pid int) {
	t.Helper()
	deadline := time.Now().Add(rtRstHold)
	for time.Now().Before(deadline) {
		pids, out := rtRstKernelPIDs(name)
		if len(pids) != 1 || pids[0] != pid {
			t.Fatalf("ядро %s не держится с PID %d после итога: pidof %q", name, pid, out)
		}
		time.Sleep(time.Second)
	}
}

// rtRstApplyView — apply из GET /api/configlayer/state.
func (e *rtSrc) rtRstApplyView() map[string]any {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	apply, _ := r.object(e.t)["apply"].(map[string]any)
	if apply == nil {
		e.t.Fatalf("в состоянии слоя нет apply: %.200s", r.Data)
	}
	return apply
}

// rtRstIdle ждёт конца фонового применения (сборка после включения слоя): «не идёт»
// должно продержаться три опроса подряд, иначе сборка, ещё не взявшая замок, осталась бы незамеченной.
func (e *rtSrc) rtRstIdle() {
	e.t.Helper()
	deadline := time.Now().Add(rtRstApplyTimeout)
	calm := 0
	for calm < 3 {
		if time.Now().After(deadline) {
			e.t.Fatalf("фоновое применение не закончилось за %v", rtRstApplyTimeout)
		}
		if running, _ := e.rtRstApplyView()["running"].(bool); running {
			calm = 0
		} else {
			calm++
		}
		time.Sleep(time.Second)
	}
}

// rtRstWaitApply ждёт итог запущенного «Применить» и возвращает apply. before — apply
// до запуска: по нему итог нового запуска отличается от результата предыдущего.
func (e *rtSrc) rtRstWaitApply(before map[string]any) map[string]any {
	e.t.Helper()
	prev, _ := json.Marshal(before)
	start := time.Now()
	seen := false
	for {
		a := e.rtRstApplyView()
		if running, _ := a["running"].(bool); running {
			seen = true
		} else if a["result"] != nil {
			cur, _ := json.Marshal(a)
			if seen || string(cur) != string(prev) || time.Since(start) > 5*time.Second {
				return a
			}
		}
		if time.Since(start) > rtRstApplyTimeout {
			e.t.Fatalf("итог применения не получен за %v: %s", rtRstApplyTimeout, rtRstDescribe(a))
		}
		time.Sleep(time.Second)
	}
}

// rtRstDescribe — итог применения для сообщений: код, текст, исходы перезапуска и шаги.
func rtRstDescribe(a map[string]any) string {
	raw, _ := json.Marshal(map[string]any{"result": a["result"], "restart": a["restart"], "steps": a["steps"]})
	return string(raw)
}

// rtRstStartApply запускает «Применить» кнопкой и возвращает apply до запуска.
func (e *rtSrc) rtRstStartApply() map[string]any {
	e.t.Helper()
	before := e.rtRstApplyView()
	r := e.call(http.MethodPost, "/api/configlayer/apply", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("POST /api/configlayer/apply: %d %s", r.Status, r.Code)
	}
	if started, _ := r.object(e.t)["started"].(bool); !started {
		e.t.Fatalf("POST /api/configlayer/apply: started=false: %.200s", r.Data)
	}
	return before
}

// rtRstResult возвращает result итога применения.
func rtRstResult(a map[string]any) (ok bool, code string) {
	res, _ := a["result"].(map[string]any)
	ok, _ = res["ok"].(bool)
	code, _ = res["code"].(string)
	return ok, code
}

// rtRstRestartOutcome — исход перезапуска ядра из apply.restart ("" — записи нет).
func rtRstRestartOutcome(a map[string]any, kernel string) string {
	list, _ := a["restart"].([]any)
	for _, it := range list {
		m, _ := it.(map[string]any)
		if m["kernel"] == kernel {
			s, _ := m["outcome"].(string)
			return s
		}
	}
	return ""
}

// rtRstLayerFile — путь файла слоя из GET /api/configlayer/state, оканчивающийся на suffix.
func (e *rtSrc) rtRstLayerFile(suffix string) string {
	e.t.Helper()
	r := e.call(http.MethodGet, "/api/configlayer/state", nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("GET /api/configlayer/state: %d %s", r.Status, r.Code)
	}
	files, _ := r.object(e.t)["files"].([]any)
	for _, f := range files {
		m, _ := f.(map[string]any)
		if p, _ := m["path"].(string); strings.HasSuffix(p, suffix) {
			return p
		}
	}
	return ""
}

func rtRstExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// rtRstMyEntry возвращает entry_id первого узла «Моих узлов» ("" — узлов нет).
func (e *rtSrc) rtRstMyEntry() string {
	e.t.Helper()
	view := e.call(http.MethodGet, rtSourcesBase, nil)
	if view.Status != http.StatusOK {
		e.t.Fatalf("GET %s: %d %s", rtSourcesBase, view.Status, view.Code)
	}
	srcs, _ := view.object(e.t)["sources"].([]any)
	for _, s := range srcs {
		src, _ := s.(map[string]any)
		if src["kind"] != "my" {
			continue
		}
		nodes, _ := src["nodes"].([]any)
		if len(nodes) > 0 {
			id, _ := nodes[0].(map[string]any)["entry_id"].(string)
			return id
		}
	}
	return ""
}

// rtRstDeleteMyNode удаляет узел «Моих узлов» из черновика.
func (e *rtSrc) rtRstDeleteMyNode(entryID string) {
	e.t.Helper()
	del := e.call(http.MethodPost, rtSourcesBase+"/nodes/delete", map[string]any{"revision": e.revision(), "entry_id": entryID})
	if del.Status != http.StatusOK {
		e.t.Fatalf("nodes/delete: %d %s", del.Status, del.Code)
	}
}

// rtRstAddMyNode добавляет в черновик узел из ссылки (предпросмотр и «Добавить»).
func (e *rtSrc) rtRstAddMyNode(link string) {
	e.t.Helper()
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": link})
	if pv.Status != http.StatusOK {
		e.t.Fatalf("preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(e.t)["token"].(string)
	if token == "" {
		e.t.Fatalf("preview без токена: %.200s", pv.Data)
	}
	add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token})
	if add.Status != http.StatusOK {
		e.t.Fatalf("add: %d %s", add.Status, add.Code)
	}
}

// rtRstExpectApplied проверяет итог «Применить» с изменением: ok, код applied, отката нет.
func rtRstExpectApplied(t *testing.T, a map[string]any) {
	t.Helper()
	ok, code := rtRstResult(a)
	res, _ := a["result"].(map[string]any)
	if rb, _ := res["rolled_back"].(bool); rb || !ok || code != "applied" {
		t.Fatalf("итог применения: ok=%v code=%q rolled_back=%v; %s", ok, code, rb, rtRstDescribe(a))
	}
}

func TestRouterConfigApplyRestartsRunningKernel(t *testing.T) {
	tg := routertest.Current(t)
	kernel := tg.Core
	if kernel != "xray" && kernel != "mihomo" {
		t.Fatalf("неизвестное ядро цели %q", kernel)
	}
	nodeFile := "04_outbounds.xcp-my.tail.json"
	if kernel == "mihomo" {
		nodeFile = "proxy_providers/xcp-my.yaml"
	}
	link := rtLinkText(t, "vless_ws")

	// Ядро активной ветви матрицы обязано работать до начала сценария.
	rtRstKernelPID(t, kernel)

	root := rtNewSrc(t)
	var layerPath string // путь файла слоя «Моих узлов» текущего ядра

	// Провал посреди сценария не оставляет узел в применённом состоянии: следующие
	// проверки работают с тем же стендом. Выполняется до возврата флага слоя.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		root.setLayer(true)
		root.rtRstIdle()
		if id := root.rtRstMyEntry(); id != "" {
			root.rtRstDeleteMyNode(id)
			before := root.rtRstStartApply()
			root.rtRstWaitApply(before)
			root.rtRstIdle()
		}
	})

	t.Run("подготовка", func(t *testing.T) {
		e := root.with(t)
		e.setLayer(true)
		e.rtRstIdle()
		e.resetDraft()
		// Узел прошлых прогонов не должен превращать «Применить» в «нечего применять».
		if id := e.rtRstMyEntry(); id != "" {
			e.rtRstDeleteMyNode(id)
			before := e.rtRstStartApply()
			e.rtRstWaitApply(before)
			e.rtRstIdle()
		}
		rtRstKernelPID(t, kernel)
	})

	t.Run("применить с узлом", func(t *testing.T) {
		e := root.with(t)
		e.rtRstAddMyNode(link)
		pidBefore := rtRstKernelPID(t, kernel)
		before := e.rtRstStartApply()
		a := e.rtRstWaitApply(before)
		rtRstExpectApplied(t, a)
		layerPath = e.rtRstLayerFile(nodeFile)
		if layerPath == "" || !rtRstExists(layerPath) {
			t.Fatalf("файл слоя %s не найден на диске (путь %q); %s", nodeFile, layerPath, rtRstDescribe(a))
		}
		out := rtRstRestartOutcome(a, kernel)
		switch kernel {
		case "xray":
			if out != "restarted" {
				t.Fatalf("исход перезапуска xray %q, ожидался restarted; %s", out, rtRstDescribe(a))
			}
			pidAfter := rtRstKernelPID(t, kernel)
			if pidAfter == pidBefore {
				t.Fatalf("PID xray не сменился после перезапуска (%d); %s", pidBefore, rtRstDescribe(a))
			}
			rtRstHoldPID(t, kernel, pidAfter)
		default:
			switch out {
			case "no_runtime_effect", "hot_reloaded", "restarted", "restarted_after_reload_failed":
			default:
				t.Fatalf("исход перезапуска mihomo %q вне допустимых; %s", out, rtRstDescribe(a))
			}
			rtRstKernelPID(t, kernel)
		}
	})

	t.Run("применить без изменений", func(t *testing.T) {
		e := root.with(t)
		pidBefore := rtRstKernelPID(t, kernel)
		before := e.rtRstStartApply()
		a := e.rtRstWaitApply(before)
		ok, code := rtRstResult(a)
		if !ok || (code != "nothing_to_apply" && code != "applied") {
			t.Fatalf("итог применения без изменений: ok=%v code=%q; %s", ok, code, rtRstDescribe(a))
		}
		switch out := rtRstRestartOutcome(a, kernel); out {
		case "restarted", "restarted_after_reload_failed", "failed_rolled_back", "failed_rollback_failed", "failed_kernel_down", "interrupted":
			t.Fatalf("«Применить» без изменений дало исход перезапуска %q; %s", out, rtRstDescribe(a))
		}
		if pid := rtRstKernelPID(t, kernel); pid != pidBefore {
			t.Fatalf("PID %s сменился без перезапуска: %d -> %d", kernel, pidBefore, pid)
		}
		if !rtRstExists(layerPath) {
			t.Fatalf("файл слоя %s пропал", layerPath)
		}
	})

	t.Run("выключение флага", func(t *testing.T) {
		e := root.with(t)
		pidBefore := rtRstKernelPID(t, kernel)
		r := e.callSlow(http.MethodPost, "/api/settings/config-layer", map[string]bool{"enabled": false})
		// Mihomo без external-controller (так настроен стенд mipsle) подтверждается
		// только по процессу: Clash API у него нет, ждать его незачем.
		if r.Status != http.StatusOK {
			t.Fatalf("выключение слоя: %d %s %s", r.Status, r.Code, r.Detail)
		}
		pid := rtRstKernelPID(t, kernel)
		if kernel == "xray" {
			if pid == pidBefore {
				t.Fatalf("PID xray не сменился при выключении слоя (%d)", pidBefore)
			}
			rtRstHoldPID(t, kernel, pid)
		}
		if rtRstExists(layerPath) {
			t.Fatalf("файл слоя %s остался после выключения", layerPath)
		}
	})

	t.Run("возврат стенда", func(t *testing.T) {
		e := root.with(t)
		e.setLayer(true)
		e.rtRstIdle()
		id := e.rtRstMyEntry()
		if id == "" {
			t.Fatal("после повторного включения слоя узла «Моих узлов» нет")
		}
		e.rtRstDeleteMyNode(id)
		pidBefore := rtRstKernelPID(t, kernel)
		before := e.rtRstStartApply()
		a := e.rtRstWaitApply(before)
		rtRstExpectApplied(t, a)
		if kernel == "xray" {
			if out := rtRstRestartOutcome(a, kernel); out != "restarted" {
				t.Fatalf("исход перезапуска xray %q, ожидался restarted; %s", out, rtRstDescribe(a))
			}
			pid := rtRstKernelPID(t, kernel)
			if pid == pidBefore {
				t.Fatalf("PID xray не сменился (%d); %s", pidBefore, rtRstDescribe(a))
			}
			rtRstHoldPID(t, kernel, pid)
		} else {
			rtRstKernelPID(t, kernel)
		}
		if layerPath != "" && rtRstExists(layerPath) {
			t.Fatalf("файл слоя %s не удалён после удаления узла", layerPath)
		}
	})
}
