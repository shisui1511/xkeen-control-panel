/**
 * serviceControl.ts — единый клиент POST /api/service/control: start/stop/restart,
 * остановка выбранного ядра. Тексты ошибок гейта ядра (409) переводятся здесь,
 * а не в компонентах (D-06). Имя ядра уходит в URL только как KernelName
 * через encodeURIComponent (T-140.1-20); изменяющие запросы идут через apiFetch
 * и получают X-CSRF-Token (T-140.1-22).
 */
import { get } from 'svelte/store';
import { apiFetch, apiFetchJSON } from './api';
import { activateRestartGrace, clearRestartGrace } from './serviceGrace';
import { kernelGateMessage, readKernelGateMeta } from './kernelGateError';
import { KERNEL_NAMES, kernelLabel, type KernelName } from './kernelState';
import { activeKernelName, fetchCapabilities, isConflict, showConfirm, showToast } from '../stores';
import { t } from '../i18n';

export type { KernelName } from './kernelState';

export type SwitchOutcome = 'switched' | 'old_still_running' | 'new_not_started';

export interface SwitchResult {
  outcome: SwitchOutcome;
  /** Прежнее ядро; 'none', если до переключения не работало ни одно. */
  old: string;
  new: KernelName;
  old_running: boolean;
  new_running: boolean;
  output: string;
}

const SWITCH_OUTCOMES: ReadonlySet<string> = new Set([
  'switched',
  'old_still_running',
  'new_not_started'
]);

/**
 * Окно перезапуска на время переключения: не меньше дедлайна опроса бэкенда (15 с)
 * плюс запас, иначе баннер конфликта и ApiOffline вспыхнут посреди легального переключения.
 * После исхода `switched` оно снимается (см. API_WARMUP_GRACE_MS), иначе конфликт,
 * возникший сразу после переключения, был бы скрыт до 20 с.
 */
const SWITCH_GRACE_MS = 20000;

/**
 * Окно прогрева API Mihomo после `switched`: процесс подтверждён по PID, осталось
 * дождаться ответа API. `fetchCapabilities` снимет окно раньше при api_reachable.
 */
const API_WARMUP_GRACE_MS = 6000;

/** 409 kernel_op_in_progress: окно принадлежит чужой идущей операции, его не трогаем. */
function isOpInProgress(e: unknown): boolean {
  return (e as { code?: unknown } | null)?.code === 'kernel_op_in_progress';
}

export type StopKernelOutcome = 'stopped' | 'not_running' | 'still_running';

export interface StopKernelResult {
  kernel: KernelName;
  outcome: StopKernelOutcome;
  method?: 'signal' | 'xkeen';
}

const STOP_OUTCOMES: ReadonlySet<string> = new Set(['stopped', 'not_running', 'still_running']);

/**
 * Останавливает процесс выбранного ядра. Ядро остаётся в ответе сервера как есть;
 * ошибки запроса уже переведены apiFetchJSON (409 гейта, текст конверта).
 */
export async function stopKernelProcess(kernel: KernelName): Promise<StopKernelResult> {
  const data = await apiFetchJSON<Partial<StopKernelResult> | null>(
    `/api/service/control?action=stop&kernel=${encodeURIComponent(kernel)}`,
    { method: 'POST' }
  );
  const outcome = typeof data?.outcome === 'string' ? data.outcome : '';
  if (!STOP_OUTCOMES.has(outcome)) throw new Error('Invalid stop response');
  return {
    kernel,
    outcome: outcome as StopKernelOutcome,
    method: data?.method
  };
}

/**
 * Подтверждение остановки выбранного ядра в конфликте (D-02). Один и тот же диалог
 * для баннера и карточки дашборда; `true` — пользователь согласен.
 */
export function confirmKernelStop(kernel: KernelName): Promise<boolean> {
  const tr = get(t);
  const label = kernelLabel(kernel);
  return showConfirm({
    title: tr('kernel.conflict_stop_confirm_title', { kernel: label }),
    message: tr('kernel.conflict_stop_confirm_msg', { kernel: label }),
    objectName: label,
    confirmLabel: tr('kernel.conflict_stop', { kernel: label }),
    cancelLabel: tr('app.cancel'),
    variant: 'warning'
  });
}

/**
 * Останавливает выбранное ядро в конфликте и сообщает проверенный итог тостом (D-04):
 * после остановки перечитывает capabilities и по ним решает, снят ли конфликт.
 * Не бросает: ошибка запроса (кроме 401, который обрабатывает общий клиент) — error-тост.
 */
