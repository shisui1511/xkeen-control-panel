import { get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { serviceAction } from './serviceControl';
import { kernelLabel } from './kernelState';
import { showToast, fetchCapabilities } from '../stores';
import { activateRestartGrace } from './serviceGrace';
import { parseValidationError } from './errorParser';
import { currentLang, t } from '../i18n';

/**
 * Client of the server-side "apply to kernel" action
 * (POST /api/service/control?action=apply). The server decides whether the
 * target kernel is restarted (active and running) and reports the outcome;
 * the toast is chosen from that outcome, never guessed on the client.
 */

export type ApplyOutcome =
  | 'restarted'
  | 'saved_kernel_stopped'
  | 'saved_kernel_inactive'
  | 'saved_kernel_conflict'
  | 'restart_failed'
  | 'unknown';

export interface ApplyResult {
  outcome: ApplyOutcome;
  kernel: string;
  active_kernel?: string;
  active_running?: boolean;
  error?: string;
}

export type ApplyTarget = { kernel: 'xray' | 'mihomo' | 'active' } | { path: string };

const KNOWN_OUTCOMES: ReadonlySet<string> = new Set([
  'restarted',
  'saved_kernel_stopped',
  'saved_kernel_inactive',
  'saved_kernel_conflict',
  'restart_failed'
]);

function tr(key: string, params?: Record<string, string | number>): string {
  return get(t)(key, params);
}

/** Capabilities' prediction: true only when Apply will really restart the kernel. */
export function willRestartOnApply(
  caps: { apply_restarts?: Record<string, boolean> } | null | undefined,
  kernel: string
): boolean {
  return caps?.apply_restarts?.[kernel] === true;
}

/**
 * Applies already written files to the kernel. HTTP errors are rethrown:
 * the caller shows its own error (the files are already on disk by then).
 */
export async function applyToKernel(target: ApplyTarget): Promise<ApplyResult> {
  // The server restarts synchronously (up to 45 s); background status polls
  // must not flood errors meanwhile. If no restart happens the window expires.
  activateRestartGrace(6000);
  const query =
    'kernel' in target
      ? `kernel=${encodeURIComponent(target.kernel)}`
      : `path=${encodeURIComponent(target.path)}`;
  const data = await apiFetchJSON<Partial<ApplyResult> | null>(
    `/api/service/control?action=apply&${query}`,
    { method: 'POST' }
  );
  const raw = typeof data?.outcome === 'string' ? data.outcome : '';
  return {
    outcome: KNOWN_OUTCOMES.has(raw) ? (raw as ApplyOutcome) : 'unknown',
    kernel: data?.kernel ?? ('kernel' in target ? target.kernel : ''),
    active_kernel: data?.active_kernel,
    active_running: data?.active_running,
    error: data?.error
  };
}

/** Explicit "start now" requested by the user from the toast action. */
export async function startKernelNow(): Promise<void> {
  try {
    await serviceAction('start');
    await fetchCapabilities();
  } catch (e: unknown) {
    const message = e instanceof Error ? e.message : String(e);
    showToast('error', tr('apply.start_failed', { error: message }));
  }
}

function restartFailureReason(result: ApplyResult): string {
  const raw = result.error?.trim() ?? '';
  if (!raw) return tr('apply.restart_failed_unknown');
  return parseValidationError(raw, get(currentLang)) || raw;
}

/** Shows the toast that matches the outcome the server actually reported. */
export function notifyApplyOutcome(
  result: ApplyResult,
  opts: { restartedMessage?: string } = {}
): void {
  switch (result.outcome) {
    case 'restarted':
      showToast('success', opts.restartedMessage ?? tr('apply.restarted'));
      break;
    case 'saved_kernel_stopped':
      showToast('info', tr('apply.saved_kernel_stopped'), 8000, {
        label: tr('apply.start_now'),
        onClick: () => {
          void startKernelNow();
        }
      });
      break;
    case 'saved_kernel_inactive':
      showToast('info', tr('apply.saved_kernel_inactive', { kernel: kernelLabel(result.kernel) }));
      break;
    case 'saved_kernel_conflict':
      showToast('error', tr('apply.saved_kernel_conflict'), 8000);
      break;
    case 'restart_failed':
      showToast(
        'error',
        tr('apply.restart_failed', { reason: restartFailureReason(result) }),
        10000,
        {
          label: tr('apply.open_logs'),
          onClick: () => {
            window.location.hash = '#/logs';
          }
        }
      );
      break;
    default:
      showToast('success', tr('app.saved'));
  }
}
