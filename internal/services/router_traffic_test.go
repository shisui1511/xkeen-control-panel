//go:build router

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/proxy"
	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
)

// Трафик и TPROXY на устройстве.
//
// TestRouterTrafficSeparateCore (D-42): активные конфиги стендов не содержат socks-входящих,
// поэтому трафик через узел подписки проверяется отдельным процессом того же бинарника
// активного ядра: узел берётся из активного конфига, входящий socks слушает 127.0.0.1 на
// порту, выбранном системой. Работающее ядро и его файлы не трогаются.
//
// TestRouterTproxyRules (D-22): правила XKeen в iptables ведут на порт, который слушает
// активное ядро.
//
// Вывод — только протокол узла и «адреса различаются: да/нет» (D-21): ни адресов, ни имён
// узлов, ни URL подписки в логе нет.

// rtEchoServices — сервисы определения внешнего адреса; при ответе 429 берётся следующий.
var rtEchoServices = []string{
	"https://ipinfo.io/ip",
	"https://icanhazip.com",
	"https://ifconfig.me/ip",
	"https://api.ipify.org",
}

// rtXrayProxyProtocols — протоколы исходящих Xray, которые считаются узлом подписки.
var rtXrayProxyProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"hysteria": true, "hysteria2": true, "wireguard": true,
}

// rtMihomoProxyTypes — типы прокси Mihomo, которые считаются узлом подписки.
var rtMihomoProxyTypes = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "ss": true,
	"hysteria": true, "hysteria2": true, "wireguard": true, "tuic": true,
}

// rtFreePort просит систему выдать свободный порт loopback и сразу его освобождает.
func rtFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось выбрать свободный порт: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// rtMaxNodes — сколько узлов из активного конфига пробуем по порядку: узел подписки может
// быть мёртвым или информационным, это не дефект панели.
const rtMaxNodes = 6

// rtXrayNodes возвращает первые узлы-прокси из активного конфига Xray.
func rtXrayNodes(t *testing.T) []map[string]any {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(rtXrayConfDir, "*.json"))
	if err != nil {
		t.Fatalf("каталог конфигов Xray: %v", err)
	}
	sort.Strings(files)
	var nodes []map[string]any
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var doc struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		for _, ob := range doc.Outbounds {
			if p, _ := ob["protocol"].(string); rtXrayProxyProtocols[p] && len(nodes) < rtMaxNodes {
				nodes = append(nodes, ob)
			}
		}
	}
	if len(nodes) == 0 {
		t.Fatal("в активном конфиге нет узла подписки")
	}
	return nodes
}

