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
	"github.com/shisui1511/xkeen-control-panel/internal/utils/xtables"
)

// watchdogCheckInterval is how often the watchdog polls kernel health.
const watchdogCheckInterval = 30 * time.Second

// watchdogMaxFailures is the number of consecutive failed health checks
// before the watchdog trips the emergency TPROXY disarm (STAB-05).
const watchdogMaxFailures = 3

// disarmedRecheckInterval is the number of unhealthy CheckHealth cycles
// between non-destructive mangle-table re-checks while w.disarmed is latched
// true (WR-03). A restart-loop (e.g. a supervisor repeatedly restarting a
// crashing XKeen, which reinstalls its TPROXY rule on every restart) can
// leave the kernel permanently unhealthy without ever reporting a recovery,
// which is the only event that currently clears the latch — without a
// periodic re-check the watchdog would stay silently latched disarmed even
// after interception returns. At the default 30s check interval this is a
// ~2.5 minute cadence.
const disarmedRecheckInterval = 5

// watchdogResetCooldown is the minimum interval between manual watchdog resets.
const watchdogResetCooldown = 5 * time.Second

// watchdogBackoffGrid defines the retry backoff sequence after failed disarm attempts (D-10, D-35).
var watchdogBackoffGrid = []time.Duration{
	30 * time.Second,
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

const (
	// watchdogMaxDisarmAttempts is the maximum number of consecutive failed
	// disarm attempts before entering degraded state (D-11).
	watchdogMaxDisarmAttempts = 5

	// watchdogStartGracePeriod is the window after Start() during which
	// emergency disarm is suppressed to allow cold boot / deployment (D-05, D-40).
	watchdogStartGracePeriod = 90 * time.Second

	// watchdogDisarmTimeout is the overall context timeout for an EmergencyDisarmTProxy operation (D-14).
	watchdogDisarmTimeout = 20 * time.Second
)

// Sentinel errors returned by TryReset.
var (
	ErrWatchdogResetInFlight = errors.New("watchdog disarm in flight")
	ErrWatchdogResetCooldown = errors.New("watchdog reset cooldown active")
)

// Watchdog state constants for API and UI.
const (
	WatchdogStateArmed    = "armed"
	WatchdogStateIdle     = "idle"
	WatchdogStateDegraded = "degraded"
	WatchdogStateDisarmed = "disarmed"
)

// WatchdogSnapshot captures the internal watchdog state at a single point in time.
type WatchdogSnapshot struct {
	State               string
	ConsecutiveFailures int
	DisarmAttempts      int
	LastDisarmError     string
	InterceptionActive  bool
	InterceptionFamily  string
	NextAttemptAt       time.Time
	DegradedAt          time.Time
}

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

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	now       func() time.Time
	startedAt time.Time

	mu                     sync.Mutex
	consecutiveFailures    int
	disarmed               bool
	disarmInFlight         bool
	disarmEpoch            uint64
	disarmedRecheckCounter int
	interceptionActive     bool
	interceptionFamily     string
	lastResetAt            time.Time
	disarmAttempts         int
	nextDisarmAttempt      time.Time
	degradedAt             time.Time
	lastDisarmError        string

	iptablesSaveBin  string
	iptablesBin      string
	ip6tablesSaveBin string
	ip6tablesBin     string
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
		now:       time.Now,
	}
}

// inGraceLocked reports whether the service is within its start grace period.
// Returns false if startedAt is zero (service not started via Start()).
// Must be called with w.mu held.
func (w *WatchdogService) inGraceLocked() bool {
	if w.startedAt.IsZero() {
		return false
	}
	return w.now().Sub(w.startedAt) < watchdogStartGracePeriod
}

// Start launches the background health-check loop.
func (w *WatchdogService) Start() {
	w.mu.Lock()
	w.startedAt = w.now()
	w.mu.Unlock()

	w.wg.Add(1)
	go w.loop()
}

