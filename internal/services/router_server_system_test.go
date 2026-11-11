//go:build router

// router-area: server

package services

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Тесты панели как сервера на устройстве: сессия от ПК и разбор вывода ndmc (модель, прошивка,
// клиенты hotspot). Вынесены из router_system_test.go (область kernel): правка файлов сервера
// (internal/handlers/system.go, services/device_info.go, clients.go) выбирает их по области server.

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
