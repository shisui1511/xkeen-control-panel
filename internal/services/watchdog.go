package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shisui1511/xkeen-control-panel/internal/utils"
)

// watchdogCheckInterval is how often the watchdog polls kernel health.
const watchdogCheckInterval = 30 * time.Second

// watchdogMaxFailures is the number of consecutive failed health checks
// before the watchdog trips the emergency TPROXY disarm (STAB-05).
const watchdogMaxFailures = 3

// WatchdogService supervises the health of the currently active proxy kernel
// (Xray/Mihomo, driven via XKeenService) and acts as a circuit breaker: after
// watchdogMaxFailures consecutive failed health checks it removes the
// XKEEN_TPROXY interception rule from iptables so LAN devices regain direct
// internet access instead of being stuck behind a dead proxy ("soft lock" of
// the router — see Phase 99 analysis).
type WatchdogService struct {
	xkeenSvc  *XKeenService
	mihomoDir string
	xrayDir   string

	stopCh chan struct{}
	wg     sync.WaitGroup

	mu                  sync.Mutex
	consecutiveFailures int
	disarmed            bool
	disarmInFlight      bool
}

// NewWatchdogService creates a watchdog for the given XKeen service instance.
// mihomoDir/xrayDir are the kernel config directories, used by
// EnsureDefaultMihomoConfig and ValidateXrayRoutingTags respectively — kept
// on the struct so a future periodic self-heal pass can reuse them without
// re-plumbing parameters through main.go.
func NewWatchdogService(xkeenSvc *XKeenService, mihomoDir, xrayDir string) *WatchdogService {
	return &WatchdogService{
		xkeenSvc:  xkeenSvc,
		mihomoDir: mihomoDir,
		xrayDir:   xrayDir,
		stopCh:    make(chan struct{}),
	}
}

// Start launches the background health-check loop.
func (w *WatchdogService) Start() {
	w.wg.Add(1)
	go w.loop()
}

// Stop signals the loop to exit and waits for it to finish.
func (w *WatchdogService) Stop() {
	close(w.stopCh)
	w.wg.Wait()
}

func (w *WatchdogService) loop() {
	defer w.wg.Done()

	// Run the first health check immediately rather than waiting a full
	// watchdogCheckInterval — a kernel that is already wedged at boot (e.g.
	// right after a router reboot where XKeen fails to come up) should be
	// detected as soon as this service starts, not up to ~120s later.
	w.runCheck()

	ticker := time.NewTicker(watchdogCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.runCheck()
		case <-w.stopCh:
			return
		}
	}
}

// runCheck performs one health-check + routing-validation pass.
func (w *WatchdogService) runCheck() {
	w.CheckHealth()
	if issues := ValidateXrayRoutingTags(w.xrayDir); len(issues) > 0 {
		log.Printf("Watchdog: Xray routing validation found %d issue(s): %s", len(issues), strings.Join(issues, "; "))
	}
}

// isKernelStatusHealthy interprets the free-form output of `xkeen -status`
// (Russian and English builds both exist in the wild). Negative phrasing is
// checked first: the real stopped-state output is literally "XKeen is not
// running", which contains the substring "running" and would otherwise be
// misread as healthy.
func isKernelStatusHealthy(status string) bool {
	lower := strings.ToLower(status)
	negativeMarkers := []string{"not running", "не запущен", "не активен", "остановлен", "stopped"}
	for _, m := range negativeMarkers {
		if strings.Contains(lower, m) {
			return false
		}
	}
	return strings.Contains(lower, "running") || strings.Contains(lower, "активен")
}