// Stop signals the loop to exit and waits for it to finish. Safe to call multiple times.
func (w *WatchdogService) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
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
	negativeMarkers := []string{"not running", "не запущен", "незапущен", "не актив", "неактив", "не работает", "неработает", "остановлен", "stopped"}
	for _, m := range negativeMarkers {
		if strings.Contains(lower, m) {
			return false
		}
	}
	return strings.Contains(lower, "running") || strings.Contains(lower, "актив") || strings.Contains(lower, "запущен") || strings.Contains(lower, "работает")
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

	if healthy {
		if w.consecutiveFailures > 0 {
			log.Printf("Watchdog: kernel recovered after %d failed health check(s)", w.consecutiveFailures)
		}
		w.consecutiveFailures = 0
		w.disarmed = false
		w.disarmedRecheckCounter = 0
		w.disarmEpoch++
		w.interceptionActive = false
		w.interceptionFamily = ""
		w.mu.Unlock()
		return
	}

	w.consecutiveFailures++
	log.Printf("Watchdog: kernel health check failed (%d/%d): status=%q err=%v",
		w.consecutiveFailures, watchdogMaxFailures, strings.TrimSpace(status), err)

	// WR-03: while latched disarmed, the failure-threshold check below is
	// gated on "!w.disarmed" and so stays silent even if the interception
	// rule was reinstalled by an external mechanism without the kernel ever
	// reporting healthy again (the only event that currently clears the
	// latch). Periodically perform a non-destructive re-check of the mangle
	// table and unlatch if the rule is back, so the threshold check below
	// can trigger a fresh EmergencyDisarmTProxy cycle instead of never
	// noticing.
	recheckDue := false
	if w.disarmed && !w.disarmInFlight {
		w.disarmedRecheckCounter++
		if w.disarmedRecheckCounter >= disarmedRecheckInterval {
			w.disarmedRecheckCounter = 0
			recheckDue = true
		}
	}
	w.mu.Unlock()

	if recheckDue {
		v4, v6 := w.tproxyInterceptionFamilies()
		var family string
		switch {
		case v4 && v6:
			family = "ipv4+ipv6"
		case v4:
			family = "ipv4"
		case v6:
			family = "ipv6"
		default:
			family = ""
		}
		w.mu.Lock()
		w.interceptionActive = v4 || v6
		w.interceptionFamily = family
		if (v4 || v6) && w.disarmed && !w.disarmInFlight {
			log.Printf("Watchdog: TPROXY interception rule reappeared while latched disarmed — unlatching to allow a fresh disarm attempt")
			w.disarmed = false
		}
		w.mu.Unlock()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	canDisarm := w.consecutiveFailures >= watchdogMaxFailures &&
		!w.disarmed &&
		!w.disarmInFlight &&
		w.degradedAt.IsZero() &&
		!w.inGraceLocked() &&
		!w.now().Before(w.nextDisarmAttempt)

	if canDisarm {
		w.disarmInFlight = true
		epoch := w.disarmEpoch
		log.Printf("Watchdog: %d consecutive kernel failures — triggering emergency TPROXY disarm", w.consecutiveFailures)
		// Tracked by wg so Stop() (called during graceful shutdown/restart)
		// waits for an in-flight disarm sequence instead of abandoning it
		// mid-way through mutating iptables state.
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			outcome := w.EmergencyDisarmTProxy()
			w.mu.Lock()
			w.disarmInFlight = false
			// Latch only if the epoch has not changed while the disarm was in flight.
			// If the kernel recovered and subsequently failed again, this previous attempt's
			// outcome is stale and must not block the new failure cycle from disarming (CR-01).
			if epoch == w.disarmEpoch {
				if outcome != DisarmFailed {
					w.disarmed = true
					w.interceptionActive = false
					w.interceptionFamily = ""
					w.disarmAttempts = 0
					w.nextDisarmAttempt = time.Time{}
					w.lastDisarmError = ""
				} else {
					w.disarmed = false
					w.interceptionActive = true
					w.disarmAttempts++
					if w.disarmAttempts < watchdogMaxDisarmAttempts {
						w.nextDisarmAttempt = w.now().Add(watchdogBackoffGrid[w.disarmAttempts-1])
						log.Printf("Watchdog: EmergencyDisarmTProxy failed (attempt %d/%d) — next attempt in %v at %s",
							w.disarmAttempts, watchdogMaxDisarmAttempts, watchdogBackoffGrid[w.disarmAttempts-1], w.nextDisarmAttempt.Format(time.RFC3339))
					} else {
						gridIdx := w.disarmAttempts - 1
						if gridIdx >= len(watchdogBackoffGrid) {
							gridIdx = len(watchdogBackoffGrid) - 1
						}
						w.nextDisarmAttempt = w.now().Add(watchdogBackoffGrid[gridIdx])
					}
				}
			} else {
				log.Printf("Watchdog: EmergencyDisarmTProxy outcome %v discarded — health recovered and re-failed while attempt was in flight", outcome)
			}
			w.mu.Unlock()
			if outcome == DisarmFailed {
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

// stateLocked derives the current watchdog state from latches and timestamps.
// Must be called with w.mu held.
func (w *WatchdogService) stateLocked() string {
	if !w.degradedAt.IsZero() {
		return WatchdogStateDegraded
	}
	if w.disarmed {
		return WatchdogStateDisarmed
	}
	return WatchdogStateArmed
}

// Snapshot returns an atomic point-in-time snapshot of the watchdog state.
// It never spawns subprocesses or queries netfilter directly.
func (w *WatchdogService) Snapshot() WatchdogSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()

	return WatchdogSnapshot{
		State:               w.stateLocked(),
		ConsecutiveFailures: w.consecutiveFailures,
		DisarmAttempts:      w.disarmAttempts,
		LastDisarmError:     w.lastDisarmError,
		InterceptionActive:  w.interceptionActive,
		InterceptionFamily:  w.interceptionFamily,
		NextAttemptAt:       w.nextDisarmAttempt,
		DegradedAt:          w.degradedAt,
	}
}

// TryReset attempts an atomic manual reset of watchdog counters and latches.
// Rejects with ErrWatchdogResetInFlight if a disarm operation is currently running,
// or with ErrWatchdogResetCooldown if called within watchdogResetCooldown of the last reset.
// Records the reset action in XKeenService restart log and triggers an out-of-order health check.
func (w *WatchdogService) TryReset() (WatchdogSnapshot, error) {
	w.mu.Lock()
	if w.disarmInFlight {
		w.mu.Unlock()
		return WatchdogSnapshot{}, ErrWatchdogResetInFlight
	}

	if !w.lastResetAt.IsZero() && w.now().Sub(w.lastResetAt) < watchdogResetCooldown {
		w.mu.Unlock()
		return WatchdogSnapshot{}, ErrWatchdogResetCooldown
	}

	w.lastResetAt = w.now()
	w.consecutiveFailures = 0
	w.disarmAttempts = 0
	w.disarmedRecheckCounter = 0
	w.lastDisarmError = ""
	w.nextDisarmAttempt = time.Time{}
	w.degradedAt = time.Time{}
	w.disarmed = false
	w.disarmEpoch++

	snapshot := WatchdogSnapshot{
		State:               w.stateLocked(),
		ConsecutiveFailures: w.consecutiveFailures,
		DisarmAttempts:      w.disarmAttempts,
		LastDisarmError:     w.lastDisarmError,
		InterceptionActive:  w.interceptionActive,
		InterceptionFamily:  w.interceptionFamily,
		NextAttemptAt:       w.nextDisarmAttempt,
		DegradedAt:          w.degradedAt,
	}
	w.mu.Unlock()

	if w.xkeenSvc != nil {
		w.xkeenSvc.RecordAction("watchdog_reset", "", nil)
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.CheckHealth()
	}()

	return snapshot, nil
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

// DisarmOutcome represents the result of an EmergencyDisarmTProxy attempt.
type DisarmOutcome int

const (
	// DisarmFailed indicates at least one family could not be verified clean.
	DisarmFailed DisarmOutcome = iota
	// DisarmAlreadyClean indicates no TPROXY interception rules were present to disarm.
	DisarmAlreadyClean
	// DisarmDisarmed indicates interception rules were present, removed, and verified gone.
	DisarmDisarmed
)

func (o DisarmOutcome) String() string {
	switch o {
	case DisarmFailed:
		return "failed"
	case DisarmAlreadyClean:
		return "already-clean"
	case DisarmDisarmed:
		return "disarmed"
	default:
		return "unknown"
	}
}

// EmergencyDisarmTProxy removes the TPROXY interception rule(s) installed by
// XKeen from the iptables (and ip6tables, when present) mangle table so LAN
// devices regain direct internet access when the proxy kernel has failed
// watchdogMaxFailures consecutive health checks (STAB-05). This is invoked
// directly (not via the xkeen binary) because the XKeen binary itself may be
// the thing that's wedged.
//
// It reports a DisarmOutcome: DisarmDisarmed if rules were found and their removal
// was confirmed via re-reading the mangle table; DisarmAlreadyClean if no rules
// were found; or DisarmFailed if execution or verification failed.
func (w *WatchdogService) EmergencyDisarmTProxy() DisarmOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), watchdogDisarmTimeout)
	defer cancel()

	saveV4, delV4, saveV6, delV6 := w.resolveXtablesBins()

	waitArgsV4 := xtables.WaitArgsFor(ctx, delV4)
	waitArgsV6 := xtables.WaitArgsFor(ctx, delV6)

	removedV4, okV4, failV4 := disarmTProxyFamily(ctx, saveV4, delV4, waitArgsV4)
	removedV6, okV6, failV6 := disarmTProxyFamily(ctx, saveV6, delV6, waitArgsV6)

	var lastErr string
	switch {
	case !okV4 && !okV6:
		lastErr = fmt.Sprintf("IPv4 failed: %s, IPv6 failed: %s", failV4, failV6)
	case !okV4 && okV6:
		lastErr = fmt.Sprintf("IPv6 disarmed, IPv4 failed: %s", failV4)
	case okV4 && !okV6:
		lastErr = fmt.Sprintf("IPv4 disarmed, IPv6 failed: %s", failV6)
	default:
		lastErr = ""
	}

	w.mu.Lock()
	w.lastDisarmError = lastErr
	w.mu.Unlock()

	if !okV4 || !okV6 {
		log.Printf("Watchdog: EmergencyDisarmTProxy incomplete (ipv4 ok=%v removed=%d, ipv6 ok=%v removed=%d) — TPROXY interception may still be active",
			okV4, removedV4, okV6, removedV6)
		return DisarmFailed
	}

	if removedV4+removedV6 == 0 {
		log.Printf("Watchdog: EmergencyDisarmTProxy: no TPROXY interception rules found in mangle table (already absent or interception not installed)")
		return DisarmAlreadyClean
	}

	log.Printf("Watchdog: EmergencyDisarmTProxy removed %d TPROXY interception rule(s) (ipv4=%d, ipv6=%d)",
		removedV4+removedV6, removedV4, removedV6)
	return DisarmDisarmed
}