// rtMihomoNodes возвращает первые узлы-прокси активного конфига Mihomo: из proxies,
// затем из файлов провайдеров, указанных в config.yaml, затем из каталога proxy_providers.
func rtMihomoNodes(t *testing.T) []map[string]any {
	t.Helper()
	var nodes []map[string]any
	add := func(list []map[string]any) {
		for _, p := range list {
			if ty, _ := p["type"].(string); rtMihomoProxyTypes[ty] && len(nodes) < rtMaxNodes {
				nodes = append(nodes, p)
			}
		}
	}
	readProxies := func(path string) []map[string]any {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var doc struct {
			Proxies []map[string]any `yaml:"proxies"`
		}
		if yaml.Unmarshal(raw, &doc) != nil {
			return nil
		}
		return doc.Proxies
	}

	raw, err := os.ReadFile(filepath.Join(rtMihomoDir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml Mihomo не читается: %v", err)
	}
	var cfg struct {
		Proxies   []map[string]any `yaml:"proxies"`
		Providers map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("config.yaml Mihomo не разбирается: %v", err)
	}
	add(cfg.Proxies)
	var paths []string
	for _, pr := range cfg.Providers {
		if pr.Path != "" {
			paths = append(paths, filepath.Join(rtMihomoDir, pr.Path))
		}
	}
	more, _ := filepath.Glob(filepath.Join(rtMihomoDir, "proxy_providers", "*.yaml"))
	sort.Strings(paths)
	sort.Strings(more)
	for _, path := range append(paths, more...) {
		add(readProxies(path))
	}
	if len(nodes) == 0 {
		t.Fatal("в активном конфиге нет узла подписки")
	}
	return nodes
}

// rtCoreProc — отдельный процесс ядра с socks на loopback.
type rtCoreProc struct {
	cmd    *exec.Cmd
	exited chan struct{}
	mu     sync.Mutex
	buf    bytes.Buffer
	redact func(string) string
}

// rtStartCore запускает команду отдельным процессом и ждёт порт socks на loopback.
// Ошибка запуска или порта возвращается (узел может быть негодным), процесс при этом
// остановлен. Остановка при успехе — Stop.
func rtStartCore(redact func(string) string, port int, bin string, args ...string) (*rtCoreProc, error) {
	pr := &rtCoreProc{exited: make(chan struct{}), redact: redact}
	pr.cmd = exec.Command(bin, args...)
	w := &lockedWriter{mu: &pr.mu, buf: &pr.buf}
	pr.cmd.Stdout = w
	pr.cmd.Stderr = w
	if err := pr.cmd.Start(); err != nil {
		return nil, fmt.Errorf("процесс ядра не запустился: %w", err)
	}
	go func() {
		_ = pr.cmd.Wait()
		close(pr.exited)
	}()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(120 * time.Second)
	for {
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			return pr, nil
		}
		select {
		case <-pr.exited:
			return nil, fmt.Errorf("процесс ядра завершился до открытия порта: %s", pr.Tail())
		default:
		}
		if time.Now().After(deadline) {
			pr.Stop()
			return nil, fmt.Errorf("порт socks не открылся за 120 с: %s", pr.Tail())
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Tail возвращает очищенный вывод процесса.
func (p *rtCoreProc) Tail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.redact(p.buf.String())
}

// Stop завершает процесс и ждёт его выхода.
func (p *rtCoreProc) Stop() {
	select {
	case <-p.exited:
	default:
		_ = p.cmd.Process.Kill()
		select {
		case <-p.exited:
		case <-time.After(20 * time.Second):
		}
	}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() < 1<<16 {
		w.buf.Write(p)
	}
	return len(p), nil
}

// rtFetchIP запрашивает внешний адрес у сервиса через переданный клиент. Возвращает адрес,
// HTTP-статус (0 — сетевая ошибка) и текст сетевой ошибки (пусто, если ответ получен).
func rtFetchIP(client *http.Client, service string) (string, int, string) {
	req, err := http.NewRequest(http.MethodGet, service, nil)
	if err != nil {
		return "", 0, err.Error()
	}
	req.Header.Set("User-Agent", "curl/7.88.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, ""
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", resp.StatusCode, "тело ответа не похоже на адрес"
	}
	return ip, resp.StatusCode, ""
}

// rtInternetUp проверяет выход самой цели в интернет прямыми запросами (мимо узла): интернет
// есть, если хотя бы один сервис определения адреса ответил по HTTP (любой статус, в том
// числе 429). Возвращает также причины по сервисам для сообщения.
func rtInternetUp(client *http.Client) (bool, string) {
	var reasons []string
	for i, svc := range rtEchoServices {
		_, st, errText := rtFetchIP(client, svc)
		if st != 0 {
			return true, ""
		}
		reasons = append(reasons, fmt.Sprintf("сервис %d: %s", i+1, rtShortErr(errText)))
	}
	return false, strings.Join(reasons, "; ")
}

// rtGetPrefixRe — начало ошибки net/http: «Get "адрес": », адрес сервиса в сообщении не нужен.
var rtGetPrefixRe = regexp.MustCompile(`^Get "[^"]*": `)

// rtShortErr сокращает текст сетевой ошибки для сообщения (адреса скрыты).
func rtShortErr(s string) string {
	s = rtGetPrefixRe.ReplaceAllString(s, "")
	s = rtIPv4Re.ReplaceAllStringFunc(s, func(ip string) string {
		if ip == "127.0.0.1" || ip == "0.0.0.0" {
			return ip
		}
		return "x.x.x.x"
	})
	s = rtIPv6Re.ReplaceAllString(s, "x:x")
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:80]) + " … " + string(r[len(r)-70:])
	}
	return s
}

