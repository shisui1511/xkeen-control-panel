//go:build router

package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Точечное обновление провайдера узлов при взятой маршрутизации (D-28) на настоящей
// панели устройства. Профиль панели подключает proxy_providers/xcp-<источник>.yaml
// через use:, поэтому новый узел «Моих узлов» меняет только файл провайдера, и
// конвейер обязан выбрать точечный PUT /providers/proxies/xcp-my со сверкой Expect
// вместо полной перезагрузки профиля. Вердикт — итог «Применить» (строка перезапуска
// Mihomo), PID процесса Mihomo в /proc, число узлов провайдера в Clash API через
// панель и файл провайдера на диске. В сообщения попадают только коды и числа.

const (
	// rtProvCountWait — сколько ждать нужного числа узлов провайдера в Clash API.
	rtProvCountWait = 30 * time.Second
	// rtProvProviderFile — путь файла провайдера «Моих узлов» в каталоге Mihomo.
	rtProvProviderFile = "proxy_providers/xcp-my.yaml"
)

// rtProvLink берёт ссылку фикстуры и подставляет порт base+shift: так узлы одного
// сценария получают разные ключи и не схлопываются как дубли.
func rtProvLink(t *testing.T, base, shift int) string {
	t.Helper()
	link := rtLinkText(t, "vless_ws")
	re := regexp.MustCompile(`(@[^:/?#]+):\d+`)
	if !re.MatchString(link) {
		t.Fatal("в ссылке фикстуры нет порта")
	}
	return re.ReplaceAllString(link, "${1}:"+strconv.Itoa(base+shift))
}

// rtProvApply запускает «Применить», ждёт итог и требует успех; возвращает apply.
func (e *rtSrc) rtProvApply(label string) map[string]any {
	e.t.Helper()
	before := e.rtRstStartApply()
	a := e.rtRstWaitApply(before)
	if ok, code := rtRstResult(a); !ok {
		e.t.Fatalf("«Применить» (%s): код %q: %s", label, code, rtRstDescribe(a))
	}
	return a
}

// rtProvRestartNote — note_code строки перезапуска ядра из apply.restart.
func rtProvRestartNote(a map[string]any, kernel string) string {
	list, _ := a["restart"].([]any)
	for _, it := range list {
		m, _ := it.(map[string]any)
		if m["kernel"] == kernel {
			s, _ := m["note_code"].(string)
			return s
		}
	}
	return ""
}

// rtProvClashCount опрашивает Clash API (через панель) по провайдеру xcp-my, пока
// число узлов не станет want или не истечёт rtProvCountWait; возвращает последнее
// прочитанное число (-1 — провайдер не отвечал).
func (e *rtSrc) rtProvClashCount(want int) int {
	e.t.Helper()
	got := -1
	deadline := time.Now().Add(rtProvCountWait)
	for {
		r := e.call(http.MethodGet, "/api/mihomo/proxy/providers/proxies/xcp-my", nil)
		if r.Status == http.StatusOK {
			var body struct {
				Proxies []any `json:"proxies"`
			}
			if json.Unmarshal(r.Raw, &body) == nil && body.Proxies != nil {
				got = len(body.Proxies)
			}
		}
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(2 * time.Second)
	}
}

// rtProvDiskCount — число узлов в файле провайдера «Моих узлов» на диске (-1 — файла
// нет или он не разбирается).
func rtProvDiskCount(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rtRtMihomoDir, rtProvProviderFile))
	if err != nil {
		return -1
	}
	var doc struct {
		Proxies []any `yaml:"proxies"`
	}
	if yaml.Unmarshal(data, &doc) != nil {
		return -1
	}
	return len(doc.Proxies)
}

// Трасса: «Мои узлы» из двух ссылок → take → «Применить» → третий узел →
// «Применить». Активный Mihomo: итог второго применения — hot_reloaded с note_code
// providers_updated (точечная ветка, не PUT /configs), PID Mihomo прежний, в Clash
// API три узла. Активный Xray: Mihomo не перезагружается, файл провайдера на диске
// обновлён. Метка ядра: тест читает активное ядро и меняет его маршрутизацию.
func TestRouterRoutingProviderTargeted(t *testing.T) {
	tg := routertest.CoreSpecific(t)
	e := rtNewSrc(t)
	e.enableLayer()
	e.rtRstIdle()
	e.rtRtPurge()
	taken := false

	// Маршрутизация и следы сценария убираются при любом исходе: release →
	// «Применить», затем очистка узлов и секций. Cleanup регистрируется после
	// enableLayer и потому идёт раньше сброса черновика и возврата флага.
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

	base := 20000 + int(time.Now().UnixNano()%30000)
	e.rtRstAddMyNode(rtProvLink(t, base, 0))
	e.rtRstAddMyNode(rtProvLink(t, base, 1))

	if tk := e.call(http.MethodPost, rtRoutingBase+"/take", map[string]int64{"revision": e.rtRtRevision()}); tk.Status != http.StatusOK {
		t.Fatalf("take: %d %s", tk.Status, tk.Code)
	}
	taken = true
	e.rtProvApply("взятие")
	if desired, got := e.rtRtOwnership(tg.Core); !desired || !got {
		t.Fatalf("снимок слоя: желаемое=%v, взято %s=%v", desired, tg.Core, got)
	}

	switch tg.Core {
	case "mihomo":
		if got := e.rtProvClashCount(2); got != 2 {
			t.Fatalf("Mihomo: в провайдере xcp-my после взятия узлов %d, ожидалось 2", got)
		}
		pid := rtRstKernelPID(t, "mihomo")
		e.rtRstAddMyNode(rtProvLink(t, base, 2))
		a := e.rtProvApply("третий узел")
		if out := rtRstRestartOutcome(a, "mihomo"); out != "hot_reloaded" {
			t.Errorf("Mihomo: исход перезапуска %q, ожидался hot_reloaded", out)
		}
		if note := rtProvRestartNote(a, "mihomo"); note != "providers_updated" {
			t.Errorf("Mihomo: note_code %q, ожидался providers_updated (точечная ветка)", note)
		}
		if now := rtRstKernelPID(t, "mihomo"); now != pid {
			t.Errorf("Mihomo: PID сменился при точечном обновлении (%d → %d)", pid, now)
		}
		if got := e.rtProvClashCount(3); got != 3 {
			t.Errorf("Mihomo: в провайдере xcp-my узлов %d, ожидалось 3", got)
		}
		if got := rtProvDiskCount(t); got != 3 {
			t.Errorf("Mihomo: в файле провайдера на диске узлов %d, ожидалось 3", got)
		}
		rtRstHoldPID(t, "mihomo", pid)
	case "xray":
		e.rtRstAddMyNode(rtProvLink(t, base, 2))
		a := e.rtProvApply("третий узел")
		if out := rtRstRestartOutcome(a, "mihomo"); out == "hot_reloaded" || out == "restarted" || out == "restarted_after_reload_failed" {
			t.Errorf("Xray активно: Mihomo не должен перезагружаться, исход %q", out)
		}
		if got := rtProvDiskCount(t); got != 3 {
			t.Errorf("Xray активно: в файле провайдера Mihomo на диске узлов %d, ожидалось 3", got)
		}
	default:
		t.Fatalf("неизвестное ядро цели %q", tg.Core)
	}
}