// resolveXtablesBins returns the iptables-save/iptables and ip6tables-save/
// ip6tables binary paths to use, falling back to the bare command names
// (resolved via PATH at exec time) when the service has no override
// configured (overrides are used by tests to point at fake binaries).
func (w *WatchdogService) resolveXtablesBins() (saveV4, delV4, saveV6, delV6 string) {
	saveV4 = w.iptablesSaveBin
	if saveV4 == "" {
		saveV4 = "iptables-save"
	}
	delV4 = w.iptablesBin
	if delV4 == "" {
		delV4 = "iptables"
	}
	saveV6 = w.ip6tablesSaveBin
	if saveV6 == "" {
		saveV6 = "ip6tables-save"
	}
	delV6 = w.ip6tablesBin
	if delV6 == "" {
		delV6 = "ip6tables"
	}
	return saveV4, delV4, saveV6, delV6
}

// tproxyInterceptionFamilies performs a non-destructive check of both IPv4 and IPv6
// mangle tables, returning whether TPROXY interception rules are present in each family.
func (w *WatchdogService) tproxyInterceptionFamilies() (v4, v6 bool) {
	ctx := context.Background()
	saveV4, _, saveV6, _ := w.resolveXtablesBins()

	check := func(saveBin string) bool {
		lines, err := listMangleRules(ctx, saveBin)
		if err != nil {
			return false
		}
		return len(selectTproxyRules(lines)) > 0
	}

	return check(saveV4), check(saveV6)
}

