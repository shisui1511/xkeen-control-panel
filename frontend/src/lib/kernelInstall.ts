// Установка ядра без привязки к странице: запрос установки, чтение статуса и установка с
// опросом и замком бокового меню. Страница «Службы» берёт сетевые функции и держит свой
// список ядер, тосты и опрос; шаг 1 мастера ставит ядро целиком через runKernelInstall.

import { apiFetch } from './api';
import type { KernelName } from './kernelState';
import { lockNav } from '../stores';

/** Статус установки ядра: поля ответа `GET /api/kernels/{name}/status`. */
export interface KernelInstallStatus extends Record<string, unknown> {
  status: string;
  stage?: string;
  result_kind?: string;
  result_version?: string;
  error?: string;
}

/** Шаг опроса статуса установки. */
export const KERNEL_INSTALL_POLL_MS = 2000;

/** Статусы, после которых установка не продолжается. */
export const TERMINAL_INSTALL_STATUSES: readonly string[] = ['idle', 'done', 'failed'];

/** Ошибка запроса установки: текст ответа сервера и HTTP-статус. */
export class KernelInstallError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'KernelInstallError';
    this.status = status;
  }
}

/** HTTP-статус «установка уже идёт» (со «Служб» или из другой вкладки). */
export const KERNEL_INSTALL_BUSY = 409;

/**
 * Запускает установку ядра; отказ сервера — KernelInstallError с полем `error` конверта
 * ответа (тело не JSON или без поля — «HTTP <код>»).
 */
export async function requestKernelInstall(name: KernelName): Promise<void> {
  const res = await apiFetch(`/api/kernels/${name}/install`, { method: 'POST' });
  if (!res.ok) {
    let msg = '';
    try {
      const body = await res.json();
      if (typeof body?.error === 'string') msg = body.error;
    } catch {
      // тело не JSON — остаётся код ответа
    }
    throw new KernelInstallError(msg || `HTTP ${res.status}`, res.status);
  }
}

/**
 * Читает статус установки. Ответ сервера не ok — `null`; сетевой сбой и 401 — исключение
 * (их вызывающий различает сам, как прежде страница «Службы»).
 */
export async function fetchKernelInstallStatus(
  name: KernelName,
  signal?: AbortSignal
): Promise<KernelInstallStatus | null> {
  const res = await apiFetch(`/api/kernels/${name}/status`, { signal });
  if (!res.ok) return null;
  const envelope = await res.json();
  const data = envelope?.data ?? envelope;
  if (data === null || typeof data !== 'object') return null;
  return { ...data, status: typeof data.status === 'string' ? data.status : '' };
}

/** Статус терминальный: установка закончилась или не идёт. */
export function isTerminalInstallStatus(status: string): boolean {
  return TERMINAL_INSTALL_STATUSES.includes(status);
}

function abortError(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException('Aborted', 'AbortError');
}

function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(abortError(signal));
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    function onAbort() {
      clearTimeout(timer);
      reject(abortError(signal as AbortSignal));
    }
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

/** Подряд неудачных чтений статуса (сетевой сбой), после которых опрос сдаётся. */
export const KERNEL_INSTALL_POLL_RETRIES = 3;

/** Ошибка отмены запроса: не сбой сети, повторять её не нужно. */
function isAbort(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError';
}

/**
 * Ставит ядро целиком: запрос установки, затем опрос статуса раз в KERNEL_INSTALL_POLL_MS до
 * терминального статуса. Установка уже идёт (409) — сразу опрос её статуса. Меню заморожено
 * замком на всё время и размораживается в finally. Возвращает последний статус; ответ
 * статуса не ok — статус `failed`. Сетевой сбой чтения статуса повторяется, пока не
 * наберётся KERNEL_INSTALL_POLL_RETRIES подряд; 401 и отмена не повторяются. Отмена через
 * `signal` прекращает опрос (установка на сервере продолжается) и отклоняет промис ошибкой
 * отмены.
 */
export async function runKernelInstall(
  name: KernelName,
  opts: { onUpdate?: (s: KernelInstallStatus) => void; signal?: AbortSignal } = {}
): Promise<KernelInstallStatus> {
  const { onUpdate, signal } = opts;
  const unlock = lockNav();
  try {
    if (signal?.aborted) throw abortError(signal);
    try {
      await requestKernelInstall(name);
    } catch (e: unknown) {
      if (!(e instanceof KernelInstallError && e.status === KERNEL_INSTALL_BUSY)) throw e;
    }
    let failures = 0;
    for (;;) {
      if (signal?.aborted) throw abortError(signal);
      let st: KernelInstallStatus;
      try {
        st = (await fetchKernelInstallStatus(name, signal)) ?? { status: 'failed' };
        failures = 0;
      } catch (e: unknown) {
        const status = (e as { status?: unknown } | null)?.status;
        if (signal?.aborted || isAbort(e) || status === 401) throw e;
        failures++;
        if (failures >= KERNEL_INSTALL_POLL_RETRIES) throw e;
        await wait(KERNEL_INSTALL_POLL_MS, signal);
        continue;
      }
      onUpdate?.(st);
      if (isTerminalInstallStatus(st.status)) return st;
      await wait(KERNEL_INSTALL_POLL_MS, signal);
    }
  } finally {
    unlock();
  }
}
