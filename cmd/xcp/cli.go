package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/auth"
	"github.com/shisui1511/xkeen-control-panel/internal/config"
	"golang.org/x/term"
)

// cliDeps — зависимости CLI-команд xcp (--reset-password, --setup-code),
// подменяемые в тестах: без реального терминала, реальных сигналов ОС и
// файлов /proc.
type cliDeps struct {
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	isTerminal     func() bool
	readPassword   func(prompt string) (string, error)
	findDaemonPIDs func() ([]int, error)
	signalPID      func(pid int) error
	selfPID        int
	sleep          func(time.Duration)
}

// defaultCLIDeps — реальные зависимости для запуска на роутере: isTerminal и
// readPassword используют golang.org/x/term — ввод пароля без эха
// (T-134-39), с подтверждением в интерактивном режиме.
func defaultCLIDeps() cliDeps {
	return cliDeps{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		isTerminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd()))
		},
		readPassword: func(prompt string) (string, error) {
			fmt.Fprint(os.Stderr, prompt)
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		findDaemonPIDs: findDaemonPIDsDefault,
		signalPID: func(pid int) error {
			return syscall.Kill(pid, syscall.SIGHUP)
		},
		selfPID: os.Getpid(),
		sleep:   time.Sleep,
	}
}

// runResetPassword реализует `xcp --reset-password` (D-21, D-22): читает
// новый пароль (из stdin для скриптов или интерактивно с подтверждением),
// проверяет его политикой auth.ValidateNewPassword, пишет bcrypt-хеш в
// config.json (0600, атомарно через Config.SavePasswordHash), удаляет код
// первичной настройки и сбрасывает персистентное состояние сессий/
// rate-limiter'а (auth.ResetPersistedAuthState), затем уведомляет работающую
// панель сигналом SIGHUP (без сетевого эндпоинта — T-134-38) и проверяет, что
// панель не перезаписала конфиг раньше CLI. Пароль нигде не печатается и не
// попадает в лог (T-134-39) — только служебная строка [auth] в xcp.log.
func runResetPassword(configPath string, fromStdin bool, d cliDeps) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(d.stderr, "конфиг не найден: %s\n", configPath)
		return 1
	}

	password, code, ok := readNewPassword(fromStdin, d)
	if !ok {
		return code
	}

	if err := auth.ValidateNewPassword(password, cfg.Auth.PasswordHash); err != nil {
		fmt.Fprintln(d.stderr, policyErrorMessage(err))
		return 1
	}

	hash, err := auth.GeneratePasswordHash(password)
	if err != nil {
		fmt.Fprintf(d.stderr, "не удалось хешировать пароль: %v\n", err)
		return 1
	}

	if err := cfg.SavePasswordHash(cfg.ConfigPath, hash); err != nil {
		fmt.Fprintf(d.stderr, "не удалось сохранить конфиг: %v\n", err)
		return 1
	}

	// Код первичной настройки (D-24) больше не действителен, как только
	// пароль задан; побочные сессии и блокировки rate-limiter'а не должны
	// пережить сброс пароля мимо запущенной панели (134-03).
	if err := auth.RemoveSetupCode(cfg.DataDir); err != nil {
		fmt.Fprintf(d.stderr, "не удалось удалить код настройки: %v\n", err)
	}
	if err := auth.ResetPersistedAuthState(cfg.DataDir); err != nil {
		fmt.Fprintf(d.stderr, "не удалось сбросить состояние сессий: %v\n", err)
	}

	daemonNotified := notifyDaemon(d)

	if daemonNotified {
		// Окно гонки (RESEARCH, ребро unclassified): если демон в этот
		// момент сам перезаписывает config.json (сохранение настроек из
		// UI), новый хеш может потеряться. Перечитываем файл и просим
		// повторить команду, если хеш не совпал с только что записанным.
		d.sleep(2 * time.Second)
		if reCfg, rerr := config.Load(cfg.ConfigPath); rerr == nil && reCfg.Auth.PasswordHash != hash {
			fmt.Fprintln(d.stderr, "конфиг перезаписан панелью — повторите команду")
			return 1
		}
	}

	appendAuthLog(cfg.XCPLogPath, daemonNotified)
	return 0
}

