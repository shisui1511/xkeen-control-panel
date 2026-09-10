// Package xtables detects and manages xtables CLI argument dialects
// across different iptables versions and router firmware variants.
package xtables

import (
	"context"
	"errors"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	mu       sync.Mutex
	cached   []string
	haveGood bool
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
	probeBinary  = "iptables"
)

// WaitArgs probes and returns the xtables lock-waiting argument slice supported
// by the host's iptables binary. Successful results are cached for the lifetime
// of the process. Failures (e.g. lock contention or missing binary) are not cached
// and return an empty slice for the current call only.
func WaitArgs(ctx context.Context) []string {
	mu.Lock()
	defer mu.Unlock()
	if haveGood {
		return append([]string{}, cached...)
	}

	for _, args := range candidateWaitArgs {
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		full := append(append([]string{}, args...), "-t", "mangle", "-n", "-L")
		out, err := exec.CommandContext(probeCtx, probeBinary, full...).CombinedOutput()
		cancel()

		if err == nil {
			cached = append([]string{}, args...)
			haveGood = true
			return append([]string{}, cached...)
		}

		if isCommandNotFound(err) {
			log.Printf("xtables: %s not found on PATH; using most compatible variant for this call only (not cached)", probeBinary)
			return []string{}
		}

		if !isArgParseFailure(string(out)) {
			// Honest failure (e.g. xtables lock busy) — do not degrade the
			// dialect and do not cache; caller gets the most compatible
			// variant for this call only (D-06).
			log.Printf("xtables: probe failed with non-argument error: %v (%s); not degrading dialect and not caching",
				err, strings.TrimSpace(string(out)))
			return []string{}
		}
	}

	log.Printf("xtables: probe exhausted without success; using most compatible variant for this call only (not cached)")
	return []string{}
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

// isCommandNotFound reports whether err comes from exec failing to locate
// the binary on PATH (as opposed to the binary running and failing).
func isCommandNotFound(err error) bool {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return errors.Is(execErr.Err, exec.ErrNotFound)
	}
	return false
}

// ResetForTest clears the process-wide cache so tests can re-probe against
// freshly installed fake binaries. Must only be called from _test.go files.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	haveGood = false
	cached = nil
}
