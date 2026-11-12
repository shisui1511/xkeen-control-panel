//go:build router

package routertest

// Срок, который действует у живого процесса панели. Панель читает переменные
// XCP_RT_* один раз при старте (internal/rtenv), поэтому тест берёт значение из
// окружения её процесса и разбирает тем же rtenv.Var.Parse: тест и панель видят
// один срок при любом наборе, теге и наличии блока xcp-rt в xcp.env, а копий
// сроков в тестах нет.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/rtenv"
)

// Deadline — срок v у работающей панели: значение переменной из
// /proc/<pid панели>/environ, разобранное v.Parse; переменной нет — v.Default.
// Не ровно один процесс панели или нечитаемое окружение — провал теста.
func Deadline(t testing.TB, v rtenv.Var) time.Duration {
	t.Helper()
	d, note, err := panelDeadline(v)
	if err != nil {
		t.Fatalf("срок %s: %v", v.Name, err)
	}
	if note == "" {
		note = "переменной нет, значение по умолчанию"
	}
	t.Logf("срок %s у панели: %v (%s)", v.Name, d, note)
	return d
}

// panelDeadline — Deadline без testing: для t.Cleanup, где ошибка идёт в t.Errorf.
// Возвращает срок, заметку разбора и ошибку наблюдения.
func panelDeadline(v rtenv.Var) (time.Duration, string, error) {
	pids, out := PanelPIDs()
	if len(pids) != 1 {
		return 0, "", fmt.Errorf("процессов панели %d, ожидался один (pidof: %s)", len(pids), out)
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pids[0]) + "/environ")
	if err != nil {
		return 0, "", fmt.Errorf("окружение процесса панели не читается: %w", err)
	}
	val := ""
	for _, kv := range strings.Split(string(raw), "\x00") {
		if s, ok := strings.CutPrefix(kv, v.Name+"="); ok {
			val = s
			break
		}
	}
	d, note := v.Parse(val)
	return d, note, nil
}
