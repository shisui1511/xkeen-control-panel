//go:build router

package routing

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gopkg.in/yaml.v3"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// Спайк фактов ядер для перехвата DNS (Wave 0 фазы): принимают ли Xray и Mihomo
// поля, на которые опирается генератор перехвата, идут ли апстримы через группу,
// отвечают ли локальные зоны при мёртвой группе, как Mihomo опрашивает список
// серверов и что делает ядро, когда порт 53 занят прошивкой. Факты печатаются
// строками `FACT <ключ>=<значение>` и тест не роняют; вердикт — процессы теста
// остановлены, процессы ядер стенда прежние, прошивка отвечает на 127.0.0.1:53.
//
// Метки ядра нет: тест запускает отдельные процессы обоих ядер в каталоге теста
// (DNS-слушатели на loopback) и не читает и не переключает активное ядро стенда.
// «Узел» — rtStartNode (Xray socks с журналом доступа): соединение DoH к адресу
// апстрима в его журнале значит, что запрос прошёл через группу. Перед
// измерениями с мёртвой группой процесс ядра перезапускается, чтобы ответ не
// пришёл из кеша. В журнал идут только да/нет, коды ответов и миллисекунды.

const (
	// rtdsExtAlive и rtdsExtDead — нейтральные внешние имена (разные, чтобы
	// второе не попало в кеш).
	rtdsExtAlive = "example.com."
	rtdsExtDead  = "example.net."
	rtdsDoH      = "https://1.1.1.1/dns-query"
	// rtdsDoHSeen — адрес апстрима в журнале доступа узла.
	rtdsDoHSeen = "1.1.1.1:443"

	rtdsGroupCyr        = "Прокси"
	rtdsGroupASCII      = "Proxy"
	rtdsNameserverCyr   = "https://1.1.1.1/dns-query#Прокси"
	rtdsNameserverASCII = "https://1.1.1.1/dns-query#Proxy"
	rtdsRuleSet         = "rt-local"

	// rtdsMihomoServerTimeout — таймаут одного сервера DNS у Mihomo по
	// умолчанию; меньше половины — список опрашивается параллельно.
	rtdsMihomoServerTimeout = 5 * time.Second
	rtdsFirmwareDNS         = "127.0.0.1:53"
)

var rtdsVendorRe = regexp.MustCompile(`(?mi)^\s*vendor:\s*([A-Za-z0-9]+)`)

// rtdsFact печатает факт спайка.
func rtdsFact(t *testing.T, key, value string) {
	t.Helper()
	t.Logf("FACT %s=%s", key, value)
}

func rtdsYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// rtdsResult — итог одного DNS-запроса.
type rtdsResult struct {
	answered bool
	rcode    dnsmessage.RCode
	answers  int
	elapsed  time.Duration
}

// resolved — ответ NOERROR хотя бы с одной записью.
func (r rtdsResult) resolved() bool {
	return r.answered && r.rcode == dnsmessage.RCodeSuccess && r.answers > 0
}

// code — код ответа строкой или timeout.
func (r rtdsResult) code() string {
	if !r.answered {
		return "timeout"
	}
	switch r.rcode {
	case dnsmessage.RCodeSuccess:
		if r.answers == 0 {
			return "noerror-empty"
		}
		return "noerror"
	case dnsmessage.RCodeNameError:
		return "nxdomain"
	case dnsmessage.RCodeServerFailure:
		return "servfail"
	case dnsmessage.RCodeRefused:
		return "refused"
	default:
		return "rcode" + strconv.Itoa(int(r.rcode))
	}
}

func (r rtdsResult) ms() string {
	return strconv.FormatInt(r.elapsed.Milliseconds(), 10)
}

