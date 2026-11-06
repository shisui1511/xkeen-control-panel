package dnstakeover

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Прошивка NDMS: доступность ndmc, бренд и служебный домен, владелец порта 53,
// старое перенаправление DNS XKeen (proxy_dns). Команды прошивки — только
// константы типа ndmcCommand через ndmc без оболочки: пользовательский ввод в
// строку команды не попадает (T-148-14).

// ndmcCommand — константная команда прошивки для `ndmc -c`. Отдельный тип не
// даёт передать в runNdmc произвольную строку без явного приведения.
type ndmcCommand string

// Константные команды прошивки.
const (
	// cmdShowVersion — сведения о прошивке: по ответу судим, что ndmc работает.
	cmdShowVersion ndmcCommand = "show version"
	// cmdShowRunning — рабочая конфигурация прошивки: по ней читается override
	// (факт override_read 148-SPIKE).
	cmdShowRunning ndmcCommand = "show running-config"
	// cmdOverrideOn и cmdOverrideOff — прошивка отдаёт порт 53 и забирает его
	// обратно. Сохранение конфигурации прошивки пакет не вызывает (D-16): флаг
	// живёт до перезагрузки роутера.
	cmdOverrideOn  ndmcCommand = "opkg dns-override"
	cmdOverrideOff ndmcCommand = "no opkg dns-override"
)

const (
	// firmwareTimeout — предел одной команды прошивки.
	firmwareTimeout = 3 * time.Second
	// availableOKTTL и availableFailTTL — сколько помнится ответ о доступности
	// прошивки: удачный дольше (прошивка не пропадает), неудачный перепроверяется
	// чаще (ndmc мог не успеть подняться после загрузки роутера).
	availableOKTTL   = 10 * time.Minute
	availableFailTTL = time.Minute
	// xkeenInitScript — init-скрипт XKeen с переменной proxy_dns.
	xkeenInitScript = "/opt/etc/init.d/S05xkeen"
	// dnsPort — порт DNS.
	dnsPort = 53
	// ndmcLongTimeout — предел команды прошивки, которая читает или меняет
	// конфигурацию (show running-config отвечает дольше show version).
	ndmcLongTimeout = 15 * time.Second
	// overrideSettle — сколько ждать, пока прошивка применит override: строка в
	// рабочей конфигурации и порт 53 (rebind_s 148-SPIKE — доли секунды).
	overrideSettle = 30 * time.Second
	// overridePoll — пауза между проверками override и порта 53.
	overridePoll = 300 * time.Millisecond
	// resolverProbeTimeout — предел пробы одного порта резолвера прошивки.
	resolverProbeTimeout = 700 * time.Millisecond
	// resolverProbeParallel — сколько портов прошивки пробуется одновременно.
	resolverProbeParallel = 16
	// proxyDNSTimeout — предел `xkeen -dns on|off`.
	proxyDNSTimeout = 30 * time.Second
)

// errResolverNotFound — резолвер прошивки не найден: ни один порт процесса
// резолвера прошивки, кроме 53, не ответил как DNS.
var errResolverNotFound = errors.New("dnstakeover: резолвер прошивки не найден")

// Служебные домены роутера по бренду (D-11). Неизвестный бренд — пусто: домен
// не угадывается (RESEARCH A7).
var serviceDomains = map[string]string{
	"keenetic": "my.keenetic.net",
	"netcraze": "my.netcraze.net",
}

// firmwareResolverComms — процессы резолвера прошивки, которые держат :53 без
// opkg dns-override.
var firmwareResolverComms = map[string]bool{
	"ndnproxy": true,
}

// Firmware — сведения о прошивке с кэшем доступности.
type Firmware struct {
	device *services.DeviceInfo
	// xkeen — служба XKeen для `xkeen -dns on|off` (строка proxy_dns).
	xkeen *services.XKeenService
	now   func() time.Time
	// lookPath ищет ndmc (exec.LookPath).
	lookPath func(string) (string, error)

	mu      sync.Mutex
	checked time.Time
	ok      bool
	reason  string
}

func newFirmware(device *services.DeviceInfo, xkeen *services.XKeenService) *Firmware {
	return &Firmware{device: device, xkeen: xkeen, now: time.Now, lookPath: exec.LookPath}
}

// runNdmc выполняет константную команду прошивки без оболочки с таймаутом.
func runNdmc(ctx context.Context, cmd ndmcCommand) ([]byte, error) {
	return runNdmcFor(ctx, cmd, firmwareTimeout)
}

// runNdmcFor — runNdmc с заданным пределом.
func runNdmcFor(ctx context.Context, cmd ndmcCommand, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return exec.CommandContext(ctx, "ndmc", "-c", string(cmd)).CombinedOutput()
}

