import { writable } from 'svelte/store';

export const isServiceRestarting = writable<boolean>(false);

let restartTimer: ReturnType<typeof setTimeout> | null = null;

/**
 * Activates the service restart grace period.
 * While active, temporary polling failures and unreachable Mihomo API states
 * do not trigger ApiOffline banners or badge blinks.
 *
 * @param durationMs Duration in ms (default: 6000)
 */
export function activateRestartGrace(durationMs: number = 6000): void {
  isServiceRestarting.set(true);

  if (restartTimer) {
    clearTimeout(restartTimer);
    restartTimer = null;
  }

  restartTimer = setTimeout(() => {
    isServiceRestarting.set(false);
    restartTimer = null;
  }, durationMs);
}

/**
 * Manually clears the service restart grace period.
 * Called when a successful health check confirms the service is up.
 */
export function clearRestartGrace(): void {
  if (restartTimer) {
    clearTimeout(restartTimer);
    restartTimer = null;
  }
  isServiceRestarting.set(false);
}