// rtdsQuery отправляет один UDP-запрос A/NS имени name и ждёт ответ до timeout.
func rtdsQuery(addr, name string, qtype dnsmessage.Type, timeout time.Duration) rtdsResult {
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return rtdsResult{}
	}
	var idb [2]byte
	_, _ = rand.Read(idb[:])
	id := binary.BigEndian.Uint16(idb[:])
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: n, Type: qtype, Class: dnsmessage.ClassINET}},
	}
	packed, err := msg.Pack()
	if err != nil {
		return rtdsResult{}
	}
	start := time.Now()
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return rtdsResult{elapsed: time.Since(start)}
	}
	defer conn.Close()
	_ = conn.SetDeadline(start.Add(timeout))
	if _, err := conn.Write(packed); err != nil {
		return rtdsResult{elapsed: time.Since(start)}
	}
	buf := make([]byte, 4096)
	for {
		k, err := conn.Read(buf)
		if err != nil {
			return rtdsResult{elapsed: time.Since(start)}
		}
		var resp dnsmessage.Message
		if resp.Unpack(buf[:k]) != nil || resp.Header.ID != id || !resp.Header.Response {
			continue
		}
		res := rtdsResult{answered: true, rcode: resp.Header.RCode, elapsed: time.Since(start)}
		for _, a := range resp.Answers {
			if a.Header.Type == qtype {
				res.answers++
			}
		}
		return res
	}
}

// rtdsResolveWait повторяет запрос, пока имя не разрешится или не выйдет wait
// (слушатель DNS ядра и соединение DoH поднимаются не сразу).
func rtdsResolveWait(addr, name string, wait time.Duration) rtdsResult {
	end := time.Now().Add(wait)
	for {
		r := rtdsQuery(addr, name, dnsmessage.TypeA, 5*time.Second)
		if r.resolved() || time.Now().After(end) {
			return r
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// rtdsServiceDomain — служебный домен роутера по бренду прошивки
// («my.<бренд>.net.») или "", если бренд не определён.
func rtdsServiceDomain() string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, _, code := routertest.Exec(ctx, "ndmc", "-c", "show version")
	if code != 0 {
		return ""
	}
	m := rtdsVendorRe.FindStringSubmatch(out)
	if len(m) != 2 {
		return ""
	}
	return "my." + strings.ToLower(m[1]) + ".net."
}

// rtdsFirmwareAlive — прошивка отвечает на 127.0.0.1:53 (любой ответ), ждём до wait.
func rtdsFirmwareAlive(svc string, wait time.Duration) bool {
	name, qtype := ".", dnsmessage.TypeNS
	if svc != "" {
		name, qtype = svc, dnsmessage.TypeA
	}
	end := time.Now().Add(wait)
	for {
		if rtdsQuery(rtdsFirmwareDNS, name, qtype, 2*time.Second).answered {
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// rtdsCoreProcs делит процессы ядер (xray, mihomo) на процессы теста (командная
// строка содержит рабочий каталог теста) и процессы стенда («имя:pid» по
// возрастанию).
func rtdsCoreProcs(workDir string) (stand []string, testPIDs []int) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		if name != "xray" && name != "mihomo" {
			continue
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if workDir != "" && strings.Contains(string(cmdline), workDir) {
			testPIDs = append(testPIDs, pid)
			continue
		}
		stand = append(stand, name+":"+e.Name())
	}
	sort.Strings(stand)
	return stand, testPIDs
}

// rtdsStopNode останавливает «узел» rtStartNode по пути его конфига (каталог
// журнала доступа) и ждёт выхода процесса.
func rtdsStopNode(accessLog string) bool {
	cfg := filepath.Join(filepath.Dir(accessLog), "node.json")
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err == nil && strings.Contains(string(cmdline), cfg) {
			pids = append(pids, pid)
		}
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	end := time.Now().Add(15 * time.Second)
	for _, pid := range pids {
		for syscall.Kill(pid, 0) == nil && time.Now().Before(end) {
			time.Sleep(200 * time.Millisecond)
		}
	}
	return len(pids) > 0
}

// rtdsLaunch запускает процесс без ожидания порта (процесс может выйти сразу —
// это и есть измеряемый факт). Процесс останавливается в t.Cleanup.
func rtdsLaunch(t *testing.T, bin string, args, env []string) *rtProc {
	t.Helper()
	p := &rtProc{exited: make(chan struct{})}
	p.cmd = exec.Command(bin, args...)
	p.cmd.Stdout = p
	p.cmd.Stderr = p
	if env != nil {
		p.cmd.Env = env
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("процесс %s не запустился: %v", filepath.Base(bin), err)
	}
	go func() {
		_ = p.cmd.Wait()
		close(p.exited)
	}()
	t.Cleanup(p.stop)
	return p
}

// rtdsSettle ждёт, пока процесс выйдет или откроет порт готовности и проживёт
// ещё 3 с: exit | runs | timeout.
func rtdsSettle(p *rtProc, readyPort int, wait time.Duration) string {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(readyPort))
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		if !p.alive() {
			return "exit"
		}
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			_ = c.Close()
			time.Sleep(3 * time.Second)
			if !p.alive() {
				return "exit"
			}
			return "runs"
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "timeout"
}

// rtdsOwnsPort53 — держит ли процесс сокет на порту 53 (по inode из /proc/net).
func rtdsOwnsPort53(pid int) bool {
	fds, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return false
	}
	inodes := map[string]bool{}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", fd.Name()))
		if err == nil && strings.HasPrefix(link, "socket:[") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")] = true
		}
	}
	for _, proto := range []string{"udp", "udp6", "tcp", "tcp6"} {
		data, err := os.ReadFile("/proc/net/" + proto)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || !inodes[f[9]] {
				continue
			}
			if _, port, ok := strings.Cut(f[1], ":"); ok && strings.EqualFold(port, "0035") {
				return true
			}
		}
	}
	return false
}