// cmdOutput — вывод команды для detail: без пробелов по краям, не длиннее
// maxDetail последних байт.
func cmdOutput(out []byte) string {
	return truncateDetail(strings.TrimSpace(string(out)))
}

// OverrideEnabled читает override из рабочей конфигурации прошивки (факт
// override_read): строка «opkg dns-override» плоская или «dns-override» внутри
// раздела opkg.
func (f *Firmware) OverrideEnabled(ctx context.Context) (bool, error) {
	out, err := runNdmcFor(ctx, cmdShowRunning, ndmcLongTimeout)
	if err != nil {
		return false, fmt.Errorf("ndmc -c %q: %v: %s", string(cmdShowRunning), err, cmdOutput(out))
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return false, fmt.Errorf("ndmc -c %q: empty output", string(cmdShowRunning))
	}
	return parseOverride(string(out)), nil
}

// parseOverride ищет строку включённого override в рабочей конфигурации.
func parseOverride(cfg string) bool {
	for _, line := range strings.Split(cfg, "\n") {
		switch strings.TrimSpace(line) {
		case string(cmdOverrideOn), "dns-override":
			return true
		}
	}
	return false
}

// SetOverride включает или выключает override прошивки и ждёт, пока прошивка его
// применит: строка в рабочей конфигурации совпала, затем порт 53 отпущен (on) или
// прошивка снова отвечает через 127.0.0.1:53 (off, A12). Общий предел — 30 с.
func (f *Firmware) SetOverride(ctx context.Context, on bool) error {
	cmd := cmdOverrideOff
	if on {
		cmd = cmdOverrideOn
	}
	if out, err := runNdmcFor(ctx, cmd, ndmcLongTimeout); err != nil {
		return fmt.Errorf("ndmc -c %q: %v: %s", string(cmd), err, cmdOutput(out))
	}
	deadline := f.now().Add(overrideSettle)
	for {
		cur, err := f.OverrideEnabled(ctx)
		if err == nil && cur == on {
			break
		}
		if !f.now().Before(deadline) {
			if err == nil {
				err = fmt.Errorf("в рабочей конфигурации override=%v", cur)
			}
			return fmt.Errorf("ndmc -c %q: за %v не применена: %v", string(cmd), overrideSettle, err)
		}
		if err := sleepCtx(ctx, overridePoll); err != nil {
			return err
		}
	}
	for !f.port53Settled(ctx, on) {
		if !f.now().Before(deadline) {
			if on {
				return fmt.Errorf("прошивка за %v не отпустила порт %d", overrideSettle, dnsPort)
			}
			return fmt.Errorf("прошивка за %v не ответила через %s", overrideSettle, LoopbackDNS)
		}
		if err := sleepCtx(ctx, overridePoll); err != nil {
			return err
		}
	}
	return nil
}

// port53Settled — порт 53 в ожидаемом после override состоянии: при on его не
// держит резолвер прошивки; при off через 127.0.0.1:53 отвечает служебный домен
// (домен неизвестен — резолвер прошивки снова держит порт).
func (f *Firmware) port53Settled(ctx context.Context, on bool) bool {
	if on {
		return PortOwner().Kind != OwnerFirmware
	}
	if domain := f.ServiceDomain(); domain != "" {
		return resolverAnswers(ctx, LoopbackDNS, domain, probeAttempt)
	}
	return PortOwner().Kind == OwnerFirmware
}

// sleepCtx ждёт d или отмены ctx.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ResolverAddr находит адрес резолвера прошивки (127.0.0.1:<порт>) только
// чтением, без команд прошивки: UDP-порты процесса резолвера прошивки (тот же
// comm, что держит :53 до override; /proc/net/udp и udp6, владелец по inode),
// кроме 53, на loopback или любом адресе; из них — первый по номеру порт, который
// отвечает на служебный домен роутера (домен неизвестен — любым ответом DNS).
// Факты 148-SPIKE (fw_ports_before, fw_before_port_41100_svc, redir_before):
// перенаправления _NDM_HOTSPOT_DNSREDIR нет, порт резолвера виден и до, и после
// override. Адрес находится заново при каждом вызове; не найден —
// errResolverNotFound.
func (f *Firmware) ResolverAddr(ctx context.Context) (string, error) {
	ports := resolverPorts()
	if len(ports) == 0 {
		return "", fmt.Errorf("%w: у процесса резолвера прошивки нет UDP-портов, кроме %d", errResolverNotFound, dnsPort)
	}
	domain := f.ServiceDomain()
	ok := make([]bool, len(ports))
	sem := make(chan struct{}, resolverProbeParallel)
	var wg sync.WaitGroup
	for i, port := range ports {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ok[i] = resolverAnswers(ctx, loopbackAddr(port), domain, resolverProbeTimeout)
		}()
	}
	wg.Wait()
	for i, port := range ports {
		if ok[i] {
			return loopbackAddr(port), nil
		}
	}
	return "", fmt.Errorf("%w: ни один из %d портов не ответил как DNS", errResolverNotFound, len(ports))
}