// CheckHealth polls XKeen's current status and updates the failure counter.
// XKeenService.Status() already wraps the underlying `xkeen -status` call
// with a 5s timeout (STAB-01), so a hung/unresponsive kernel process
// surfaces here as an error/timeout rather than blocking this goroutine.
func (w *WatchdogService) CheckHealth() {
	if w.xkeenSvc == nil {
		return
	}

	status, err := w.xkeenSvc.Status()
	healthy := err == nil && isKernelStatusHealthy(status)

	w.mu.Lock()
	defer w.mu.Unlock()

	if healthy {
		if w.consecutiveFailures > 0 {
			log.Printf("Watchdog: kernel recovered after %d failed health check(s)", w.consecutiveFailures)
		}
		w.consecutiveFailures = 0
		w.disarmed = false
		return
	}

	w.consecutiveFailures++
	log.Printf("Watchdog: kernel health check failed (%d/%d): status=%q err=%v",
		w.consecutiveFailures, watchdogMaxFailures, strings.TrimSpace(status), err)

	if w.consecutiveFailures >= watchdogMaxFailures && !w.disarmed && !w.disarmInFlight {
		w.disarmInFlight = true
		log.Printf("Watchdog: %d consecutive kernel failures — triggering emergency TPROXY disarm", w.consecutiveFailures)
		// Tracked by wg so Stop() (called during graceful shutdown/restart)
		// waits for an in-flight disarm sequence instead of abandoning it
		// mid-way through mutating iptables state.
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ok := w.EmergencyDisarmTProxy()
			w.mu.Lock()
			w.disarmInFlight = false
			// Only latch disarmed on confirmed success. On failure, leave it
			// false so the next qualifying health check (consecutiveFailures
			// is still >= watchdogMaxFailures) retries instead of the
			// circuit breaker silently sitting there having never actually
			// removed the interception rule.
			w.disarmed = ok
			w.mu.Unlock()
			if !ok {
				log.Printf("Watchdog: EmergencyDisarmTProxy failed — will retry on next qualifying health check")
			}
		}()
	}
}

// ConsecutiveFailures returns the current failure streak (for diagnostics/tests).
func (w *WatchdogService) ConsecutiveFailures() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.consecutiveFailures
}

// tproxyChainMarker is a literal chain/comment name some XKeen builds may
// use for their interception rule. It is matched as a substring fallback,
// but is NOT assumed to be the primary signal: per this project's own
// stability analysis (ideas/system-freeze-causes-and-stability-analysis.md
// §6.1), the actual interception is installed as a `mangle/PREROUTING` rule
// using the kernel's real `TPROXY` netfilter target (`-j TPROXY --on-port
// 7892` or `--on-port 10808`), which may live directly in PREROUTING or in a
// custom chain jumped to from PREROUTING — not necessarily under a chain
// literally named "XKEEN_TPROXY". See tproxyTarget below for the primary
// match.
const tproxyChainMarker = "XKEEN_TPROXY"

// tproxyTarget is the real netfilter target module XKeen's interception
// rule invokes (`-j TPROXY --on-port ...`). Matching on this rather than a
// guessed chain name is what makes EmergencyDisarmTProxy correct regardless
// of which chain XKeen happens to install the rule into.
const tproxyTarget = "TPROXY"

// builtinMangleChains lists the standard chains iptables predefines in the
// mangle table. Any other chain name encountered while scanning is a
// custom/user chain (e.g. one XKeen creates for its own interception rules),
// which disarmTProxyFamily also tracks so it can remove the PREROUTING (or
// similar) jump into it once that chain is confirmed to hold a TPROXY rule.
var builtinMangleChains = map[string]bool{
	"PREROUTING": true, "INPUT": true, "FORWARD": true, "OUTPUT": true, "POSTROUTING": true,
}

// EmergencyDisarmTProxy removes the TPROXY interception rule(s) installed by
// XKeen from the iptables (and ip6tables, when present) mangle table so LAN
// devices regain direct internet access when the proxy kernel has failed
// watchdogMaxFailures consecutive health checks (STAB-05). This is invoked
// directly (not via the xkeen binary) because the XKeen binary itself may be
// the thing that's wedged.
//
// It reports whether the disarm can be considered handled: true if every
// family it could query came back clean (rules removed, or confirmed none
// present); false on an execution failure (iptables missing, xtables lock
// contention, permission error) so the caller can retry on the next
// qualifying health check instead of silently latching a failed attempt as
// success.
func (w *WatchdogService) EmergencyDisarmTProxy() bool {
	ctx := context.Background()

	removedV4, okV4 := disarmTProxyFamily(ctx, "iptables-save", "iptables")
	removedV6, okV6 := disarmTProxyFamily(ctx, "ip6tables-save", "ip6tables")

	if !okV4 || !okV6 {
		log.Printf("Watchdog: EmergencyDisarmTProxy incomplete (ipv4 ok=%v removed=%d, ipv6 ok=%v removed=%d) — TPROXY interception may still be active",
			okV4, removedV4, okV6, removedV6)
		return false
	}

	if removedV4+removedV6 == 0 {
		log.Printf("Watchdog: EmergencyDisarmTProxy: no %s rules found in mangle table (already absent or interception not installed)", tproxyChainMarker)
	} else {
		log.Printf("Watchdog: EmergencyDisarmTProxy removed %d TPROXY interception rule(s) (ipv4=%d, ipv6=%d)",
			removedV4+removedV6, removedV4, removedV6)
	}
	return true
}

