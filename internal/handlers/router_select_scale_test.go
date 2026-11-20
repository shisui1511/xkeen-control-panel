//go:build router

// router-area: routing

package handlers

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Масштаб горячего выбора Xray на конфигурации уровня UAT (G-150-3, пункт 2):
// настоящая подписка стенда, правила пресета со списками, серия горячих выборов и
// пара ожидания с порогами времени плана 150-17. Порог выведен из выборки с
// выбросом и плавает (класс flaky, todo 150-18-select-sporadic-io-stalls), поэтому
// тест в карантине; проверки правильности D-17 без порога идут в
// TestRouterRoutingSelectLive. Каждый выбор сопровождается замером носителя
// (/proc устройства) и составляющими этапа build из журнала панели — вход правил
// вердикта плана 150-23. Ошибки правильности начинаются с «корректность:»
// (находка продукта и в карантине), превышения порогов и бюджетов — с «порог:».
// Адрес подписки, подписи узлов, адреса и содержимое файлов не печатаются
// (T-150-45, T-150-54, T-150-63): в журнал теста — только числа, ID групп, коды и
// ID генераторов.

// rtRtSelectFixes — решение плана 150-17 по выборке масштаба UAT сессии 150-16
// (четыре строки select-stages: core=xray kind=scale): θ = min(1000; 0,2·T₁) =
// 779,9 мс; F2 (сверка и публикация после запуска выбора в фоне) — A = медиана
// after RunLive 663,5 < θ, не сработало; F4 (проверка Xray запуска выбора по
// изменённым исходящим с откатом к полной) — V = медиана validate_xray 1310 ≥ θ,
// сработало; F5 (снятие маркеров selection_unconfirmed в записи коммита) — M =
// медиана markers 46,5 < θ, не сработало. План 150-24 пересмотрел F2 и добавил F6
// по 24 строкам kind=scale сессии 150-23 (отчёт build/router/20261116-192531-ceb56a96),
// разброс r = второй сверху − второй снизу: F6 (сборка запуска выбора только
// генератором файла xcp-groups) срабатывает при медиане build ≥ θ, вариант — по
// наибольшей составляющей build (generate/plan — fired-gen, drift — fired-drift,
// check/routing — skip); медиана build 835 ≥ 779,9, наибольшая составляющая —
// generate 592 (drift 7,5, check 229, plan 1, routing 0), сработало fired-gen. F2
// срабатывает при медиане total > G = 2000 мс и медиане after ≥ 100 мс; медиана
// total 4296 > 2000, after 714,5 ≥ 100, сработало. IO — решение владельца 150-25
// (16.11.2026, Q1=B): маркер selection_unconfirmed снимается в записи коммита
// запуска выбора, опрос PID и запись xcp-groups с fsync остаются в ожидании ответа,
// смысл D-11 не меняется; поэтому F5 сработало по решению, а не по медиане M.
const rtRtSelectFixes = "F2=fired F4=fired F5=fired F6=fired-gen IO=B"

// rtRtSelectBudget — порог total строки этапов выбора Xray (только ядро xray) с
// wait ≤ 100 мс; rtRtSelectWaitBudget — с wait > 100 мс (выбор ждал не больше
// одного чужого прохода и прошёл свой). Формулы плана 150-17:
// B = вверх до 500 мс (T_max − R) + selectBusyWait (2 с),
// B_wait = 2 × вверх до 500 мс (T_max − R) + selectBusyWait (2 с),
// R = [F2]·A + [F5]·M — снятые этапы; сокращение F4 не вычитается.
// Выборка: отчёт build/router/20261116-143808-1686a2f2 (xray, перегон 2), total
// 3939, 3860, 3838, 16163 мс: T₁ = медиана 3899,5, T_max = 16163, разброс 12325
// (четвёртый выбор — выброс: validate_xray 9151, write 4441; максимум его
// учитывает). R = 0 (F2 и F5 не сработали): B = 16500 + 2000 = 18500 мс,
// B_wait = 2 × 16500 + 2000 = 35000 мс. Порог — регрессионный страж общего
// времени, эффект исправлений доказывают бюджеты этапов rtRtStageBudget.
const rtRtSelectBudget = 18500 * time.Millisecond