// loopbackAddr — адрес 127.0.0.1:<порт>.
func loopbackAddr(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// resolverAnswers — addr отвечает как резолвер прошивки: на служебный домен —
// NOERROR с адресом; домен неизвестен — любой ответ DNS на localhost.
func resolverAnswers(ctx context.Context, addr, domain string, timeout time.Duration) bool {
	if domain == "" {
		_, err := Query(ctx, addr, "localhost", dnsmessage.TypeA, timeout)
		return err == nil
	}
	ans, err := Query(ctx, addr, domain, dnsmessage.TypeA, timeout)
	return err == nil && ans.RCode == dnsmessage.RCodeSuccess && len(ans.Addrs) > 0
}

// resolverPorts — UDP-порты процесса резолвера прошивки, кроме 53, по
// возрастанию: сокеты IPv4 на loopback или любом адресе и IPv6 на любом адресе
// (bindv6only=0: принимает и IPv4).
func resolverPorts() []int {
	var socks []sockInfo
	for _, proto := range []string{"udp", "udp6"} {
		for _, s := range readSockets(proto) {
			if s.port == dnsPort {
				continue
			}
			v4 := s.ip.To4() != nil
			if (v4 && (s.ip.IsLoopback() || s.ip.IsUnspecified())) || (!v4 && s.ip.IsUnspecified()) {
				socks = append(socks, s)
			}
		}
	}
	if len(socks) == 0 {
		return nil
	}
	want := make(map[string]bool, len(socks))
	for _, s := range socks {
		want[s.inode] = true
	}
	comms := socketOwners(want)
	seen := map[int]bool{}
	var out []int
	for _, s := range socks {
		if !firmwareResolverComms[comms[s.inode]] || seen[s.port] {
			continue
		}
		seen[s.port] = true
		out = append(out, s.port)
	}
	sort.Ints(out)
	return out
}

// HasIPv6 — у роутера есть глобальный адрес IPv6 на поднятом интерфейсе (D-31,
// факт ipv6_global).
func (f *Firmware) HasIPv6() bool {
	return routerHasIPv6()
}

// SetProxyDNS меняет строку proxy_dns init-скрипта XKeen (`xkeen -dns on|off`:
// аргументы — константы, без оболочки, с пределом) и проверяет её. Ядро не
// перезапускается (факт proxy_dns_toggle_restart): правила перехвата XKeen
// перестраиваются при следующем старте ядра, его делает конвейер слоя.
func (f *Firmware) SetProxyDNS(ctx context.Context, on bool) error {
	if f.xkeen == nil {
		return errors.New("xkeen: служба не задана")
	}
	ctx, cancel := context.WithTimeout(ctx, proxyDNSTimeout)
	defer cancel()
	out, err := f.xkeen.SetProxyDNSLine(ctx, on)
	if err != nil {
		return fmt.Errorf("xkeen -dns: %v: %s", err, truncateDetail(strings.TrimSpace(out)))
	}
	cur, err := ProxyDNS()
	if err != nil {
		return fmt.Errorf("proxy_dns не прочитан: %v", err)
	}
	if cur != on {
		return fmt.Errorf("xkeen -dns: proxy_dns=%v, ожидалось %v: %s", cur, on, truncateDetail(strings.TrimSpace(out)))
	}
	return nil
}

// Available сообщает, что ndmc найден и отвечает на show version; reason —
// короткая техническая причина отказа для unavailable_reason. Ответ кэшируется.
func (f *Firmware) Available(ctx context.Context) (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ttl := availableFailTTL
	if f.ok {
		ttl = availableOKTTL
	}
	if !f.checked.IsZero() && f.now().Sub(f.checked) < ttl {
		return f.ok, f.reason
	}
	f.ok, f.reason = f.probe(ctx)
	f.checked = f.now()
	return f.ok, f.reason
}

// probe проверяет ndmc без кэша.
func (f *Firmware) probe(ctx context.Context) (bool, string) {
	if _, err := f.lookPath("ndmc"); err != nil {
		return false, "ndmc: not found"
	}
	out, err := runNdmc(ctx, cmdShowVersion)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return false, "ndmc: canceled"
		}
		return false, fmt.Sprintf("ndmc -c %q: %v", string(cmdShowVersion), err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return false, fmt.Sprintf("ndmc -c %q: empty output", string(cmdShowVersion))
	}
	return true, ""
}

// Vendor — бренд прошивки из DeviceInfo (148-26); без ndmc пусто.
func (f *Firmware) Vendor() string {
	return f.device.Vendor()
}