// readNewPassword возвращает новый пароль: из первой строки stdin
// (fromStdin, до 1024 байт) или интерактивно с подтверждением (терминал).
// ok=false означает, что вызывающий код должен немедленно вернуть code.
func readNewPassword(fromStdin bool, d cliDeps) (password string, code int, ok bool) {
	if fromStdin {
		reader := bufio.NewReader(io.LimitReader(d.stdin, 1024))
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(d.stderr, "не удалось прочитать пароль из stdin")
			return "", 1, false
		}
		return strings.TrimRight(line, "\r\n"), 0, true
	}

	if !d.isTerminal() {
		fmt.Fprintln(d.stderr, "нет терминала: используйте --password-stdin")
		return "", 1, false
	}

	pw1, err := d.readPassword("Новый пароль: ")
	if err != nil {
		fmt.Fprintf(d.stderr, "не удалось прочитать пароль: %v\n", err)
		return "", 1, false
	}
	pw2, err := d.readPassword("Повторите пароль: ")
	if err != nil {
		fmt.Fprintf(d.stderr, "не удалось прочитать пароль: %v\n", err)
		return "", 1, false
	}
	if pw1 != pw2 {
		fmt.Fprintln(d.stderr, "Пароли не совпадают")
		return "", 1, false
	}
	return pw1, 0, true
}

// notifyDaemon уведомляет запущенную панель сигналом SIGHUP. Возвращает
// true, если хотя бы один процесс был успешно уведомлён.
func notifyDaemon(d cliDeps) bool {
	pids, err := d.findDaemonPIDs()
	if err != nil || len(pids) == 0 {
		fmt.Fprintln(d.stdout, "Панель не запущена: новый пароль применится при запуске")
		return false
	}

	notified := false
	for _, pid := range pids {
		if err := d.signalPID(pid); err != nil {
			fmt.Fprintf(d.stderr, "не удалось уведомить панель (PID %d): %v\n", pid, err)
			continue
		}
		notified = true
	}
	if notified {
		fmt.Fprintln(d.stdout, "Панель уведомлена: все сессии завершены")
	}
	return notified
}

// policyErrorMessage переводит ошибку auth.ValidateNewPassword в тот же
// русский текст, что и серверные i18n-ключи auth.password_* (ru.json) — CLI
// не имеет доступа к internal/i18n, поэтому тексты продублированы буквально.
func policyErrorMessage(err error) string {
	switch auth.PolicyErrorCode(err) {
	case "password_too_short":
		return "Пароль должен быть не менее 8 символов"
	case "password_too_long":
		return "Пароль не должен превышать 72 байта"
	case "password_repeated_char":
		return "Пароль не может состоять из одного повторяющегося символа"
	case "password_blacklisted":
		return "Пароль слишком простой — выберите другой"
	case "password_same_as_current":
		return "Новый пароль совпадает с текущим"
	default:
		return err.Error()
	}
}

// appendAuthLog дописывает служебную строку в xcp.log (D-11): факт сброса
// пароля через CLI и то, была ли уведомлена запущенная панель. Пароль и хеш
// сюда никогда не попадают.
func appendAuthLog(path string, daemonNotified bool) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Printf("[auth] failed to close audit log %s: %v", path, cerr)
		}
	}()
	log.New(f, "", log.LstdFlags).Printf("[auth] password reset via CLI (daemon notified: %t)", daemonNotified)
}

// parseDaemonPIDs исключает из вывода pidof собственный PID вызывающего CLI
// и PID других CLI-вызовов (reset-password/setup-code, определяются по
// /proc/<pid>/cmdline через readCmdline) — оставшиеся PID принадлежат
// запущенному демону панели (T-134-41). readCmdline==nil — фильтрация по
// cmdline пропускается (используется, когда /proc недоступен).
func parseDaemonPIDs(pidofOut []byte, selfPID int, readCmdline func(int) (string, error)) []int {
	fields := strings.Fields(string(pidofOut))
	pids := make([]int, 0, len(fields))
	for _, f := range fields {
		if pid, err := strconv.Atoi(f); err == nil {
			pids = append(pids, pid)
		}
	}
	return filterDaemonPIDs(pids, selfPID, readCmdline)
}

