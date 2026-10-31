//go:build router

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Живые сценарии секций ядер на настоящей панели устройства (SEC-02, D-10, D-16):
// XKeen продолжает читать порты и теги входящих, взятых панелью, а способ
// перезапуска, объявленный до «Применить», совпадает с фактическим. Вердикт —
// правила iptables цепочек XKeen (nat REDIRECT --to-ports, mangle TPROXY
// --on-port), `xkeen -status`, процессы ядер в /proc и файлы ядер на диске
// устройства; ответы панели — только вспомогательные. Флаг слоя «Конфигурация» на
// время сценариев переключается и возвращается прежним; взятое возвращается
// «Отпустить» + «Применить» при любом исходе, исходные файлы сверяются по sha256,
// черновик сбрасывается. В сообщения попадают только коды, порты фикстур и числа.

const (
	// rtCoresLiveXrayPanelFile — файл панели секции «Входящие» Xray.
	rtCoresLiveXrayPanelFile = "03_inbounds.xcp-inbounds.json"
	// rtCoresLiveRulesWait — сколько ждать, пока XKeen переставит правила перехвата.
	rtCoresLiveRulesWait = 90 * time.Second
	// rtCoresLivePortFrom, rtCoresLivePortTo — диапазон портов фикстур смены порта.
	rtCoresLivePortFrom = 61180
	rtCoresLivePortTo   = 61199
)

// rtCoresLiveForceProxy — префикс тегов служебных входящих, которые XKeen не
// берёт при выборе порта.
const rtCoresLiveForceProxy = "force-proxy"

// rtCoresLiveInbound — прозрачное входящее Xray из значения секции «Входящие».
type rtCoresLiveInbound struct {
	tag  string
	mode string // redirect или tproxy
	port int
	raw  map[string]any
}

// rtCoresLiveInt приводит число из JSON или YAML к int; не число — 0.
func rtCoresLiveInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case string:
		p, err := strconv.Atoi(strings.TrimSpace(n))
		if err == nil {
			return p
		}
	}
	return 0
}

// rtCoresLiveMode — вид прозрачного входящего по правилам XKeen (143 Q7): протокол
// dokodemo-door или tunnel, settings.followRedirect, sockopt.tproxy пуст,
// redirect или tproxy. Пустая строка — входящее не прозрачное.
func rtCoresLiveMode(in map[string]any) string {
	if proto, _ := in["protocol"].(string); proto != "dokodemo-door" && proto != "tunnel" {
		return ""
	}
	settings, _ := in["settings"].(map[string]any)
	if follow, _ := settings["followRedirect"].(bool); !follow {
		return ""
	}
	stream, _ := in["streamSettings"].(map[string]any)
	sockopt, _ := stream["sockopt"].(map[string]any)
	switch mode, _ := sockopt["tproxy"].(string); mode {
	case "", "redirect":
		return "redirect"
	case "tproxy":
		return "tproxy"
	}
	return ""
}