// rtRtSelectWaitBudget — порог total строки с wait > 100 мс (см. rtRtSelectBudget).
const rtRtSelectWaitBudget = 35000 * time.Millisecond

// rtRtWaitedMS — wait строки этапов, начиная с которого выбор считается ждавшим
// хода (порог rtRtSelectWaitBudget).
const rtRtWaitedMS = 100

// rtRtScaleSelects — число последовательных горячих выборов замера масштаба (план
// 150-21): две сессии по 12 выборов дают 24 строки для медиан и правил носителя.
const rtRtScaleSelects = 12

// rtRtStageBudget — бюджет медианы этапа строк kind=scale для сработавших правил
// с подтверждаемым эффектом: вниз до 100 мс от min(0,7·med; med − разброс) по
// выборке 150-16. F4 (validate_xray) — по устойчивой формуле 150-24 (решение
// владельца 150-25, Q2 = new-evidence): выборка 150-16 1350, 1189, 1270, 9151 мс,
// med 1310, r = второй сверху − второй снизу = 1350 − 1270 = 80, min(917; 1230) =
// 917 → 900 мс. Ключ after — медиана after строк runlive-stages: масштаба. План 150-24 по 24
// строкам сессии 150-23 (отчёт build/router/20261116-192531-ceb56a96): build (F6) —
// med 835, r 903 − 813 = 90, min(584,5; 745) → 500 мс; after (F2) — med 714,5,
// r 1166 − 633 = 533, min(500,2; 181,5) → 100 мс.
var rtRtStageBudget = map[string]time.Duration{
	"after":         100 * time.Millisecond,
	"build":         500 * time.Millisecond,
	"validate_xray": 900 * time.Millisecond,
}

// rtRtCheckSelectBudget сверяет total строки этапов выбора Xray с порогом по wait.
// В сообщении — пометка «порог:», вид выбора и числа.
func rtRtCheckSelectBudget(t *testing.T, kind string, st rtRtStage) {
	t.Helper()
	wait, total := st.num("wait"), st.num("total")
	limit := rtRtSelectBudget
	if wait > rtRtWaitedMS {
		limit = rtRtSelectWaitBudget
	}
	if total > limit.Milliseconds() {
		t.Errorf("порог: время выбора Xray (%s): total %d мс при wait %d мс, порог %d мс", kind, total, wait, limit.Milliseconds())
	}
}

// rtRtBuildRe — строка составляющих этапа build запуска выбора («configlayer:
// build trigger=select …»): только числа и ID генераторов.
var rtRtBuildRe = regexp.MustCompile(`configlayer: build (trigger=select(?: \S+=\S*)*)`)

// Номера системных вызовов fsync и fdatasync на arm64 (asm-generic/unistd.h):
// задача, стоящая в одном из них, ждёт записи на носитель.
const (
	rtRtSysFsync     = 82
	rtRtSysFdatasync = 83
)

// rtRtCarrierPoll — шаг опроса /proc замером носителя.
const rtRtCarrierPoll = 100 * time.Millisecond

// rtRtCarrierRes — итог замера носителя за окно одного выбора: суммы интервалов
// опроса, на конце которых признак держался, мс; максимумы и приросты счётчиков.
// Поле со значением -1 — источник недоступен (в строке — na).
type rtRtCarrierRes struct {
	samples                    int
	dstateMS, fsyncMS          int64
	inflightMS                 int64
	dirtyKBMax, writebackKBMax int64
	writeKB                    int64
	psiFullMS                  int64
	devWriteKB, devWriteMS     int64
}

// rtRtNA — значение поля строки замера: -1 — na.
func rtRtNA(v int64) string {
	if v < 0 {
		return "na"
	}
	return strconv.FormatInt(v, 10)
}

