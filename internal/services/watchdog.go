package services

import (
	"context"
	"encoding/json"
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

	ticker := time.NewTicker(watchdogCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.CheckHealth()
			if issues := ValidateXrayRoutingTags(w.xrayDir); len(issues) > 0 {
				log.Printf("Watchdog: Xray routing validation found %d issue(s): %s", len(issues), strings.Join(issues, "; "))
			}
		case <-w.stopCh:
			return
		}
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

	if w.consecutiveFailures >= watchdogMaxFailures && !w.disarmed {
		w.disarmed = true
		log.Printf("Watchdog: %d consecutive kernel failures — triggering emergency TPROXY disarm", w.consecutiveFailures)
		go w.EmergencyDisarmTProxy()
	}
}

// ConsecutiveFailures returns the current failure streak (for diagnostics/tests).
func (w *WatchdogService) ConsecutiveFailures() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.consecutiveFailures
}

// EmergencyDisarmTProxy removes the XKEEN_TPROXY interception rule from
// iptables mangle/PREROUTING so LAN devices regain direct internet access
// when the proxy kernel has failed watchdogMaxFailures consecutive health
// checks (STAB-05). This is invoked directly (not via the xkeen binary)
// because the XKeen binary itself may be the thing that's wedged.
func (w *WatchdogService) EmergencyDisarmTProxy() {
	// iptables -D removes a single matching rule per invocation; loop until
	// no more matches are found (bounded, so a misbehaving iptables can't
	// wedge this goroutine forever).
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "iptables", "-t", "mangle", "-D", "PREROUTING", "-j", "XKEEN_TPROXY")
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			if i == 0 {
				log.Printf("Watchdog: EmergencyDisarmTProxy: no XKEEN_TPROXY rule removed (already absent or iptables unavailable): %v — %s",
					err, strings.TrimSpace(string(out)))
			}
			return
		}
		log.Printf("Watchdog: removed XKEEN_TPROXY interception rule from mangle/PREROUTING (iteration %d)", i+1)
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
