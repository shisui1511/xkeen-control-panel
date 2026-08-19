import { onDestroy } from 'svelte';

export interface PollerOptions {
  /**
   * Interval in ms to use when document.hidden is true and allowBackground is true.
   * Default: 30000 ms.
   */
  backgroundIntervalMs?: number;
  /**
   * If true, polling will continue at backgroundIntervalMs when the page is hidden.
   * If false (default), polling is completely paused when hidden.
   */
  allowBackground?: boolean;
  /**
   * If true (default), triggers an immediate poll execution upon initialization.
   */
  immediate?: boolean;
}

export interface PollerControls {
  refetch: () => Promise<void>;
  stop: () => void;
  pause: () => void;
  resume: () => void;
  isPaused: () => boolean;
}

/**
 * Svelte 5 friendly poller helper with visibility handling, AbortController cancellation,
 * exponential backoff, pause/resume capabilities, and automatic cleanup on unmount.
 * Session-expiry handling is centralized in lib/api.ts's apiFetch — this poller has no
 * auth-status-specific logic.
 */
export function usePoller(
  pollFn: (signal: AbortSignal) => Promise<void>,
  activeIntervalMs: number,
  options: PollerOptions = {}
): PollerControls {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let inFlightController: AbortController | null = null;
  let backoffMs = activeIntervalMs;
  let isStopped = false;
  let isPaused = false;

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  async function executePoll() {
    if (isStopped || isPaused) return;
    clearTimer();

    if (inFlightController) {
      inFlightController.abort();
    }
    inFlightController = new AbortController();
    const signal = inFlightController.signal;

    try {
      await pollFn(signal);
      backoffMs = activeIntervalMs; // Reset backoff on success
    } catch (err: any) {
      if (err?.name === 'AbortError') return;
      // Session-expiry / auth errors are handled centrally by apiFetch
      // (logout+toast+redirect fires there before this catch runs) — no
      // special-casing needed here beyond falling through to the generic
      // backoff below.
      // Exponential backoff up to 60000 ms
      backoffMs = Math.min(backoffMs * 2, 60000);
    } finally {
      if (!isStopped && !isPaused) {
        scheduleNext();
      }
    }
  }

  function scheduleNext() {
    if (isStopped || isPaused) return;
    clearTimer();

    const isHidden = typeof document !== 'undefined' && document.hidden;
    if (isHidden && !options.allowBackground) {
      return; // Completely paused when tab is hidden
    }

    const interval = isHidden ? (options.backgroundIntervalMs ?? 30000) : backoffMs;

    timer = setTimeout(executePoll, interval);
  }

  function handleVisibilityChange() {
    if (isStopped || isPaused) return;
    if (!document.hidden) {
      // Tab became visible: immediately refetch and reset backoff
      backoffMs = activeIntervalMs;
      executePoll();
    } else if (!options.allowBackground) {
      // Tab hidden: cancel in-flight request and pause timer
      if (inFlightController) {
        inFlightController.abort();
        inFlightController = null;
      }
      clearTimer();
    }
  }

  if (typeof window !== 'undefined') {
    window.addEventListener('visibilitychange', handleVisibilityChange);
  }

  if (options.immediate !== false) {
    executePoll();
  }

  function pause() {
    if (isPaused || isStopped) return;
    isPaused = true;
    clearTimer();
    if (inFlightController) {
      inFlightController.abort();
      inFlightController = null;
    }
  }

  function resume() {
    if (!isPaused || isStopped) return;
    isPaused = false;
    backoffMs = activeIntervalMs;
    executePoll();
  }

  function stop() {
    isStopped = true;
    clearTimer();
    if (inFlightController) {
      inFlightController.abort();
      inFlightController = null;
    }
    if (typeof window !== 'undefined') {
      window.removeEventListener('visibilitychange', handleVisibilityChange);
    }
  }

  try {
    onDestroy(stop);
  } catch {
    // Graceful fallback if instantiated outside a component lifecycle
  }

  return {
    refetch: executePoll,
    stop,
    pause,
    resume,
    isPaused: () => isPaused
  };
}