// ServiceDomain — служебный домен роутера по бренду; неизвестный бренд — пусто.
func (f *Firmware) ServiceDomain() string {
	return serviceDomains[strings.ToLower(strings.TrimSpace(f.Vendor()))]
}

// sockInfo — сокет из /proc/net: слушающий TCP или несвязанный UDP.
type sockInfo struct {
	proto string // udp, udp6, tcp, tcp6
	ip    net.IP
	port  int
	inode string
}

// PortOwner определяет, кто держит порт 53: разбор /proc/net/{udp,udp6,tcp,tcp6},
// inode сокета → /proc/<pid>/fd → /proc/<pid>/comm. Ядро (xray, mihomo) — kernel,
// резолвер прошивки — firmware, иной процесс — other с его именем; сокетов нет —
// none. Если порт держат несколько процессов, ядро важнее прошивки, прошивка —
// прочих.
func PortOwner() Owner {
	socks := sockets53()
	if len(socks) == 0 {
		return Owner{Kind: OwnerNone}
	}
	want := make(map[string]bool, len(socks))
	for _, s := range socks {
		want[s.inode] = true
	}
	comms := socketOwners(want)
	best := Owner{Kind: OwnerOther}
	rank := -1
	for _, s := range socks {
		comm := comms[s.inode]
		o, r := classify(comm)
		if r > rank {
			o.Addr = net.JoinHostPort(s.ip.String(), strconv.Itoa(dnsPort))
			best, rank = o, r
		}
	}
	return best
}

// classify — владелец по имени процесса и его приоритет.
func classify(comm string) (Owner, int) {
	switch {
	case comm == "xray" || comm == "mihomo":
		return Owner{Kind: OwnerKernel, Kernel: comm, Process: comm}, 3
	case firmwareResolverComms[comm]:
		return Owner{Kind: OwnerFirmware, Process: comm}, 2
	case comm != "":
		return Owner{Kind: OwnerOther, Process: comm}, 1
	}
	return Owner{Kind: OwnerOther}, 0
}

// sockets53 читает сокеты порта 53 из /proc/net.
func sockets53() []sockInfo {
	var out []sockInfo
	for _, proto := range []string{"udp", "udp6", "tcp", "tcp6"} {
		for _, s := range readSockets(proto) {
			if s.port == dnsPort {
				out = append(out, s)
			}
		}
	}
	return out
}

// readSockets читает слушающие TCP и несвязанные UDP сокеты протокола proto из
// /proc/net/<proto>.
func readSockets(proto string) []sockInfo {
	data, err := os.ReadFile("/proc/net/" + proto)
	if err != nil {
		return nil
	}
	var out []sockInfo
	for i, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 10 {
			continue
		}
		ip, port, ok := parseHexAddr(f[1])
		if !ok {
			continue
		}
		if strings.HasPrefix(proto, "tcp") && f[3] != "0A" {
			continue // только LISTEN
		}
		if _, remPort, _ := parseHexAddr(f[2]); strings.HasPrefix(proto, "udp") && remPort != 0 {
			continue // связанный исходящий сокет
		}
		out = append(out, sockInfo{proto: proto, ip: ip, port: port, inode: f[9]})
	}
	return out
}

// parseHexAddr разбирает адрес вида 0100007F:0035 из /proc/net: слова по 4 байта
// в порядке хоста (little-endian на целевых платформах).
func parseHexAddr(s string) (net.IP, int, bool) {
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

// socketOwners находит имена процессов, держащих сокеты с inode из want.
func socketOwners(want map[string]bool) map[string]string {
	out := make(map[string]string, len(want))
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		if len(out) == len(want) {
			break
		}
		if _, err := strconv.Atoi(e.Name()); err != nil || !e.IsDir() {
			continue
		}
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		comm := ""
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
			if !want[inode] {
				continue
			}
			if _, seen := out[inode]; seen {
				continue
			}
			if comm == "" {
				comm = procComm(e.Name())
			}
			out[inode] = comm
		}
	}
	return out
}

// procComm — имя процесса по PID.
func procComm(pid string) string {
	data, err := os.ReadFile(filepath.Join("/proc", pid, "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ProxyDNS читает proxy_dns из init-скрипта XKeen при каждом вызове (значение
// меняется мимо панели и при обновлении XKeen, кэшировать нельзя). Нет файла —
// старого перенаправления нет.
func ProxyDNS() (bool, error) {
	data, err := os.ReadFile(xkeenInitScript)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return parseProxyDNS(string(data)), nil
}

// parseProxyDNS — разбор строки proxy_dns=… как в IsDNSProxyingEnabled.
func parseProxyDNS(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "proxy_dns" {
			continue
		}
		// Значение — первое слово: хвостовой комментарий не входит.
		fields := strings.Fields(val)
		return len(fields) > 0 && strings.Trim(fields[0], `"'`) == "on"
	}
	return false
}