// filterDaemonPIDs — общая логика исключения для parseDaemonPIDs и
// findDaemonPIDsDefault (ветка без pidof).
func filterDaemonPIDs(pids []int, selfPID int, readCmdline func(int) (string, error)) []int {
	out := make([]int, 0, len(pids))
	for _, pid := range pids {
		if pid == selfPID {
			continue
		}
		if readCmdline != nil {
			if cmdline, err := readCmdline(pid); err == nil {
				if strings.Contains(cmdline, "reset-password") || strings.Contains(cmdline, "setup-code") {
					continue
				}
			}
		}
		out = append(out, pid)
	}
	return out
}

// readProcCmdline читает /proc/<pid>/cmdline (аргументы разделены NUL-байтом).
func readProcCmdline(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// findDaemonPIDsDefault ищет PID запущенной панели через pidof (штатный путь
// на роутере, RESEARCH находка 3); при отсутствии pidof — обход /proc/*/comm.
func findDaemonPIDsDefault() ([]int, error) {
	selfPID := os.Getpid()
	if out, err := exec.Command("pidof", "xcp").Output(); err == nil {
		return parseDaemonPIDs(out, selfPID, readProcCmdline), nil
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	pids := make([]int, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "xcp" {
			pids = append(pids, pid)
		}
	}
	return filterDaemonPIDs(pids, selfPID, readProcCmdline), nil
}

// runSetupCode реализует `xcp --setup-code` (D-26): пока пароль не задан,
// печатает действующий одноразовый код первичной настройки (переиздавая его
// при необходимости через auth.EnsureSetupCode — тот же код, что видит
// setup.sh/веб-UI); если пароль уже задан, код настройки больше не
// применяется — сообщает об этом и предлагает `xcp --reset-password`.
func runSetupCode(configPath string, d cliDeps) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(d.stderr, "конфиг не найден: %s\n", configPath)
		return 1
	}

	if cfg.Auth.PasswordHash != "" {
		fmt.Fprintln(d.stderr, "Пароль уже задан. Для сброса выполните: xcp --reset-password")
		return 1
	}

	code, err := auth.EnsureSetupCode(cfg.DataDir)
	if err != nil {
		fmt.Fprintf(d.stderr, "не удалось получить код настройки: %v\n", err)
		return 1
	}

	fmt.Fprintln(d.stdout, code)
	return 0
}

// printUsage печатает справку `xcp --help`/`-h` (D-23): синтаксис, флаги и
// инструкцию восстановления доступа при утере пароля.
func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Использование: xcp [-config путь] [флаги]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Флаги:")
	fmt.Fprintln(w, "  -config путь       Путь к config.json (по умолчанию /opt/etc/xcp/config.json)")
	fmt.Fprintln(w, "  -v, --version      Показать версию и выйти")
	fmt.Fprintln(w, "  --reset-password   Задать новый пароль администратора (интерактивно, без эха)")
	fmt.Fprintln(w, "  --password-stdin   С --reset-password: прочитать новый пароль из первой строки stdin")
	fmt.Fprintln(w, "  --setup-code       Показать действующий код первичной настройки (пока пароль не задан)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Забыли пароль? Подключитесь к роутеру по SSH и выполните: xcp --reset-password")
}

// reloadAuthFromConfig перечитывает config.json и применяет новый хеш пароля
// к живому AuthService (D-22) — вызывается обработчиком SIGHUP в main.go.
// cfg.Auth.PasswordHash синхронизируется тем же вызовом (через колбэк apply),
// чтобы следующее сохранение настроек из веб-UI не затёрло свежий пароль
// старым значением, оставшимся в памяти демона.
func reloadAuthFromConfig(configPath string, cfg *config.Config, authSvc *auth.AuthService) error {
	newCfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	authSvc.ReloadPasswordHash(newCfg.Auth.PasswordHash, func(hash string) {
		cfg.Auth.PasswordHash = hash
	})
	return nil
}
