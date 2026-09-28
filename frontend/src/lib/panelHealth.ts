import { get, writable } from 'svelte/store';
import { panelUnreachable } from '../stores';

/**
 * panelNewVersion — set once the panel comes back reachable with a
 * `panel_version` different from the one the page was loaded with (D-20).
 * Stays set until the user reloads (never auto-resets) so the "reload
 * page" prompt does not silently disappear.
 */
export const panelNewVersion = writable<string | null>(null);

// Backoff ladder for /api/version polling while the panel is unreachable
// (D-20 cadence from UI-SPEC §5): 2s, 4s, 8s, 16s, then a steady 30s.
const RETRY_DELAYS_MS = [2000, 4000, 8000, 16000, 30000];

let knownVersion: string | null = null;
let pollTimer: ReturnType<typeof setTimeout> | null = null;
let pollAttempt = 0;

function delayForAttempt(attempt: number): number {
  return RETRY_DELAYS_MS[Math.min(attempt, RETRY_DELAYS_MS.length - 1)];
}

/**
 * rememberPanelVersion — records the first non-empty panel_version seen
 * (the version the page was actually built/loaded with). Later calls with a
 * different value are ignored: the baseline never moves during a session.
 */
export function rememberPanelVersion(version: string | undefined | null): void {
  if (!knownVersion && version) {
    knownVersion = version;
  }
}

async function probe(): Promise<void> {
  try {
    // Must NOT go through apiFetch here: apiFetch's own network-failure
    // branch calls reportPanelUnreachable(), which would re-enter this
    // probe loop.
    // eslint-disable-next-line no-restricted-syntax
    const res = await fetch('/api/version', { cache: 'no-store' });
    if (res.ok) {
      let data: { panel_version?: string } | null = null;
      try {
        data = await res.json();
      } catch {
        // non-JSON success body: treat as reachable, skip version comparison
      }
      const version = data?.panel_version;
      if (knownVersion && version && version !== knownVersion) {
        panelNewVersion.set(version);
      }
      pollAttempt = 0;
      pollTimer = null;
      panelUnreachable.set(false);
      return;
    }
  } catch {
    // network still down — fall through to the next retry
  }
  pollAttempt++;
  pollTimer = setTimeout(probe, delayForAttempt(pollAttempt));
}

/**
 * reportPanelUnreachable — idempotently flips panelUnreachable to true and
 * (re)starts the backoff polling loop. Safe to call from every failed
 * apiFetch: a second call while already unreachable is a no-op, so
 * concurrent requests never spawn duplicate polling cycles.
 */
export function reportPanelUnreachable(): void {
  if (get(panelUnreachable)) return;
  panelUnreachable.set(true);
  pollAttempt = 0;
  pollTimer = setTimeout(probe, delayForAttempt(pollAttempt));
}

/**
 * confirmPanelReachable — the same reset performed by probe()'s success
 * branch, exposed for callers that already know the panel answered (a 401
 * response, for instance): cancels any pending backoff poll started by an
 * earlier network failure so it doesn't fire a redundant /api/version
 * request after reachability was already confirmed by other means
 * (134-REVIEW IN-01).
 */
export function confirmPanelReachable(): void {
  if (pollTimer) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
  pollAttempt = 0;
  panelUnreachable.set(false);
}

/** Test-only reset: clears timers/state between unit test cases. */
export function __resetPanelHealthForTests(): void {
  if (pollTimer) {
    clearTimeout(pollTimer);
    pollTimer = null;
  }
  pollAttempt = 0;
  knownVersion = null;
  panelUnreachable.set(false);
  panelNewVersion.set(null);
}
