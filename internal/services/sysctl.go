package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// defaultSysctlDir is where Entware's init scripts pick up sysctl profiles on boot.
const defaultSysctlDir = "/opt/etc/sysctl.d"

// sysctlProfileFilename is the name of the profile XCP owns and manages.
const sysctlProfileFilename = "99-xkeen.conf"

// sysctlProfileContent tunes the Linux network stack for a router running
// XKeen/Mihomo/Xray under sustained NAT/proxy load (STAB-06):
//   - nf_conntrack_max/timeouts avoid exhausting the connection-tracking
//     table (the default 5-day established timeout lets stale NAT entries
//     pile up for days under heavy proxy traffic).
//   - tcp_tw_reuse/tcp_fin_timeout recycle TIME_WAIT sockets faster.
//   - fs.file-max/kernel.pid_max raise ceilings that a leaking-goroutine
//     bug (see Phase 99 analysis) would otherwise hit before OOM-killer
//     intervenes, turning a slow leak into an outright router freeze.
const sysctlProfileContent = `# XKeen Control Panel — системный профиль оптимизации ядра Linux (STAB-06).
# Управляется автоматически, ручные правки будут перезаписаны при следующем
# запуске xcp. Снижает риск исчерпания conntrack/файловых дескрипторов и
# зависания роутера под длительной прокси-нагрузкой.

# Максимальный размер таблицы conntrack (по умолчанию часто занижен на роутерах).
net.netfilter.nf_conntrack_max = 32768

# Established-соединения по умолчанию живут в conntrack 5 суток (432000s) —
# снижаем до 30 минут, чтобы устаревшие NAT-записи не копились неделями.
net.netfilter.nf_conntrack_tcp_timeout_established = 1800
net.netfilter.nf_conntrack_tcp_timeout_time_wait = 15

# Быстрее переиспользовать TIME_WAIT сокеты под высокой частотой соединений.
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15

# Запас по файловым дескрипторам и PID, чтобы утечка горутин/сокетов не
# приводила к EAGAIN и мягкому зависанию роутера раньше, чем сработает OOM.
fs.file-max = 65535
kernel.pid_max = 32768
`

// execSysctlApply invokes `sysctl -p <path>` to apply the profile immediately
// (in addition to Entware applying it again on next boot). It is a package
// variable so tests can stub it out instead of mutating the real kernel
// sysctl state of the machine running the test suite.
var execSysctlApply = func(path string) error {
	sysctlBin, err := exec.LookPath("sysctl")
	if err != nil {
		// Binary not present (e.g. minimal container/dev machine) — the file
		// is still deployed and will be picked up by Entware's own sysctl.d
		// handling on next boot where applicable.
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sysctlBin, "-p", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("DeploySysctlProfile: sysctl -p failed (profile file was still written): %v — %s",
			err, strings.TrimSpace(string(out)))
		return nil
	}
	log.Printf("DeploySysctlProfile: applied kernel profile from %s", path)
	return nil
}

// DeploySysctlProfile writes the XCP sysctl profile into sysctlDir (defaults
// to /opt/etc/sysctl.d when empty) and attempts to apply it immediately via
// `sysctl -p`. It is a safe no-op — returning nil without writing anything —
// when sysctlDir's parent directory does not exist, which is how we detect
// "not running on Entware" (e.g. a developer's workstation) without hardcoding
// a platform check (STAB-06).
func DeploySysctlProfile(sysctlDir string) error {
	if sysctlDir == "" {
		sysctlDir = defaultSysctlDir
	}

	parent := filepath.Dir(sysctlDir)
	if _, err := os.Stat(parent); err != nil {
		// Parent (e.g. /opt/etc) is missing — not an Entware environment.
		return nil
	}

	if err := os.MkdirAll(sysctlDir, 0755); err != nil {
		return fmt.Errorf("create sysctl.d dir %s: %w", sysctlDir, err)
	}

	path := filepath.Join(sysctlDir, sysctlProfileFilename)
	if err := utils.AtomicWriteFile(path, []byte(sysctlProfileContent), 0644); err != nil {
		return fmt.Errorf("write sysctl profile %s: %w", path, err)
	}

	return execSysctlApply(path)
}