// rtStartNodeProc готовит собственный конфиг с этим узлом и socks на loopback и запускает
// отдельный процесс ядра. Возвращает процесс, порт и протокол узла.
func rtStartNodeProc(t *testing.T, core, dir string, node map[string]any) (*rtCoreProc, int, string, error) {
	t.Helper()
	port := rtFreePort(t)
	redact := rtRedactorValue(node)
	switch core {
	case "xray":
		protocol, _ := node["protocol"].(string)
		delete(node, "proxySettings")
		if ss, ok := node["streamSettings"].(map[string]any); ok {
			if so, ok := ss["sockopt"].(map[string]any); ok {
				delete(so, "dialerProxy")
			}
		}
		node["tag"] = "rt-node"
		cfg := map[string]any{
			"log": map[string]any{"loglevel": "warning"},
			"inbounds": []any{map[string]any{
				"tag": "rt-in", "listen": "127.0.0.1", "port": port,
				"protocol": "socks", "settings": map[string]any{"udp": false},
			}},
			"outbounds": []any{node},
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return nil, 0, protocol, fmt.Errorf("сериализация конфига: %w", err)
		}
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, 0, protocol, err
		}
		pr, err := rtStartCore(redact, port, rtXrayBinary, "run", "-config", path)
		return pr, port, protocol, err
	default:
		protocol, _ := node["type"].(string)
		delete(node, "dialer-proxy")
		node["name"] = "rt-node"
		cfg := map[string]any{
			"mode":      "rule",
			"log-level": "warning",
			"listeners": []any{map[string]any{
				"name": "rt-socks", "type": "socks", "listen": "127.0.0.1", "port": port, "udp": false,
			}},
			"proxies": []any{node},
			"rules":   []string{"MATCH,rt-node"},
		}
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			return nil, 0, protocol, fmt.Errorf("сериализация конфига: %w", err)
		}
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, 0, protocol, err
		}
		pr, err := rtStartCore(redact, port, rtMihomoBin, "-d", dir, "-f", path)
		return pr, port, protocol, err
	}
}

// rtCoreReasonRe — причины отказа в выводе ядра: «error: …» у Mihomo, «failed to …» у Xray.
var rtCoreReasonRe = regexp.MustCompile(`(?:error: |failed to )([^"<>|]{3,70})`)

// Reasons возвращает до двух различных причин отказа из вывода ядра (значения узла и
// адреса в них скрыты).
func (p *rtCoreProc) Reasons() string {
	p.mu.Lock()
	raw := strings.Join(strings.Fields(p.buf.String()), " ")
	p.mu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, m := range rtCoreReasonRe.FindAllStringSubmatch(raw, -1) {
		// logrus пишет многострочную ошибку с литералом \n: берётся первая строка
		r, _, _ := strings.Cut(m[1], `\n`)
		r = strings.TrimSpace(p.redact(r))
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
		if len(out) == 2 {
			break
		}
	}
	return strings.Join(out, ", ")
}

