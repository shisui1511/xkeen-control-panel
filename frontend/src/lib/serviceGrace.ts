import { writable } from 'svelte/store';

export const isServiceRestarting = writable<boolean>(false);

let restartTimer: ReturnType<typeof setTimeout> | null = null;
/** Момент окончания идущего окна (мс, Date.now()); 0 — окна нет. */
let graceUntil = 0;

/**
 * Activates the service restart grace period.
 * While active, temporary polling failures and unreachable Mihomo API states
 * do not trigger ApiOffline banners or badge blinks.
 *
 * A new activation extends a running window but never shortens it: an apply
 * started during a kernel switch (20 s) must not cut the switch window down
 * to its own 6 s. Use clearRestartGrace() to end a window early.
 *
 * @param durationMs Duration in ms (default: 6000)
 */
export function activateRestartGrace(durationMs: number = 6000): void {
  const until = Date.now() + durationMs;
  if (restartTimer && until <= graceUntil) {
    isServiceRestarting.set(true);
    return;
  }
  isServiceRestarting.set(true);

  if (restartTimer) {
    clearTimeout(restartTimer);
    restartTimer = null;
  }

  graceUntil = until;
  restartTimer = setTimeout(() => {
    isServiceRestarting.set(false);
    restartTimer = null;
    graceUntil = 0;
  }, durationMs);
}

/**
 * Manually clears the service restart grace period.
 * Called when a successful health check confirms the service is up, and when
 * an operation ends without a restart (refusal, error, outcome without restart)
 * so the conflict banner is not hidden behind a stale window.
 */
export function clearRestartGrace(): void {
  if (restartTimer) {
    clearTimeout(restartTimer);
    restartTimer = null;
  }
  graceUntil = 0;
  isServiceRestarting.set(false);
}