// tproxyRulePresent performs a non-destructive check of both mangle tables
// (iptables and, when present, ip6tables) for a live TPROXY interception
// rule, without deleting anything. Used by CheckHealth's periodic re-latch
// check (WR-03) to detect a rule that was reinstalled by an external
// mechanism (e.g. a supervisor restart-looping XKeen) while the watchdog was
// latched disarmed and the kernel never reported healthy again. A read
// failure for a given family is treated as "not confirmed reinstalled"
// rather than forcing a spurious unlatch on a transient error — a missing
// ip6tables (xtables.IsCommandNotFound) is expected on many router variants
// and must not be logged as one.
func (w *WatchdogService) tproxyRulePresent() bool {
	v4, v6 := w.tproxyInterceptionFamilies()
	return v4 || v6
}

// splitIptablesRule splits an iptables-save rule string into individual command-line
// arguments, correctly preserving arguments enclosed in single or double quotes
// (such as rule comments or complex match options).
func splitIptablesRule(line string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	var quoteChar rune
	escaped := false

	for _, r := range line {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		switch {
		case (r == '"' || r == '\'') && !inQuotes:
			inQuotes = true
			quoteChar = r
		case inQuotes && r == quoteChar:
			inQuotes = false
		case (r == ' ' || r == '\t') && !inQuotes:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	// WR-06: an unpaired quote (malformed iptables-save output, or manual
	// third-party edits to the rule set) would otherwise leave inQuotes
	// true and silently swallow the rest of the line — including spaces —
	// into a single argument, producing a malformed "-D ..." command that
	// either fails opaquely or, worse, could coincidentally match and
	// delete an unrelated rule. Refuse to guess: log and skip the line.
	if inQuotes {
		log.Printf("splitIptablesRule: unterminated quote in line, skipping: %q", line)
		return nil
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// listMangleRules lists all rule appending lines ("-A ") from the mangle table using saveBin.
func listMangleRules(ctx context.Context, saveBin string) ([]string, error) {
	saveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	out, err := exec.CommandContext(saveCtx, saveBin, "-t", "mangle").Output()
	cancel()
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "-A ") {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// isXkeenTproxyRule reports whether an iptables rule targets TPROXY or has the XKeen marker.
func isXkeenTproxyRule(line string, fields []string) bool {
	for i, f := range fields {
		if (f == "-j" || f == "-g") && i+1 < len(fields) && fields[i+1] == tproxyTarget {
			return true
		}
	}
	return strings.Contains(line, tproxyChainMarker)
}

// selectTproxyRules finds all rules in mangle lines that intercept traffic or jump into custom chains doing so.
func selectTproxyRules(lines []string) map[string]bool {
	toDelete := map[string]bool{} // dedup: a line could match both signals
	customChains := map[string]bool{}
	for _, line := range lines {
		fields := splitIptablesRule(line)
		if len(fields) < 2 {
			continue
		}
		chain := fields[1]
		if isXkeenTproxyRule(line, fields) {
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
			fields := splitIptablesRule(line)
			for i, f := range fields {
				if (f == "-j" || f == "-g") && i+1 < len(fields) && customChains[fields[i+1]] {
					toDelete[line] = true
					break
				}
			}
		}
	}
	return toDelete
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
func disarmTProxyFamily(ctx context.Context, saveBin, delBin string, waitArgs []string) (removed int, ok bool, failReason string) {
	lines, err := listMangleRules(ctx, saveBin)
	if err != nil {
		if xtables.IsCommandNotFound(err) {
			// This iptables family isn't present on this system — nothing to
			// disarm here, not a failure.
			return 0, true, ""
		}
		log.Printf("Watchdog: EmergencyDisarmTProxy: failed to list mangle table via %s: %v", saveBin, err)
		return 0, false, fmt.Sprintf("list mangle table via %s: %v", saveBin, err)
	}

	toDelete := selectTproxyRules(lines)
	if len(toDelete) == 0 {
		return 0, true, ""
	}

	var lastDelErr error
	deleteRules := func(rules map[string]bool) (deleted int, hasErr bool) {
		for line := range rules {
			args := splitIptablesRule(line)
			if len(args) == 0 {
				continue
			}
			args[0] = "-D" // "-A CHAIN ..." -> "-D CHAIN ..." removes exactly this rule
			delArgs := append(append([]string{}, waitArgs...), "-t", "mangle")
			delArgs = append(delArgs, args...)

			delCtx, delCancel := context.WithTimeout(ctx, 10*time.Second)
			delOut, delErr := exec.CommandContext(delCtx, delBin, delArgs...).CombinedOutput()
			delCancel()
			if delErr != nil {
				log.Printf("Watchdog: EmergencyDisarmTProxy: failed to remove rule via %s (%q): %v — %s",
					delBin, line, delErr, strings.TrimSpace(string(delOut)))
				hasErr = true
				lastDelErr = fmt.Errorf("%v (%s)", delErr, strings.TrimSpace(string(delOut)))
				continue
			}

			log.Printf("Watchdog: removed TPROXY interception rule via %s: %s", delBin, line)
			deleted++
		}
		return deleted, hasErr
	}

	// WR-02: iptables' "-D" removes exactly one physical match per invocation.
	// selectTproxyRules dedups by rule text, so if the mangle table holds 3+
	// physically identical copies of the same rule (e.g. XKeen looping
	// crash-restart re-installs its interception rule before the watchdog
	// catches up), a single deletion pass per unique rule text leaves
	// duplicates behind. Loop deletion+re-read passes — driven by whether
	// selectTproxyRules still finds a match, not a hardcoded pass count —
	// until the table is clean or maxDisarmPasses is reached (a hard ceiling
	// so a persistently re-installed rule can't spin this forever).
	const maxDisarmPasses = 5
	var hasErrAny bool
	pass := 0
	for {
		pass++
		delCount, hasErr := deleteRules(toDelete)
		removed += delCount
		hasErrAny = hasErrAny || hasErr

		lines, err := listMangleRules(ctx, saveBin)
		if err != nil {
			log.Printf("Watchdog: EmergencyDisarmTProxy: failed to re-read mangle table via %s after pass %d: %v", saveBin, pass, err)
			return removed, false, fmt.Sprintf("re-read mangle table via %s: %v", saveBin, err)
		}
		remaining := selectTproxyRules(lines)
		if len(remaining) == 0 {
			if hasErrAny {
				log.Printf("Watchdog: EmergencyDisarmTProxy: rule deletion encountered errors via %s, but re-read after pass %d confirmed mangle table clean", delBin, pass)
			} else if pass == 1 && delCount == 0 {
				log.Printf("Watchdog: EmergencyDisarmTProxy: TPROXY rules disappeared from mangle table before deletion via %s (cleared concurrently)", delBin)
			}
			return removed, true, ""
		}

		if pass >= maxDisarmPasses {
			log.Printf("Watchdog: EmergencyDisarmTProxy: %d interception rule(s) still remain after %d passes via %s",
				len(remaining), pass, delBin)
			reason := fmt.Sprintf("%d rule(s) remain after %d passes", len(remaining), pass)
			if lastDelErr != nil {
				reason += fmt.Sprintf(" (%v)", lastDelErr)
			}
			return removed, false, reason
		}

		log.Printf("Watchdog: EmergencyDisarmTProxy: %d interception rule(s) remain after pass %d — retrying via %s",
			len(remaining), pass, delBin)
		toDelete = remaining
	}
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
