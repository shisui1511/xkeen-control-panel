//go:build router

package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Отдельный процесс ядра для сценариев трафика. Работающие ядра стенда не
// трогаются: процесс читает копию файлов панели во временном каталоге рабочего
// каталога теста, слушает socks на 127.0.0.1 на порту, выбранном системой, и
// останавливается в t.Cleanup. «Узел» — второй процесс Xray с socks-входящим
// без авторизации и журналом доступа: по нему видно, какие домены дошли до узла.
// В лог теста попадают только «да/нет» и числа: адресов узлов и внешних IP нет.

// rtCoreReadyTimeout — сколько ждём открытия порта socks.
const rtCoreReadyTimeout = 60 * time.Second

// rtIPv4Re — IPv4 в выводе ядра (кроме loopback скрываются).
var rtIPv4Re = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)

// rtRedact скрывает адреса в тексте вывода ядра и ограничивает его длину.
func rtRedact(s string) string {
	s = rtIPv4Re.ReplaceAllStringFunc(s, func(ip string) string {
		if ip == "127.0.0.1" {
			return ip
		}
		return "x.x.x.x"
	})
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 400 {
		s = string(r[:400]) + " …"
	}
	return s
}

// rtProc — запущенный процесс ядра.
type rtProc struct {
	cmd    *exec.Cmd
	exited chan struct{}
	mu     sync.Mutex
	buf    bytes.Buffer
}

// Write собирает вывод процесса (не больше 64 КиБ).
func (p *rtProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.buf.Len() < 1<<16 {
		p.buf.Write(b)
	}
	return len(b), nil
}

// alive сообщает, что процесс ещё не завершился.
func (p *rtProc) alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

// tail — очищенный от адресов вывод процесса.
func (p *rtProc) tail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return rtRedact(p.buf.String())
}

// stop завершает процесс и ждёт его выхода.
func (p *rtProc) stop() {
	if !p.alive() {
		return
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.exited:
	case <-time.After(20 * time.Second):
	}
}

// rtFreePort возвращает свободный порт loopback, выбранный системой.
func rtFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("свободный порт: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// rtWorkSubdir создаёт каталог внутри рабочего каталога теста.
func rtWorkSubdir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp(routertest.Current(t).WorkDir, prefix)
	if err != nil {
		t.Fatalf("каталог теста: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// rtStartProc запускает процесс и ждёт, пока откроется порт socks на loopback.
// Процесс останавливается в t.Cleanup. Процесс, завершившийся до открытия порта,
// или порт, не открывшийся вовремя, — провал теста с очищенным выводом.
func rtStartProc(t *testing.T, bin string, args []string, env []string, port int) *rtProc {
	t.Helper()
	p := &rtProc{exited: make(chan struct{})}
	p.cmd = exec.Command(bin, args...)
	p.cmd.Stdout = p
	p.cmd.Stderr = p
	if env != nil {
		p.cmd.Env = env
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("процесс ядра %s не запустился: %v", filepath.Base(bin), err)
	}
	go func() {
		_ = p.cmd.Wait()
		close(p.exited)
	}()
	t.Cleanup(p.stop)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(rtCoreReadyTimeout)
	for {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			return p
		}
		if !p.alive() {
			t.Fatalf("процесс ядра %s завершился до открытия порта: %s", filepath.Base(bin), p.tail())
		}
		if time.Now().After(deadline) {
			t.Fatalf("порт socks ядра %s не открылся за %s: %s", filepath.Base(bin), rtCoreReadyTimeout, p.tail())
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// rtXrayCoreDir готовит каталог отдельного ядра Xray: копия *.json из filesDir
// (файлы панели) и 00_rt_inbound.json с socks-входящим на порту. Возвращает
// каталог; файлы можно править до запуска.
func rtXrayCoreDir(t *testing.T, filesDir string, port int) string {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-core-")
	entries, err := os.ReadDir(filesDir)
	if err != nil {
		t.Fatalf("каталог файлов панели: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filesDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inbound, err := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"tag": "rt-in", "listen": "127.0.0.1", "port": port,
			"protocol": "socks", "settings": map[string]any{"udp": false},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "00_rt_inbound.json"), inbound, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// rtStartXrayDir запускает `xray run -confdir <dir>` и ждёт порт socks.
func rtStartXrayDir(t *testing.T, dir string, port int) *rtProc {
	t.Helper()
	return rtStartProc(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""), port)
}

// rtStartMihomoCore запускает отдельное ядро Mihomo на каталоге dir
// (config.yaml в нём) и ждёт порт socks. Для сценариев Mihomo (146-11).
func rtStartMihomoCore(t *testing.T, dir string, port int) *rtProc {
	t.Helper()
	return rtStartProc(t, rtMihomoBin, []string{"-d", dir, "-f", filepath.Join(dir, "config.yaml")}, nil, port)
}

// rtStartNode запускает «узел»: Xray с socks-входящим без авторизации и freedom,
// журнал доступа пишется в файл. Возвращает порт узла и путь журнала.
func rtStartNode(t *testing.T) (port int, accessLog string) {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-node-")
	port = rtFreePort(t)
	accessLog = filepath.Join(dir, "access.log")
	cfg, err := json.Marshal(map[string]any{
		"log": map[string]any{"loglevel": "warning", "access": accessLog},
		"inbounds": []any{map[string]any{
			"tag": "rt-node-in", "listen": "127.0.0.1", "port": port,
			"protocol": "socks", "settings": map[string]any{"udp": false, "auth": "noauth"},
		}},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "node.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	rtStartProc(t, rtXrayBin, []string{"run", "-config", path}, utils.XrayAssetEnv(os.Environ(), ""), port)
	return port, accessLog
}

// rtNodeSaw сообщает, есть ли домен в журнале доступа узла.
func rtNodeSaw(accessLog, domain string) bool {
	data, err := os.ReadFile(accessLog)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), domain)
}

// rtWaitNodeSaw ждёт появления домена в журнале узла (запись идёт асинхронно).
func rtWaitNodeSaw(accessLog, domain string, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for {
		if rtNodeSaw(accessLog, domain) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// rtSocksGet делает GET через socks5 отдельного ядра (имя узла отдаётся ядру,
// не резолвится тестом). Возвращает HTTP-статус; ошибка — запрос не прошёл
// (соединение закрыто, таймаут, отказ socks).
func rtSocksGet(socksPort int, rawURL string, timeout time.Duration) (int, error) {
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), nil, proxy.Direct)
	if err != nil {
		return 0, err
	}
	cd, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return 0, io.ErrUnexpectedEOF
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return cd.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<12))
	return resp.StatusCode, nil
}
