//go:build router

// router-area: dns

package dnstakeover

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/shisui1511/xkeen-control-panel/internal/routertest"
	"github.com/shisui1511/xkeen-control-panel/internal/services"
	"github.com/shisui1511/xkeen-control-panel/internal/utils/xtables"
)

// Спайк фактов прошивки NDMS для перехвата DNS (Wave 0 фазы): кто держит порт 53
// до и после `opkg dns-override`, где после этого отвечает резолвер прошивки,
// что делает цепочка перенаправления DNS клиентов политик, как прошивка
// возвращает порт и как XKeen меняет proxy_dns. Факты печатаются строками
// `FACT <ключ>=<значение>` и в вердикт не входят; вердикт — стенд возвращён:
// override и proxy_dns равны исходным, прошивка отвечает на 127.0.0.1:53, PID
// ядер стенда прежние (или их смену объясняет факт переключения proxy_dns).
//
// Метки ядра нет: тест меряет прошивку (ndmc, /proc, iptables) и init-скрипт
// XKeen, активное ядро стенда не читает и не переключает; процессы ядер
// сверяются только как «не изменились».
//
// Команды прошивки — константные строки через ndmc без оболочки. Команда
// сохранения конфигурации прошивки не вызывается: override живёт только в
// рабочей конфигурации. В журнал не попадают адреса, имена клиентов и сырые
// правила: только да/нет, числа, порты и имена процессов.

const (
	fwCmdShowVersion = "show version"
	fwCmdRunning     = "show running-config"
	fwCmdStartup     = "show startup-config"
	fwCmdOverrideOn  = "opkg dns-override"
	fwCmdOverrideOff = "no opkg dns-override"

	fwXKeenBin    = "/opt/sbin/xkeen"
	fwXKeenInit   = "/opt/etc/init.d/S05xkeen"
	fwRedirChain  = "_NDM_HOTSPOT_DNSREDIR"
	fwHotspotRCI  = "http://127.0.0.1:79/rci/show/ip/hotspot"
	fwLoopbackDNS = "127.0.0.1:53"

	// fwRebindLimit — сколько ждём, пока прошивка снова ответит на :53.
	fwRebindLimit = 30 * time.Second
	// fwPortsLimit — сколько портов резолвера прошивки опрашиваем (каждый — до 2 с).
	fwPortsLimit = 8
)

var (
	fwVendorRe   = regexp.MustCompile(`(?mi)^\s*vendor:\s*(\S+)`)
	fwVendorSafe = regexp.MustCompile(`[^A-Za-z0-9]+`)
	fwProxyDNSRe = regexp.MustCompile(`(?m)^\s*proxy_dns\s*=\s*["']?([A-Za-z]+)`)
	fwToPortsRe  = regexp.MustCompile(`--to-ports\s+(\S+)`)
	fwDport53Re  = regexp.MustCompile(`--dports?\s+(?:\S*[,:])?53(?:[,:]\S*)?(?:\s|$)`)
	fwIfaceRe    = regexp.MustCompile(`(?:^|\s)-i\s+(\S+)`)
	fwLabelRe    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	fwIPv4Re     = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	fwIPv6Re     = regexp.MustCompile(`\b[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{0,4}){2,7}\b`)
)

// fwFact печатает факт спайка.
func fwFact(t *testing.T, key, value string) {
	t.Helper()
	t.Logf("FACT %s=%s", key, value)
}

func fwYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func fwOkFail(v bool) string {
	if v {
		return "ok"
	}
	return "fail"
}

func fwOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// fwRedact скрывает адреса в выводе команды и оставляет первую строку не длиннее 160 знаков.
func fwRedact(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	line = fwIPv4Re.ReplaceAllStringFunc(line, func(ip string) string {
		if ip == "127.0.0.1" {
			return ip
		}
		return "x.x.x.x"
	})
	line = fwIPv6Re.ReplaceAllString(line, "x:x::x")
	line = strings.Join(strings.Fields(line), " ")
	if r := []rune(line); len(r) > 160 {
		line = string(r[:160]) + " …"
	}
	return fwOrNone(line)
}

// fwNdmc выполняет константную команду прошивки без оболочки.
func fwNdmc(cmd string) (out string, code int) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stdout, stderr, code := routertest.Exec(ctx, "ndmc", "-c", cmd)
	return stdout + stderr, code
}

