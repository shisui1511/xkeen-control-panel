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

/** Запускает установку ядра; отказ сервера — KernelInstallError с текстом ответа. */
export async function requestKernelInstall(name: KernelName): Promise<void> {
  const res = await apiFetch(`/api/kernels/${name}/install`, { method: 'POST' });
  if (!res.ok) {
    throw new KernelInstallError(await res.text(), res.status);
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

/**
 * Ставит ядро целиком: запрос установки, затем опрос статуса раз в KERNEL_INSTALL_POLL_MS до
 * терминального статуса. Меню заморожено замком на всё время и размораживается в finally.
 * Возвращает последний статус; ответ статуса не ok — статус `failed`. Отмена через `signal`
 * прекращает опрос (установка на сервере продолжается) и отклоняет промис ошибкой отмены.
 */
export async function runKernelInstall(
  name: KernelName,
  opts: { onUpdate?: (s: KernelInstallStatus) => void; signal?: AbortSignal } = {}
): Promise<KernelInstallStatus> {
  const { onUpdate, signal } = opts;
  const unlock = lockNav();
  try {
    if (signal?.aborted) throw abortError(signal);
    await requestKernelInstall(name);
    for (;;) {
      if (signal?.aborted) throw abortError(signal);
      const st = (await fetchKernelInstallStatus(name, signal)) ?? { status: 'failed' };
      onUpdate?.(st);
      if (isTerminalInstallStatus(st.status)) return st;
      await wait(KERNEL_INSTALL_POLL_MS, signal);
    }
  } finally {
    unlock();
  }
}