// TestRouterTrafficSeparateCore: вердикт теста — «ядро пропустило трафик через узел подписки»
// (внешний адрес через узел отличается от прямого). Исходы без повторов (D-16):
//   - есть узел, через который запрос прошёл: PASS (адреса различаются) или FAIL (совпали);
//   - у самой цели нет выхода в интернет (до или после перебора): SKIP — вердикта о ядре нет;
//   - выход цели есть, процесс ядра поднимался и принимал соединения, но ни один узел подписки
//     не ответил (обрыв до узла, узел мёртв или отвергает клиента): SKIP с причинами по узлам —
//     вердикт о ядре невозможен, пока нет живого узла;
//   - ядро не приняло конфиг узла (процесс не поднялся, порт не открылся): FAIL.
func TestRouterTrafficSeparateCore(t *testing.T) {
	tg := routertest.Current(t)
	if tg.Core != "xray" && tg.Core != "mihomo" {
		t.Fatalf("активное ядро цели неизвестно: %q", tg.Core)
	}
	var nodes []map[string]any
	if tg.Core == "xray" {
		nodes = rtXrayNodes(t)
	} else {
		nodes = rtMihomoNodes(t)
	}

	direct := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}}
	if up, why := rtInternetUp(direct); !up {
		t.Skipf("у цели нет выхода в интернет: прямые запросы не прошли (%s)", why)
	}
	directIP := map[string]string{} // сервис -> прямой адрес (по одному запросу на сервис)
	limited := map[string]bool{}

	var startFailures int
	var startReason string
	var summaries []string // по узлу: протокол и причина, почему запрос не прошёл
	for i, node := range nodes {
		redact := rtRedactorValue(node)
		dir, err := os.MkdirTemp(tg.WorkDir, "traffic-")
		if err != nil {
			t.Fatalf("рабочий каталог: %v", err)
		}
		pr, port, protocol, err := rtStartNodeProc(t, tg.Core, dir, node)
		if err != nil {
			_ = os.RemoveAll(dir)
			startFailures++
			startReason = err.Error()
			t.Logf("узел %d из %d (протокол %s): процесс ядра не поднялся", i+1, len(nodes), protocol)
			continue
		}

		dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), nil, proxy.Direct)
		if err != nil {
			pr.Stop()
			_ = os.RemoveAll(dir)
			t.Fatalf("клиент socks5: %v", err)
		}
		cd, ok := dialer.(proxy.ContextDialer)
		if !ok {
			pr.Stop()
			_ = os.RemoveAll(dir)
			t.Fatal("клиент socks5 без DialContext")
		}
		viaNode := &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return cd.DialContext(ctx, network, addr)
			},
		}}

		var nodeIP, usedDirect string
		var attempts []string // что вернул каждый сервис через этот узел: статус или сетевая ошибка
		for si, svc := range rtEchoServices {
			if limited[svc] {
				continue
			}
			d, seen := directIP[svc]
			if !seen {
				var st int
				d, st, _ = rtFetchIP(direct, svc)
				if st == http.StatusTooManyRequests {
					limited[svc] = true
					continue
				}
				if d == "" {
					continue
				}
				directIP[svc] = d
			}
			n, st, errText := rtFetchIP(viaNode, svc)
			if st == http.StatusTooManyRequests {
				limited[svc] = true
				continue
			}
			if n == "" {
				if st != 0 {
					// узел ответил, но сервис не отдал адрес: пробуем следующий сервис
					attempts = append(attempts, fmt.Sprintf("сервис %d: HTTP %d", si+1, st))
					continue
				}
				attempts = append(attempts, fmt.Sprintf("сервис %d: %s", si+1, rtShortErr(redact(errText))))
				break // обрыв до узла или узел мёртв: к следующему узлу
			}
			nodeIP, usedDirect = n, d
			break
		}
		coreWhy := pr.Reasons()
		pr.Stop()
		_ = os.RemoveAll(dir)

		if nodeIP == "" {
			why := strings.Join(attempts, "; ")
			if r := coreWhy; r != "" {
				why += "; ядро: " + r
			}
			summaries = append(summaries, fmt.Sprintf("узел %d (%s): %s", i+1, protocol, why))
			t.Logf("узел %d из %d (протокол %s): запрос через узел не прошёл: %s", i+1, len(nodes), protocol, why)
			continue
		}
		same := usedDirect == nodeIP
		t.Logf("ядро %s, узел %d из %d, протокол %s: адреса различаются: %s", tg.Core, i+1, len(nodes), protocol, map[bool]string{true: "нет", false: "да"}[same])
		if same {
			t.Errorf("внешний адрес через узел совпал с прямым: трафик не ушёл через узел подписки (протокол %s)", protocol)
		}
		return
	}
	if len(limited) == len(rtEchoServices) {
		t.Fatal("все сервисы определения адреса ответили лимитом частоты (429)")
	}
	if startFailures > 0 {
		t.Fatalf("ядро не приняло конфиг узла у %d из %d узлов: %s", startFailures, len(nodes), startReason)
	}
	// Интернет у цели мог пропасть во время перебора узлов.
	if up, why := rtInternetUp(direct); !up {
		t.Skipf("выход цели в интернет пропал во время теста: прямые запросы не прошли (%s)", why)
	}
	t.Skipf("ни один из %d узлов подписки не ответил через ядро %s (ядро запускалось и принимало соединения, выход цели в интернет есть): %s",
		len(nodes), tg.Core, strings.Join(summaries, " | "))
}

