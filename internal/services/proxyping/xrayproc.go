package proxyping

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/nodes"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

type tempXray struct {
	dir   string
	ports []int
	cmd   *exec.Cmd
	done  chan struct{}
	once  sync.Once
}

func randomID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func privateFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

func privateJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return privateFile(path, b, 0o600)
}

// linkBinary ставит в каталог пачки жёсткую ссылку на бинарник Xray. Любой
// отказ ссылки (другая ФС, ФС без жёстких ссылок, EPERM/ENOTSUP/EMLINK) —
// копия: она работает везде, где каталог доступен на запись.
func linkBinary(bin, dest string) error {
	if err := os.Link(bin, dest); err == nil {
		return nil
	}
	src, err := os.Open(bin)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return errors.Join(err, dst.Close())
}

// checkTmpRoot допускает корень временных пачек, только если это настоящий
// каталог (не ссылка) внутри своего родителя, владелец — сам процесс, а у группы
// и остальных нет прав. Путь в общем tmp мог создать кто-то другой: в чужой
// каталог не пишутся учётные данные узлов, его содержимое не удаляется.
func checkTmpRoot(root string) error {
	unsafe := errors.New("ping_tmp_root_unsafe")
	if _, err := utils.NewPathValidator([]string{filepath.Dir(root)}).Validate(root); err != nil {
		return unsafe
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return unsafe
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Geteuid() {
		return unsafe
	}
	return nil
}

// startTempXray повторяет всю подготовку после раннего выхода (например,
// другой процесс занял освобождённый порт). Ошибки наружу — только коды.
func startTempXray(ctx context.Context, bin, root string, targets []Target) (*tempXray, error) {
	for attempt := 0; attempt < 3; attempt++ {
		p, err := startTempAttempt(ctx, bin, root, targets)
		if err == nil {
			return p, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New(ReasonStart)
}

func startTempAttempt(ctx context.Context, bin, root string, targets []Target) (_ *tempXray, err error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err = checkTmpRoot(root); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, id)
	if err = os.Mkdir(dir, 0o700); err != nil {
		return nil, err
	}
	p := &tempXray{dir: dir, done: make(chan struct{})}
	defer func() {
		if err != nil {
			p.stop()
		}
	}()
	if err = linkBinary(bin, filepath.Join(dir, ProcName)); err != nil {
		return nil, err
	}
	var inbounds, outbounds, rules []map[string]any
	// Слушатели удерживают все выбранные порты до готовности конфигурации.
	var listeners []net.Listener
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for i, target := range targets {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			return nil, e
		}
		listeners = append(listeners, l)
		port := l.Addr().(*net.TCPAddr).Port
		p.ports = append(p.ports, port)
		in, out := "pin-"+strconv.Itoa(i), "pout-"+strconv.Itoa(i)
		ob, e := nodes.XrayOutbound(target.Node, out)
		if e != nil {
			return nil, errors.New(ReasonStart)
		}
		inbounds = append(inbounds, map[string]any{"tag": in, "listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}})
		outbounds = append(outbounds, ob)
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{in}, "outboundTag": out})
	}
	if err = privateJSON(filepath.Join(dir, "00_base.json"), map[string]any{"log": map[string]any{"loglevel": "warning"}}); err != nil {
		return nil, err
	}
	if err = privateJSON(filepath.Join(dir, "10_probe.json"), map[string]any{"inbounds": inbounds, "outbounds": outbounds, "routing": map[string]any{"rules": rules}}); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(dir, "out.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	p.cmd = &exec.Cmd{Path: filepath.Join(dir, ProcName), Args: []string{ProcName, "run", "-confdir", dir}, Env: utils.XrayAssetEnv(os.Environ(), ""), SysProcAttr: &syscall.SysProcAttr{Setpgid: true}, Stdout: log, Stderr: log}
	for _, l := range listeners {
		_ = l.Close()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = p.cmd.Wait(); close(p.done) }()
	if err = privateFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(p.cmd.Process.Pid)), 0o600); err != nil {
		return nil, err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		ready := true
		for _, port := range p.ports {
			c, e := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(readyCtx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if e != nil {
				ready = false
				break
			}
			_ = c.Close()
		}
		if ready {
			select {
			case <-p.done:
				return nil, errors.New(ReasonStart)
			default:
				return p, nil
			}
		}
		select {
		case <-p.done:
			return nil, errors.New(ReasonStart)
		case <-readyCtx.Done():
			return nil, readyCtx.Err()
		case <-tick.C:
		}
	}
}

// ownsProcess сверяет отдельный аргумент cmdline, чтобы каталог с общим
// префиксом или PID, повторно выданный другому процессу, не прошёл проверку.
func ownsProcess(pid int, dir string) bool {
	if pid <= 1 {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(string(b), "\x00") {
		if arg == dir {
			return true
		}
	}
	return false
}

func (p *tempXray) stop() {
	p.once.Do(func() {
		defer os.RemoveAll(p.dir)
		if p.cmd == nil || p.cmd.Process == nil {
			return
		}
		pid := p.cmd.Process.Pid
		if !ownsProcess(pid, p.dir) {
			return
		}
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		t := time.NewTimer(2 * time.Second)
		defer t.Stop()
		select {
		case <-p.done:
		case <-t.C:
			if ownsProcess(pid, p.dir) {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
			<-p.done
		}
	})
}

// SweepStale вызывается в main до приёма запросов. Сигнал получает только
// процесс, чей cmdline содержит точный каталог пачки; чужой PID не трогается.
// Каталоги удаляются и без PID, чтобы не оставлять учётные данные на носителе.
func (r *Runner) SweepStale() error {
	if _, err := os.Lstat(r.d.TmpRoot); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// Подменённый корень (ссылка, чужой владелец, открытые права) не читается:
	// иначе уборка удалила бы содержимое каталога, на который он указывает.
	if err := checkTmpRoot(r.d.TmpRoot); err != nil {
		return errors.New("ping_sweep_read")
	}
	entries, err := os.ReadDir(r.d.TmpRoot)
	if err != nil {
		return errors.New("ping_sweep_read")
	}
	var errs []error
	for _, entry := range entries {
		dir := filepath.Join(r.d.TmpRoot, entry.Name())
		if entry.IsDir() {
			data, e := os.ReadFile(filepath.Join(dir, "pid"))
			if e == nil {
				pid, e := strconv.Atoi(strings.TrimSpace(string(data)))
				if e == nil && ownsProcess(pid, dir) {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		}
		if e := os.RemoveAll(dir); e != nil {
			errs = append(errs, errors.New("ping_sweep_remove"))
		}
	}
	return errors.Join(errs...)
}