// rtCoresLiveItems приводит значение секции «Входящие» к списку объектов.
func rtCoresLiveItems(v any) []map[string]any {
	list, _ := v.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// rtCoresLiveTransparent выбирает прозрачные входящие с числовым портом в порядке
// значения.
func rtCoresLiveTransparent(items []map[string]any) []rtCoresLiveInbound {
	var out []rtCoresLiveInbound
	for _, it := range items {
		mode := rtCoresLiveMode(it)
		port := rtCoresLiveInt(it["port"])
		if mode == "" || port <= 0 {
			continue
		}
		tag, _ := it["tag"].(string)
		out = append(out, rtCoresLiveInbound{tag: tag, mode: mode, port: port, raw: it})
	}
	return out
}

// rtCoresLivePick возвращает первое прозрачное входящее вида mode, которое
// читает XKeen (теги force-proxy* пропускаются).
func rtCoresLivePick(ins []rtCoresLiveInbound, mode string) (rtCoresLiveInbound, bool) {
	for _, in := range ins {
		if in.mode == mode && !strings.HasPrefix(in.tag, rtCoresLiveForceProxy) {
			return in, true
		}
	}
	return rtCoresLiveInbound{}, false
}

// rtCoresLivePinned достаёт поля входящего, которые читает XKeen, записью JSON;
// withPort добавляет порт.
func rtCoresLivePinned(in map[string]any, withPort bool) map[string]string {
	paths := [][]string{
		{"tag"}, {"protocol"},
		{"settings", "followRedirect"}, {"settings", "network"},
		{"streamSettings", "sockopt", "tproxy"},
	}
	if withPort {
		paths = append(paths, []string{"port"})
	}
	out := map[string]string{}
	for _, p := range paths {
		var cur any = in
		for _, k := range p {
			m, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = m[k]
		}
		b, _ := json.Marshal(cur)
		out[strings.Join(p, ".")] = string(b)
	}
	return out
}

// rtCoresLiveNums — номера портов, найденные в строках правил XKeen.
type rtCoresLiveNums []int

// Порт из правил перехвата: REDIRECT (--to-ports) и TPROXY (--on-port).
var (
	rtCoresLiveRedirectRe = regexp.MustCompile(`--to-ports (\d+)`)
	rtCoresLiveTProxyRe   = regexp.MustCompile(`--on-port (\d+)`)
)

// rtCoresLiveRules читает порты правил перехвата XKeen: строки iptables -t nat и
// -t mangle с «xkeen» и переходом -j REDIRECT (порт --to-ports) или -j TPROXY
// (порт --on-port).
func rtCoresLiveRules() (redirect, tproxy rtCoresLiveNums, err string) {
	for _, table := range []string{"nat", "mangle"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		out, stderr, code := routertest.Exec(ctx, "iptables", "-w", "-t", table, "-S")
		cancel()
		if code != 0 {
			return nil, nil, "iptables -t " + table + ": код " + strconv.Itoa(code) + " " + strings.TrimSpace(stderr)
		}
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, "xkeen") {
				continue
			}
			if strings.Contains(line, "-j REDIRECT") {
				if m := rtCoresLiveRedirectRe.FindStringSubmatch(line); m != nil {
					n, _ := strconv.Atoi(m[1])
					redirect = append(redirect, n)
				}
			}
			if strings.Contains(line, "-j TPROXY") {
				if m := rtCoresLiveTProxyRe.FindStringSubmatch(line); m != nil {
					n, _ := strconv.Atoi(m[1])
					tproxy = append(tproxy, n)
				}
			}
		}
	}
	return redirect, tproxy, ""
}

// rtCoresLiveWaitRules ждёт правил XKeen: redirect и tproxy (0 — не проверять)
// должны быть среди портов REDIRECT и TPROXY, goneRedirect и goneTProxy (0 — не
// проверять) — отсутствовать. XKeen ставит правила уже после возврата запуска.
func rtCoresLiveWaitRules(t *testing.T, redirect, tproxy, goneRedirect, goneTProxy int) {
	t.Helper()
	deadline := time.Now().Add(rtCoresLiveRulesWait)
	var last string
	for {
		red, tpr, errText := rtCoresLiveRules()
		switch {
		case errText != "":
			last = errText
		case redirect != 0 && !slices.Contains(red, redirect):
			last = "REDIRECT --to-ports " + strconv.Itoa(redirect) + " нет среди " + rtCoresLiveJoin(red)
		case tproxy != 0 && !slices.Contains(tpr, tproxy):
			last = "TPROXY --on-port " + strconv.Itoa(tproxy) + " нет среди " + rtCoresLiveJoin(tpr)
		case goneRedirect != 0 && slices.Contains(red, goneRedirect):
			last = "REDIRECT --to-ports " + strconv.Itoa(goneRedirect) + " остался среди " + rtCoresLiveJoin(red)
		case goneTProxy != 0 && slices.Contains(tpr, goneTProxy):
			last = "TPROXY --on-port " + strconv.Itoa(goneTProxy) + " остался среди " + rtCoresLiveJoin(tpr)
		default:
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("правила XKeen за %v не стали ожидаемыми: %s", rtCoresLiveRulesWait, last)
		}
		time.Sleep(2 * time.Second)
	}
}

// rtCoresLiveJoin записывает порты через запятую.
func rtCoresLiveJoin(nums rtCoresLiveNums) string {
	if len(nums) == 0 {
		return "(пусто)"
	}
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// rtCoresLiveStatus читает `xkeen -status`: код и вывод без цветовых кодов.
func rtCoresLiveStatus() (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, config.Default().XKeenBinary, "-status")
	return code, strings.TrimSpace(rtSwitchRaceANSI.ReplaceAllString(stdout+" "+stderr, ""))
}