// --- TPROXY ---

var (
	rtOnPortRe   = regexp.MustCompile(`-j TPROXY\b.*--on-port (\d+)`)
	rtToPortsRe  = regexp.MustCompile(`-j REDIRECT\b.*--to-ports? (\d+)`)
	rtChainNewRe = regexp.MustCompile(`(?m)^-N (\S+)`)
)

// rtActiveInboundPorts возвращает порты tproxy/redirect-входящих активного ядра из конфигов.
func rtActiveInboundPorts(t *testing.T, core string) map[int]bool {
	t.Helper()
	ports := map[int]bool{}
	switch core {
	case "xray":
		files, _ := filepath.Glob(filepath.Join(rtXrayConfDir, "*.json"))
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			var doc struct {
				Inbounds []struct {
					Tag  string `json:"tag"`
					Port any    `json:"port"`
				} `json:"inbounds"`
			}
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			for _, in := range doc.Inbounds {
				tag := strings.ToLower(in.Tag)
				if !strings.Contains(tag, "tproxy") && !strings.Contains(tag, "redirect") {
					continue
				}
				switch v := in.Port.(type) {
				case float64:
					ports[int(v)] = true
				case string:
					if p, err := strconv.Atoi(v); err == nil {
						ports[p] = true
					}
				}
			}
		}
	case "mihomo":
		raw, err := os.ReadFile(filepath.Join(rtMihomoDir, "config.yaml"))
		if err != nil {
			t.Fatalf("config.yaml Mihomo не читается: %v", err)
		}
		var cfg struct {
			Listeners []struct {
				Type string `yaml:"type"`
				Port int    `yaml:"port"`
			} `yaml:"listeners"`
			TProxyPort int `yaml:"tproxy-port"`
			RedirPort  int `yaml:"redir-port"`
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("config.yaml Mihomo не разбирается: %v", err)
		}
		for _, l := range cfg.Listeners {
			if l.Type == "tproxy" || l.Type == "redir" {
				ports[l.Port] = true
			}
		}
		if cfg.TProxyPort > 0 {
			ports[cfg.TProxyPort] = true
		}
		if cfg.RedirPort > 0 {
			ports[cfg.RedirPort] = true
		}
	}
	return ports
}

// rtListeningPorts возвращает порты, которые слушает система (TCP в состоянии LISTEN и
// привязанные UDP), по /proc/net.
func rtListeningPorts() map[int]bool {
	ports := map[int]bool{}
	for _, f := range []struct{ file, state string }{
		{"/proc/net/tcp", "0A"}, {"/proc/net/tcp6", "0A"},
		{"/proc/net/udp", "07"}, {"/proc/net/udp6", "07"},
	} {
		raw, err := os.ReadFile(f.file)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if i == 0 || len(fields) < 4 || fields[3] != f.state {
				continue
			}
			_, hexPort, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			if p, err := strconv.ParseUint(hexPort, 16, 32); err == nil {
				ports[int(p)] = true
			}
		}
	}
	return ports
}

