//go:build router

package xtables

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// routerIptablesBinary — iptables устройства: Entware, иначе из PATH.
func routerIptablesBinary() string {
	if _, err := os.Stat("/opt/sbin/iptables"); err == nil {
		return "/opt/sbin/iptables"
	}
	return "iptables"
}

// TestRouterWaitArgsAccepted проверяет, что аргументы ожидания блокировки, которые
// выбрала панель, принимает настоящий iptables устройства. Ожидаемый список
// аргументов в тесте не зашит: вердикт — код выхода iptables (старые сборки
// iptables 1.4.x не принимают «-w 5»).
func TestRouterWaitArgsAccepted(t *testing.T) {
	routertest.Current(t)
	bin := routerIptablesBinary()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	waitArgs := WaitArgsFor(ctx, bin)
	args := append(append([]string{}, waitArgs...), "-t", "mangle", "-S")
	stdout, stderr, code := routertest.Exec(ctx, bin, args...)
	t.Logf("выбранные аргументы ожидания: %q; код выхода iptables: %d; строк правил: %d",
		strings.Join(waitArgs, " "), code, strings.Count(stdout, "\n"))
	routertest.Verdict(t, code == 0, routertest.KnownMark{},
		"iptables %s отклонил аргументы %q: код %d, %s",
		bin, strings.Join(args, " "), code, strings.TrimSpace(stderr))
}
