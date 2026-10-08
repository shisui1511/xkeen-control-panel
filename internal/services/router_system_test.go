//go:build router

package services

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Системные тесты устройства. Вердикт в каждом — поведение самой системы (ndmc,
// процессы, ядра), а не эталонный файл. Файл не использует помощников из обычных
// _test.go пакета: те удаляются отдельно от роутерных тестов.

const (
	rtXKeenBinary = "/opt/sbin/xkeen"
	rtXrayBinary  = "/opt/sbin/xray"
	rtMihomoBin   = "/opt/sbin/mihomo"
	rtXrayConfDir = "/opt/etc/xray/configs"
	rtMihomoDir   = "/opt/etc/mihomo"
)

var (
	rtModelRe  = regexp.MustCompile(`(?mi)^\s*model:\s*(.+)$`)
	rtTitleRe  = regexp.MustCompile(`(?mi)^\s*title:\s*(\S+)`)
	rtVendorRe = regexp.MustCompile(`(?mi)^\s*vendor:\s*(\S+)`)
	rtIPLineRe = regexp.MustCompile(`(?m)^\s*ip:\s*(\S+)`)
	rtAlnumRe  = regexp.MustCompile(`[^a-z0-9]+`)
)

// rtNdmc выполняет `ndmc -c "<команда>"` и возвращает его вывод.
func rtNdmc(t *testing.T, command string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, "ndmc", "-c", command)
	if code != 0 {
		t.Fatalf("ndmc -c %q: код %d: %s", command, code, strings.TrimSpace(stderr))
	}
	return stdout
}

// rtPids возвращает PID процессов с этим именем (пусто — процесса нет).
func rtPids(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, _, _ := routertest.Exec(ctx, "pidof", name)
	return strings.TrimSpace(stdout)
}

// rtWaitRunning ждёт, пока процесс появится (want=true) или исчезнет (want=false),
// и возвращает PID на момент выхода.
func rtWaitRunning(name string, want bool, limit time.Duration) string {
	deadline := time.Now().Add(limit)
	for {
		pids := rtPids(name)
		if (pids != "") == want || time.Now().After(deadline) {
			return pids
		}
		time.Sleep(time.Second)
	}
}

// rtWaitChanged ждёт, пока у процесса появится PID, отличный от old (перезапуск
// XKeen возвращает управление раньше, чем ядро поднялось заново), и возвращает PID.
func rtWaitChanged(name, old string, limit time.Duration) string {
	deadline := time.Now().Add(limit)
	for {
		pids := rtPids(name)
		if (pids != "" && pids != old) || time.Now().After(deadline) {
			return pids
		}
		time.Sleep(time.Second)
	}
}

// rtWaitHealthy опрашивает `xkeen -status` через панель, пока её оценка не станет
// want (запуск XKeen возвращает управление раньше, чем статус перестаёт быть «не запущен»).
// Возвращает последнюю оценку и текст статуса.
func rtWaitHealthy(svc *XKeenService, want bool, limit time.Duration) (bool, string) {
	deadline := time.Now().Add(limit)
	for {
		// у остановленного XKeen `xkeen -status` завершается с кодом 1: вердикт по тексту
		st, _ := svc.StatusWithTimeout(30 * time.Second)
		healthy := IsKernelStatusHealthy(st)
		if healthy == want || time.Now().After(deadline) {
			return healthy, st
		}
		time.Sleep(2 * time.Second)
	}
}