// fwXKeen выполняет xkeen с аргументами-константами (переключение proxy_dns может
// перезапустить ядро, поэтому предел длинный).
func fwXKeen(args ...string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	_, _, code := routertest.Exec(ctx, fwXKeenBin, args...)
	return code
}

// fwVendor — бренд прошивки из `show version` (только буквы и цифры).
func fwVendor(out string) string {
	m := fwVendorRe.FindStringSubmatch(out)
	if len(m) != 2 {
		return ""
	}
	return fwVendorSafe.ReplaceAllString(m[1], "")
}

// fwOverrideState читает override из рабочей конфигурации прошивки. Строка
// команды бывает плоской («opkg dns-override») или вложенной в раздел opkg
// («dns-override»): засчитываются обе. lines — сколько строк упоминают override.
func fwOverrideState() (state, readVia string, lines int) {
	out, code := fwNdmc(fwCmdRunning)
	if code != 0 || strings.TrimSpace(out) == "" {
		return "unknown", "none", 0
	}
	state = "off"
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if !strings.Contains(l, "dns-override") {
			continue
		}
		lines++
		if l == fwCmdOverrideOn || l == "dns-override" {
			state = "on"
		}
	}
	return state, "running-config", lines
}

// fwStartup читает сохранённую конфигурацию прошивки: хеш содержимого (для
// сравнения, в журнал не идёт) и есть ли в ней override. Команда без поддержки —
// read=unsupported.
func fwStartup() (hash, read, override string) {
	out, code := fwNdmc(fwCmdStartup)
	low := strings.ToLower(out)
	if code != 0 || strings.TrimSpace(out) == "" || strings.Contains(low, "error[") || strings.Contains(low, "no such command") {
		return "", "unsupported", "unknown"
	}
	sum := sha256.Sum256([]byte(out))
	override = "off"
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == fwCmdOverrideOn || l == "dns-override" {
			override = "on"
		}
	}
	return hex.EncodeToString(sum[:]), "ok", override
}

// fwProxyDNS читает proxy_dns из init-скрипта XKeen.
func fwProxyDNS() string {
	data, err := os.ReadFile(fwXKeenInit)
	if err != nil {
		return "unknown"
	}
	m := fwProxyDNSRe.FindSubmatch(data)
	if m == nil {
		return "unknown"
	}
	switch v := strings.ToLower(string(m[1])); v {
	case "on", "off":
		return v
	default:
		return "unknown"
	}
}

