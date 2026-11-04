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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/services"
)

// Прошивка NDMS: доступность ndmc, бренд и служебный домен, владелец порта 53,
// старое перенаправление DNS XKeen (proxy_dns). Команды прошивки — только
// константы типа ndmcCommand через ndmc без оболочки: пользовательский ввод в
// строку команды не попадает (T-148-14).

// ndmcCommand — константная команда прошивки для `ndmc -c`. Отдельный тип не
// даёт передать в runNdmc произвольную строку без явного приведения.
type ndmcCommand string

// cmdShowVersion — сведения о прошивке: по ответу судим, что ndmc работает.
const cmdShowVersion ndmcCommand = "show version"

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
)

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
	now    func() time.Time
	// lookPath ищет ndmc (exec.LookPath).
	lookPath func(string) (string, error)

	mu      sync.Mutex
	checked time.Time
	ok      bool
	reason  string
}

func newFirmware(device *services.DeviceInfo) *Firmware {
	return &Firmware{device: device, now: time.Now, lookPath: exec.LookPath}
}

// runNdmc выполняет константную команду прошивки без оболочки с таймаутом.
func runNdmc(ctx context.Context, cmd ndmcCommand) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, firmwareTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "ndmc", "-c", string(cmd)).CombinedOutput()
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

// sock53 — сокет на порту 53: слушающий TCP или несвязанный UDP.
type sock53 struct {
	proto string // udp, udp6, tcp, tcp6
	ip    net.IP
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
func sockets53() []sock53 {
	var out []sock53
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
			ip, port, ok := parseHexAddr(f[1])
			if !ok || port != dnsPort {
				continue
			}
			if strings.HasPrefix(proto, "tcp") && f[3] != "0A" {
				continue // только LISTEN
			}
			if _, remPort, _ := parseHexAddr(f[2]); strings.HasPrefix(proto, "udp") && remPort != 0 {
				continue // связанный исходящий сокет
			}
			out = append(out, sock53{proto: proto, ip: ip, inode: f[9]})
		}
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