// rtdsWriteJSON пишет конфиг Xray в каталог.
func rtdsWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rtdsWriteYAML пишет конфиг Mihomo в каталог.
func rtdsWriteYAML(t *testing.T, path string, v any) {
	t.Helper()
	data, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// rtdsCheck — проверка конфига ядром: пусто — принят, иначе первая строка
// ошибки без адресов.
func rtdsCheck(bin string, args []string, env []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(string(out))
	for _, line := range strings.Split(text, "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "error") || strings.Contains(low, "fail") || strings.Contains(low, "invalid") {
			return rtRedact(line)
		}
	}
	if text == "" {
		return rtRedact(err.Error())
	}
	line, _, _ := strings.Cut(text, "\n")
	return rtRedact(line)
}

// rtdsAccepted — значение факта «ядро приняло конфиг».
func rtdsAccepted(errLine string) string {
	if errLine == "" {
		return "yes"
	}
	return "no: " + errLine
}

// rtdsXrayConfig — конфиг Xray с тегами перехвата из пространства панели
// (как их выпустит генератор): DNS-входящий без прозрачного перенаправления,
// исходящий dns, прямой исходящий с меткой 255 и группа — socks на порт узла.
func rtdsXrayConfig(readyPort, dnsPort, nodePort int, localDomain string) map[string]any {
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"dns": map[string]any{
			"tag": "xcp-dns",
			"servers": []any{
				map[string]any{
					"address":      "127.0.0.1",
					"port":         53,
					"domains":      []string{"full:" + localDomain},
					"tag":          "xcp-dns-direct",
					"finalQuery":   true,
					"skipFallback": true,
				},
				rtdsDoH,
			},
		},
		"inbounds": []any{
			map[string]any{
				"tag": "rt-in", "listen": "127.0.0.1", "port": readyPort,
				"protocol": "socks", "settings": map[string]any{"udp": false},
			},
			map[string]any{
				"tag": "xcp-dns-in", "listen": "127.0.0.1", "port": dnsPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{"address": "1.1.1.1", "port": 53, "network": "tcp,udp"},
			},
		},
		"outbounds": []any{
			map[string]any{"tag": "xcp-block", "protocol": "blackhole"},
			map[string]any{"tag": "xcp-dns-out", "protocol": "dns"},
			map[string]any{
				"tag": "xcp-direct", "protocol": "freedom",
				"streamSettings": map[string]any{"sockopt": map[string]any{"mark": 255}},
			},
			map[string]any{
				"tag": "xcp-g-rt", "protocol": "socks",
				"settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": nodePort}}},
			},
		},
		"routing": map[string]any{
			"rules": []any{
				map[string]any{"inboundTag": []string{"xcp-dns-in"}, "outboundTag": "xcp-dns-out"},
				map[string]any{"inboundTag": []string{"xcp-dns"}, "outboundTag": "xcp-g-rt"},
				map[string]any{"inboundTag": []string{"xcp-dns-direct"}, "outboundTag": "xcp-direct"},
			},
		},
	}
}