// fwComm — имя процесса по PID.
func fwComm(pid string) string {
	data, err := os.ReadFile(filepath.Join("/proc", pid, "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// fwPIDs — числовые каталоги /proc.
func fwPIDs() []string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() {
			pids = append(pids, e.Name())
		}
	}
	return pids
}

// fwCorePIDs — процессы ядер («имя:pid» по возрастанию) или none.
func fwCorePIDs() string {
	var out []string
	for _, pid := range fwPIDs() {
		if c := fwComm(pid); c == "xray" || c == "mihomo" {
			out = append(out, c+":"+pid)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ",")
}

// fwCoreNames — только имена ядер из fwCorePIDs.
func fwCoreNames(pids string) string {
	if pids == "none" {
		return pids
	}
	var names []string
	for _, p := range strings.Split(pids, ",") {
		n, _, _ := strings.Cut(p, ":")
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// fwSock — сокет из /proc/net: слушающий TCP или несвязанный UDP.
type fwSock struct {
	proto string // udp, udp6, tcp, tcp6
	ip    net.IP
	port  int
	inode string
}

// scope — класс адреса сокета без самого адреса.
func (s fwSock) scope() string {
	switch {
	case s.ip.IsUnspecified():
		return "any"
	case s.ip.IsLoopback():
		return "loopback"
	default:
		return "other"
	}
}

// queryAddr — адрес, на который тест шлёт запрос этому сокету.
func (s fwSock) queryAddr() string {
	ip := s.ip
	if ip.IsUnspecified() {
		if strings.HasSuffix(s.proto, "6") {
			ip = net.IPv6loopback
		} else {
			ip = net.IPv4(127, 0, 0, 1)
		}
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(s.port))
}

// fwParseHexAddr разбирает адрес вида 0100007F:0035 из /proc/net (слова по 4
// байта в порядке хоста, little-endian на целевых платформах).
func fwParseHexAddr(s string) (net.IP, int, bool) {
	host, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return nil, 0, false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return nil, 0, false
	}
	raw, err := hex.DecodeString(host)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return nil, 0, false
	}
	ip := make(net.IP, len(raw))
	for i := 0; i < len(raw); i += 4 {
		binary.BigEndian.PutUint32(ip[i:], binary.LittleEndian.Uint32(raw[i:]))
	}
	return ip, int(port), true
}

// fwSockets читает /proc/net/{udp,udp6,tcp,tcp6} и владельцев сокетов
// (inode → имя процесса через /proc/<pid>/fd).
func fwSockets() ([]fwSock, map[string]string) {
	var socks []fwSock
	for _, proto := range []string{"udp", "udp6", "tcp", "tcp6"} {
		data, err := os.ReadFile("/proc/net/" + proto)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if i == 0 || len(f) < 10 {
				continue
			}
			ip, port, ok := fwParseHexAddr(f[1])
			if !ok {
				continue
			}
			_, remPort, _ := fwParseHexAddr(f[2])
			if strings.HasPrefix(proto, "tcp") && f[3] != "0A" {
				continue // только LISTEN
			}
			if strings.HasPrefix(proto, "udp") && remPort != 0 {
				continue // связанный исходящий сокет
			}
			socks = append(socks, fwSock{proto: proto, ip: ip, port: port, inode: f[9]})
		}
	}
	owner := map[string]string{}
	for _, pid := range fwPIDs() {
		fds, err := os.ReadDir(filepath.Join("/proc", pid, "fd"))
		if err != nil {
			continue
		}
		comm := ""
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join("/proc", pid, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if _, seen := owner[inode]; seen {
				continue
			}
			if comm == "" {
				comm = fwComm(pid)
			}
			owner[inode] = comm
		}
	}
	return socks, owner
}

// fwDescribe53 — кто держит :53: «comm/proto:scope» через запятую (или none) и
// имена процессов-владельцев по порядку появления.
func fwDescribe53(socks []fwSock, owner map[string]string) (string, []string) {
	set := map[string]bool{}
	var comms []string
	seenComm := map[string]bool{}
	for _, s := range socks {
		if s.port != 53 {
			continue
		}
		c := fwOrNone(owner[s.inode])
		set[c+"/"+s.proto+":"+s.scope()] = true
		if !seenComm[c] {
			seenComm[c] = true
			comms = append(comms, c)
		}
	}
	if len(set) == 0 {
		return "none", nil
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ","), comms
}

// fwPortsFact печатает порты процесса comm (listKey) и для каждого UDP-порта —
// разрешает ли он служебный домен (и имя клиента LAN, если withClient).
func fwPortsFact(t *testing.T, prefix, listKey, comm, svc, client string, withClient bool) {
	t.Helper()
	if comm == "" {
		fwFact(t, listKey, "none")
		return
	}
	socks, owner := fwSockets()
	listSet := map[string]bool{}
	byPort := map[int][]fwSock{}
	var udpPorts []int
	for _, s := range socks {
		if owner[s.inode] != comm {
			continue
		}
		listSet[strconv.Itoa(s.port)+"/"+s.proto] = true
		if strings.HasPrefix(s.proto, "udp") {
			if _, seen := byPort[s.port]; !seen {
				udpPorts = append(udpPorts, s.port)
			}
			byPort[s.port] = append(byPort[s.port], s)
		}
	}
	var list []string
	for k := range listSet {
		list = append(list, k)
	}
	sort.Strings(list)
	fwFact(t, listKey, fwOrNone(strings.Join(list, ",")))
	sort.Ints(udpPorts)
	if len(udpPorts) > fwPortsLimit {
		fwFact(t, prefix+"_ports_truncated", strconv.Itoa(len(udpPorts)))
		udpPorts = udpPorts[:fwPortsLimit]
	}
	for _, port := range udpPorts {
		key := prefix + "_port_" + strconv.Itoa(port)
		if svc != "" {
			ok := false
			for _, s := range byPort[port] {
				if fwQuery(s.queryAddr(), svc, dnsmessage.TypeA, 2*time.Second).resolved() {
					ok = true
					break
				}
			}
			fwFact(t, key+"_svc", fwOkFail(ok))
		} else {
			fwFact(t, key+"_svc", "none")
		}
		if !withClient {
			continue
		}
		if client == "" {
			fwFact(t, key+"_client", "none")
			continue
		}
		ok := false
		for _, s := range byPort[port] {
			if fwQuery(s.queryAddr(), client, dnsmessage.TypeA, 2*time.Second).resolved() {
				ok = true
				break
			}
		}
		fwFact(t, key+"_client", fwOkFail(ok))
	}
}

// fwDNSResult — итог одного DNS-запроса.
type fwDNSResult struct {
	answered bool
	rcode    dnsmessage.RCode
	answers  int
	elapsed  time.Duration
}

// resolved — ответ NOERROR хотя бы с одной записью.
func (r fwDNSResult) resolved() bool {
	return r.answered && r.rcode == dnsmessage.RCodeSuccess && r.answers > 0
}

// fwQuery отправляет один UDP-запрос имени name и ждёт ответ до timeout.
func fwQuery(addr, name string, qtype dnsmessage.Type, timeout time.Duration) fwDNSResult {
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	n, err := dnsmessage.NewName(name)
	if err != nil {
		return fwDNSResult{}
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
		return fwDNSResult{}
	}
	start := time.Now()
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		return fwDNSResult{elapsed: time.Since(start)}
	}
	defer conn.Close()
	_ = conn.SetDeadline(start.Add(timeout))
	if _, err := conn.Write(packed); err != nil {
		return fwDNSResult{elapsed: time.Since(start)}
	}
	buf := make([]byte, 4096)
	for {
		k, err := conn.Read(buf)
		if err != nil {
			return fwDNSResult{elapsed: time.Since(start)}
		}
		var resp dnsmessage.Message
		if resp.Unpack(buf[:k]) != nil || resp.Header.ID != id || !resp.Header.Response {
			continue
		}
		res := fwDNSResult{answered: true, rcode: resp.Header.RCode, elapsed: time.Since(start)}
		for _, a := range resp.Answers {
			if a.Header.Type == qtype {
				res.answers++
			}
		}
		return res
	}
}