// disarmTProxyFamily removes every mangle-table rule that installs XKeen's
// TPROXY interception, for one iptables family (saveBin/delBin is either
// "iptables-save"/"iptables" or "ip6tables-save"/"ip6tables"). It lists the
// live rules via saveBin and identifies matches two ways:
//  1. any rule invoking the real `-j TPROXY` netfilter target (the
//     interception mechanism XKeen actually uses per this project's own
//     analysis — see tproxyTarget), regardless of which chain it lives in;
//  2. any rule mentioning tproxyChainMarker as a fallback, in case a given
//     build names its chain/comment "XKEEN_TPROXY" literally.
//
// If a match (1) lives in a custom (non-builtin) chain, this also removes
// the jump rule(s) that reference that chain (e.g. "-A PREROUTING -j
// xkeen"), so the interception is fully disarmed rather than leaving a
// dangling-but-still-invoked custom chain. Each matching "-A ..." line is
// converted to the equivalent "-D ..." invocation and executed — the exact
// rule signature is discovered live rather than hardcoded/guessed.
//
// Returns the number of rules removed and whether the operation can be
// trusted as complete. ok is true both when rules were found and removed and
// when the binary simply isn't present on this system (ip6tables may not be
// installed on all router variants — that's not a failure of the disarm
// attempt). ok is false only on a genuine execution failure such as xtables
// lock contention or a permission error, where we can't tell whether the
// interception rule is actually gone.
func disarmTProxyFamily(ctx context.Context, saveBin, delBin string) (removed int, ok bool) {
	saveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	out, err := exec.CommandContext(saveCtx, saveBin, "-t", "mangle").Output()
	cancel()
	if err != nil {
		if isCommandNotFound(err) {
			// This iptables family isn't present on this system — nothing to
			// disarm here, not a failure.
			return 0, true
		}
		log.Printf("Watchdog: EmergencyDisarmTProxy: failed to list mangle table via %s: %v", saveBin, err)
		return 0, false
	}

	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "-A ") {
			lines = append(lines, l)
		}
	}

	toDelete := map[string]bool{} // dedup: a line could match both signals
	customChains := map[string]bool{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		chain := fields[1]
		matchesTarget := false
		for i, f := range fields {
			if f == "-j" && i+1 < len(fields) && fields[i+1] == tproxyTarget {
				matchesTarget = true
				break
			}
		}
		if matchesTarget || strings.Contains(line, tproxyChainMarker) {
			toDelete[line] = true
			if !builtinMangleChains[chain] {
				customChains[chain] = true
			}
		}
	}

	// Second pass: also remove any rule that jumps into a custom chain we
	// just identified as holding the interception rule, so the chain is
	// fully unreachable (not just emptied).
	if len(customChains) > 0 {
		for _, line := range lines {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "-j" && i+1 < len(fields) && customChains[fields[i+1]] {
					toDelete[line] = true
					break
				}
			}
		}
	}

	for line := range toDelete {
		args := strings.Fields(line)
		args[0] = "-D" // "-A CHAIN ..." -> "-D CHAIN ..." removes exactly this rule
		delArgs := append([]string{"-w", "5", "-t", "mangle"}, args...)

		delCtx, delCancel := context.WithTimeout(ctx, 10*time.Second)
		delOut, delErr := exec.CommandContext(delCtx, delBin, delArgs...).CombinedOutput()
		delCancel()
		if delErr != nil {
			log.Printf("Watchdog: EmergencyDisarmTProxy: failed to remove rule via %s (%q): %v — %s",
				delBin, line, delErr, strings.TrimSpace(string(delOut)))
			return removed, false
		}

		log.Printf("Watchdog: removed TPROXY interception rule via %s: %s", delBin, line)
		removed++
	}

	return removed, true
}

// isCommandNotFound reports whether err comes from exec failing to locate
// the binary on PATH (as opposed to the binary running and failing).
func isCommandNotFound(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.Is(execErr.Err, exec.ErrNotFound)
	}
	return false
}

