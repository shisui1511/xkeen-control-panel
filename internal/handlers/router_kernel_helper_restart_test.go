//go:build router

package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Перезапуск ядра через панель, пока панель непрерывно проверяет конфигурацию
// ядра (`xray -test` / `mihomo -t` через POST /api/config/validate). XKeen считает
// ядро запущенным по `pidof <ядро>`: процесс проверки, живой в момент, когда
// init-скрипт после остановки ядра решает, запускать ли его, принимался за ядро, и
// `xkeen -restart` выходил, не запустив ядро (ядро лежало до ручного запуска, а
// подтверждение перезапуска слоя «Конфигурация» падало через 90 с). Вердикт —
// процесс ядра в /proc (pidof) независимо от панели.

const (
	// rtHelperRestartWait — сколько ждать нового процесса ядра после перезапуска:
	// столько же даёт подтверждение перезапуска на панели.
	rtHelperRestartWait = 90 * time.Second
	// rtHelperLead — сколько проверки идут до первого перезапуска.
	rtHelperLead = 2 * time.Second
	// rtHelperValidators — параллельных потоков проверки: процесс проверки жив почти
	// всё время, как при частых опросах панели на слабом устройстве.
	rtHelperValidators = 2
	// rtHelperRounds — перезапусков подряд.
	rtHelperRounds = 3
)

// rtHelperValidateTarget — файл конфигурации активного ядра для проверки и его содержимое.
func rtHelperValidateTarget(t *testing.T, kernel string) (path, content string) {
	t.Helper()
	switch kernel {
	case "xray":
		files, _ := filepath.Glob("/opt/etc/xray/configs/*.json")
		sort.Strings(files)
		for _, f := range files {
			if raw, err := os.ReadFile(f); err == nil && len(raw) > 0 {
				return f, string(raw)
			}
		}
		t.Fatal("в /opt/etc/xray/configs нет непустого файла конфигурации")
	case "mihomo":
		f := "/opt/etc/mihomo/config.yaml"
		raw, err := os.ReadFile(f)
		if err != nil || len(raw) == 0 {
			t.Fatalf("конфигурация Mihomo %s не читается: %v", f, err)
		}
		return f, string(raw)
	}
	t.Fatalf("неизвестное ядро %q", kernel)
	return "", ""
}

// rtHelperWaitNewPID ждёт процесс ядра с PID, отличным от old, до wait.
func rtHelperWaitNewPID(name string, old int, wait time.Duration) (int, string) {
	deadline := time.Now().Add(wait)
	var out string
	for time.Now().Before(deadline) {
		var pids []int
		pids, out = rtRstKernelPIDs(name)
		if len(pids) == 1 && pids[0] != old {
			return pids[0], out
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, out
}

func TestRouterRestartWithKernelHelper(t *testing.T) {
	tg := routertest.Current(t)
	kernel := tg.Core
	if kernel != "xray" && kernel != "mihomo" {
		t.Fatalf("неизвестное ядро цели %q", kernel)
	}
	pid := rtRstKernelPID(t, kernel)
	path, content := rtHelperValidateTarget(t, kernel)
	p := routertest.NewPanel(t)

	// Ядро, оставшееся лежать после провала, поднимается: следующие проверки
	// работают с тем же стендом.
	t.Cleanup(func() {
		if pids, _ := rtRstKernelPIDs(kernel); len(pids) > 0 {
			return
		}
		_, _, _ = p.Call(http.MethodPost, "/api/service/control?action=start", nil)
		rtHelperWaitNewPID(kernel, 0, rtHelperRestartWait)
	})

	body := map[string]string{"path": path, "content": content}
	for round := 1; round <= rtHelperRounds; round++ {
		var (
			stop      = make(chan struct{})
			wg        sync.WaitGroup
			completed atomic.Int64
			lastErr   atomic.Value
		)
		for i := 0; i < rtHelperValidators; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					status, raw, err := p.Call(http.MethodPost, "/api/config/validate", body)
					switch {
					case err != nil:
						lastErr.Store(err.Error())
					case status != http.StatusOK:
						lastErr.Store(string(raw))
					default:
						completed.Add(1)
					}
				}
			}()
		}
		stopHelpers := func() {
			select {
			case <-stop:
			default:
				close(stop)
			}
			wg.Wait()
		}

		time.Sleep(rtHelperLead)
		before := completed.Load()
		if before == 0 {
			stopHelpers()
			t.Fatalf("перезапуск %d: за %v ни одна проверка конфигурации %s не завершилась (ошибка %v)",
				round, rtHelperLead, path, lastErr.Load())
		}
		status, raw, err := p.Call(http.MethodPost, "/api/service/control?action=restart", nil)
		if err != nil || status != http.StatusOK {
			stopHelpers()
			t.Fatalf("перезапуск %d: %d %v %.300s", round, status, err, raw)
		}
		next, out := rtHelperWaitNewPID(kernel, pid, rtHelperRestartWait)
		stopHelpers()
		if next == 0 {
			t.Fatalf("перезапуск %d: ядро %s не поднялось за %v при идущих проверках конфигурации (pidof: %q, проверок %d)",
				round, kernel, rtHelperRestartWait, out, completed.Load())
		}
		if completed.Load() == before {
			t.Fatalf("перезапуск %d: за время перезапуска ни одна проверка конфигурации не завершилась (ошибка %v)",
				round, lastErr.Load())
		}
		// Ядро держится с новым PID уже без проверок: вспомогательные процессы не
		// мешают подсчёту.
		rtRstHoldPID(t, kernel, next)
		pid = next
	}
}