// fwAlive — прошивка отвечает на 127.0.0.1:53: служебный домен разрешается (или,
// без бренда, на корневой запрос приходит любой ответ).
func fwAlive(svc string) bool {
	if svc != "" {
		return fwQuery(fwLoopbackDNS, svc, dnsmessage.TypeA, 2*time.Second).resolved()
	}
	return fwQuery(fwLoopbackDNS, ".", dnsmessage.TypeNS, 2*time.Second).answered
}

// fwWaitAlive ждёт ответа прошивки на 127.0.0.1:53 до limit; возвращает время.
func fwWaitAlive(svc string, limit time.Duration) (time.Duration, bool) {
	start := time.Now()
	for {
		if fwAlive(svc) {
			return time.Since(start), true
		}
		if time.Since(start) > limit {
			return time.Since(start), false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// fwClientLabel — имя первого клиента LAN из RCI (одна метка DNS) или "". Имя в
// журнал не идёт.
func fwClientLabel() string {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(fwHotspotRCI)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ""
	}
	clients, err := services.ParseRCIHotspotJSON(data)
	if err != nil {
		return ""
	}
	var macs []string
	for mac := range clients {
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	pick := func(active bool) string {
		for _, mac := range macs {
			c := clients[mac]
			if c.Active != active {
				continue
			}
			for _, n := range []string{strings.TrimSpace(c.Hostname), strings.TrimSpace(c.Name)} {
				if fwLabelRe.MatchString(n) {
					return n
				}
			}
		}
		return ""
	}
	if n := pick(true); n != "" {
		return n
	}
	return pick(false)
}

// fwIPv6Facts — есть ли у роутера глобальный (2000::/3) и уникальный локальный
// (fc00::/7) адрес IPv6; сами адреса не печатаются.
func fwIPv6Facts(t *testing.T) {
	t.Helper()
	global, ula := false, false
	if data, err := os.ReadFile("/proc/net/if_inet6"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 6 || len(f[0]) != 32 {
				continue
			}
			switch first := f[0][0]; {
			case first == '2' || first == '3':
				global = true
			case f[0][:2] == "fc" || f[0][:2] == "fd":
				ula = true
			}
		}
		fwFact(t, "ipv6_global", fwYesNo(global))
		fwFact(t, "ipv6_ula", fwYesNo(ula))
	} else {
		fwFact(t, "ipv6_global", "unknown")
	}
	if data, err := os.ReadFile("/proc/sys/net/ipv6/bindv6only"); err == nil {
		fwFact(t, "bindv6only", strings.TrimSpace(string(data)))
	} else {
		fwFact(t, "bindv6only", "unknown")
	}
}

// fwTables выполняет iptables/ip6tables с диалектом -w этого бинарника.
func fwTables(bin string, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	full := append(xtables.WaitArgsFor(ctx, bin), args...)
	out, _, code := routertest.Exec(ctx, bin, full...)
	return out, code
}

// fwRedir — сводка цепочки nat перенаправления DNS: absent или
// «<число правил>:<порты --to-ports через запятую или ->».
func fwRedir(bin string) string {
	out, code := fwTables(bin, "-t", "nat", "-S", fwRedirChain)
	if code != 0 {
		return "absent"
	}
	rules := 0
	ports := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "-A ") {
			continue
		}
		rules++
		if m := fwToPortsRe.FindStringSubmatch(l); m != nil {
			ports[m[1]] = true
		}
	}
	var list []string
	for p := range ports {
		list = append(list, p)
	}
	sort.Strings(list)
	if len(list) == 0 {
		list = []string{"-"}
	}
	return strconv.Itoa(rules) + ":" + strings.Join(list, ",")
}

