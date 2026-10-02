/**
 * serviceControl.ts — единый клиент POST /api/service/control: start/stop/restart,
 * остановка выбранного ядра. Тексты ошибок гейта ядра (409) переводятся здесь,
 * а не в компонентах (D-06). Имя ядра уходит в URL только как KernelName
 * через encodeURIComponent (T-140.1-20); изменяющие запросы идут через apiFetch
 * и получают X-CSRF-Token (T-140.1-22).
 */
import { get } from 'svelte/store';
import { apiFetch, apiFetchJSON } from './api';
import { activateRestartGrace } from './serviceGrace';
import { kernelGateMessage, readKernelGateMeta } from './kernelGateError';
import type { KernelName } from './kernelState';
import { t } from '../i18n';

export type { KernelName } from './kernelState';

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
  const res = await apiFetch(`/api/service/control?action=${action}`, { method: 'POST' });
  if (!res.ok) throw await serviceErrorOf(res);
  return await res.text();
}
