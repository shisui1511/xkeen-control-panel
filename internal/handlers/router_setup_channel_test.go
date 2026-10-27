//go:build router

package handlers

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Каналы установщика: настоящий scripts/setup.sh этой сборки запускается на
// устройстве (копия из XCP_RT_SETUP, go-suite.sh) и только во временном каталоге:
// XCP_INSTALL_DIR, XCP_BIN_PATH и XCP_INIT_SCRIPT указывают внутрь t.TempDir().
// Работающая панель, /opt/sbin/xcp, /opt/etc/xcp и настоящий init-скрипт не
// затрагиваются: сценарий вызывает только --help, --status и --update, причём
// --update — лишь когда версия стенда равна последней версии канала (ветка
// «уже установлена»: без скачивания, остановки и запуска службы).
//
// Проверяется: справка называет каналы stable/beta, имя канала из файла channel
// или --channel проверяется в одном месте (неизвестное — stable и ровно одно
// предупреждение, файл channel не переписывается), а выбор версии установщиком
// для Основного и Предварительного совпадает с панелью. Разбор опубликованного
// тега -beta.N настоящим установщиком проверяется после выхода такого тега.

const (
	rtSetupUnknownMark = "Неизвестный канал «%s» — используется Основной (stable)"
	rtSetupUpToDate    = "Уже установлена последняя версия: v"
	rtSetupLiveBin     = "/opt/sbin/xcp"
	rtSetupUpdateLimit = 120 * time.Second
)