// fwWAN53Facts — закрыт ли :53 для не-LAN интерфейсов фильтром прошивки. Правила
// не печатаются: только политика INPUT и числа.
func fwWAN53Facts(t *testing.T) {
	t.Helper()
	out, code := fwTables("iptables", "-S")
	if code != 0 {
		fwFact(t, "wan53_filtered", "unknown")
		return
	}
	policy := "unknown"
	drops, dport53, dport53Drop := 0, 0, false
	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(l, "-P INPUT "); ok {
			policy = strings.TrimSpace(rest)
			continue
		}
		if !strings.HasPrefix(l, "-A ") {
			continue
		}
		isDrop := strings.Contains(l, "-j DROP") || strings.Contains(l, "-j REJECT")
		if isDrop {
			drops++
		}
		if !fwDport53Re.MatchString(l) {
			continue
		}
		dport53++
		if !isDrop {
			continue
		}
		iface := ""
		if m := fwIfaceRe.FindStringSubmatch(l); m != nil {
			iface = m[1]
		}
		if iface == "" || (iface != "lo" && !strings.HasPrefix(iface, "br")) {
			dport53Drop = true
		}
	}
	fwFact(t, "input_policy", policy)
	fwFact(t, "filter_drop_rules", strconv.Itoa(drops))
	fwFact(t, "dport53_rules", strconv.Itoa(dport53))
	switch {
	case dport53Drop || policy == "DROP":
		fwFact(t, "wan53_filtered", "yes")
	case drops > 0:
		fwFact(t, "wan53_filtered", "unknown")
	default:
		fwFact(t, "wan53_filtered", "no")
	}
}

// fwWaitCores ждёт, пока после команды XKeen снова появятся процессы ядер (если
// они были до неё), не дольше минуты.
func fwWaitCores(before string) string {
	cur := fwCorePIDs()
	for end := time.Now().Add(time.Minute); before != "none" && cur == "none" && time.Now().Before(end); {
		time.Sleep(time.Second)
		cur = fwCorePIDs()
	}
	return cur
}

