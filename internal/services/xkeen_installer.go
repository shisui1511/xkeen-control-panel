package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// XKeenInstallerURL — официальный установщик XKeen. Он сам скачивает архив
// выбранного канала (с проверкой SHA-256 для stable) и раскладывает файлы;
// затем `xkeen -i` проводит интерактивную настройку.
const XKeenInstallerURL = "https://raw.githubusercontent.com/jameszeroX/XKeen/main/install.sh"

// xkeenInstallerMirrors — те же зеркала GitHub, что использует сам
// установщик XKeen: raw.githubusercontent.com бывает недоступен.
var xkeenInstallerMirrors = []string{"", "https://gh-proxy.com/", "https://ghfast.top/"}

// xkeenInstallerMaxBytes — предел размера install.sh (сейчас ~15 КБ).
const xkeenInstallerMaxBytes = 1 << 20

// xkeenInstallerMarker — строка, по которой скачанный файл опознаётся как
// установщик XKeen, а не страница-заглушка зеркала.
var xkeenInstallerMarker = []byte("jameszeroX/XKeen")

// XKeenChannels — каналы установщика и соответствующие флаги install.sh.
var XKeenChannels = map[string]string{
	"stable": "--stable",
	"beta":   "--beta",
}

// ErrXKeenInstallRunning — установка уже идёт в другой сессии.
var ErrXKeenInstallRunning = errors.New("XKeen installation is already running")

// XKeenInstaller готовит запуск официального установщика XKeen.
type XKeenInstaller struct {
	// URL и Mirrors переопределяются в тестах
	URL     string
	Mirrors []string
	Client  *http.Client
	// Dir — каталог для скачанного install.sh. На роутере это /opt/tmp на
	// накопителе: /tmp в RAM, а установщик качает в текущий каталог архивы
	Dir string
	// InitDir — каталог init-скриптов Entware: без него XKeen не установить
	// (и установщик не должен запускаться на ПК разработчика)
	InitDir string
	// Shell — оболочка для запуска установщика; пусто — xkeenInstallerShell()
	Shell string

	running sync.Mutex
}

// ErrXKeenNoEntware — установка невозможна: Entware не найден.
var ErrXKeenNoEntware = errors.New("Entware not found: XKeen can only be installed on a router with Entware")

// xkeenShellCandidates — оболочки по приоритету. /bin/sh на Keenetic — это
// NDM Shell Wrapper: в режиме `-c` он отбрасывает аргументы после скрипта,
// и установщик получал пустой путь. Шелл Entware передаёт их как положено.
var xkeenShellCandidates = []string{"/opt/bin/sh", "/bin/sh"}

// xkeenInstallerShell возвращает первую существующую оболочку из кандидатов.
func xkeenInstallerShell() string {
	for _, sh := range xkeenShellCandidates {
		if fi, err := os.Stat(sh); err == nil && !fi.IsDir() {
			return sh
		}
	}
	return "/bin/sh"
}

// xkeenInitScript — init-скрипт, который создаёт `xkeen -i` в конце настройки.
// Бинарник без него — прерванная установка.
const xkeenInitScript = "S05xkeen"

// SetupComplete сообщает, доведена ли настройка XKeen до конца.
func (x *XKeenInstaller) SetupComplete() bool {
	fi, err := os.Stat(filepath.Join(x.InitDir, xkeenInitScript))
	return err == nil && !fi.IsDir()
}

// Available сообщает, можно ли ставить XKeen на этой системе.
func (x *XKeenInstaller) Available() bool {
	fi, err := os.Stat(x.InitDir)
	return err == nil && fi.IsDir()
}

// NewXKeenInstaller создаёт установщик с официальным URL и зеркалами.
func NewXKeenInstaller() *XKeenInstaller {
	dir := "/opt/tmp"
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		dir = os.TempDir()
	}
	return &XKeenInstaller{
		URL:     XKeenInstallerURL,
		Mirrors: xkeenInstallerMirrors,
		Client:  utils.SafeHTTPClient(30 * time.Second),
		Dir:     dir,
		InitDir: "/opt/etc/init.d",
	}
}

// Acquire занимает установщик; release освобождает его. Одновременно
// допускается только одна установка.
func (x *XKeenInstaller) Acquire() (release func(), err error) {
	if !x.running.TryLock() {
		return nil, ErrXKeenInstallRunning
	}
	return x.running.Unlock, nil
}

// Download скачивает install.sh (напрямую или через зеркало) и сохраняет во
// временный файл. Возвращает путь к нему и адрес, откуда он получен.
func (x *XKeenInstaller) Download(ctx context.Context) (path, source string, err error) {
	var errs []error
	for _, mirror := range x.Mirrors {
		src := mirror + x.URL
		body, err := x.fetch(ctx, src)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", src, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		f, err := os.CreateTemp(x.Dir, "xcp-xkeen-install-*.sh")
		if err != nil {
			return "", "", err
		}
		if _, err := f.Write(body); err != nil {
			f.Close()
			os.Remove(f.Name())
			return "", "", err
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return "", "", err
		}
		return f.Name(), src, nil
	}
	return "", "", fmt.Errorf("failed to download XKeen installer: %w", errors.Join(errs...))
}

func (x *XKeenInstaller) fetch(ctx context.Context, src string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := x.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, xkeenInstallerMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > xkeenInstallerMaxBytes {
		return nil, errors.New("installer is too large")
	}
	if !bytes.HasPrefix(body, []byte("#!/bin/sh")) || !bytes.Contains(body, xkeenInstallerMarker) {
		return nil, errors.New("response is not the XKeen installer")
	}
	return body, nil
}

// Command — argv для PTY: установщик выбранного канала. install.sh сам
// заканчивается `exec xkeen -i`; повторно настройка запускается, только если
// init-скрипт XKeen так и не появился. Скачанный скрипт удаляется сразу после
// запуска установщика.
func (x *XKeenInstaller) Command(scriptPath, channel string) ([]string, error) {
	flag, ok := XKeenChannels[channel]
	if !ok {
		return nil, fmt.Errorf("unknown XKeen channel %q", channel)
	}
	if filepath.Dir(scriptPath) != filepath.Clean(x.Dir) {
		return nil, errors.New("installer script outside of installer dir")
	}
	// Путь, флаг и init-скрипт передаются позиционными аргументами, не
	// подстановкой в текст скрипта. cd — установщик скачивает архив в текущий каталог
	const script = `cd "$(dirname "$1")" || exit 1
sh "$1" "$2"; rc=$?
rm -f "$1"
[ "$rc" -eq 0 ] || exit "$rc"
[ -f "$3" ] || exec xkeen -i`
	shell := x.Shell
	if shell == "" {
		shell = xkeenInstallerShell()
	}
	initScript := filepath.Join(x.InitDir, xkeenInitScript)
	return []string{shell, "-c", script, "xkeen-install", scriptPath, flag, initScript}, nil
}