// rtRtCarrier — замер носителя на окне одного выбора: горутина без t.* опрашивает
// /proc каждые rtRtCarrierPoll от отправки запроса до появления строки этапов.
type rtRtCarrier struct {
	pid  int
	devs map[string]bool
	stop chan struct{}
	done chan rtRtCarrierRes
}

// rtRtDevOf — «major:minor» устройства, на котором лежит path, по
// /proc/self/mountinfo (точка монтирования с самым длинным префиксом пути).
func rtRtDevOf(path string) string {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	best, dev := -1, ""
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		mp := f[4]
		if mp != "/" && path != mp && !strings.HasPrefix(path, mp+"/") {
			continue
		}
		if len(mp) > best {
			best, dev = len(mp), f[2]
		}
	}
	return dev
}

// rtRtNewCarrier готовит замер: блочные устройства каталога Xray и каталога данных
// панели (по major:minor в /proc/diskstats) и PID панели. Нет устройства —
// inflight не меряется (na); нет PID — задачи и запись панели не меряются (na).
func rtRtNewCarrier() *rtRtCarrier {
	c := &rtRtCarrier{devs: map[string]bool{}}
	if pids, _ := routertest.PanelPIDs(); len(pids) > 0 {
		c.pid = pids[0]
	}
	dataDir := config.Default().DataDir
	if dataDir == "" {
		dataDir = "/opt/etc/xcp"
	}
	for _, p := range []string{rtRtXrayDir, dataDir} {
		if d := rtRtDevOf(p); d != "" && !strings.HasPrefix(d, "0:") {
			c.devs[d] = true
		}
	}
	return c
}

// rtRtCarrierSample — один отсчёт опроса /proc.
type rtRtCarrierSample struct {
	at                     time.Time
	dstate                 bool
	fsync                  int // 1 — задача в fsync, 0 — нет, -1 — syscall нечитаем
	inflight               int64
	devSectors, devWriteMS int64
	dirtyKB, writebackKB   int64
	writeBytes             int64
	psiFullUS              int64
}

// rtRtProcInt — целое из строки (ошибка — -1).
func rtRtProcInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// sample читает один отсчёт: /proc/diskstats, /proc/meminfo, /proc/<pid>/io,
// /proc/<pid>/task/*/stat и syscall, /proc/pressure/io.
func (c *rtRtCarrier) sample() rtRtCarrierSample {
	s := rtRtCarrierSample{at: time.Now(), fsync: -1, inflight: -1, devSectors: -1, devWriteMS: -1, writeBytes: -1, psiFullUS: -1}
	if len(c.devs) > 0 {
		if data, err := os.ReadFile("/proc/diskstats"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				f := strings.Fields(line)
				if len(f) < 12 || !c.devs[f[0]+":"+f[1]] {
					continue
				}
				if s.inflight < 0 {
					s.inflight, s.devSectors, s.devWriteMS = 0, 0, 0
				}
				s.devSectors += max(rtRtProcInt(f[9]), 0)
				s.devWriteMS += max(rtRtProcInt(f[10]), 0)
				s.inflight += max(rtRtProcInt(f[11]), 0)
			}
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			switch f[0] {
			case "Dirty:":
				s.dirtyKB = rtRtProcInt(f[1])
			case "Writeback:":
				s.writebackKB = rtRtProcInt(f[1])
			}
		}
	}
	if c.pid > 0 {
		base := "/proc/" + strconv.Itoa(c.pid)
		if data, err := os.ReadFile(base + "/io"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if v, ok := strings.CutPrefix(line, "write_bytes:"); ok {
					s.writeBytes = rtRtProcInt(strings.TrimSpace(v))
				}
			}
		}
		if tasks, err := os.ReadDir(base + "/task"); err == nil {
			for _, tk := range tasks {
				dir := base + "/task/" + tk.Name()
				if st, err := os.ReadFile(dir + "/stat"); err == nil {
					// Состояние — первое поле после имени в скобках (имя может
					// содержать пробелы и скобки, поэтому — после последней «)»).
					if i := strings.LastIndexByte(string(st), ')'); i >= 0 {
						if f := strings.Fields(string(st[i+1:])); len(f) > 0 && f[0] == "D" {
							s.dstate = true
						}
					}
				}
				sc, err := os.ReadFile(dir + "/syscall")
				if err != nil {
					continue
				}
				if s.fsync < 0 {
					s.fsync = 0
				}
				if f := strings.Fields(string(sc)); len(f) > 0 {
					if nr := rtRtProcInt(f[0]); nr == rtRtSysFsync || nr == rtRtSysFdatasync {
						s.fsync = 1
					}
				}
			}
		}
	}
	if data, err := os.ReadFile("/proc/pressure/io"); err == nil {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) == 0 || f[0] != "full" {
				continue
			}
			for _, kv := range f[1:] {
				if v, ok := strings.CutPrefix(kv, "total="); ok {
					s.psiFullUS = rtRtProcInt(v)
				}
			}
		}
	}
	return s
}