// rtdsMihomoConfig — конфиг Mihomo: socks готовности, DNS на loopback, группа
// select с единственным участником — узлом, набор правил с локальным доменом.
func rtdsMihomoConfig(readyPort, dnsPort, nodePort int, group, nameserver string, policy map[string]any) map[string]any {
	return map[string]any{
		"socks-port":   readyPort,
		"bind-address": "127.0.0.1",
		"allow-lan":    false,
		"mode":         "rule",
		"log-level":    "warning",
		"proxies": []any{map[string]any{
			"name": "rt-node", "type": "socks5", "server": "127.0.0.1", "port": nodePort,
		}},
		"proxy-groups": []any{map[string]any{
			"name": group, "type": "select", "proxies": []string{"rt-node"},
		}},
		"rule-providers": map[string]any{
			rtdsRuleSet: map[string]any{"type": "file", "behavior": "domain", "path": "./" + rtdsRuleSet + ".yaml"},
		},
		"rules": []string{"RULE-SET," + rtdsRuleSet + ",DIRECT", "MATCH," + group},
		"dns": map[string]any{
			"enable":                  true,
			"listen":                  net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort)),
			"ipv6":                    false,
			"enhanced-mode":           "redir-host",
			"nameserver":              []string{nameserver},
			"proxy-server-nameserver": []string{rtdsDoH},
			"nameserver-policy":       policy,
		},
	}
}

// rtdsMihomoDir готовит каталог Mihomo: config.yaml и файл набора правил с
// локальным доменом.
func rtdsMihomoDir(t *testing.T, cfg map[string]any, localDomain string) string {
	t.Helper()
	dir := rtWorkSubdir(t, "rt-dns-m-")
	rtdsWriteYAML(t, filepath.Join(dir, rtdsRuleSet+".yaml"), map[string]any{"payload": []string{localDomain}})
	rtdsWriteYAML(t, filepath.Join(dir, "config.yaml"), cfg)
	return dir
}