// rtShort обрезает вывод для сообщения о падении.
func rtShort(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

// TestRouterPanelSession: сессия, созданная на ПК, действует для панели на этом же
// устройстве (проверка механизма, на котором стоят остальные тесты панели).
func TestRouterPanelSession(t *testing.T) {
	p := routertest.NewPanel(t)
	var me map[string]any
	status, err := p.GetJSON("/api/auth/me", &me)
	routertest.Verdict(t, err == nil && status == 200, routertest.KnownMark{},
		"GET /api/auth/me с сессией от ПК: статус %d, ошибка %v", status, err)
}

// TestRouterNdmcDeviceInfo: модель и версия прошивки, которые панель получает от
// ndmc, совпадают с сырым выводом ndmc на этом же устройстве. Модель в лог не пишется.
func TestRouterNdmcDeviceInfo(t *testing.T) {
	tg := routertest.Current(t)
	raw := rtNdmc(t, "show version")
	model, osName, osVersion := NewDeviceInfo().Get()
	t.Logf("архитектура %s", tg.Arch)

	mm := rtModelRe.FindStringSubmatch(raw)
	tm := rtTitleRe.FindStringSubmatch(raw)
	if mm == nil || tm == nil {
		t.Fatal("в выводе ndmc show version нет model или title")
	}
	// Панель очищает модель от пробелов и скобок: сравниваются только буквы и цифры.
	norm := func(s string) string { return rtAlnumRe.ReplaceAllString(strings.ToLower(s), "") }
	routertest.Verdict(t, norm(model) == norm(mm[1]), routertest.KnownMark{},
		"модель в панели не совпадает с моделью из ndmc (по буквам и цифрам)")
	routertest.Verdict(t, osVersion == tm[1], routertest.KnownMark{},
		"версия прошивки в панели %q не равна title из ndmc %q", osVersion, tm[1])
	if vm := rtVendorRe.FindStringSubmatch(raw); vm != nil {
		routertest.Verdict(t, strings.HasPrefix(osName, vm[1]), routertest.KnownMark{},
			"название ОС в панели %q не начинается с vendor из ndmc %q", osName, vm[1])
	}
}

// TestRouterNdmcHotspot: разбор `show ip hotspot` из clients.go отдаёт список без
// ошибки, и число записей равно числу различных адресов хостов в выводе ndmc
// (хосты без адреса, 0.0.0.0, в список клиентов не входят).
func TestRouterNdmcHotspot(t *testing.T) {
	routertest.Current(t)
	raw := rtNdmc(t, "show ip hotspot")
	addrs := map[string]bool{}
	for _, m := range rtIPLineRe.FindAllStringSubmatch(raw, -1) {
		if m[1] != "0.0.0.0" {
			addrs[m[1]] = true
		}
	}

	clients, err := NewClientResolver().fetchFromNdmc()
	routertest.Verdict(t, err == nil, routertest.KnownMark{}, "разбор show ip hotspot вернул ошибку: %v", err)
	t.Logf("адресов хостов в выводе ndmc: %d, записей в панели: %d", len(addrs), len(clients))
	routertest.Verdict(t, len(clients) == len(addrs), routertest.KnownMark{},
		"панель вернула записей %d, а в выводе ndmc хостов с адресом %d", len(clients), len(addrs))
}

// TestRouterXKeenLifecycle: настоящий XKeen останавливает и запускает ядро, а оценка
// состояния панелью совпадает с наличием процесса. Тест разрушающий (стенд, D-04):
// исходное состояние возвращается в Cleanup, а прогон в целом — восстановлением по снимку.
func TestRouterXKeenLifecycle(t *testing.T) {
	tg := routertest.Current(t)
	core := tg.Core
	if core != "xray" && core != "mihomo" {
		t.Fatalf("активное ядро цели не определено (%q)", core)
	}
	if rtPids(core) == "" {
		t.Fatalf("перед тестом ядро %s не запущено: стенд должен быть в рабочем состоянии", core)
	}
	svc := NewXKeenService(rtXKeenBinary, tg.WorkDir)
	t.Cleanup(func() {
		if rtPids(core) == "" {
			_, _ = svc.Start()
			rtWaitRunning(core, true, 3*time.Minute)
		}
	})
	limit := 3 * time.Minute

	t.Run("stop", func(t *testing.T) {
		out, err := svc.Stop()
		routertest.Verdict(t, err == nil, routertest.KnownMark{}, "Stop: %v: %s", err, rtShort(out))
		pids := rtWaitRunning(core, false, limit)
		routertest.Verdict(t, pids == "", routertest.KnownMark{}, "после Stop процесс %s остался (PID %s)", core, pids)
		healthy, st := rtWaitHealthy(svc, false, limit)
		routertest.Verdict(t, !healthy, routertest.KnownMark{},
			"после Stop панель считает XKeen работающим: %s", rtShort(st))
	})

	t.Run("start", func(t *testing.T) {
		out, err := svc.Start()
		routertest.Verdict(t, err == nil, routertest.KnownMark{}, "Start: %v: %s", err, rtShort(out))
		pids := rtWaitRunning(core, true, limit)
		routertest.Verdict(t, pids != "", routertest.KnownMark{}, "после Start процесса %s нет", core)
		healthy, st := rtWaitHealthy(svc, true, limit)
		routertest.Verdict(t, healthy, routertest.KnownMark{},
			"после Start панель считает XKeen неработающим: %s", rtShort(st))
	})

	t.Run("restart", func(t *testing.T) {
		before := rtPids(core)
		out, err := svc.Restart()
		routertest.Verdict(t, err == nil, routertest.KnownMark{}, "Restart: %v: %s", err, rtShort(out))
		after := rtWaitChanged(core, before, limit)
		routertest.Verdict(t, after != "" && after != before, routertest.KnownMark{},
			"после Restart PID ядра %s не сменился (было %q, стало %q)", core, before, after)
	})
}

// TestRouterActiveConfigAccepted: активный конфиг принимает само ядро.
func TestRouterActiveConfigAccepted(t *testing.T) {
	tg := routertest.Current(t)
	switch tg.Core {
	case "xray":
		if _, err := os.Stat(rtXrayBinary); err != nil {
			t.Fatalf("бинарник Xray не найден: %v", err)
		}
		// ValidateXrayConfigDir запускает `xray -test -confdir` — вердикт даёт ядро.
		ok, out := ValidateXrayConfigDir(rtXrayConfDir)
		routertest.Verdict(t, ok, routertest.KnownMark{}, "Xray не принял каталог конфигов: %s", rtShort(out))
		res, err := NewXKeenService(rtXKeenBinary, tg.WorkDir).ValidateXrayConfig(rtXrayConfDir)
		routertest.Verdict(t, err == nil, routertest.KnownMark{}, "проверка конфигов Xray панелью: %v", err)
		t.Logf("предупреждений проверки панели: %d", len(res.Warnings))
	case "mihomo":
		if _, err := os.Stat(rtMihomoBin); err != nil {
			t.Fatalf("бинарник Mihomo не найден: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_, stderr, code := routertest.Exec(ctx, rtMihomoBin, "-t", "-d", rtMihomoDir, "-f", rtMihomoDir+"/config.yaml")
		routertest.Verdict(t, code == 0, routertest.KnownMark{}, "Mihomo не принял config.yaml (код %d): %s", code, rtShort(stderr))
		res, err := NewMihomoService(rtMihomoBin, rtXKeenBinary, rtMihomoDir).ValidateMihomoConfig()
		routertest.Verdict(t, err == nil, routertest.KnownMark{}, "проверка config.yaml панелью: %v", err)
		t.Logf("ошибок проверки панели: %d, предупреждений: %d", len(res.Errors), len(res.Warnings))
	default:
		t.Fatalf("активное ядро цели не определено (%q)", tg.Core)
	}
}

// TestRouterKernelVersions: версии ядер, которые определяет панель, совпадают с
// выводом самих ядер на устройстве.
func TestRouterKernelVersions(t *testing.T) {
	tg := routertest.Current(t)
	ks := NewKernelService(tg.WorkDir)
	cases := []struct {
		name string
		bin  string
		args []string
	}{
		{"xray", rtXrayBinary, []string{"version"}},
		{"mihomo", rtMihomoBin, []string{"-v"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := os.Stat(c.bin); err != nil {
				t.Skipf("ядро %s не установлено", c.name)
			}
			info := ks.Get(c.name)
			if info == nil {
				t.Fatalf("панель не знает ядро %s", c.name)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			stdout, stderr, code := routertest.Exec(ctx, c.bin, c.args...)
			if code != 0 {
				// Ядро не запускается само по себе: версию определить нечем. Панель
				// при этом не должна показывать настоящую версию.
				routertest.Verdict(t, info.CurrentVersion == "error", routertest.KnownMark{},
					"бинарник %s не запускается (код %d), а панель показывает версию %q", c.name, code, info.CurrentVersion)
				t.Skipf("бинарник ядра %s не запускается на этом устройстве (код %d): %s", c.name, code, rtShort(stderr))
			}
			valid := info.CurrentVersion != "" && info.CurrentVersion != "error" &&
				info.CurrentVersion != "unknown" && info.CurrentVersion != "not installed"
			routertest.Verdict(t, valid && strings.Contains(stdout, info.CurrentVersion), routertest.KnownMark{},
				"версия ядра %s в панели %q не найдена в выводе ядра", c.name, info.CurrentVersion)
		})
	}
}