// rtCoresLiveWaitStatus ждёт, пока `xkeen -status` завершится кодом 0 и покажет
// запущенный клиент.
func rtCoresLiveWaitStatus(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(rtCoresLiveRulesWait)
	for {
		code, text := rtCoresLiveStatus()
		if code == 0 && services.IsKernelStatusHealthy(text) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("xkeen -status за %v: код %d, клиент не запущен (длина вывода %d)", rtCoresLiveRulesWait, code, len(text))
		}
		time.Sleep(2 * time.Second)
	}
}

// rtCoresLiveFreePort выбирает первый порт диапазона фикстур, который никто не
// слушает (TCP и UDP).
func rtCoresLiveFreePort(t *testing.T) int {
	t.Helper()
	var out string
	for _, cmd := range [][]string{{"netstat", "-lntu"}, {"ss", "-lntu"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stdout, _, code := routertest.Exec(ctx, cmd[0], cmd[1:]...)
		cancel()
		if code == 0 {
			out = stdout
			break
		}
	}
	if out == "" {
		t.Fatal("список слушающих портов недоступен ни через netstat, ни через ss")
	}
	busy := map[int]bool{}
	for _, m := range regexp.MustCompile(`:(\d{4,5})\b`).FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[1])
		busy[n] = true
	}
	for p := rtCoresLivePortFrom; p <= rtCoresLivePortTo; p++ {
		if !busy[p] {
			return p
		}
	}
	t.Fatalf("в диапазоне %d-%d нет свободного порта", rtCoresLivePortFrom, rtCoresLivePortTo)
	return 0
}

// --- ветка Xray -----------------------------------------------------------------

// rtCoresLiveXray — взятие «Входящих» Xray: что было до и что надо вернуть.
type rtCoresLiveXray struct {
	e     *rtSrc
	files []string          // файлы секции до взятия
	left  []string          // из них остающиеся на месте (с другими секциями)
	sums  map[string]string // sha256 файлов секции до взятия
	items []map[string]any  // входящие до взятия
	taken bool
}

// rtCoresLiveXrayTake берёт «Входящие» Xray через API и применяет.
func rtCoresLiveXrayTake(t *testing.T, e *rtSrc) *rtCoresLiveXray {
	t.Helper()
	view := e.rtCoresView("xray")
	if installed, _ := view["installed"].(bool); !installed {
		t.Fatal("Xray не установлен на стенде: ветка xray не проверяется")
	}
	sec := rtCoresSection(view, "inbounds")
	if sec == nil {
		t.Fatal("в виде Xray нет секции inbounds")
	}
	if blocked, _ := sec["blocked"].(string); blocked != "" {
		t.Fatalf("секцию «Входящие» Xray нельзя взять: %s; SEC-02 на этом стенде не проверить", blocked)
	}
	if owner, _ := sec["owner"].(string); owner == "panel" || owner == "pending" || owner == "drift" {
		t.Fatalf("«Входящие» Xray уже у панели (владелец %s): стенд не в исходном состоянии", owner)
	}
	x := &rtCoresLiveXray{
		e:     e,
		files: rtCoresStrings(sec["files"]),
		left:  rtCoresStrings(sec["left"]),
		sums:  map[string]string{},
		items: rtCoresLiveItems(sec["value"]),
	}
	// Стенд возвращается при любом исходе, в том числе при провале самого взятия.
	t.Cleanup(func() { x.rtCoresLiveCleanup(t) })
	if len(x.files) == 0 {
		t.Fatal("у «Входящих» Xray нет файлов-источников: брать нечего")
	}
	for _, name := range x.files {
		x.sums[name] = rtCoresFileSum(name)
		if x.sums[name] == "" {
			t.Fatalf("файл входящих (длина имени %d) не читается", len(name))
		}
	}
	if len(rtCoresLiveTransparent(x.items)) == 0 {
		t.Fatal("в «Входящих» Xray нет прозрачных входящих: SEC-02 на этом стенде не проверить")
	}

	pv := e.call(http.MethodGet, rtCoresBase+"/takeover?kernel=xray&sections=inbounds", nil)
	if pv.Status != http.StatusOK {
		t.Fatalf("GET takeover: %d %s", pv.Status, pv.Code)
	}
	ack := false
	if list, _ := pv.object(t)["sections"].([]any); len(list) == 1 {
		m, _ := list[0].(map[string]any)
		comments, _ := m["comments"].([]any)
		ack = len(comments) > 0
	}
	r := e.rtCoresPost("/take", map[string]any{"kernel": "xray", "sections": []string{"inbounds"}, "ack_comments": ack})
	if r.Status != http.StatusOK {
		t.Fatalf("take inbounds: %d %s", r.Status, r.Code)
	}
	x.taken = true
	e.rtRtApplyOK("взятие «Входящих» Xray")
	return x
}