export async function stopKernelAndReport(kernel: KernelName): Promise<void> {
  const tr = get(t);
  try {
    const result = await stopKernelProcess(kernel);
    await fetchCapabilities();
    if (result.outcome === 'still_running') {
      showToast('error', tr('kernel.stop_still_running', { kernel: kernelLabel(kernel) }), 10000);
    } else if (!get(isConflict)) {
      const other = KERNEL_NAMES.find((k) => k !== kernel) ?? kernel;
      const active = get(activeKernelName) || other;
      showToast('success', tr('kernel.conflict_resolved', { kernel: kernelLabel(active) }));
    } else {
      // Процесс остановлен, но второй снова виден: нужен повтор или логи.
      showToast('error', tr('kernel.conflict_not_resolved'), 10000);
    }
  } catch (e: unknown) {
    if ((e as { status?: number } | null)?.status === 401) return;
    showToast('error', e instanceof Error ? e.message : String(e), 10000);
  }
}

/** Ошибка из ответа без конверта apiFetchJSON: гейт ядра, поле error, текст, статус. */
async function serviceErrorOf(res: Response): Promise<Error> {
  const meta = await readKernelGateMeta(res);
  const gate = kernelGateMessage(meta, get(t));
  if (meta && gate) {
    const err: any = new Error(gate);
    err.status = res.status;
    err.code = meta.code;
    return err;
  }
  const raw = await res.text().catch(() => '');
  let message = raw.trim();
  try {
    const body = JSON.parse(raw);
    if (body && typeof body === 'object' && typeof body.error === 'string' && body.error) {
      message = body.error;
    }
  } catch {
    // не JSON: остаётся текст ответа
  }
  const err: any = new Error(message || `HTTP ${res.status}`);
  err.status = res.status;
  return err;
}

/** start / stop / restart активного ядра. Успех — текст ответа сервера. */
export async function serviceAction(action: 'start' | 'stop' | 'restart'): Promise<string> {
  if (action !== 'stop') {
    // Сервер перезапускает синхронно; фоновые опросы в это время не шумят.
    activateRestartGrace(6000);
  }
  try {
    const res = await apiFetch(`/api/service/control?action=${action}`, { method: 'POST' });
    if (!res.ok) throw await serviceErrorOf(res);
    return await res.text();
  } catch (e) {
    // Отказ или сбой: перезапуска нет, переходного состояния скрывать не нужно
    if (action !== 'stop' && !isOpInProgress(e)) clearRestartGrace();
    throw e;
  }
}

/**
 * Переключает XKeen на ядро и запускает его. Единственный клиент switch_kernel:
 * тип исхода приходит от сервера (D-04), текстовый ответ не поддерживается.
 * При исходе не `switched` и при ошибке окно перезапуска снимается, чтобы баннер
 * конфликта (old_still_running) появился сразу; исключение — 409 kernel_op_in_progress
 * (окно чужой операции). После `switched` окно снимается; для Mihomo вместо него
 * открывается короткое окно прогрева API.
 */
export async function switchKernel(kernel: KernelName): Promise<SwitchResult> {
  activateRestartGrace(SWITCH_GRACE_MS);
  try {
    const data = await apiFetchJSON<Partial<SwitchResult> | null>(
      `/api/service/control?action=switch_kernel&kernel=${encodeURIComponent(kernel)}`,
      { method: 'POST' }
    );
    const outcome = typeof data?.outcome === 'string' ? data.outcome : '';
    if (!data || !SWITCH_OUTCOMES.has(outcome)) throw new Error('Invalid switch response');
    clearRestartGrace();
    if (outcome === 'switched' && kernel === 'mihomo') activateRestartGrace(API_WARMUP_GRACE_MS);
    return {
      outcome: outcome as SwitchOutcome,
      old: typeof data.old === 'string' ? data.old : 'none',
      new: kernel,
      old_running: data.old_running === true,
      new_running: data.new_running === true,
      output: typeof data.output === 'string' ? data.output : ''
    };
  } catch (e) {
    if (!isOpInProgress(e)) clearRestartGrace();
    throw e;
  }
}

/** Тост по типизированному исходу переключения (UI-SPEC «Исходы SwitchKernel»). */
export function notifySwitchOutcome(result: SwitchResult): void {
  const tr = get(t);
  const target = kernelLabel(result.new);
  switch (result.outcome) {
    case 'switched':
      showToast('success', tr('kernel.switch_ok', { kernel: target }), 4000);
      break;
    case 'old_still_running':
      showToast(
        'warning',
        tr('kernel.switch_old_running', { old: kernelLabel(result.old) }),
        10000
      );
      break;
    case 'new_not_started': {
      const message =
        result.old === 'none'
          ? tr('kernel.switch_new_not_started_plain', { new: target })
          : tr('kernel.switch_new_not_started', { new: target, old: kernelLabel(result.old) });
      showToast('error', message, 10000, {
        label: tr('apply.open_logs'),
        onClick: () => {
          window.location.hash = '#/logs';
        }
      });
      break;
    }
  }
}