func TestRouterTproxyRules(t *testing.T) {
	tg := routertest.Current(t)
	if tg.Core != "xray" && tg.Core != "mihomo" {
		t.Fatalf("активное ядро цели неизвестно: %q", tg.Core)
	}
	want := rtActiveInboundPorts(t, tg.Core)
	if len(want) == 0 {
		t.Fatalf("в активном конфиге ядра %s нет tproxy/redirect-входящих", tg.Core)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	// После перезапуска XKeen (предыдущие тесты) правила на медленном устройстве
	// появляются не сразу: ждём их до трёх минут, затем вердикт по тому, что есть.
	var rulePorts map[int]bool
	var chains []string
	deadline := time.Now().Add(3 * time.Minute)
	for {
		rulePorts = map[int]bool{}
		chains = nil
		for _, table := range []string{"mangle", "nat"} {
			out, stderr, code := routertest.Exec(ctx, "iptables", "-w", "-t", table, "-S")
			if code != 0 {
				t.Fatalf("iptables -w -t %s -S: код %d: %s", table, code, strings.TrimSpace(stderr))
			}
			for _, line := range strings.Split(out, "\n") {
				for _, re := range []*regexp.Regexp{rtOnPortRe, rtToPortsRe} {
					if m := re.FindStringSubmatch(line); m != nil {
						if p, err := strconv.Atoi(m[1]); err == nil {
							rulePorts[p] = true
						}
					}
				}
			}
			for _, m := range rtChainNewRe.FindAllStringSubmatch(out, -1) {
				if strings.Contains(strings.ToLower(m[1]), "xkeen") {
					chains = append(chains, table+" "+m[1])
				}
			}
		}
		if len(rulePorts) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if len(rulePorts) == 0 {
		t.Fatal("в iptables нет правил TPROXY/REDIRECT: XKeen не поставил перехват")
	}

	// Ядро после перезапуска XKeen открывает порты позже, чем встают правила: ждём
	// порты правил до трёх минут, затем вердикт по тому, что слушается.
	listening := rtListeningPorts()
	for end := time.Now().Add(3 * time.Minute); time.Now().Before(end); {
		all := true
		for p := range rulePorts {
			if !listening[p] {
				all = false
			}
		}
		if all {
			break
		}
		time.Sleep(2 * time.Second)
		listening = rtListeningPorts()
	}
	var matched int
	for p := range rulePorts {
		if want[p] {
			matched++
		}
		if !listening[p] {
			t.Errorf("правило XKeen ведёт на порт %d, который никто не слушает", p)
		}
	}
	if matched == 0 {
		t.Errorf("правила XKeen ведут на порты %v, ни один не совпал с входящими активного ядра %s", rtSortedPorts(rulePorts), tg.Core)
	}
	t.Logf("ядро %s: порты правил %v, порты входящих ядра %v, слушаются все порты правил: %v",
		tg.Core, rtSortedPorts(rulePorts), rtSortedPorts(want), rtAllListening(rulePorts, listening))

	// Счётчики пакетов цепочек XKeen: только числа.
	for _, ch := range chains {
		table, name, _ := strings.Cut(ch, " ")
		out, _, code := routertest.Exec(ctx, "iptables", "-w", "-t", table, "-L", name, "-v", "-x", "-n")
		if code != 0 {
			t.Errorf("iptables -t %s -L %s: код %d", table, name, code)
			continue
		}
		var pkts, bytesTotal, rules uint64
		for i, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if i < 2 || len(fields) < 2 {
				continue
			}
			p, err1 := strconv.ParseUint(fields[0], 10, 64)
			b, err2 := strconv.ParseUint(fields[1], 10, 64)
			if err1 != nil || err2 != nil {
				t.Errorf("счётчики цепочки не числа: %q", fmt.Sprint(fields[:2]))
				continue
			}
			pkts += p
			bytesTotal += b
			rules++
		}
		t.Logf("цепочка %s/%s: правил %d, пакетов %d, байт %d", table, name, rules, pkts, bytesTotal)
	}
}

func rtSortedPorts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func rtAllListening(rules, listening map[int]bool) bool {
	for p := range rules {
		if !listening[p] {
			return false
		}
	}
	return true
}