// rtSetupRun запускает установщик с окружением, указывающим во временный каталог dir.
// Возвращает объединённый вывод и код выхода.
func rtSetupRun(t *testing.T, dir string, timeout time.Duration, args ...string) (string, int) {
	t.Helper()
	script := routertest.SetupScript(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	argv := []string{
		"XCP_INSTALL_DIR=" + dir,
		"XCP_BIN_PATH=" + filepath.Join(dir, "xcp"),
		"XCP_INIT_SCRIPT=" + filepath.Join(dir, "S99xcp"),
		"sh", script,
	}
	argv = append(argv, args...)
	stdout, stderr, code := routertest.Exec(ctx, "env", argv...)
	return stdout + stderr, code
}

// rtSetupUnknownWarn — предупреждение установщика о неизвестном канале name.
func rtSetupUnknownWarn(name string) string {
	return strings.Replace(rtSetupUnknownMark, "%s", name, 1)
}

// rtSetupStatusOK — --status печатает состояние службы: код 0 (запущена) или 3.
func rtSetupStatusOK(code int) bool { return code == 0 || code == 3 }

// rtSetupChannelFile читает файл channel установщика во временном каталоге.
func rtSetupChannelFile(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "channel"))
	if err != nil {
		t.Fatalf("файл channel не читается: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// rtSetupLatest — текущая и последняя версии канала по панели; ok=false, если
// проверка обновлений недоступна (нет интернета у стенда) — путь --update не проверяется.
func rtSetupLatest(t *testing.T, p *routertest.Panel, channel string) (current, latest string, ok bool) {
	t.Helper()
	var info struct {
		CurrentVersion string `json:"current_version"`
		LatestVersion  string `json:"latest_version"`
	}
	status, err := p.GetJSON("/api/update/check?channel="+channel, &info)
	if err != nil || status != http.StatusOK {
		t.Logf("проверка обновлений канала %s недоступна: статус %d, ошибка %v", channel, status, err)
		return "", "", false
	}
	return info.CurrentVersion, info.LatestVersion, true
}

func rtSetupCopyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("исходный файл не открывается: %v", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("копия не создаётся: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("копирование не удалось: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("копия не закрывается: %v", err)
	}
}

func TestRouterSetupChannel(t *testing.T) {
	routertest.Current(t)

	t.Run("help", func(t *testing.T) {
		out, code := rtSetupRun(t, t.TempDir(), 30*time.Second, "--help")
		if code != 0 {
			t.Fatalf("--help: код %d, вывод: %s", code, out)
		}
		if !strings.Contains(out, "stable/beta") {
			t.Errorf("справка не называет каналы stable/beta: %s", out)
		}
		if strings.Contains(strings.ToLower(out), "prerelease") {
			t.Errorf("справка упоминает прежний канал: %s", out)
		}
	})

	t.Run("unknown-channel", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "channel"), []byte("prerelease\n"), 0o644); err != nil {
			t.Fatalf("файл channel не создан: %v", err)
		}
		out, code := rtSetupRun(t, dir, 30*time.Second, "--status")
		if !rtSetupStatusOK(code) {
			t.Fatalf("--status: код %d, вывод: %s", code, out)
		}
		if n := strings.Count(out, rtSetupUnknownWarn("prerelease")); n != 1 {
			t.Errorf("предупреждение о неизвестном канале встретилось %d раз, нужно ровно 1: %s", n, out)
		}
		if got := rtSetupChannelFile(t, dir); got != "prerelease" {
			t.Errorf("--status переписал файл channel: %q", got)
		}
	})

	t.Run("channel-flag", func(t *testing.T) {
		out, code := rtSetupRun(t, t.TempDir(), 30*time.Second, "--channel", "beta", "--status")
		if !rtSetupStatusOK(code) {
			t.Fatalf("--channel beta --status: код %d, вывод: %s", code, out)
		}
		if strings.Contains(out, "Неизвестный канал") {
			t.Errorf("канал beta признан неизвестным: %s", out)
		}
		out, code = rtSetupRun(t, t.TempDir(), 30*time.Second, "--channel", "nightly", "--status")
		if !rtSetupStatusOK(code) {
			t.Fatalf("--channel nightly --status: код %d, вывод: %s", code, out)
		}
		if n := strings.Count(out, rtSetupUnknownWarn("nightly")); n != 1 {
			t.Errorf("предупреждение о канале nightly встретилось %d раз, нужно ровно 1: %s", n, out)
		}
	})

	for _, channel := range []string{"stable", "beta"} {
		t.Run("update-"+channel, func(t *testing.T) {
			p := routertest.NewPanel(t)
			current, latest, ok := rtSetupLatest(t, p, channel)
			if !ok {
				t.Skipf("проверка обновлений канала %s недоступна: путь --update не проверяется", channel)
			}
			if latest == "" || current != latest {
				t.Skipf("версия стенда %s не последняя в канале %s (%s): путь --update не проверяется", current, channel, latest)
			}

			before, err := os.Stat(rtSetupLiveBin)
			if err != nil {
				t.Fatalf("бинарник панели не найден: %v", err)
			}
			dir := t.TempDir()
			rtSetupCopyFile(t, rtSetupLiveBin, filepath.Join(dir, "xcp"))

			out, code := rtSetupRun(t, dir, rtSetupUpdateLimit, "--channel", channel, "--update")
			if code != 0 {
				t.Errorf("--update: код %d, вывод: %s", code, out)
			}
			if want := rtSetupUpToDate + latest; !strings.Contains(out, want) {
				t.Errorf("установщик выбрал для канала %s не ту версию, что панель (%s): %s", channel, latest, out)
			}
			if got := rtSetupChannelFile(t, dir); got != channel {
				t.Errorf("файл channel содержит %q, нужно %q", got, channel)
			}

			after, err := os.Stat(rtSetupLiveBin)
			if err != nil {
				t.Fatalf("бинарник панели пропал: %v", err)
			}
			if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
				t.Errorf("бинарник панели изменился: размер %d→%d, время %s→%s",
					before.Size(), after.Size(), before.ModTime(), after.ModTime())
			}
			if status, err := p.GetJSON("/api/update/check?channel="+channel, nil); err != nil || status != http.StatusOK {
				t.Errorf("панель после --update не отвечает: статус %d, ошибка %v", status, err)
			}
		})
	}
}