// fwRestore возвращает стенд: override (первым — DNS сети важнее всего),
// proxy_dns и остановленное до теста ядро. Идемпотентна: вызывается в конце
// сценария и в t.Cleanup.
func fwRestore(t *testing.T, overrideBefore, proxyBefore, coresBefore string) {
	t.Helper()
	if overrideBefore == "on" || overrideBefore == "off" {
		if cur, _, _ := fwOverrideState(); cur != overrideBefore {
			cmd := fwCmdOverrideOff
			if overrideBefore == "on" {
				cmd = fwCmdOverrideOn
			}
			_, code := fwNdmc(cmd)
			t.Logf("возврат: override %s (код %d)", overrideBefore, code)
		}
	}
	if proxyBefore == "on" || proxyBefore == "off" {
		if cur := fwProxyDNS(); cur != proxyBefore {
			code := fwXKeen("-dns", proxyBefore)
			t.Logf("возврат: proxy_dns %s (код %d)", proxyBefore, code)
		}
	}
	if coresBefore == "none" && fwCorePIDs() != "none" {
		code := fwXKeen("-stop")
		t.Logf("возврат: ядро, запущенное переключением proxy_dns, остановлено (код %d)", code)
	}
}

// fwProxyToggle переключает proxy_dns туда и обратно и печатает, перезапускает
// ли XKeen ядро и запускает ли остановленное. Возвращает, менялись ли PID ядер.
func fwProxyToggle(t *testing.T, before, coresBefore string) bool {
	t.Helper()
	if before != "on" && before != "off" {
		fwFact(t, "proxy_dns_toggle_restart", "skipped")
		fwFact(t, "proxy_dns_starts_stopped", "skipped")
		return false
	}
	first, second := "on", "off"
	if before == "on" {
		first, second = "off", "on"
	}
	c1 := fwXKeen("-dns", first)
	time.Sleep(3 * time.Second)
	coresMid := fwWaitCores(coresBefore)
	fwFact(t, "proxy_dns_mid", fwProxyDNS())
	c2 := fwXKeen("-dns", second)
	time.Sleep(3 * time.Second)
	coresEnd := fwWaitCores(coresBefore)
	fwFact(t, "proxy_dns_toggle_rc", strconv.Itoa(c1)+"/"+strconv.Itoa(c2))
	fwFact(t, "proxy_dns_end", fwProxyDNS())
	restarted := coresMid != coresBefore || coresEnd != coresMid
	fwFact(t, "proxy_dns_toggle_restart", fwYesNo(restarted))
	starts := "n/a"
	if coresBefore == "none" {
		starts = fwYesNo(coresMid != "none" || coresEnd != "none")
	}
	fwFact(t, "proxy_dns_starts_stopped", starts)
	return restarted
}