// start запускает опрос; вызывается до отправки запроса выбора.
func (c *rtRtCarrier) start() {
	c.stop, c.done = make(chan struct{}), make(chan rtRtCarrierRes, 1)
	go func() {
		first := c.sample()
		prev := first
		res := rtRtCarrierRes{samples: 1, dirtyKBMax: first.dirtyKB, writebackKBMax: first.writebackKB, fsyncMS: -1, inflightMS: -1}
		if first.fsync >= 0 {
			res.fsyncMS = 0
		}
		if first.inflight >= 0 {
			res.inflightMS = 0
		}
		tick := time.NewTicker(rtRtCarrierPoll)
		defer tick.Stop()
		for {
			select {
			case <-c.stop:
				res.writeKB, res.psiFullMS, res.devWriteKB, res.devWriteMS = -1, -1, -1, -1
				if first.writeBytes >= 0 && prev.writeBytes >= 0 {
					res.writeKB = (prev.writeBytes - first.writeBytes) / 1024
				}
				if first.psiFullUS >= 0 && prev.psiFullUS >= 0 {
					res.psiFullMS = (prev.psiFullUS - first.psiFullUS) / 1000
				}
				if first.devSectors >= 0 && prev.devSectors >= 0 {
					res.devWriteKB = (prev.devSectors - first.devSectors) / 2
					res.devWriteMS = prev.devWriteMS - first.devWriteMS
				}
				c.done <- res
				return
			case <-tick.C:
			}
			cur := c.sample()
			step := cur.at.Sub(prev.at).Milliseconds()
			res.samples++
			if cur.dstate {
				res.dstateMS += step
			}
			if cur.fsync < 0 {
				res.fsyncMS = -1
			} else if cur.fsync == 1 && res.fsyncMS >= 0 {
				res.fsyncMS += step
			}
			if cur.inflight > 0 && res.inflightMS >= 0 {
				res.inflightMS += step
			}
			res.dirtyKBMax = max(res.dirtyKBMax, cur.dirtyKB)
			res.writebackKBMax = max(res.writebackKBMax, cur.writebackKB)
			// Счётчик, пропавший посреди окна (процесс ушёл), не портит прирост.
			if cur.writeBytes < 0 {
				cur.writeBytes = prev.writeBytes
			}
			if cur.psiFullUS < 0 {
				cur.psiFullUS = prev.psiFullUS
			}
			if cur.devSectors < 0 {
				cur.devSectors, cur.devWriteMS = prev.devSectors, prev.devWriteMS
			}
			prev = cur
		}
	}()
}

// finish останавливает опрос и возвращает итог окна.
func (c *rtRtCarrier) finish() rtRtCarrierRes {
	close(c.stop)
	return <-c.done
}