// rtCoresLiveRelease отпускает «Входящие» и применяет; false — не удалось.
func (x *rtCoresLiveXray) rtCoresLiveRelease(t *testing.T) bool {
	t.Helper()
	c := x.e.with(t)
	r := c.rtCoresPost("/release", map[string]any{"kernel": "xray", "section": "inbounds"})
	if r.Status != http.StatusOK {
		t.Errorf("очистка: release inbounds: %d %s", r.Status, r.Code)
		return false
	}
	c.rtRtApplyOK("возврат «Входящих» Xray")
	x.taken = false
	return true
}

// rtCoresLiveCheckRestored сверяет исходные файлы секции по sha256 и отсутствие
// файла панели.
func (x *rtCoresLiveXray) rtCoresLiveCheckRestored(t *testing.T) {
	t.Helper()
	for name, sum := range x.sums {
		if got := rtCoresFileSum(name); got != sum {
			t.Errorf("после «Отпустить» файл входящих (длина имени %d) вернулся с другим содержимым или не вернулся", len(name))
		}
	}
	if rtCoresFileSum(rtCoresLiveXrayPanelFile) != "" {
		t.Errorf("после «Отпустить» на устройстве остался %s", rtCoresLiveXrayPanelFile)
	}
}

// rtCoresLiveCleanup возвращает стенд при любом исходе: отпускает секцию, если
// она взята, и сверяет файлы.
func (x *rtCoresLiveXray) rtCoresLiveCleanup(t *testing.T) {
	c := x.e.with(t)
	c.rtRstIdle()
	if !x.taken {
		return
	}
	if x.rtCoresLiveRelease(t) {
		x.rtCoresLiveCheckRestored(t)
	}
}

// rtCoresLivePanelInbounds читает входящие из файла панели на диске.
func rtCoresLivePanelInbounds(t *testing.T) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rtRtXrayDir, rtCoresLiveXrayPanelFile))
	if err != nil {
		t.Fatalf("на устройстве нет %s после «Применить»: %v", rtCoresLiveXrayPanelFile, err)
	}
	var doc struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s не JSON: %v", rtCoresLiveXrayPanelFile, err)
	}
	return doc.Inbounds
}

// rtCoresLiveXrayRules возвращает порты redirect и tproxy, которые XKeen возьмёт
// из входящих (0 — такого входящего нет).
func rtCoresLiveXrayRules(ins []rtCoresLiveInbound) (redirect, tproxy int) {
	if in, ok := rtCoresLivePick(ins, "redirect"); ok {
		redirect = in.port
	}
	if in, ok := rtCoresLivePick(ins, "tproxy"); ok {
		tproxy = in.port
	}
	return redirect, tproxy
}