func TestRouterDNSFirmwareFacts(t *testing.T) {
	routertest.Current(t)

	// 1. До изменений.
	verOut, verCode := fwNdmc(fwCmdShowVersion)
	fwFact(t, "ndmc_rc", strconv.Itoa(verCode))
	vendor := fwVendor(verOut)
	fwFact(t, "vendor", fwOrNone(vendor))
	svc := ""
	if vendor != "" {
		svc = "my." + strings.ToLower(vendor) + ".net."
		fwFact(t, "svc_domain_ok", fwOkFail(fwQuery(fwLoopbackDNS, svc, dnsmessage.TypeA, 3*time.Second).resolved()))
	} else {
		fwFact(t, "svc_domain_ok", "none")
	}

	overrideBefore, readVia, lines := fwOverrideState()
	fwFact(t, "override_before", overrideBefore)
	fwFact(t, "override_read", readVia)
	fwFact(t, "override_lines", strconv.Itoa(lines))
	startupHash, startupRead, startupOverride := fwStartup()
	fwFact(t, "startup_read", startupRead)
	fwFact(t, "startup_override", startupOverride)
	proxyBefore := fwProxyDNS()
	fwFact(t, "proxy_dns_before", proxyBefore)
	coresBefore := fwCorePIDs()
	fwFact(t, "cores_before", fwCoreNames(coresBefore))

	socks, owner := fwSockets()
	p53, comms := fwDescribe53(socks, owner)
	fwFact(t, "p53_before", p53)
	resolverComm := ""
	for _, c := range comms {
		if c != "xray" && c != "mihomo" && c != "none" {
			resolverComm = c
			break
		}
	}
	fwFact(t, "fw_comm", fwOrNone(resolverComm))
	fwIPv6Facts(t)

	client := fwClientLabel()
	if client != "" {
		fwFact(t, "client_name", "present")
		fwFact(t, "client_before", fwOkFail(fwQuery(fwLoopbackDNS, client, dnsmessage.TypeA, 3*time.Second).resolved()))
	} else {
		fwFact(t, "client_name", "none")
		fwFact(t, "client_before", "none")
	}
	fwPortsFact(t, "fw_before", "fw_ports_before", resolverComm, svc, client, false)
	fwFact(t, "redir_before", fwRedir("iptables"))
	fwFact(t, "redir6_before", fwRedir("ip6tables"))
	fwWAN53Facts(t)

	// Страховка: при любом исходе (провал, паника) override и proxy_dns
	// возвращаются к исходным.
	t.Cleanup(func() { fwRestore(t, overrideBefore, proxyBefore, coresBefore) })

	// 2. Override включён: кто держит :53, где отвечает резолвер прошивки.
	if overrideBefore == "unknown" {
		fwFact(t, "override_on_rc", "skipped")
	} else {
		out, code := fwNdmc(fwCmdOverrideOn)
		fwFact(t, "override_on_rc", strconv.Itoa(code))
		fwFact(t, "override_on_msg", fwRedact(out))
		time.Sleep(2 * time.Second)
		socks, owner = fwSockets()
		p53On, _ := fwDescribe53(socks, owner)
		fwFact(t, "p53_after_on", p53On)
		stateOn, _, _ := fwOverrideState()
		fwFact(t, "override_after_on", stateOn)
		fwPortsFact(t, "fw", "fw_ports", resolverComm, svc, client, true)
		fwFact(t, "redir_after", fwRedir("iptables"))
		fwFact(t, "redir6_after", fwRedir("ip6tables"))
		if startupRead == "ok" {
			hashOn, readOn, _ := fwStartup()
			if readOn == "ok" {
				fwFact(t, "startup_changed", fwYesNo(hashOn != startupHash))
			} else {
				fwFact(t, "startup_changed", "unknown")
			}
		} else {
			fwFact(t, "startup_changed", "unknown")
		}

		// 3. Override выключен: за сколько секунд прошивка снова отвечает на :53.
		out, code = fwNdmc(fwCmdOverrideOff)
		fwFact(t, "override_off_rc", strconv.Itoa(code))
		fwFact(t, "override_off_msg", fwRedact(out))
		if d, ok := fwWaitAlive(svc, fwRebindLimit); ok {
			fwFact(t, "rebind_s", strconv.FormatFloat(d.Seconds(), 'f', 1, 64))
		} else {
			fwFact(t, "rebind_s", "timeout")
		}
		socks, owner = fwSockets()
		p53Off, _ := fwDescribe53(socks, owner)
		fwFact(t, "p53_after_off", p53Off)
		if overrideBefore == "on" {
			_, code = fwNdmc(fwCmdOverrideOn)
			t.Logf("override возвращён к исходному on (код %d)", code)
		}
	}

	// 4. proxy_dns туда и обратно: перезапускает ли XKeen ядро.
	restarted := fwProxyToggle(t, proxyBefore, coresBefore)
	fwRestore(t, overrideBefore, proxyBefore, coresBefore)

	// Вердикт: стенд возвращён.
	overrideEnd, _, _ := fwOverrideState()
	routertest.Verdict(t, overrideBefore == "unknown" || overrideEnd == overrideBefore, routertest.KnownMark{},
		"override в конце %s, до теста %s", overrideEnd, overrideBefore)
	proxyEnd := fwProxyDNS()
	routertest.Verdict(t, proxyBefore == "unknown" || proxyEnd == proxyBefore, routertest.KnownMark{},
		"proxy_dns в конце %s, до теста %s", proxyEnd, proxyBefore)
	_, alive := fwWaitAlive(svc, fwRebindLimit)
	routertest.Verdict(t, alive, routertest.KnownMark{},
		"прошивка не отвечает на 127.0.0.1:53 после теста (ждали %s)", fwRebindLimit)
	coresEnd := fwWaitCores(coresBefore)
	samePIDs := coresEnd == coresBefore
	explained := restarted && fwCoreNames(coresEnd) == fwCoreNames(coresBefore)
	routertest.Verdict(t, samePIDs || explained, routertest.KnownMark{},
		"процессы ядер стенда изменились: до %s, после %s (переключение proxy_dns перезапускало ядро: %s)",
		fwCoreNames(coresBefore), fwCoreNames(coresEnd), fwYesNo(restarted))
}
