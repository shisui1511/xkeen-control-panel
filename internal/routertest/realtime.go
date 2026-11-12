//go:build router

package routertest

// Сценарий «реальное время» (D-21): на каждую переменную XCP_RT_* один
// существующий тест яруса full идёт на настоящем сроке панели. Обвязка стенда на
// время прогона пишет сокращённые сроки блоком строк в xcp.env панели
// (scripts/router/lib.sh); RealTime убирает из блока строки своих переменных,
// перезапускает панель и в t.Cleanup возвращает блок как был.

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/rtenv"
)

const (
	// rtEnvFile — файл окружения панели, который подключает init-скрипт S99xcp.
	rtEnvFile = "/opt/etc/xcp/xcp.env"
	// rtEnvBegin и rtEnvEnd — маркеры блока сокращённых сроков. Те же строки —
	// RT_RTENV_BEGIN и RT_RTENV_END в scripts/router/lib.sh.
	rtEnvBegin = "# xcp-rt begin"
	rtEnvEnd   = "# xcp-rt end"

	// rtInitLimit — предел init-скрипта при перезапуске панели.
	rtInitLimit = 75 * time.Second
	// rtUpLimit — сколько ждать ответа новой панели.
	rtUpLimit = 90 * time.Second
)

// RealTime переводит тест на настоящие сроки vars: если в блоке xcp-rt файла
// окружения панели есть строки этих переменных, убирает их (маркеры и остальные
// строки остаются), перезапускает панель init-скриптом и проверяет, что Deadline
// каждой переменной равен умолчанию; в t.Cleanup возвращает прежнее содержимое
// файла, снова перезапускает панель и сверяет сроки с прежними значениями.
// Без файла, без блока (смоук, ручной запуск) или без строк своих переменных в
// блоке ничего не делает и панель не перезапускает. Вызывать прямо в теле
// TestRouter* яруса full, первой строкой после меток яруса и ядра: уборка
// RealTime срабатывает последней, после уборки самого теста.
func RealTime(t *testing.T, vars ...rtenv.Var) {
	t.Helper()
	if len(vars) == 0 {
		t.Fatalf("RealTime без переменных: нужен хотя бы один срок rtenv")
	}
	names := make([]string, 0, len(vars))
	for _, v := range vars {
		names = append(names, v.Name)
	}
	orig, err := os.ReadFile(rtEnvFile)
	if os.IsNotExist(err) {
		t.Logf("сроки настоящие: блока xcp-rt нет (%s нет)", rtEnvFile)
		return
	}
	if err != nil {
		t.Fatalf("файл окружения панели не читается: %v", err)
	}
	stripped, prev, hasBlock := rtEnvStrip(string(orig), names)
	if !hasBlock {
		t.Logf("сроки настоящие: блока xcp-rt нет")
		return
	}
	if len(prev) == 0 {
		t.Logf("сроки настоящие: %s в блоке нет", strings.Join(names, ", "))
		return
	}

	pids, out := PanelPIDs()
	if len(pids) != 1 {
		t.Fatalf("реальное время: процессов панели %d, ожидался один (pidof: %s)", len(pids), out)
	}
	p := NewPanel(t)
	t.Cleanup(func() {
		if err := os.WriteFile(rtEnvFile, orig, 0o600); err != nil {
			t.Errorf("реальное время: прежний %s не записан: %v", rtEnvFile, err)
			return
		}
		old := 0
		if pids, _ := PanelPIDs(); len(pids) == 1 {
			old = pids[0]
		}
		if err := rtEnvRestart(p, old); err != nil {
			t.Errorf("реальное время: возврат сокращённых сроков: %v", err)
			return
		}
		for _, v := range vars {
			want, _ := v.Parse(prev[v.Name])
			got, _, err := panelDeadline(v)
			switch {
			case err != nil:
				t.Errorf("реальное время: срок %s после возврата блока: %v", v.Name, err)
			case got != want:
				t.Errorf("реальное время: срок %s после возврата блока %v, ожидалось %v", v.Name, got, want)
			}
		}
	})

	if err := os.WriteFile(rtEnvFile, []byte(stripped), 0o600); err != nil {
		t.Fatalf("реальное время: %s не переписан: %v", rtEnvFile, err)
	}
	if err := rtEnvRestart(p, pids[0]); err != nil {
		t.Fatalf("реальное время: %v", err)
	}
	for _, v := range vars {
		if d := Deadline(t, v); d != v.Default {
			t.Fatalf("реальное время: срок %s у панели %v, ожидалось умолчание %v", v.Name, d, v.Default)
		}
	}
	t.Logf("сроки настоящие: %s убраны из блока xcp-rt на время теста", strings.Join(names, ", "))
}

// rtEnvStrip убирает из блока xcp-rt строки NAME=… переменных names. Возвращает
// новое содержимое, значения убранных строк по имени и признак блока. Блок без
// закрывающего маркера кончается на первой строке не вида XCP_RT_<ИМЯ>=…
// (та же граница у awk в lib.sh: cur и body), строка остаётся в файле.
func rtEnvStrip(content string, names []string) (string, map[string]string, bool) {
	var b strings.Builder
	prev := map[string]string{}
	in, hasBlock := false, false
	for _, line := range strings.SplitAfter(content, "\n") {
		s := strings.TrimRight(line, "\r\n")
		switch {
		case !in && s == rtEnvBegin:
			in, hasBlock = true, true
		case in && s == rtEnvEnd:
			in = false
		case in && !rtEnvLine(s):
			// незакрытый блок кончился: строка не из блока, остаётся как есть
			in = false
		case in:
			key, val, ok := strings.Cut(strings.TrimSpace(s), "=")
			if ok && slices.Contains(names, key) {
				prev[key] = strings.Trim(val, `"'`)
				continue
			}
		}
		b.WriteString(line)
	}
	return b.String(), prev, hasBlock
}

// rtEnvLine — строка вида XCP_RT_<ИМЯ>=…, как /^XCP_RT_[A-Z0-9_]*=/ у awk в lib.sh.
func rtEnvLine(s string) bool {
	name, _, ok := strings.Cut(s, "=")
	if !ok || !strings.HasPrefix(name, "XCP_RT_") {
		return false
	}
	for _, r := range name[len("XCP_RT_"):] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// rtEnvRestart перезапускает панель init-скриптом (предел rtInitLimit) и ждёт
// новый процесс, отвечающий по сессии p (предел rtUpLimit).
func rtEnvRestart(p *Panel, old int) error {
	ctx, cancel := context.WithTimeout(context.Background(), rtInitLimit)
	defer cancel()
	out, code := PanelInit(ctx, "restart", nil)
	if pid, last := PanelWaitUp(p, old, rtUpLimit); pid == 0 {
		return fmt.Errorf("панель не поднялась за %v после перезапуска: %s; init-скрипт (код %d): %q", rtUpLimit, last, code, out)
	}
	return nil
}