// rtRtBuildLines — строки составляющих build окна журнала с offset (только пары).
func rtRtBuildLines(offset int64) []string {
	text, err := rtReadLogFrom(rtRtLogPath(), offset)
	if err != nil {
		text, _ = rtReadLogFrom(rtRtLogPath(), 0)
	}
	var out []string
	for _, m := range rtRtBuildRe.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// rtRtScaleUAT снимает этапы горячего выбора Xray на конфигурации уровня UAT
// (G-150-3): настоящая подписка стенда в «Выбор» (sources.all), правила пресета со
// списками geosite/geoip из dat-файлов стенда, «Применить», rtRtScaleSelects
// последовательных горячих выборов с замером носителя и составляющими build и пара
// ожидания (rtRtLongwaitPair, kind=longwait) с порогом времени. Адрес подписки,
// ответы источников с ним, подписи узлов, адреса и содержимое файлов не печатаются
// (T-150-45, T-150-54): в журнал теста — только числа, ID групп и коды.
func (e *rtSrc) rtRtScaleUAT(subURL, gid, secondID string, runtimeView func() map[string]any) {
	t := e.t
	t.Helper()

	// Подписка удаляется при любом исходе блока, до общей очистки теста.
	t.Cleanup(func() {
		c := e.with(t)
		for i := 0; i < 5; i++ {
			id := c.rtDNSSubscriptionID()
			if id == "" {
				break
			}
			if r := c.call(http.MethodPost, rtSourcesBase+"/delete", map[string]any{"revision": c.revision(), "id": id}); r.Status != http.StatusOK {
				t.Errorf("очистка масштаба: удаление подписки: %d %s", r.Status, r.Code)
				return
			}
		}
		if c.rtRtDraftChanges() > 0 {
			c.rtRtApplyOK("очистка масштаба: подписка")
		}
	})
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": subURL})
	if pv.Status != http.StatusOK {
		t.Fatalf("масштаб: sources/preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(t)["token"].(string)
	if add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token}); add.Status != http.StatusOK {
		t.Fatalf("масштаб: sources/add: %d %s", add.Status, add.Code)
	}
	subID := e.rtDNSSubscriptionID()
	if subID == "" {
		t.Fatal("масштаб: подписки стенда нет в источниках после добавления")
	}

	// Правила пресета со списками: «всё, кроме страны» с RU, если RU есть среди
	// стран пресетов, иначе «всё через прокси». Маршрутизация уже взята.
	pr := e.call(http.MethodGet, rtRoutingBase+"/presets", nil)
	if pr.Status != http.StatusOK {
		t.Fatalf("масштаб: GET presets: %d %s", pr.Status, pr.Code)
	}
	hasRU := false
	presets, _ := pr.object(t)["presets"].([]any)
	for _, p := range presets {
		pm, _ := p.(map[string]any)
		cs, _ := pm["countries"].([]any)
		for _, c := range cs {
			if c == "RU" {
				hasRU = true
			}
		}
	}
	flags := map[string]bool{"quic": false, "torrent": false, "ads": false, "service_groups": false}
	choice := map[string]any{"id": "all_proxy", "flags": flags}
	if hasRU {
		choice = map[string]any{"id": "except_country", "country": "RU", "flags": flags}
	}
	pl := e.call(http.MethodPost, rtRoutingBase+"/presets/plan", map[string]any{"choice": choice})
	if pl.Status != http.StatusOK {
		t.Fatalf("масштаб: presets/plan: %d %s", pl.Status, pl.Code)
	}
	if add, _ := pl.object(t)["add"].([]any); len(add) == 0 {
		t.Errorf("масштаб: presets/plan (%s) не добавляет правил", choice["id"])
	}
	if ap := e.call(http.MethodPost, rtRoutingBase+"/presets/apply", map[string]any{"revision": e.rtRtRevision(), "choice": choice, "take": false}); ap.Status != http.StatusOK {
		t.Fatalf("масштаб: presets/apply: %d %s", ap.Status, ap.Code)
	}
	e.rtRtApplyOK("масштаб UAT")
	draftScale := e.rtRtDraftChanges()

	// Конфигурация замера: узлы «Выбор», совместимые с Xray, правила и
	// условия-списки, размеры файлов панели (только числа).
	rv := runtimeView()
	rg := rtRtFindGroup(rv, "Выбор")
	if rg == nil {
		t.Fatal("масштаб: нет группы «Выбор» в виде «Прокси»")
	}
	members, _ := rg["members"].([]any)
	var cands []string
	xrayNodes := 0
	for _, it := range members {
		m, _ := it.(map[string]any)
		if m["kind"] != "node" || m["xray"] != true {
			continue
		}
		xrayNodes++
		if m["source"] == subID {
			member, _ := m["member"].(string)
			cands = append(cands, member)
		}
	}
	rules, lists := 0, 0
	rs, _ := e.rtRtView()["rules"].([]any)
	for _, it := range rs {
		r, _ := it.(map[string]any)
		rules++
		conds, _ := r["conds"].([]any)
		for _, ci := range conds {
			c, _ := ci.(map[string]any)
			ls, _ := c["lists"].([]any)
			if c["type"] == "geosite" || c["type"] == "geoip" || len(ls) > 0 {
				lists++
			}
		}
	}
	size := func(name string) int64 {
		st, err := os.Stat(filepath.Join(rtRtXrayDir, name))
		if err != nil {
			return 0
		}
		return st.Size()
	}
	t.Logf("scale-config: core=xray nodes=%d rules=%d lists=%d groups_bytes=%d routing_bytes=%d",
		xrayNodes, rules, lists, size("04_outbounds.xcp-groups.tail.json"), size("99_xcp-routing.json"))
	if xrayNodes < 20 {
		t.Logf("scale: узлов группы, совместимых с Xray, %d (меньше 20): замер менее представителен", xrayNodes)
	}
	// Выборы замера — разные узлы подписки, пока они есть, затем повторно.
	if len(cands) < 2 {
		t.Fatalf("масштаб: узлов подписки, совместимых с Xray, %d, ожидалось не меньше 2", len(cands))
	}
	if len(cands) <= rtRtScaleSelects {
		t.Logf("scale: узлов подписки %d при %d выборах — часть узлов выбирается повторно", len(cands), rtRtScaleSelects)
	}
	used := map[string]bool{}
	pick := func(cur string) string {
		for _, m := range cands {
			if m != cur && !used[m] {
				used[m] = true
				return m
			}
		}
		for _, m := range cands {
			if m != cur {
				return m
			}
		}
		return ""
	}
	currentOf := func(name string) string {
		g := rtRtFindGroup(e.rtRtView(), name)
		if g == nil {
			t.Fatalf("масштаб: нет группы %s в представлении", name)
		}
		cur, _ := g["current"].(string)
		return cur
	}
	expectHot := func(label string, obj map[string]any) {
		ks, _ := obj["kernels"].(map[string]any)
		if out, _ := ks["xray"].(string); out != "applied" {
			t.Errorf("корректность: %s: исход Xray %q, ожидалось applied", label, out)
		}
		if reason, present := obj["restart_reason"]; present {
			t.Errorf("корректность: %s: горячая смена потребовала перезапуск: restart_reason=%v", label, reason)
		}
	}
	pids, _ := rtRstKernelPIDs("xray")
	if len(pids) != 1 {
		t.Fatalf("масштаб: до выборов PID Xray %d, ожидался один", len(pids))
	}

	// Последовательные горячие выборы: строка RunLive окна принадлежит своему
	// выбору. На каждый — замер носителя от отправки до строки этапов и строка
	// составляющих build того же окна. Порог total — rtRtSelectBudget и
	// rtRtSelectWaitBudget, медианы этапов сработавших правил — rtRtStageBudget
	// (план 150-17).
	scaleVals := map[string][]int64{}
	for n := 1; n <= rtRtScaleSelects; n++ {
		member := pick(currentOf("Выбор"))
		offset := rtRtLogOffset(t)
		carrier := rtRtNewCarrier()
		carrier.start()
		start := time.Now()
		r := e.callSlow(http.MethodPost, rtRoutingBase+"/select", map[string]string{"group": gid, "member": member})
		httpMS := time.Since(start).Milliseconds()
		if r.Status != http.StatusOK {
			carrier.finish()
			t.Fatalf("корректность: масштаб: выбор №%d: %d %s, ожидалось 200", n, r.Status, r.Code)
		}
		expectHot("масштаб: выбор №"+strconv.Itoa(n), r.object(t))
		stages, runlive, ok := rtRtWaitStages(offset, []string{gid}, 1, rtRtStagesWait("xray"))
		cr := carrier.finish()
		t.Logf("carrier: core=xray kind=scale n=%d samples=%d dstate_ms=%d fsync_ms=%s inflight_ms=%s dirty_kb_max=%d writeback_kb_max=%d write_kb=%s psi_full_ms=%s dev_write_kb=%s dev_write_ms=%s",
			n, cr.samples, cr.dstateMS, rtRtNA(cr.fsyncMS), rtRtNA(cr.inflightMS), cr.dirtyKBMax, cr.writebackKBMax,
			rtRtNA(cr.writeKB), rtRtNA(cr.psiFullMS), rtRtNA(cr.devWriteKB), rtRtNA(cr.devWriteMS))
		if !ok {
			t.Errorf("масштаб: выбор №%d: в журнале панели нет строки этапов или RunLive (строк RunLive %d)", n, len(runlive))
			continue
		}
		st := stages[gid]
		t.Logf("select-stages: core=xray kind=scale n=%d http=%d %s", n, httpMS, st.pairs)
		for _, rl := range runlive {
			t.Logf("runlive-stages: core=xray kind=scale n=%d %s", n, rl)
			if after := rtRtRunLiveNum(rl, "after"); after >= 0 {
				scaleVals["after"] = append(scaleVals["after"], after)
			}
		}
		builds := rtRtBuildLines(offset)
		if len(builds) == 0 {
			t.Errorf("масштаб: выбор №%d: в журнале панели нет строки составляющих build", n)
		}
		for _, b := range builds {
			t.Logf("build-stages: core=xray kind=scale n=%d %s", n, b)
		}
		rtRtCheckSelectBudget(t, "масштаб №"+strconv.Itoa(n), st)
		for stage := range rtRtStageBudget {
			if stage != "after" {
				scaleVals[stage] = append(scaleVals[stage], st.num(stage))
			}
		}
	}
	// Эффект сработавших правил: медиана этапа по строкам масштаба не больше бюджета.
	stageKeys := make([]string, 0, len(rtRtStageBudget))
	for stage := range rtRtStageBudget {
		stageKeys = append(stageKeys, stage)
	}
	sort.Strings(stageKeys)
	for _, stage := range stageKeys {
		budget := rtRtStageBudget[stage]
		vals := scaleVals[stage]
		if len(vals) == 0 {
			t.Errorf("порог: масштаб: этап %s: нет значений в строках этапов, бюджет %d мс", stage, budget.Milliseconds())
			continue
		}
		if med := rtRtMedian(vals); med > float64(budget.Milliseconds()) {
			t.Errorf("порог: масштаб: медиана этапа %s %.1f мс по %d выборам, бюджет %d мс", stage, med, len(vals), budget.Milliseconds())
		}
	}
	checkAfter := func(label string, groups ...string) {
		after, _ := rtRstKernelPIDs("xray")
		if len(after) != 1 || after[0] != pids[0] {
			t.Errorf("корректность: %s: PID Xray прежний=%t, процессов %d", label, reflect.DeepEqual(after, pids), len(after))
		}
		v := runtimeView()
		for _, name := range groups {
			g := rtRtFindGroup(v, name)
			if g == nil || g["switching"] != false || g["unconfirmed"] != false {
				t.Errorf("корректность: %s: группа %s не в покое (есть=%t)", label, name, g != nil)
			}
		}
		if e.rtRtDraftChanges() != draftScale {
			t.Errorf("корректность: %s: выборы изменили черновик", label)
		}
	}
	checkAfter("масштаб", "Выбор")

	// Пара ожидания: условие отказа G-150-3 на проходе уровня UAT с порогом
	// времени. Длительность прохода тест не подстраивает.
	e.rtRtLongwaitPair("xray", "longwait", gid, secondID, rtRtCheckSelectBudget, runtimeView)
	checkAfter("масштаб: пара ожидания", "Выбор", "Вторая")
}

// Масштаб горячего выбора Xray (G-150-3, пункт 2; карантин снят по вердикту
// повторного замера 150-40 — условия У1–У5 выполнены): подготовка —
// слой, очистка, четыре узла «Моих узлов» на «чёрной дыре», группы «Выбор» и
// «Вторая», взятие маршрутизации, «Применить»; затем rtRtScaleUAT. Метка ядра:
// горячая замена исходящих есть только у Xray, на Mihomo тест пропускается. Без
// подписки стенда (XCP_RT_SUBSCRIPTION_URL) тест пропускается: класс stand.
func TestRouterRoutingSelectScale(t *testing.T) {
	routertest.Slow(t, "конфигурация уровня UAT: подписка, «Применить» и серия горячих выборов Xray")
	tg := routertest.CoreSpecific(t)
	if tg.Core != "xray" {
		t.Skip("ядро ветви не Xray: горячего выбора Xray нет")
	}
	subURL := os.Getenv("XCP_RT_SUBSCRIPTION_URL")
	if subURL == "" {
		t.Skip("нет XCP_RT_SUBSCRIPTION_URL: замеру масштаба нужна подписка стенда (класс stand)")
	}
	t.Logf("select-fixes: %s", rtRtSelectFixes)
	e := rtNewSrc(t)
	e.freshDraft()
	e.rtRstIdle()
	e.rtRtPurge()
	taken := false

	// Подписка, оставшаяся от прерванного прогона, удаляется до подготовки; число
	// подписок до удаления и после всех очисток — только числа.
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
	// Выполняется последней: подписок после всех очисток не остаётся.
	t.Cleanup(func() {
		n := e.with(t).rtRtSourcesCount()
		t.Logf("sources-end: core=%s subscriptions=%d", tg.Core, n)
		if n > 0 {
			t.Errorf("очистка: подписка осталась в источниках (%d)", n)
		}
	})
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

	// «Мои узлы» держат группы непустыми при удалении подписки в очистке (иначе
	// «Применить» очистки отверг бы пустую группу); подписи различны, чтобы у пары
	// ожидания были цели, отличные от текущего выбора.
	hole := rtRtBlackhole(t)
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_ws", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "trojan_grpc", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_grpc", hole))
	e.rtRstAddMyNode(rtRtLinkAt(t, "vless_tls_alpn", hole))

	saveGroup := func(name string) string {
		r := e.call(http.MethodPost, rtRoutingBase+"/groups/save", map[string]any{
			"revision": e.rtRtRevision(),
			"group": map[string]any{
				"name": name, "mode": "manual",
				"sources": map[string]any{"all": true},
				"members": []map[string]string{},
			},
		})
		if r.Status != http.StatusOK {
			t.Fatalf("groups/save: %d %s", r.Status, r.Code)
		}
		id, _ := r.object(t)["group"].(map[string]any)["id"].(string)
		if len(id) != 8 {
			t.Fatalf("groups/save: идентификатор группы длиной %d, ожидалось 8", len(id))
		}
		return id
	}
	gid := saveGroup("Выбор")
	secondID := saveGroup("Вторая")
	if tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()}); tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	taken = true
	e.rtRtApplyOK("взятие")
	e.rtRtRebuildReleased()

	runtimeView := func() map[string]any {
		r := e.call(http.MethodGet, "/api/configlayer/proxies", nil)
		if r.Status != http.StatusOK {
			t.Fatalf("proxies: %d %s", r.Status, r.Code)
		}
		return r.object(t)
	}
	e.rtRtScaleUAT(subURL, gid, secondID, runtimeView)
}