// rtCoresLiveXrayInbounds — ветка Xray TestRouterCoresXKeenInbounds.
func rtCoresLiveXrayInbounds(t *testing.T, e *rtSrc) {
	t.Helper()
	rtRstKernelPID(t, "xray")
	x := rtCoresLiveXrayTake(t, e)

	// Файл панели: те же входящие с теми же полями, которые читает XKeen.
	after := rtCoresLivePanelInbounds(t)
	for _, in := range rtCoresLiveTransparent(x.items) {
		var found map[string]any
		for _, a := range after {
			if tag, _ := a["tag"].(string); tag == in.tag {
				found = a
				break
			}
		}
		if found == nil {
			t.Errorf("в файле панели нет прозрачного входящего %q", in.tag)
			continue
		}
		if want, got := rtCoresLivePinned(in.raw, true), rtCoresLivePinned(found, true); !rtCoresSumsEqual(want, got) {
			t.Errorf("входящее %q: поля XKeen в файле панели иные: было %v, стало %v", in.tag, want, got)
		}
	}
	// Исходные файлы ушли, кроме тех, где остались другие секции.
	for _, name := range x.files {
		_, statErr := os.Stat(filepath.Join(rtRtXrayDir, name))
		if slices.Contains(x.left, name) {
			if statErr != nil {
				t.Errorf("файл входящих с другими секциями (длина имени %d) пропал из configs/", len(name))
			}
		} else if statErr == nil {
			t.Errorf("файл входящих (длина имени %d) остался в configs/ после взятия", len(name))
		}
	}

	// XKeen читает порты из файла панели: правила на прежних портах, клиент работает.
	ins := rtCoresLiveTransparent(x.items)
	redirect, tproxy := rtCoresLiveXrayRules(ins)
	if redirect == 0 && tproxy == 0 {
		t.Fatal("среди прозрачных входящих нет ни redirect, ни tproxy без тега force-proxy")
	}
	rtCoresLiveWaitRules(t, redirect, tproxy, 0, 0)
	rtCoresLiveWaitStatus(t)
	rtRstKernelPID(t, "xray")

	// Возврат: файлы побайтно, правила прежние.
	if x.rtCoresLiveRelease(t) {
		x.rtCoresLiveCheckRestored(t)
		rtCoresLiveWaitRules(t, redirect, tproxy, 0, 0)
		rtCoresLiveWaitStatus(t)
	}
}

// --- ветка Mihomo ---------------------------------------------------------------

// rtCoresLiveMihomo — взятие маршрутизации Mihomo (с ней секции профиля).
type rtCoresLiveMihomo struct {
	e      *rtSrc
	tg     routertest.Target
	before rtRtDiskState
	redir  int // порты прежнего профиля (0 — не заданы)
	tproxy int
	taken  bool
}

// rtCoresLiveMihomoPorts достаёт порты прозрачного прокси из профиля так, как их
// читает XKeen: redir-port и tproxy-port, иначе порт первого listener типа redir
// или tproxy (кроме force-proxy*).
func rtCoresLiveMihomoPorts(data []byte) (redir, tproxy int) {
	var doc map[string]any
	if yaml.Unmarshal(data, &doc) != nil {
		return 0, 0
	}
	redir, tproxy = rtCoresLiveInt(doc["redir-port"]), rtCoresLiveInt(doc["tproxy-port"])
	list, _ := doc["listeners"].([]any)
	for _, it := range list {
		l, _ := it.(map[string]any)
		name, _ := l["name"].(string)
		kind, _ := l["type"].(string)
		port := rtCoresLiveInt(l["port"])
		if strings.HasPrefix(name, rtCoresLiveForceProxy) || port == 0 {
			continue
		}
		if kind == "redir" && redir == 0 {
			redir = port
		}
		if kind == "tproxy" && tproxy == 0 {
			tproxy = port
		}
	}
	return redir, tproxy
}

// rtCoresLiveMihomoActivePorts читает порты действующего config.yaml.
func rtCoresLiveMihomoActivePorts(t *testing.T) (redir, tproxy int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml Mihomo не читается: %v", err)
	}
	return rtCoresLiveMihomoPorts(data)
}

// rtCoresLiveMihomoTake берёт маршрутизацию Mihomo через API и применяет.
func rtCoresLiveMihomoTake(t *testing.T, e *rtSrc, tg routertest.Target) *rtCoresLiveMihomo {
	t.Helper()
	if installed, _ := e.rtCoresView("mihomo")["installed"].(bool); !installed {
		t.Fatal("Mihomo не установлен на стенде: ветка mihomo не проверяется")
	}
	m := &rtCoresLiveMihomo{e: e, tg: tg}
	t.Cleanup(func() { m.rtCoresLiveCleanup(t) })

	e.rtRtPurge()
	m.before = rtRtScanDisk(t)
	m.redir, m.tproxy = rtCoresLiveMihomoActivePorts(t)

	// «Мои узлы»: одна ссылка с уникальным портом; без узла маршрутизацию не взять.
	pv := e.call(http.MethodPost, rtSourcesBase+"/preview", map[string]any{"text": rtRtUniqueLink(t)})
	if pv.Status != http.StatusOK {
		t.Fatalf("sources/preview: %d %s", pv.Status, pv.Code)
	}
	token, _ := pv.object(t)["token"].(string)
	if add := e.call(http.MethodPost, rtSourcesBase+"/add", map[string]any{"revision": e.revision(), "token": token}); add.Status != http.StatusOK {
		t.Fatalf("sources/add: %d %s", add.Status, add.Code)
	}
	take := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()})
	if take.Status != http.StatusOK {
		t.Fatalf("routing/take: %d %s", take.Status, take.Code)
	}
	m.taken = true
	e.rtRtApplyOK("взятие маршрутизации Mihomo")
	return m
}