// defaultMihomoConfigYAML is a minimal, self-contained recovery config: DIRECT-only
// routing with no external dependencies (no rule-providers/proxy-providers), so
// Mihomo can start successfully even with zero configured subscriptions. The user
// is expected to configure real proxies afterwards via Subscriptions/Constructor.
const defaultMihomoConfigYAML = `# Автоматически создано XKeen Control Panel: config.yaml отсутствовал в
# директории Mihomo, панель развернула безопасный минимальный конфиг
# (весь трафик DIRECT), чтобы ядро могло запуститься без падения (STAB-04).
# Настройте прокси через раздел Подписки / Конструктор Mihomo панели.
mixed-port: 7890
allow-lan: false
mode: rule
log-level: warning
ipv6: false
external-controller: 127.0.0.1:9090

dns:
  enable: true
  listen: 0.0.0.0:1053
  enhanced-mode: fake-ip
  nameserver:
    - 1.1.1.1
    - 8.8.8.8

proxies: []
proxy-groups: []

rules:
  - MATCH,DIRECT
`

// EnsureDefaultMihomoConfig writes a minimal, valid config.yaml into
// mihomoDir when neither config.yaml nor config.yml already exists there
// (STAB-04). It is a no-op — and returns nil — if a config file is already
// present, or if mihomoDir itself does not exist (e.g. Mihomo isn't
// installed on this system), so it never surprises a working installation.
func EnsureDefaultMihomoConfig(mihomoDir string) error {
	if mihomoDir == "" {
		return nil
	}
	if _, err := os.Stat(mihomoDir); err != nil {
		// Directory doesn't exist — Mihomo likely isn't installed; nothing to recover.
		return nil
	}

	yamlPath := filepath.Join(mihomoDir, "config.yaml")
	ymlPath := filepath.Join(mihomoDir, "config.yml")
	if _, err := os.Stat(yamlPath); err == nil {
		return nil
	}
	if _, err := os.Stat(ymlPath); err == nil {
		return nil
	}

	if err := utils.AtomicWriteFile(yamlPath, []byte(defaultMihomoConfigYAML), 0644); err != nil {
		return fmt.Errorf("write default mihomo config: %w", err)
	}
	log.Printf("Watchdog: config.yaml was missing in %s — wrote a minimal safe default (STAB-04)", mihomoDir)
	return nil
}

// xrayOutboundFragment is the subset of a 04_outbounds*.json fragment we need
// to collect declared outbound tags.
type xrayOutboundFragment struct {
	Outbounds []struct {
		Tag string `json:"tag"`
	} `json:"outbounds"`
}

// xrayRoutingFragment is the subset of a 05_routing*.json fragment we need to
// collect referenced outboundTag values.
type xrayRoutingFragment struct {
	Routing struct {
		Rules []struct {
			OutboundTag string `json:"outboundTag"`
		} `json:"rules"`
	} `json:"routing"`
	// Some fragments (per-subscription) store rules at the top level instead
	// of nesting under "routing" — support both shapes defensively.
	Rules []struct {
		OutboundTag string `json:"outboundTag"`
	} `json:"rules"`
}

// ValidateXrayRoutingTags scans all 04_outbounds*.json fragments in configDir
// to build the set of declared outbound tags, then scans all 05_routing*.json
// fragments for outboundTag references that don't resolve to any declared
// tag. It never mutates anything; the caller decides what to do with the
// returned human-readable issue descriptions (STAB-04 — the same
// misconfiguration class causes xray -test to fail and can leave a kernel
// permanently in a failed/looping restart state).
func ValidateXrayRoutingTags(configDir string) []string {
	if configDir == "" {
		return nil
	}
	if _, err := os.Stat(configDir); err != nil {
		return nil
	}

	knownTags := map[string]bool{}
	outboundFiles, _ := filepath.Glob(filepath.Join(configDir, "04_outbounds*.json"))
	for _, f := range outboundFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var frag xrayOutboundFragment
		if err := json.Unmarshal(data, &frag); err != nil {
			continue
		}
		for _, ob := range frag.Outbounds {
			if ob.Tag != "" {
				knownTags[ob.Tag] = true
			}
		}
	}

	if len(knownTags) == 0 {
		// No outbound fragments found at all — nothing meaningful to validate.
		return nil
	}

	var issues []string
	routingFiles, _ := filepath.Glob(filepath.Join(configDir, "05_routing*.json"))
	for _, f := range routingFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var frag xrayRoutingFragment
		if err := json.Unmarshal(data, &frag); err != nil {
			continue
		}
		rules := frag.Routing.Rules
		if len(rules) == 0 {
			rules = frag.Rules
		}
		for _, rule := range rules {
			if rule.OutboundTag == "" {
				continue
			}
			if !knownTags[rule.OutboundTag] {
				issues = append(issues, fmt.Sprintf("%s: outboundTag %q not declared in any 04_outbounds*.json",
					filepath.Base(f), rule.OutboundTag))
			}
		}
	}
	return issues
}
