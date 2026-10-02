import { get } from 'svelte/store';
import { apiFetchJSON } from './api';
import { serviceAction } from './serviceControl';
import { kernelLabel } from './kernelState';
import { showToast, fetchCapabilities } from '../stores';
import { activateRestartGrace, clearRestartGrace } from './serviceGrace';
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
  /** Клиентский исход: замок жизненного цикла занят чужой операцией; сервер его не шлёт. */
  | 'saved_op_in_progress'
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
 * Applies already written files to the kernel. The files are on disk by then,
 * so two gate refusals are returned as outcomes instead of being thrown (D-06:
 * the machine code becomes a text in one helper, not in each caller):
 * - 409 `kernel_conflict` becomes `saved_kernel_conflict` (D-02: the kernel is
 *   deliberately not restarted while both are running);
 * - 409 `kernel_op_in_progress` becomes `saved_op_in_progress` (another kernel
 *   operation holds the lifecycle lock; its restart window is left alone).
 * Any other HTTP error is rethrown for the caller to show. The restart window
 * is dropped whenever no restart of ours is under way, so the conflict banner
 * is not hidden behind it.
 */
export async function applyToKernel(target: ApplyTarget): Promise<ApplyResult> {
  // The server restarts synchronously (up to 45 s); background status polls
  // must not flood errors meanwhile. If no restart happens the window expires.
  activateRestartGrace(6000);
  const query =
    'kernel' in target
      ? `kernel=${encodeURIComponent(target.kernel)}`
      : `path=${encodeURIComponent(target.path)}`;
  const kernel = 'kernel' in target && target.kernel !== 'active' ? target.kernel : '';
  let data: Partial<ApplyResult> | null;
  try {
    data = await apiFetchJSON<Partial<ApplyResult> | null>(
      `/api/service/control?action=apply&${query}`,
      { method: 'POST' }
    );
  } catch (e: unknown) {
    const code = (e as { code?: unknown } | null)?.code;
    if ((e as { status?: unknown } | null)?.status === 409) {
      if (code === 'kernel_conflict') {
        clearRestartGrace();
        return {
          outcome: 'saved_kernel_conflict',
          kernel,
          active_kernel: 'both',
          active_running: true
        };
      }
      if (code === 'kernel_op_in_progress') {
        // The window belongs to the other running operation: do not touch it.
        return { outcome: 'saved_op_in_progress', kernel };
      }
    }
    clearRestartGrace();
    throw e;
  }
  const raw = typeof data?.outcome === 'string' ? data.outcome : '';
  const outcome = KNOWN_OUTCOMES.has(raw) ? (raw as ApplyOutcome) : 'unknown';
  // No restart happened, so there is no transitional state to hide.
  if (outcome !== 'restarted') clearRestartGrace();
  return {
    outcome,
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
    case 'saved_op_in_progress':
      showToast('warning', tr('apply.saved_op_in_progress'), 8000);
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