// rtCoresLiveRelease отпускает маршрутизацию и применяет; false — не удалось.
func (m *rtCoresLiveMihomo) rtCoresLiveRelease(t *testing.T) bool {
	t.Helper()
	c := m.e.with(t)
	r := c.call(http.MethodPost, rtRoutingBase+"/release", map[string]int64{"revision": c.rtRtRevision()})
	if r.Status != http.StatusOK {
		t.Errorf("очистка: routing/release: %d %s", r.Status, r.Code)
		return false
	}
	c.rtRtApplyOK("возврат маршрутизации Mihomo")
	m.taken = false
	c.rtRtExpectReleased(m.tg, m.before)
	return true
}

// rtCoresLiveCleanup возвращает стенд при любом исходе: отпускает маршрутизацию,
// если она взята, и убирает следы узлов и секций.
func (m *rtCoresLiveMihomo) rtCoresLiveCleanup(t *testing.T) {
	c := m.e.with(t)
	c.rtRstIdle()
	if m.taken && !m.rtCoresLiveRelease(t) {
		return
	}
	c.rtRtPurge()
}

// rtCoresLiveMihomoPorted проверяет профиль панели: config.yaml указывает на
// profiles/xcp-main.yaml, порты прозрачного прокси — прежние, правила XKeen —
// на них, `xkeen -status` — код 0. Возвращает порты профиля.
func (m *rtCoresLiveMihomo) rtCoresLivePorted(t *testing.T) (redir, tproxy int) {
	t.Helper()
	now := rtRtScanDisk(t)
	if !strings.HasSuffix(now.mihomoTarget, rtRtPanelProfile) {
		t.Fatalf("config.yaml Mihomo не указывает на %s", rtRtPanelProfile)
	}
	redir, tproxy = rtCoresLiveMihomoActivePorts(t)
	if m.redir != 0 && redir != m.redir {
		t.Errorf("redir-порт профиля панели %d, у прежнего профиля был %d", redir, m.redir)
	}
	if m.tproxy != 0 && tproxy != m.tproxy {
		t.Errorf("tproxy-порт профиля панели %d, у прежнего профиля был %d", tproxy, m.tproxy)
	}
	if redir == 0 && tproxy == 0 {
		t.Fatal("в профиле панели нет портов redir и tproxy")
	}
	return redir, tproxy
}

// rtCoresLiveMihomoProfile — ветка Mihomo TestRouterCoresXKeenInbounds.
func rtCoresLiveMihomoProfile(t *testing.T, e *rtSrc, tg routertest.Target) {
	t.Helper()
	rtRstKernelPID(t, "mihomo")
	m := rtCoresLiveMihomoTake(t, e, tg)
	redir, tproxy := m.rtCoresLivePorted(t)
	rtCoresLiveWaitRules(t, redir, tproxy, 0, 0)
	rtCoresLiveWaitStatus(t)
	rtRstKernelPID(t, "mihomo")
	m.rtCoresLiveRelease(t)
}

// Трасса: «Входящие» Xray берутся через API и «Применить» (Mihomo — маршрутизация
// вместе с секциями профиля) → поля входящих, порты профиля и правила
// перехвата XKeen на тех же портах, `xkeen -status` с кодом 0 → «Отпустить» и
// «Применить» → исходные файлы побайтно. Метка ядра: ветка выбирается по
// активному ядру цели.
func TestRouterCoresXKeenInbounds(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	switch tg.Core {
	case "xray":
		rtCoresLiveXrayInbounds(t, e)
	case "mihomo":
		rtCoresLiveMihomoProfile(t, e, tg)
	default:
		t.Fatalf("неизвестное ядро цели %q", tg.Core)
	}
}
