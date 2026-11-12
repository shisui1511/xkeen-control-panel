//go:build router

package routertest

// Помощники управления настоящей панелью устройства: поиск её процесса, запуск
// init-скрипта и ожидание новой панели после перезапуска. Их зовут тесты пакета
// handlers (перезапуск панели, DNS, geodata) и помощники этого пакета, которым
// нужна остановка и запуск панели.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// PanelInitScript — init-скрипт панели (S99xcp из scripts/setup.sh).
	PanelInitScript = "/opt/etc/init.d/S99xcp"
	// PanelShellMark — первая строка обёртки команды: оболочка запущена.
	PanelShellMark = "xcp-rt-shell-started"
)

// PanelPIDs — PID процессов панели: только запуски с -config (init-скрипт и
// самообновление), без «xcp -v» и других коротких вызовов бинарника.
func PanelPIDs() ([]int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, code := Exec(ctx, "pidof", "xcp")
	out := strings.TrimSpace(stdout + " " + stderr)
	if code != 0 {
		return nil, out
	}
	var pids []int
	for _, f := range strings.Fields(stdout) {
		n, err := strconv.Atoi(f)
		if err != nil || n <= 0 {
			continue
		}
		raw, err := os.ReadFile("/proc/" + f + "/cmdline")
		if err != nil {
			continue
		}
		for _, a := range strings.Split(string(raw), "\x00") {
			if a == "-config" {
				pids = append(pids, n)
				break
			}
		}
	}
	return pids, out
}

// panelANSI — цветовые коды вывода init-скриптов Entware.
var panelANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// PanelInit запускает init-скрипт панели с действием action и чистым окружением:
// новая панель не должна унаследовать переменные теста (адрес подписки, сессия,
// каталоги). Вывод скрипта по мере поступления пишется и в extra (nil — некуда):
// по нему видно, когда оболочка запустилась (PanelShellMark) и когда скрипт дошёл
// до своих шагов. Возвращает вывод одной строкой без цветовых кодов и метки
// оболочки и код выхода (-1 — скрипт не запустился или убит по контексту).
func PanelInit(ctx context.Context, action string, extra io.Writer) (string, int) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "echo "+PanelShellMark+
		"; exec env -i PATH=/opt/sbin:/opt/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root "+PanelInitScript+" "+action)
	cmd.Stdout = &out
	if extra != nil {
		cmd.Stdout = io.MultiWriter(&out, extra)
	}
	cmd.Stderr = &errOut
	code := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		code = -1
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if errOut.Len() == 0 {
			errOut.WriteString(err.Error())
		}
	}
	text := strings.ReplaceAll(out.String()+" "+errOut.String(), PanelShellMark, "")
	return strings.Join(strings.Fields(panelANSI.ReplaceAllString(text, "")), " "), code
}

// PanelWaitUp ждёт панель с PID, отличным от old, которая отвечает на
// /api/auth/me по сессии p. Возвращает PID или 0 и последнее наблюдение.
func PanelWaitUp(p *Panel, old int, limit time.Duration) (int, string) {
	deadline := time.Now().Add(limit)
	last := ""
	for time.Now().Before(deadline) {
		pids, out := PanelPIDs()
		switch {
		case len(pids) != 1:
			last = "процессов панели " + strconv.Itoa(len(pids)) + " (pidof: " + out + ")"
		case pids[0] == old:
			last = "работает старый процесс " + strconv.Itoa(old)
		default:
			code, err := p.GetJSON("/api/auth/me", nil)
			if err == nil && code == http.StatusOK {
				return pids[0], ""
			}
			last = "новый процесс " + strconv.Itoa(pids[0]) + ", /api/auth/me: http " + strconv.Itoa(code)
			if err != nil {
				last += " (" + err.Error() + ")"
			}
		}
		time.Sleep(time.Second)
	}
	return 0, last
}