// rtdsSilentUDP открывает «немой» UDP-сокет: читает запросы и не отвечает.
func rtdsSilentUDP(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("немой UDP-сокет: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func TestRouterDNSCoreFacts(t *testing.T) {
	work := routertest.Current(t).WorkDir
	standBefore, _ := rtdsCoreProcs(work)
	svc := rtdsServiceDomain()
	rtdsFact(t, "svc_domain", rtdsYesNo(svc != ""))
	// localDomain — домен локальной зоны в конфигах (без точки в конце); без
	// бренда — заведомо несуществующее имя, факты локального пути — none.
	localDomain := "rt-local.invalid"
	if svc != "" {
		localDomain = strings.TrimSuffix(svc, ".")
	}

	t.Run("xray", func(t *testing.T) {
		rtNeedBins(t, rtXrayBin)
		nodePort, accessLog := rtStartNode(t)
		readyPort, dnsPort := rtFreePort(t), rtFreePort(t)
		dir := rtWorkSubdir(t, "rt-dns-x-")
		rtdsWriteJSON(t, filepath.Join(dir, "config.json"), rtdsXrayConfig(readyPort, dnsPort, nodePort, localDomain))
		env := utils.XrayAssetEnv(os.Environ(), "")

		errLine := rtdsCheck(rtXrayBin, []string{"run", "-test", "-confdir", dir}, env)
		rtdsFact(t, "xray_fields_ok", rtdsAccepted(errLine))
		if errLine != "" {
			return
		}
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))
		p := rtStartXrayDir(t, dir, readyPort)
		ext := rtdsResolveWait(addr, rtdsExtAlive, 20*time.Second)
		saw := rtWaitNodeSaw(accessLog, rtdsDoHSeen, 5*time.Second)
		rtdsFact(t, "xray_ext_alive", ext.code()+"/"+ext.ms())
		rtdsFact(t, "xray_via_group", rtdsYesNo(ext.resolved() && saw))
		if svc != "" {
			rtdsFact(t, "xray_direct_ok", rtdsYesNo(rtdsQuery(addr, svc, dnsmessage.TypeA, 5*time.Second).resolved()))
		} else {
			rtdsFact(t, "xray_direct_ok", "none")
		}

		// Мёртвая группа: узел остановлен, ядро перезапущено (пустой кеш).
		rtdsFact(t, "xray_node_stopped", rtdsYesNo(rtdsStopNode(accessLog)))
		p.stop()
		rtStartXrayDir(t, dir, readyPort)
		dead := rtdsQuery(addr, rtdsExtDead, dnsmessage.TypeA, 10*time.Second)
		rtdsFact(t, "xray_dead_ext", dead.code()+"/"+dead.ms())
		if svc != "" {
			local := rtdsQuery(addr, svc, dnsmessage.TypeA, 10*time.Second)
			if local.resolved() {
				rtdsFact(t, "xray_dead_local", local.ms())
			} else {
				rtdsFact(t, "xray_dead_local", "fail/"+local.code())
			}
		} else {
			rtdsFact(t, "xray_dead_local", "none")
		}
		rtdsFact(t, "xray_dead_leak", rtdsYesNo(dead.resolved()))
	})

	t.Run("mihomo", func(t *testing.T) {
		rtNeedBins(t, rtMihomoBin)

		// Кириллическое имя группы в суффиксе DoH и rule-set в nameserver-policy.
		nodePort, accessLog := rtStartNode(t)
		readyPort, dnsPort := rtFreePort(t), rtFreePort(t)
		policy := map[string]any{"+.lan": "127.0.0.1", "rule-set:" + rtdsRuleSet: "127.0.0.1"}
		dir := rtdsMihomoDir(t, rtdsMihomoConfig(readyPort, dnsPort, nodePort, rtdsGroupCyr, rtdsNameserverCyr, policy), localDomain)
		cfgPath := filepath.Join(dir, "config.yaml")
		errLine := rtdsCheck(rtMihomoBin, []string{"-t", "-d", dir, "-f", cfgPath}, nil)
		rtdsFact(t, "mihomo_suffix_cyr_ok", rtdsAccepted(errLine))
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))
		if errLine == "" {
			p := rtStartMihomoCore(t, dir, readyPort)
			ext := rtdsResolveWait(addr, rtdsExtAlive, 20*time.Second)
			saw := rtWaitNodeSaw(accessLog, rtdsDoHSeen, 5*time.Second)
			rtdsFact(t, "mihomo_ext_alive", ext.code()+"/"+ext.ms())
			rtdsFact(t, "mihomo_via_group", rtdsYesNo(ext.resolved() && saw))
			if svc != "" {
				rtdsFact(t, "mihomo_policy_ruleset_ok", rtdsYesNo(rtdsResolveWait(addr, svc, 10*time.Second).resolved()))
			} else {
				rtdsFact(t, "mihomo_policy_ruleset_ok", "none")
			}

			// Мёртвая группа: узел остановлен, ядро перезапущено (пустой кеш).
			rtdsFact(t, "mihomo_node_stopped", rtdsYesNo(rtdsStopNode(accessLog)))
			p.stop()
			rtStartMihomoCore(t, dir, readyPort)
			time.Sleep(time.Second)
			dead := rtdsQuery(addr, rtdsExtDead, dnsmessage.TypeA, 10*time.Second)
			rtdsFact(t, "mihomo_dead_ext", dead.code()+"/"+dead.ms())
			if svc != "" {
				local := rtdsQuery(addr, svc, dnsmessage.TypeA, 10*time.Second)
				if local.resolved() {
					rtdsFact(t, "mihomo_dead_local", local.ms())
				} else {
					rtdsFact(t, "mihomo_dead_local", "fail/"+local.code())
				}
			} else {
				rtdsFact(t, "mihomo_dead_local", "none")
			}
			rtdsFact(t, "mihomo_dead_leak", rtdsYesNo(dead.resolved()))
		}

		// ASCII-имя группы и порядок опроса списка серверов: первый — немой сокет.
		nodePort, accessLog = rtStartNode(t)
		readyPort, dnsPort = rtFreePort(t), rtFreePort(t)
		silent := rtdsSilentUDP(t)
		listPolicy := map[string]any{}
		if svc != "" {
			listPolicy[localDomain] = []string{"udp://127.0.0.1:" + strconv.Itoa(silent), "udp://127.0.0.1"}
		}
		dir = rtdsMihomoDir(t, rtdsMihomoConfig(readyPort, dnsPort, nodePort, rtdsGroupASCII, rtdsNameserverASCII, listPolicy), localDomain)
		cfgPath = filepath.Join(dir, "config.yaml")
		errLine = rtdsCheck(rtMihomoBin, []string{"-t", "-d", dir, "-f", cfgPath}, nil)
		rtdsFact(t, "mihomo_suffix_ascii_ok", rtdsAccepted(errLine))
		if errLine != "" {
			return
		}
		addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(dnsPort))
		rtStartMihomoCore(t, dir, readyPort)
		ext := rtdsResolveWait(addr, rtdsExtAlive, 20*time.Second)
		rtdsFact(t, "mihomo_via_group_ascii", rtdsYesNo(ext.resolved() && rtWaitNodeSaw(accessLog, rtdsDoHSeen, 5*time.Second)))
		if svc == "" {
			rtdsFact(t, "mihomo_list_mode", "none")
			return
		}
		r := rtdsQuery(addr, svc, dnsmessage.TypeA, 3*rtdsMihomoServerTimeout)
		switch {
		case !r.answered:
			rtdsFact(t, "mihomo_list_ms", "timeout")
			rtdsFact(t, "mihomo_list_mode", "unknown")
		case r.elapsed < rtdsMihomoServerTimeout/2:
			rtdsFact(t, "mihomo_list_ms", r.ms())
			rtdsFact(t, "mihomo_list_mode", "parallel")
		default:
			rtdsFact(t, "mihomo_list_ms", r.ms())
			rtdsFact(t, "mihomo_list_mode", "sequential")
		}
	})

	t.Run("bind53", func(t *testing.T) {
		// Ядро пытается занять :53, пока его держит прошивка; резолвер прошивки
		// не трогается, процесс останавливается сразу после измерения.
		if _, err := os.Stat(rtXrayBin); err == nil {
			readyPort := rtFreePort(t)
			dir := rtWorkSubdir(t, "rt-dns-b-")
			rtdsWriteJSON(t, filepath.Join(dir, "config.json"), map[string]any{
				"log": map[string]any{"loglevel": "warning"},
				"inbounds": []any{
					map[string]any{
						"tag": "rt-in", "listen": "127.0.0.1", "port": readyPort,
						"protocol": "socks", "settings": map[string]any{"udp": false},
					},
					map[string]any{
						"tag": "xcp-dns-in", "listen": "0.0.0.0", "port": 53,
						"protocol": "dokodemo-door",
						"settings": map[string]any{"address": "1.1.1.1", "port": 53, "network": "tcp,udp"},
					},
				},
				"outbounds": []any{map[string]any{"tag": "xcp-block", "protocol": "blackhole"}},
			})
			p := rtdsLaunch(t, rtXrayBin, []string{"run", "-confdir", dir}, utils.XrayAssetEnv(os.Environ(), ""))
			state := rtdsSettle(p, readyPort, 30*time.Second)
			p.stop()
			rtdsFact(t, "xray_bind53_conflict", state)
		} else {
			rtdsFact(t, "xray_bind53_conflict", "none")
		}

		if _, err := os.Stat(rtMihomoBin); err == nil {
			readyPort := rtFreePort(t)
			dir := rtWorkSubdir(t, "rt-dns-b-")
			rtdsWriteYAML(t, filepath.Join(dir, "config.yaml"), map[string]any{
				"socks-port":   readyPort,
				"bind-address": "127.0.0.1",
				"allow-lan":    false,
				"mode":         "direct",
				"log-level":    "warning",
				"dns": map[string]any{
					"enable":     true,
					"listen":     "0.0.0.0:53",
					"ipv6":       false,
					"nameserver": []string{"127.0.0.1"},
				},
			})
			p := rtdsLaunch(t, rtMihomoBin, []string{"-d", dir, "-f", filepath.Join(dir, "config.yaml")}, nil)
			state := rtdsSettle(p, readyPort, 30*time.Second)
			if state == "runs" {
				state = "runs_without_dns"
				if rtdsOwnsPort53(p.cmd.Process.Pid) {
					state = "runs_with_dns"
				}
			}
			p.stop()
			rtdsFact(t, "mihomo_bind53_conflict", state)
		} else {
			rtdsFact(t, "mihomo_bind53_conflict", "none")
		}
	})

	// Вердикт: стенд не тронут.
	standAfter, testLeft := rtdsCoreProcs(work)
	routertest.Verdict(t, len(testLeft) == 0, routertest.KnownMark{},
		"процессы ядер теста не остановлены: %d", len(testLeft))
	routertest.Verdict(t, strings.Join(standAfter, ",") == strings.Join(standBefore, ","), routertest.KnownMark{},
		"процессы ядер стенда изменились: было %d, стало %d", len(standBefore), len(standAfter))
	routertest.Verdict(t, rtdsFirmwareAlive(svc, 15*time.Second), routertest.KnownMark{},
		"прошивка не отвечает на 127.0.0.1:53 после теста")
}
