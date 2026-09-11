// Package xtables detects and manages xtables CLI argument dialects
// across different iptables versions and router firmware variants.
package xtables

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	mu       sync.Mutex
	cached   = make(map[string][]string)
	haveGood = make(map[string]bool)
)

// candidateWaitArgs defines the probe ladder from most capable to most compatible.
var candidateWaitArgs = [][]string{
	{"-w", "5"},
	{"-w"},
	{},
}

// argParseFailureMarkers are output substrings that indicate the CLI rejected
// the wait flag arguments (unsupported dialect), allowing fallback to the next candidate.
var argParseFailureMarkers = []string{
	"Bad argument",
	"unknown option",
	"unrecognized option",
	"Try `iptables -h'",
	"Try `iptables --help'",
	"Try 'iptables -h'",
	"Try 'iptables --help'",
}

const (
	probeTimeout = 5 * time.Second
)

// WaitArgs probes and returns the xtables lock-waiting argument slice supported
// by the host's iptables binary. Successful results are cached for the lifetime
// of the process. Failures (e.g. lock contention or missing binary) are not cached
// and return an empty slice for the current call only.
func WaitArgs(ctx context.Context) []string {
	return WaitArgsFor(ctx, "iptables")
}

// WaitArgsFor probes and returns the xtables lock-waiting argument slice supported
// by the specified binary (e.g. "iptables", "ip6tables"). Successful results are
// cached per binary for the lifetime of the process.
func WaitArgsFor(ctx context.Context, probeBinary string) []string {
	if probeBinary == "" {
		probeBinary = "iptables"
	}
	mu.Lock()
	defer mu.Unlock()
	if haveGood[probeBinary] {
		return append([]string{}, cached[probeBinary]...)
	}

	for _, args := range candidateWaitArgs {
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		full := append(append([]string{}, args...), "-t", "mangle", "-n", "-L")
		out, err := exec.CommandContext(probeCtx, probeBinary, full...).CombinedOutput()
		cancel()

		if err == nil {
			cached[probeBinary] = append([]string{}, args...)
			haveGood[probeBinary] = true
			return append([]string{}, cached[probeBinary]...)
		}

		if IsCommandNotFound(err) {
			log.Printf("xtables: %s not found on PATH; using most compatible variant for this call only (not cached)", probeBinary)
			return []string{}
		}

		if isLockBusy(string(out)) {
			// Honest failure: xtables lock busy — do not degrade the
			// dialect and do not cache; caller gets the most compatible
			// variant for this call only (D-06).
			log.Printf("xtables: lock busy during probe of %s %v: %v (%s); using most compatible variant for this call only (not cached)",
				probeBinary, args, err, strings.TrimSpace(string(out)))
			return []string{}
		}

		// Otherwise: probe failed due to unsupported dialect (e.g. 1.4.21 rejecting
		// wait-seconds argument or treating it as chain name). Try next candidate.
		log.Printf("xtables: probe %s %v failed (%v: %s); trying next candidate",
			probeBinary, args, err, strings.TrimSpace(string(out)))
		continue
	}

	log.Printf("xtables: probe exhausted without success; using most compatible variant for this call only (not cached)")
	return []string{}
}

// isLockBusy checks if the command output indicates that xtables lock is currently held by another process.
func isLockBusy(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "xtables lock") || strings.Contains(lower, "holding the xtables lock")
}

// isArgParseFailure checks if the command output indicates an argument parsing failure.
func isArgParseFailure(output string) bool {
	for _, m := range argParseFailureMarkers {
		if strings.Contains(output, m) {
			return true
		}
	}
	return false
}

// IsCommandNotFound reports whether err comes from exec failing to locate
// the binary on PATH or at an absolute path (as opposed to the binary running and failing).
func IsCommandNotFound(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.Is(execErr.Err, exec.ErrNotFound)
	}
	return errors.Is(err, os.ErrNotExist)
}

// ResetForTest clears the process-wide cache so tests can re-probe against
// freshly installed fake binaries. Must only be called from _test.go files.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	haveGood = make(map[string]bool)
	cached = make(map[string][]string)
}
